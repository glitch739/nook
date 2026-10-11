// Package groups is the tier above internal/spaces -- the real
// invite/tenant boundary for multi-user (see migrations/0009_groups.sql
// for the full reasoning). Deliberately mirrors internal/spaces' own
// shape (Store, membership roles, position scheme) closely rather than
// sharing code with it: the two are conceptually the same "a resource
// with per-user-ordered membership" pattern, but keeping them as
// separate, independent packages means a change to one's semantics
// (e.g. spaces someday growing a feature groups shouldn't have) never
// has to thread through a shared abstraction neither fully wants.
package groups

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"nook/internal/db"
)

// Membership roles, weakest to strongest -- same small set as
// internal/spaces' own, redefined here rather than imported from it so
// this package doesn't depend on spaces for three string constants.
const (
	RoleViewer = "viewer"
	RoleEditor = "editor"
	RoleAdmin  = "admin"
	RoleOwner  = "owner"
)

// positionWidth matches every other package's own zero-padded,
// full-restamp-on-reorder scheme (internal/spaces, internal/databases,
// internal/pages).
const positionWidth = 6

// ErrLastOwner is returned by RemoveMember/UpdateMemberRole when the
// call would leave a group with zero owners -- a permanent dead end
// (no one left who could ever re-add an owner), not something any
// caller should be able to reach no matter how they're a member.
var ErrLastOwner = errors.New("groups: cannot remove or demote the last owner")

// ErrGroupHasSpaces is returned by Archive when the group still
// contains at least one non-archived space -- see Archive's own
// comment for why archiving isn't allowed to just orphan them.
var ErrGroupHasSpaces = errors.New("groups: cannot archive a group that still has spaces in it")

type Group struct {
	ID         string
	Name       string
	Icon       string
	OwnerID    string
	ArchivedAt *string
	CreatedAt  string
}

// UserGroup is a Group plus the calling user's own ordering of it --
// only ListForUser returns this, same split as spaces.UserSpace.
type UserGroup struct {
	Group
	Position string
	Role     string // the user's own role in this group
}

// Member is one group_members row joined with the user's own public
// fields, for a group's member-management UI.
type Member struct {
	UserID      string
	Username    string
	DisplayName string
	Role        string
}

type Store struct {
	db *sql.DB
}

func NewStore(conn *sql.DB) *Store {
	return &Store{db: conn}
}

func defaultIcon() string {
	return "👥"
}

