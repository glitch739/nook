package assets

import (
	"bufio"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"

	"nook/internal/db"
)

// MaxImageSize bounds one image pasted into a page's text. The frontend
// already downscales and re-encodes large screenshots before upload
// (see frontend/src/imageUpload.ts), so real uploads land far below
// this; the cap is what a client that skips that step can send.
const MaxImageSize = 10 << 20 // 10 MiB

// ErrUnsupportedImage means the uploaded bytes are not one of the
// allowed image formats, whatever the client claimed they were.
var ErrUnsupportedImage = errors.New("assets: unsupported image type")

// ErrImageTooLarge means the upload exceeded MaxImageSize.
var ErrImageTooLarge = errors.New("assets: image too large")

type Image struct {
	ID         string
	PageID     string
	Filename   string
	MimeType   string
	SizeBytes  int64
	UploadedBy string
	CreatedAt  string
}

// AddImage stores a new image for pageID. The MIME type is sniffed from
// the bytes themselves rather than taken from the client, so a file
// that merely claims to be image/png can never be served back under an
// image type it isn't. The file is written first and the row inserted
// second, so a crash in between leaves at worst an unreferenced file,
// never a row pointing at nothing.
func (s *Store) AddImage(ctx context.Context, pageID string, r io.Reader, filename, uploadedBy string) (Image, error) {
	br := bufio.NewReaderSize(r, 512)
	head, err := br.Peek(512)
	if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, bufio.ErrBufferFull) {
		return Image{}, fmt.Errorf("assets: read image: %w", err)
	}
	mimeType := http.DetectContentType(head)
	if !IsAllowedMimeType(mimeType) {
		return Image{}, ErrUnsupportedImage
	}

	id := db.NewID()
	dest := s.filePath(id)
	f, err := os.Create(dest)
	if err != nil {
		return Image{}, fmt.Errorf("assets: create file: %w", err)
	}
	// One byte past the cap tells "exactly at the limit" apart from
	// "over it" without trusting any client-supplied length.
	written, copyErr := io.Copy(f, io.LimitReader(br, MaxImageSize+1))
	closeErr := f.Close()
	switch {
	case copyErr != nil:
		os.Remove(dest)
		return Image{}, fmt.Errorf("assets: write file: %w", copyErr)
	case closeErr != nil:
		os.Remove(dest)
		return Image{}, fmt.Errorf("assets: close file: %w", closeErr)
	case written > MaxImageSize:
		os.Remove(dest)
		return Image{}, ErrImageTooLarge
	}

	_, err = s.db.ExecContext(ctx, `
		INSERT INTO page_images (id, page_id, filename, mime_type, size_bytes, uploaded_by)
		VALUES (?, ?, ?, ?, ?, ?);
	`, id, pageID, filename, mimeType, written, uploadedBy)
	if err != nil {
		os.Remove(dest)
		return Image{}, fmt.Errorf("assets: save image metadata: %w", err)
	}
	return s.GetImage(ctx, pageID, id)
}

// GetImage looks up one image's metadata. It is scoped to pageID on
// purpose: the caller authorized access to that page, so an image id
// belonging to some other page must look exactly like a missing one.
func (s *Store) GetImage(ctx context.Context, pageID, imageID string) (Image, error) {
	var img Image
	err := s.db.QueryRowContext(ctx, `
		SELECT id, page_id, filename, mime_type, size_bytes, uploaded_by, created_at
		FROM page_images WHERE id = ? AND page_id = ?;
	`, imageID, pageID).Scan(&img.ID, &img.PageID, &img.Filename, &img.MimeType, &img.SizeBytes, &img.UploadedBy, &img.CreatedAt)
	if err == sql.ErrNoRows {
		return Image{}, fmt.Errorf("assets: %w", db.ErrNotFound)
	}
	if err != nil {
		return Image{}, err
	}
	return img, nil
}

// OpenImage returns a readable handle to one of pageID's images plus
// its metadata. The caller must Close it.
func (s *Store) OpenImage(ctx context.Context, pageID, imageID string) (*os.File, Image, error) {
	img, err := s.GetImage(ctx, pageID, imageID)
	if err != nil {
		return nil, Image{}, err
	}
	f, err := os.Open(s.filePath(img.ID))
	if err != nil {
		return nil, Image{}, fmt.Errorf("assets: open file: %w", err)
	}
	return f, img, nil
}
