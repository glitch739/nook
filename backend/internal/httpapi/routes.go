// Package httpapi wires the HTTP routes together. Handlers themselves
// live in their owning domain package (internal/auth, internal/users,
// ...) — this package's only job is composing them onto a mux.
package httpapi

import (
	"net/http"
	"strings"
	"time"

	"nook/internal/activity"
	"nook/internal/archive"
	"nook/internal/assets"
	"nook/internal/auth"
	"nook/internal/authtokens"
	"nook/internal/collab"
	"nook/internal/databases"
	"nook/internal/events"
	"nook/internal/export"
	"nook/internal/groups"
	"nook/internal/mail"
	"nook/internal/pages"
	"nook/internal/permissions"
	"nook/internal/ratelimit"
	"nook/internal/search"
	"nook/internal/spaces"
	"nook/internal/users"
)

// Deps are the handlers and stores routes are wired to. Passed in from
// cmd/nook/main.go, which owns constructing them.
type Deps struct {
	Auth        *auth.Handler
	Users       *users.Store
	Spaces      *spaces.Store
	Groups      *groups.Store
	Pages       *pages.Store
	Databases   *databases.Store
	Assets      *assets.Store
	Permissions *permissions.Checker
	AuthTokens  *authtokens.Store
	Activity    *activity.Store
	// Archive lists archived pages/spaces/groups for the archive view.
	Archive *archive.Store
	// Events pushes "sidebar changed" to open browsers (GET /api/events).
	// Optional: NewMux makes its own hub when nil.
	Events *events.Hub
	Search *search.Store
	// Collab/CollabHub: realtime editing (ADR 0006). Optional in tests.
	Collab    *collab.Store
	CollabHub *collab.Hub

	// Mailer sends verification/reset emails; nil when no SMTP (and no
	// dev log mailer) is configured.
	Mailer mail.Sender
	// Registration is the sign-up mode: open, approval, invite, closed.
	// Empty means open.
	Registration string
	// PublicURL is the base for links in emails.
	PublicURL string

	// LoginLimiter is a strict, login-specific rate limiter — separate
	// from GeneralLimiter because /api/auth/login does deliberately
	// expensive work (argon2id) on every attempt, including failed ones,
	// so it needs a much tighter budget than ordinary app traffic. See
	// docs/ROADMAP.md, "Security posture".
	LoginLimiter *ratelimit.Limiter
	// GeneralLimiter applies to every route, as a baseline against
	// endpoint-flooding generally.
	GeneralLimiter *ratelimit.Limiter
	// IPCeiling, optional: a wider per-IP limit in front of
	// GeneralLimiter, for when GeneralLimiter is keyed per session (see
	// SessionOrIPKey).
	IPCeiling *ratelimit.Limiter

	// RequestTimeout bounds every request (see config.Config.RequestTimeout).
	RequestTimeout time.Duration

	// WebUI serves the embedded frontend build (internal/webui) at "/",
	// behind everything else registered here so it never shadows an API
	// route. Optional -- nil is fine (e.g. for tests that only care
	// about the API), in which case "/" simply isn't registered.
	WebUI http.Handler
}

