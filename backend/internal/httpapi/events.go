package httpapi

import (
	"net/http"
	"strings"
	"time"

	"nook/internal/events"
)

// GET /api/events (signed in): a Server-Sent Events stream. Sends
// "event: tree" whenever pages, spaces or groups changed anywhere (the
// browser reloads its sidebar) and "event: data" whenever database
// cells, columns or select options changed (open database views and
// row pages re-fetch). No data about the change itself is sent, so
// nobody learns about spaces they can't see.
func eventsHandler(hub *events.Hub) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "streaming unsupported", http.StatusInternalServerError)
			return
		}
		h := w.Header()
		h.Set("Content-Type", "text/event-stream")
		h.Set("Cache-Control", "no-cache, no-transform")
		h.Set("X-Accel-Buffering", "no")
		sub, stop := hub.Subscribe()
		defer stop()
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("retry: 5000\n: connected\n\n"))
		flusher.Flush()

		// A comment every 25 s keeps proxies (Cloudflare closes idle
		// connections after ~100 s) from dropping the stream.
		heartbeat := time.NewTicker(25 * time.Second)
		defer heartbeat.Stop()
		for {
			select {
			case <-r.Context().Done():
				return
			case <-heartbeat.C:
				if _, err := w.Write([]byte(": ping\n\n")); err != nil {
					return
				}
			case <-sub.C:
				k := sub.Take()
				if k&events.Tree != 0 {
					if _, err := w.Write([]byte("event: tree\ndata: {}\n\n")); err != nil {
						return
					}
				}
				if k&events.Data != 0 {
					if _, err := w.Write([]byte("event: data\ndata: {}\n\n")); err != nil {
						return
					}
				}
			}
			flusher.Flush()
		}
	}
}

// notifyChanges signals the hub after a successful write. Tree: pages,
// spaces, groups (incl. members), archive restore/delete, which change
// what the sidebar shows. Data: cell values, columns and their order,
// select options, which change what an open database shows. Covers and
// realtime text are neither (realtime text has its own channel).
func notifyChanges(hub *events.Hub, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet || r.Method == http.MethodHead {
			next.ServeHTTP(w, r)
			return
		}
		var kind events.Kind
		if affectsTree(r.URL.Path) {
			kind |= events.Tree
		}
		if affectsData(r.URL.Path) {
			kind |= events.Data
		}
		if kind == 0 {
			next.ServeHTTP(w, r)
			return
		}
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		if rec.status < 400 {
			hub.Signal(kind)
		}
	})
}

// affectsData: PUT /api/pages/{row}/properties/{prop} (a cell),
// POST /api/pages/{db}/properties (new column), PUT
// /api/pages/{db}/columns/order, PUT/DELETE /api/properties/{prop}
// (rename, options, delete column).
func affectsData(p string) bool {
	if strings.HasPrefix(p, "/api/properties/") {
		return true
	}
	return strings.HasPrefix(p, "/api/pages/") &&
		(strings.Contains(p, "/properties") || strings.Contains(p, "/columns"))
}

func affectsTree(p string) bool {
	if !strings.HasPrefix(p, "/api/pages") && !strings.HasPrefix(p, "/api/spaces") &&
		!strings.HasPrefix(p, "/api/groups") && !strings.HasPrefix(p, "/api/archive") {
		return false
	}
	for _, skip := range []string{"/properties", "/cover", "/columns", "/collab", "/rows"} {
		if strings.Contains(p, skip) {
			return false
		}
	}
	return true
}

type statusRecorder struct {
	http.ResponseWriter
	status int
	wrote  bool
}

func (s *statusRecorder) WriteHeader(code int) {
	if !s.wrote {
		s.status, s.wrote = code, true
	}
	s.ResponseWriter.WriteHeader(code)
}

func (s *statusRecorder) Write(b []byte) (int, error) {
	s.wrote = true
	return s.ResponseWriter.Write(b)
}
