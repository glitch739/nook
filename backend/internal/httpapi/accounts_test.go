package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"nook/internal/assets"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/time/rate"

	"nook/internal/activity"
	"nook/internal/archive"
	"nook/internal/auth"
	"nook/internal/authtokens"
	"nook/internal/databases"
	"nook/internal/dbtest"
	"nook/internal/groups"
	"nook/internal/pages"
	"nook/internal/permissions"
	"nook/internal/ratelimit"
	"nook/internal/search"
	"nook/internal/spaces"
	"nook/internal/users"
)

// fakeMailer captures links instead of sending.
type fakeMailer struct {
	mu   sync.Mutex
	sent []string
}

func (f *fakeMailer) Send(_ context.Context, to, subject, body string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sent = append(f.sent, to+"|"+body)
	return nil
}

// lastToken waits for the async send and extracts ?param=token.
func (f *fakeMailer) lastToken(t *testing.T, param string) string {
	t.Helper()
	for i := 0; i < 100; i++ {
		f.mu.Lock()
		for j := len(f.sent) - 1; j >= 0; j-- {
			if k := strings.Index(f.sent[j], "?"+param+"="); k >= 0 {
				rest := f.sent[j][k+len(param)+2:]
				tok := strings.Fields(rest)[0]
				f.mu.Unlock()
				v, _ := url.QueryUnescape(tok)
				return v
			}
		}
		f.mu.Unlock()
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("no %s mail sent", param)
	return ""
}

type testEnv struct {
	srv   *httptest.Server
	users *users.Store
	mail  *fakeMailer
}

func newTestEnv(t *testing.T, registration string) *testEnv {
	conn := dbtest.New(t)
	us := users.NewStore(conn)
	fm := &fakeMailer{}
	lim := ratelimit.New(rate.Inf, 1000, time.Minute)
	as, err := assets.NewStore(conn, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	h := NewMux(Deps{
		Assets: as,
		Auth:   auth.NewHandler(us, auth.NewSessionStore(conn), false), Users: us,
		Spaces: spaces.NewStore(conn), Groups: groups.NewStore(conn), Pages: pages.NewStore(conn),
		Databases: databases.NewStore(conn), Permissions: permissions.NewChecker(conn),
		AuthTokens: authtokens.NewStore(conn), Activity: activity.NewStore(conn),
		Search: search.NewStore(conn), Archive: archive.NewStore(conn),
		Mailer: fm, Registration: registration, PublicURL: "http://nook.test",
		LoginLimiter: lim, GeneralLimiter: lim, RequestTimeout: 5 * time.Second,
	})
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return &testEnv{srv: srv, users: us, mail: fm}
}

func (e *testEnv) client() *http.Client {
	jar, _ := cookiejar.New(nil)
	return &http.Client{Jar: jar}
}

func (e *testEnv) do(t *testing.T, c *http.Client, method, path string, body any, out any) int {
	t.Helper()
	var r *bytes.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		r = bytes.NewReader(b)
	} else {
		r = bytes.NewReader(nil)
	}
	req, _ := http.NewRequest(method, e.srv.URL+path, r)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if out != nil {
		_ = json.NewDecoder(resp.Body).Decode(out)
	}
	return resp.StatusCode
}

type m = map[string]any

func TestAccountLifecycle(t *testing.T) {
	e := newTestEnv(t, RegOpen)
	anna := e.client()

	// Sign-up logs in; first-ever account is not auto-admin.
	var me auth.UserInfo
	if code := e.do(t, anna, "POST", "/api/auth/register", m{"email": "Anna@Example.hu", "username": "Anna", "display_name": "Anna", "password": "hosszujelszo"}, &me); code != 201 {
		t.Fatalf("register = %d", code)
	}
	if me.Username != "anna" || me.Email != "anna@example.hu" || me.EmailVerified {
		t.Fatalf("me = %+v", me)
	}
	if code := e.do(t, anna, "POST", "/api/auth/register", m{"email": "anna@example.hu", "username": "anna2", "password": "hosszujelszo"}, nil); code != 409 {
		t.Fatalf("duplicate email = %d", code)
	}
	if code := e.do(t, anna, "POST", "/api/auth/register", m{"email": "x@example.hu", "username": "x", "password": "hosszujelszo"}, nil); code != 400 {
		t.Fatalf("short username = %d", code)
	}

	// Verify email via the mailed link.
	if code := e.do(t, anna, "POST", "/api/auth/verify-email", m{"token": e.mail.lastToken(t, "verify")}, nil); code != 204 {
		t.Fatalf("verify = %d", code)
	}
	e.do(t, anna, "GET", "/api/me", nil, &me)
	if !me.EmailVerified {
		t.Fatal("email not verified")
	}

	// Login by email on a second device, then change password on the first:
	// the other session dies.
	other := e.client()
	if code := e.do(t, other, "POST", "/api/auth/login", m{"login": "ANNA@example.hu", "password": "hosszujelszo"}, nil); code != 200 {
		t.Fatalf("login by email = %d", code)
	}
	var sess []sessionResponse
	e.do(t, anna, "GET", "/api/me/sessions", nil, &sess)
	if len(sess) != 2 {
		t.Fatalf("sessions = %d", len(sess))
	}
	if code := e.do(t, anna, "PUT", "/api/me/password", m{"current_password": "rossz", "new_password": "ujjelszo123"}, nil); code != 403 {
		t.Fatalf("wrong current = %d", code)
	}
	if code := e.do(t, anna, "PUT", "/api/me/password", m{"current_password": "hosszujelszo", "new_password": "ujjelszo123"}, nil); code != 204 {
		t.Fatalf("change password = %d", code)
	}
	if code := e.do(t, other, "GET", "/api/me", nil, nil); code != 401 {
		t.Fatalf("other session after pw change = %d", code)
	}
	if code := e.do(t, anna, "GET", "/api/me", nil, nil); code != 200 {
		t.Fatalf("own session after pw change = %d", code)
	}

	// Forgot → reset → all sessions gone, new password works.
	if code := e.do(t, e.client(), "POST", "/api/auth/forgot", m{"email": "nobody@example.hu"}, nil); code != 202 {
		t.Fatalf("forgot unknown = %d", code)
	}
	if code := e.do(t, e.client(), "POST", "/api/auth/forgot", m{"email": "anna@example.hu"}, nil); code != 202 {
		t.Fatalf("forgot = %d", code)
	}
	tok := e.mail.lastToken(t, "reset")
	if code := e.do(t, e.client(), "POST", "/api/auth/reset", m{"token": tok, "password": "harmadik123"}, nil); code != 204 {
		t.Fatalf("reset = %d", code)
	}
	if code := e.do(t, e.client(), "POST", "/api/auth/reset", m{"token": tok, "password": "negyedik123"}, nil); code != 400 {
		t.Fatalf("reset reuse = %d", code)
	}
	if code := e.do(t, anna, "GET", "/api/me", nil, nil); code != 401 {
		t.Fatalf("session after reset = %d", code)
	}
	if code := e.do(t, anna, "POST", "/api/auth/login", m{"login": "anna", "password": "harmadik123"}, nil); code != 200 {
		t.Fatalf("login new pw = %d", code)
	}
	// Non-admin can't reach admin endpoints.
	if code := e.do(t, anna, "GET", "/api/admin/users", nil, nil); code != 403 {
		t.Fatalf("non-admin admin list = %d", code)
	}
}

func TestApprovalInviteAndAdmin(t *testing.T) {
	e := newTestEnv(t, RegApproval)
	ctx := context.Background()
	root := e.client()
	e.do(t, root, "POST", "/api/auth/register", m{"email": "root@example.hu", "username": "root", "password": "hosszujelszo"}, nil)
	u, _ := e.users.GetByUsername(ctx, "root")
	// Approval mode: pending, can't log in yet. Promote/activate directly.
	if u.Status != users.StatusPending {
		t.Fatalf("status = %s", u.Status)
	}
	if code := e.do(t, root, "POST", "/api/auth/login", m{"login": "root", "password": "hosszujelszo"}, nil); code != 403 {
		t.Fatalf("pending login = %d", code)
	}
	if code := e.do(t, root, "POST", "/api/auth/login", m{"login": "root", "password": "wrongwrong"}, nil); code != 401 {
		t.Fatalf("pending wrong pw = %d", code)
	}
	_ = e.users.SetStatus(ctx, u.ID, users.StatusActive)
	_ = e.users.SetAdmin(ctx, u.ID, true)
	e.do(t, root, "POST", "/api/auth/login", m{"login": "root", "password": "hosszujelszo"}, nil)

	// Last-admin guard.
	if code := e.do(t, root, "PUT", "/api/admin/users/"+u.ID, m{"is_admin": false}, nil); code != 409 {
		t.Fatalf("demote last admin = %d", code)
	}

	// Invite skips approval.
	var inv inviteResponse
	if code := e.do(t, root, "POST", "/api/admin/invites", m{"email": "bob@example.hu"}, &inv); code != 201 {
		t.Fatalf("create invite = %d", code)
	}
	token, _ := url.QueryUnescape(strings.TrimPrefix(inv.Path, "/?invite="))
	bob := e.client()
	var chk map[string]string
	if code := e.do(t, bob, "POST", "/api/auth/invite/check", m{"token": token}, &chk); code != 200 || chk["email"] != "bob@example.hu" {
		t.Fatalf("invite check = %d %v", code, chk)
	}
	if code := e.do(t, bob, "POST", "/api/auth/register", m{"email": "bob@example.hu", "username": "bob", "password": "hosszujelszo", "invite": token}, nil); code != 201 {
		t.Fatalf("invite register = %d", code)
	}
	b, _ := e.users.GetByUsername(ctx, "bob")

	// Disable bob → session dies, login refused like a wrong password.
	if code := e.do(t, root, "PUT", "/api/admin/users/"+b.ID, m{"status": "disabled"}, nil); code != 200 {
		t.Fatalf("disable = %d", code)
	}
	if code := e.do(t, bob, "GET", "/api/me", nil, nil); code != 401 {
		t.Fatalf("disabled session = %d", code)
	}
	if code := e.do(t, bob, "POST", "/api/auth/login", m{"login": "bob", "password": "hosszujelszo"}, nil); code != 401 {
		t.Fatalf("disabled login = %d", code)
	}

	// Admin reset link works without email.
	var link map[string]string
	e.do(t, root, "PUT", "/api/admin/users/"+b.ID, m{"status": "active"}, nil)
	e.do(t, root, "POST", "/api/admin/users/"+b.ID+"/reset-link", nil, &link)
	tok, _ := url.QueryUnescape(strings.TrimPrefix(link["path"], "/?reset="))
	if code := e.do(t, bob, "POST", "/api/auth/reset", m{"token": tok, "password": "bobujjelszo"}, nil); code != 204 {
		t.Fatalf("admin reset = %d", code)
	}

	// Delete: only disabled accounts, never yourself, admins only.
	if code := e.do(t, root, "DELETE", "/api/admin/users/"+b.ID, nil, nil); code != 409 {
		t.Fatalf("delete active account = %d, want 409", code)
	}
	if code := e.do(t, root, "DELETE", "/api/admin/users/"+u.ID, nil, nil); code != 400 {
		t.Fatalf("delete self = %d, want 400", code)
	}
	e.do(t, root, "PUT", "/api/admin/users/"+b.ID, m{"status": "disabled"}, nil)
	if code := e.do(t, bob, "DELETE", "/api/admin/users/"+b.ID, nil, nil); code != 401 && code != 403 {
		t.Fatalf("non-admin delete = %d", code)
	}
	if code := e.do(t, root, "DELETE", "/api/admin/users/"+b.ID, nil, nil); code != 204 {
		t.Fatalf("delete disabled = %d, want 204", code)
	}
	if _, err := e.users.GetByID(ctx, b.ID); err == nil {
		t.Fatal("bob still exists")
	}
	if code := e.do(t, root, "DELETE", "/api/admin/users/"+b.ID, nil, nil); code != 404 {
		t.Fatalf("delete again = %d, want 404", code)
	}
}

func TestClosedRegistration(t *testing.T) {
	e := newTestEnv(t, RegClosed)
	if code := e.do(t, e.client(), "POST", "/api/auth/register", m{"email": "a@example.hu", "username": "anna", "password": "hosszujelszo"}, nil); code != 403 {
		t.Fatalf("closed register = %d", code)
	}
}

func TestPreferences(t *testing.T) {
	e := newTestEnv(t, RegOpen)
	alice, bob := e.client(), e.client()
	e.do(t, alice, "POST", "/api/auth/register", m{"email": "alice@example.hu", "username": "alice", "password": "hosszujelszo"}, nil)
	e.do(t, bob, "POST", "/api/auth/register", m{"email": "bob@example.hu", "username": "bob", "password": "hosszujelszo"}, nil)

	// Signed out: no access.
	if code := e.do(t, e.client(), "GET", "/api/me/preferences", nil, nil); code != 401 {
		t.Fatalf("anonymous = %d", code)
	}

	var got map[string]any
	if code := e.do(t, alice, "GET", "/api/me/preferences", nil, &got); code != 200 || len(got) != 0 {
		t.Fatalf("empty prefs = %d %v", code, got)
	}

	// Keys merge; null removes one; other users are unaffected.
	e.do(t, alice, "PUT", "/api/me/preferences", m{"appearance": m{"theme": "github-dark"}, "x": 1}, nil)
	e.do(t, alice, "PUT", "/api/me/preferences", m{"x": nil, "y": "keep"}, &got)
	app, _ := got["appearance"].(map[string]any)
	if app["theme"] != "github-dark" || got["y"] != "keep" || got["x"] != nil {
		t.Fatalf("merged = %v", got)
	}
	var bobs map[string]any
	e.do(t, bob, "GET", "/api/me/preferences", nil, &bobs)
	if len(bobs) != 0 {
		t.Fatalf("bob sees %v", bobs)
	}

	// Not an object: rejected.
	if code := e.do(t, alice, "PUT", "/api/me/preferences", []int{1}, nil); code != 400 {
		t.Fatalf("array body = %d", code)
	}
}

func TestEmailChangeNeedsPassword(t *testing.T) {
	e := newTestEnv(t, RegOpen)
	c := e.client()
	e.do(t, c, "POST", "/api/auth/register", m{"email": "anna@example.hu", "username": "anna", "password": "hosszujelszo"}, nil)

	if code := e.do(t, c, "PUT", "/api/me", m{"email": "thief@example.hu"}, nil); code != 403 {
		t.Fatalf("email change without password = %d, want 403", code)
	}
	if code := e.do(t, c, "PUT", "/api/me", m{"email": "thief@example.hu", "current_password": "rossz-jelszo"}, nil); code != 403 {
		t.Fatalf("email change with wrong password = %d, want 403", code)
	}
	if code := e.do(t, c, "PUT", "/api/me", m{"display_name": "Anna B"}, nil); code != 200 {
		t.Fatalf("name change without password = %d, want 200", code)
	}
	var me struct {
		Email string `json:"email"`
	}
	if code := e.do(t, c, "PUT", "/api/me", m{"email": "anna2@example.hu", "current_password": "hosszujelszo"}, &me); code != 200 || me.Email != "anna2@example.hu" {
		t.Fatalf("email change with password = %d %q", code, me.Email)
	}
}
