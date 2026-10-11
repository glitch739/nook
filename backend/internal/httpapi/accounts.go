package httpapi

// Account lifecycle endpoints (ADR 0005): sign-up, email verification,
// forgot/reset password, profile, sessions, and the admin panel.

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"time"

	"nook/internal/auth"
	"nook/internal/authtokens"
	"nook/internal/db"
	"nook/internal/groups"
	"nook/internal/httpx"
	"nook/internal/mail"
	"nook/internal/spaces"
	"nook/internal/users"
)

// Registration modes (config.Config.Registration).
const (
	RegOpen     = "open"
	RegApproval = "approval"
	RegInvite   = "invite"
	RegClosed   = "closed"
)

type accountsAPI struct {
	auth         *auth.Handler
	users        *users.Store
	groups       *groups.Store
	spaces       *spaces.Store
	tokens       *authtokens.Store
	mailer       mail.Sender // nil: no email
	registration string
	publicURL    string // base for links in emails
}

// ProvisionAccount creates an account plus its personal group and
// personal space ("every account gets one"). Used by sign-up, admin
// create and cmd/nook's bootstrapAdmin.
func ProvisionAccount(ctx context.Context, us *users.Store, gs *groups.Store, ss *spaces.Store, n users.NewUser) (users.User, error) {
	u, err := us.CreateAccount(ctx, n)
	if err != nil {
		return users.User{}, err
	}
	g, err := gs.Create(ctx, "Personal", u.ID)
	if err != nil {
		return users.User{}, err
	}
	if _, err := ss.Create(ctx, "Personal Space", spaces.TypePrivate, u.ID, g.ID); err != nil {
		return users.User{}, err
	}
	return u, nil
}

func internalError(w http.ResponseWriter, err error) {
	slog.Error("internal error", "err", err)
	http.Error(w, "internal server error", http.StatusInternalServerError)
}

// sendLink emails "{publicURL}/?{param}={token}" in the background, so
// the response time doesn't reveal whether an email was sent (forgot
// password must not leak which addresses have accounts).
func (a accountsAPI) sendLink(to, subject, intro, param, token string) {
	if a.mailer == nil {
		return
	}
	link := a.publicURL + "/?" + param + "=" + url.QueryEscape(token)
	body := intro + "\n\n" + link + "\n\nIf you didn't ask for this, you can ignore this email.\n\n— Nook"
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		if err := a.mailer.Send(ctx, to, subject, body); err != nil {
			slog.Error("send mail failed", "event", "mail_failed", "err", err)
		}
	}()
}

func (a accountsAPI) sendVerification(ctx context.Context, u users.User) error {
	if a.mailer == nil || u.Email == "" {
		return nil
	}
	token, _, err := a.tokens.Create(ctx, authtokens.NewParams{Purpose: authtokens.PurposeVerifyEmail, UserID: u.ID, Email: u.Email})
	if err != nil {
		return err
	}
	a.sendLink(u.Email, "Nook – confirm your email address", "Hi "+u.DisplayName+"! Please confirm your email address:", "verify", token)
	return nil
}

// GET /api/auth/config (public)
func (a accountsAPI) configHandler(w http.ResponseWriter, r *http.Request) {
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"registration":  a.registration,
		"email_enabled": a.mailer != nil,
	})
}

// POST /api/auth/invite/check {token} (public) → {email}
func (a accountsAPI) checkInviteHandler(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Token string `json:"token"`
	}
	if !httpx.DecodeJSONBody(w, r, &req) {
		return
	}
	t, err := a.tokens.Peek(r.Context(), authtokens.PurposeInvite, req.Token)
	if err != nil {
		http.Error(w, "invalid or expired invite", http.StatusNotFound)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"email": t.Email})
}

