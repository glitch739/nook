// Package pages manages Nook pages: creation, lookup, content/title
// updates, archiving, and building the nested tree a space's pages form.
//
// A page's content is a single Tiptap/ProseMirror document JSON blob
// (content_json) — see ADR 0002. This package treats it as an opaque
// string; it's the frontend's job to understand its structure, not the
// backend's.
package pages

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"nook/internal/db"
)

// EmptyDoc is the content_json a brand new page starts with — an empty
// Tiptap/ProseMirror document. Matches the column's DB-level DEFAULT in
// migrations/0001_init.sql; kept here too so Create's return value is
// correct without a round-trip SELECT.
const EmptyDoc = `{"type":"doc","content":[]}`

// Errors specific to Move -- see its own comment for what each guards
// against.
var (
	ErrSelfParent             = errors.New("pages: a page cannot be its own parent")
	ErrCircularHierarchy      = errors.New("pages: cannot move a page under its own descendant")
	ErrParentInDifferentSpace = errors.New("pages: parent page must be in the same space")
)

// positionWidth zero-pads sibling positions so plain lexicographic
// ORDER BY position sorts correctly (SQLite TEXT columns compare as
// strings — "10" would otherwise sort before "2"). Good for up to
// 999999 siblings under one parent. Same full-restamp-on-reorder scheme
// as spaces/groups/database columns (see ReorderSiblings below) rather
// than the fractional-indexing ROADMAP's Phase 4 note once
// anticipated -- a page's sibling count is small in exactly the same
// way a user's space/group count is, so there's no real cost the other
// three ordering schemes in this codebase don't already accept, and
// diverging here would mean two different reorder mental models to
// maintain instead of one.
const positionWidth = 6

// Page kinds. A 'document' page (the only kind that existed before
// ADR 0004) holds a Tiptap document in ContentJSON. A 'database' page
// holds no meaningful ContentJSON of its own -- its "content" is the
// set of its child pages (its rows) plus their structured properties,
// owned by internal/databases. Rows are themselves ordinary 'document'
// pages; only the database page's own row has Kind == KindDatabase.
const (
	KindDocument = "document"
	KindDatabase = "database"
	// KindCollection (migration 0018) gathers documents and databases as
	// its children and lists them on its own page; the sidebar shows the
	// collection as one row instead of every child. It can't hold another
	// collection.
	KindCollection = "collection"
)

// DefaultDocumentIcon / DefaultDatabaseIcon are what a new page starts
// with (lucide icons since 2026-10-02, at the user's request, instead of
// the 📄 emoji). Create always writes one explicitly, so the column's
// old DEFAULT '📄' (migration 0002) is never what a new page gets, and
// existing pages keep whatever icon they already have. Frontend mirror:
// DEFAULT_ICON in pageIcon.ts.
const (
	DefaultDocumentIcon   = "lucide:square-text"
	DefaultDatabaseIcon   = "lucide:table"
	DefaultCollectionIcon = "lucide:library-big"
)

func defaultIcon(kind string) string {
	switch kind {
	case KindDatabase:
		return DefaultDatabaseIcon
	case KindCollection:
		return DefaultCollectionIcon
	}
	return DefaultDocumentIcon
}

// CanHold reports whether a page of kind child may be created in or
// moved under a parent of kind parent. A collection holds documents and
// databases, never another collection, and a collection never sits in
// a database (whose children are its rows).
func CanHold(parent, child string) bool {
	if child == KindCollection {
		return parent != KindCollection && parent != KindDatabase
	}
	return true
}

// CollectionItem is one entry of a collection's list.
type CollectionItem struct {
	ID            string `json:"id"`
	Title         string `json:"title"`
	Icon          string `json:"icon"`
	Kind          string `json:"kind"`
	CreatedAt     string `json:"created_at"`
	UpdatedAt     string `json:"updated_at"`
	UpdatedByName string `json:"updated_by_name"`
}

