package httpapi

import (
	"errors"
	"net/http"
	"nook/internal/activity"
	"nook/internal/collab"

	"nook/internal/auth"
	"nook/internal/db"
	"nook/internal/httpx"
	"nook/internal/pages"
	"nook/internal/permissions"
	"nook/internal/spaces"
)

// pagesAPI bundles the pages.Store with the permissions.Checker every
// handler here needs -- unlike spaces/auth, page access isn't
// self-evidently scoped to the caller (ListForUser already filters by
// membership; a page ID on its own doesn't), so every handler has to
// look up the page's space and check membership explicitly.
type pagesAPI struct {
	pages    *pages.Store
	perm     *permissions.Checker
	activity *activity.Store
	collab   *collab.Store
}

// authorizePage loads pageID and checks the caller holds at least
// minRole in its space. On any failure it writes the response itself
// (404 for "doesn't exist" AND "exists but you can't see it" -- these
// are deliberately indistinguishable from the response, same
// anti-enumeration principle as login) and returns ok=false; callers
// should return immediately when ok is false.
func (a pagesAPI) authorizePage(w http.ResponseWriter, r *http.Request, pageID, minRole string) (pages.Page, bool) {
	return authorizePageWith(w, r, a.perm, a.pages.GetMetaByID, pageID, minRole)
}

// authorizePageFull is authorizePage including the document
// (ContentJSON), for the handlers that serve it.
func (a pagesAPI) authorizePageFull(w http.ResponseWriter, r *http.Request, pageID, minRole string) (pages.Page, bool) {
	return authorizePageWith(w, r, a.perm, a.pages.GetByID, pageID, minRole)
}

// authorizeSpace checks the caller holds at least minRole in spaceID,
// writing the response itself on failure (same 404-for-both convention
// as authorizePage). Used by the create/list-tree handlers, which have
// a space ID but no page yet.
func (a pagesAPI) authorizeSpace(w http.ResponseWriter, r *http.Request, spaceID, minRole string) bool {
	userID := auth.UserIDFromContext(r.Context())
	ok, err := a.perm.CanAccessSpace(r.Context(), userID, spaceID, minRole)
	if err != nil {
		internalError(w, err)
		return false
	}
	if !ok {
		http.Error(w, "not found", http.StatusNotFound)
		return false
	}
	return true
}

// treeNodeResponse is the sidebar-shaped view of a page: no content_json
// (potentially large, and the sidebar never needs it) or timestamps,
// just enough to render and navigate a tree.
type treeNodeResponse struct {
	ID           string              `json:"id"`
	ParentPageID *string             `json:"parent_page_id"`
	Title        string              `json:"title"`
	Icon         string              `json:"icon"`
	Position     string              `json:"position"`
	Kind         string              `json:"kind"`
	Children     []*treeNodeResponse `json:"children"`
}

func toTreeResponse(n *pages.TreeNode) *treeNodeResponse {
	resp := &treeNodeResponse{
		ID:           n.ID,
		ParentPageID: n.ParentPageID,
		Title:        n.Title,
		Icon:         n.Icon,
		Position:     n.Position,
		Kind:         n.Kind,
		Children:     make([]*treeNodeResponse, len(n.Children)),
	}
	for i, c := range n.Children {
		resp.Children[i] = toTreeResponse(c)
	}
	return resp
}

// pageResponse is the full single-page view, content_json included --
// used when a specific page is opened for editing.
type pageResponse struct {
	ID            string  `json:"id"`
	SpaceID       string  `json:"space_id"`
	ParentPageID  *string `json:"parent_page_id"`
	Title         string  `json:"title"`
	Icon          string  `json:"icon"`
	ContentJSON   string  `json:"content_json"`
	Position      string  `json:"position"`
	CreatedBy     string  `json:"created_by"`
	Kind          string  `json:"kind"`
	CreatedAt     string  `json:"created_at"`
	UpdatedAt     string  `json:"updated_at"`
	UpdatedBy     string  `json:"updated_by"`
	UpdatedByName string  `json:"updated_by_name"`
}