// POST /api/auth/register (public)
// {email, username, display_name, password, invite?}
func (a accountsAPI) registerHandler(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Email       string `json:"email"`
		Username    string `json:"username"`
		DisplayName string `json:"display_name"`
		Password    string `json:"password"`
		Invite      string `json:"invite"`
	}
	if !httpx.DecodeJSONBody(w, r, &req) {
		return
	}
	if a.registration == RegClosed && req.Invite == "" {
		http.Error(w, "registration is closed", http.StatusForbidden)
		return
	}
	if a.registration == RegInvite && req.Invite == "" {
		http.Error(w, "an invite is required to register", http.StatusForbidden)
		return
	}

	n := users.NewUser{
		Username:    users.NormalizeUsername(req.Username),
		Email:       users.NormalizeEmail(req.Email),
		DisplayName: trimName(req.DisplayName),
	}
	if err := users.ValidateUsername(n.Username); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := users.ValidateEmail(n.Email); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := auth.ValidatePassword(req.Password); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if n.DisplayName == "" {
		n.DisplayName = n.Username
	}
	if _, err := a.users.GetByUsername(r.Context(), n.Username); err == nil {
		http.Error(w, "username already taken", http.StatusConflict)
		return
	}
	if _, err := a.users.GetByEmail(r.Context(), n.Email); err == nil {
		http.Error(w, "email already registered", http.StatusConflict)
		return
	}

	// An invite skips approval, may pre-join a group, and (being a link
	// the admin sent to that address) doesn't prove the email itself.
	var invite authtokens.Token
	if req.Invite != "" {
		t, err := a.tokens.Consume(r.Context(), authtokens.PurposeInvite, req.Invite)
		if err != nil {
			http.Error(w, "invalid or expired invite", http.StatusBadRequest)
			return
		}
		invite = t
	}

	n.Status = users.StatusActive
	if a.registration == RegApproval && invite.ID == "" {
		n.Status = users.StatusPending
	}
	hash, err := auth.HashPassword(req.Password)
	if err != nil {
		internalError(w, err)
		return
	}
	n.PasswordHash = hash

	u, err := ProvisionAccount(r.Context(), a.users, a.groups, a.spaces, n)
	switch {
	case errors.Is(err, users.ErrUsernameTaken):
		http.Error(w, "username already taken", http.StatusConflict)
		return
	case errors.Is(err, users.ErrEmailTaken):
		http.Error(w, "email already registered", http.StatusConflict)
		return
	case err != nil:
		internalError(w, err)
		return
	}
	if invite.ID != "" {
		_ = a.tokens.SetUser(r.Context(), invite.ID, u.ID)
		if invite.GroupID != "" && invite.GroupRole != "" {
			if err := a.groups.AddMember(r.Context(), invite.GroupID, u.ID, invite.GroupRole); err != nil {
				slog.Warn("invite group join failed", "err", err, "group_id", invite.GroupID)
			}
		}
	}
	if err := a.sendVerification(r.Context(), u); err != nil {
		slog.Warn("verification email failed", "err", err)
	}
	slog.Info("account registered", "event", "register", "user_id", u.ID, "username", u.Username,
		"status", u.Status, "ip", httpx.ClientIP(r))

	if u.Status == users.StatusPending {
		httpx.WriteJSON(w, http.StatusAccepted, map[string]any{"pending": true})
		return
	}
	if !a.auth.StartSession(w, r, u) {
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, auth.NewUserInfo(u))
}

