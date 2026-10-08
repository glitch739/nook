package webui

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"
)

func TestPrecompressed(t *testing.T) {
	root := fstest.MapFS{
		"index.html":         {Data: []byte("<html>plain</html>")},
		"index.html.gz":      {Data: []byte("GZ-INDEX")},
		"assets/app.js":      {Data: []byte("console.log('plain')")},
		"assets/app.js.br":   {Data: []byte("BR-JS")},
		"assets/app.js.gz":   {Data: []byte("GZ-JS")},
		"assets/style.css":   {Data: []byte("body{}")},
		"assets/icon.svg":    {Data: []byte("<svg/>")},
		"assets/icon.svg.gz": {Data: []byte("GZ-SVG")},
	}
	h := newHandler(root)

	cases := []struct {
		name, path, accept, wantBody, wantEnc, wantType string
	}{
		{"brotli preferred", "/assets/app.js", "gzip, deflate, br", "BR-JS", "br", "text/javascript; charset=utf-8"},
		{"gzip when no br", "/assets/app.js", "gzip", "GZ-JS", "gzip", "text/javascript; charset=utf-8"},
		{"br refused with q=0", "/assets/app.js", "br;q=0, gzip", "GZ-JS", "gzip", "text/javascript; charset=utf-8"},
		{"no compression asked", "/assets/app.js", "", "console.log('plain')", "", "text/javascript; charset=utf-8"},
		{"no variant: plain", "/assets/style.css", "br, gzip", "body{}", "", "text/css; charset=utf-8"},
		{"only gz exists", "/assets/icon.svg", "br, gzip", "GZ-SVG", "gzip", "image/svg+xml"},
		{"index via /", "/", "gzip", "GZ-INDEX", "gzip", "text/html; charset=utf-8"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, c.path, nil)
			if c.accept != "" {
				req.Header.Set("Accept-Encoding", c.accept)
			}
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			if rec.Code != 200 {
				t.Fatalf("status %d", rec.Code)
			}
			if got := rec.Body.String(); got != c.wantBody {
				t.Errorf("body %q, want %q", got, c.wantBody)
			}
			if got := rec.Header().Get("Content-Encoding"); got != c.wantEnc {
				t.Errorf("Content-Encoding %q, want %q", got, c.wantEnc)
			}
			if got := rec.Header().Get("Content-Type"); got != c.wantType {
				t.Errorf("Content-Type %q, want %q", got, c.wantType)
			}
			if c.wantEnc != "" && rec.Header().Get("Vary") != "Accept-Encoding" {
				t.Error("missing Vary: Accept-Encoding")
			}
		})
	}

	// HEAD: headers, no body.
	req := httptest.NewRequest(http.MethodHead, "/assets/app.js", nil)
	req.Header.Set("Accept-Encoding", "br")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Header().Get("Content-Encoding") != "br" || rec.Body.Len() != 0 {
		t.Errorf("HEAD: enc %q, body %d bytes", rec.Header().Get("Content-Encoding"), rec.Body.Len())
	}
	// Asset cache header unchanged.
	if rec.Header().Get("Cache-Control") != "public, max-age=31536000, immutable" {
		t.Errorf("Cache-Control %q", rec.Header().Get("Cache-Control"))
	}
}
