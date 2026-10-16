package httpapi

import (
	"errors"
	"net/http"

	"nook/internal/auth"
	"nook/internal/db"
	"nook/internal/groups"
	"nook/internal/httpx"
	"nook/internal/permissions"
	"nook/internal/spaces"
)

// spacesAPI bundles the spaces.Store with the permissions.Checker the
// update/archive handlers need -- same shape as pagesAPI in pages.go,
// for the same reason: a space ID on its own doesn't prove membership,
// that has to be checked explicitly.
type spacesAPI struct {
	spaces *spaces.Store
	perm   *permissions.Checker
}

type spaceResponse struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Type    string `json:"type"`
	Icon    string `json:"icon"`
	GroupID string `json:"group_id"`
	// Only ever set by listSpacesHandler -- the caller's own ordering
	// of this space, meaningless (and left blank) everywhere else a
	// spaceResponse gets built, since Create/UpdateName/UpdateIcon
	// return a plain spaces.Space with no position attached to it.
	Position string `json:"position,omitempty"`
}

func toSpaceResponse(sp spaces.Space) spaceResponse {
	return spaceResponse{ID: sp.ID, Name: sp.Name, Type: sp.Type, Icon: sp.Icon, GroupID: sp.GroupID}
}

// authorizeSpace loads spaceID and checks the caller holds at least
// minRole there, writing the response itself on failure (404 for both
// "doesn't exist" and "exists but you can't see it" -- same
// anti-enumeration convention as pagesAPI.authorizeSpace/authorizePage).
func (a spacesAPI) authorizeSpace(w http.ResponseWriter, r *http.Request, spaceID, minRole string) (spaces.Space, bool) {
	sp, err := a.spaces.GetByID(r.Context(), spaceID)
	if errors.Is(err, db.ErrNotFound) || (sp.ArchivedAt != nil) {
		http.Error(w, "not found", http.StatusNotFound)
		return spaces.Space{}, false
	}
	if err != nil {
		internalError(w, err)
		return spaces.Space{}, false
	}

	userID := auth.UserIDFromContext(r.Context())
	ok, err := a.perm.CanAccessSpace(r.Context(), userID, spaceID, minRole)
	if err != nil {
		internalError(w, err)
		return spaces.Space{}, false
	}
	if !ok {
		http.Error(w, "not found", http.StatusNotFound)
		return spaces.Space{}, false
	}
	return sp, true
}

// createSpaceHandler handles POST /api/spaces (RequireAuth-protected).
// Body: {"name": "...", "type": "private"|"shared", "group_id": "..."}.
// Every space belongs to a group now (migrations/0009_groups.sql) --
// the caller must hold at least editor there, same bar as adding a page
// to an existing space needs. The caller becomes the space's own
// owner (its group role is separate from, and doesn't have to match,
// their role in the group it lives in); its icon starts at a per-type
// default (spaces.Store.Create) and is editable afterward via
// updateSpaceHandler.
func (a spacesAPI) createSpaceHandler(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name    string `json:"name"`
		Type    string `json:"type"`
		GroupID string `json:"group_id"`
	}
	if !httpx.DecodeJSONBody(w, r, &req) {
		return
	}
	if req.Name == "" {
		http.Error(w, "name is required", http.StatusBadRequest)
		return
	}
	if req.Type != spaces.TypePrivate && req.Type != spaces.TypeShared {
		http.Error(w, `type must be "private" or "shared"`, http.StatusBadRequest)
		return
	}
	if req.GroupID == "" {
		http.Error(w, "group_id is required", http.StatusBadRequest)
		return
	}

	userID := auth.UserIDFromContext(r.Context())
	ok, err := a.perm.CanAccessGroup(r.Context(), userID, req.GroupID, groups.RoleEditor)
	if err != nil {
		internalError(w, err)
		return
	}
	if !ok {
		// 404, not 403 -- same anti-enumeration convention as
		// authorizeSpace: an unknown group_id and one the caller just
		// isn't a member of look identical from here.
		http.Error(w, "not found", http.StatusNotFound)
		return
	}

	sp, err := a.spaces.Create(r.Context(), req.Name, req.Type, userID, req.GroupID)
	if err != nil {
		internalError(w, err)
		return
	}

	httpx.WriteJSON(w, http.StatusCreated, toSpaceResponse(sp))
}

// listSpacesHandler handles GET /api/spaces (RequireAuth-protected):
// every non-archived space the caller is a member of.
func (a spacesAPI) listSpacesHandler(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromContext(r.Context())
	list, err := a.spaces.ListForUser(r.Context(), userID)
	if err != nil {
		internalError(w, err)
		return
	}

	resp := make([]spaceResponse, len(list))
	for i, sp := range list {
		resp[i] = toSpaceResponse(sp.Space)
		resp[i].Position = sp.Position
	}

	httpx.WriteJSON(w, http.StatusOK, resp)
}