// ListCollectionItems returns a collection's live children, most
// recently edited first, with what its list shows (no documents).
func (s *Store) ListCollectionItems(ctx context.Context, collectionID string) ([]CollectionItem, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT p.id, p.title, p.icon, p.kind, p.created_at, p.updated_at, COALESCE(u.display_name, '')
		FROM pages AS p
		LEFT JOIN users AS u ON u.id = p.updated_by
		WHERE p.parent_page_id = ? AND p.archived_at IS NULL
		ORDER BY p.updated_at DESC;
	`, collectionID)
	if err != nil {
		return nil, fmt.Errorf("pages: list collection: %w", err)
	}
	defer rows.Close()
	list := make([]CollectionItem, 0)
	for rows.Next() {
		var it CollectionItem
		if err := rows.Scan(&it.ID, &it.Title, &it.Icon, &it.Kind, &it.CreatedAt, &it.UpdatedAt, &it.UpdatedByName); err != nil {
			return nil, fmt.Errorf("pages: scan collection item: %w", err)
		}
		list = append(list, it)
	}
	return list, rows.Err()
}

type Page struct {
	ID           string
	SpaceID      string
	ParentPageID *string
	Title        string
	Icon         string
	ContentJSON  string
	Position     string
	CreatedBy    string
	Kind         string
	ArchivedAt   *string
	CreatedAt    string
	UpdatedAt    string
	// UpdatedBy/UpdatedByName: last editor (migration 0011, written by
	// internal/activity.Record). Empty for never-touched rows.
	UpdatedBy     string
	UpdatedByName string
}

type Store struct {
	db *sql.DB
}

func NewStore(conn *sql.DB) *Store {
	return &Store{db: conn}
}

// Create inserts a new page as the last child of parentPageID (nil for
// a space's top-level pages) within spaceID, owned by createdBy. kind
// is one of the Kind* constants -- KindDocument for an ordinary page,
// KindDatabase for a database page (or a database's row, which is
// itself a KindDocument page with a database page as its parent -- the
// two are the same Create call, just with a different parentPageID).
func (s *Store) Create(ctx context.Context, spaceID string, parentPageID *string, title, createdBy, kind string) (Page, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Page{}, fmt.Errorf("pages: begin create transaction: %w", err)
	}
	defer tx.Rollback()

	position, err := nextPosition(ctx, tx, spaceID, parentPageID)
	if err != nil {
		return Page{}, fmt.Errorf("pages: compute position: %w", err)
	}

	id := db.NewID()
	_, err = tx.ExecContext(ctx, `
		INSERT INTO pages (id, space_id, parent_page_id, title, icon, position, created_by, kind)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?);
	`, id, spaceID, parentPageID, title, defaultIcon(kind), position, createdBy, kind)
	if err != nil {
		return Page{}, fmt.Errorf("pages: create page: %w", err)
	}

	page, err := scanOne(tx.QueryRowContext(ctx, selectByID, id))
	if err != nil {
		return Page{}, fmt.Errorf("pages: read created page: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return Page{}, fmt.Errorf("pages: commit create transaction: %w", err)
	}
	return page, nil
}

// nextPosition returns a position that sorts after every existing
// sibling of parentPageID within spaceID.
func nextPosition(ctx context.Context, tx *sql.Tx, spaceID string, parentPageID *string) (string, error) {
	var maxPosition sql.NullString
	err := tx.QueryRowContext(ctx, `
		SELECT MAX(position) FROM pages
		WHERE space_id = ? AND parent_page_id IS ?;
	`, spaceID, parentPageID).Scan(&maxPosition)
	if err != nil {
		return "", err
	}

	next := 1
	if maxPosition.Valid {
		var current int
		if _, err := fmt.Sscanf(maxPosition.String, "%d", &current); err == nil {
			next = current + 1
		}
	}
	return fmt.Sprintf("%0*d", positionWidth, next), nil
}

const selectByID = `
	SELECT id, space_id, parent_page_id, title, icon, content_json, position,
	       created_by, kind, archived_at, created_at, updated_at,
	       COALESCE(updated_by, ''), COALESCE((SELECT display_name FROM users WHERE users.id = pages.updated_by), '')
	FROM pages WHERE id = ?;
