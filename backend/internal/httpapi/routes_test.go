package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestExemptStaticAssets(t *testing.T) {
	tests := []struct {
		method, path string
		wantLimited  bool
	}{
		{http.MethodGet, "/assets/database-backup-CIzZHK-F.svg", false},
		{http.MethodHead, "/assets/index-abc123.js", false},
		{http.MethodPost, "/assets/x.svg", true},
		{http.MethodGet, "/api/groups", true},
		{http.MethodGet, "/", true},
		{http.MethodGet, "/assetsfoo", true},
		{http.MethodGet, "/api/pages/p1/images/i1", false},
		{http.MethodPost, "/api/pages/p1/images", true},
		{http.MethodPost, "/api/pages/p1/images/i1", true},
		{http.MethodGet, "/api/pages/p1/images/i1/x", true},
		{http.MethodGet, "/api/pages/p1/cover", true},
	}
	for _, tt := range tests {
		var limitedHit, unlimitedHit bool
		h := exemptStaticAssets(
			http.HandlerFunc(func(http.ResponseWriter, *http.Request) { limitedHit = true }),
			http.HandlerFunc(func(http.ResponseWriter, *http.Request) { unlimitedHit = true }),
		)
		h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(tt.method, tt.path, nil))
		if limitedHit != tt.wantLimited || unlimitedHit == tt.wantLimited {
			t.Errorf("%s %s: limited=%v unlimited=%v, want limited=%v", tt.method, tt.path, limitedHit, unlimitedHit, tt.wantLimited)
		}
	}
}
