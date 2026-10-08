// Package activity is the append-only "who changed what" log (ADR 0005
// §9), plus pages.updated_by for the cheap "last edited by X".
package activity

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"nook/internal/db"
)

const (
	PageCreate  = "page.create"
	PageEdit    = "page.edit"
	PageRename  = "page.rename"
	PageMove    = "page.move"
	PageArchive = "page.archive"
	PageRestore = "page.restore"
	RowEdit     = "row.edit"
)

// editCoalesce: repeated page.edit/row.edit by the same user on the
// same page within this window update one entry instead of adding more,
// so autosave doesn't flood the feed.
const editCoalesce = 10 * time.Minute

type Entry struct {
	ID        string          `json:"id"`
	ActorID   string          `json:"actor_id"`
	ActorName string          `json:"actor_name"`
	SpaceID   string          `json:"space_id"`
	PageID    string          `json:"page_id"`
	PageTitle string          `json:"page_title"`
	Action    string          `json:"action"`
	Meta      json.RawMessage `json:"meta"`
	CreatedAt string          `json:"created_at"`
}

type Store struct{ db *sql.DB }

func NewStore(conn *sql.DB) *Store { return &Store{db: conn} }

func nullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// Record appends an entry. For page-level actions it also sets
// pages.updated_by / updated_at. meta may be nil.
func (s *Store) Record(ctx context.Context, actorID, spaceID, pageID, action string, meta map[string]any) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	metaJSON := []byte("{}")
	if meta != nil {
		var err error
		if metaJSON, err = json.Marshal(meta); err != nil {
			return err
		}
	}
	if pageID != "" && action != PageArchive {
		if _, err := s.db.ExecContext(ctx, `UPDATE pages SET updated_by = ? WHERE id = ?;`, actorID, pageID); err != nil {
			return fmt.Errorf("activity: touch page: %w", err)
		}
	}
	if action == PageEdit || action == RowEdit {
		res, err := s.db.ExecContext(ctx, `
			UPDATE activity SET created_at = ?, meta_json = ?
			WHERE id = (SELECT id FROM activity WHERE actor_id = ? AND page_id = ? AND action = ? AND created_at > ?
			            ORDER BY created_at DESC LIMIT 1);`,
			now, string(metaJSON), actorID, pageID, action, time.Now().UTC().Add(-editCoalesce).Format(time.RFC3339Nano))
		if err != nil {
			return fmt.Errorf("activity: coalesce: %w", err)
		}
		if n, _ := res.RowsAffected(); n > 0 {
			return nil
		}
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO activity (id, actor_id, space_id, page_id, action, meta_json, created_at) VALUES (?, ?, ?, ?, ?, ?, ?);`,
		db.NewID(), nullable(actorID), nullable(spaceID), nullable(pageID), action, string(metaJSON), now)
	if err != nil {
		return fmt.Errorf("activity: record: %w", err)
	}
	return nil
}

func (s *Store) list(ctx context.Context, where string, arg any, limit int) ([]Entry, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT a.id, COALESCE(a.actor_id,''), COALESCE(u.display_name,''), COALESCE(a.space_id,''),
		       COALESCE(a.page_id,''), COALESCE(p.title,''), a.action, a.meta_json, a.created_at
		FROM activity a
		LEFT JOIN users u ON u.id = a.actor_id
		LEFT JOIN pages p ON p.id = a.page_id
		WHERE `+where+` ORDER BY a.created_at DESC LIMIT ?;`, arg, limit)
	if err != nil {
		return nil, fmt.Errorf("activity: list: %w", err)
	}
	defer rows.Close()
	out := []Entry{}
	for rows.Next() {
		var e Entry
		var meta string
		if err := rows.Scan(&e.ID, &e.ActorID, &e.ActorName, &e.SpaceID, &e.PageID, &e.PageTitle, &e.Action, &meta, &e.CreatedAt); err != nil {
			return nil, err
		}
		e.Meta = json.RawMessage(meta)
		out = append(out, e)
	}
	return out, rows.Err()
}

// ForPage lists a page's activity, including its database rows'
// (children's) edits.
func (s *Store) ForPage(ctx context.Context, pageID string, limit int) ([]Entry, error) {
	return s.list(ctx, `(a.page_id = ?1 OR a.page_id IN (SELECT id FROM pages WHERE parent_page_id = ?1))`, pageID, limit)
}

func (s *Store) ForSpace(ctx context.Context, spaceID string, limit int) ([]Entry, error) {
	// Entries for pages since moved to another space are left out, so a
	// space feed never leaks titles from spaces the viewer may not see.
	return s.list(ctx, `a.space_id = ? AND (a.page_id IS NULL OR p.space_id = a.space_id)`, spaceID, limit)
}