`

// selectMetaByID is selectByID without the document: content_json comes
// back empty. For the many requests that only need to know where a page
// lives and who may see it (cells, covers, images, attachments, ...).
const selectMetaByID = `
	SELECT id, space_id, parent_page_id, title, icon, '', position,
	       created_by, kind, archived_at, created_at, updated_at,
	       COALESCE(updated_by, ''), ''
	FROM pages WHERE id = ?;
`

// GetMetaByID is GetByID without ContentJSON and UpdatedByName (both
// left empty). Same not-found behavior.
func (s *Store) GetMetaByID(ctx context.Context, id string) (Page, error) {
	return scanOne(s.db.QueryRowContext(ctx, selectMetaByID, id))
}

// GetByID looks up a page by ID, including archived ones — callers that
// only want live pages should check ArchivedAt themselves. Returns an
// error wrapping db.ErrNotFound if no such page exists.
func (s *Store) GetByID(ctx context.Context, id string) (Page, error) {
	return scanOne(s.db.QueryRowContext(ctx, selectByID, id))
}

// ListForSpace returns every non-archived page in spaceID, ordered so
// that a caller building a tree can do it in one pass: parents strictly
// come from OrderBy(position) within each parent_page_id group, but
// since callers need the *whole* tree, this returns everything ordered
// by (parent_page_id, position) and leaves nesting to BuildTree.
// ListTreeForSpaces returns the sidebar fields only (id, space,
// parent, title, icon, position, kind) of every non-archived page in
// the given spaces, in one query. Unlike ListForSpace it never reads
// content_json (a page's whole document) or the last editor's name,
// which a tree doesn't show. Order: parent, then position, as BuildTree
// expects.
func (s *Store) ListTreeForSpaces(ctx context.Context, spaceIDs []string) ([]Page, error) {
	list := make([]Page, 0)
	if len(spaceIDs) == 0 {
		return list, nil
	}
	args := make([]any, len(spaceIDs))
	for i, id := range spaceIDs {
		args[i] = id
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, space_id, parent_page_id, title, icon, position, kind
		FROM pages
		WHERE archived_at IS NULL AND space_id IN (?`+strings.Repeat(",?", len(spaceIDs)-1)+`)
		ORDER BY parent_page_id, position;
	`, args...)
	if err != nil {
		return nil, fmt.Errorf("pages: list tree: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var p Page
		if err := rows.Scan(&p.ID, &p.SpaceID, &p.ParentPageID, &p.Title, &p.Icon, &p.Position, &p.Kind); err != nil {
			return nil, fmt.Errorf("pages: scan tree row: %w", err)
		}
		list = append(list, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("pages: list tree rows: %w", err)
	}
	return list, nil
}

func (s *Store) ListForSpace(ctx context.Context, spaceID string) ([]Page, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, space_id, parent_page_id, title, icon, content_json, position,
		       created_by, kind, archived_at, created_at, updated_at,
	       COALESCE(updated_by, ''), COALESCE((SELECT display_name FROM users WHERE users.id = pages.updated_by), '')
		FROM pages
		WHERE space_id = ? AND archived_at IS NULL
		ORDER BY parent_page_id, position;
	`, spaceID)
	if err != nil {
		return nil, fmt.Errorf("pages: list for space: %w", err)
	}
	defer rows.Close()

	list := make([]Page, 0)
	for rows.Next() {
		p, err := scanRow(rows)
		if err != nil {
			return nil, fmt.Errorf("pages: scan page: %w", err)
		}
		list = append(list, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("pages: list rows: %w", err)
	}
	return list, nil
}

// ListChildren returns every non-archived direct child of parentPageID,
// ordered by position. Used for a database page's rows (see
// internal/databases) -- "the children of this page" *is* the row set
// there, unlike a document page's children, which are just nested pages
// in the sidebar tree.
func (s *Store) ListChildren(ctx context.Context, parentPageID string) ([]Page, error) {
	// Only the fields a database table shows for its rows (id, space,
	// parent, title, icon, position, kind): this backs GET .../rows, so
	// reading every row's whole document (content_json) and its last
	// editor's name per refresh would be wasted work.
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, space_id, parent_page_id, title, icon, position, kind
		FROM pages
		WHERE parent_page_id = ? AND archived_at IS NULL
		ORDER BY position;
	`, parentPageID)
	if err != nil {
		return nil, fmt.Errorf("pages: list children: %w", err)
	}
	defer rows.Close()

	list := make([]Page, 0)
	for rows.Next() {
		var p Page
		if err := rows.Scan(&p.ID, &p.SpaceID, &p.ParentPageID, &p.Title, &p.Icon, &p.Position, &p.Kind); err != nil {
			return nil, fmt.Errorf("pages: scan child: %w", err)
		}
		list = append(list, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("pages: list children rows: %w", err)
	}
	return list, nil
}

// UpdateTitle sets a page's title.
func (s *Store) UpdateTitle(ctx context.Context, id, title string) (Page, error) {
	res, err := s.db.ExecContext(ctx, `
		UPDATE pages SET title = ?, updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
		WHERE id = ?;
	`, title, id)
	if err != nil {
		return Page{}, fmt.Errorf("pages: update title: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return Page{}, fmt.Errorf("pages: update title: %w", db.ErrNotFound)
	}
	return s.GetByID(ctx, id)
}

// UpdateIcon sets a page's icon (a single emoji or short string, opaque
// to this package — same treatment as the frontend's icon-editor popover:
// no validation of what counts as a valid icon).
func (s *Store) UpdateIcon(ctx context.Context, id, icon string) (Page, error) {
	res, err := s.db.ExecContext(ctx, `
		UPDATE pages SET icon = ?, updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
		WHERE id = ?;
	`, icon, id)
	if err != nil {
		return Page{}, fmt.Errorf("pages: update icon: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return Page{}, fmt.Errorf("pages: update icon: %w", db.ErrNotFound)
	}
	return s.GetByID(ctx, id)
}

// UpdateContent sets a page's content_json (the full Tiptap/ProseMirror
// document, opaque to this package — see ADR 0002).
func (s *Store) UpdateContent(ctx context.Context, id, contentJSON string) (Page, error) {
	res, err := s.db.ExecContext(ctx, `
		UPDATE pages SET content_json = ?, updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
		WHERE id = ?;
	`, contentJSON, id)
	if err != nil {
		return Page{}, fmt.Errorf("pages: update content: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return Page{}, fmt.Errorf("pages: update content: %w", db.ErrNotFound)
	}
	return s.GetByID(ctx, id)
}

// subtree is a recursive CTE "sub(id)" of the page bound to its single
// parameter plus every page beneath it.
const subtree = `
	WITH RECURSIVE sub(id) AS (
		SELECT ?
		UNION ALL
		SELECT p.id FROM pages AS p JOIN sub ON p.parent_page_id = sub.id
	)`

// ErrSpaceArchived is returned by Restore when the page's space itself
// is archived: there is nowhere visible to bring the page back to
// (restore the space instead).
var ErrSpaceArchived = errors.New("pages: space is archived")

// sqliteNow matches strftime('%Y-%m-%dT%H:%M:%fZ', 'now'), the format of
// every other timestamp column.
func sqliteNow() string { return time.Now().UTC().Format("2006-01-02T15:04:05.000Z") }

// Archive soft-deletes a page together with every live page beneath it
// (a database page's rows included), all with one shared archived_at
// stamp. That stamp is what lets Restore bring back exactly this batch
// and nothing archived separately before. (Until 2026-10-02 archiving
// didn't cascade, and children of an archived page surfaced as
// top-level pages instead.)
func (s *Store) Archive(ctx context.Context, id string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("pages: begin archive: %w", err)
	}
	defer tx.Rollback()

	var live int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM pages WHERE id = ? AND archived_at IS NULL;`, id).Scan(&live); err != nil {
		return fmt.Errorf("pages: archive: %w", err)
	}
	if live == 0 {
		return fmt.Errorf("pages: archive: %w", db.ErrNotFound)
	}
	now := sqliteNow()
	if _, err := tx.ExecContext(ctx, subtree+`
		UPDATE pages SET archived_at = ?, updated_at = ?
		WHERE archived_at IS NULL AND id IN (SELECT id FROM sub);
	`, id, now, now); err != nil {
		return fmt.Errorf("pages: archive: %w", err)
	}
	return tx.Commit()
}

// DeleteArchived permanently deletes an archived page and the sub-pages
// archived together with it (same stamp). Sub-pages archived separately
// earlier are detached first (moved to the top level, still archived),
// so the ON DELETE CASCADE on parent_page_id can't take them along --
// they stay in the archive as their own entries. Comments, cells,
// covers, image rows, realtime history and search rows cascade.
func (s *Store) DeleteArchived(ctx context.Context, id string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("pages: begin delete: %w", err)
	}
	defer tx.Rollback()
	var stamp sql.NullString
	err = tx.QueryRowContext(ctx, `SELECT archived_at FROM pages WHERE id = ?;`, id).Scan(&stamp)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && !stamp.Valid) {
		return fmt.Errorf("pages: delete: %w", db.ErrNotFound)
	}
	if err != nil {
		return fmt.Errorf("pages: delete: %w", err)
	}
	if _, err := tx.ExecContext(ctx, subtree+`
		UPDATE pages SET parent_page_id = NULL
		WHERE parent_page_id IN (SELECT id FROM sub)
		  AND (archived_at IS NULL OR archived_at <> ?);
	`, id, stamp.String); err != nil {
		return fmt.Errorf("pages: delete (detach): %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM pages WHERE id = ?;`, id); err != nil {
		return fmt.Errorf("pages: delete: %w", err)
	}
	return tx.Commit()
}

// Restore un-archives a page and the sub-pages that were archived with
// it (same archived_at stamp). If its parent is still archived, the
// page comes back at the top level of its space rather than staying
// hidden under it.
func (s *Store) Restore(ctx context.Context, id string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("pages: begin restore: %w", err)
	}
	defer tx.Rollback()

	var stamp sql.NullString
	var spaceArchived, parentArchived bool
	err = tx.QueryRowContext(ctx, `
		SELECT p.archived_at,
		       (SELECT s.archived_at IS NOT NULL FROM spaces AS s WHERE s.id = p.space_id),
		       COALESCE((SELECT pp.archived_at IS NOT NULL FROM pages AS pp WHERE pp.id = p.parent_page_id), 0)
		FROM pages AS p WHERE p.id = ?;
	`, id).Scan(&stamp, &spaceArchived, &parentArchived)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && !stamp.Valid) {
		return fmt.Errorf("pages: restore: %w", db.ErrNotFound)
	}
	if err != nil {
		return fmt.Errorf("pages: restore: %w", err)
	}
	if spaceArchived {
		return ErrSpaceArchived
	}
	if _, err := tx.ExecContext(ctx, subtree+`
		UPDATE pages SET archived_at = NULL, updated_at = ?
		WHERE archived_at = ? AND id IN (SELECT id FROM sub);
	`, id, sqliteNow(), stamp.String); err != nil {
		return fmt.Errorf("pages: restore: %w", err)
	}
	if parentArchived {
		if _, err := tx.ExecContext(ctx, `UPDATE pages SET parent_page_id = NULL WHERE id = ?;`, id); err != nil {
			return fmt.Errorf("pages: restore to top level: %w", err)
		}
	}
	return tx.Commit()
}

