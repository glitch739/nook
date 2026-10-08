// Package assets stores page cover images on local disk -- see
// docs/adr/0004-database-pages.md's "deliberately last" scoping note.
// Narrow on purpose: one cover image per page, image MIME types only, a
// modest size cap. The general multi-file "Attachments" feature
// (ROADMAP Phase 5) can grow this same on-disk layout later rather than
// needing a separate one built from scratch.
package assets

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"nook/internal/db"
)

// MaxCoverSize bounds an uploaded cover image -- generous for a real
// photo, nowhere near "someone can exhaust disk with it" given it's one
// file per page, not an open-ended attachment list.
const MaxCoverSize = 8 << 20 // 8 MiB

var allowedMimeTypes = map[string]bool{
	"image/png":  true,
	"image/jpeg": true,
	"image/webp": true,
	"image/gif":  true,
}

// IsAllowedMimeType reports whether contentType is one of the image
// types a cover image may be uploaded as.
func IsAllowedMimeType(contentType string) bool {
	return allowedMimeTypes[contentType]
}

type Cover struct {
	PageID     string
	ID         string
	Filename   string
	MimeType   string
	SizeBytes  int64
	UploadedBy string
	// PositionY is which vertical slice of the image shows through its
	// fixed-height crop window -- 0-100, the frontend renders it as
	// object-position: center <value>%. 50 (centered) is the default,
	// both for a freshly uploaded cover and for anything uploaded before
	// this field existed.
	PositionY float64
	CreatedAt string
}

type Store struct {
	db  *sql.DB
	dir string
}

// NewStore creates the upload directory (if it doesn't already exist)
// and returns a Store rooted there.
func NewStore(conn *sql.DB, dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("assets: create uploads dir: %w", err)
	}
	return &Store{db: conn, dir: dir}, nil
}

func (s *Store) filePath(assetID string) string {
	return filepath.Join(s.dir, assetID)
}

// SetCover replaces pageID's cover image (there's at most one), both on
// disk and in the database. Order matters for crash-safety: the new
// file is written and only then is the DB row updated, and the *old*
// file (if any) is only removed after that DB write succeeds -- so a
// crash mid-upload never leaves the page's cover metadata pointing at a
// file that doesn't exist, and a failed DB write cleans up the new file
// it can no longer reach instead of leaking it.
func (s *Store) SetCover(
	ctx context.Context,
	pageID string,
	r io.Reader,
	filename, mimeType, uploadedBy string,
) (Cover, error) {
	newID := db.NewID()
	dest := s.filePath(newID)

	f, err := os.Create(dest)
	if err != nil {
		return Cover{}, fmt.Errorf("assets: create file: %w", err)
	}
	written, copyErr := io.Copy(f, r)
	closeErr := f.Close()
	if copyErr != nil {
		os.Remove(dest)
		return Cover{}, fmt.Errorf("assets: write file: %w", copyErr)
	}
	if closeErr != nil {
		os.Remove(dest)
		return Cover{}, fmt.Errorf("assets: close file: %w", closeErr)
	}

	old, err := s.GetCover(ctx, pageID)
	hadOld := err == nil
	if err != nil && !errors.Is(err, db.ErrNotFound) {
		os.Remove(dest)
		return Cover{}, fmt.Errorf("assets: check existing cover: %w", err)
	}

	_, err = s.db.ExecContext(ctx, `
		INSERT INTO page_covers (page_id, id, filename, mime_type, size_bytes, uploaded_by)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT (page_id) DO UPDATE SET
			id = excluded.id, filename = excluded.filename, mime_type = excluded.mime_type,
			size_bytes = excluded.size_bytes, uploaded_by = excluded.uploaded_by,
			position_y = 50,
			created_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now');
	`, pageID, newID, filename, mimeType, written, uploadedBy)
	if err != nil {
		os.Remove(dest)
		return Cover{}, fmt.Errorf("assets: save cover metadata: %w", err)
	}

	if hadOld {
		os.Remove(s.filePath(old.ID))
	}

	return s.GetCover(ctx, pageID)
}

const selectCoverByPageID = `
	SELECT page_id, id, filename, mime_type, size_bytes, uploaded_by, position_y, created_at
	FROM page_covers WHERE page_id = ?;
`

// GetCover looks up pageID's cover metadata. Returns an error wrapping
// db.ErrNotFound if the page has no cover set.
func (s *Store) GetCover(ctx context.Context, pageID string) (Cover, error) {
	var c Cover
	err := s.db.QueryRowContext(ctx, selectCoverByPageID, pageID).Scan(
		&c.PageID, &c.ID, &c.Filename, &c.MimeType, &c.SizeBytes, &c.UploadedBy, &c.PositionY, &c.CreatedAt,
	)
	if err == sql.ErrNoRows {
		return Cover{}, fmt.Errorf("assets: %w", db.ErrNotFound)
	}
	if err != nil {
		return Cover{}, err
	}
	return c, nil
}

// SetPosition updates pageID's cover crop position (see Cover.PositionY's
// own comment) without touching the image file itself. Returns an error
// wrapping db.ErrNotFound if the page has no cover set.
func (s *Store) SetPosition(ctx context.Context, pageID string, positionY float64) (Cover, error) {
	res, err := s.db.ExecContext(ctx, `
		UPDATE page_covers SET position_y = ? WHERE page_id = ?;
	`, positionY, pageID)
	if err != nil {
		return Cover{}, fmt.Errorf("assets: set position: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return Cover{}, fmt.Errorf("assets: set position: %w", db.ErrNotFound)
	}
	return s.GetCover(ctx, pageID)
}

// Open returns a readable handle to pageID's cover image file, plus its
// metadata. The caller must Close it.
func (s *Store) Open(ctx context.Context, pageID string) (io.ReadCloser, Cover, error) {
	cover, err := s.GetCover(ctx, pageID)
	if err != nil {
		return nil, Cover{}, err
	}
	f, err := os.Open(s.filePath(cover.ID))
	if err != nil {
		return nil, Cover{}, fmt.Errorf("assets: open file: %w", err)
	}
	return f, cover, nil
}

// DeleteCover removes pageID's cover, both the DB row and the file on
// disk. Returns an error wrapping db.ErrNotFound if the page had no
// cover set.
func (s *Store) DeleteCover(ctx context.Context, pageID string) error {
	cover, err := s.GetCover(ctx, pageID)
	if err != nil {
		return err
	}
	if _, err := s.db.ExecContext(ctx, `DELETE FROM page_covers WHERE page_id = ?;`, pageID); err != nil {
		return fmt.Errorf("assets: delete cover metadata: %w", err)
	}
	os.Remove(s.filePath(cover.ID))
	return nil
}
