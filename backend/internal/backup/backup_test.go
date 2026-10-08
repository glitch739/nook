package backup

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"nook/internal/db"
)

func TestCreateAndPrune(t *testing.T) {
	ctx := context.Background()
	work := t.TempDir()
	conn, err := db.Open(filepath.Join(work, "nook.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if err := db.Migrate(ctx, conn); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.ExecContext(ctx, `CREATE TABLE marker (v TEXT); INSERT INTO marker VALUES ('hello');`); err != nil {
		t.Fatal(err)
	}
	uploads := filepath.Join(work, "uploads")
	if err := os.MkdirAll(filepath.Join(uploads, "covers"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(uploads, "covers", "a.png"), []byte("png-bytes"), 0o600); err != nil {
		t.Fatal(err)
	}

	out := filepath.Join(work, "backups")
	start := time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)
	var last string
	for i := 0; i < 3; i++ {
		last, err = Create(ctx, conn, uploads, out, start.Add(time.Duration(i)*time.Minute))
		if err != nil {
			t.Fatalf("Create: %v", err)
		}
	}
	if info, err := os.Stat(last); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("backup file mode: %v %v", info, err)
	}

	// The archive holds a working database copy and the uploads.
	files := map[string][]byte{}
	f, _ := os.Open(last)
	gz, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		b, _ := io.ReadAll(tr)
		files[h.Name] = b
	}
	f.Close()
	if string(files["uploads/covers/a.png"]) != "png-bytes" {
		t.Fatalf("uploads missing from archive: %v", keys(files))
	}
	restored := filepath.Join(work, "restored.db")
	if err := os.WriteFile(restored, files["nook.db"], 0o600); err != nil {
		t.Fatal(err)
	}
	rc, err := db.Open(restored)
	if err != nil {
		t.Fatal(err)
	}
	var v string
	if err := rc.QueryRowContext(ctx, `SELECT v FROM marker;`).Scan(&v); err != nil || v != "hello" {
		t.Fatalf("restored db: v=%q err=%v", v, err)
	}
	rc.Close()

	removed, err := Prune(out, 2)
	want := "nook-backup-" + start.Local().Format("20060102-150405") + ".tar.gz"
	if err != nil || len(removed) != 1 || filepath.Base(removed[0]) != want {
		t.Fatalf("Prune removed %v, err %v", removed, err)
	}
	left, _ := os.ReadDir(out)
	if len(left) != 2 {
		t.Fatalf("%d entries left after prune (temp dirs must be cleaned up too)", len(left))
	}
}

func keys(m map[string][]byte) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	return out
}
