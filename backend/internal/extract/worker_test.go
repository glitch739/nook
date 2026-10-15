package extract

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"nook/internal/assets"
	"nook/internal/dbtest"
	"nook/internal/groups"
	"nook/internal/pages"
	"nook/internal/spaces"
	"nook/internal/users"
)

type fakeExtractor struct {
	res   Result
	err   error
	calls int
}

func (f *fakeExtractor) Extract(_ context.Context, r io.Reader, _ string) (Result, error) {
	f.calls++
	io.Copy(io.Discard, r)
	return f.res, f.err
}

// setupFile returns a migrated DB holding one uploaded attachment.
func setupFile(t *testing.T) (*sql.DB, *assets.Store, string) {
	t.Helper()
	conn := dbtest.New(t)
	ctx := context.Background()
	u, err := users.NewStore(conn).Create(ctx, "alice", "hash", "Alice")
	if err != nil {
		t.Fatal(err)
	}
	g, err := groups.NewStore(conn).Create(ctx, "Csoport", u.ID)
	if err != nil {
		t.Fatal(err)
	}
	sp, err := spaces.NewStore(conn).Create(ctx, "Tér", spaces.TypePrivate, u.ID, g.ID)
	if err != nil {
		t.Fatal(err)
	}
	p, err := pages.NewStore(conn).Create(ctx, sp.ID, nil, "Oldal", u.ID, pages.KindDocument)
	if err != nil {
		t.Fatal(err)
	}
	store, err := assets.NewStore(conn, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	f, err := store.AddAttachment(ctx, p.ID, strings.NewReader("%PDF-1.4 fake"), "kezikonyv.pdf", u.ID)
	if err != nil {
		t.Fatal(err)
	}
	return conn, store, f.ID
}

func newTestWorker(conn *sql.DB, files FileOpener, ex Extractor) *Worker {
	return NewWorker(conn, files, ex, slog.New(slog.NewTextHandler(io.Discard, nil)))
}

func status(t *testing.T, conn *sql.DB, fileID string) (st, msg string, attempts int) {
	t.Helper()
	if err := conn.QueryRow(`SELECT status, error, attempts FROM file_extractions WHERE file_id = ?`, fileID).
		Scan(&st, &msg, &attempts); err != nil {
		t.Fatal(err)
	}
	return
}

func chunks(t *testing.T, conn *sql.DB, fileID string) []string {
	t.Helper()
	rows, err := conn.Query(`SELECT COALESCE(page_no, 0), body FROM file_text WHERE file_id = ? ORDER BY chunk_id`, fileID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var n int
		var b string
		rows.Scan(&n, &b)
		out = append(out, strings.Repeat("p", n)+":"+b)
	}
	return out
}

func TestWorker_UploadIsQueuedAndNameIndexed(t *testing.T) {
	conn, _, fileID := setupFile(t)
	if st, _, _ := status(t, conn, fileID); st != StatusPending {
		t.Fatalf("status after upload = %q, want pending", st)
	}
	if got := chunks(t, conn, fileID); len(got) != 1 || got[0] != ":" {
		t.Fatalf("chunks after upload = %q, want one empty (name-only) chunk", got)
	}
}

func TestWorker_StoresChunksAndReplacesNameChunk(t *testing.T) {
	conn, store, fileID := setupFile(t)
	ex := &fakeExtractor{res: Result{UsedOCR: true, Chunks: []Chunk{
		{PageNo: 1, Text: "első oldal"}, {PageNo: 2, OCR: true, Text: "második oldal"},
	}}}
	w := newTestWorker(conn, store, ex)

	worked, err := w.ProcessNext(context.Background())
	if err != nil || !worked {
		t.Fatalf("ProcessNext = %v, %v", worked, err)
	}
	if st, _, _ := status(t, conn, fileID); st != StatusDone {
		t.Fatalf("status = %q, want done", st)
	}
	if got := chunks(t, conn, fileID); strings.Join(got, "|") != "p:első oldal|pp:második oldal" {
		t.Fatalf("chunks = %q", got)
	}
	var ocr bool
	conn.QueryRow(`SELECT used_ocr FROM file_extractions WHERE file_id = ?`, fileID).Scan(&ocr)
	if !ocr {
		t.Error("used_ocr not stored")
	}
	// The FTS index follows file_text (triggers).
	var n int
	conn.QueryRow(`SELECT count(*) FROM file_search_index WHERE file_search_index MATCH '"masodik"*'`).Scan(&n)
	if n != 1 {
		t.Errorf("FTS hits for masodik = %d, want 1", n)
	}

	if worked, _ := w.ProcessNext(context.Background()); worked {
		t.Error("queue should be empty")
	}
}

func TestWorker_TransientErrorIsRetriedLater(t *testing.T) {
	conn, store, fileID := setupFile(t)
	ex := &fakeExtractor{err: errors.New("connection refused")}
	w := newTestWorker(conn, store, ex)

	if _, err := w.ProcessNext(context.Background()); err != nil {
		t.Fatal(err)
	}
	st, msg, attempts := status(t, conn, fileID)
	if st != StatusPending || attempts != 1 || !strings.Contains(msg, "connection refused") {
		t.Fatalf("after failure: %q %q %d, want pending, with error, 1 attempt", st, msg, attempts)
	}
	// Not due yet: the retry waits.
	if worked, _ := w.ProcessNext(context.Background()); worked {
		t.Fatal("retry ran before its not_before")
	}

	// Past every retry: failed for good.
	conn.Exec(`UPDATE file_extractions SET not_before = '', attempts = ? WHERE file_id = ?`, len(retryDelays), fileID)
	w.ProcessNext(context.Background())
	if st, _, _ := status(t, conn, fileID); st != StatusFailed {
		t.Fatalf("after last retry: %q, want failed", st)
	}
	// The name stays searchable.
	if got := chunks(t, conn, fileID); len(got) != 1 {
		t.Fatalf("chunks = %q, want the name chunk kept", got)
	}
}

func TestWorker_TimeoutFailsWithoutRetry(t *testing.T) {
	conn, store, fileID := setupFile(t)
	w := newTestWorker(conn, store, &fakeExtractor{err: fmt.Errorf("tika: %w", context.DeadlineExceeded)})
	w.ProcessNext(context.Background())
	if st, msg, _ := status(t, conn, fileID); st != StatusFailed || !strings.Contains(msg, "took longer") {
		t.Fatalf("status = %q %q, want failed / took longer", st, msg)
	}
}

func TestWorker_EncryptedIsSkippedWithoutRetry(t *testing.T) {
	conn, store, fileID := setupFile(t)
	ex := &fakeExtractor{err: ErrEncrypted}
	w := newTestWorker(conn, store, ex)
	w.ProcessNext(context.Background())
	if st, msg, _ := status(t, conn, fileID); st != StatusSkipped || msg != "password protected" {
		t.Fatalf("status = %q %q, want skipped / password protected", st, msg)
	}
}

func TestWorker_InterruptedFileIsResumed(t *testing.T) {
	conn, store, fileID := setupFile(t)
	conn.Exec(`UPDATE file_extractions SET status = 'processing' WHERE file_id = ?`, fileID)
	ex := &fakeExtractor{res: Result{Chunks: []Chunk{{Text: "kész"}}}}
	w := newTestWorker(conn, store, ex)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { w.Run(ctx); close(done) }()
	for i := 0; i < 200; i++ {
		if st, _, _ := status(t, conn, fileID); st == StatusDone {
			break
		}
		w.Wake()
		waitABit()
	}
	cancel()
	<-done
	if st, _, _ := status(t, conn, fileID); st != StatusDone {
		t.Fatalf("status = %q, want done after restart", st)
	}
}

func TestWorker_DeletedFileCascades(t *testing.T) {
	conn, _, fileID := setupFile(t)
	// Nook removes attachment rows only in the uploads sweep: same cascade.
	if _, err := conn.Exec(`DELETE FROM page_attachments WHERE id = ?`, fileID); err != nil {
		t.Fatal(err)
	}
	var n int
	conn.QueryRow(`SELECT (SELECT count(*) FROM file_text) + (SELECT count(*) FROM file_extractions) + (SELECT count(*) FROM file_search_index)`).Scan(&n)
	if n != 0 {
		t.Fatalf("%d rows left behind after delete", n)
	}
}

func waitABit() { time.Sleep(10 * time.Millisecond) }
