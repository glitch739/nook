package assets

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"nook/internal/db"
	"nook/internal/dbtest"
	"nook/internal/groups"
	"nook/internal/pages"
	"nook/internal/spaces"
	"nook/internal/users"
)

// setup creates a user, a space, a page, and a Store rooted at a fresh
// temp directory -- the fixture every test below starts from.
func setup(t *testing.T) (*sql.DB, pages.Page, *Store) {
	t.Helper()
	conn := dbtest.New(t)
	ctx := context.Background()

	user, err := users.NewStore(conn).Create(ctx, "alice", "hash", "Alice")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	group, err := groups.NewStore(conn).Create(ctx, "Alice's Group", user.ID)
	if err != nil {
		t.Fatalf("create group: %v", err)
	}
	space, err := spaces.NewStore(conn).Create(ctx, "Alice's Space", spaces.TypePrivate, user.ID, group.ID)
	if err != nil {
		t.Fatalf("create space: %v", err)
	}
	page, err := pages.NewStore(conn).Create(ctx, space.ID, nil, "Page", user.ID, pages.KindDocument)
	if err != nil {
		t.Fatalf("create page: %v", err)
	}

	store, err := NewStore(conn, t.TempDir())
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	return conn, page, store
}

func TestSetCover_ThenOpen(t *testing.T) {
	_, page, store := setup(t)
	ctx := context.Background()

	content := []byte("fake-png-bytes")
	cover, err := store.SetCover(ctx, page.ID, bytes.NewReader(content), "cover.png", "image/png", page.CreatedBy)
	if err != nil {
		t.Fatalf("SetCover: %v", err)
	}
	if cover.Filename != "cover.png" || cover.MimeType != "image/png" || cover.SizeBytes != int64(len(content)) {
		t.Fatalf("unexpected cover: %+v", cover)
	}
	if cover.PositionY != 50 {
		t.Fatalf("PositionY = %v, want the centered default 50", cover.PositionY)
	}

	r, meta, err := store.Open(ctx, page.ID)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer r.Close()
	got, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !bytes.Equal(got, content) {
		t.Fatalf("file content = %q, want %q", got, content)
	}
	if meta.ID != cover.ID {
		t.Fatalf("Open metadata ID = %q, want %q", meta.ID, cover.ID)
	}
}

func TestSetCover_ReplacesAndRemovesOldFile(t *testing.T) {
	_, page, store := setup(t)
	ctx := context.Background()

	first, err := store.SetCover(ctx, page.ID, bytes.NewReader([]byte("first")), "a.png", "image/png", page.CreatedBy)
	if err != nil {
		t.Fatalf("SetCover (first): %v", err)
	}
	firstPath := store.filePath(first.ID)
	if _, err := os.Stat(firstPath); err != nil {
		t.Fatalf("expected first file to exist: %v", err)
	}

	second, err := store.SetCover(ctx, page.ID, bytes.NewReader([]byte("second")), "b.png", "image/png", page.CreatedBy)
	if err != nil {
		t.Fatalf("SetCover (second): %v", err)
	}
	if second.ID == first.ID {
		t.Fatal("expected a fresh asset ID on replace")
	}
	if _, err := os.Stat(firstPath); !os.IsNotExist(err) {
		t.Fatalf("expected the old file to be removed, stat err = %v", err)
	}

	got, err := store.GetCover(ctx, page.ID)
	if err != nil {
		t.Fatalf("GetCover: %v", err)
	}
	if got.Filename != "b.png" {
		t.Fatalf("GetCover().Filename = %q, want %q (the replacement)", got.Filename, "b.png")
	}
}

