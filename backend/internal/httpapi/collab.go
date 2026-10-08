package httpapi

import (
	"net/http"

	"nook/internal/auth"
	"nook/internal/collab"
	"nook/internal/pages"
	"nook/internal/permissions"
	"nook/internal/spaces"
)

type collabAPI struct {
	hub   *collab.Hub
	pages *pages.Store
	perm  *permissions.Checker
}

// collabHandler: GET /api/pages/{pageID}/collab, upgraded to a WebSocket
// (ADR 0006). Viewer+ may join; below editor the connection is
// read-only (server drops their writes).
func (a collabAPI) collabHandler(w http.ResponseWriter, r *http.Request) {
	p := pagesAPI{pages: a.pages, perm: a.perm}
	page, ok := p.authorizePage(w, r, r.PathValue("pageID"), spaces.RoleViewer)
	if !ok {
		return
	}
	if page.ArchivedAt != nil || page.Kind != pages.KindDocument {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	userID := auth.UserIDFromContext(r.Context())
	canEdit, err := a.perm.CanAccessSpace(r.Context(), userID, page.SpaceID, spaces.RoleEditor)
	if err != nil {
		internalError(w, err)
		return
	}
	a.hub.Serve(w, r, page.ID, userID, !canEdit)
}
