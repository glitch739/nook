package httpapi

import (
	"errors"
	"net/http"

	"nook/internal/activity"
	"nook/internal/archive"
	"nook/internal/auth"
	"nook/internal/db"
	"nook/internal/groups"
	"nook/internal/httpx"
	"nook/internal/pages"
	"nook/internal/permissions"
	"nook/internal/spaces"
)

// archiveAPI backs the archive view: list what's archived, bring it
// back. Restoring needs the same role archiving did: editor for a page,
// owner for a space or group.
type archiveAPI struct {
	archive  *archive.Store
	pages    pagesAPI
	spaces   *spaces.Store
	groups   *groups.Store
	perm     *permissions.Checker
	activity *activity.Store
}

// GET /api/archive
func (a archiveAPI) listHandler(w http.ResponseWriter, r *http.Request) {
	l, err := a.archive.ForUser(r.Context(), auth.UserIDFromContext(r.Context()))
	if err != nil {
		internalError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, l)
}

// POST /api/pages/{pageID}/restore (editor+)
func (a archiveAPI) restorePageHandler(w http.ResponseWriter, r *http.Request) {
	page, ok := a.pages.authorizePage(w, r, r.PathValue("pageID"), spaces.RoleEditor)
	if !ok {
		return
	}
	switch err := a.pages.pages.Restore(r.Context(), page.ID); {
	case errors.Is(err, db.ErrNotFound):
		http.Error(w, "page is not archived", http.StatusConflict)
		return
	case errors.Is(err, pages.ErrSpaceArchived):
		http.Error(w, "the page's space is archived; restore the space first", http.StatusConflict)
		return
	case err != nil:
		internalError(w, err)
		return
	}
	recordActivity(r, a.activity, &page, activity.PageRestore, map[string]any{"title": page.Title})
	w.WriteHeader(http.StatusNoContent)
}

// POST /api/spaces/{spaceID}/restore (owner). If the space's group is
// archived too, the group comes back with it -- otherwise the restored
// space would have nowhere to show up in the sidebar. With an archived
// group the space-level check can't see group membership (it ignores
// archived groups), so ownership of the group is what counts then.
func (a archiveAPI) restoreSpaceHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	userID := auth.UserIDFromContext(ctx)
	sp, err := a.spaces.GetByID(ctx, r.PathValue("spaceID"))
	if errors.Is(err, db.ErrNotFound) {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	if err != nil {
		internalError(w, err)
		return
	}

	var groupArchived bool
	if sp.GroupID != "" {
		g, err := a.groups.GetByID(ctx, sp.GroupID)
		if err != nil && !errors.Is(err, db.ErrNotFound) {
			internalError(w, err)
			return
		}
		groupArchived = err == nil && g.ArchivedAt != nil
	}

	var allowed bool
	if groupArchived {
		allowed, err = a.perm.CanAccessGroup(ctx, userID, sp.GroupID, groups.RoleOwner)
	} else {
		allowed, err = a.perm.CanAccessArchivedSpace(ctx, userID, sp.ID, spaces.RoleOwner)
	}
	if err != nil {
		internalError(w, err)
		return
	}
	if !allowed {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	if groupArchived {
		if err := a.groups.Restore(ctx, sp.GroupID); err != nil && !errors.Is(err, db.ErrNotFound) {
			internalError(w, err)
			return
		}
	}
	switch err := a.spaces.Restore(ctx, sp.ID); {
	case errors.Is(err, db.ErrNotFound):
		http.Error(w, "space is not archived", http.StatusConflict)
		return
	case err != nil:
		internalError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// POST /api/groups/{groupID}/restore (owner)
func (a archiveAPI) restoreGroupHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	groupID := r.PathValue("groupID")
	allowed, err := a.perm.CanAccessGroup(ctx, auth.UserIDFromContext(ctx), groupID, groups.RoleOwner)
	if err != nil {
		internalError(w, err)
		return
	}
	if !allowed {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	switch err := a.groups.Restore(ctx, groupID); {
	case errors.Is(err, db.ErrNotFound):
		http.Error(w, "group is not archived", http.StatusConflict)
		return
	case err != nil:
		internalError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// DELETE /api/archive/pages/{pageID} (editor+): permanent.
func (a archiveAPI) deletePageHandler(w http.ResponseWriter, r *http.Request) {
	page, ok := a.pages.authorizePage(w, r, r.PathValue("pageID"), spaces.RoleEditor)
	if !ok {
		return
	}
	a.finishDelete(w, a.pages.pages.DeleteArchived(r.Context(), page.ID), "page")
}

// DELETE /api/archive/spaces/{spaceID} (owner): permanent, with its pages.
func (a archiveAPI) deleteSpaceHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	userID := auth.UserIDFromContext(ctx)
	sp, err := a.spaces.GetByID(ctx, r.PathValue("spaceID"))
	if errors.Is(err, db.ErrNotFound) {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	if err != nil {
		internalError(w, err)
		return
	}
	// Same rule as restore: through the group if the group is archived.
	allowed, err := a.perm.CanAccessArchivedSpace(ctx, userID, sp.ID, spaces.RoleOwner)
	if err == nil && !allowed && sp.GroupID != "" {
		allowed, err = a.perm.CanAccessGroup(ctx, userID, sp.GroupID, groups.RoleOwner)
	}
	if err != nil {
		internalError(w, err)
		return
	}
	if !allowed {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	a.finishDelete(w, a.spaces.DeleteArchived(ctx, sp.ID), "space")
}

// DELETE /api/archive/groups/{groupID} (owner): permanent, empty groups only.
func (a archiveAPI) deleteGroupHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	groupID := r.PathValue("groupID")
	allowed, err := a.perm.CanAccessGroup(ctx, auth.UserIDFromContext(ctx), groupID, groups.RoleOwner)
	if err != nil {
		internalError(w, err)
		return
	}
	if !allowed {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	err = a.groups.DeleteArchived(ctx, groupID)
	if errors.Is(err, groups.ErrGroupHasSpaces) {
		http.Error(w, "the group still has spaces; delete or restore them first", http.StatusConflict)
		return
	}
	a.finishDelete(w, err, "group")
}

func (a archiveAPI) finishDelete(w http.ResponseWriter, err error, what string) {
	switch {
	case errors.Is(err, db.ErrNotFound):
		http.Error(w, "only an archived "+what+" can be deleted", http.StatusConflict)
	case err != nil:
		internalError(w, err)
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}
