package extract

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"time"
	"unicode/utf8"
)

// Queue states (file_extractions.status, migration 0021).
const (
	StatusPending    = "pending"
	StatusProcessing = "processing"
	StatusDone       = "done"
	StatusFailed     = "failed"
	StatusSkipped    = "skipped"
)

// FileOpener opens a stored attachment; *assets.Store in production.
type FileOpener interface {
	OpenFileReader(ctx context.Context, pageID, fileID string) (io.ReadCloser, error)
}

// retryDelays: how long a file waits after its 1st, 2nd, 3rd failed
// attempt (Tika down, timed out). After the last one it is failed.
var retryDelays = []time.Duration{time.Minute, 10 * time.Minute, time.Hour}

// Worker drains the extraction queue one file at a time: OCR is heavy,
// and one at a time keeps it from competing with the app for the CPU.
// The Tika call runs outside any transaction -- Nook has a single
// SQLite connection (internal/db) and a minutes-long OCR must never hold it.
type Worker struct {
	DB        *sql.DB
	Files     FileOpener
	Extractor Extractor
	Logger    *slog.Logger
	// Timeout bounds one file end to end (upload to Tika + OCR).
	Timeout time.Duration
	// MaxTextBytes caps the text kept per file; the rest is dropped.
	MaxTextBytes int
	// PollInterval is how often an idle worker looks for work that
	// Wake did not announce (retries coming due).
	PollInterval time.Duration

	wake chan struct{}
}

func NewWorker(conn *sql.DB, files FileOpener, ex Extractor, logger *slog.Logger) *Worker {
	return &Worker{
		DB: conn, Files: files, Extractor: ex, Logger: logger,
		Timeout:      60 * time.Minute,
		MaxTextBytes: 5 << 20,
		PollInterval: 30 * time.Second,
		wake:         make(chan struct{}, 1),
	}
}

// Wake tells an idle worker there is new work (an upload). Never blocks.
func (w *Worker) Wake() {
	select {
	case w.wake <- struct{}{}:
	default:
	}
}

// Run processes the queue until ctx is done. A file left "processing"
// by a crash or restart is put back first.
func (w *Worker) Run(ctx context.Context) {
	if _, err := w.DB.ExecContext(ctx,
		`UPDATE file_extractions SET status = 'pending' WHERE status = 'processing';`); err != nil {
		w.Logger.Error("extract: reset interrupted files", "err", err)
	}
	ticker := time.NewTicker(w.PollInterval)
	defer ticker.Stop()
	for {
		worked, err := w.ProcessNext(ctx)
		if err != nil {
			w.Logger.Error("extract: queue", "err", err)
		}
		if worked {
			continue
		}
		select {
		case <-ctx.Done():
			return
		case <-w.wake:
		case <-ticker.C:
		}
	}
}

func now() string { return time.Now().UTC().Format("2006-01-02T15:04:05.000Z") }

// ProcessNext extracts the next due file, if any. Reports whether it
// found one. Exported for tests, which drive the queue step by step.
func (w *Worker) ProcessNext(ctx context.Context) (bool, error) {
	var fileID, pageID, filename string
	var attempts int
	err := w.DB.QueryRowContext(ctx, `
		UPDATE file_extractions
		SET status = 'processing', attempts = attempts + 1, updated_at = ?
		WHERE file_id = (
			SELECT file_id FROM file_extractions
			WHERE status = 'pending' AND not_before <= ?
			ORDER BY not_before LIMIT 1
		)
		RETURNING file_id, attempts,
			(SELECT page_id FROM page_attachments WHERE id = file_id),
			(SELECT filename FROM page_attachments WHERE id = file_id);
	`, now(), now()).Scan(&fileID, &attempts, &pageID, &filename)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("claim next file: %w", err)
	}

	res, exErr := w.extract(ctx, pageID, fileID, filename)
	switch {
	case exErr == nil:
		return true, w.store(ctx, fileID, res)
	case errors.Is(exErr, ErrEncrypted):
		return true, w.finish(ctx, fileID, StatusSkipped, "password protected")
	case errors.Is(exErr, ErrUnprocessable):
		return true, w.finish(ctx, fileID, StatusFailed, exErr.Error())
	case ctx.Err() != nil:
		// Shutting down: leave it for the next start.
		return true, nil
	case errors.Is(exErr, context.DeadlineExceeded):
		// The file itself is too much (a very long scan): another try
		// would only burn the same hour of CPU again.
		return true, w.finish(ctx, fileID, StatusFailed, fmt.Sprintf("took longer than %s to read", w.Timeout))
	case attempts > len(retryDelays):
		w.Logger.Warn("extract: giving up", "file_id", fileID, "err", exErr)
		return true, w.finish(ctx, fileID, StatusFailed, exErr.Error())
	default:
		w.Logger.Warn("extract: will retry", "file_id", fileID, "attempt", attempts, "err", exErr)
		_, err := w.DB.ExecContext(ctx, `
			UPDATE file_extractions SET status = 'pending', error = ?, not_before = ?, updated_at = ?
			WHERE file_id = ?;
		`, exErr.Error(), time.Now().UTC().Add(retryDelays[attempts-1]).Format("2006-01-02T15:04:05.000Z"), now(), fileID)
		return true, err
	}
}

