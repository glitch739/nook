package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"time"

	"nook/internal/assets"
	"nook/internal/auth"
	"nook/internal/db"
	"nook/internal/httpx"
	"nook/internal/spaces"
)

type attachmentsAPI struct {
	covers coversAPI
	// index wakes the text extraction worker after an upload (ADR 0009);
	// nil when there is none. docs converts documents for /view; nil: no
	// document preview (e-mails still preview).
	index interface{ Wake() }
	docs  DocConverter
}

type attachmentResponse struct {
	ID        string `json:"id"`
	URL       string `json:"url"`
	Filename  string `json:"filename"`
	MimeType  string `json:"mime_type"`
	SizeBytes int64  `json:"size_bytes"`
}

// POST /api/pages/{pageID}/attachments (editor+), multipart field "file".
func (a attachmentsAPI) uploadHandler(w http.ResponseWriter, r *http.Request) {
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
			var tooBig *http.MaxBytesError
			if errors.As(err, &tooBig) {
				http.Error(w, "file too large (25 MB max)", http.StatusRequestEntityTooLarge)
				return
			}
			http.Error(w, "invalid upload", http.StatusBadRequest)
			return
		}
		if part.FormName() != "file" {
			part.Close()
			continue
		}
		att, err := a.covers.assets.AddAttachment(r.Context(), page.ID, part, part.FileName(), auth.UserIDFromContext(r.Context()))
		part.Close()
		var tooBig *http.MaxBytesError
		switch {
		case errors.Is(err, assets.ErrUnsupportedAttachment):
			http.Error(w, "this file type can't be attached (documents, spreadsheets, slides, text, archives, images, audio, video)", http.StatusBadRequest)
			return
		case errors.Is(err, assets.ErrAttachmentTooLarge), errors.As(err, &tooBig):
			http.Error(w, "file too large (25 MB max)", http.StatusRequestEntityTooLarge)
			return
		case err != nil:
			internalError(w, err)
			return
		}
		if a.index != nil {
			a.index.Wake()
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(attachmentResponse{
			ID: att.ID, URL: "/api/pages/" + page.ID + "/attachments/" + att.ID,
			Filename: att.Filename, MimeType: att.MimeType, SizeBytes: att.SizeBytes,
		})
		return
	}
}

// GET /api/pages/{pageID}/attachments/{attachmentID} (viewer+). A
// download, unless ?preview=1 asks for a type the browser shows safely by
// itself (assets.IsPreviewable: PDF, images, plain text), which is served
// inline. Sandboxed by CSP either way, except a PDF: Chrome refuses its
// PDF viewer under a sandbox CSP, and the viewer never runs the file's
// script on our origin anyway.
func (a attachmentsAPI) downloadHandler(w http.ResponseWriter, r *http.Request) {
	page, ok := a.covers.authorizePage(w, r, r.PathValue("pageID"), spaces.RoleViewer)
	if !ok {
		return
	}
	f, att, err := a.covers.assets.OpenAttachment(r.Context(), page.ID, r.PathValue("attachmentID"))
	if errors.Is(err, db.ErrNotFound) {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	if err != nil {
		internalError(w, err)
		return
	}
	defer f.Close()
	h := w.Header()
	disposition := "attachment"
	if r.URL.Query().Get("preview") == "1" && assets.IsPreviewable(att.MimeType) {
		disposition = "inline"
	}
	h.Set("Content-Type", att.MimeType)
	h.Set("Content-Disposition", mime.FormatMediaType(disposition, map[string]string{"filename": att.Filename}))
	h.Set("X-Content-Type-Options", "nosniff")
	if disposition != "inline" || att.MimeType != "application/pdf" {
		h.Set("Content-Security-Policy", "sandbox; default-src 'none'; img-src 'self'; style-src 'unsafe-inline'")
	}
	h.Set("Cache-Control", "private, max-age=31536000, immutable")
	http.ServeContent(w, r, "", time.Time{}, f)
}

// fileIndexResponse: how far an attachment is in being made searchable
// (internal/extract). status is pending, processing, done, failed or
// skipped; error says why for the last two.
type fileIndexResponse struct {
	Status  string `json:"status"`
	UsedOCR bool   `json:"used_ocr"`
	HasText bool   `json:"has_text"`
	Error   string `json:"error"`
}

// GET /api/pages/{pageID}/attachments/{attachmentID}/index (viewer+).
func (a attachmentsAPI) indexStatusHandler(w http.ResponseWriter, r *http.Request) {
	page, ok := a.covers.authorizePage(w, r, r.PathValue("pageID"), spaces.RoleViewer)
	if !ok {
		return
	}
	st, err := a.covers.assets.FileIndexStatus(r.Context(), page.ID, r.PathValue("attachmentID"))
	if errors.Is(err, db.ErrNotFound) {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	if err != nil {
		internalError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, fileIndexResponse{Status: st.Status, UsedOCR: st.UsedOCR, HasText: st.HasText, Error: st.Error})
}
