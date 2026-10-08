package permissions

import (
	"context"
	"testing"

	"nook/internal/dbtest"
	"nook/internal/groups"
	"nook/internal/spaces"
	"nook/internal/users"
)

func TestCanAccessSpace_OwnerMeetsEveryMinRole(t *testing.T) {
	conn := dbtest.New(t)
	ctx := context.Background()

	owner, err := users.NewStore(conn).Create(ctx, "alice", "hash", "Alice")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	group, err := groups.NewStore(conn).Create(ctx, "Alice's Group", owner.ID)
	if err != nil {
		t.Fatalf("create group: %v", err)
	}
	space, err := spaces.NewStore(conn).Create(ctx, "Alice's Space", spaces.TypePrivate, owner.ID, group.ID)
	if err != nil {
		t.Fatalf("create space: %v", err)
	}

	checker := NewChecker(conn)
	for _, minRole := range []string{spaces.RoleViewer, spaces.RoleEditor, spaces.RoleAdmin, spaces.RoleOwner} {
		ok, err := checker.CanAccessSpace(ctx, owner.ID, space.ID, minRole)
		if err != nil {
			t.Fatalf("CanAccessSpace(minRole=%s): %v", minRole, err)
		}
		if !ok {
			t.Errorf("owner should satisfy minRole=%s, got false", minRole)
		}
	}
}

func TestCanAccessSpace_NonMemberIsDenied(t *testing.T) {
	conn := dbtest.New(t)
	ctx := context.Background()

	userStore := users.NewStore(conn)
	owner, err := userStore.Create(ctx, "alice", "hash", "Alice")
	if err != nil {
		t.Fatalf("create owner: %v", err)
	}
	outsider, err := userStore.Create(ctx, "mallory", "hash", "Mallory")
	if err != nil {
		t.Fatalf("create outsider: %v", err)
	}
	group, err := groups.NewStore(conn).Create(ctx, "Alice's Group", owner.ID)
	if err != nil {
		t.Fatalf("create group: %v", err)
	}
	space, err := spaces.NewStore(conn).Create(ctx, "Alice's Space", spaces.TypePrivate, owner.ID, group.ID)
	if err != nil {
		t.Fatalf("create space: %v", err)
	}

	ok, err := NewChecker(conn).CanAccessSpace(ctx, outsider.ID, space.ID, spaces.RoleViewer)
	if err != nil {
		t.Fatalf("CanAccessSpace: %v", err)
	}
	if ok {
		t.Fatal("non-member was granted access")
	}
}

func TestCanAccessSpace_RoleBelowMinRoleIsDenied(t *testing.T) {
	conn := dbtest.New(t)
	ctx := context.Background()

	userStore := users.NewStore(conn)
	owner, err := userStore.Create(ctx, "alice", "hash", "Alice")
	if err != nil {
		t.Fatalf("create owner: %v", err)
	}
	viewer, err := userStore.Create(ctx, "bob", "hash", "Bob")
	if err != nil {
		t.Fatalf("create viewer: %v", err)
	}
	group, err := groups.NewStore(conn).Create(ctx, "Alice's Group", owner.ID)
	if err != nil {
		t.Fatalf("create group: %v", err)
	}
	space, err := spaces.NewStore(conn).Create(ctx, "Shared Space", spaces.TypeShared, owner.ID, group.ID)
	if err != nil {
		t.Fatalf("create space: %v", err)
	}

	if _, err := conn.ExecContext(ctx,
		`INSERT INTO space_members (space_id, user_id, role) VALUES (?, ?, ?);`,
		space.ID, viewer.ID, spaces.RoleViewer,
	); err != nil {
		t.Fatalf("add viewer membership: %v", err)
	}

	checker := NewChecker(conn)

	ok, err := checker.CanAccessSpace(ctx, viewer.ID, space.ID, spaces.RoleViewer)
	if err != nil {
		t.Fatalf("CanAccessSpace(minRole=viewer): %v", err)
	}
	if !ok {
		t.Fatal("viewer should satisfy minRole=viewer")
	}

	ok, err = checker.CanAccessSpace(ctx, viewer.ID, space.ID, spaces.RoleEditor)
	if err != nil {
		t.Fatalf("CanAccessSpace(minRole=editor): %v", err)
	}
	if ok {
		t.Fatal("viewer should NOT satisfy minRole=editor")
	}
}

