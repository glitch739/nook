package spaces

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"nook/internal/db"
	"nook/internal/dbtest"
	"nook/internal/groups"
	"nook/internal/users"
)

// testGroup creates a group owned by ownerID and returns its id --
// every space now has to live in one (migrations/0009_groups.sql), so
// every test that creates a space needs a real group row to satisfy
// spaces.group_id's foreign key (SQLite foreign key enforcement is on,
// see internal/db.Open's own DSN).
func testGroup(t *testing.T, conn *sql.DB, ownerID string) string {
	t.Helper()
	g, err := groups.NewStore(conn).Create(context.Background(), "Test Group", ownerID)
	if err != nil {
		t.Fatalf("create test group: %v", err)
	}
	return g.ID
}

func TestSpaceStore_CreateAddsOwnerMembership(t *testing.T) {
	conn := dbtest.New(t)
	owner, err := users.NewStore(conn).Create(context.Background(), "alice", "hash", "Alice")
	if err != nil {
		t.Fatalf("create test user: %v", err)
	}
	groupID := testGroup(t, conn, owner.ID)

	store := NewStore(conn)
	space, err := store.Create(context.Background(), "Alice's Notes", TypePrivate, owner.ID, groupID)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if space.Name != "Alice's Notes" || space.Type != TypePrivate || space.OwnerID != owner.ID {
		t.Fatalf("unexpected space: %+v", space)
	}
	if space.GroupID != groupID {
		t.Fatalf("GroupID = %q, want %q", space.GroupID, groupID)
	}
	if space.Icon != "🏠" {
		t.Fatalf("Icon = %q, want the private-space default %q", space.Icon, "🏠")
	}

	// The real point of this test: creating a space must also create the
	// owner's membership row, in the same operation. Verified indirectly
	// through ListForUser, which only returns spaces reachable via
	// space_members (or group membership, see the group-inherited test
	// below).
	list, err := store.ListForUser(context.Background(), owner.ID)
	if err != nil {
		t.Fatalf("ListForUser: %v", err)
	}
	if len(list) != 1 || list[0].ID != space.ID {
		t.Fatalf("ListForUser = %+v, want exactly the space just created", list)
	}
}

func TestSpaceStore_ListForUser_OnlyReturnsMemberSpaces(t *testing.T) {
	conn := dbtest.New(t)
	userStore := users.NewStore(conn)
	alice, err := userStore.Create(context.Background(), "alice", "hash", "Alice")
	if err != nil {
		t.Fatalf("create alice: %v", err)
	}
	bob, err := userStore.Create(context.Background(), "bob", "hash", "Bob")
	if err != nil {
		t.Fatalf("create bob: %v", err)
	}
	aliceGroup := testGroup(t, conn, alice.ID)
	bobGroup := testGroup(t, conn, bob.ID)

	store := NewStore(conn)
	if _, err := store.Create(context.Background(), "Alice's Private Space", TypePrivate, alice.ID, aliceGroup); err != nil {
		t.Fatalf("create alice's space: %v", err)
	}
	if _, err := store.Create(context.Background(), "Bob's Private Space", TypePrivate, bob.ID, bobGroup); err != nil {
		t.Fatalf("create bob's space: %v", err)
	}

	aliceSpaces, err := store.ListForUser(context.Background(), alice.ID)
	if err != nil {
		t.Fatalf("ListForUser(alice): %v", err)
	}
	if len(aliceSpaces) != 1 || aliceSpaces[0].Name != "Alice's Private Space" {
		t.Fatalf("alice can see bob's private space (or something else is wrong): %+v", aliceSpaces)
	}
}

