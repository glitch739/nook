// Package webui embeds and serves the built frontend (frontend/dist),
// copied into dist/ here at build time — see the repo root Makefile's
// `embed` target. This is what makes `go build` produce a single
// deployable binary with the whole app inside it, no separate frontend
// server or static-file directory to ship alongside it.
//
// dist/ ships with a placeholder index.html on a fresh checkout (real
// frontend build output isn't committed) — go:embed requires at least
// one matching file to exist at compile time, and a placeholder page
// ("run make build") is a far friendlier failure mode than a compile
// error when someone runs `go build` before ever building the frontend.
// `make embed` overwrites it with the real build.
package webui

import (
	"embed"
	"io"
	"io/fs"
	"mime"
	"net/http"
	"path"
	"strconv"
	"strings"
)

//go:embed dist
var distFS embed.FS

// Handler serves the embedded frontend build as static files rooted at
// "/". Returns an error only if the embedded dist/ directory is
// missing entirely, which shouldn't happen (it's always present as a
// package-relative subdirectory, even if empty).
//
// Explicit Cache-Control throughout -- embed.FS reports a zero mtime
// for every file, so http.FileServer never emits a usable Last-Modified/
// ETag pair for the browser to revalidate against, and with no
// Cache-Control at all a plain reload (not a hard refresh) can still
// serve a stale copy straight from the browser's disk cache after a
// redeploy. index.html (and anything else outside /assets/, e.g. the
// favicon) has to be revalidated every time -- it's the one file whose
// content changes across deploys without its own URL changing.
// /assets/* files are named with a content hash by the Vite build
// (index-<hash>.js), so a new deploy is always a *new* URL -- safe to
// cache for as long as the browser wants, forever.
// Go's built-in MIME table has no entry for the web app manifest; without
// this it would go out as application/octet-stream.
func init() {
	_ = mime.AddExtensionType(".webmanifest", "application/manifest+json")
}

func Handler() (http.Handler, error) {
	sub, err := fs.Sub(distFS, "dist")
	if err != nil {
		return nil, err
	}
	return newHandler(sub), nil
}

// newHandler is Handler over any fs.FS, so tests can supply their own.
//
// Precompressed variants: the image build (scripts/precompress.sh)
// writes name.br and name.gz next to every text asset worth
// compressing. When the browser accepts one of those encodings and the
// variant exists, it is served as-is with Content-Encoding set -- no
// compression work per request. Anything else (no variant, a browser
// that doesn't ask for compression, a local build without the
// precompress step) falls through to the plain file server exactly as
// before.
func newHandler(root fs.FS) http.Handler {
	fileServer := http.FileServer(http.FS(root))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/assets/") {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		} else {
			w.Header().Set("Cache-Control", "no-cache")
		}
		if (r.Method == http.MethodGet || r.Method == http.MethodHead) && servePrecompressed(w, r, root) {
			return
		}
		fileServer.ServeHTTP(w, r)
	})
}

// encodings in order of preference: brotli is ~15-20% smaller than gzip
// for JS/CSS.
var encodings = []struct{ token, ext string }{{"br", ".br"}, {"gzip", ".gz"}}

func servePrecompressed(w http.ResponseWriter, r *http.Request, root fs.FS) bool {
	name := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
	if name == "" || strings.HasSuffix(r.URL.Path, "/") {
		name = path.Join(name, "index.html")
	}
	if strings.HasSuffix(name, ".br") || strings.HasSuffix(name, ".gz") {
		return false // never re-encode a variant someone asked for by name
	}
	accept := r.Header.Get("Accept-Encoding")
	for _, enc := range encodings {
		if !acceptsEncoding(accept, enc.token) {
			continue
		}
		f, err := root.Open(name + enc.ext)
		if err != nil {
			continue
		}
		info, err := f.Stat()
		rs, ok := f.(io.ReadSeeker)
		if err != nil || info.IsDir() || !ok {
			f.Close()
			continue
		}
		h := w.Header()
		ctype := mime.TypeByExtension(path.Ext(name))
		if ctype == "" {
			ctype = "application/octet-stream"
		}
		h.Set("Content-Type", ctype)
		h.Set("Content-Encoding", enc.token)
		h.Add("Vary", "Accept-Encoding")
		// No Range support on an encoded body: a range would be of the
		// compressed bytes, which browsers never ask for on these files.
		h.Set("Content-Length", strconv.FormatInt(info.Size(), 10))
		w.WriteHeader(http.StatusOK)
		if r.Method == http.MethodGet {
			_, _ = io.Copy(w, rs)
		}
		f.Close()
		return true
	}
	return false
}

// acceptsEncoding reports whether an Accept-Encoding header allows
// token (ignoring entries with q=0).
func acceptsEncoding(header, token string) bool {
	for _, part := range strings.Split(header, ",") {
		fields := strings.Split(strings.TrimSpace(part), ";")
		if !strings.EqualFold(strings.TrimSpace(fields[0]), token) {
			continue
		}
		for _, f := range fields[1:] {
			f = strings.ReplaceAll(strings.TrimSpace(f), " ", "")
			if f == "q=0" || f == "q=0.0" || f == "q=0.00" || f == "q=0.000" {
				return false
			}
		}
		return true
	}
	return false
}
