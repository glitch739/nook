package assets

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"

	"nook/internal/db"
	"nook/internal/pages"
)

// A real 1x1 PNG header is enough for http.DetectContentType.
var pngBytes = append([]byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR"), make([]byte, 64)...)

func TestAddImage_ThenOpen(t *testing.T) {
	_, page, store := setup(t)
	ctx := context.Background()

	img, err := store.AddImage(ctx, page.ID, bytes.NewReader(pngBytes), "shot.png", page.CreatedBy)
	if err != nil {
		t.Fatalf("AddImage: %v", err)
	}
	if img.MimeType != "image/png" || img.SizeBytes != int64(len(pngBytes)) {
		t.Fatalf("unexpected metadata: %+v", img)
	}

	f, got, err := store.OpenImage(ctx, page.ID, img.ID)
	if err != nil {
		t.Fatalf("OpenImage: %v", err)
	}
	defer f.Close()
	data, _ := io.ReadAll(f)
	if !bytes.Equal(data, pngBytes) || got.ID != img.ID {
		t.Fatal("stored bytes or id do not match")
	}
}

func TestAddImage_RejectsNonImage(t *testing.T) {
	_, page, store := setup(t)
	_, err := store.AddImage(context.Background(), page.ID, bytes.NewReader([]byte("<html><script>x</script>")), "a.png", page.CreatedBy)
	if !errors.Is(err, ErrUnsupportedImage) {
		t.Fatalf("want ErrUnsupportedImage, got %v", err)
	}
}

func TestAddImage_RejectsOversize(t *testing.T) {
	_, page, store := setup(t)
	big := append(append([]byte{}, pngBytes...), make([]byte, MaxImageSize)...)
	_, err := store.AddImage(context.Background(), page.ID, bytes.NewReader(big), "big.png", page.CreatedBy)
	if !errors.Is(err, ErrImageTooLarge) {
		t.Fatalf("want ErrImageTooLarge, got %v", err)
	}
}

func TestGetImage_ScopedToPage(t *testing.T) {
	conn, page, store := setup(t)
	ctx := context.Background()
	img, err := store.AddImage(ctx, page.ID, bytes.NewReader(pngBytes), "shot.png", page.CreatedBy)
	if err != nil {
		t.Fatalf("AddImage: %v", err)
	}
	other, err := pages.NewStore(conn).Create(ctx, page.SpaceID, nil, "Other", page.CreatedBy, pages.KindDocument)
	if err != nil {
		t.Fatalf("create page: %v", err)
	}
	if _, err := store.GetImage(ctx, other.ID, img.ID); !errors.Is(err, db.ErrNotFound) {
		t.Fatalf("image leaked across pages: %v", err)
	}
}