// ReorderSiblings re-stamps position for exactly the page IDs given, in
// that order, for the sibling group (spaceID, parentPageID) -- same
// "validate as a permutation of the caller's current set, then full
// re-stamp inside one transaction" shape as
// spaces.Store.ReorderForUser/databases.Store.ReorderColumns. Callers
// (the HTTP layer) are expected to have already validated pageIDs is
// exactly this sibling group's current member set; this doesn't
// re-derive it, since doing so here would mean querying the same rows
// twice for every reorder.
func (s *Store) ReorderSiblings(ctx context.Context, spaceID string, parentPageID *string, pageIDs []string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("pages: begin reorder transaction: %w", err)
	}
	defer tx.Rollback()

	for i, id := range pageIDs {
		position := fmt.Sprintf("%0*d", positionWidth, i+1)
		res, err := tx.ExecContext(ctx, `
			UPDATE pages
			SET position = ?, updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
			WHERE id = ? AND space_id = ? AND parent_page_id IS ?;
		`, position, id, spaceID, parentPageID)
		if err != nil {
			return fmt.Errorf("pages: reorder sibling: %w", err)
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return fmt.Errorf("pages: reorder sibling: %w", db.ErrNotFound)
		}
	}
	return tx.Commit()
}

// Move relocates pageID to be the last child of (newSpaceID,
// newParentPageID) -- covers every kind of move this app supports:
// reordering falls entirely under ReorderSiblings instead (Move always
// lands at the end; a drag that also needs a specific position among
// the new siblings follows this with a ReorderSiblings call), so this
// only ever has to handle a *parent* changing: reparenting within one
// space (newSpaceID unchanged), or moving to a different space
// entirely (with or without a parent there) -- newParentPageID nil
// either way just means "as a top-level page of newSpaceID". A page
// moving to a space in a different group is the identical operation;
// a page is never parented directly to a group (see ADR 0004 and the
// domain model this mirrors), so there's no separate "which group"
// concept for Move to know about at all.
//
// Refuses (returning one of the sentinel errors above, not a generic
// error) three ways a move could corrupt the tree: newParentPageID
// equal to pageID itself (ErrSelfParent), newParentPageID anywhere in
// pageID's own subtree (ErrCircularHierarchy -- walked by following
// parent_page_id upward from newParentPageID, which terminates in at
// most the tree's own depth many steps), and newParentPageID belonging
// to a different space than newSpaceID (ErrParentInDifferentSpace --
// parent_page_id has no FK-level constraint tying it to the same
// space_id as its child, so this has to be checked in code). None of
// these are permission checks (the HTTP layer's job, via
// permissions.Checker on both the source and destination space) --
// they're structural invariants of the tree itself.
func (s *Store) Move(ctx context.Context, pageID, newSpaceID string, newParentPageID *string) error {
	if newParentPageID != nil && *newParentPageID == pageID {
		return ErrSelfParent
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("pages: begin move transaction: %w", err)
	}
	defer tx.Rollback()

	if newParentPageID != nil {
		var parentSpaceID string
		err := tx.QueryRowContext(ctx, `SELECT space_id FROM pages WHERE id = ?;`, *newParentPageID).Scan(&parentSpaceID)
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("pages: move: parent %w", db.ErrNotFound)
		}
		if err != nil {
			return fmt.Errorf("pages: move: check parent space: %w", err)
		}
		if parentSpaceID != newSpaceID {
			return ErrParentInDifferentSpace
		}

		circular, err := isDescendantOf(ctx, tx, *newParentPageID, pageID)
		if err != nil {
			return fmt.Errorf("pages: move: check circular hierarchy: %w", err)
		}
		if circular {
			return ErrCircularHierarchy
		}
	}

	position, err := nextPosition(ctx, tx, newSpaceID, newParentPageID)
	if err != nil {
		return fmt.Errorf("pages: move: compute position: %w", err)
	}

	res, err := tx.ExecContext(ctx, `
		UPDATE pages
		SET space_id = ?, parent_page_id = ?, position = ?,
		    updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
		WHERE id = ?;
	`, newSpaceID, newParentPageID, position, pageID)
	if err != nil {
		return fmt.Errorf("pages: move: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("pages: move: %w", db.ErrNotFound)
	}
	// Descendants follow the page: access is checked by each page's own space_id.
	if _, err := tx.ExecContext(ctx, subtree+`
		UPDATE pages SET space_id = ? WHERE id IN (SELECT id FROM sub);
	`, pageID, newSpaceID); err != nil {
		return fmt.Errorf("pages: move subtree: %w", err)
	}
	return tx.Commit()
}