func (w *Worker) extract(ctx context.Context, pageID, fileID, filename string) (Result, error) {
	ctx, cancel := context.WithTimeout(ctx, w.Timeout)
	defer cancel()
	f, err := w.Files.OpenFileReader(ctx, pageID, fileID)
	if err != nil {
		return Result{}, fmt.Errorf("%w: open file: %v", ErrUnprocessable, err)
	}
	defer f.Close()
	return w.Extractor.Extract(ctx, f, filename)
}

// store replaces the file's chunks with the extracted ones. A file
// that yields no text keeps one empty chunk, so it stays findable by
// name. A file deleted meanwhile simply fails the foreign key; fine.
func (w *Worker) store(ctx context.Context, fileID string, res Result) error {
	chunks := capText(res.Chunks, w.MaxTextBytes)
	status, note := StatusDone, res.Problem
	if len(chunks) == 0 && res.Problem != "" {
		status = StatusFailed
		if res.Problem == "password protected" {
			status = StatusSkipped
		}
	}

	tx, err := w.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM file_text WHERE file_id = ?;`, fileID); err != nil {
		return fmt.Errorf("clear chunks: %w", err)
	}
	if len(chunks) == 0 {
		chunks = []Chunk{{}}
	}
	for _, c := range chunks {
		var page any
		if c.PageNo > 0 {
			page = c.PageNo
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO file_text (file_id, page_no, part, ocr, body) VALUES (?, ?, ?, ?, ?);`,
			fileID, page, c.Part, c.OCR, c.Text); err != nil {
			return fmt.Errorf("insert chunk: %w", err)
		}
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE file_extractions SET status = ?, used_ocr = ?, error = ?, updated_at = ? WHERE file_id = ?;
	`, status, anyOCR(chunks), note, now(), fileID); err != nil {
		return fmt.Errorf("mark done: %w", err)
	}
	return tx.Commit()
}

func (w *Worker) finish(ctx context.Context, fileID, status, msg string) error {
	_, err := w.DB.ExecContext(ctx, `
		UPDATE file_extractions SET status = ?, error = ?, updated_at = ? WHERE file_id = ?;
	`, status, msg, now(), fileID)
	return err
}

// capText keeps chunks, in order, until max bytes of text; the chunk
// that crosses the line is cut at a rune boundary.
func capText(chunks []Chunk, max int) []Chunk {
	out := make([]Chunk, 0, len(chunks))
	left := max
	for _, c := range chunks {
		if left <= 0 {
			break
		}
		if len(c.Text) > left {
			cut := left
			for cut > 0 && !utf8.RuneStart(c.Text[cut]) {
				cut--
			}
			c.Text = c.Text[:cut]
			return append(out, c)
		}
		left -= len(c.Text)
		out = append(out, c)
	}
	return out
}

func anyOCR(chunks []Chunk) bool {
	for _, c := range chunks {
		if c.OCR {
			return true
		}
	}
	return false
}
