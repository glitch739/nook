package httpapi

import (
	"errors"
	"io"
	"net/http"
	"strconv"

	"nook/internal/assets"
	"nook/internal/auth"
	"nook/internal/db"
	"nook/internal/httpx"
	"nook/internal/pages"
	"nook/internal/permissions"
	"nook/internal/spaces"
)

// coversAPI serves page cover images (ADR 0004's deliberately-last
// piece) -- one image per page, stored on local disk via
// internal/assets.
type coversAPI struct {
	assets *assets.Store
	pages  *pages.Store
	perm   *permissions.Checker
}

// authorizePage: same check as pagesAPI.authorizePage (authorizePageWith),
// without loading the document.
func (a coversAPI) authorizePage(w http.ResponseWriter, r *http.Request, pageID, minRole string) (pages.Page, bool) {
	return authorizePageWith(w, r, a.perm, a.pages.GetMetaByID, pageID, minRole)
}

type coverResponse struct {
	ID        string  `json:"id"`
	Filename  string  `json:"filename"`
	MimeType  string  `json:"mime_type"`
	SizeBytes int64   `json:"size_bytes"`
	PositionY float64 `json:"position_y"`
}

func toCoverResponse(c assets.Cover) coverResponse {
	return coverResponse{
		ID:        c.ID,
		Filename:  c.Filename,
		MimeType:  c.MimeType,
		SizeBytes: c.SizeBytes,
		PositionY: c.PositionY,
	}
}

// uploadCoverHandler handles PUT /api/pages/{pageID}/cover
// (RequireAuth-protected). multipart/form-data with one file field
// named "file" -- image/png|jpeg|webp|gif only, capped at
// assets.MaxCoverSize. Replaces any existing cover. Requires at least
// editor on the page's space.
func (a coversAPI) uploadCoverHandler(w http.ResponseWriter, r *http.Request) {
	page, ok := a.authorizePage(w, r, r.PathValue("pageID"), spaces.RoleEditor)
	if !ok {
		return
	}

	if err := r.ParseMultipartForm(assets.MaxCoverSize); err != nil {
		http.Error(w, "invalid or oversized upload", http.StatusBadRequest)
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		http.Error(w, `expected a "file" field`, http.StatusBadRequest)
		return
	}
	defer file.Close()

	// The bytes decide the type, not the client's label (same as AddImage).
	head := make([]byte, 512)
	n, _ := file.ReadAt(head, 0)
	contentType := http.DetectContentType(head[:n])
	if !assets.IsAllowedMimeType(contentType) {
		http.Error(w, "unsupported file type -- PNG, JPEG, WEBP, or GIF only", http.StatusBadRequest)
		return
	}

	userID := auth.UserIDFromContext(r.Context())
	cover, err := a.assets.SetCover(r.Context(), page.ID, file, header.Filename, contentType, userID)
	if err != nil {
		internalError(w, err)
		return
	}

	httpx.WriteJSON(w, http.StatusOK, toCoverResponse(cover))
}

// getCoverMetaHandler handles GET /api/pages/{pageID}/cover/meta
// (RequireAuth-protected): the cover's metadata as JSON, notably
// position_y -- PageCover.tsx needs this to render the image with the
// right object-position, which the raw-bytes GET /cover route below
// can't carry (an <img> tag has no way to read response headers, and
// this is cleaner than a header anyway). Also doubles as "does this
// page have a cover at all" -- 404 if not. Requires at least viewer on
// the page's space.
func (a coversAPI) getCoverMetaHandler(w http.ResponseWriter, r *http.Request) {
	page, ok := a.authorizePage(w, r, r.PathValue("pageID"), spaces.RoleViewer)
	if !ok {
		return
	}
	cover, err := a.assets.GetCover(r.Context(), page.ID)
	if errors.Is(err, db.ErrNotFound) {
		// No cover is the normal case, not an error: 204 keeps it out of
		// the browser console (a 404 is logged there as a failed request
		// on every page without a cover).
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if err != nil {
		internalError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, toCoverResponse(cover))
}

// updateCoverPositionHandler handles PUT
// /api/pages/{pageID}/cover/position (RequireAuth-protected). Body:
// {"position_y": 0-100}. Requires at least editor on the page's space.
func (a coversAPI) updateCoverPositionHandler(w http.ResponseWriter, r *http.Request) {
	page, ok := a.authorizePage(w, r, r.PathValue("pageID"), spaces.RoleEditor)
	if !ok {
		return
	}

	var req struct {
		PositionY float64 `json:"position_y"`
	}
	if !httpx.DecodeJSONBody(w, r, &req) {
		return
	}
	if req.PositionY < 0 || req.PositionY > 100 {
		http.Error(w, "position_y must be between 0 and 100", http.StatusBadRequest)
		return
	}

	cover, err := a.assets.SetPosition(r.Context(), page.ID, req.PositionY)
	if errors.Is(err, db.ErrNotFound) {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	if err != nil {
		internalError(w, err)
		return
	}

	httpx.WriteJSON(w, http.StatusOK, toCoverResponse(cover))
}

// getCoverHandler handles GET /api/pages/{pageID}/cover
// (RequireAuth-protected): the raw image bytes. Requires at least
// viewer on the page's space; 404 if the page has no cover set. No
// explicit Cache-Control -- the URL is stable per page (not
// content-hashed like the frontend's own build assets), so caching it
// aggressively would risk showing a stale image after a replace.
func (a coversAPI) getCoverHandler(w http.ResponseWriter, r *http.Request) {
	page, ok := a.authorizePage(w, r, r.PathValue("pageID"), spaces.RoleViewer)
	if !ok {
		return
	}

	f, cover, err := a.assets.Open(r.Context(), page.ID)
	if errors.Is(err, db.ErrNotFound) {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	if err != nil {
		internalError(w, err)
		return
	}
	defer f.Close()

	w.Header().Set("Content-Type", cover.MimeType)
	w.Header().Set("Content-Length", strconv.FormatInt(cover.SizeBytes, 10))
	io.Copy(w, f)
}

// deleteCoverHandler handles DELETE /api/pages/{pageID}/cover
// (RequireAuth-protected). Removing a cover that's already gone still
// succeeds (204) -- the end state either way is "no cover", the usual
// idempotent-DELETE convention. Requires at least editor on the page's
// space.
func (a coversAPI) deleteCoverHandler(w http.ResponseWriter, r *http.Request) {
	page, ok := a.authorizePage(w, r, r.PathValue("pageID"), spaces.RoleEditor)
	if !ok {
		return
	}
	if err := a.assets.DeleteCover(r.Context(), page.ID); err != nil && !errors.Is(err, db.ErrNotFound) {
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
