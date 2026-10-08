package httpapi

import (
	"log/slog"
	"mime"
	"net/http"
	"time"

	"nook/internal/auth"
	"nook/internal/export"
	"nook/internal/spaces"
)

type exportAPI struct {
	exp   export.Exporter
	pages pagesAPI
}

// GET /api/export.zip: everything the signed-in user can see, as
// Markdown + files + full JSON. Streams; runs outside the 5 s timeout.
func (a exportAPI) zipHandler(w http.ResponseWriter, r *http.Request) {
	now := time.Now()
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment",
		map[string]string{"filename": "nook-export-" + now.Format("2006-01-02") + ".zip"}))
	w.Header().Set("Cache-Control", "no-store")
	if err := a.exp.Zip(r.Context(), w, auth.UserIDFromContext(r.Context()), now); err != nil {
		// Headers are gone by now; the client gets a truncated (invalid)
		// ZIP, and the log says why.
		slog.Error("export failed", "err", err)
	}
}

// GET /api/pages/{pageID}/export.md (viewer+): one page as Markdown.
func (a exportAPI) pageMarkdownHandler(w http.ResponseWriter, r *http.Request) {
	page, ok := a.pages.authorizePageFull(w, r, r.PathValue("pageID"), spaces.RoleViewer)
	if !ok {
		return
	}
	md, err := a.exp.PageMarkdown(r.Context(), page)
	if err != nil {
		internalError(w, err)
		return
	}
	name := page.Title
	if name == "" {
		name = "Untitled"
	}
	w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": name + ".md"}))
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write([]byte(md))
}
