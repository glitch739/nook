package httpapi

import (
	"net/http"
	"net/url"
	"strings"
)

// sameOriginWrites rejects state-changing requests (anything but GET,
// HEAD, OPTIONS) that a browser says came from another site.
//
// Defense in depth on top of ADR 0003 (SameSite=Lax cookie + JSON-only
// bodies). That ADR explicitly doesn't cover multipart uploads (cover
// images, images in page text), and SameSite treats every subdomain of
// the same registrable domain as "same site". Checking the browser's
// own provenance headers closes both gaps:
//
//   - Sec-Fetch-Site (all current browsers): only "same-origin" and
//     "none" (typed URL / bookmark, never a write in practice) pass.
//   - Origin, when Sec-Fetch-Site is absent (older engines): its host
//     must be this request's Host or NOOK_PUBLIC_URL's host.
//
// A request with neither header isn't from a browser at all (curl,
// scripts); it can't carry a victim's cookies by accident, so it isn't
// a CSRF vector and passes through to normal authentication.
func sameOriginWrites(publicURL string, next http.Handler) http.Handler {
	publicHost := ""
	if u, err := url.Parse(publicURL); err == nil {
		publicHost = strings.ToLower(u.Host)
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet, http.MethodHead, http.MethodOptions:
			next.ServeHTTP(w, r)
			return
		}
		if site := r.Header.Get("Sec-Fetch-Site"); site != "" {
			if site != "same-origin" && site != "none" {
				http.Error(w, "cross-site request rejected", http.StatusForbidden)
				return
			}
		} else if origin := r.Header.Get("Origin"); origin != "" {
			u, err := url.Parse(origin)
			host := ""
			if err == nil {
				host = strings.ToLower(u.Host)
			}
			if host == "" || (host != strings.ToLower(r.Host) && host != publicHost) {
				http.Error(w, "cross-origin request rejected", http.StatusForbidden)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}
