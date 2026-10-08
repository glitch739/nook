package users

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"nook/internal/db"
)

// ErrNotDisabled: only disabled accounts can be deleted.
var ErrNotDisabled = errors.New("users: account must be disabled before it can be deleted")

// DeleteDisabled permanently removes a disabled account, in one
// transaction. heirID (the admin doing it) inherits whatever of the
// account's work is shared with other people:
//
//   - Groups the account owns with no other members are deleted, with
//     all their spaces and pages: that is the account's private
//     content (for a rejected sign-up, its untouched personal space).
//   - Spaces outside any group that only the account belongs to are
//     deleted the same way.
//   - Everything else it owns or created (shared groups, spaces, pages,
//     covers, images) is reassigned to heirID, so nobody else loses
//     content.
//   - Sessions, memberships and tokens go with the row (ON DELETE
//     CASCADE); activity and edit attribution become anonymous (SET NULL).
//
// The image files of deleted pages stay on disk until the planned
// unreferenced-upload sweep (ROADMAP Phase 5).
func (s *Store) DeleteDisabled(ctx context.Context, userID, heirID string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("users: delete: %w", err)
	}
	defer tx.Rollback()

	var status string
	err = tx.QueryRowContext(ctx, `SELECT status FROM users WHERE id = ?;`, userID).Scan(&status)
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("users: delete: %w", db.ErrNotFound)
	}
	if err != nil {
		return fmt.Errorf("users: delete: %w", err)
	}
	if status != StatusDisabled {
		return ErrNotDisabled
	}

	steps := []struct{ what, q string }{
		// Private groups: owned by the account, nobody else in them.
		{"private group spaces", `DELETE FROM spaces WHERE group_id IN (
			SELECT g.id FROM groups g WHERE g.owner_id = ?1 AND NOT EXISTS (
				SELECT 1 FROM group_members m WHERE m.group_id = g.id AND m.user_id <> ?1));`},
		{"private groups", `DELETE FROM groups WHERE owner_id = ?1 AND NOT EXISTS (
			SELECT 1 FROM group_members m WHERE m.group_id = groups.id AND m.user_id <> ?1);`},
		// Legacy spaces with no group that only the account is in.
		{"private ungrouped spaces", `DELETE FROM spaces WHERE group_id IS NULL AND owner_id = ?1 AND NOT EXISTS (
			SELECT 1 FROM space_members m WHERE m.space_id = spaces.id AND m.user_id <> ?1);`},
		// Shared things survive under the heir.
		{"groups", `UPDATE groups SET owner_id = ?2 WHERE owner_id = ?1;`},
		{"spaces", `UPDATE spaces SET owner_id = ?2 WHERE owner_id = ?1;`},
		{"pages", `UPDATE pages SET created_by = ?2 WHERE created_by = ?1;`},
		{"covers", `UPDATE page_covers SET uploaded_by = ?2 WHERE uploaded_by = ?1;`},
		{"images", `UPDATE page_images SET uploaded_by = ?2 WHERE uploaded_by = ?1;`},
		{"attachments", `UPDATE page_attachments SET uploaded_by = ?2 WHERE uploaded_by = ?1;`},
		{"account", `DELETE FROM users WHERE id = ?1;`},
	}
	for _, st := range steps {
		if _, err := tx.ExecContext(ctx, st.q, userID, heirID); err != nil {
			return fmt.Errorf("users: delete (%s): %w", st.what, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("users: delete: %w", err)
	}
	return nil
}
