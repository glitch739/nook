package assets

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode"

	"nook/internal/db"
)

// MaxAttachmentSize bounds one attached file.
const MaxAttachmentSize = 25 << 20 // 25 MiB

var (
	// ErrUnsupportedAttachment: the file's extension isn't on the list.
	ErrUnsupportedAttachment = errors.New("assets: unsupported attachment type")
	// ErrAttachmentTooLarge: over MaxAttachmentSize.
	ErrAttachmentTooLarge = errors.New("assets: attachment too large")
)

// attachmentTypes maps the allowed extensions to the Content-Type stored
// with the file. An allow-list on purpose: documents, spreadsheets,
// slides, plain data, archives and images. No executables, scripts,
// HTML or SVG -- those are served as downloads anyway, but there is no
// reason to accept them in a notes app.
var attachmentTypes = map[string]string{
	".pdf":  "application/pdf",
	".doc":  "application/msword",
	".docx": "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
	".xls":  "application/vnd.ms-excel",
	".xlsx": "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
	".ppt":  "application/vnd.ms-powerpoint",
	".pptx": "application/vnd.openxmlformats-officedocument.presentationml.presentation",
	".odt":  "application/vnd.oasis.opendocument.text",
	".ods":  "application/vnd.oasis.opendocument.spreadsheet",
	".odp":  "application/vnd.oasis.opendocument.presentation",
	".rtf":  "application/rtf",
	// E-mails: read in the browser through mailview (sanitized), and
	// their text is searchable (internal/extract).
	".eml":  "message/rfc822",
	".msg":  "application/vnd.ms-outlook",
	".txt":  "text/plain",
	".md":   "text/markdown",
	".csv":  "text/csv",
	".json": "application/json",
	".zip":  "application/zip",
	".7z":   "application/x-7z-compressed",
	".png":  "image/png",
	".jpg":  "image/jpeg",
	".jpeg": "image/jpeg",
	".gif":  "image/gif",
	".webp": "image/webp",
	".heic": "image/heic",
	".mp3":  "audio/mpeg",
	".m4a":  "audio/mp4",
	".mp4":  "video/mp4",
	".mov":  "video/quicktime",
}

// AttachmentType returns the stored Content-Type for filename, or false
// if the extension isn't allowed.
func AttachmentType(filename string) (string, bool) {
	t, ok := attachmentTypes[strings.ToLower(filepath.Ext(filename))]
	return t, ok
}

// CleanFilename keeps the user's name for the download but drops path
// parts and control characters, and caps the length.
func CleanFilename(name string) string {
	name = filepath.Base(strings.ReplaceAll(name, "\\", "/"))
	name = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || r == '"' {
			return -1
		}
		return r
	}, name)
	name = strings.TrimSpace(name)
	if name == "" || name == "." || name == "/" {
		name = "file"
	}
	if len(name) > 200 {
		ext := filepath.Ext(name)
		if len(ext) > 20 {
			ext = ""
		}
		name = name[:200-len(ext)] + ext
	}
	return name
}

type Attachment struct {
	ID        string
	PageID    string
	Filename  string
	MimeType  string
	SizeBytes int64
}

// AddAttachment stores one file for pageID. File first, row second, as
// with images: a crash in between leaves at worst an unreferenced file
// (removed by the uploads sweep), never a row pointing at nothing.
func (s *Store) AddAttachment(ctx context.Context, pageID string, r io.Reader, filename, uploadedBy string) (Attachment, error) {
	filename = CleanFilename(filename)
	mimeType, ok := AttachmentType(filename)
	if !ok {
		return Attachment{}, ErrUnsupportedAttachment
	}
	id := db.NewID()
	dest := s.filePath(id)
	f, err := os.Create(dest)
	if err != nil {
		return Attachment{}, fmt.Errorf("assets: create file: %w", err)
	}
	written, copyErr := io.Copy(f, io.LimitReader(r, MaxAttachmentSize+1))
	closeErr := f.Close()
	switch {
	case copyErr != nil:
		os.Remove(dest)
		return Attachment{}, fmt.Errorf("assets: write file: %w", copyErr)
	case closeErr != nil:
		os.Remove(dest)
		return Attachment{}, fmt.Errorf("assets: close file: %w", closeErr)
	case written > MaxAttachmentSize:
		os.Remove(dest)
		return Attachment{}, ErrAttachmentTooLarge
	}
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO page_attachments (id, page_id, filename, mime_type, size_bytes, uploaded_by)
		VALUES (?, ?, ?, ?, ?, ?);`, id, pageID, filename, mimeType, written, uploadedBy)
	if err != nil {
		os.Remove(dest)
		return Attachment{}, fmt.Errorf("assets: save attachment metadata: %w", err)
	}
	return Attachment{ID: id, PageID: pageID, Filename: filename, MimeType: mimeType, SizeBytes: written}, nil
}

// OpenAttachment is scoped to pageID like OpenImage: an id from another
// page looks exactly like a missing one.
func (s *Store) OpenAttachment(ctx context.Context, pageID, id string) (*os.File, Attachment, error) {
	var a Attachment
	err := s.db.QueryRowContext(ctx, `
		SELECT id, page_id, filename, mime_type, size_bytes FROM page_attachments WHERE id = ? AND page_id = ?;`,
		id, pageID).Scan(&a.ID, &a.PageID, &a.Filename, &a.MimeType, &a.SizeBytes)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, Attachment{}, fmt.Errorf("assets: attachment: %w", db.ErrNotFound)
	}
	if err != nil {
		return nil, Attachment{}, fmt.Errorf("assets: attachment: %w", err)
	}
	f, err := os.Open(s.filePath(a.ID))
	if errors.Is(err, os.ErrNotExist) {
		return nil, Attachment{}, fmt.Errorf("assets: attachment file: %w", db.ErrNotFound)
	}
	if err != nil {
		return nil, Attachment{}, fmt.Errorf("assets: open attachment: %w", err)
	}
	return f, a, nil
}
