package auth

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strconv"

	"nook/internal/db"
	"nook/internal/httpx"
	"nook/internal/users"
)

// SessionCookieName is the cookie the session ID travels in.
const SessionCookieName = "nook_session"

type contextKey int

const (
	userIDContextKey contextKey = iota
	sessionIDContextKey
	isAdminContextKey
)

// Handler wires the login/logout HTTP endpoints and the auth middleware.
type Handler struct {
	Users    *users.Store
	Sessions *SessionStore
	// SecureCookies sets the session cookie's Secure flag. Leave true for
	// any real deployment (HTTPS, typically via a reverse proxy) — a
	// browser will refuse to store or send a Secure cookie over plain
	// HTTP, which is exactly what you want in production. Set false only
	// for local development over plain http://, where a Secure cookie
	// would silently never stick (login looks like it works, then every
	// subsequent request looks logged out).
	SecureCookies bool
	// Logger records login attempts (success and failure) as a
	// structured audit trail — event, ip, username, at Info/Warn level.
	// Never the password. Defaults to slog.Default() if left nil.
	Logger *slog.Logger
	// Guard throttles repeated wrong passwords per account (see
	// AccountGuard). Optional: nil disables it.
	Guard *AccountGuard
}

func NewHandler(u *users.Store, s *SessionStore, secureCookies bool) *Handler {
	return &Handler{Users: u, Sessions: s, SecureCookies: secureCookies}
}

func (h *Handler) logger() *slog.Logger {
	if h.Logger != nil {
		return h.Logger
	}
	return slog.Default()
}

type loginRequest struct {
	// Login is a username or an email address. Username is the
	// pre-0011 field name, still accepted.
	Login    string `json:"login"`
	Username string `json:"username"`
	Password string `json:"password"`
}

// UserInfo is the JSON shape of "the current user", shared by login,
// register and GET /api/me so the frontend gets the same fields
// everywhere.
type UserInfo struct {
	ID                 string `json:"id"`
	Username           string `json:"username"`
	DisplayName        string `json:"display_name"`
	Email              string `json:"email"`
	EmailVerified      bool   `json:"email_verified"`
	IsAdmin            bool   `json:"is_admin"`
	MustChangePassword bool   `json:"must_change_password"`
}

func NewUserInfo(u users.User) UserInfo {
	return UserInfo{
		ID: u.ID, Username: u.Username, DisplayName: u.DisplayName,
		Email: u.Email, EmailVerified: u.EmailVerified,
		IsAdmin: u.IsAdmin, MustChangePassword: u.MustChangePassword,
	}
}

// dummyPasswordHash is checked against when a username doesn't exist, so
// LoginHandler always pays the cost of one argon2id run regardless of
// whether the username is real. Without this, a not-found username
// returns before VerifyPassword ever runs while a real-username-wrong-
// password does not — measurably faster, letting an attacker enumerate
// valid usernames by timing alone even though both cases return the same
// 401 body.
var dummyPasswordHash = func() string {
	h, err := HashPassword("nook-dummy-password-for-constant-time-login")
	if err != nil {
		// HashPassword only fails if crypto/rand is broken, in which case
		// the process can't do anything safely anyway.
		panic("auth: failed to prepare dummy password hash: " + err.Error())
	}
	return h
}()

