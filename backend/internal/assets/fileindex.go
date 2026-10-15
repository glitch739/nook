package assets

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"

	"nook/internal/db"
)

// OpenFileReader opens one attachment for the text extraction worker
// (internal/extract, ADR 0009).
func (s *Store) OpenFileReader(ctx context.Context, pageID, fileID string) (io.ReadCloser, error) {
	f, _, err := s.OpenAttachment(ctx, pageID, fileID)
	return f, err
}

// IndexStatus is how far an attachment is in being made searchable.
type IndexStatus struct {
	Status  string // pending, processing, done, failed, skipped
	UsedOCR bool
	HasText bool // some text was found, not only the name
	Error   string
}

// FileIndexStatus reads an attachment's extraction state, scoped to
// pageID like OpenAttachment.
func (s *Store) FileIndexStatus(ctx context.Context, pageID, fileID string) (IndexStatus, error) {
	var st IndexStatus
	err := s.db.QueryRowContext(ctx, `
		SELECT e.status, e.used_ocr, e.error,
		       EXISTS (SELECT 1 FROM file_text t WHERE t.file_id = e.file_id AND t.body != '')
		FROM file_extractions AS e JOIN page_attachments AS a ON a.id = e.file_id
		WHERE e.file_id = ? AND a.page_id = ?;`, fileID, pageID).Scan(&st.Status, &st.UsedOCR, &st.Error, &st.HasText)
	if errors.Is(err, sql.ErrNoRows) {
		return IndexStatus{}, fmt.Errorf("assets: index status: %w", db.ErrNotFound)
	}
	return st, err
}

// IsPreviewable: types a browser shows safely by itself, served inline
// on ?preview=1 (PDF in its viewer, images, plain text). Everything else
// is always a download.
func IsPreviewable(mimeType string) bool {
	switch mimeType {
	case "application/pdf", "image/png", "image/jpeg", "image/gif", "image/webp",
		"text/plain", "text/markdown", "text/csv":
		return true
	}
	return false
}
