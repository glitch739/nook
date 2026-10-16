package spaces

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"nook/internal/db"
)

// Space types. A "private" space belongs to exactly one user (still
// modeled through space_members like any other, for a uniform
// authorization check later); "shared" spaces can have many members.
const (
	TypePrivate = "private"
	TypeShared  = "shared"
)

// Membership roles, weakest to strongest.
const (
	RoleViewer = "viewer"
	RoleEditor = "editor"
	RoleAdmin  = "admin"
	RoleOwner  = "owner"
)

type Space struct {
	ID      string
	Name    string
	Type    string
	Icon    string
	OwnerID string
	// GroupID is the internal/groups group this space belongs to --
	// required for every space Create makes; see
	// migrations/0009_groups.sql for why the column itself is still
	// nullable at the schema level (pre-migration rows only).
	GroupID    string
	ArchivedAt *string
	CreatedAt  string
}

// UserSpace is a Space plus the calling user's own ordering of it --
// only ListForUser returns this; Position is meaningless outside that
// one user's own space_members row, so it doesn't belong on Space
// itself (which every other method here returns).
type UserSpace struct {
	Space
	Position string
}

// positionWidth matches internal/pages' and internal/databases' own
// positionWidth -- same zero-padded lexicographic scheme, full
// re-stamp on every reorder rather than fractional indexing (a user
// has few enough spaces that this is never a real cost).
const positionWidth = 6

// defaultIcon is what a space's icon starts as, matching what the
// frontend used to derive client-side from `type` alone (spaceIcon() in
// sidebarData.ts) before the icon column existed -- see
// migrations/0003_space_icon.sql. Editable afterward via UpdateIcon,
// same split as pages.Store already has between a created-with default
// and a later UpdateIcon call.
func defaultIcon(spaceType string) string {
	if spaceType == TypeShared {
		return "👥"
	}
	return "🏠"
}

type Store struct {
	db *sql.DB
}

func NewStore(conn *sql.DB) *Store {
	return &Store{db: conn}
}

// Create inserts a new space (inside groupID) and adds ownerID as its
// "owner" member, in a single transaction.
//
// The space and its owner membership must be created atomically. A space
// without a matching space_members row would be permanently inaccessible
// because authorization is based on membership. groupID isn't validated
// here -- callers (the HTTP layer) are expected to have already checked
// ownerID actually belongs to that group with a strong enough role.
func (s *Store) Create(ctx context.Context, name, spaceType, ownerID, groupID string) (Space, error) {
	id := db.NewID()

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Space{}, fmt.Errorf("spaces: begin create transaction: %w", err)
	}

	defer tx.Rollback()

	_, err = tx.ExecContext(ctx, `
		INSERT INTO spaces (id, name, type, icon, owner_id, group_id)
		VALUES (?, ?, ?, ?, ?, ?);
	`, id, name, spaceType, defaultIcon(spaceType), ownerID, groupID)
	if err != nil {
		return Space{}, fmt.Errorf("spaces: create space: %w", err)
	}

	position, err := nextMemberPosition(ctx, tx, ownerID)
	if err != nil {
		return Space{}, fmt.Errorf("spaces: compute owner position: %w", err)
	}

	_, err = tx.ExecContext(ctx, `
		INSERT INTO space_members (space_id, user_id, role, position)
		VALUES (?, ?, ?, ?);
	`, id, ownerID, RoleOwner, position)
	if err != nil {
		return Space{}, fmt.Errorf("spaces: add owner membership: %w", err)
	}

	var space Space

	err = tx.QueryRowContext(ctx, `
		SELECT id, name, type, icon, owner_id, group_id, archived_at, created_at
		FROM spaces
		WHERE id = ?;
	`, id).Scan(
		&space.ID,
		&space.Name,
		&space.Type,
		&space.Icon,
		&space.OwnerID,
		&space.GroupID,
		&space.ArchivedAt,
		&space.CreatedAt,
	)
	if err != nil {
		return Space{}, fmt.Errorf("spaces: read created space: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return Space{}, fmt.Errorf("spaces: commit create transaction: %w", err)
	}

	return space, nil
}