// LoginHandler handles POST /api/auth/login.
// Request body: {"login": "username or email", "password": "..."}
func (h *Handler) LoginHandler(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if !httpx.DecodeJSONBody(w, r, &req) {
		return
	}
	if req.Login == "" {
		req.Login = req.Username
	}

	// Checked before any work, and answered the same way whether or not
	// the account exists (the guard keys on the submitted name only), so
	// it reveals nothing about which names are real.
	if locked, wait := h.Guard.Locked(req.Login); locked {
		h.logger().Warn("login blocked: account temporarily locked",
			"event", "login_locked",
			"ip", httpx.ClientIP(r),
			"username", req.Login,
		)
		w.Header().Set("Retry-After", strconv.Itoa(int(wait.Seconds())+1))
		http.Error(w, "too many failed attempts, try again later", http.StatusTooManyRequests)
		return
	}

	user, err := h.Users.GetByLogin(r.Context(), req.Login)
	notFound := errors.Is(err, db.ErrNotFound)
	if err != nil && !notFound {
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	// Always run VerifyPassword — against the real hash if the user
	// exists, against a fixed dummy hash otherwise — so a nonexistent
	// username takes the same time to reject as a wrong password.
	hashToCheck := dummyPasswordHash
	if !notFound {
		hashToCheck = user.PasswordHash
	}

	passwordOK, err := VerifyPassword(hashToCheck, req.Password)
	if err != nil {
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	// A disabled account gets the same answer as a wrong password.
	if notFound || !passwordOK || user.Status == users.StatusDisabled {
		// Deliberately no distinction in the log between "no such user"
		// and "wrong password" — same as the HTTP response, this is an
		// audit trail of login attempts, not a tool for narrowing down
		// which half of a guess was wrong.
		lock := h.Guard.Fail(req.Login)
		h.logger().Warn("login failed",
			"event", "login_failed",
			"ip", httpx.ClientIP(r),
			"username", req.Login,
			"account_locked_for", lock.String(),
		)
		http.Error(w, "invalid username or password", http.StatusUnauthorized)
		return
	}
	// Only revealed after a correct password.
	if user.Status == users.StatusPending {
		http.Error(w, "account awaiting approval", http.StatusForbidden)
		return
	}

	if !h.StartSession(w, r, user) {
		return
	}
	h.Guard.Succeed(req.Login)
	h.logger().Info("login succeeded",
		"event", "login_succeeded",
		"ip", httpx.ClientIP(r),
		"user_id", user.ID,
		"username", user.Username,
	)
	httpx.WriteJSON(w, http.StatusOK, NewUserInfo(user))
}

// StartSession creates a session for user and sets its cookie. Writes
// a 500 and returns false on failure.
func (h *Handler) StartSession(w http.ResponseWriter, r *http.Request, user users.User) bool {
	session, err := h.Sessions.CreateWithMeta(r.Context(), user.ID, r.UserAgent(), httpx.ClientIP(r))
	if err != nil {
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return false
	}
	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookieName,
		Value:    session.Token,
		Path:     "/",
		HttpOnly: true,
		Secure:   h.SecureCookies,
		SameSite: http.SameSiteLaxMode,
		Expires:  session.ExpiresAt,
	})
	return true
}

// LogoutHandler handles POST /api/auth/logout.
func (h *Handler) LogoutHandler(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie(SessionCookieName)

	if err == nil {
		if err := h.Sessions.Delete(r.Context(), cookie.Value); err != nil {
			http.Error(w, "internal server error", http.StatusInternalServerError)
			return
		}
	} else if !errors.Is(err, http.ErrNoCookie) {
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   h.SecureCookies,
		SameSite: http.SameSiteLaxMode,
	})

	w.WriteHeader(http.StatusOK)
}

// RequireAuth is middleware that protects handlers behind a valid
// session of an active account.
func (h *Handler) RequireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie(SessionCookieName)
		if err != nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		session, user, ok := h.ActiveSession(r.Context(), cookie.Value)
		if !ok {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		ctx := context.WithValue(r.Context(), userIDContextKey, session.UserID)
		ctx = context.WithValue(ctx, sessionIDContextKey, session.ID)
		ctx = context.WithValue(ctx, isAdminContextKey, user.IsAdmin)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// ActiveSession resolves a session cookie token to its session and user,
// ok=false unless the session is valid and the account active. Also used
// to re-check long-lived connections that outlive their opening request.
func (h *Handler) ActiveSession(ctx context.Context, token string) (Session, users.User, bool) {
	session, err := h.Sessions.Get(ctx, token)
	if err != nil {
		return Session{}, users.User{}, false
	}
	user, err := h.Users.GetByID(ctx, session.UserID)
	if err != nil || user.Status != users.StatusActive {
		return Session{}, users.User{}, false
	}
	return session, user, true
}

// RequireAdmin is RequireAuth plus "must be an instance admin".
func (h *Handler) RequireAdmin(next http.Handler) http.Handler {
	return h.RequireAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if admin, _ := r.Context().Value(isAdminContextKey).(bool); !admin {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	}))
}

// UserIDFromContext retrieves the user ID set by RequireAuth. Returns ""
// if called on a context RequireAuth never touched.
func UserIDFromContext(ctx context.Context) string {
	userID, _ := ctx.Value(userIDContextKey).(string)
	return userID
}

// SessionIDFromContext returns the public ID of the current session.
func SessionIDFromContext(ctx context.Context) string {
	id, _ := ctx.Value(sessionIDContextKey).(string)
	return id
}
