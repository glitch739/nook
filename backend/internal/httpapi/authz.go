package httpapi

import (
	"context"
	"errors"
	"net/http"

	"nook/internal/auth"
	"nook/internal/db"
	"nook/internal/pages"
	"nook/internal/permissions"
)

// authorizePageWith loads pageID with get and checks the caller holds at
// least minRole in its space. On any failure it writes the response
// itself (404 for "doesn't exist" AND "exists but you can't see it",
// deliberately indistinguishable, same anti-enumeration principle as
// login) and returns ok=false; callers return immediately then.
//
// get decides how much of the page is read: GetMetaByID for the many
// handlers that only need to know where the page lives, GetByID when
// the handler actually serves the document.
func authorizePageWith(
	w http.ResponseWriter,
	r *http.Request,
	perm *permissions.Checker,
	get func(context.Context, string) (pages.Page, error),
	pageID, minRole string,
) (pages.Page, bool) {
	page, err := get(r.Context(), pageID)
	if errors.Is(err, db.ErrNotFound) {
		http.Error(w, "not found", http.StatusNotFound)
		return pages.Page{}, false
	}
	if err != nil {
		internalError(w, err)
		return pages.Page{}, false
	}
	ok, err := perm.CanAccessSpace(r.Context(), auth.UserIDFromContext(r.Context()), page.SpaceID, minRole)
	if err != nil {
		internalError(w, err)
		return pages.Page{}, false
	}
	if !ok {
		http.Error(w, "not found", http.StatusNotFound)
		return pages.Page{}, false
	}
	return page, true
}