func toPageResponse(p pages.Page) pageResponse {
	return pageResponse{
		ID:            p.ID,
		SpaceID:       p.SpaceID,
		ParentPageID:  p.ParentPageID,
		Title:         p.Title,
		Icon:          p.Icon,
		ContentJSON:   p.ContentJSON,
		Position:      p.Position,
		CreatedBy:     p.CreatedBy,
		Kind:          p.Kind,
		CreatedAt:     p.CreatedAt,
		UpdatedAt:     p.UpdatedAt,
		UpdatedBy:     p.UpdatedBy,
		UpdatedByName: p.UpdatedByName,
	}
}

// createPageHandler handles POST /api/spaces/{spaceID}/pages
// (RequireAuth-protected). Body: {"parent_page_id": "..."|null,
// "title": "...", "kind": "document"|"database"?} -- kind defaults to
// "document" when omitted, so every existing caller keeps working
// unchanged; a database's *rows* are also created through this same
// endpoint (kind "document", parent_page_id = the database page -- see
// ADR 0004), nothing special about them at creation time. Requires at
// least editor on the space.
func (a pagesAPI) createPageHandler(w http.ResponseWriter, r *http.Request) {
	spaceID := r.PathValue("spaceID")
	if !a.authorizeSpace(w, r, spaceID, spaces.RoleEditor) {
		return
	}

	var req struct {
		ParentPageID *string `json:"parent_page_id"`
		Title        string  `json:"title"`
		Kind         string  `json:"kind"`
	}
	if !httpx.DecodeJSONBody(w, r, &req) {
		return
	}
	if req.Kind == "" {
		req.Kind = pages.KindDocument
	}
	if req.Kind != pages.KindDocument && req.Kind != pages.KindDatabase && req.Kind != pages.KindCollection {
		http.Error(w, `kind must be "document", "database" or "collection"`, http.StatusBadRequest)
		return
	}

	// A page's parent, if given, must actually be in this space --
	// otherwise a caller with editor access to space A could nest a new
	// page under a page ID that happens to belong to space B, wiring
	// content across spaces they don't necessarily have any access to.
	if req.ParentPageID != nil {
		parent, err := a.pages.GetMetaByID(r.Context(), *req.ParentPageID)
		if errors.Is(err, db.ErrNotFound) || (err == nil && parent.SpaceID != spaceID) {
			http.Error(w, "parent_page_id is not a page in this space", http.StatusBadRequest)
			return
		}
		if err != nil {
			internalError(w, err)
			return
		}
		if !pages.CanHold(parent.Kind, req.Kind) {
			http.Error(w, "a collection can't be placed there", http.StatusBadRequest)
			return
		}
	}

	userID := auth.UserIDFromContext(r.Context())
	page, err := a.pages.Create(r.Context(), spaceID, req.ParentPageID, req.Title, userID, req.Kind)
	if err != nil {
		internalError(w, err)
		return
	}
	recordActivity(r, a.activity, &page, activity.PageCreate, map[string]any{"title": page.Title, "kind": page.Kind})

	httpx.WriteJSON(w, http.StatusCreated, toPageResponse(page))
}

// listPagesHandler handles GET /api/spaces/{spaceID}/pages
// (RequireAuth-protected): the space's page tree. Requires at least
// viewer on the space.
func (a pagesAPI) listPagesHandler(w http.ResponseWriter, r *http.Request) {
	spaceID := r.PathValue("spaceID")
	if !a.authorizeSpace(w, r, spaceID, spaces.RoleViewer) {
		return
	}

	list, err := a.pages.ListTreeForSpaces(r.Context(), []string{spaceID})
	if err != nil {
		internalError(w, err)
		return
	}

	tree := pages.BuildTree(list)
	resp := make([]*treeNodeResponse, len(tree))
	for i, n := range tree {
		resp[i] = toTreeResponse(n)
	}

	httpx.WriteJSON(w, http.StatusOK, resp)
}

// getPageHandler handles GET /api/pages/{pageID} (RequireAuth-protected):
// the full page including content_json. Requires at least viewer on the
// page's space.
func (a pagesAPI) getPageHandler(w http.ResponseWriter, r *http.Request) {
	page, ok := a.authorizePageFull(w, r, r.PathValue("pageID"), spaces.RoleViewer)
	if !ok {
		return
	}
	httpx.WriteJSON(w, http.StatusOK, toPageResponse(page))
}