func NewMux(deps Deps) http.Handler {
	mux := http.NewServeMux()
	// Routes that move file bytes live on `outer`, outside the general
	// RequestTimeout (5 s) and its in-memory response buffer: an upload
	// over a slow phone connection can take minutes, and a download
	// should stream from disk, not be buffered whole. Uploads still get
	// a deadline (uploadTimeout); downloads end when the client does.
	outer := http.NewServeMux()
	upload := func(h http.Handler) http.Handler {
		return http.TimeoutHandler(h, uploadTimeout, "upload timed out")
	}

	mux.HandleFunc("GET /healthz", healthzHandler)

	mux.Handle("POST /api/auth/login",
		maxBody(loginBodyLimit)(deps.LoginLimiter.Middleware(http.HandlerFunc(deps.Auth.LoginHandler))))
	mux.Handle("POST /api/auth/logout",
		maxBody(logoutBodyLimit)(http.HandlerFunc(deps.Auth.LogoutHandler)))

	// Accounts (ADR 0005). The public endpoints share LoginLimiter's
	// strict per-IP budget: they hash passwords or hand out tokens.
	reg := deps.Registration
	if reg == "" {
		reg = RegOpen
	}
	acc := accountsAPI{auth: deps.Auth, users: deps.Users, groups: deps.Groups, spaces: deps.Spaces,
		tokens: deps.AuthTokens, mailer: deps.Mailer, registration: reg, publicURL: deps.PublicURL}
	public := func(h http.HandlerFunc) http.Handler {
		return maxBody(loginBodyLimit)(deps.LoginLimiter.Middleware(h))
	}
	authed := func(h http.HandlerFunc) http.Handler { return maxBody(loginBodyLimit)(deps.Auth.RequireAuth(h)) }
	admin := func(h http.HandlerFunc) http.Handler { return maxBody(loginBodyLimit)(deps.Auth.RequireAdmin(h)) }
	mux.HandleFunc("GET /api/auth/config", acc.configHandler)
	mux.Handle("POST /api/auth/register", public(acc.registerHandler))
	mux.Handle("POST /api/auth/invite/check", public(acc.checkInviteHandler))
	mux.Handle("POST /api/auth/verify-email", public(acc.verifyEmailHandler))
	mux.Handle("POST /api/auth/forgot", public(acc.forgotHandler))
	mux.Handle("POST /api/auth/reset", public(acc.resetHandler))

	mux.Handle("GET /api/me", authed(acc.meHandler))
	// Login budget: an email change checks the password.
	mux.Handle("PUT /api/me",
		maxBody(loginBodyLimit)(deps.LoginLimiter.Middleware(deps.Auth.RequireAuth(http.HandlerFunc(acc.updateMeHandler)))))
	mux.Handle("GET /api/me/preferences", authed(acc.preferencesHandler))
	mux.Handle("PUT /api/me/preferences", authed(acc.updatePreferencesHandler))
	mux.Handle("POST /api/me/verify-email", authed(acc.resendVerificationHandler))
	mux.Handle("PUT /api/me/password",
		maxBody(loginBodyLimit)(deps.LoginLimiter.Middleware(deps.Auth.RequireAuth(http.HandlerFunc(acc.changePasswordHandler)))))
	mux.Handle("GET /api/me/sessions", authed(acc.listSessionsHandler))
	mux.Handle("DELETE /api/me/sessions", authed(acc.revokeOtherSessionsHandler))
	mux.Handle("DELETE /api/me/sessions/{sessionID}", authed(acc.revokeSessionHandler))

	mux.Handle("GET /api/admin/users", admin(acc.adminListUsersHandler))
	mux.Handle("POST /api/admin/users", admin(acc.adminCreateUserHandler))
	mux.Handle("PUT /api/admin/users/{userID}", admin(acc.adminUpdateUserHandler))
	mux.Handle("POST /api/admin/users/{userID}/reset-link", admin(acc.adminResetLinkHandler))
	mux.Handle("DELETE /api/admin/users/{userID}", admin(acc.adminDeleteUserHandler))
	mux.Handle("GET /api/admin/invites", admin(acc.adminListInvitesHandler))
	mux.Handle("POST /api/admin/invites", admin(acc.adminCreateInviteHandler))
	mux.Handle("DELETE /api/admin/invites/{inviteID}", admin(acc.adminDeleteInviteHandler))

	spacesAPI := spacesAPI{spaces: deps.Spaces, perm: deps.Permissions}
	mux.Handle("POST /api/spaces",
		maxBody(spaceBodyLimit)(deps.Auth.RequireAuth(http.HandlerFunc(spacesAPI.createSpaceHandler))))
	mux.Handle("GET /api/spaces",
		deps.Auth.RequireAuth(http.HandlerFunc(spacesAPI.listSpacesHandler)))
	mux.Handle("PUT /api/spaces/{spaceID}",
		maxBody(spaceBodyLimit)(deps.Auth.RequireAuth(http.HandlerFunc(spacesAPI.updateSpaceHandler))))
	mux.Handle("DELETE /api/spaces/{spaceID}",
		deps.Auth.RequireAuth(http.HandlerFunc(spacesAPI.archiveSpaceHandler)))
	mux.Handle("PUT /api/spaces/{spaceID}/move",
		maxBody(spaceBodyLimit)(deps.Auth.RequireAuth(http.HandlerFunc(spacesAPI.moveSpaceHandler))))
	mux.Handle("PUT /api/spaces/order",
		maxBody(spaceBodyLimit)(deps.Auth.RequireAuth(http.HandlerFunc(spacesAPI.reorderSpacesHandler))))

	groupsAPI := groupsAPI{groups: deps.Groups, spaces: deps.Spaces, users: deps.Users, perm: deps.Permissions}
	mux.Handle("POST /api/groups",
		maxBody(groupBodyLimit)(deps.Auth.RequireAuth(http.HandlerFunc(groupsAPI.createGroupHandler))))
	mux.Handle("GET /api/groups",
		deps.Auth.RequireAuth(http.HandlerFunc(groupsAPI.listGroupsHandler)))
	mux.Handle("PUT /api/groups/{groupID}",
		maxBody(groupBodyLimit)(deps.Auth.RequireAuth(http.HandlerFunc(groupsAPI.updateGroupHandler))))
	mux.Handle("DELETE /api/groups/{groupID}",
		deps.Auth.RequireAuth(http.HandlerFunc(groupsAPI.archiveGroupHandler)))
	mux.Handle("PUT /api/groups/order",
		maxBody(groupBodyLimit)(deps.Auth.RequireAuth(http.HandlerFunc(groupsAPI.reorderGroupsHandler))))
	mux.Handle("GET /api/groups/{groupID}/members",
		deps.Auth.RequireAuth(http.HandlerFunc(groupsAPI.listMembersHandler)))
	mux.Handle("POST /api/groups/{groupID}/members",
		maxBody(inviteBodyLimit)(deps.Auth.RequireAuth(http.HandlerFunc(groupsAPI.inviteMemberHandler))))
	mux.Handle("PUT /api/groups/{groupID}/members/{userID}",
		maxBody(groupBodyLimit)(deps.Auth.RequireAuth(http.HandlerFunc(groupsAPI.updateMemberRoleHandler))))
	mux.Handle("DELETE /api/groups/{groupID}/members/{userID}",
		deps.Auth.RequireAuth(http.HandlerFunc(groupsAPI.removeMemberHandler)))

	pagesAPI := pagesAPI{pages: deps.Pages, perm: deps.Permissions, activity: deps.Activity, collab: deps.Collab}

	// Export: one page as Markdown, or everything as a ZIP (streamed,
	// so outside the general timeout).
	exp := exportAPI{pages: pagesAPI, exp: export.Exporter{Pages: deps.Pages, Spaces: deps.Spaces, Groups: deps.Groups,
		Databases: deps.Databases, Assets: deps.Assets}}
	mux.Handle("GET /api/pages/{pageID}/export.md", deps.Auth.RequireAuth(http.HandlerFunc(exp.pageMarkdownHandler)))
	outer.Handle("GET /api/export.zip", deps.Auth.RequireAuth(http.HandlerFunc(exp.zipHandler)))

	// Archive view (list + restore). Optional so tests that don't need it
	// can leave Deps.Archive nil.
	if deps.Archive != nil {
		arch := archiveAPI{archive: deps.Archive, pages: pagesAPI, spaces: deps.Spaces, groups: deps.Groups,
			perm: deps.Permissions, activity: deps.Activity}
		mux.Handle("GET /api/archive", deps.Auth.RequireAuth(http.HandlerFunc(arch.listHandler)))
		mux.Handle("POST /api/pages/{pageID}/restore", deps.Auth.RequireAuth(http.HandlerFunc(arch.restorePageHandler)))
		mux.Handle("POST /api/spaces/{spaceID}/restore", deps.Auth.RequireAuth(http.HandlerFunc(arch.restoreSpaceHandler)))
		mux.Handle("POST /api/groups/{groupID}/restore", deps.Auth.RequireAuth(http.HandlerFunc(arch.restoreGroupHandler)))
		mux.Handle("DELETE /api/archive/pages/{pageID}", deps.Auth.RequireAuth(http.HandlerFunc(arch.deletePageHandler)))
		mux.Handle("DELETE /api/archive/spaces/{spaceID}", deps.Auth.RequireAuth(http.HandlerFunc(arch.deleteSpaceHandler)))
		mux.Handle("DELETE /api/archive/groups/{groupID}", deps.Auth.RequireAuth(http.HandlerFunc(arch.deleteGroupHandler)))
	}
	mux.Handle("POST /api/spaces/{spaceID}/pages",
		maxBody(pageCreateBodyLimit)(deps.Auth.RequireAuth(http.HandlerFunc(pagesAPI.createPageHandler))))
	sidebar := sidebarAPI{groups: deps.Groups, spaces: deps.Spaces, pages: deps.Pages, perm: deps.Permissions}
	mux.Handle("GET /api/sidebar", deps.Auth.RequireAuth(http.HandlerFunc(sidebar.sidebarHandler)))
	mux.Handle("GET /api/spaces/{spaceID}/pages",
		deps.Auth.RequireAuth(http.HandlerFunc(pagesAPI.listPagesHandler)))
	mux.Handle("PUT /api/spaces/{spaceID}/pages/order",
		maxBody(pageOrderBodyLimit)(deps.Auth.RequireAuth(http.HandlerFunc(pagesAPI.reorderPageSiblingsHandler))))
	mux.Handle("GET /api/pages/{pageID}/items",
		deps.Auth.RequireAuth(http.HandlerFunc(pagesAPI.collectionItemsHandler)))
	mux.Handle("GET /api/pages/{pageID}",
		deps.Auth.RequireAuth(http.HandlerFunc(pagesAPI.getPageHandler)))
	mux.Handle("PUT /api/pages/{pageID}",
		maxBody(pageUpdateBodyLimit)(deps.Auth.RequireAuth(http.HandlerFunc(pagesAPI.updatePageHandler))))
	mux.Handle("PUT /api/pages/{pageID}/move",
		maxBody(pageOrderBodyLimit)(deps.Auth.RequireAuth(http.HandlerFunc(pagesAPI.movePageHandler))))
	mux.Handle("DELETE /api/pages/{pageID}",
		deps.Auth.RequireAuth(http.HandlerFunc(pagesAPI.archivePageHandler)))

	// Sidebar full-text search (ADR 0007).
	searchAPI := searchAPI{search: deps.Search}
	mux.Handle("GET /api/search",
		deps.Auth.RequireAuth(http.HandlerFunc(searchAPI.searchHandler)))

	// Database pages (ADR 0004): properties are a database page's
	// columns, rows are its non-archived child pages (created/deleted
	// through the ordinary /api/spaces/{spaceID}/pages and
	// /api/pages/{pageID} routes above -- nothing new needed there).
	databasesAPI := databasesAPI{databases: deps.Databases, pages: deps.Pages, spaces: deps.Spaces, perm: deps.Permissions, activity: deps.Activity}
	mux.Handle("GET /api/pages/{pageID}/people",
		deps.Auth.RequireAuth(http.HandlerFunc(databasesAPI.peopleHandler)))
	mux.Handle("POST /api/pages/{pageID}/properties",
		maxBody(propertyBodyLimit)(deps.Auth.RequireAuth(http.HandlerFunc(databasesAPI.createPropertyHandler))))
	mux.Handle("PUT /api/pages/{pageID}/columns/order",
		maxBody(propertyBodyLimit)(deps.Auth.RequireAuth(http.HandlerFunc(databasesAPI.reorderColumnsHandler))))
	mux.Handle("PUT /api/properties/{propertyID}",
		maxBody(propertyBodyLimit)(deps.Auth.RequireAuth(http.HandlerFunc(databasesAPI.updatePropertyHandler))))
	mux.Handle("DELETE /api/properties/{propertyID}",
		deps.Auth.RequireAuth(http.HandlerFunc(databasesAPI.deletePropertyHandler)))
	mux.Handle("GET /api/pages/{pageID}/rows",
		deps.Auth.RequireAuth(http.HandlerFunc(databasesAPI.listRowsHandler)))
	mux.Handle("PUT /api/pages/{rowID}/properties/{propertyID}",
		maxBody(rowValueBodyLimit)(deps.Auth.RequireAuth(http.HandlerFunc(databasesAPI.setRowPropertyHandler))))

	if deps.Activity != nil {
		actAPI := activityAPI{activity: deps.Activity, pages: deps.Pages, perm: deps.Permissions}
		mux.Handle("GET /api/pages/{pageID}/activity", deps.Auth.RequireAuth(http.HandlerFunc(actAPI.pageActivityHandler)))
		mux.Handle("GET /api/spaces/{spaceID}/activity", deps.Auth.RequireAuth(http.HandlerFunc(actAPI.spaceActivityHandler)))
	}

	// Page cover images (ADR 0004's deliberately-last piece).
	coversAPI := coversAPI{assets: deps.Assets, pages: deps.Pages, perm: deps.Permissions}
	outer.Handle("PUT /api/pages/{pageID}/cover",
		upload(maxBody(coverUploadBodyLimit)(deps.Auth.RequireAuth(http.HandlerFunc(coversAPI.uploadCoverHandler)))))
	outer.Handle("GET /api/pages/{pageID}/cover",
		deps.Auth.RequireAuth(http.HandlerFunc(coversAPI.getCoverHandler)))
	mux.Handle("DELETE /api/pages/{pageID}/cover",
		deps.Auth.RequireAuth(http.HandlerFunc(coversAPI.deleteCoverHandler)))
	mux.Handle("GET /api/pages/{pageID}/cover/meta",
		deps.Auth.RequireAuth(http.HandlerFunc(coversAPI.getCoverMetaHandler)))

	// Images inside a page's text (pasted, dropped, or picked).
	imagesAPI := imagesAPI{covers: coversAPI}
	outer.Handle("POST /api/pages/{pageID}/images",
		upload(maxBody(imageUploadBodyLimit)(deps.Auth.RequireAuth(http.HandlerFunc(imagesAPI.uploadImageHandler)))))
	outer.Handle("GET /api/pages/{pageID}/images/{imageID}",
		deps.Auth.RequireAuth(http.HandlerFunc(imagesAPI.getImageHandler)))

	// Files attached inside a page's text (PDF, Word, ...).
	attachmentsAPI := attachmentsAPI{covers: coversAPI}
	outer.Handle("POST /api/pages/{pageID}/attachments",
		upload(maxBody(attachmentUploadBodyLimit)(deps.Auth.RequireAuth(http.HandlerFunc(attachmentsAPI.uploadHandler)))))
	outer.Handle("GET /api/pages/{pageID}/attachments/{attachmentID}",
		deps.Auth.RequireAuth(http.HandlerFunc(attachmentsAPI.downloadHandler)))
	mux.Handle("PUT /api/pages/{pageID}/cover/position",
		maxBody(coverPositionBodyLimit)(deps.Auth.RequireAuth(http.HandlerFunc(coversAPI.updateCoverPositionHandler))))

	if deps.WebUI != nil {
		mux.Handle("/", deps.WebUI)
	}

	// http.TimeoutHandler derives a context with this deadline for the
	// request it wraps and races the handler against it — if the handler
	// (and, transitively, any DB call using r.Context(), which is every
	// one of them) hasn't responded in time, the client gets a 503
	// instead of hanging forever. This is what actually gives request
	// contexts a deadline; every repository method already threads
	// ctx through to database/sql, so this one wrapper is what makes
	// that matter instead of being ctx.Background() in practice.
	hub := deps.Events
	if hub == nil {
		hub = events.NewHub()
	}
	// Long-lived stream: outside the timeout and gzip, like collab.
	outer.Handle("GET /api/events", deps.Auth.RequireAuth(eventsHandler(hub)))
	var handler http.Handler = gzipJSON(http.TimeoutHandler(notifyChanges(hub, mux), deps.RequestTimeout, "request timed out"))

	// The collab WebSocket is long-lived, so it must bypass
	// TimeoutHandler (which also can't hijack a connection).
	if deps.CollabHub != nil {
		ca := collabAPI{hub: deps.CollabHub, pages: deps.Pages, perm: deps.Permissions, auth: deps.Auth}
		outer.Handle("GET /api/pages/{pageID}/collab", deps.Auth.RequireAuth(http.HandlerFunc(ca.collabHandler)))
	}
	outer.Handle("/", handler)
	handler = outer
	handler = sameOriginWrites(deps.PublicURL, handler)
	handler = securityHeaders(handler, strings.HasPrefix(deps.PublicURL, "https://"))

	// Rate limiting stays outermost: it's a cheap in-memory check and
	// should reject abusive traffic before it costs a goroutine sitting
	// inside TimeoutHandler.
	limited := deps.GeneralLimiter.Middleware(handler)
	if deps.IPCeiling != nil {
		limited = deps.IPCeiling.Middleware(limited)
	}
	return exemptStaticAssets(limited, handler)
}

