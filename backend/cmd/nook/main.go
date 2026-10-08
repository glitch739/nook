// Command nook runs the Nook server: applies pending database migrations
// and serves the API plus the embedded frontend build (internal/webui)
// over HTTP -- one binary, no separate frontend server or static-file
// directory to deploy alongside it.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"golang.org/x/time/rate"

	"nook/internal/activity"
	"nook/internal/archive"
	"nook/internal/assets"
	"nook/internal/auth"
	"nook/internal/authtokens"
	"nook/internal/backup"
	"nook/internal/collab"
	"nook/internal/config"
	"nook/internal/databases"
	"nook/internal/db"
	"nook/internal/groups"
	"nook/internal/httpapi"
	"nook/internal/httpx"
	"nook/internal/mail"
	"nook/internal/pages"
	"nook/internal/permissions"
	"nook/internal/ratelimit"
	"nook/internal/search"
	"nook/internal/spaces"
	"nook/internal/users"
	"nook/internal/webui"
)

func main() {
	cfg := config.Load()

	if len(os.Args) > 1 && os.Args[1] == "backup" {
		runBackup(cfg, os.Args[2:])
		return
	}
	if len(os.Args) > 1 && os.Args[1] == "sweep-uploads" {
		runSweep(cfg, os.Args[2:])
		return
	}
	if len(os.Args) > 1 {
		// Anything else is a mistake (a typo, or a subcommand an older
		// binary doesn't have yet) -- never fall through to starting a
		// second server.
		fmt.Fprintf(os.Stderr, "nook: unknown command %q\nusage: nook [backup|sweep-uploads] [flags]\n", os.Args[1])
		os.Exit(2)
	}

	// JSON to stdout: one line per event, machine-parseable. This is the
	// self-hosted-appropriate "log collector" — write structured lines to
	// stdout and let whatever's running the process capture them
	// (`docker logs`, journald, a redirected file, or piped into a real
	// aggregator like Loki/Promtail if someone wants one). Nook's code
	// doesn't need to know or care which; that's a deploy-time choice,
	// not something to bake in here.
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	conn, err := db.Open(cfg.DBPath)
	if err != nil {
		log.Fatalf("open database: %v", err)
	}
	defer conn.Close()

	migrateCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	err = db.Migrate(migrateCtx, conn)
	cancel()
	if err != nil {
		log.Fatalf("run migrations: %v", err)
	}

	usersStore := users.NewStore(conn)
	sessionStore := auth.NewSessionStore(conn)
	authHandler := auth.NewHandler(usersStore, sessionStore, cfg.CookieSecure)
	authHandler.Logger = logger
	authHandler.Guard = auth.NewAccountGuard()
	spacesStore := spaces.NewStore(conn)
	groupsStore := groups.NewStore(conn)
	pagesStore := pages.NewStore(conn)
	databasesStore := databases.NewStore(conn)
	assetsStore, err := assets.NewStore(conn, cfg.UploadsDir)
	if err != nil {
		log.Fatalf("set up uploads directory: %v", err)
	}
	permChecker := permissions.NewChecker(conn)
	tokensStore := authtokens.NewStore(conn)
	activityStore := activity.NewStore(conn)
	collabStore := collab.NewStore(conn)
	collabHub := collab.NewHub(collabStore, collab.Hooks{
		OnEdit: func(ctx context.Context, userID, pageID string) {
			p, err := pagesStore.GetByID(ctx, pageID)
			if err != nil {
				return
			}
			if err := activityStore.Record(ctx, userID, p.SpaceID, pageID, activity.PageEdit, nil); err != nil {
				logger.Warn("activity record failed", "err", err)
			}
		},
	})

	if err := httpx.SetTrustedProxies(cfg.TrustedProxies); err != nil {
		log.Fatalf("NOOK_TRUSTED_PROXIES: %v", err)
	}

	var mailer mail.Sender
	switch {
	case cfg.SMTPHost != "":
		if cfg.PublicURL == "" {
			// Links in emails must never be built from the request's Host
			// header: an attacker could otherwise get a victim's reset
			// link pointed at their own domain.
			log.Fatalf("NOOK_PUBLIC_URL is required when NOOK_SMTP_HOST is set")
		}
		mailer = mail.SMTPSender{Host: cfg.SMTPHost, Port: cfg.SMTPPort, User: cfg.SMTPUser, Pass: cfg.SMTPPass, From: cfg.SMTPFrom}
	case cfg.DevMail == "log":
		mailer = mail.LogSender{Logger: logger}
		if cfg.PublicURL == "" {
			cfg.PublicURL = "http://localhost" + cfg.Addr
		}
	}

	if err := bootstrapAdmin(context.Background(), usersStore, groupsStore, spacesStore, cfg); err != nil {
		log.Fatalf("bootstrap admin user: %v", err)
	}

	// Login is deliberately expensive per-attempt (argon2id runs even on
	// unknown usernames, see internal/auth's anti-enumeration fix), so it
	// gets a much tighter budget than ordinary traffic: 5 attempts
	// immediately, refilling 1 every 12s (5/min sustained) per IP.
	// GeneralLimiter is a much looser baseline against endpoint-flooding
	// generally: 10 req/s with a burst of 30 per IP. Both keep an idle
	// IP's bucket for 10 minutes before it's eligible for eviction — long
	// enough to span a normal session, bounded so memory doesn't grow
	// forever with the number of distinct IPs seen.
	loginLimiter := ratelimit.New(rate.Every(12*time.Second), 5, 10*time.Minute)
	loginLimiter.Logger = logger
	generalLimiter := ratelimit.New(rate.Limit(10), 30, 10*time.Minute)
	generalLimiter.Logger = logger
	// Budget per signed-in session (not per IP: a household shares one
	// public IP), with a 3x wider per-IP ceiling in front so unverified
	// cookies can't be rotated to escape limiting.
	generalLimiter.Key = httpapi.SessionOrIPKey
	ipCeiling := ratelimit.New(rate.Limit(30), 90, 10*time.Minute)
	ipCeiling.Logger = logger

	webHandler, err := webui.Handler()
	if err != nil {
		log.Fatalf("set up embedded frontend: %v", err)
	}

	mux := httpapi.NewMux(httpapi.Deps{
		Auth:           authHandler,
		Users:          usersStore,
		Spaces:         spacesStore,
		Groups:         groupsStore,
		Pages:          pagesStore,
		Databases:      databasesStore,
		Assets:         assetsStore,
		Permissions:    permChecker,
		AuthTokens:     tokensStore,
		Activity:       activityStore,
		Archive:        archive.NewStore(conn),
		Search:         search.NewStore(conn),
		Collab:         collabStore,
		CollabHub:      collabHub,
		Mailer:         mailer,
		Registration:   cfg.Registration,
		PublicURL:      cfg.PublicURL,
		LoginLimiter:   loginLimiter,
		GeneralLimiter: generalLimiter,
		IPCeiling:      ipCeiling,
		RequestTimeout: cfg.RequestTimeout,
		WebUI:          webHandler,
	})

	if strings.HasPrefix(cfg.PublicURL, "https://") && cfg.Registration == "open" {
		logger.Warn("open registration on an internet-facing instance: anyone can create an account",
			"public_url", cfg.PublicURL)
	}

	// Explicit timeouts instead of http.ListenAndServe's none-at-all: a
	// client that opens connections and then trickles headers (or never
	// sends the next request on a keep-alive) would otherwise hold a
	// goroutine and a socket each, indefinitely ("slowloris").
	// ReadTimeout/WriteTimeout stay unset on purpose: they'd also cut off
	// the long-lived collab WebSocket and large uploads, which are
	// bounded by RequestTimeout/maxBody per route instead.
	srv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    64 << 10,
	}

	// Graceful stop on SIGTERM (systemd stop/restart) and Ctrl-C: stop
	// accepting, let in-flight requests finish for a few seconds.
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-stop
		logger.Info("shutting down")
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
		// Refresh the query planner's statistics on the way out (cheap;
		// SQLite only re-analyzes tables that changed enough).
		if _, err := conn.Exec("PRAGMA optimize;"); err != nil {
			logger.Warn("PRAGMA optimize failed", "err", err)
		}
	}()

	logger.Info("nook listening", "addr", cfg.Addr, "db_path", cfg.DBPath, "registration", cfg.Registration)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatalf("server: %v", err)
	}
}

