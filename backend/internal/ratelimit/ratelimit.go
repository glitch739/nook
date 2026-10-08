// Package ratelimit provides per-key (typically per-client-IP) request
// rate limiting, built on golang.org/x/time/rate's token bucket. It's
// in-memory and per-process by design — Nook only ever runs as a single
// process (see docs/ROADMAP.md, "Security posture": no plan to ever run
// multiple instances behind a load balancer), so there's no need for a
// shared external store like Redis for this.
package ratelimit

import (
	"log/slog"
	"math"
	"net/http"
	"strconv"
	"sync"
	"time"

	"golang.org/x/time/rate"

	"nook/internal/httpx"
)

// Limiter tracks one token-bucket limiter per key. Safe for concurrent use.
type Limiter struct {
	rate  rate.Limit
	burst int
	ttl   time.Duration

	// Logger records rejected requests (event, ip, path) at Warn level.
	// Defaults to slog.Default() if left nil.
	Logger *slog.Logger

	// Key picks the bucket a request counts against. Nil means the
	// client IP (httpx.ClientIP), the original behavior.
	Key func(r *http.Request) string

	mu      sync.Mutex
	buckets map[string]*entry
}

type entry struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

// New creates a Limiter allowing `burst` requests immediately per key,
// refilling at `r` tokens/sec thereafter. A key not seen for `ttl` is
// eligible for eviction (see Allow) so memory doesn't grow unboundedly
// with the number of distinct IPs seen over the process's lifetime —
// this matters once Nook is publicly reachable, not just for local dev.
func New(r rate.Limit, burst int, ttl time.Duration) *Limiter {
	return &Limiter{
		rate:    r,
		burst:   burst,
		ttl:     ttl,
		buckets: make(map[string]*entry),
	}
}

// Allow reports whether a request for key is allowed right now, and
// consumes one token from key's bucket if so.
//
// Requirements:
//   - Lock l.mu for the duration (buckets is shared, concurrent HTTP
//     requests will call this).
//   - Look up key in l.buckets; if absent, create a new entry with
//     rate.NewLimiter(l.rate, l.burst).
//   - Update that entry's lastSeen to time.Now().
//   - While the lock is held anyway, opportunistically evict every
//     *other* entry whose lastSeen is older than l.ttl. A background
//     sweep goroutine would be unnecessary complexity here — piggy-
//     backing cleanup on normal calls keeps the map bounded without a
//     second goroutine lifecycle to manage. (This does mean an
//     abandoned bucket only actually gets evicted the next time *some*
//     key is checked, not exactly at TTL expiry — fine for this
//     purpose.)
//   - Return the entry's limiter.Allow() for key.
func (l *Limiter) Allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := time.Now()

	e, ok := l.buckets[key]
	if !ok {
		e = &entry{limiter: rate.NewLimiter(l.rate, l.burst)}
		l.buckets[key] = e
	}
	e.lastSeen = now

	for k, other := range l.buckets {
		if k != key && now.Sub(other.lastSeen) > l.ttl {
			delete(l.buckets, k)
		}
	}

	return e.limiter.Allow()
}

// Middleware rejects requests over the limit with 429 and a Retry-After
// header, keyed by the client's IP (httpx.ClientIP). Requests that don't
// exceed the limit are forwarded to next unchanged.
//
// Requirements:
//   - key := httpx.ClientIP(r)
//   - If !l.Allow(key): set a Retry-After header (doesn't need to be
//     exact — something derived from 1/l.rate seconds, rounded up to at
//     least 1, is fine), respond http.StatusTooManyRequests, and do NOT
//     call next.
//   - Otherwise, next.ServeHTTP(w, r) as normal.
func (l *Limiter) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip := httpx.ClientIP(r)
		key := ip
		if l.Key != nil {
			key = l.Key(r)
		}
		if !l.Allow(key) {
			retryAfter := int(math.Ceil(1 / float64(l.rate)))
			if retryAfter < 1 {
				retryAfter = 1
			}
			l.logger().Warn("rate limited",
				"event", "rate_limited",
				"ip", ip,
				"path", r.URL.Path,
			)
			w.Header().Set("Retry-After", strconv.Itoa(retryAfter))
			http.Error(w, "too many requests", http.StatusTooManyRequests)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (l *Limiter) logger() *slog.Logger {
	if l.Logger != nil {
		return l.Logger
	}
	return slog.Default()
}