// exemptStaticAssets routes GET/HEAD /assets/* (the embedded frontend
// build's content-hashed, immutable files, see internal/webui) around
// the general rate limiter, everything else through it.
//
// Why: the icon picker's search renders each matching lucide icon as
// its own /assets/<name>-<hash>.svg -- up to 200 per keystroke -- and a
// cold page load alone fetches a few dozen JS/CSS/SVG files. That blew
// straight through the 30-request burst: the excess icons came back 429
// and rendered as broken images, and the same flood starved real API
// calls (/api/spaces, /api/groups) of their budget too. The earlier
// "broken images in the picker" (DEVLOG 2026-09-08, blamed on the
// browser's per-origin connection limit) was this same 429 all along.
// These files are served from memory with a one-year immutable
// Cache-Control, so each is fetched at most once per browser; limiting
// them buys no protection worth having (a real flood needs a CDN in
// front, not app code) and costs real breakage. The limiter still
// covers every API route and index.html.
//
// Images inside a page's text (GET /api/pages/{id}/images/{id}) are
// exempt for the same reason: a page with a few dozen screenshots
// fetches them all at once, and each id is immutable and cached for a
// year. They still go through RequireAuth and the page permission
// check; only the per-IP request budget is skipped.
func exemptStaticAssets(limited, unlimited http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if (r.Method == http.MethodGet || r.Method == http.MethodHead) &&
			(strings.HasPrefix(r.URL.Path, "/assets/") || isPageImagePath(r.URL.Path)) {
			unlimited.ServeHTTP(w, r)
			return
		}
		limited.ServeHTTP(w, r)
	})
}

// isPageImagePath matches /api/pages/{pageID}/images/{imageID} exactly.
func isPageImagePath(p string) bool {
	parts := strings.Split(strings.TrimPrefix(p, "/api/pages/"), "/")
	return strings.HasPrefix(p, "/api/pages/") && len(parts) == 3 &&
		parts[0] != "" && parts[1] == "images" && parts[2] != ""
}
