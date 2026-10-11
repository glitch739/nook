package httpx

import (
	"net/http/httptest"
	"testing"
)

// Behind the tunnel every request comes from the proxy, so rate limits
// and lockouts depend on reading X-Forwarded-For right.
func TestClientIP(t *testing.T) {
	if err := SetTrustedProxies("172.31.80.10"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { SetTrustedProxies("") })

	for _, c := range []struct{ name, remote, xff, want string }{
		{"direct client ignores header", "198.51.100.9:4000", "203.0.113.7", "198.51.100.9"},
		{"trusted proxy, real client", "172.31.80.10:4000", "203.0.113.7", "203.0.113.7"},
		{"spoofed hop on the left is ignored", "172.31.80.10:4000", "6.6.6.6, 203.0.113.7", "203.0.113.7"},
		{"trusted proxy, no header", "172.31.80.10:4000", "", "172.31.80.10"},
		{"garbage last hop falls back to proxy", "172.31.80.10:4000", "203.0.113.7, evil", "172.31.80.10"},
		{"IPv6 client", "172.31.80.10:4000", "2001:db8::1", "2001:db8::1"},
	} {
		r := httptest.NewRequest("GET", "/", nil)
		r.RemoteAddr = c.remote
		if c.xff != "" {
			r.Header.Set("X-Forwarded-For", c.xff)
		}
		if got := ClientIP(r); got != c.want {
			t.Errorf("%s: ClientIP = %q, want %q", c.name, got, c.want)
		}
	}
}