func TestCanAccessSpace_UnknownMinRoleIsAnError(t *testing.T) {
	conn := dbtest.New(t)
	ctx := context.Background()

	user, err := users.NewStore(conn).Create(ctx, "alice", "hash", "Alice")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	group, err := groups.NewStore(conn).Create(ctx, "Alice's Group", user.ID)
	if err != nil {
		t.Fatalf("create group: %v", err)
	}
	space, err := spaces.NewStore(conn).Create(ctx, "Alice's Space", spaces.TypePrivate, user.ID, group.ID)
	if err != nil {
		t.Fatalf("create space: %v", err)
	}

	_, err = NewChecker(conn).CanAccessSpace(ctx, user.ID, space.ID, "superadmin")
	if err == nil {
		t.Fatal("expected an error for an unrecognized minRole, got nil")
	}
}

func TestCanAccessSpace_UnknownSpaceIsDeniedNotError(t *testing.T) {
	conn := dbtest.New(t)
	ctx := context.Background()

	user, err := users.NewStore(conn).Create(ctx, "alice", "hash", "Alice")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	ok, err := NewChecker(conn).CanAccessSpace(ctx, user.ID, "does-not-exist", spaces.RoleViewer)
	if err != nil {
		t.Fatalf("CanAccessSpace: %v", err)
	}
	if ok {
		t.Fatal("access to a nonexistent space should be denied, not granted")
	}
}

// TestCanAccessSpace_GroupMembershipAloneGrantsAccess is the actual
// point of migrations/0009_groups.sql: someone invited to a group, with
// no space_members row of their own for any specific space inside it,
// still has to be able to open those spaces.
func TestCanAccessSpace_GroupMembershipAloneGrantsAccess(t *testing.T) {
	conn := dbtest.New(t)
	ctx := context.Background()

	userStore := users.NewStore(conn)
	owner, err := userStore.Create(ctx, "alice", "hash", "Alice")
	if err != nil {
		t.Fatalf("create owner: %v", err)
	}
	invitee, err := userStore.Create(ctx, "bob", "hash", "Bob")
	if err != nil {
		t.Fatalf("create invitee: %v", err)
	}

	groupStore := groups.NewStore(conn)
	group, err := groupStore.Create(ctx, "Shared Group", owner.ID)
	if err != nil {
		t.Fatalf("create group: %v", err)
	}
	if err := groupStore.AddMember(ctx, group.ID, invitee.ID, groups.RoleEditor); err != nil {
		t.Fatalf("add invitee to group: %v", err)
	}

	space, err := spaces.NewStore(conn).Create(ctx, "Team Space", spaces.TypeShared, owner.ID, group.ID)
	if err != nil {
		t.Fatalf("create space: %v", err)
	}

	checker := NewChecker(conn)

	ok, err := checker.CanAccessSpace(ctx, invitee.ID, space.ID, spaces.RoleEditor)
	if err != nil {
		t.Fatalf("CanAccessSpace(minRole=editor): %v", err)
	}
	if !ok {
		t.Fatal("group-editor should satisfy minRole=editor via group membership alone")
	}

	ok, err = checker.CanAccessSpace(ctx, invitee.ID, space.ID, spaces.RoleAdmin)
	if err != nil {
		t.Fatalf("CanAccessSpace(minRole=admin): %v", err)
	}
	if ok {
		t.Fatal("group-editor should NOT satisfy minRole=admin")
	}
}