// TestSpaceStore_ListForUser_IncludesGroupInheritedSpaces is the actual
// point of the group layer: someone invited to a *group* (not
// individually to one of its spaces) still needs to see that group's
// spaces in their own list -- otherwise CanAccessSpace would let them
// open a space by id that their own sidebar never showed them.
func TestSpaceStore_ListForUser_IncludesGroupInheritedSpaces(t *testing.T) {
	conn := dbtest.New(t)
	userStore := users.NewStore(conn)
	owner, err := userStore.Create(context.Background(), "alice", "hash", "Alice")
	if err != nil {
		t.Fatalf("create owner: %v", err)
	}
	invitee, err := userStore.Create(context.Background(), "bob", "hash", "Bob")
	if err != nil {
		t.Fatalf("create invitee: %v", err)
	}

	groupStore := groups.NewStore(conn)
	group, err := groupStore.Create(context.Background(), "Shared Group", owner.ID)
	if err != nil {
		t.Fatalf("create group: %v", err)
	}
	if err := groupStore.AddMember(context.Background(), group.ID, invitee.ID, groups.RoleEditor); err != nil {
		t.Fatalf("add invitee to group: %v", err)
	}

	store := NewStore(conn)
	space, err := store.Create(context.Background(), "Team Space", TypeShared, owner.ID, group.ID)
	if err != nil {
		t.Fatalf("create space: %v", err)
	}

	// invitee never got a direct space_members row -- only the group
	// membership above.
	list, err := store.ListForUser(context.Background(), invitee.ID)
	if err != nil {
		t.Fatalf("ListForUser(invitee): %v", err)
	}
	if len(list) != 1 || list[0].ID != space.ID {
		t.Fatalf("ListForUser(invitee) = %+v, want the group's space via inherited access", list)
	}
	if list[0].Position != "" {
		t.Fatalf("Position = %q, want empty (no personal space_members row exists)", list[0].Position)
	}
}

func TestSpaceStore_Create_SharedDefaultIcon(t *testing.T) {
	conn := dbtest.New(t)
	owner, err := users.NewStore(conn).Create(context.Background(), "alice", "hash", "Alice")
	if err != nil {
		t.Fatalf("create test user: %v", err)
	}
	groupID := testGroup(t, conn, owner.ID)

	space, err := NewStore(conn).Create(context.Background(), "Team Space", TypeShared, owner.ID, groupID)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if space.Icon != "👥" {
		t.Fatalf("Icon = %q, want the shared-space default %q", space.Icon, "👥")
	}
}

func TestSpaceStore_UpdateIcon(t *testing.T) {
	conn := dbtest.New(t)
	owner, err := users.NewStore(conn).Create(context.Background(), "alice", "hash", "Alice")
	if err != nil {
		t.Fatalf("create test user: %v", err)
	}
	groupID := testGroup(t, conn, owner.ID)
	store := NewStore(conn)
	space, err := store.Create(context.Background(), "Alice's Notes", TypePrivate, owner.ID, groupID)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	updated, err := store.UpdateIcon(context.Background(), space.ID, "🚀")
	if err != nil {
		t.Fatalf("UpdateIcon: %v", err)
	}
	if updated.Icon != "🚀" {
		t.Fatalf("Icon = %q, want %q", updated.Icon, "🚀")
	}
}

func TestSpaceStore_UpdateIcon_UnknownIsNotFound(t *testing.T) {
	conn := dbtest.New(t)
	_, err := NewStore(conn).UpdateIcon(context.Background(), "does-not-exist", "🚀")
	if !errors.Is(err, db.ErrNotFound) {
		t.Fatalf("UpdateIcon(unknown) error = %v, want wrapping db.ErrNotFound", err)
	}
}

func TestSpaceStore_UpdateName(t *testing.T) {
	conn := dbtest.New(t)
	owner, err := users.NewStore(conn).Create(context.Background(), "alice", "hash", "Alice")
	if err != nil {
		t.Fatalf("create test user: %v", err)
	}
	groupID := testGroup(t, conn, owner.ID)
	store := NewStore(conn)
	space, err := store.Create(context.Background(), "Alice's Notes", TypePrivate, owner.ID, groupID)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	updated, err := store.UpdateName(context.Background(), space.ID, "Renamed Space")
	if err != nil {
		t.Fatalf("UpdateName: %v", err)
	}
	if updated.Name != "Renamed Space" {
		t.Fatalf("Name = %q, want %q", updated.Name, "Renamed Space")
	}
}

func TestSpaceStore_UpdateName_UnknownIsNotFound(t *testing.T) {
	conn := dbtest.New(t)
	_, err := NewStore(conn).UpdateName(context.Background(), "does-not-exist", "New Name")
	if !errors.Is(err, db.ErrNotFound) {
		t.Fatalf("UpdateName(unknown) error = %v, want wrapping db.ErrNotFound", err)
	}
}

