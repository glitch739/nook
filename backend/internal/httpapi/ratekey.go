package httpapi

import (
	"net/http"

	"nook/internal/auth"
	"nook/internal/httpx"
)

// SessionOrIPKey is the general rate limiter's bucket key: the session
// for a request that carries a session cookie, the client IP otherwise.
// Two people at home share one public IP; with this each signed-in
// person has their own budget.
//
// The cookie isn't validated here (that would cost a DB lookup per
// request), so made-up cookies get buckets too. That is why routes.go
// keeps a wider per-IP ceiling (Deps.IPCeiling) in front of this one:
// rotating fake cookies can't get past it. The cookie is stored only
// as its hash, the same one the sessions table uses.
func SessionOrIPKey(r *http.Request) string {
	if c, err := r.Cookie(auth.SessionCookieName); err == nil && c.Value != "" {
		return "s:" + auth.HashToken(c.Value)
	}
	return "ip:" + httpx.ClientIP(r)
}