// runBackup implements `nook backup [-dir DIR] [-keep N]`: a consistent
// snapshot of the database and uploads (see internal/backup), safe to
// run while the server is running, then pruning to the newest N.
func runBackup(cfg config.Config, args []string) {
	fs := flag.NewFlagSet("backup", flag.ExitOnError)
	dir := fs.String("dir", "backups", "directory to write backups into")
	keep := fs.Int("keep", 14, "number of most recent backups to keep (0 = keep all)")
	_ = fs.Parse(args)

	conn, err := db.Open(cfg.DBPath)
	if err != nil {
		log.Fatalf("open database: %v", err)
	}
	defer conn.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	path, err := backup.Create(ctx, conn, cfg.UploadsDir, *dir, time.Now())
	if err != nil {
		log.Fatalf("backup: %v", err)
	}
	removed, err := backup.Prune(*dir, *keep)
	if err != nil {
		log.Fatalf("prune old backups: %v", err)
	}
	slog.Info("backup written", "path", path, "pruned", len(removed))
}

// bootstrapAdmin creates the NOOK_ADMIN_USERNAME account (with
// NOOK_ADMIN_PASSWORD) plus their personal group and the personal space
// inside it, if both env vars are set and that username doesn't already
// exist. Nook has no public registration, so this is how the very
// first account gets created; safe to leave set across restarts since
// it's a no-op once the account exists.
//
// The personal-group-then-personal-space creation here is a stand-in
// for "every new account gets one" -- the group-invite endpoint
// (internal/httpapi/groups.go's inviteMemberHandler) is the only other
// user-creation path, and does the identical two calls for the same
// reason.
func bootstrapAdmin(ctx context.Context, userStore *users.Store, groupsStore *groups.Store, spacesStore *spaces.Store, cfg config.Config) error {
	if cfg.AdminUsername == "" || cfg.AdminPassword == "" {
		return nil
	}

	_, err := userStore.GetByUsername(ctx, cfg.AdminUsername)
	if err == nil {
		return nil // already exists, nothing to do
	}
	if !errors.Is(err, db.ErrNotFound) {
		return err
	}

	hash, err := auth.HashPassword(cfg.AdminPassword)
	if err != nil {
		return err
	}
	if _, err := httpapi.ProvisionAccount(ctx, userStore, groupsStore, spacesStore, users.NewUser{
		Username: users.NormalizeUsername(cfg.AdminUsername), PasswordHash: hash,
		DisplayName: cfg.AdminUsername, IsAdmin: true,
	}); err != nil {
		return err
	}
	log.Printf("created initial admin user %q (with personal group and space)", cfg.AdminUsername)
	return nil
}