// nextMemberPosition returns a position that sorts after every group
// userID already belongs to -- same "MAX + 1" idiom as
// spaces.nextMemberPosition/pages.nextPosition.
func nextMemberPosition(ctx context.Context, tx *sql.Tx, userID string) (string, error) {
	var maxPosition sql.NullString
	err := tx.QueryRowContext(ctx, `
		SELECT MAX(position) FROM group_members WHERE user_id = ?;
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

// Create inserts a new group and adds ownerID as its "owner" member, in
// a single transaction -- a group without a matching group_members row
// would be permanently inaccessible, same reasoning as
// spaces.Store.Create.
func (s *Store) Create(ctx context.Context, name, ownerID string) (Group, error) {
	id := db.NewID()

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Group{}, fmt.Errorf("groups: begin create transaction: %w", err)
	}
	defer tx.Rollback()

	_, err = tx.ExecContext(ctx, `
		INSERT INTO groups (id, name, icon, owner_id)
		VALUES (?, ?, ?, ?);
	`, id, name, defaultIcon(), ownerID)
	if err != nil {
		return Group{}, fmt.Errorf("groups: create group: %w", err)
	}

	position, err := nextMemberPosition(ctx, tx, ownerID)
	if err != nil {
		return Group{}, fmt.Errorf("groups: compute owner position: %w", err)
	}

	_, err = tx.ExecContext(ctx, `
		INSERT INTO group_members (group_id, user_id, role, position)
		VALUES (?, ?, ?, ?);
	`, id, ownerID, RoleOwner, position)
	if err != nil {
		return Group{}, fmt.Errorf("groups: add owner membership: %w", err)
	}

	var group Group
	err = tx.QueryRowContext(ctx, `
		SELECT id, name, icon, owner_id, archived_at, created_at FROM groups WHERE id = ?;
	`, id).Scan(&group.ID, &group.Name, &group.Icon, &group.OwnerID, &group.ArchivedAt, &group.CreatedAt)
	if err != nil {
		return Group{}, fmt.Errorf("groups: read created group: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return Group{}, fmt.Errorf("groups: commit create transaction: %w", err)
	}
	return group, nil
}

// ListForUser returns every non-archived group userID is a member of,
// in that user's own chosen order (ReorderForUser).
func (s *Store) ListForUser(ctx context.Context, userID string) ([]UserGroup, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT g.id, g.name, g.icon, g.owner_id, g.archived_at, g.created_at, gm.position, gm.role
		FROM groups AS g
		JOIN group_members AS gm ON gm.group_id = g.id
		WHERE gm.user_id = ? AND g.archived_at IS NULL
		ORDER BY gm.position;
	`, userID)
	if err != nil {
		return nil, fmt.Errorf("groups: list for user: %w", err)
	}
	defer rows.Close()

	result := make([]UserGroup, 0)
	for rows.Next() {
		var ug UserGroup
		if err := rows.Scan(
			&ug.ID, &ug.Name, &ug.Icon, &ug.OwnerID, &ug.ArchivedAt, &ug.CreatedAt, &ug.Position, &ug.Role,
		); err != nil {
			return nil, fmt.Errorf("groups: scan group: %w", err)
		}
		result = append(result, ug)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("groups: list rows: %w", err)
	}
	return result, nil
}

// GetByID looks up one group regardless of archived state or the
// caller's own membership -- used by authorizeGroup-style handlers
// that need the group's owner_id/archived state before they have
// anything else to check against.
func (s *Store) GetByID(ctx context.Context, id string) (Group, error) {
	var g Group
	err := s.db.QueryRowContext(ctx, `
		SELECT id, name, icon, owner_id, archived_at, created_at FROM groups WHERE id = ?;
	`, id).Scan(&g.ID, &g.Name, &g.Icon, &g.OwnerID, &g.ArchivedAt, &g.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Group{}, db.ErrNotFound
	}
	if err != nil {
		return Group{}, fmt.Errorf("groups: get by id: %w", err)
	}
	return g, nil
}

