package collab

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"nook/internal/db"
)

// versionWindow: a snapshot within this long of the newest version's
// start updates that version; a later one starts a new version.
const versionWindow = "10 minutes"

// maxVersions per page; the oldest go first.
const maxVersions = 200

// recordVersion keeps page_versions in step with a new content_json (see
// migration 0019). Unchanged content writes nothing.
func recordVersion(ctx context.Context, tx *sql.Tx, pageID, contentJSON, userID string) error {
	var id, last string
	var fresh bool
	err := tx.QueryRowContext(ctx, `
		SELECT id, content_json, created_at > strftime('%Y-%m-%dT%H:%M:%fZ', 'now', '-`+versionWindow+`')
		FROM page_versions WHERE page_id = ? ORDER BY created_at DESC LIMIT 1;`, pageID).Scan(&id, &last, &fresh)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("collab: read version: %w", err)
	}
	if err == nil && last == contentJSON {
		return nil
	}
	if err == nil && fresh {
		_, err = tx.ExecContext(ctx, `
			UPDATE page_versions SET content_json = ?, user_id = ?, updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
			WHERE id = ?;`, contentJSON, nullable(userID), id)
		if err != nil {
			return fmt.Errorf("collab: update version: %w", err)
		}
		return nil
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO page_versions (id, page_id, content_json, user_id) VALUES (?, ?, ?, ?);`,
		db.NewID(), pageID, contentJSON, nullable(userID)); err != nil {
		return fmt.Errorf("collab: insert version: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
		DELETE FROM page_versions WHERE page_id = ?1 AND id NOT IN (
			SELECT id FROM page_versions WHERE page_id = ?1 ORDER BY created_at DESC LIMIT ?2);`, pageID, maxVersions); err != nil {
		return fmt.Errorf("collab: prune versions: %w", err)
	}
	return nil
}

func nullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// linkedPages returns the ids of the pages a document links to: the id
// attribute of every pageMention node, each once.
func linkedPages(contentJSON string) []string {
	var doc any
	if json.Unmarshal([]byte(contentJSON), &doc) != nil {
		return nil
	}
	seen := map[string]bool{}
	var out []string
	var walk func(any)
	walk = func(n any) {
		switch v := n.(type) {
		case map[string]any:
			if v["type"] == "pageMention" {
				if attrs, ok := v["attrs"].(map[string]any); ok {
					if id, ok := attrs["id"].(string); ok && id != "" && !seen[id] {
						seen[id] = true
						out = append(out, id)
					}
				}
			}
			for _, c := range v {
				walk(c)
			}
		case []any:
			for _, c := range v {
				walk(c)
			}
		}
	}
	walk(doc)
	return out
}

// syncLinks replaces pageID's outgoing links with the ones in its
// document. Ids of pages that don't exist (pasted from elsewhere,
// deleted since) are skipped rather than failing the snapshot.
func syncLinks(ctx context.Context, tx *sql.Tx, pageID, contentJSON string) error {
	if _, err := tx.ExecContext(ctx, `DELETE FROM page_links WHERE from_page_id = ?;`, pageID); err != nil {
		return fmt.Errorf("collab: clear links: %w", err)
	}
	for _, to := range linkedPages(contentJSON) {
		if to == pageID {
			continue
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT OR IGNORE INTO page_links (from_page_id, to_page_id) SELECT ?, id FROM pages WHERE id = ?;`, pageID, to); err != nil {
			return fmt.Errorf("collab: store link: %w", err)
		}
	}
	return nil
}

// Version is one entry of a page's history (without its document).
type Version struct {
	ID        string `json:"id"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
	UserName  string `json:"user_name"`
}

// Versions lists a page's history, newest first.
func (s *Store) Versions(ctx context.Context, pageID string) ([]Version, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT v.id, v.created_at, v.updated_at, COALESCE(u.display_name, '')
		FROM page_versions AS v LEFT JOIN users AS u ON u.id = v.user_id
		WHERE v.page_id = ? ORDER BY v.created_at DESC;`, pageID)
	if err != nil {
		return nil, fmt.Errorf("collab: list versions: %w", err)
	}
	defer rows.Close()
	list := make([]Version, 0)
	for rows.Next() {
		var v Version
		if err := rows.Scan(&v.ID, &v.CreatedAt, &v.UpdatedAt, &v.UserName); err != nil {
			return nil, err
		}
		list = append(list, v)
	}
	return list, rows.Err()
}

// VersionContent returns one version's document; it must belong to pageID.
func (s *Store) VersionContent(ctx context.Context, pageID, versionID string) (string, error) {
	var content string
	err := s.db.QueryRowContext(ctx, `SELECT content_json FROM page_versions WHERE id = ? AND page_id = ?;`,
		versionID, pageID).Scan(&content)
	if errors.Is(err, sql.ErrNoRows) {
		return "", db.ErrNotFound
	}
	return content, err
}

// Backlink is a page that links to another one.
type Backlink struct {
	ID      string `json:"id"`
	Title   string `json:"title"`
	Icon    string `json:"icon"`
	SpaceID string `json:"-"`
}

// Backlinks lists the live pages that link to pageID. The caller filters
// them by what the viewer may see.
func (s *Store) Backlinks(ctx context.Context, pageID string) ([]Backlink, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT p.id, p.title, p.icon, p.space_id FROM page_links AS l JOIN pages AS p ON p.id = l.from_page_id
		WHERE l.to_page_id = ? AND p.archived_at IS NULL ORDER BY p.title;`, pageID)
	if err != nil {
		return nil, fmt.Errorf("collab: backlinks: %w", err)
	}
	defer rows.Close()
	var list []Backlink
	for rows.Next() {
		var b Backlink
		if err := rows.Scan(&b.ID, &b.Title, &b.Icon, &b.SpaceID); err != nil {
			return nil, err
		}
		list = append(list, b)
	}
	return list, rows.Err()
}
