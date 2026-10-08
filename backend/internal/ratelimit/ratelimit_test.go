package ratelimit

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"golang.org/x/time/rate"
)

func TestLimiter_AllowsBurstThenDenies(t *testing.T) {
	l := New(rate.Limit(1), 3, time.Minute)

	for i := 0; i < 3; i++ {
		if !l.Allow("a") {
			t.Fatalf("request %d within burst was denied", i+1)
		}
	}
	if l.Allow("a") {
		t.Fatal("request beyond burst was allowed")
	}
}

func TestLimiter_RefillsOverTime(t *testing.T) {
	// One token every 20ms, burst of 1 -- exhaust it, wait past the
	// refill interval, expect exactly one more to succeed.
	l := New(rate.Every(20*time.Millisecond), 1, time.Minute)

	if !l.Allow("a") {
		t.Fatal("first request was denied")
	}
	if l.Allow("a") {
		t.Fatal("second immediate request should have been denied")
	}

	time.Sleep(40 * time.Millisecond)

	if !l.Allow("a") {
		t.Fatal("request after refill interval was denied")
	}
}

func TestLimiter_KeysAreIndependent(t *testing.T) {
	l := New(rate.Limit(1), 1, time.Minute)

	if !l.Allow("a") {
		t.Fatal("first request for key a was denied")
	}
	if l.Allow("a") {
		t.Fatal("second immediate request for key a should have been denied")
	}
	if !l.Allow("b") {
		t.Fatal("key b should have its own independent bucket, unaffected by key a")
	}
}

func TestLimiter_EvictsStaleEntries(t *testing.T) {
	l := New(rate.Limit(100), 5, 20*time.Millisecond)

	l.Allow("stale")
	time.Sleep(40 * time.Millisecond)
	l.Allow("fresh") // should trigger cleanup of "stale" while adding "fresh"

	l.mu.Lock()
	_, staleStillPresent := l.buckets["stale"]
	_, freshPresent := l.buckets["fresh"]
	count := len(l.buckets)
	l.mu.Unlock()

	if staleStillPresent {
		t.Error("stale entry should have been evicted")
	}
	if !freshPresent {
		t.Error("fresh entry should be present")
	}
	if count != 1 {
		t.Errorf("expected exactly 1 entry after eviction, got %d", count)
	}
}

func TestMiddleware_AllowsThenRejectsWith429(t *testing.T) {
	l := New(rate.Limit(1), 1, time.Minute)

	called := 0
	handler := l.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called++
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "203.0.113.5:12345"

	rec1 := httptest.NewRecorder()
	handler.ServeHTTP(rec1, req)
	if rec1.Code != http.StatusOK {
		t.Fatalf("first request: status = %d, want 200", rec1.Code)
	}

	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, req)
	if rec2.Code != http.StatusTooManyRequests {
		t.Fatalf("second request: status = %d, want 429", rec2.Code)
	}
	if rec2.Header().Get("Retry-After") == "" {
		t.Error("429 response is missing a Retry-After header")
	}

	if called != 1 {
		t.Fatalf("wrapped handler called %d times, want exactly 1 (the second request should have been rejected before reaching it)", called)
	}
}

func TestMiddleware_DifferentIPsAreIndependent(t *testing.T) {
	l := New(rate.Limit(1), 1, time.Minute)
	handler := l.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	reqA := httptest.NewRequest(http.MethodGet, "/", nil)
	reqA.RemoteAddr = "203.0.113.5:1"
	reqB := httptest.NewRequest(http.MethodGet, "/", nil)
	reqB.RemoteAddr = "203.0.113.9:1"

	recA := httptest.NewRecorder()
	handler.ServeHTTP(recA, reqA)
	if recA.Code != http.StatusOK {
		t.Fatalf("IP A first request: status = %d, want 200", recA.Code)
	}

	recB := httptest.NewRecorder()
	handler.ServeHTTP(recB, reqB)
	if recB.Code != http.StatusOK {
		t.Fatalf("IP B first request: status = %d, want 200 (independent bucket from IP A)", recB.Code)
	}
}