// POST /api/auth/verify-email {token} (public)
func (a accountsAPI) verifyEmailHandler(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Token string `json:"token"`
	}
	if !httpx.DecodeJSONBody(w, r, &req) {
		return
	}
	t, err := a.tokens.Consume(r.Context(), authtokens.PurposeVerifyEmail, req.Token)
	if err != nil {
		http.Error(w, "invalid or expired link", http.StatusBadRequest)
		return
	}
	u, err := a.users.GetByID(r.Context(), t.UserID)
	if err != nil {
		internalError(w, err)
		return
	}
	// The address may have changed since the link was sent.
	if u.Email != t.Email {
		http.Error(w, "invalid or expired link", http.StatusBadRequest)
		return
	}
	if err := a.users.MarkEmailVerified(r.Context(), u.ID); err != nil {
		internalError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// POST /api/auth/forgot {email} (public). Always 202, whatever happens.
func (a accountsAPI) forgotHandler(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Email string `json:"email"`
	}
	if !httpx.DecodeJSONBody(w, r, &req) {
		return
	}
	defer w.WriteHeader(http.StatusAccepted)
	if a.mailer == nil {
		return
	}
	u, err := a.users.GetByEmail(r.Context(), req.Email)
	// Only to verified addresses: otherwise registering someone else's
	// address would route their reset links to whoever did that.
	if err != nil || u.Status != users.StatusActive || !u.EmailVerified {
		return
	}
	if recent, err := a.tokens.RecentlyIssued(r.Context(), u.ID, authtokens.PurposeReset, 2*time.Minute); err != nil || recent {
		return
	}
	token, _, err := a.tokens.Create(r.Context(), authtokens.NewParams{Purpose: authtokens.PurposeReset, UserID: u.ID})
	if err != nil {
		slog.Error("create reset token", "err", err)
		return
	}
	slog.Info("password reset requested", "event", "reset_requested", "user_id", u.ID, "ip", httpx.ClientIP(r))
	a.sendLink(u.Email, "Nook – reset your password", "Hi "+u.DisplayName+"! Set a new password with the link below (valid for 1 hour):", "reset", token)
}

// POST /api/auth/reset {token, password} (public)
func (a accountsAPI) resetHandler(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Token    string `json:"token"`
		Password string `json:"password"`
	}
	if !httpx.DecodeJSONBody(w, r, &req) {
		return
	}
	if err := auth.ValidatePassword(req.Password); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	t, err := a.tokens.Consume(r.Context(), authtokens.PurposeReset, req.Token)
	if err != nil {
		http.Error(w, "invalid or expired link", http.StatusBadRequest)
		return
	}
	u, err := a.users.GetByID(r.Context(), t.UserID)
	if err != nil || u.Status == users.StatusDisabled {
		http.Error(w, "invalid or expired link", http.StatusBadRequest)
		return
	}
	hash, err := auth.HashPassword(req.Password)
	if err != nil {
		internalError(w, err)
		return
	}
	if err := a.users.UpdatePassword(r.Context(), u.ID, hash); err != nil {
		internalError(w, err)
		return
	}
	// A self-requested link went to the account's address: that proves it.
	if t.CreatedBy == "" && u.Email != "" {
		_ = a.users.MarkEmailVerified(r.Context(), u.ID)
	}
	if err := a.auth.Sessions.DeleteAllForUser(r.Context(), u.ID, ""); err != nil {
		internalError(w, err)
		return
	}
	slog.Info("password reset", "event", "password_reset", "user_id", u.ID, "ip", httpx.ClientIP(r))
	w.WriteHeader(http.StatusNoContent)
}

func trimName(s string) string {
	r := []rune(s)
	for len(r) > 0 && (r[0] == ' ' || r[0] == '\t') {
		r = r[1:]
	}
	for len(r) > 0 && (r[len(r)-1] == ' ' || r[len(r)-1] == '\t') {
		r = r[:len(r)-1]
	}
	if len(r) > 80 {
		r = r[:80]
	}
	return string(r)
}

// ---- profile (RequireAuth) ----

func (a accountsAPI) currentUser(w http.ResponseWriter, r *http.Request) (users.User, bool) {
	u, err := a.users.GetByID(r.Context(), auth.UserIDFromContext(r.Context()))
	if err != nil {
		internalError(w, err)
		return users.User{}, false
	}
	return u, true
}

// GET /api/me
func (a accountsAPI) meHandler(w http.ResponseWriter, r *http.Request) {
	if u, ok := a.currentUser(w, r); ok {
		httpx.WriteJSON(w, http.StatusOK, auth.NewUserInfo(u))
	}
}

// GET /api/me/preferences: the account's UI preferences (theme etc.),
// a JSON object the frontend owns.
func (a accountsAPI) preferencesHandler(w http.ResponseWriter, r *http.Request) {
	prefs, err := a.users.Preferences(r.Context(), auth.UserIDFromContext(r.Context()))
	if err != nil {
		internalError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, prefs)
}

