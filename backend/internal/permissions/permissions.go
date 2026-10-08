// Package permissions is the single place authorization decisions get
// made. Every handler that touches a space or (once it exists) a page
// goes through Checker.CanAccessSpace rather than querying
// space_members itself — see docs/ARCHITECTURE.md, "Authorization rule".
package permissions

import (
	"context"
	"database/sql"
	"fmt"

	"nook/internal/spaces"
)

// roleRank orders roles from weakest to strongest, so "does this member
// meet at least minRole" is a simple integer comparison rather than a
// hardcoded set of allowed-roles lists scattered across handlers.
// internal/groups' own Role* constants are the identical strings (same
// four roles, same meaning), so this one map serves both without a
// permissions->groups import purely for that.
var roleRank = map[string]int{
	spaces.RoleViewer: 1,
	spaces.RoleEditor: 2,
	spaces.RoleAdmin:  3,
	spaces.RoleOwner:  4,
}

type Checker struct {
	db *sql.DB
}

func NewChecker(conn *sql.DB) *Checker {
	return &Checker{db: conn}
}

// CanAccessSpace reports whether userID can access spaceID with a role
// at least as strong as minRole (one of the spaces.Role* constants) --
// either through a direct space_members row, or through membership in
// the space's own group (migrations/0009_groups.sql: inviting someone
// to a group grants them the group's role on every space inside it).
// Whichever of the two grants the stronger role wins, so a space's own
// space_members row can only ever add access on top of what the group
// already grants, never take it away.
//
// A false, nil result covers "not reachable through either path at
// all" and "reachable, but neither role is strong enough" — callers
// should treat both the same way (reject the request) and don't need
// to tell them apart, which is what keeps this the one place that
// decision gets made instead of every handler re-deriving it.
func (c *Checker) CanAccessSpace(ctx context.Context, userID, spaceID, minRole string) (bool, error) {
	required, ok := roleRank[minRole]
	if !ok {
		return false, fmt.Errorf("permissions: unknown role %q", minRole)
	}

	var spaceRole, groupRole sql.NullString
	err := c.db.QueryRowContext(ctx, `
		SELECT
			(SELECT role FROM space_members WHERE space_id = s.id AND user_id = ?),
			(SELECT gm.role FROM group_members AS gm
			 JOIN groups AS g ON g.id = gm.group_id
			 WHERE gm.group_id = s.group_id AND gm.user_id = ? AND g.archived_at IS NULL)
		FROM spaces AS s
		WHERE s.id = ?;
	`, userID, userID, spaceID).Scan(&spaceRole, &groupRole)

	if err == sql.ErrNoRows {
		return false, nil // the space itself doesn't exist
	}
	if err != nil {
		return false, fmt.Errorf("permissions: check space access: %w", err)
	}

	held, err := maxRank(spaceRole, groupRole)
	if err != nil {
		return false, err
	}
	return held >= required, nil
}

// CanAccessGroup reports whether userID is a member of groupID with a
// role at least as strong as minRole. Used for the group-management
// endpoints themselves (rename, invite, remove/change a member's role,
// ...) — a group has no further parent to inherit access from the way
// a space inherits from its group, so this is a plain single-table
// check, no OR-of-two-sources logic needed.
func (c *Checker) CanAccessGroup(ctx context.Context, userID, groupID, minRole string) (bool, error) {
	required, ok := roleRank[minRole]
	if !ok {
		return false, fmt.Errorf("permissions: unknown role %q", minRole)
	}

	var role string
	err := c.db.QueryRowContext(ctx, `
		SELECT role FROM group_members WHERE group_id = ? AND user_id = ?;
	`, groupID, userID).Scan(&role)

	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("permissions: check group access: %w", err)
	}

	held, ok := roleRank[role]
	if !ok {
		return false, fmt.Errorf("permissions: member has unrecognized role %q", role)
	}
	return held >= required, nil
}

// maxRank resolves the stronger of two possibly-absent role strings to
// its rank, 0 if neither is present. An unrecognized-but-present role
// (data corruption or a schema/code mismatch, not normal "no access")
// surfaces as an error rather than being silently treated as no access.
func maxRank(roles ...sql.NullString) (int, error) {
	held := 0
	for _, r := range roles {
		if !r.Valid {
			continue
		}
		rank, ok := roleRank[r.String]
		if !ok {
			return 0, fmt.Errorf("permissions: member has unrecognized role %q", r.String)
		}
		held = max(held, rank)
	}
	return held, nil
}
