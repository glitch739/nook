// Package httpx holds small HTTP helpers shared across handler/middleware
// packages (internal/auth, internal/ratelimit, internal/httpapi) that
// don't belong to any one of them specifically.
package httpx

import (
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"strings"
)

// trustedProxies is set once at startup by SetTrustedProxies.
var trustedProxies []*net.IPNet

// SetTrustedProxies parses a comma-separated list of IPs/CIDRs (e.g.
// "127.0.0.1,10.0.0.0/8") of reverse proxies allowed to set
// X-Forwarded-For. Call once at startup, before serving.
func SetTrustedProxies(list string) error {
	trustedProxies = nil
	for _, part := range strings.Split(list, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if !strings.Contains(part, "/") {
			if strings.Contains(part, ":") {
				part += "/128"
			} else {
				part += "/32"
			}
		}
		_, n, err := net.ParseCIDR(part)
		if err != nil {
			return err
		}
		trustedProxies = append(trustedProxies, n)
	}
	return nil
}

func isTrusted(ip net.IP) bool {
	for _, n := range trustedProxies {
		if ip != nil && n.Contains(ip) {
			return true
		}
	}
	return false
}

// ClientIP returns the request's client IP.
//
// X-Forwarded-For is only believed when the direct peer is a configured
// trusted proxy (NOOK_TRUSTED_PROXIES); then the rightmost address that
// isn't itself a trusted proxy is the client. Trusting the header
// unconditionally would let any client spoof a fresh "IP" per request
// and bypass rate limiting and the login audit trail.
func ClientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	if !isTrusted(net.ParseIP(host)) {
		return host
	}
	hops := strings.Split(r.Header.Get("X-Forwarded-For"), ",")
	for i := len(hops) - 1; i >= 0; i-- {
		h := strings.TrimSpace(hops[i])
		ip := net.ParseIP(h)
		if ip == nil {
			break
		}
		if !isTrusted(ip) {
			return h
		}
	}
	return host
}

// DecodeJSONBody decodes r.Body as JSON into v. On failure it writes an
// appropriate error response itself — 413 if the body was cut off by a
// http.MaxBytesReader limit somewhere upstream (see httpapi.maxBody),
// 400 for any other decode failure (malformed JSON, wrong shape) — and
// returns false; callers should return immediately without writing
// anything else. On success it returns true having written nothing.
func DecodeJSONBody(w http.ResponseWriter, r *http.Request, v any) bool {
	err := json.NewDecoder(r.Body).Decode(v)
	if err == nil {
		return true
	}

	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		http.Error(w, "request body too large", http.StatusRequestEntityTooLarge)
		return false
	}

	http.Error(w, "invalid request body", http.StatusBadRequest)
	return false
}

// WriteJSON writes v as a JSON response with the given status.
func WriteJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