func (s *Store) UpdateName(ctx context.Context, id, name string) (Group, error) {
	res, err := s.db.ExecContext(ctx, `UPDATE groups SET name = ? WHERE id = ?;`, name, id)
	if err != nil {
		return Group{}, fmt.Errorf("groups: update name: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return Group{}, fmt.Errorf("groups: update name: %w", db.ErrNotFound)
	}
	return s.GetByID(ctx, id)
}

func (s *Store) UpdateIcon(ctx context.Context, id, icon string) (Group, error) {
	res, err := s.db.ExecContext(ctx, `UPDATE groups SET icon = ? WHERE id = ?;`, icon, id)
	if err != nil {
		return Group{}, fmt.Errorf("groups: update icon: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return Group{}, fmt.Errorf("groups: update icon: %w", db.ErrNotFound)
	}
	return s.GetByID(ctx, id)
}

// Archive soft-deletes a group, same soft-delete shape as
// spaces.Store.Archive -- refuses (ErrGroupHasSpaces) while it still
// has any non-archived space in it. Unlike archiving a space (whose
// pages just stay in the database, unreachable but harmlessly inert),
// a space with a now-archived group would still show up for anyone
// with a *direct* space_members row on it (CanAccessSpace only blocks
// the group-inherited path, not direct membership -- archiving a
// shared group shouldn't retroactively lock its creator out of a space
// they personally own), but vanish from the sidebar entirely, since
// GroupSection only ever renders a space under a group that's actually
// in the caller's own group list. Requiring the group to be empty
// first avoids that orphaned, half-reachable state outright rather
// than trying to reconcile it -- move or archive its spaces first.
// Restore brings an archived group back (archived groups are always
// empty of live spaces, see Archive).
func (s *Store) Restore(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `UPDATE groups SET archived_at = NULL WHERE id = ? AND archived_at IS NOT NULL;`, id)
	if err != nil {
		return fmt.Errorf("groups: restore: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("groups: restore: %w", db.ErrNotFound)
	}
	return nil
}

// DeleteArchived permanently deletes an archived group that no space
// (live or archived) belongs to any more.
func (s *Store) DeleteArchived(ctx context.Context, id string) error {
	var spaceCount int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM spaces WHERE group_id = ?;`, id).Scan(&spaceCount); err != nil {
		return fmt.Errorf("groups: delete: %w", err)
	}
	if spaceCount > 0 {
		return ErrGroupHasSpaces
	}
	res, err := s.db.ExecContext(ctx, `DELETE FROM groups WHERE id = ? AND archived_at IS NOT NULL;`, id)
	if err != nil {
		return fmt.Errorf("groups: delete: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("groups: delete: %w", db.ErrNotFound)
	}
	return nil
}

func (s *Store) Archive(ctx context.Context, id string) error {
	var spaceCount int
	err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM spaces WHERE group_id = ? AND archived_at IS NULL;
	`, id).Scan(&spaceCount)
	if err != nil {
		return fmt.Errorf("groups: count spaces: %w", err)
	}
	if spaceCount > 0 {
		return ErrGroupHasSpaces
	}

	res, err := s.db.ExecContext(ctx, `
		UPDATE groups
		SET archived_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
		WHERE id = ? AND archived_at IS NULL;
	`, id)
	if err != nil {
		return fmt.Errorf("groups: archive: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("groups: archive: %w", db.ErrNotFound)
	}
	return nil
}

// ReorderForUser re-stamps userID's own group_members.position for
// exactly the group IDs given, in that order -- same shape as
// spaces.Store.ReorderForUser (see its own comment for why this is
// structurally, not just policy-wise, scoped to the caller's own
// membership rows).
func (s *Store) ReorderForUser(ctx context.Context, userID string, groupIDs []string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("groups: begin reorder transaction: %w", err)
	}
	defer tx.Rollback()

	for i, id := range groupIDs {
		position := fmt.Sprintf("%0*d", positionWidth, i+1)
		res, err := tx.ExecContext(ctx, `
			UPDATE group_members SET position = ? WHERE group_id = ? AND user_id = ?;
		`, position, id, userID)
		if err != nil {
			return fmt.Errorf("groups: reorder membership: %w", err)
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return fmt.Errorf("groups: reorder membership: %w", db.ErrNotFound)
		}
	}
	return tx.Commit()
}

// AddMember adds userID to groupID with the given role -- used both to
// invite a brand-new account (after creating it) and to add an
// existing account to another group.
func (s *Store) AddMember(ctx context.Context, groupID, userID, role string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("groups: begin add member transaction: %w", err)
	}
	defer tx.Rollback()

	position, err := nextMemberPosition(ctx, tx, userID)
	if err != nil {
		return fmt.Errorf("groups: compute member position: %w", err)
	}

	_, err = tx.ExecContext(ctx, `
		INSERT INTO group_members (group_id, user_id, role, position)
		VALUES (?, ?, ?, ?);
	`, groupID, userID, role, position)
	if err != nil {
		return fmt.Errorf("groups: add member: %w", err)
	}
	return tx.Commit()
}

