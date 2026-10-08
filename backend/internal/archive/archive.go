// Package archive lists what a user can see in the archive view:
// archived pages, spaces and groups they have access to. Restoring is
// done by the pages/spaces/groups stores; this package only reads.
package archive

import (
	"context"
	"database/sql"
	"fmt"
)

type Store struct{ db *sql.DB }

func NewStore(conn *sql.DB) *Store { return &Store{db: conn} }

// Page is one archived batch: a page archived on its own, with the
// sub-pages that went with it counted in SubPages (they come back
// together).
type Page struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Icon        string `json:"icon"`
	Kind        string `json:"kind"`
	SpaceID     string `json:"space_id"`
	SpaceName   string `json:"space_name"`
	ParentTitle string `json:"parent_title"`
	ArchivedAt  string `json:"archived_at"`
	ArchivedBy  string `json:"archived_by"`
	SubPages    int    `json:"sub_pages"`
}

type Space struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	Icon          string `json:"icon"`
	ArchivedAt    string `json:"archived_at"`
	GroupName     string `json:"group_name"`
	GroupArchived bool   `json:"group_archived"`
	Pages         int    `json:"pages"`
}

type Group struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Icon       string `json:"icon"`
	ArchivedAt string `json:"archived_at"`
}

type List struct {
	Pages  []Page  `json:"pages"`
	Spaces []Space `json:"spaces"`
	Groups []Group `json:"groups"`
}

// limit caps each list; the archive is a recovery tool, not a browser.
const limit = 500

// ForUser returns what userID may see in the archive:
//   - pages: roots of archived batches, in live spaces the user can
//     reach (directly or through a live group); pages inside an archived
//     space are reached by restoring the space instead
//   - spaces: archived spaces the user is in, directly or through their
//     group (even if that group is archived too)
//   - groups: archived groups the user is a member of, that have no
//     archived space of their own listed (restoring such a space brings
//     the group back with it)
//
// Restoring still checks the role per item (editor for pages, owner for
// spaces and groups), the same roles archiving requires.
func (s *Store) ForUser(ctx context.Context, userID string) (List, error) {
	out := List{Pages: []Page{}, Spaces: []Space{}, Groups: []Group{}}

	rows, err := s.db.QueryContext(ctx, `
		SELECT p.id, p.title, p.icon, p.kind, sp.id, sp.name,
		       COALESCE(pp.title, ''), p.archived_at,
		       COALESCE((SELECT u.display_name FROM activity a JOIN users u ON u.id = a.actor_id
		                 WHERE a.page_id = p.id AND a.action = 'page.archive'
		                 ORDER BY a.created_at DESC LIMIT 1), ''),
		       (WITH RECURSIVE sub(id) AS (
		            SELECT p.id
		            UNION ALL
		            SELECT c.id FROM pages c JOIN sub ON c.parent_page_id = sub.id WHERE c.archived_at = p.archived_at)
		        SELECT COUNT(*) - 1 FROM sub)
		FROM pages p
		JOIN spaces sp ON sp.id = p.space_id
		LEFT JOIN pages pp ON pp.id = p.parent_page_id
		WHERE p.archived_at IS NOT NULL
		  AND (pp.id IS NULL OR pp.archived_at IS NULL OR pp.archived_at <> p.archived_at)
		  AND sp.archived_at IS NULL
		  AND (EXISTS (SELECT 1 FROM space_members sm WHERE sm.space_id = sp.id AND sm.user_id = ?1)
		    OR EXISTS (SELECT 1 FROM group_members gm JOIN groups g ON g.id = gm.group_id
		               WHERE gm.group_id = sp.group_id AND gm.user_id = ?1 AND g.archived_at IS NULL))
		ORDER BY p.archived_at DESC LIMIT ?2;`, userID, limit)
	if err != nil {
		return out, fmt.Errorf("archive: pages: %w", err)
	}
	for rows.Next() {
		var p Page
		if err := rows.Scan(&p.ID, &p.Title, &p.Icon, &p.Kind, &p.SpaceID, &p.SpaceName,
			&p.ParentTitle, &p.ArchivedAt, &p.ArchivedBy, &p.SubPages); err != nil {
			rows.Close()
			return out, fmt.Errorf("archive: scan page: %w", err)
		}
		out.Pages = append(out.Pages, p)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return out, fmt.Errorf("archive: pages: %w", err)
	}

	rows, err = s.db.QueryContext(ctx, `
		SELECT sp.id, sp.name, sp.icon, sp.archived_at, COALESCE(g.name, ''), g.archived_at IS NOT NULL,
		       (SELECT COUNT(*) FROM pages WHERE space_id = sp.id AND archived_at IS NULL)
		FROM spaces sp
		LEFT JOIN groups g ON g.id = sp.group_id
		WHERE sp.archived_at IS NOT NULL
		  AND (EXISTS (SELECT 1 FROM space_members sm WHERE sm.space_id = sp.id AND sm.user_id = ?1)
		    OR EXISTS (SELECT 1 FROM group_members gm WHERE gm.group_id = sp.group_id AND gm.user_id = ?1))
		ORDER BY sp.archived_at DESC LIMIT ?2;`, userID, limit)
	if err != nil {
		return out, fmt.Errorf("archive: spaces: %w", err)
	}
	for rows.Next() {
		var sp Space
		var groupArchived sql.NullBool
		if err := rows.Scan(&sp.ID, &sp.Name, &sp.Icon, &sp.ArchivedAt, &sp.GroupName, &groupArchived, &sp.Pages); err != nil {
			rows.Close()
			return out, fmt.Errorf("archive: scan space: %w", err)
		}
		sp.GroupArchived = groupArchived.Valid && groupArchived.Bool
		out.Spaces = append(out.Spaces, sp)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return out, fmt.Errorf("archive: spaces: %w", err)
	}

	rows, err = s.db.QueryContext(ctx, `
		SELECT g.id, g.name, g.icon, g.archived_at
		FROM groups g
		JOIN group_members gm ON gm.group_id = g.id AND gm.user_id = ?1
		WHERE g.archived_at IS NOT NULL
		  AND NOT EXISTS (SELECT 1 FROM spaces sp WHERE sp.group_id = g.id AND sp.archived_at IS NOT NULL)
		ORDER BY g.archived_at DESC LIMIT ?2;`, userID, limit)
	if err != nil {
		return out, fmt.Errorf("archive: groups: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var g Group
		if err := rows.Scan(&g.ID, &g.Name, &g.Icon, &g.ArchivedAt); err != nil {
			return out, fmt.Errorf("archive: scan group: %w", err)
		}
		out.Groups = append(out.Groups, g)
	}
	return out, rows.Err()
}