// updatePageHandler handles PUT /api/pages/{pageID}
// (RequireAuth-protected). Body: {"title": "..."?, "icon": "..."?,
// "content_json": "..."?} -- any subset of these may be provided; only
// fields present get updated. Requires at least editor on the page's
// space.
func (a pagesAPI) updatePageHandler(w http.ResponseWriter, r *http.Request) {
	page, ok := a.authorizePage(w, r, r.PathValue("pageID"), spaces.RoleEditor)
	if !ok {
		return
	}

	var req struct {
		Title       *string `json:"title"`
		Icon        *string `json:"icon"`
		ContentJSON *string `json:"content_json"`
	}
	if !httpx.DecodeJSONBody(w, r, &req) {
		return
	}
	if req.Title == nil && req.Icon == nil && req.ContentJSON == nil {
		http.Error(w, "nothing to update: provide title, icon, and/or content_json", http.StatusBadRequest)
		return
	}

	if req.Title != nil {
		updated, err := a.pages.UpdateTitle(r.Context(), page.ID, *req.Title)
		if err != nil {
			internalError(w, err)
			return
		}
		page = updated
	}
	if req.Icon != nil {
		updated, err := a.pages.UpdateIcon(r.Context(), page.ID, *req.Icon)
		if err != nil {
			internalError(w, err)
			return
		}
		page = updated
	}
	// A page under realtime editing (ADR 0006) has its content in Yjs;
	// a plain content_json write would be silently overwritten by the
	// next snapshot, so refuse it rather than lose it.
	if req.ContentJSON != nil && a.collab != nil {
		live, err := a.collab.HasState(r.Context(), page.ID)
		if err != nil {
			internalError(w, err)
			return
		}
		if live {
			http.Error(w, "page content is edited live; use the collaborative editor", http.StatusConflict)
			return
		}
	}
	if req.ContentJSON != nil {
		updated, err := a.pages.UpdateContent(r.Context(), page.ID, *req.ContentJSON)
		if err != nil {
			internalError(w, err)
			return
		}
		page = updated
	}

	switch {
	case req.Title != nil:
		recordActivity(r, a.activity, &page, activity.PageRename, map[string]any{"title": page.Title})
	default:
		recordActivity(r, a.activity, &page, activity.PageEdit, nil)
	}

	httpx.WriteJSON(w, http.StatusOK, toPageResponse(page))
}