// ListMembers returns every member of groupID, joined with their public
// user fields, oldest-added first.
// GetMemberRole returns userID's own role in groupID -- used by the
// HTTP layer to enforce "can't grant/hold a role higher than your own"
// when inviting or re-roling someone, without reaching into this
// package's own SQL directly.
func (s *Store) GetMemberRole(ctx context.Context, groupID, userID string) (string, error) {
	var role string
	err := s.db.QueryRowContext(ctx, `
		SELECT role FROM group_members WHERE group_id = ? AND user_id = ?;
	`, groupID, userID).Scan(&role)
	if errors.Is(err, sql.ErrNoRows) {
		return "", fmt.Errorf("groups: get member role: %w", db.ErrNotFound)
	}
	if err != nil {
		return "", fmt.Errorf("groups: get member role: %w", err)
	}
	return role, nil
}

func (s *Store) ListMembers(ctx context.Context, groupID string) ([]Member, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT u.id, u.username, u.display_name, gm.role
		FROM group_members gm
		JOIN users u ON u.id = gm.user_id
		WHERE gm.group_id = ?
		ORDER BY gm.added_at, gm.rowid; -- rowid breaks same-millisecond ties in insertion order
	`, groupID)
	if err != nil {
		return nil, fmt.Errorf("groups: list members: %w", err)
	}
	defer rows.Close()

	result := make([]Member, 0)
	for rows.Next() {
		var m Member
		if err := rows.Scan(&m.UserID, &m.Username, &m.DisplayName, &m.Role); err != nil {
			return nil, fmt.Errorf("groups: scan member: %w", err)
		}
		result = append(result, m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("groups: list members rows: %w", err)
	}
	return result, nil
}

// RemoveMember removes userID from groupID -- refuses (ErrLastOwner) if
// userID is the group's only remaining owner, see ErrLastOwner's own
// comment on why that has to be a hard rule, not just a UI nudge.
func (s *Store) RemoveMember(ctx context.Context, groupID, userID string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("groups: begin remove member transaction: %w", err)
	}
	defer tx.Rollback()

	if err := refuseIfLastOwner(ctx, tx, groupID, userID); err != nil {
		return err
	}

	res, err := tx.ExecContext(ctx, `
		DELETE FROM group_members WHERE group_id = ? AND user_id = ?;
	`, groupID, userID)
	if err != nil {
		return fmt.Errorf("groups: remove member: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("groups: remove member: %w", db.ErrNotFound)
	}
	return tx.Commit()
}

// UpdateMemberRole changes userID's role in groupID -- refuses
// (ErrLastOwner) if userID is the group's only remaining owner and
// role isn't RoleOwner (i.e. this would be a demotion).
func (s *Store) UpdateMemberRole(ctx context.Context, groupID, userID, role string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("groups: begin update role transaction: %w", err)
	}
	defer tx.Rollback()

	if role != RoleOwner {
		if err := refuseIfLastOwner(ctx, tx, groupID, userID); err != nil {
			return err
		}
	}

	res, err := tx.ExecContext(ctx, `
		UPDATE group_members SET role = ? WHERE group_id = ? AND user_id = ?;
	`, role, groupID, userID)
	if err != nil {
		return fmt.Errorf("groups: update member role: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("groups: update member role: %w", db.ErrNotFound)
	}
	return tx.Commit()
}

// refuseIfLastOwner returns ErrLastOwner if userID currently holds
// RoleOwner in groupID and is the only member who does.
func refuseIfLastOwner(ctx context.Context, tx *sql.Tx, groupID, userID string) error {
	var currentRole string
	err := tx.QueryRowContext(ctx, `
		SELECT role FROM group_members WHERE group_id = ? AND user_id = ?;
	`, groupID, userID).Scan(&currentRole)
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("groups: %w", db.ErrNotFound)
	}
	if err != nil {
		return fmt.Errorf("groups: check current role: %w", err)
	}
	if currentRole != RoleOwner {
		return nil
	}

	var ownerCount int
	err = tx.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM group_members WHERE group_id = ? AND role = ?;
	`, groupID, RoleOwner).Scan(&ownerCount)
	if err != nil {
		return fmt.Errorf("groups: count owners: %w", err)
	}
	if ownerCount <= 1 {
		return ErrLastOwner
	}
	return nil
}
