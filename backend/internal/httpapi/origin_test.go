package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSameOriginWrites(t *testing.T) {
	ok := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) })
	h := sameOriginWrites("https://nook.example", ok)

	cases := []struct {
		name    string
		method  string
		headers map[string]string
		want    int
	}{
		{"GET from anywhere", http.MethodGet, map[string]string{"Sec-Fetch-Site": "cross-site"}, 204},
		{"same-origin POST", http.MethodPost, map[string]string{"Sec-Fetch-Site": "same-origin"}, 204},
		{"cross-site POST", http.MethodPost, map[string]string{"Sec-Fetch-Site": "cross-site"}, 403},
		{"same-site (sibling subdomain) PUT", http.MethodPut, map[string]string{"Sec-Fetch-Site": "same-site"}, 403},
		{"no browser headers", http.MethodDelete, nil, 204},
		{"Origin matches Host", http.MethodPost, map[string]string{"Origin": "http://localhost:8080"}, 204},
		{"Origin matches public URL", http.MethodPost, map[string]string{"Origin": "https://nook.example"}, 204},
		{"foreign Origin", http.MethodPost, map[string]string{"Origin": "https://evil.example"}, 403},
		{"null Origin", http.MethodPost, map[string]string{"Origin": "null"}, 403},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			req := httptest.NewRequest(c.method, "http://localhost:8080/api/spaces", nil)
			for k, v := range c.headers {
				req.Header.Set(k, v)
			}
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			if rec.Code != c.want {
				t.Fatalf("status %d, want %d", rec.Code, c.want)
			}
		})
	}
}
