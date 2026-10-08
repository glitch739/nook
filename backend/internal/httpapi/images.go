package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"nook/internal/assets"
	"nook/internal/auth"
	"nook/internal/db"
	"nook/internal/spaces"
)

// imagesAPI serves images placed inside a page's text. Authorization
// reuses coversAPI's page check: both are "files that belong to one
// page", readable by its viewers and addable by its editors.
type imagesAPI struct {
	covers coversAPI
}

type imageResponse struct {
	ID        string `json:"id"`
	URL       string `json:"url"`
	MimeType  string `json:"mime_type"`
	SizeBytes int64  `json:"size_bytes"`
}

// uploadImageHandler handles POST /api/pages/{pageID}/images
// (RequireAuth-protected). multipart/form-data with one file field
// named "file". The part is streamed straight to disk rather than
// parsed into memory or a temp file first; the type is sniffed from
// the bytes (PNG, JPEG, WEBP, GIF only). Requires at least editor on
// the page's space.
func (a imagesAPI) uploadImageHandler(w http.ResponseWriter, r *http.Request) {
	page, ok := a.covers.authorizePage(w, r, r.PathValue("pageID"), spaces.RoleEditor)
	if !ok {
		return
	}

	mr, err := r.MultipartReader()
	if err != nil {
		http.Error(w, "expected multipart/form-data", http.StatusBadRequest)
		return
	}
	for {
		part, err := mr.NextPart()
		if errors.Is(err, io.EOF) {
			http.Error(w, `expected a "file" field`, http.StatusBadRequest)
			return
		}
		if err != nil {
			http.Error(w, "invalid or oversized upload", http.StatusBadRequest)
			return
		}
		if part.FormName() != "file" {
			part.Close()
			continue
		}

		filename := part.FileName()
		if len(filename) > 255 {
			filename = filename[:255]
		}
		userID := auth.UserIDFromContext(r.Context())
		img, err := a.covers.assets.AddImage(r.Context(), page.ID, part, filename, userID)
		part.Close()
		switch {
		case errors.Is(err, assets.ErrUnsupportedImage):
			http.Error(w, "unsupported file type -- PNG, JPEG, WEBP, or GIF only", http.StatusBadRequest)
			return
		case errors.Is(err, assets.ErrImageTooLarge):
			http.Error(w, "image too large", http.StatusRequestEntityTooLarge)
			return
		case err != nil:
			// A body over the route's maxBody limit surfaces here as a
			// read error from inside the copy.
			var tooBig *http.MaxBytesError
			if errors.As(err, &tooBig) {
				http.Error(w, "image too large", http.StatusRequestEntityTooLarge)
				return
			}
			http.Error(w, "internal server error", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(imageResponse{
			ID:        img.ID,
			URL:       "/api/pages/" + page.ID + "/images/" + img.ID,
			MimeType:  img.MimeType,
			SizeBytes: img.SizeBytes,
		})
		return
	}
}

// getImageHandler handles GET /api/pages/{pageID}/images/{imageID}
// (RequireAuth-protected): the raw image bytes. Requires at least
// viewer on the page's space. An image id never changes content (a
// new paste is a new id), so unlike the cover route this one can be
// cached by the browser indefinitely -- "private" keeps shared proxies
// from holding on to a logged-in user's image.
func (a imagesAPI) getImageHandler(w http.ResponseWriter, r *http.Request) {
	page, ok := a.covers.authorizePage(w, r, r.PathValue("pageID"), spaces.RoleViewer)
	if !ok {
		return
	}

	f, img, err := a.covers.assets.OpenImage(r.Context(), page.ID, r.PathValue("imageID"))
	if errors.Is(err, db.ErrNotFound) {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	if err != nil {
		internalError(w, err)
		return
	}
	defer f.Close()

	w.Header().Set("Content-Type", img.MimeType)
	w.Header().Set("Cache-Control", "private, max-age=31536000, immutable")
	http.ServeContent(w, r, "", time.Time{}, f)
}