// runSweep implements `nook sweep-uploads [-grace 168h] [-orphan-grace 24h] [-dry-run]`:
// removes uploaded images/files nothing references any more (see
// assets.Store.Sweep). Meant to run right after the nightly backup.
func runSweep(cfg config.Config, args []string) {
	fs := flag.NewFlagSet("sweep-uploads", flag.ExitOnError)
	grace := fs.Duration("grace", 7*24*time.Hour, "keep unreferenced images/files at least this long (undo)")
	orphanGrace := fs.Duration("orphan-grace", 24*time.Hour, "keep files without any record at least this long")
	dryRun := fs.Bool("dry-run", false, "only report what would be removed")
	_ = fs.Parse(args)

	conn, err := db.Open(cfg.DBPath)
	if err != nil {
		log.Fatalf("open database: %v", err)
	}
	defer conn.Close()
	as, err := assets.NewStore(conn, cfg.UploadsDir)
	if err != nil {
		log.Fatalf("uploads dir: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	res, err := as.Sweep(ctx, *grace, *orphanGrace, *dryRun)
	if err != nil {
		log.Fatalf("sweep: %v", err)
	}
	slog.Info("uploads swept", "dry_run", *dryRun, "images", res.Images, "attachments", res.Attachments,
		"orphan_files", res.OrphanFiles, "bytes", res.Bytes)
}