// PUT /api/me/preferences {key: value, ...}: sets those top-level keys
// (null removes one), keeps the others. Body capped by loginBodyLimit.
func (a accountsAPI) updatePreferencesHandler(w http.ResponseWriter, r *http.Request) {
	var patch map[string]json.RawMessage
	if !httpx.DecodeJSONBody(w, r, &patch) {
		return
	}
	if patch == nil {
		http.Error(w, "expected a JSON object", http.StatusBadRequest)
		return
	}
	prefs, err := a.users.MergePreferences(r.Context(), auth.UserIDFromContext(r.Context()), patch)
	if err != nil {
		internalError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, prefs)
}

// PUT /api/me {display_name?, email?}
func (a accountsAPI) updateMeHandler(w http.ResponseWriter, r *http.Request) {
	var req struct {
		DisplayName     *string `json:"display_name"`
		Email           *string `json:"email"`
		CurrentPassword string  `json:"current_password"`
	}
	if !httpx.DecodeJSONBody(w, r, &req) {
		return
	}
	u, ok := a.currentUser(w, r)
	if !ok {
		return
	}
	// A new email can reset the password, so changing it needs the
	// current one: a borrowed session must not become a lasting takeover.
	if req.Email != nil && users.NormalizeEmail(*req.Email) != u.Email {
		if ok, err := auth.VerifyPassword(u.PasswordHash, req.CurrentPassword); err != nil || !ok {
			http.Error(w, "current password is incorrect", http.StatusForbidden)
			return
		}
	}
	if req.DisplayName != nil {
		name := trimName(*req.DisplayName)
		if name == "" {
			http.Error(w, "display name is required", http.StatusBadRequest)
			return
		}
		if err := a.users.UpdateDisplayName(r.Context(), u.ID, name); err != nil {
			internalError(w, err)
			return
		}
	}
	if req.Email != nil && users.NormalizeEmail(*req.Email) != u.Email {
		email := users.NormalizeEmail(*req.Email)
		if err := users.ValidateEmail(email); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if err := a.users.UpdateEmail(r.Context(), u.ID, email); err != nil {
			if errors.Is(err, users.ErrEmailTaken) {
				http.Error(w, "email already registered", http.StatusConflict)
				return
			}
			internalError(w, err)
			return
		}
	}
	u, ok = a.currentUser(w, r)
	if !ok {
		return
	}
	if req.Email != nil && !u.EmailVerified {
		if err := a.sendVerification(r.Context(), u); err != nil {
			slog.Warn("verification email failed", "err", err)
		}
	}
	httpx.WriteJSON(w, http.StatusOK, auth.NewUserInfo(u))
}