// nextMemberPosition returns a position that sorts after every space
// userID already has, for a newly created/joined membership row --
// same "MAX + 1" idiom as internal/pages' nextPosition.
func nextMemberPosition(ctx context.Context, tx *sql.Tx, userID string) (string, error) {
	var maxPosition sql.NullString
	err := tx.QueryRowContext(ctx, `
		SELECT MAX(position) FROM space_members WHERE user_id = ?;
	`, userID).Scan(&maxPosition)
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

// ListForUser returns every non-archived space userID can reach --
// either a direct space_members row, or membership in the space's own
// group (see migrations/0009_groups.sql; CanAccessSpace in
// internal/permissions grants access the same either-or way, this just
// has to *list* the same set CanAccessSpace would say yes to, or a
// group-invited user's sidebar would silently miss spaces they can
// actually open). Ordered by the user's own space_members.position
// where they have one (ReorderForUser); a space reachable only through
// group membership (no personal position ever set for it) has none, so
// those sort after every personally-ordered space, themselves ordered
// by creation time -- reordering one of those isn't wired up yet
// (ReorderForUser only ever updates an *existing* space_members row),
// a known, acceptable gap for now rather than auto-provisioning a
// personal position for every group member on every space up front.
func (s *Store) ListForUser(ctx context.Context, userID string) ([]UserSpace, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT DISTINCT
			s.id, s.name, s.type, s.icon, s.owner_id, s.group_id, s.archived_at, s.created_at,
			COALESCE(sm.position, so.position, '')
		FROM spaces AS s
		LEFT JOIN space_members AS sm ON sm.space_id = s.id AND sm.user_id = ?
		LEFT JOIN group_members AS gm ON gm.group_id = s.group_id AND gm.user_id = ?
		LEFT JOIN space_order AS so ON so.space_id = s.id AND so.user_id = ?
		WHERE s.archived_at IS NULL AND (sm.user_id IS NOT NULL OR gm.user_id IS NOT NULL)
		ORDER BY COALESCE(sm.position, so.position, '999999-' || s.created_at);
	`, userID, userID, userID)
	if err != nil {
		return nil, fmt.Errorf("spaces: list for user: %w", err)
	}
	defer rows.Close()

	result := make([]UserSpace, 0)

	for rows.Next() {
		var us UserSpace

		if err := rows.Scan(
			&us.ID,
			&us.Name,
			&us.Type,
			&us.Icon,
			&us.OwnerID,
			&us.GroupID,
			&us.ArchivedAt,
			&us.CreatedAt,
			&us.Position,
		); err != nil {
			return nil, fmt.Errorf("spaces: scan space: %w", err)
		}

		result = append(result, us)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("spaces: list rows: %w", err)
	}

	return result, nil
}

// ReorderForUser re-stamps userID's own space_members.position (or,
// for a space reached only through a group, space_order.position) for
// exactly the space IDs given, in that order -- same "validate as a
// permutation of the caller's current set, then full re-stamp in one
// transaction" shape as databases.Store.ReorderColumns. Scoped
// entirely to userID's own membership rows by construction (the UPDATE
// below filters on user_id, not just space_id), so this can never
// touch another user's ordering or a space the caller isn't a member
// of, regardless of what IDs are passed in -- a caller supplying an ID
// they don't have access to simply fails the permutation check in the
// HTTP handler before this is ever called, and even if it weren't
// checked there, the UPDATE's own WHERE clause would just affect zero
// rows for it.
func (s *Store) ReorderForUser(ctx context.Context, userID string, spaceIDs []string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("spaces: begin reorder transaction: %w", err)
	}
	defer tx.Rollback()

	for i, id := range spaceIDs {
		position := fmt.Sprintf("%0*d", positionWidth, i+1)
		res, err := tx.ExecContext(ctx, `
			UPDATE space_members SET position = ? WHERE space_id = ? AND user_id = ?;
		`, position, id, userID)
		if err != nil {
			return fmt.Errorf("spaces: reorder membership: %w", err)
		}
		if n, _ := res.RowsAffected(); n > 0 {
			continue
		}
		// No membership row: reached through a group (ListForUser), so
		// the position goes to space_order -- only if that group
		// membership really exists.
		res, err = tx.ExecContext(ctx, `
			INSERT INTO space_order (space_id, user_id, position)
			SELECT s.id, gm.user_id, ? FROM spaces AS s
			JOIN group_members AS gm ON gm.group_id = s.group_id AND gm.user_id = ?
			WHERE s.id = ?
			ON CONFLICT (space_id, user_id) DO UPDATE SET position = excluded.position;
		`, position, userID, id)
		if err != nil {
			return fmt.Errorf("spaces: reorder group space: %w", err)
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return fmt.Errorf("spaces: reorder membership: %w", db.ErrNotFound)
		}
	}

	return tx.Commit()
}

// GetByID looks up one space regardless of archived state -- used by
// authorizeSpace-style handlers that need the space's owner_id (e.g. to
// require RoleOwner for archiving) before they have anything else to
// check permissions against.
func (s *Store) GetByID(ctx context.Context, id string) (Space, error) {
	var space Space
	err := s.db.QueryRowContext(ctx, `
		SELECT id, name, type, icon, owner_id, group_id, archived_at, created_at
		FROM spaces
		WHERE id = ?;
	`, id).Scan(
		&space.ID,
		&space.Name,
		&space.Type,
		&space.Icon,
		&space.OwnerID,
		&space.GroupID,
		&space.ArchivedAt,
		&space.CreatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return Space{}, db.ErrNotFound
	}
	if err != nil {
		return Space{}, fmt.Errorf("spaces: get by id: %w", err)
	}
	return space, nil
}

// UpdateIcon sets a space's icon (opaque to this package, same
// treatment as pages.Store.UpdateIcon -- no validation of what counts
// as a valid icon).
func (s *Store) UpdateIcon(ctx context.Context, id, icon string) (Space, error) {
	res, err := s.db.ExecContext(ctx, `
		UPDATE spaces SET icon = ? WHERE id = ? AND archived_at IS NULL;
	`, icon, id)
	if err != nil {
		return Space{}, fmt.Errorf("spaces: update icon: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return Space{}, fmt.Errorf("spaces: update icon: %w", db.ErrNotFound)
	}
	return s.GetByID(ctx, id)
}

// UpdateName renames a space -- previously the only way to fix a
// space's name was to delete it and create a new one from scratch
// (losing every page in it, since Archive doesn't cascade). No
// validation of what counts as a valid name beyond what Create already
// requires at the HTTP layer (non-empty).
func (s *Store) UpdateName(ctx context.Context, id, name string) (Space, error) {
	res, err := s.db.ExecContext(ctx, `
		UPDATE spaces SET name = ? WHERE id = ? AND archived_at IS NULL;
	`, name, id)
	if err != nil {
		return Space{}, fmt.Errorf("spaces: update name: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return Space{}, fmt.Errorf("spaces: update name: %w", db.ErrNotFound)
	}
	return s.GetByID(ctx, id)
}

// MoveToGroup reassigns a space to a different group -- the HTTP layer
// is expected to have already checked the caller holds at least editor
// in *both* the space's current group and newGroupID (moving a space
// out of a group you can edit but into one you can't, or the reverse,
// would each be a real permission bypass). The space's own
// space_members.position is untouched: it's a per-user ordering value
// independent of which group the space currently renders under, so a
// group change doesn't invalidate it.
func (s *Store) MoveToGroup(ctx context.Context, id, newGroupID string) (Space, error) {
	res, err := s.db.ExecContext(ctx, `
		UPDATE spaces SET group_id = ? WHERE id = ? AND archived_at IS NULL;
	`, newGroupID, id)
	if err != nil {
		return Space{}, fmt.Errorf("spaces: move to group: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return Space{}, fmt.Errorf("spaces: move to group: %w", db.ErrNotFound)
	}
	return s.GetByID(ctx, id)
}

// Archive soft-deletes a space (sets archived_at) -- it stops appearing
// in ListForUser but isn't destroyed, matching pages.Store.Archive's
// approach exactly (and for the same reason: a hard DELETE would
// cascade to every page in the space via ON DELETE CASCADE, which is
// far more destructive than one archived page ever was). Its pages are
// left as they are -- still archivable/restorable individually later,
// they just stop being reachable through a space no client can list
// anymore.
// Restore brings an archived space back. Its pages were never touched
// by the space's archive, so they reappear exactly as they were. The
// caller deals with an archived group (see httpapi restoreSpaceHandler).
func (s *Store) Restore(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `UPDATE spaces SET archived_at = NULL WHERE id = ? AND archived_at IS NOT NULL;`, id)
	if err != nil {
		return fmt.Errorf("spaces: restore: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("spaces: restore: %w", db.ErrNotFound)
	}
	return nil
}

// DeleteArchived permanently deletes an archived space with all its
// pages (ON DELETE CASCADE).
func (s *Store) DeleteArchived(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM spaces WHERE id = ? AND archived_at IS NOT NULL;`, id)
	if err != nil {
		return fmt.Errorf("spaces: delete: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("spaces: delete: %w", db.ErrNotFound)
	}
	return nil
}

func (s *Store) Archive(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `
		UPDATE spaces
		SET archived_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
		WHERE id = ? AND archived_at IS NULL;
	`, id)
	if err != nil {
		return fmt.Errorf("spaces: archive: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("spaces: archive: %w", db.ErrNotFound)
	}
	return nil
}

// Person is someone who can use a space, as the "Person" database
// column offers them: id and names only, never the email address.
type Person struct {
	ID          string `json:"id"`
	Username    string `json:"username"`
	DisplayName string `json:"display_name"`
}

// People lists the active users who can reach spaceID: direct members
// plus members of its (live) group -- the same two paths
// permissions.CanAccessSpace honors.
func (s *Store) People(ctx context.Context, spaceID string) ([]Person, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT u.id, u.username, u.display_name
		FROM users u
		WHERE u.status = 'active' AND (
		      EXISTS (SELECT 1 FROM space_members sm WHERE sm.space_id = ?1 AND sm.user_id = u.id)
		   OR EXISTS (SELECT 1 FROM spaces sp
		              JOIN group_members gm ON gm.group_id = sp.group_id
		              JOIN groups g ON g.id = gm.group_id
		              WHERE sp.id = ?1 AND gm.user_id = u.id AND g.archived_at IS NULL))
		ORDER BY u.display_name COLLATE NOCASE, u.username;`, spaceID)
	if err != nil {
		return nil, fmt.Errorf("spaces: people: %w", err)
	}
	defer rows.Close()
	out := []Person{}
	for rows.Next() {
		var p Person
		if err := rows.Scan(&p.ID, &p.Username, &p.DisplayName); err != nil {
			return nil, fmt.Errorf("spaces: people: %w", err)
		}
		out = append(out, p)
	}
	return out, rows.Err()
}
