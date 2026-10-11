package httpapi

import (
	"context"
	"net/http"
	"time"

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
	auth  *auth.Handler
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
	cookie, err := r.Cookie(auth.SessionCookieName)
	if err != nil { // RequireAuth already passed, so this can't happen
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	a.hub.Serve(w, r, page.ID, userID, !canEdit, a.stillAllowed(cookie.Value, userID, page.ID, canEdit))
}

// stillAllowed re-checks, for an open connection, everything collabHandler
// checked when it opened: session, account, page, and the same role. Any
// failure (errors included) closes it; the client reconnects and gets
// whatever access it has now.
func (a collabAPI) stillAllowed(token, userID, pageID string, canEdit bool) func(context.Context) bool {
	role := spaces.RoleViewer
	if canEdit {
		role = spaces.RoleEditor
	}
	return func(ctx context.Context) bool {
		ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		if _, u, ok := a.auth.ActiveSession(ctx, token); !ok || u.ID != userID {
			return false
		}
		page, err := a.pages.GetMetaByID(ctx, pageID)
		if err != nil || page.ArchivedAt != nil {
			return false
		}
		ok, err := a.perm.CanAccessSpace(ctx, userID, page.SpaceID, role)
		return err == nil && ok
	}
}
