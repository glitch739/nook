package httpapi

import (
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestGzipJSON(t *testing.T) {
	big := `{"x":"` + strings.Repeat("a", 4000) + `"}`
	small := `{"ok":true}`
	h := gzipJSON(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/big":
			w.Header().Set("Content-Type", "application/json")
			// Several writes, like json.Encoder can do.
			_, _ = io.WriteString(w, big[:100])
			_, _ = io.WriteString(w, big[100:])
		case "/small":
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, small)
		case "/text":
			http.Error(w, strings.Repeat("e", 3000), http.StatusBadRequest)
		case "/created":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			_, _ = io.WriteString(w, big)
		case "/empty":
			w.WriteHeader(http.StatusNoContent)
		}
	}))

	get := func(path, accept string) *http.Response {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		if accept != "" {
			req.Header.Set("Accept-Encoding", accept)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec.Result()
	}
	body := func(res *http.Response) string {
		var r io.Reader = res.Body
		if res.Header.Get("Content-Encoding") == "gzip" {
			zr, err := gzip.NewReader(res.Body)
			if err != nil {
				t.Fatal(err)
			}
			r = zr
		}
		b, _ := io.ReadAll(r)
		return string(b)
	}

	if res := get("/big", "gzip, br"); res.Header.Get("Content-Encoding") != "gzip" || body(res) != big {
		t.Fatalf("big: enc %q", res.Header.Get("Content-Encoding"))
	}
	if res := get("/big", ""); res.Header.Get("Content-Encoding") != "" || body(res) != big {
		t.Fatal("big without Accept-Encoding must be plain")
	}
	if res := get("/big", "gzip;q=0"); res.Header.Get("Content-Encoding") != "" {
		t.Fatal("gzip;q=0 must be plain")
	}
	if res := get("/small", "gzip"); res.Header.Get("Content-Encoding") != "" || body(res) != small {
		t.Fatal("small must be plain")
	}
	if res := get("/text", "gzip"); res.Header.Get("Content-Encoding") != "" || res.StatusCode != 400 {
		t.Fatal("error text must be plain")
	}
	if res := get("/created", "gzip"); res.StatusCode != 201 || res.Header.Get("Content-Encoding") != "gzip" || body(res) != big {
		t.Fatalf("created: %d %q", res.StatusCode, res.Header.Get("Content-Encoding"))
	}
	if res := get("/empty", "gzip"); res.StatusCode != 204 || res.Header.Get("Content-Encoding") != "" {
		t.Fatal("204 must stay empty and plain")
	}
}