// isDescendantOf reports whether candidateID is ancestorID itself or
// appears anywhere in ancestorID's own subtree -- walked the other way
// around from how that reads: starting at candidateID and following
// parent_page_id upward, which reaches ancestorID if and only if
// candidateID is ancestorID or one of its descendants. Terminates in at
// most the tree's depth many steps (each page has exactly one parent,
// and the chain can't cycle back on itself in a valid tree).
func isDescendantOf(ctx context.Context, tx *sql.Tx, candidateID, ancestorID string) (bool, error) {
	current := candidateID
	for {
		if current == ancestorID {
			return true, nil
		}
		var parent sql.NullString
		err := tx.QueryRowContext(ctx, `SELECT parent_page_id FROM pages WHERE id = ?;`, current).Scan(&parent)
		if err != nil {
			return false, err
		}
		if !parent.Valid {
			return false, nil
		}
		current = parent.String
	}
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanOne(row *sql.Row) (Page, error) {
	return scanRowLike(row)
}

func scanRow(rows *sql.Rows) (Page, error) {
	return scanRowLike(rows)
}

func scanRowLike(s rowScanner) (Page, error) {
	var p Page
	err := s.Scan(
		&p.ID, &p.SpaceID, &p.ParentPageID, &p.Title, &p.Icon, &p.ContentJSON,
		&p.Position, &p.CreatedBy, &p.Kind, &p.ArchivedAt, &p.CreatedAt, &p.UpdatedAt,
		&p.UpdatedBy, &p.UpdatedByName,
	)
	if err == sql.ErrNoRows {
		return Page{}, fmt.Errorf("pages: %w", db.ErrNotFound)
	}
	if err != nil {
		return Page{}, err
	}
	return p, nil
}

// TreeNode is one page plus its children, for API responses / building
// the sidebar tree.
type TreeNode struct {
	Page
	Children []*TreeNode
}

// BuildTree nests a flat, space-scoped page list (as returned by
// ListForSpace) into parent/child trees, one root per top-level page
// (ParentPageID == nil), in the order ListForSpace already provides
// (parent_page_id, position).
func BuildTree(pages []Page) []*TreeNode {
	nodes := make(map[string]*TreeNode, len(pages))
	for _, p := range pages {
		nodes[p.ID] = &TreeNode{Page: p}
	}

	var roots []*TreeNode
	for _, p := range pages {
		node := nodes[p.ID]
		if p.ParentPageID == nil {
			roots = append(roots, node)
			continue
		}
		parent, ok := nodes[*p.ParentPageID]
		if !ok {
			// Parent isn't in this list -- e.g. it was archived while a
			// child wasn't. Treat the child as a root rather than
			// dropping it silently.
			roots = append(roots, node)
			continue
		}
		parent.Children = append(parent.Children, node)
	}
	return roots
}
