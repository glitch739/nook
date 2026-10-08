package auth

import (
	"strings"
	"sync"
	"time"
)

// AccountGuard throttles login attempts per *account* (the submitted
// username/email), on top of the per-IP limiter in internal/ratelimit.
//
// The per-IP limiter alone doesn't stop a distributed, low-and-slow
// guess against one account: a botnet sends each guess from a fresh IP
// and never trips a per-IP budget. Keying on the login name does.
//
// Policy: the first FreeFailures wrong passwords for a name cost
// nothing. Every failure after that locks the name for BaseLock,
// doubling per further failure, capped at MaxLock. A successful login
// clears the record. A name with no failures for ForgetAfter is
// forgotten.
//
// Trade-off, chosen knowingly: someone who knows your username can keep
// your account locked for up to MaxLock at a time by failing on
// purpose. The cap keeps that a nuisance, not an outage, and on an
// instance behind an access gateway (docs/DEPLOY.md) strangers can't
// reach the login form at all.
//
// State is in memory, like the IP limiter: Nook is a single process
// (ADR 0001), and a restart forgetting lockouts is acceptable.
type AccountGuard struct {
	FreeFailures int
	BaseLock     time.Duration
	MaxLock      time.Duration
	ForgetAfter  time.Duration
	// MaxEntries bounds memory if someone sprays random usernames.
	MaxEntries int

	mu      sync.Mutex
	entries map[string]*guardEntry
	now     func() time.Time
}

type guardEntry struct {
	failures    int
	lastFailure time.Time
	lockedUntil time.Time
}

// NewAccountGuard returns a guard with Nook's defaults: 5 free
// failures, then 1 min, 2 min, 4 min ... up to 30 min.
func NewAccountGuard() *AccountGuard {
	return &AccountGuard{
		FreeFailures: 5,
		BaseLock:     time.Minute,
		MaxLock:      30 * time.Minute,
		ForgetAfter:  24 * time.Hour,
		MaxEntries:   10000,
		entries:      map[string]*guardEntry{},
		now:          time.Now,
	}
}

func guardKey(login string) string {
	return strings.ToLower(strings.TrimSpace(login))
}

// Locked reports whether login is currently locked, and for how long.
func (g *AccountGuard) Locked(login string) (bool, time.Duration) {
	if g == nil {
		return false, 0
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	e := g.entries[guardKey(login)]
	if e == nil {
		return false, 0
	}
	if wait := e.lockedUntil.Sub(g.now()); wait > 0 {
		return true, wait
	}
	return false, 0
}

// Fail records a wrong password for login and returns the lock it now
// carries (0 while still within the free failures).
func (g *AccountGuard) Fail(login string) time.Duration {
	if g == nil {
		return 0
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	now := g.now()
	key := guardKey(login)
	e := g.entries[key]
	if e != nil && now.Sub(e.lastFailure) > g.ForgetAfter {
		e = nil
	}
	if e == nil {
		if len(g.entries) >= g.MaxEntries {
			g.evictLocked(now)
		}
		e = &guardEntry{}
		g.entries[key] = e
	}
	e.failures++
	e.lastFailure = now
	over := e.failures - g.FreeFailures
	if over <= 0 {
		return 0
	}
	lock := g.BaseLock
	for i := 1; i < over && lock < g.MaxLock; i++ {
		lock *= 2
	}
	if lock > g.MaxLock {
		lock = g.MaxLock
	}
	e.lockedUntil = now.Add(lock)
	return lock
}

// Succeed clears login's failure record.
func (g *AccountGuard) Succeed(login string) {
	if g == nil {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	delete(g.entries, guardKey(login))
}

// evictLocked drops forgotten entries; if that frees nothing, the
// oldest one goes. Caller holds g.mu.
func (g *AccountGuard) evictLocked(now time.Time) {
	var oldestKey string
	var oldest time.Time
	for k, e := range g.entries {
		if now.Sub(e.lastFailure) > g.ForgetAfter {
			delete(g.entries, k)
			continue
		}
		if oldestKey == "" || e.lastFailure.Before(oldest) {
			oldestKey, oldest = k, e.lastFailure
		}
	}
	if len(g.entries) >= g.MaxEntries && oldestKey != "" {
		delete(g.entries, oldestKey)
	}
}