func TestSpaceStore_Archive_RemovesFromListForUser(t *testing.T) {
	conn := dbtest.New(t)
	owner, err := users.NewStore(conn).Create(context.Background(), "alice", "hash", "Alice")
	if err != nil {
		t.Fatalf("create test user: %v", err)
	}
	groupID := testGroup(t, conn, owner.ID)
	store := NewStore(conn)
	space, err := store.Create(context.Background(), "Alice's Notes", TypePrivate, owner.ID, groupID)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if err := store.Archive(context.Background(), space.ID); err != nil {
		t.Fatalf("Archive: %v", err)
	}

	list, err := store.ListForUser(context.Background(), owner.ID)
	if err != nil {
		t.Fatalf("ListForUser: %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("expected the archived space to be excluded, got %+v", list)
	}
}

func TestSpaceStore_Archive_IsIdempotentlyRejectedTwice(t *testing.T) {
	conn := dbtest.New(t)
	owner, err := users.NewStore(conn).Create(context.Background(), "alice", "hash", "Alice")
	if err != nil {
		t.Fatalf("create test user: %v", err)
	}
	groupID := testGroup(t, conn, owner.ID)
	store := NewStore(conn)
	space, err := store.Create(context.Background(), "Alice's Notes", TypePrivate, owner.ID, groupID)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if err := store.Archive(context.Background(), space.ID); err != nil {
		t.Fatalf("first Archive: %v", err)
	}
	if err := store.Archive(context.Background(), space.ID); !errors.Is(err, db.ErrNotFound) {
		t.Fatalf("second Archive error = %v, want wrapping db.ErrNotFound", err)
	}
}

func TestSpaceStore_Archive_UnknownIsNotFound(t *testing.T) {
	conn := dbtest.New(t)
	err := NewStore(conn).Archive(context.Background(), "does-not-exist")
	if !errors.Is(err, db.ErrNotFound) {
		t.Fatalf("Archive(unknown) error = %v, want wrapping db.ErrNotFound", err)
	}
}

func TestSpaceStore_ListForUser_NoSpacesIsEmptyNotError(t *testing.T) {
	conn := dbtest.New(t)
	user, err := users.NewStore(conn).Create(context.Background(), "alice", "hash", "Alice")
	if err != nil {
		t.Fatalf("create test user: %v", err)
	}

	list, err := NewStore(conn).ListForUser(context.Background(), user.ID)
	if err != nil {
		t.Fatalf("ListForUser: %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("expected no spaces, got %+v", list)
	}
}

func TestSpaceStore_ReorderForUser(t *testing.T) {
	conn := dbtest.New(t)
	owner, err := users.NewStore(conn).Create(context.Background(), "alice", "hash", "Alice")
	if err != nil {
		t.Fatalf("create test user: %v", err)
	}
	groupID := testGroup(t, conn, owner.ID)

	store := NewStore(conn)
	a, err := store.Create(context.Background(), "A", TypePrivate, owner.ID, groupID)
	if err != nil {
		t.Fatalf("create A: %v", err)
	}
	b, err := store.Create(context.Background(), "B", TypePrivate, owner.ID, groupID)
	if err != nil {
		t.Fatalf("create B: %v", err)
	}
	c, err := store.Create(context.Background(), "C", TypePrivate, owner.ID, groupID)
	if err != nil {
		t.Fatalf("create C: %v", err)
	}

	// Creation order (and therefore position, per nextMemberPosition) is
	// A, B, C -- reorder to C, A, B and confirm ListForUser reflects it.
	if err := store.ReorderForUser(context.Background(), owner.ID, []string{c.ID, a.ID, b.ID}); err != nil {
		t.Fatalf("ReorderForUser: %v", err)
	}

	list, err := store.ListForUser(context.Background(), owner.ID)
	if err != nil {
		t.Fatalf("ListForUser: %v", err)
	}
	if len(list) != 3 || list[0].ID != c.ID || list[1].ID != a.ID || list[2].ID != b.ID {
		t.Fatalf("ListForUser after reorder = %+v, want [C, A, B]", list)
	}
}

// A space bob reaches only through alice's group has no space_members
// row of his; reordering it must work (it 500'd) without making him a
// direct member.
func TestSpaceStore_ReorderForUser_GroupInheritedSpace(t *testing.T) {
	ctx := context.Background()
	conn := dbtest.New(t)
	userStore := users.NewStore(conn)
	alice, err := userStore.Create(ctx, "alice", "hash", "Alice")
	if err != nil {
		t.Fatalf("create alice: %v", err)
	}
	bob, err := userStore.Create(ctx, "bob", "hash", "Bob")
	if err != nil {
		t.Fatalf("create bob: %v", err)
	}
	aliceGroup := testGroup(t, conn, alice.ID)
	if err := groups.NewStore(conn).AddMember(ctx, aliceGroup, bob.ID, groups.RoleEditor); err != nil {
		t.Fatalf("add bob to group: %v", err)
	}

	store := NewStore(conn)
	shared, err := store.Create(ctx, "Shared", TypePrivate, alice.ID, aliceGroup)
	if err != nil {
		t.Fatalf("create shared: %v", err)
	}
	own, err := store.Create(ctx, "Own", TypePrivate, bob.ID, testGroup(t, conn, bob.ID))
	if err != nil {
		t.Fatalf("create own: %v", err)
	}

	for _, order := range [][]string{{shared.ID, own.ID}, {own.ID, shared.ID}} {
		if err := store.ReorderForUser(ctx, bob.ID, order); err != nil {
			t.Fatalf("ReorderForUser(%v): %v", order, err)
		}
		list, err := store.ListForUser(ctx, bob.ID)
		if err != nil {
			t.Fatalf("ListForUser: %v", err)
		}
		if len(list) != 2 || list[0].ID != order[0] || list[1].ID != order[1] {
			t.Fatalf("ListForUser after reorder = %+v, want %v", list, order)
		}
	}

	var n int
	if err := conn.QueryRow(`SELECT COUNT(*) FROM space_members WHERE space_id = ? AND user_id = ?`, shared.ID, bob.ID).Scan(&n); err != nil || n != 0 {
		t.Fatalf("bob got a direct membership on the shared space (n=%d, err=%v)", n, err)
	}
}

func TestSpaceStore_ReorderForUser_CannotTouchAnotherUsersSpace(t *testing.T) {
	conn := dbtest.New(t)
	userStore := users.NewStore(conn)
	alice, err := userStore.Create(context.Background(), "alice", "hash", "Alice")
	if err != nil {
		t.Fatalf("create alice: %v", err)
	}
	bob, err := userStore.Create(context.Background(), "bob", "hash", "Bob")
	if err != nil {
		t.Fatalf("create bob: %v", err)
	}
	bobGroup := testGroup(t, conn, bob.ID)

	store := NewStore(conn)
	bobSpace, err := store.Create(context.Background(), "Bob's Space", TypePrivate, bob.ID, bobGroup)
	if err != nil {
		t.Fatalf("create bob's space: %v", err)
	}

	// Alice has no spaces of her own -- passing bob's space id should
	// affect zero rows (the UPDATE's WHERE clause is scoped to
	// user_id = alice), not silently succeed or reorder bob's space.
	err = store.ReorderForUser(context.Background(), alice.ID, []string{bobSpace.ID})
	if !errors.Is(err, db.ErrNotFound) {
		t.Fatalf("ReorderForUser(alice, [bob's space]) error = %v, want wrapping db.ErrNotFound", err)
	}

	bobList, err := store.ListForUser(context.Background(), bob.ID)
	if err != nil {
		t.Fatalf("ListForUser(bob): %v", err)
	}
	if len(bobList) != 1 || bobList[0].Position != "000001" {
		t.Fatalf("bob's own space ordering changed: %+v", bobList)
	}
}

func TestSpaceStore_MoveToGroup(t *testing.T) {
	conn := dbtest.New(t)
	owner, err := users.NewStore(conn).Create(context.Background(), "alice", "hash", "Alice")
	if err != nil {
		t.Fatalf("create test user: %v", err)
	}

	store := NewStore(conn)
	groupA := testGroup(t, conn, owner.ID)
	groupB := testGroup(t, conn, owner.ID)
	space, err := store.Create(context.Background(), "Alice's Space", TypePrivate, owner.ID, groupA)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	updated, err := store.MoveToGroup(context.Background(), space.ID, groupB)
	if err != nil {
		t.Fatalf("MoveToGroup: %v", err)
	}
	if updated.GroupID != groupB {
		t.Fatalf("GroupID = %q, want %q", updated.GroupID, groupB)
	}

	reloaded, err := store.GetByID(context.Background(), space.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if reloaded.GroupID != groupB {
		t.Fatalf("GetByID after move: GroupID = %q, want %q", reloaded.GroupID, groupB)
	}
}

func TestSpaceStore_MoveToGroup_UnknownIsNotFound(t *testing.T) {
	conn := dbtest.New(t)
	owner, err := users.NewStore(conn).Create(context.Background(), "alice", "hash", "Alice")
	if err != nil {
		t.Fatalf("create test user: %v", err)
	}
	group := testGroup(t, conn, owner.ID)

	_, err = NewStore(conn).MoveToGroup(context.Background(), "does-not-exist", group)
	if !errors.Is(err, db.ErrNotFound) {
		t.Fatalf("MoveToGroup(unknown space) error = %v, want wrapping db.ErrNotFound", err)
	}
}
