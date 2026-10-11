package assets_test

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"nook/internal/assets"
	"nook/internal/db"
	"nook/internal/dbtest"
	"nook/internal/groups"
	"nook/internal/pages"
	"nook/internal/spaces"
	"nook/internal/users"
)

func TestSweep(t *testing.T) {
	ctx := context.Background()
	conn := dbtest.New(t)
	dir := t.TempDir()
	as, err := assets.NewStore(conn, dir)
	if err != nil {
		t.Fatal(err)
	}
	u, _ := users.NewStore(conn).Create(ctx, "u", "h", "U")
	g, _ := groups.NewStore(conn).Create(ctx, "G", u.ID)
	sp, _ := spaces.NewStore(conn).Create(ctx, "S", spaces.TypePrivate, u.ID, g.ID)
	ps := pages.NewStore(conn)
	page, _ := ps.Create(ctx, sp.ID, nil, "P", u.ID, pages.KindDocument)

	png := append([]byte("\x89PNG\r\n\x1a\n"), make([]byte, 64)...)
	kept, _ := as.AddImage(ctx, page.ID, bytes.NewReader(png), "kept.png", u.ID)
	oldUnused, _ := as.AddImage(ctx, page.ID, bytes.NewReader(png), "old.png", u.ID)
	newUnused, _ := as.AddImage(ctx, page.ID, bytes.NewReader(png), "new.png", u.ID)
	// Uploaded on page, then cut and pasted into other: the link keeps page's ID.
	pasted, _ := as.AddImage(ctx, page.ID, bytes.NewReader(png), "pasted.png", u.ID)
	other, _ := ps.Create(ctx, sp.ID, nil, "Other", u.ID, pages.KindDocument)
	if _, err := conn.Exec(`UPDATE pages SET content_json = ? WHERE id = ?`,
		`{"type":"doc","content":[{"type":"image","attrs":{"src":"/api/pages/`+page.ID+`/images/`+pasted.ID+`"}}]}`, other.ID); err != nil {
		t.Fatal(err)
	}
	att, err := as.AddAttachment(ctx, page.ID, bytes.NewReader([]byte("%PDF")), "doc.pdf", u.ID)
	if err != nil {
		t.Fatal(err)
	}
	// The page's text references only "kept".
	if _, err := conn.Exec(`UPDATE pages SET content_json = ? WHERE id = ?`,
		`{"type":"doc","content":[{"type":"image","attrs":{"src":"/api/pages/`+page.ID+`/images/`+kept.ID+`"}}]}`, page.ID); err != nil {
		t.Fatal(err)
	}
	old := time.Now().UTC().Add(-30 * 24 * time.Hour).Format("2006-01-02T15:04:05.000Z")
	for _, id := range []string{kept.ID, oldUnused.ID, pasted.ID} {
		conn.Exec(`UPDATE page_images SET created_at = ? WHERE id = ?`, old, id)
	}
	conn.Exec(`UPDATE page_attachments SET created_at = ? WHERE id = ?`, old, att.ID)

	// Orphan files: one old, one fresh, plus a non-id file that must be ignored.
	oldOrphan, newOrphan := db.NewID(), db.NewID()
	for _, n := range []string{oldOrphan, newOrphan, "README.txt"} {
		os.WriteFile(filepath.Join(dir, n), []byte("x"), 0o600)
	}
	past := time.Now().Add(-48 * time.Hour)
	os.Chtimes(filepath.Join(dir, oldOrphan), past, past)
	os.Chtimes(filepath.Join(dir, "README.txt"), past, past)

	dry, err := as.Sweep(ctx, 7*24*time.Hour, 24*time.Hour, true)
	if err != nil || dry.Images != 1 || dry.Attachments != 1 || dry.OrphanFiles != 1 {
		t.Fatalf("dry run = %+v, %v", dry, err)
	}
	if _, err := os.Stat(filepath.Join(dir, oldUnused.ID)); err != nil {
		t.Fatal("dry run must not delete anything")
	}

	res, err := as.Sweep(ctx, 7*24*time.Hour, 24*time.Hour, false)
	if err != nil || res != dry {
		t.Fatalf("sweep = %+v (dry %+v), %v", res, dry, err)
	}
	exists := func(n string) bool { _, err := os.Stat(filepath.Join(dir, n)); return err == nil }
	for n, want := range map[string]bool{
		kept.ID: true, pasted.ID: true, newUnused.ID: true, newOrphan: true, "README.txt": true,
		oldUnused.ID: false, att.ID: false, oldOrphan: false,
	} {
		if exists(n) != want {
			t.Errorf("%s exists = %v, want %v", n, !want, want)
		}
	}
	var rows int
	conn.QueryRow(`SELECT COUNT(*) FROM page_images WHERE id = ?`, oldUnused.ID).Scan(&rows)
	if rows != 0 {
		t.Error("swept image row still there")
	}
}
