package auth

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"nook/internal/dbtest"
	"nook/internal/users"
)

func newTestHandler(t *testing.T, conn *sql.DB) (*Handler, users.User) {
	t.Helper()

	hash, err := HashPassword("s3cret-password")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	user, err := users.NewStore(conn).Create(context.Background(), "alice", hash, "Alice")
	if err != nil {
		t.Fatalf("create test user: %v", err)
	}

	return NewHandler(users.NewStore(conn), NewSessionStore(conn), true), user
}

func loginRequestBody(t *testing.T, username, password string) *bytes.Buffer {
	t.Helper()
	body, err := json.Marshal(loginRequest{Username: username, Password: password})
	if err != nil {
		t.Fatalf("marshal login request: %v", err)
	}
	return bytes.NewBuffer(body)
}

func TestLoginHandler_Success(t *testing.T) {
	conn := dbtest.New(t)
	h, user := newTestHandler(t, conn)

	req := httptest.NewRequest(http.MethodPost, "/api/auth/login", loginRequestBody(t, "alice", "s3cret-password"))
	rec := httptest.NewRecorder()
	h.LoginHandler(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}

	cookies := rec.Result().Cookies()
	if len(cookies) != 1 || cookies[0].Name != SessionCookieName || cookies[0].Value == "" {
		t.Fatalf("expected a %s cookie to be set, got %+v", SessionCookieName, cookies)
	}
	if !cookies[0].HttpOnly {
		t.Error("session cookie is not HttpOnly")
	}

	var resp UserInfo
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.ID != user.ID || resp.Username != "alice" {
		t.Fatalf("unexpected response: %+v", resp)
	}
	if bytes.Contains(rec.Body.Bytes(), []byte(user.PasswordHash)) {
		t.Fatal("response body leaks the password hash")
	}
}

// This is the property the timing-safety fix exists for: a wrong password
// for a real user and a login attempt for a username that doesn't exist
// at all must be indistinguishable from the response alone.
func TestLoginHandler_UnknownUserAndWrongPasswordAreIndistinguishable(t *testing.T) {
	conn := dbtest.New(t)
	h, _ := newTestHandler(t, conn)

	wrongPasswordRec := httptest.NewRecorder()
	h.LoginHandler(wrongPasswordRec, httptest.NewRequest(http.MethodPost, "/api/auth/login", loginRequestBody(t, "alice", "wrong-password")))

	unknownUserRec := httptest.NewRecorder()
	h.LoginHandler(unknownUserRec, httptest.NewRequest(http.MethodPost, "/api/auth/login", loginRequestBody(t, "nobody", "whatever")))

	if wrongPasswordRec.Code != http.StatusUnauthorized || unknownUserRec.Code != http.StatusUnauthorized {
		t.Fatalf("status codes = %d / %d, want both 401", wrongPasswordRec.Code, unknownUserRec.Code)
	}
	if wrongPasswordRec.Body.String() != unknownUserRec.Body.String() {
		t.Fatalf("response bodies differ: %q vs %q — this leaks whether a username exists",
			wrongPasswordRec.Body.String(), unknownUserRec.Body.String())
	}
	if len(wrongPasswordRec.Result().Cookies()) != 0 || len(unknownUserRec.Result().Cookies()) != 0 {
		t.Fatal("a failed login must not set a session cookie")
	}
}

func TestLoginHandler_BadJSON(t *testing.T) {
	conn := dbtest.New(t)
	h, _ := newTestHandler(t, conn)

	req := httptest.NewRequest(http.MethodPost, "/api/auth/login", bytes.NewBufferString("not json"))
	rec := httptest.NewRecorder()
	h.LoginHandler(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestLogoutHandler_NoCookieIsNotAnError(t *testing.T) {
	conn := dbtest.New(t)
	h, _ := newTestHandler(t, conn)

	req := httptest.NewRequest(http.MethodPost, "/api/auth/logout", nil)
	rec := httptest.NewRecorder()
	h.LogoutHandler(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
}

func TestLogoutHandler_InvalidatesSession(t *testing.T) {
	conn := dbtest.New(t)
	h, user := newTestHandler(t, conn)

	session, err := h.Sessions.Create(context.Background(), user.ID)
	if err != nil {
		t.Fatalf("Sessions.Create: %v", err)
	}

	logoutReq := httptest.NewRequest(http.MethodPost, "/api/auth/logout", nil)
	logoutReq.AddCookie(&http.Cookie{Name: SessionCookieName, Value: session.Token})
	h.LogoutHandler(httptest.NewRecorder(), logoutReq)

	protected := h.RequireAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	checkReq := httptest.NewRequest(http.MethodGet, "/api/me", nil)
	checkReq.AddCookie(&http.Cookie{Name: SessionCookieName, Value: session.Token})
	checkRec := httptest.NewRecorder()
	protected.ServeHTTP(checkRec, checkReq)

	if checkRec.Code != http.StatusUnauthorized {
		t.Fatalf("status after logout = %d, want 401 (session should be invalidated)", checkRec.Code)
	}
}

func TestRequireAuth_NoCookie(t *testing.T) {
	conn := dbtest.New(t)
	h, _ := newTestHandler(t, conn)

	called := false
	protected := h.RequireAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	}))

	rec := httptest.NewRecorder()
	protected.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/me", nil))

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
	if called {
		t.Fatal("next handler was called without a valid session")
	}
}

func TestRequireAuth_ValidSessionSetsUserID(t *testing.T) {
	conn := dbtest.New(t)
	h, user := newTestHandler(t, conn)

	session, err := h.Sessions.Create(context.Background(), user.ID)
	if err != nil {
		t.Fatalf("Sessions.Create: %v", err)
	}

	var gotUserID string
	protected := h.RequireAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUserID = UserIDFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/me", nil)
	req.AddCookie(&http.Cookie{Name: SessionCookieName, Value: session.Token})
	rec := httptest.NewRecorder()
	protected.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if gotUserID != user.ID {
		t.Fatalf("UserIDFromContext = %q, want %q", gotUserID, user.ID)
	}
}

// After the free failures, the account is locked: even the right
// password gets 429 until the lock runs out, and the answer is the same
// for an account that doesn't exist.
func TestLoginHandler_AccountGuardLocks(t *testing.T) {
	conn := dbtest.New(t)
	h, _ := newTestHandler(t, conn)
	h.Guard = NewAccountGuard()

	try := func(username, password string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/api/auth/login", loginRequestBody(t, username, password))
		rec := httptest.NewRecorder()
		h.LoginHandler(rec, req)
		return rec
	}

	for i := 0; i < 5; i++ {
		if rec := try("alice", "wrong-password"); rec.Code != http.StatusUnauthorized {
			t.Fatalf("free failure %d: status %d, want 401", i+1, rec.Code)
		}
	}
	try("alice", "wrong-password") // first locking failure
	rec := try("alice", "s3cret-password")
	if rec.Code != http.StatusTooManyRequests || rec.Header().Get("Retry-After") == "" {
		t.Fatalf("correct password while locked: status %d, Retry-After %q", rec.Code, rec.Header().Get("Retry-After"))
	}

	for i := 0; i < 6; i++ {
		try("nobody", "x")
	}
	if rec := try("nobody", "x"); rec.Code != http.StatusTooManyRequests {
		t.Fatalf("unknown user not locked the same way: status %d", rec.Code)
	}
}
