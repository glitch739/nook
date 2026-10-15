package httpapi

import (
	"errors"
	"net/http"

	"nook/internal/auth"
	"nook/internal/collab"
	"nook/internal/db"
	"nook/internal/httpx"
	"nook/internal/spaces"
)

// historyAPI serves a page's history and the pages linking to it, both
// kept by collab.Store from the editor's snapshots (migration 0019).
type historyAPI struct {
	pages  pagesAPI
	collab *collab.Store
}

// GET /api/pages/{pageID}/versions (viewer+): newest first, no documents.
func (a historyAPI) versionsHandler(w http.ResponseWriter, r *http.Request) {
	page, ok := a.pages.authorizePage(w, r, r.PathValue("pageID"), spaces.RoleViewer)
	if !ok {
		return
	}
	list, err := a.collab.Versions(r.Context(), page.ID)
	if err != nil {
		internalError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, list)
}

// GET /api/pages/{pageID}/versions/{versionID} (viewer+): one version's
// document. Restoring happens in the editor (an ordinary edit), so
// there is no restore endpoint.
func (a historyAPI) versionHandler(w http.ResponseWriter, r *http.Request) {
	page, ok := a.pages.authorizePage(w, r, r.PathValue("pageID"), spaces.RoleViewer)
	if !ok {
		return
	}
	content, err := a.collab.VersionContent(r.Context(), page.ID, r.PathValue("versionID"))
	if errors.Is(err, db.ErrNotFound) {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	if err != nil {
		internalError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]string{"content_json": content})
}

// GET /api/pages/{pageID}/backlinks (viewer+): pages that link here,
// only those in spaces the caller can see.
func (a historyAPI) backlinksHandler(w http.ResponseWriter, r *http.Request) {
	page, ok := a.pages.authorizePage(w, r, r.PathValue("pageID"), spaces.RoleViewer)
	if !ok {
		return
	}
	all, err := a.collab.Backlinks(r.Context(), page.ID)
	if err != nil {
		internalError(w, err)
		return
	}
	userID := auth.UserIDFromContext(r.Context())
	visible := map[string]bool{}
	list := make([]collab.Backlink, 0, len(all))
	for _, b := range all {
		seen, checked := visible[b.SpaceID]
		if !checked {
			seen, err = a.pages.perm.CanAccessSpace(r.Context(), userID, b.SpaceID, spaces.RoleViewer)
			if err != nil {
				internalError(w, err)
				return
			}
			visible[b.SpaceID] = seen
		}
		if seen {
			list = append(list, b)
		}
	}
	httpx.WriteJSON(w, http.StatusOK, list)
}
