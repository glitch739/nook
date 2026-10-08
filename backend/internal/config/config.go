// Package config reads Nook's runtime configuration from environment
// variables. Kept deliberately tiny: a self-hosted single-binary app
// doesn't need a config file format or a flags library yet.
package config

import (
	"log"
	"os"
	"strings"
	"time"
)

type Config struct {
	// Addr is the address the HTTP server listens on, e.g. ":8080".
	Addr string
	// DBPath is the path to the SQLite database file.
	DBPath string
	// UploadsDir is where page cover images (internal/assets) are
	// stored on disk -- same "relative by default, set an absolute path
	// at deploy time if you want" convention as DBPath.
	UploadsDir string
	// AdminUsername/AdminPassword, if both set, create that account
	// (as instance admin) on startup if it doesn't already exist — how
	// the very first account gets in.
	AdminUsername string
	AdminPassword string
	// CookieSecure controls the session cookie's Secure flag. Defaults to
	// true (required for any real, HTTPS deployment). Set
	// NOOK_COOKIE_SECURE=false only for local development over plain
	// http:// — see internal/auth.Handler.SecureCookies for why a
	// Secure cookie silently breaks on non-HTTPS.
	CookieSecure bool
	// RequestTimeout bounds every request end-to-end (see
	// internal/httpapi.NewMux, which uses it as both an
	// http.TimeoutHandler duration and the context deadline that flows
	// into every DB call a handler makes). Keeps a stuck or slow request
	// from holding SQLite's single write connection (see internal/db.Open
	// — Nook deliberately caps it at one) away from everything else
	// indefinitely.
	RequestTimeout time.Duration

	// Registration is the sign-up mode (ADR 0005 §1): open, approval,
	// invite or closed. Defaults to open (home LAN); use approval or
	// invite before exposing Nook to the internet.
	Registration string
	// PublicURL is the base for links in emails and admin-generated
	// links, e.g. "https://nook.example.hu". If empty, links are built
	// from the request's Host header.
	PublicURL string
	// SMTP relay for verification/reset emails. Optional: without
	// SMTPHost, email features fall back to admin-generated links.
	SMTPHost, SMTPPort, SMTPUser, SMTPPass, SMTPFrom string
	// DevMail=log writes outgoing emails to the log instead of sending
	// them (LAN testing without SMTP). Ignored when SMTPHost is set.
	DevMail string
	// TrustedProxies: comma-separated IPs/CIDRs of reverse proxies
	// whose X-Forwarded-For is believed (see httpx.ClientIP).
	TrustedProxies string
}

var registrationModes = map[string]bool{"open": true, "approval": true, "invite": true, "closed": true}

func Load() Config {
	c := Config{
		Addr:           getEnv("NOOK_ADDR", ":8080"),
		DBPath:         getEnv("NOOK_DB_PATH", "nook.db"),
		UploadsDir:     getEnv("NOOK_UPLOADS_DIR", "uploads"),
		AdminUsername:  getEnv("NOOK_ADMIN_USERNAME", ""),
		AdminPassword:  getEnv("NOOK_ADMIN_PASSWORD", ""),
		CookieSecure:   getEnv("NOOK_COOKIE_SECURE", "true") != "false",
		RequestTimeout: getDuration("NOOK_REQUEST_TIMEOUT", 5*time.Second),
		Registration:   getEnv("NOOK_REGISTRATION", ""),
		PublicURL:      strings.TrimRight(getEnv("NOOK_PUBLIC_URL", ""), "/"),
		SMTPHost:       getEnv("NOOK_SMTP_HOST", ""),
		SMTPPort:       getEnv("NOOK_SMTP_PORT", "587"),
		SMTPUser:       getEnv("NOOK_SMTP_USER", ""),
		SMTPPass:       getEnv("NOOK_SMTP_PASS", ""),
		SMTPFrom:       getEnv("NOOK_SMTP_FROM", ""),
		DevMail:        getEnv("NOOK_DEV_MAIL", ""),
		TrustedProxies: getEnv("NOOK_TRUSTED_PROXIES", ""),
	}
	public := strings.HasPrefix(c.PublicURL, "https://")
	if c.Registration == "" {
		// Unset: open sign-up is convenient on a home LAN, but on an
		// internet-facing instance (an https public URL) it would let
		// anyone on the internet create a working account. Default to
		// approval there: anyone may sign up, but the account can't log
		// in until an admin approves it (Settings -> Users; admins see a
		// "waiting" badge). Open sign-up must be asked for explicitly.
		c.Registration = "open"
		if public {
			c.Registration = "approval"
		}
	}
	if public && !c.CookieSecure {
		log.Fatalf("NOOK_COOKIE_SECURE=false with an https NOOK_PUBLIC_URL: session cookies would be allowed over plain HTTP; remove it")
	}
	if !registrationModes[c.Registration] {
		log.Fatalf("NOOK_REGISTRATION must be open, approval, invite or closed (got %q)", c.Registration)
	}
	if c.SMTPHost != "" && c.SMTPFrom == "" {
		c.SMTPFrom = c.SMTPUser
	}
	return c
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func getDuration(key string, fallback time.Duration) time.Duration {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return fallback
	}
	return d
}