// TestCanAccessSpace_StrongerOfSpaceAndGroupRoleWins covers both
// directions of the "whichever grants more wins" rule: a space-level
// role stronger than the group's own, and the reverse.
func TestCanAccessSpace_StrongerOfSpaceAndGroupRoleWins(t *testing.T) {
	conn := dbtest.New(t)
	ctx := context.Background()

	userStore := users.NewStore(conn)
	owner, err := userStore.Create(ctx, "alice", "hash", "Alice")
	if err != nil {
		t.Fatalf("create owner: %v", err)
	}
	member, err := userStore.Create(ctx, "bob", "hash", "Bob")
	if err != nil {
		t.Fatalf("create member: %v", err)
	}

	groupStore := groups.NewStore(conn)
	group, err := groupStore.Create(ctx, "Shared Group", owner.ID)
	if err != nil {
		t.Fatalf("create group: %v", err)
	}
	// bob is only a viewer at the group level ...
	if err := groupStore.AddMember(ctx, group.ID, member.ID, groups.RoleViewer); err != nil {
		t.Fatalf("add member to group: %v", err)
	}

	space, err := spaces.NewStore(conn).Create(ctx, "Team Space", spaces.TypeShared, owner.ID, group.ID)
	if err != nil {
		t.Fatalf("create space: %v", err)
	}
	// ... but individually promoted to editor on this one space.
	if _, err := conn.ExecContext(ctx,
		`INSERT INTO space_members (space_id, user_id, role) VALUES (?, ?, ?);`,
		space.ID, member.ID, spaces.RoleEditor,
	); err != nil {
		t.Fatalf("add space-level membership: %v", err)
	}

	checker := NewChecker(conn)
	ok, err := checker.CanAccessSpace(ctx, member.ID, space.ID, spaces.RoleEditor)
	if err != nil {
		t.Fatalf("CanAccessSpace(minRole=editor): %v", err)
	}
	if !ok {
		t.Fatal("the stronger space-level role should win over the weaker group-level one")
	}
}

func TestCanAccessGroup_OwnerMeetsEveryMinRole(t *testing.T) {
	conn := dbtest.New(t)
	ctx := context.Background()

	owner, err := users.NewStore(conn).Create(ctx, "alice", "hash", "Alice")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	group, err := groups.NewStore(conn).Create(ctx, "Alice's Group", owner.ID)
	if err != nil {
		t.Fatalf("create group: %v", err)
	}

	checker := NewChecker(conn)
	for _, minRole := range []string{groups.RoleViewer, groups.RoleEditor, groups.RoleAdmin, groups.RoleOwner} {
		ok, err := checker.CanAccessGroup(ctx, owner.ID, group.ID, minRole)
		if err != nil {
			t.Fatalf("CanAccessGroup(minRole=%s): %v", minRole, err)
		}
		if !ok {
			t.Errorf("owner should satisfy minRole=%s, got false", minRole)
		}
	}
}

func TestCanAccessGroup_NonMemberIsDenied(t *testing.T) {
	conn := dbtest.New(t)
	ctx := context.Background()

	userStore := users.NewStore(conn)
	owner, err := userStore.Create(ctx, "alice", "hash", "Alice")
	if err != nil {
		t.Fatalf("create owner: %v", err)
	}
	outsider, err := userStore.Create(ctx, "mallory", "hash", "Mallory")
	if err != nil {
		t.Fatalf("create outsider: %v", err)
	}
	group, err := groups.NewStore(conn).Create(ctx, "Alice's Group", owner.ID)
	if err != nil {
		t.Fatalf("create group: %v", err)
	}

	ok, err := NewChecker(conn).CanAccessGroup(ctx, outsider.ID, group.ID, groups.RoleViewer)
	if err != nil {
		t.Fatalf("CanAccessGroup: %v", err)
	}
	if ok {
		t.Fatal("non-member was granted access")
	}
}

func TestCanAccessGroup_UnknownGroupIsDeniedNotError(t *testing.T) {
	conn := dbtest.New(t)
	ctx := context.Background()

	user, err := users.NewStore(conn).Create(ctx, "alice", "hash", "Alice")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	ok, err := NewChecker(conn).CanAccessGroup(ctx, user.ID, "does-not-exist", groups.RoleViewer)
	if err != nil {
		t.Fatalf("CanAccessGroup: %v", err)
	}
	if ok {
		t.Fatal("access to a nonexistent group should be denied, not granted")
	}
}
