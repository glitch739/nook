package httpapi

import "net/http"

// securityHeaders sets response headers that cost nothing and close off
// a handful of well-known browser-side attack classes. Applied to every
// response, including error responses.
//
//   - Content-Security-Policy: same-origin only for scripts/styles/
//     connections; 'unsafe-inline' on style-src because React sets
//     inline style="" attributes sometimes, but NOT on script-src — that
//     one staying strict is what actually blocks injected-script XSS.
//     frame-ancestors 'none' stops Nook being framed by another site
//     (clickjacking); X-Frame-Options: DENY duplicates that for older
//     browsers that don't honor frame-ancestors.
//   - X-Content-Type-Options: nosniff stops the browser from guessing a
//     response's content type against what Content-Type actually says
//     (relevant if an attacker could ever get Nook to reflect
//     user-controlled content).
//   - Referrer-Policy: strict-origin-when-cross-origin keeps the full
//     URL (which could carry a session-adjacent path) from leaking to
//     third-party sites via the Referer header on outbound links.
//   - Strict-Transport-Security, only when hsts is set (NOOK_PUBLIC_URL
//     is https): tells the browser to never again talk plain HTTP to
//     this host, so a network attacker can't downgrade the first
//     request of a later visit. Never sent for a plain-HTTP LAN/dev
//     setup, where it would lock the browser out of that host.
func securityHeaders(next http.Handler, hsts bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Security-Policy",
			"default-src 'self'; "+
				"script-src 'self'; "+
				"style-src 'self' 'unsafe-inline'; "+
				"img-src 'self' data:; "+
				// ws(s)://host spelled out for the collab WebSocket:
				// CSP3 'self' covers it, older engines don't.
				"connect-src 'self' ws://"+r.Host+" wss://"+r.Host+"; "+
				"frame-ancestors 'none'; "+
				"base-uri 'self'; "+
				"form-action 'self'")
		h.Set("X-Frame-Options", "DENY")
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "strict-origin-when-cross-origin")
		h.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=(), payment=(), usb=()")
		h.Set("Cross-Origin-Opener-Policy", "same-origin")
		if hsts {
			h.Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
		}

		next.ServeHTTP(w, r)
	})
}