func TestSetPosition(t *testing.T) {
	_, page, store := setup(t)
	ctx := context.Background()

	if _, err := store.SetCover(ctx, page.ID, bytes.NewReader([]byte("x")), "a.png", "image/png", page.CreatedBy); err != nil {
		t.Fatalf("SetCover: %v", err)
	}

	updated, err := store.SetPosition(ctx, page.ID, 12.5)
	if err != nil {
		t.Fatalf("SetPosition: %v", err)
	}
	if updated.PositionY != 12.5 {
		t.Fatalf("PositionY = %v, want 12.5", updated.PositionY)
	}

	got, err := store.GetCover(ctx, page.ID)
	if err != nil {
		t.Fatalf("GetCover: %v", err)
	}
	if got.PositionY != 12.5 {
		t.Fatalf("GetCover().PositionY = %v, want the position just set (12.5)", got.PositionY)
	}
}

func TestSetPosition_NoneSetIsNotFound(t *testing.T) {
	_, page, store := setup(t)
	_, err := store.SetPosition(context.Background(), page.ID, 30)
	if !errors.Is(err, db.ErrNotFound) {
		t.Fatalf("SetPosition(no cover) error = %v, want wrapping db.ErrNotFound", err)
	}
}

func TestSetCover_ReplaceResetsPosition(t *testing.T) {
	_, page, store := setup(t)
	ctx := context.Background()

	if _, err := store.SetCover(ctx, page.ID, bytes.NewReader([]byte("first")), "a.png", "image/png", page.CreatedBy); err != nil {
		t.Fatalf("SetCover (first): %v", err)
	}
	if _, err := store.SetPosition(ctx, page.ID, 90); err != nil {
		t.Fatalf("SetPosition: %v", err)
	}

	// A brand new image's "correct" crop is unknown -- replacing the
	// cover should reset to centered rather than keep the old image's
	// position.
	replaced, err := store.SetCover(ctx, page.ID, bytes.NewReader([]byte("second")), "b.png", "image/png", page.CreatedBy)
	if err != nil {
		t.Fatalf("SetCover (replace): %v", err)
	}
	if replaced.PositionY != 50 {
		t.Fatalf("PositionY after replace = %v, want reset to the centered default 50", replaced.PositionY)
	}
}

func TestGetCover_NoneSetIsNotFound(t *testing.T) {
	_, page, store := setup(t)
	_, err := store.GetCover(context.Background(), page.ID)
	if !errors.Is(err, db.ErrNotFound) {
		t.Fatalf("GetCover(no cover) error = %v, want wrapping db.ErrNotFound", err)
	}
}

func TestDeleteCover_RemovesRowAndFile(t *testing.T) {
	_, page, store := setup(t)
	ctx := context.Background()

	cover, err := store.SetCover(ctx, page.ID, bytes.NewReader([]byte("x")), "a.png", "image/png", page.CreatedBy)
	if err != nil {
		t.Fatalf("SetCover: %v", err)
	}
	path := store.filePath(cover.ID)

	if err := store.DeleteCover(ctx, page.ID); err != nil {
		t.Fatalf("DeleteCover: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("expected the file to be removed, stat err = %v", err)
	}
	if _, err := store.GetCover(ctx, page.ID); !errors.Is(err, db.ErrNotFound) {
		t.Fatalf("GetCover after delete error = %v, want wrapping db.ErrNotFound", err)
	}
}

func TestDeleteCover_NoneSetIsNotFound(t *testing.T) {
	_, page, store := setup(t)
	err := store.DeleteCover(context.Background(), page.ID)
	if !errors.Is(err, db.ErrNotFound) {
		t.Fatalf("DeleteCover(no cover) error = %v, want wrapping db.ErrNotFound", err)
	}
}

func TestNewStore_CreatesUploadsDir(t *testing.T) {
	conn := dbtest.New(t)
	dir := filepath.Join(t.TempDir(), "nested", "uploads")
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("expected dir not to exist yet, stat err = %v", err)
	}
	if _, err := NewStore(conn, dir); err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		t.Fatalf("expected NewStore to create the uploads dir, stat err = %v", err)
	}
}