// reorderSpacesHandler handles PUT /api/spaces/order
// (RequireAuth-protected). Body: {"space_ids": ["...", ...]} -- must be
// exactly a permutation of the caller's own current spaces (validated
// against a fresh ListForUser call, same "fetch current, compare as a
// set" shape reorderColumnsHandler uses); ReorderForUser itself is also
// scoped to the caller's own membership rows regardless, so this check
// exists to give a clear 400 rather than relying solely on that.
func (a spacesAPI) reorderSpacesHandler(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromContext(r.Context())

	var req struct {
		SpaceIDs []string `json:"space_ids"`
	}
	if !httpx.DecodeJSONBody(w, r, &req) {
		return
	}

	current, err := a.spaces.ListForUser(r.Context(), userID)
	if err != nil {
		internalError(w, err)
		return
	}
	if !isPermutationOfSpaces(req.SpaceIDs, current) {
		http.Error(w, "space_ids must be exactly this user's current spaces", http.StatusBadRequest)
		return
	}

	if err := a.spaces.ReorderForUser(r.Context(), userID, req.SpaceIDs); err != nil {
		internalError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func isPermutationOfSpaces(ids []string, current []spaces.UserSpace) bool {
	if len(ids) != len(current) {
		return false
	}
	want := make(map[string]bool, len(current))
	for _, sp := range current {
		want[sp.ID] = true
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

// updateSpaceHandler handles PUT /api/spaces/{spaceID}
// (RequireAuth-protected). Body: {"name": "...", "icon": "..."} -- any
// subset of these may be provided; only fields present get updated
// (type isn't editable, no UI for that yet). Requires at least editor,
// same level pages title/icon updates need.
func (a spacesAPI) updateSpaceHandler(w http.ResponseWriter, r *http.Request) {
	sp, ok := a.authorizeSpace(w, r, r.PathValue("spaceID"), spaces.RoleEditor)
	if !ok {
		return
	}

	var req struct {
		Name *string `json:"name"`
		Icon *string `json:"icon"`
	}
	if !httpx.DecodeJSONBody(w, r, &req) {
		return
	}
	if req.Name == nil && req.Icon == nil {
		http.Error(w, "nothing to update: provide name and/or icon", http.StatusBadRequest)
		return
	}
	if req.Name != nil && *req.Name == "" {
		http.Error(w, "name cannot be empty", http.StatusBadRequest)
		return
	}

	if req.Name != nil {
		updated, err := a.spaces.UpdateName(r.Context(), sp.ID, *req.Name)
		if err != nil {
			internalError(w, err)
			return
		}
		sp = updated
	}
	if req.Icon != nil {
		updated, err := a.spaces.UpdateIcon(r.Context(), sp.ID, *req.Icon)
		if err != nil {
			internalError(w, err)
			return
		}
		sp = updated
	}

	httpx.WriteJSON(w, http.StatusOK, toSpaceResponse(sp))
}

// moveSpaceHandler handles PUT /api/spaces/{spaceID}/move
// (RequireAuth-protected). Body: {"group_id": "..."}. Requires owner on
// the space (moving it changes who can see it, like archiving does) and
// at least editor on the destination group.
func (a spacesAPI) moveSpaceHandler(w http.ResponseWriter, r *http.Request) {
	sp, ok := a.authorizeSpace(w, r, r.PathValue("spaceID"), spaces.RoleOwner)
	if !ok {
		return
	}

	var req struct {
		GroupID string `json:"group_id"`
	}
	if !httpx.DecodeJSONBody(w, r, &req) {
		return
	}
	if req.GroupID == "" {
		http.Error(w, "group_id is required", http.StatusBadRequest)
		return
	}

	userID := auth.UserIDFromContext(r.Context())
	if req.GroupID != sp.GroupID {
		ok, err := a.perm.CanAccessGroup(r.Context(), userID, req.GroupID, groups.RoleEditor)
		if err != nil {
			internalError(w, err)
			return
		}
		if !ok {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
	}

	updated, err := a.spaces.MoveToGroup(r.Context(), sp.ID, req.GroupID)
	if err != nil {
		internalError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, toSpaceResponse(updated))
}

// archiveSpaceHandler handles DELETE /api/spaces/{spaceID}
// (RequireAuth-protected): soft-deletes the whole space (see
// spaces.Store.Archive). Requires owner specifically, not just editor --
// unlike archiving one page, this removes an entire space (and, for a
// shared one, everyone else's access to it), which is a call only its
// owner should be able to make.
func (a spacesAPI) archiveSpaceHandler(w http.ResponseWriter, r *http.Request) {
	sp, ok := a.authorizeSpace(w, r, r.PathValue("spaceID"), spaces.RoleOwner)
	if !ok {
		return
	}
	if err := a.spaces.Archive(r.Context(), sp.ID); err != nil {
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
