package httpapi

import (
	"log/slog"
	"net/http"
	"strconv"

	"nook/internal/activity"
	"nook/internal/auth"
	"nook/internal/httpx"
	"nook/internal/pages"
	"nook/internal/permissions"
	"nook/internal/spaces"
)

// recordActivity logs a page-level action and reflects the new
// "last edited by" onto page (for the response). Best effort: a failed
// log write must never fail the edit itself. store may be nil (tests).
func recordActivity(r *http.Request, store *activity.Store, page *pages.Page, action string, meta map[string]any) {
	if store == nil {
		return
	}
	userID := auth.UserIDFromContext(r.Context())
	if err := store.Record(r.Context(), userID, page.SpaceID, page.ID, action, meta); err != nil {
		slog.Warn("activity record failed", "err", err, "action", action, "page_id", page.ID)
		return
	}
	if action != activity.PageArchive {
		page.UpdatedBy = userID
	}
}

type activityAPI struct {
	activity *activity.Store
	pages    *pages.Store
	perm     *permissions.Checker
}

func limitParam(r *http.Request) int {
	n, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	return n
}

// pageActivityHandler: GET /api/pages/{pageID}/activity (viewer+).
func (a activityAPI) pageActivityHandler(w http.ResponseWriter, r *http.Request) {
	p := pagesAPI{pages: a.pages, perm: a.perm}
	page, ok := p.authorizePage(w, r, r.PathValue("pageID"), spaces.RoleViewer)
	if !ok {
		return
	}
	list, err := a.activity.ForPage(r.Context(), page.ID, limitParam(r))
	if err != nil {
		internalError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, list)
}

// spaceActivityHandler: GET /api/spaces/{spaceID}/activity (viewer+).
func (a activityAPI) spaceActivityHandler(w http.ResponseWriter, r *http.Request) {
	p := pagesAPI{pages: a.pages, perm: a.perm}
	spaceID := r.PathValue("spaceID")
	if !p.authorizeSpace(w, r, spaceID, spaces.RoleViewer) {
		return
	}
	list, err := a.activity.ForSpace(r.Context(), spaceID, limitParam(r))
	if err != nil {
		internalError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, list)
}
