package assets

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"time"
)

// SweepResult counts what a sweep removed (or would remove, dry run).
type SweepResult struct {
	Images      int
	Attachments int
	OrphanFiles int
	Bytes       int64
}

var idName = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// Sweep removes uploads nothing uses any more:
//
//   - images and attached files whose page's content no longer mentions
//     them (the node was deleted from the text), once older than grace.
//     The grace period is what keeps undo working: a deleted image can
//     be brought back with Ctrl+Z long after the delete. Archived pages
//     keep their content, so their files are kept for a restore; so do
//     files an older version still shows (page history, migration 0019).
//   - files in the uploads directory with no row at all -- left by pages
//     deleted for good, or by a crash mid-upload -- once their file is
//     older than orphanGrace.
//
// Only files named like the ids Nook generates are ever considered, so
// nothing else that happens to sit in the directory is touched.
func (s *Store) Sweep(ctx context.Context, grace, orphanGrace time.Duration, dryRun bool) (SweepResult, error) {
	var res SweepResult
	cutoff := time.Now().UTC().Add(-grace).Format("2006-01-02T15:04:05.000Z")

	for _, t := range []struct {
		table string
		count *int
	}{{"page_images", &res.Images}, {"page_attachments", &res.Attachments}} {
		rows, err := s.db.QueryContext(ctx, `
			SELECT a.id, a.size_bytes FROM `+t.table+` a
			WHERE a.created_at < ?
			  AND NOT EXISTS (SELECT 1 FROM pages p WHERE instr(p.content_json, a.id) > 0)
			  AND NOT EXISTS (SELECT 1 FROM page_versions v WHERE instr(v.content_json, a.id) > 0);`, cutoff)
		if err != nil {
			return res, fmt.Errorf("assets: sweep %s: %w", t.table, err)
		}
		type victim struct {
			id   string
			size int64
		}
		var victims []victim
		for rows.Next() {
			var v victim
			if err := rows.Scan(&v.id, &v.size); err != nil {
				rows.Close()
				return res, fmt.Errorf("assets: sweep %s: %w", t.table, err)
			}
			victims = append(victims, v)
		}
		rows.Close()
		for _, v := range victims {
			*t.count++
			res.Bytes += v.size
			if dryRun {
				continue
			}
			// Row first, then file: a crash in between leaves an orphan
			// file for the next sweep, never a row without its file.
			if _, err := s.db.ExecContext(ctx, `DELETE FROM `+t.table+` WHERE id = ?;`, v.id); err != nil {
				return res, fmt.Errorf("assets: sweep delete row: %w", err)
			}
			if err := os.Remove(s.filePath(v.id)); err != nil && !os.IsNotExist(err) {
				return res, fmt.Errorf("assets: sweep delete file: %w", err)
			}
		}
	}

	known := map[string]bool{}
	for _, q := range []string{`SELECT id FROM page_covers`, `SELECT id FROM page_images`, `SELECT id FROM page_attachments`} {
		rows, err := s.db.QueryContext(ctx, q)
		if err != nil {
			return res, fmt.Errorf("assets: sweep list: %w", err)
		}
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				rows.Close()
				return res, err
			}
			known[id] = true
		}
		rows.Close()
	}
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return res, fmt.Errorf("assets: sweep read dir: %w", err)
	}
	oldest := time.Now().Add(-orphanGrace)
	for _, e := range entries {
		if !e.Type().IsRegular() || !idName.MatchString(e.Name()) || known[e.Name()] {
			continue
		}
		info, err := e.Info()
		if err != nil || info.ModTime().After(oldest) {
			continue
		}
		res.OrphanFiles++
		res.Bytes += info.Size()
		if !dryRun {
			if err := os.Remove(s.filePath(e.Name())); err != nil && !os.IsNotExist(err) {
				return res, fmt.Errorf("assets: sweep orphan: %w", err)
			}
		}
	}
	return res, nil
}
