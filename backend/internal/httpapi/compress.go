package httpapi

import (
	"bytes"
	"compress/gzip"
	"net/http"
	"strings"
	"sync"
)

// gzipMinSize: below this a JSON body isn't worth compressing (the gzip
// framing and the CPU cost outweigh the saving).
const gzipMinSize = 1024

var gzipPool = sync.Pool{New: func() any {
	w, _ := gzip.NewWriterLevel(nil, gzip.DefaultCompression)
	return w
}}

// gzipJSON compresses JSON API responses of gzipMinSize bytes or more
// for clients that accept gzip. Everything else -- small bodies, error
// text, images, anything already encoded, HEAD requests -- passes
// through byte for byte as before. It sits inside the collab route
// split (routes.go), so the WebSocket upgrade never goes through it.
func gzipJSON(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodHead || !acceptsGzip(r.Header.Get("Accept-Encoding")) {
			next.ServeHTTP(w, r)
			return
		}
		gw := &gzipResponseWriter{ResponseWriter: w, status: http.StatusOK}
		defer gw.finish()
		next.ServeHTTP(gw, r)
	})
}

func acceptsGzip(header string) bool {
	for _, part := range strings.Split(header, ",") {
		fields := strings.Split(strings.TrimSpace(part), ";")
		if strings.EqualFold(strings.TrimSpace(fields[0]), "gzip") {
			for _, f := range fields[1:] {
				if strings.ReplaceAll(strings.TrimSpace(f), " ", "") == "q=0" {
					return false
				}
			}
			return true
		}
	}
	return false
}

// gzipResponseWriter holds back the first gzipMinSize bytes to decide:
// a body that ends before that is sent uncompressed and unchanged.
type gzipResponseWriter struct {
	http.ResponseWriter
	status      int
	wroteHeader bool // WriteHeader called by the handler
	decided     bool
	gz          *gzip.Writer
	buf         bytes.Buffer
}

func (g *gzipResponseWriter) WriteHeader(code int) {
	if g.wroteHeader {
		return
	}
	g.wroteHeader = true
	g.status = code
}

func (g *gzipResponseWriter) Write(p []byte) (int, error) {
	if g.decided {
		if g.gz != nil {
			return g.gz.Write(p)
		}
		return g.ResponseWriter.Write(p)
	}
	g.buf.Write(p)
	if g.buf.Len() >= gzipMinSize {
		if err := g.decide(); err != nil {
			return 0, err
		}
	}
	return len(p), nil
}

func (g *gzipResponseWriter) eligible() bool {
	h := g.Header()
	if h.Get("Content-Encoding") != "" || g.status < 200 || g.status == http.StatusNoContent ||
		g.status == http.StatusNotModified || g.buf.Len() < gzipMinSize {
		return false
	}
	return strings.HasPrefix(h.Get("Content-Type"), "application/json")
}

// decide sends the headers and whatever is buffered, compressed or not.
func (g *gzipResponseWriter) decide() error {
	g.decided = true
	h := g.Header()
	if g.eligible() {
		h.Del("Content-Length")
		h.Set("Content-Encoding", "gzip")
		h.Add("Vary", "Accept-Encoding")
		gz := gzipPool.Get().(*gzip.Writer)
		gz.Reset(g.ResponseWriter)
		g.gz = gz
		g.ResponseWriter.WriteHeader(g.status)
		_, err := g.gz.Write(g.buf.Bytes())
		g.buf.Reset()
		return err
	}
	g.ResponseWriter.WriteHeader(g.status)
	_, err := g.ResponseWriter.Write(g.buf.Bytes())
	g.buf.Reset()
	return err
}

func (g *gzipResponseWriter) finish() {
	if !g.decided {
		_ = g.decide()
	}
	if g.gz != nil {
		_ = g.gz.Close()
		gzipPool.Put(g.gz)
		g.gz = nil
	}
}