// POST /api/me/verify-email — resend the verification link.
func (a accountsAPI) resendVerificationHandler(w http.ResponseWriter, r *http.Request) {
	u, ok := a.currentUser(w, r)
	if !ok {
		return
	}
	if a.mailer == nil || u.EmailVerified || u.Email == "" {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if recent, _ := a.tokens.RecentlyIssued(r.Context(), u.ID, authtokens.PurposeVerifyEmail, 2*time.Minute); recent {
		http.Error(w, "please wait a moment before requesting another email", http.StatusTooManyRequests)
		return
	}
	if err := a.sendVerification(r.Context(), u); err != nil {
		internalError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// PUT /api/me/password {current_password, new_password}
func (a accountsAPI) changePasswordHandler(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Current string `json:"current_password"`
		New     string `json:"new_password"`
	}
	if !httpx.DecodeJSONBody(w, r, &req) {
		return
	}
	u, ok := a.currentUser(w, r)
	if !ok {
		return
	}
	if ok, err := auth.VerifyPassword(u.PasswordHash, req.Current); err != nil || !ok {
		http.Error(w, "current password is incorrect", http.StatusForbidden)
		return
	}
	if err := auth.ValidatePassword(req.New); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	hash, err := auth.HashPassword(req.New)
	if err != nil {
		internalError(w, err)
		return
	}
	if err := a.users.UpdatePassword(r.Context(), u.ID, hash); err != nil {
		internalError(w, err)
		return
	}
	if err := a.auth.Sessions.DeleteAllForUser(r.Context(), u.ID, auth.SessionIDFromContext(r.Context())); err != nil {
		internalError(w, err)
		return
	}
	slog.Info("password changed", "event", "password_changed", "user_id", u.ID, "ip", httpx.ClientIP(r))
	w.WriteHeader(http.StatusNoContent)
}

type sessionResponse struct {
	ID         string `json:"id"`
	CreatedAt  string `json:"created_at"`
	LastSeenAt string `json:"last_seen_at"`
	UserAgent  string `json:"user_agent"`
	IP         string `json:"ip"`
	Current    bool   `json:"current"`
}

// GET /api/me/sessions
func (a accountsAPI) listSessionsHandler(w http.ResponseWriter, r *http.Request) {
	list, err := a.auth.Sessions.ListForUser(r.Context(), auth.UserIDFromContext(r.Context()))
	if err != nil {
		internalError(w, err)
		return
	}
	cur := auth.SessionIDFromContext(r.Context())
	out := make([]sessionResponse, 0, len(list))
	for _, s := range list {
		out = append(out, sessionResponse{ID: s.ID, CreatedAt: s.CreatedAt, LastSeenAt: s.LastSeenAt, UserAgent: s.UserAgent, IP: s.IP, Current: s.ID == cur})
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

// DELETE /api/me/sessions/{sessionID}
func (a accountsAPI) revokeSessionHandler(w http.ResponseWriter, r *http.Request) {
	found, err := a.auth.Sessions.DeleteForUser(r.Context(), auth.UserIDFromContext(r.Context()), r.PathValue("sessionID"))
	if err != nil {
		internalError(w, err)
		return
	}
	if !found {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// DELETE /api/me/sessions — sign out everywhere else.
func (a accountsAPI) revokeOtherSessionsHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if err := a.auth.Sessions.DeleteAllForUser(ctx, auth.UserIDFromContext(ctx), auth.SessionIDFromContext(ctx)); err != nil {
		internalError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ---- admin (RequireAdmin) ----

type adminUserResponse struct {
	ID            string `json:"id"`
	Username      string `json:"username"`
	DisplayName   string `json:"display_name"`
	Email         string `json:"email"`
	EmailVerified bool   `json:"email_verified"`
	Status        string `json:"status"`
	IsAdmin       bool   `json:"is_admin"`
	CreatedAt     string `json:"created_at"`
}

func toAdminUser(u users.User) adminUserResponse {
	return adminUserResponse{u.ID, u.Username, u.DisplayName, u.Email, u.EmailVerified, u.Status, u.IsAdmin, u.CreatedAt}
}

// GET /api/admin/users
func (a accountsAPI) adminListUsersHandler(w http.ResponseWriter, r *http.Request) {
	list, err := a.users.List(r.Context())
	if err != nil {
		internalError(w, err)
		return
	}
	out := make([]adminUserResponse, 0, len(list))
	for _, u := range list {
		out = append(out, toAdminUser(u))
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

// POST /api/admin/users {username, email, display_name, password}
// Direct creation fallback: the person must change the password at
// first login.
func (a accountsAPI) adminCreateUserHandler(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username    string `json:"username"`
		Email       string `json:"email"`
		DisplayName string `json:"display_name"`
		Password    string `json:"password"`
	}
	if !httpx.DecodeJSONBody(w, r, &req) {
		return
	}
	n := users.NewUser{
		Username: users.NormalizeUsername(req.Username), Email: users.NormalizeEmail(req.Email),
		DisplayName: trimName(req.DisplayName), MustChangePassword: true,
	}
	if err := users.ValidateUsername(n.Username); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := users.ValidateEmail(n.Email); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := auth.ValidatePassword(req.Password); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if n.DisplayName == "" {
		n.DisplayName = n.Username
	}
	hash, err := auth.HashPassword(req.Password)
	if err != nil {
		internalError(w, err)
		return
	}
	n.PasswordHash = hash
	u, err := ProvisionAccount(r.Context(), a.users, a.groups, a.spaces, n)
	switch {
	case errors.Is(err, users.ErrUsernameTaken):
		http.Error(w, "username already taken", http.StatusConflict)
		return
	case errors.Is(err, users.ErrEmailTaken):
		http.Error(w, "email already registered", http.StatusConflict)
		return
	case err != nil:
		internalError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, toAdminUser(u))
}

// PUT /api/admin/users/{userID} {status?, is_admin?}
// DELETE /api/admin/users/{userID} (admin): permanently deletes a
// disabled account. Its private groups/spaces go with it; anything it
// shared with others is handed to the admin doing the delete (see
// users.Store.DeleteDisabled). Disable first is a deliberate two-step:
// a click on "Delete" can never hit an account still in use.
func (a accountsAPI) adminDeleteUserHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	me := auth.UserIDFromContext(ctx)
	targetID := r.PathValue("userID")
	if targetID == me {
		http.Error(w, "you cannot delete your own account", http.StatusBadRequest)
		return
	}
	target, err := a.users.GetByID(ctx, targetID)
	err2 := err
	if err2 == nil {
		err2 = a.users.DeleteDisabled(ctx, targetID, me)
	}
	switch {
	case errors.Is(err2, db.ErrNotFound):
		http.Error(w, "not found", http.StatusNotFound)
		return
	case errors.Is(err2, users.ErrNotDisabled):
		http.Error(w, "disable the account before deleting it", http.StatusConflict)
		return
	case err2 != nil:
		internalError(w, err2)
		return
	}
	slog.Info("account deleted", "event", "user_deleted", "user_id", target.ID, "username", target.Username,
		"by", me, "ip", httpx.ClientIP(r))
	w.WriteHeader(http.StatusNoContent)
}

func (a accountsAPI) adminUpdateUserHandler(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Status  *string `json:"status"`
		IsAdmin *bool   `json:"is_admin"`
	}
	if !httpx.DecodeJSONBody(w, r, &req) {
		return
	}
	ctx := r.Context()
	target, err := a.users.GetByID(ctx, r.PathValue("userID"))
	if errors.Is(err, db.ErrNotFound) {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	if err != nil {
		internalError(w, err)
		return
	}
	if req.Status != nil && *req.Status != users.StatusActive && *req.Status != users.StatusDisabled {
		http.Error(w, "status must be active or disabled", http.StatusBadRequest)
		return
	}
	losesAdmin := target.IsAdmin && target.Status == users.StatusActive &&
		((req.IsAdmin != nil && !*req.IsAdmin) || (req.Status != nil && *req.Status != users.StatusActive))
	if losesAdmin {
		n, err := a.users.CountActiveAdmins(ctx)
		if err != nil {
			internalError(w, err)
			return
		}
		if n <= 1 {
			http.Error(w, "cannot remove the last active admin", http.StatusConflict)
			return
		}
	}
	if req.IsAdmin != nil {
		if err := a.users.SetAdmin(ctx, target.ID, *req.IsAdmin); err != nil {
			internalError(w, err)
			return
		}
	}
	if req.Status != nil && *req.Status != target.Status {
		if err := a.users.SetStatus(ctx, target.ID, *req.Status); err != nil {
			internalError(w, err)
			return
		}
		if *req.Status == users.StatusDisabled {
			if err := a.auth.Sessions.DeleteAllForUser(ctx, target.ID, ""); err != nil {
				internalError(w, err)
				return
			}
		}
	}
	slog.Info("admin updated user", "event", "admin_user_update", "admin_id", auth.UserIDFromContext(ctx),
		"user_id", target.ID, "status", req.Status, "is_admin", req.IsAdmin)
	target, _ = a.users.GetByID(ctx, target.ID)
	httpx.WriteJSON(w, http.StatusOK, toAdminUser(target))
}

// POST /api/admin/users/{userID}/reset-link → {path}
// For handing a reset link over manually (no SMTP, or mail lost).
func (a accountsAPI) adminResetLinkHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	target, err := a.users.GetByID(ctx, r.PathValue("userID"))
	if errors.Is(err, db.ErrNotFound) {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	if err != nil {
		internalError(w, err)
		return
	}
	token, t, err := a.tokens.Create(ctx, authtokens.NewParams{
		Purpose: authtokens.PurposeReset, UserID: target.ID, CreatedBy: auth.UserIDFromContext(ctx), TTL: 24 * time.Hour,
	})
	if err != nil {
		internalError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"path": "/?reset=" + url.QueryEscape(token), "expires_at": t.ExpiresAt})
}

type inviteResponse struct {
	ID        string `json:"id"`
	Email     string `json:"email"`
	GroupID   string `json:"group_id"`
	GroupRole string `json:"group_role"`
	ExpiresAt string `json:"expires_at"`
	UsedAt    string `json:"used_at"`
	UserID    string `json:"user_id"`
	CreatedAt string `json:"created_at"`
	Path      string `json:"path,omitempty"`
}

func toInvite(t authtokens.Token) inviteResponse {
	return inviteResponse{t.ID, t.Email, t.GroupID, t.GroupRole, t.ExpiresAt, t.UsedAt, t.UserID, t.CreatedAt, ""}
}

// GET /api/admin/invites
func (a accountsAPI) adminListInvitesHandler(w http.ResponseWriter, r *http.Request) {
	list, err := a.tokens.ListInvites(r.Context())
	if err != nil {
		internalError(w, err)
		return
	}
	out := make([]inviteResponse, 0, len(list))
	for _, t := range list {
		out = append(out, toInvite(t))
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

// POST /api/admin/invites {email?, group_id?, role?, expires_in_hours?}
func (a accountsAPI) adminCreateInviteHandler(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Email          string `json:"email"`
		GroupID        string `json:"group_id"`
		Role           string `json:"role"`
		ExpiresInHours int    `json:"expires_in_hours"`
	}
	if !httpx.DecodeJSONBody(w, r, &req) {
		return
	}
	ctx := r.Context()
	adminID := auth.UserIDFromContext(ctx)
	email := users.NormalizeEmail(req.Email)
	if email != "" {
		if err := users.ValidateEmail(email); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
	}
	if req.GroupID != "" {
		if !isValidGroupRole(req.Role) {
			http.Error(w, "role must be one of viewer, editor, admin, owner", http.StatusBadRequest)
			return
		}
		// Same cap as adding a member directly: never above your own role.
		myRole, err := a.groups.GetMemberRole(ctx, req.GroupID, adminID)
		if err != nil || groupRoleRank[req.Role] > groupRoleRank[myRole] {
			http.Error(w, "you can only invite into your own groups, up to your own role", http.StatusForbidden)
			return
		}
	} else {
		req.Role = ""
	}
	ttl := time.Duration(req.ExpiresInHours) * time.Hour
	if ttl < 0 || ttl > 30*24*time.Hour {
		http.Error(w, "expires_in_hours must be between 1 and 720", http.StatusBadRequest)
		return
	}
	token, t, err := a.tokens.Create(ctx, authtokens.NewParams{
		Purpose: authtokens.PurposeInvite, CreatedBy: adminID, GroupID: req.GroupID, GroupRole: req.Role, Email: email, TTL: ttl,
	})
	if err != nil {
		internalError(w, err)
		return
	}
	resp := toInvite(t)
	resp.Path = "/?invite=" + url.QueryEscape(token)
	httpx.WriteJSON(w, http.StatusCreated, resp)
}

// DELETE /api/admin/invites/{inviteID}
func (a accountsAPI) adminDeleteInviteHandler(w http.ResponseWriter, r *http.Request) {
	if err := a.tokens.DeleteInvite(r.Context(), r.PathValue("inviteID")); err != nil {
		if errors.Is(err, db.ErrNotFound) {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		internalError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
