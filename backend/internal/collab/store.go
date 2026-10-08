// Package collab is Nook's realtime collaborative editing server
// (ADR 0006): a WebSocket relay + durable log for Yjs updates. The
// server never interprets Yjs data; it orders, stores, relays and
// compacts opaque updates, which is all Yjs needs to converge.
package collab

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// ErrStaleSnapshot: a newer snapshot is already stored.
var ErrStaleSnapshot = errors.New("collab: stale snapshot")

type Store struct{ db *sql.DB }

func NewStore(conn *sql.DB) *Store { return &Store{db: conn} }

// Update is one stored Yjs update with its per-page sequence number.
type Update struct {
	Seq  int64
	Data []byte
}

// Load returns the page's snapshot (may be nil), its upto, and every
// update after it, in seq order.
func (s *Store) Load(ctx context.Context, pageID string) (state []byte, upto int64, updates []Update, err error) {
	err = s.db.QueryRowContext(ctx, `SELECT state, upto FROM page_ydoc WHERE page_id = ?;`, pageID).Scan(&state, &upto)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, 0, nil, fmt.Errorf("collab: load snapshot: %w", err)
	}
	rows, err := s.db.QueryContext(ctx, `SELECT seq, data FROM page_yupdates WHERE page_id = ? AND seq > ? ORDER BY seq;`, pageID, upto)
	if err != nil {
		return nil, 0, nil, fmt.Errorf("collab: load updates: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var u Update
		if err := rows.Scan(&u.Seq, &u.Data); err != nil {
			return nil, 0, nil, err
		}
		updates = append(updates, u)
	}
	return state, upto, updates, rows.Err()
}

// Append stores an update under seq.
func (s *Store) Append(ctx context.Context, pageID string, seq int64, data []byte, userID string) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO page_yupdates (page_id, seq, data, user_id) VALUES (?, ?, ?, ?);`,
		pageID, seq, data, userID)
	if err != nil {
		return fmt.Errorf("collab: append: %w", err)
	}
	return nil
}

// Compact replaces the snapshot with state (which covers every update
// with seq <= upto), drops those updates, and writes contentJSON as the
// page's plain-JSON projection -- all in one transaction.
func (s *Store) Compact(ctx context.Context, pageID string, upto int64, state []byte, contentJSON string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx, `
		INSERT INTO page_ydoc (page_id, state, upto) VALUES (?, ?, ?)
		ON CONFLICT(page_id) DO UPDATE SET state = excluded.state, upto = excluded.upto,
			updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
		WHERE excluded.upto > page_ydoc.upto;`, pageID, state, upto)
	if err != nil {
		return fmt.Errorf("collab: store snapshot: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrStaleSnapshot
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM page_yupdates WHERE page_id = ? AND seq <= ?;`, pageID, upto); err != nil {
		return fmt.Errorf("collab: prune: %w", err)
	}
	if contentJSON != "" {
		if _, err := tx.ExecContext(ctx, `UPDATE pages SET content_json = ?, updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now') WHERE id = ?;`,
			contentJSON, pageID); err != nil {
			return fmt.Errorf("collab: write content_json: %w", err)
		}
	}
	return tx.Commit()
}

// HasState reports whether a page is under collaborative control (has
// any Yjs data). Plain content_json writes are refused for such pages.
func (s *Store) HasState(ctx context.Context, pageID string) (bool, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `
		SELECT (SELECT COUNT(*) FROM page_ydoc WHERE page_id = ?1) + (SELECT COUNT(*) FROM page_yupdates WHERE page_id = ?1);`,
		pageID).Scan(&n)
	return n > 0, err
}
