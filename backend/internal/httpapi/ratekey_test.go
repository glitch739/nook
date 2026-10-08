package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"golang.org/x/time/rate"

	"nook/internal/auth"
	"nook/internal/ratelimit"
)

// Two sessions behind one IP each get the full per-session budget; the
// per-IP ceiling still caps the total, also against made-up cookies.
func TestSessionBudgetsAndIPCeiling(t *testing.T) {
	general := ratelimit.New(rate.Limit(0.0001), 3, time.Minute) // 3 per session, ~no refill
	general.Key = SessionOrIPKey
	ceiling := ratelimit.New(rate.Limit(0.0001), 8, time.Minute) // 8 per IP
	ok := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})
	h := ceiling.Middleware(general.Middleware(ok))

	hit := func(cookie string) int {
		req := httptest.NewRequest(http.MethodGet, "/api/spaces", nil)
		req.RemoteAddr = "203.0.113.5:1234"
		if cookie != "" {
			req.AddCookie(&http.Cookie{Name: auth.SessionCookieName, Value: cookie})
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec.Code
	}

	for i := 0; i < 3; i++ {
		if hit("alice") != 200 || hit("reka") != 200 {
			t.Fatalf("request %d: a session's own budget should allow 3", i+1)
		}
	}
	if hit("alice") != 429 {
		t.Fatal("alice's 4th request should be limited")
	}
	// 7 of the IP's 8 used; fake cookies get one more, then the ceiling.
	if hit("fake-1") != 200 {
		t.Fatal("8th request from the IP should pass")
	}
	if hit("fake-2") != 429 {
		t.Fatal("the per-IP ceiling must stop rotated cookies")
	}
}