// archivePageHandler handles DELETE /api/pages/{pageID}
// (RequireAuth-protected): soft-deletes the page and everything under
// it (see pages.Store.Archive; restorable from the archive view).
// Requires at least editor on the page's space.
func (a pagesAPI) archivePageHandler(w http.ResponseWriter, r *http.Request) {
	page, ok := a.authorizePage(w, r, r.PathValue("pageID"), spaces.RoleEditor)
	if !ok {
		return
	}
	if err := a.pages.Archive(r.Context(), page.ID); err != nil {
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	recordActivity(r, a.activity, &page, activity.PageArchive, map[string]any{"title": page.Title})
	w.WriteHeader(http.StatusNoContent)
}

// reorderPageSiblingsHandler handles PUT /api/spaces/{spaceID}/pages/order
// (RequireAuth-protected). Body: {"parent_page_id": "..."|null,
// "page_ids": [...]} -- page_ids must be exactly a permutation of
// parent_page_id's current children within spaceID (top-level pages if
// null), same validate-then-full-restamp shape as
// reorderColumnsHandler/reorderSpacesHandler/reorderGroupsHandler.
// Requires at least editor on the space.
func (a pagesAPI) reorderPageSiblingsHandler(w http.ResponseWriter, r *http.Request) {
	spaceID := r.PathValue("spaceID")
	if !a.authorizeSpace(w, r, spaceID, spaces.RoleEditor) {
		return
	}

	var req struct {
		ParentPageID *string  `json:"parent_page_id"`
		PageIDs      []string `json:"page_ids"`
	}
	if !httpx.DecodeJSONBody(w, r, &req) {
		return
	}

	all, err := a.pages.ListTreeForSpaces(r.Context(), []string{spaceID})
	if err != nil {
		internalError(w, err)
		return
	}
	current := siblingsOf(all, req.ParentPageID)
	if !isPermutationOfPages(req.PageIDs, current) {
		http.Error(w, "page_ids must be exactly this parent's current children", http.StatusBadRequest)
		return
	}

	if err := a.pages.ReorderSiblings(r.Context(), spaceID, req.ParentPageID, req.PageIDs); err != nil {
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func samePageParent(a, b *string) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

func siblingsOf(all []pages.Page, parentPageID *string) []pages.Page {
	result := make([]pages.Page, 0, len(all))
	for _, p := range all {
		if samePageParent(p.ParentPageID, parentPageID) {
			result = append(result, p)
		}
	}
	return result
}

func isPermutationOfPages(ids []string, current []pages.Page) bool {
	if len(ids) != len(current) {
		return false
	}
	want := make(map[string]bool, len(current))
	for _, p := range current {
		want[p.ID] = true
	}
	seen := make(map[string]bool, len(ids))
	for _, id := range ids {
		if !want[id] || seen[id] {
			return false
		}
		seen[id] = true
	}
	return true
}

// movePageHandler handles PUT /api/pages/{pageID}/move
// (RequireAuth-protected). Body: {"space_id": "...", "parent_page_id":
// "..."|null}. Requires at least editor on *both* the page's current
// space and the destination space (moving a page out of a space you
// can edit but into one you can't -- or the reverse -- would each be a
// real permission bypass, not just a UX inconvenience). The moved page
// always lands as the last child of its new parent; a drag that also
// needs a specific position among the new siblings follows this with a
// PUT .../pages/order call, same two-step pattern used nowhere else
// only because nothing else here combines a parent change with a
// position choice in one gesture.
func (a pagesAPI) movePageHandler(w http.ResponseWriter, r *http.Request) {
	page, ok := a.authorizePage(w, r, r.PathValue("pageID"), spaces.RoleEditor)
	if !ok {
		return
	}

	var req struct {
		SpaceID      string  `json:"space_id"`
		ParentPageID *string `json:"parent_page_id"`
	}
	if !httpx.DecodeJSONBody(w, r, &req) {
		return
	}
	if req.SpaceID == "" {
		http.Error(w, "space_id is required", http.StatusBadRequest)
		return
	}

	if req.SpaceID != page.SpaceID && !a.authorizeSpace(w, r, req.SpaceID, spaces.RoleEditor) {
		return
	}
	if req.ParentPageID != nil {
		parent, err := a.pages.GetMetaByID(r.Context(), *req.ParentPageID)
		if err == nil && !pages.CanHold(parent.Kind, page.Kind) {
			http.Error(w, "a collection can't be placed there", http.StatusBadRequest)
			return
		}
		// Not found etc.: Move below reports it.
	}

	err := a.pages.Move(r.Context(), page.ID, req.SpaceID, req.ParentPageID)
	switch {
	case errors.Is(err, pages.ErrSelfParent), errors.Is(err, pages.ErrCircularHierarchy), errors.Is(err, pages.ErrParentInDifferentSpace):
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	case errors.Is(err, db.ErrNotFound):
		http.Error(w, "not found", http.StatusNotFound)
		return
	case err != nil:
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	moved := page
	moved.SpaceID = req.SpaceID
	recordActivity(r, a.activity, &moved, activity.PageMove, map[string]any{"from_space_id": page.SpaceID})

	updated, err := a.pages.GetByID(r.Context(), page.ID)
	if err != nil {
		internalError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, toPageResponse(updated))
}

// collectionItemsHandler handles GET /api/pages/{pageID}/items (viewer+):
// a collection's documents and databases, most recently edited first.
func (a pagesAPI) collectionItemsHandler(w http.ResponseWriter, r *http.Request) {
	page, ok := a.authorizePage(w, r, r.PathValue("pageID"), spaces.RoleViewer)
	if !ok {
		return
	}
	if page.Kind != pages.KindCollection {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	items, err := a.pages.ListCollectionItems(r.Context(), page.ID)
	if err != nil {
		internalError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, items)
}
