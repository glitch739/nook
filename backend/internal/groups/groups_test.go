package groups

import (
	"context"
	"errors"
	"testing"

	"nook/internal/db"
	"nook/internal/dbtest"
	"nook/internal/spaces"
	"nook/internal/users"
)

func TestStore_CreateAddsOwnerMembership(t *testing.T) {
	conn := dbtest.New(t)
	ctx := context.Background()
	owner, err := users.NewStore(conn).Create(ctx, "alice", "hash", "Alice")
	if err != nil {
		t.Fatalf("create test user: %v", err)
	}

	store := NewStore(conn)
	g, err := store.Create(ctx, "Alice's Group", owner.ID)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if g.Name != "Alice's Group" || g.OwnerID != owner.ID {
		t.Fatalf("unexpected group: %+v", g)
	}
	if g.Icon != "👥" {
		t.Fatalf("Icon = %q, want default %q", g.Icon, "👥")
	}

	list, err := store.ListForUser(ctx, owner.ID)
	if err != nil {
		t.Fatalf("ListForUser: %v", err)
	}
	if len(list) != 1 || list[0].ID != g.ID {
		t.Fatalf("ListForUser = %+v, want exactly the group just created", list)
	}

	role, err := store.GetMemberRole(ctx, g.ID, owner.ID)
	if err != nil {
		t.Fatalf("GetMemberRole: %v", err)
	}
	if role != RoleOwner {
		t.Fatalf("owner's role = %q, want %q", role, RoleOwner)
	}
}

func TestStore_ListForUser_OnlyReturnsMemberGroups(t *testing.T) {
	conn := dbtest.New(t)
	ctx := context.Background()
	userStore := users.NewStore(conn)
	alice, err := userStore.Create(ctx, "alice", "hash", "Alice")
	if err != nil {
		t.Fatalf("create alice: %v", err)
	}
	bob, err := userStore.Create(ctx, "bob", "hash", "Bob")
	if err != nil {
		t.Fatalf("create bob: %v", err)
	}

	store := NewStore(conn)
	if _, err := store.Create(ctx, "Alice's Group", alice.ID); err != nil {
		t.Fatalf("create alice's group: %v", err)
	}
	if _, err := store.Create(ctx, "Bob's Group", bob.ID); err != nil {
		t.Fatalf("create bob's group: %v", err)
	}

	aliceGroups, err := store.ListForUser(ctx, alice.ID)
	if err != nil {
		t.Fatalf("ListForUser(alice): %v", err)
	}
	if len(aliceGroups) != 1 || aliceGroups[0].Name != "Alice's Group" {
		t.Fatalf("alice can see bob's group (or something else is wrong): %+v", aliceGroups)
	}
}

func TestStore_ReorderForUser(t *testing.T) {
	conn := dbtest.New(t)
	ctx := context.Background()
	owner, err := users.NewStore(conn).Create(ctx, "alice", "hash", "Alice")
	if err != nil {
		t.Fatalf("create test user: %v", err)
	}

	store := NewStore(conn)
	a, err := store.Create(ctx, "A", owner.ID)
	if err != nil {
		t.Fatalf("create A: %v", err)
	}
	b, err := store.Create(ctx, "B", owner.ID)
	if err != nil {
		t.Fatalf("create B: %v", err)
	}
	c, err := store.Create(ctx, "C", owner.ID)
	if err != nil {
		t.Fatalf("create C: %v", err)
	}

	if err := store.ReorderForUser(ctx, owner.ID, []string{c.ID, a.ID, b.ID}); err != nil {
		t.Fatalf("ReorderForUser: %v", err)
	}

	list, err := store.ListForUser(ctx, owner.ID)
	if err != nil {
		t.Fatalf("ListForUser: %v", err)
	}
	if len(list) != 3 || list[0].ID != c.ID || list[1].ID != a.ID || list[2].ID != b.ID {
		t.Fatalf("ListForUser after reorder = %+v, want [C, A, B]", list)
	}
}

func TestStore_ReorderForUser_CannotTouchAnotherUsersGroup(t *testing.T) {
	conn := dbtest.New(t)
	ctx := context.Background()
	userStore := users.NewStore(conn)
	alice, err := userStore.Create(ctx, "alice", "hash", "Alice")
	if err != nil {
		t.Fatalf("create alice: %v", err)
	}
	bob, err := userStore.Create(ctx, "bob", "hash", "Bob")
	if err != nil {
		t.Fatalf("create bob: %v", err)
	}

	store := NewStore(conn)
	bobGroup, err := store.Create(ctx, "Bob's Group", bob.ID)
	if err != nil {
		t.Fatalf("create bob's group: %v", err)
	}

	err = store.ReorderForUser(ctx, alice.ID, []string{bobGroup.ID})
	if !errors.Is(err, db.ErrNotFound) {
		t.Fatalf("ReorderForUser(alice, [bob's group]) error = %v, want wrapping db.ErrNotFound", err)
	}
}

func TestStore_AddMemberAndListMembers(t *testing.T) {
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

	store := NewStore(conn)
	g, err := store.Create(ctx, "Shared Group", owner.ID)
	if err != nil {
		t.Fatalf("create group: %v", err)
	}
	if err := store.AddMember(ctx, g.ID, invitee.ID, RoleEditor); err != nil {
		t.Fatalf("AddMember: %v", err)
	}

	members, err := store.ListMembers(ctx, g.ID)
	if err != nil {
		t.Fatalf("ListMembers: %v", err)
	}
	if len(members) != 2 {
		t.Fatalf("ListMembers = %+v, want owner + invitee", members)
	}
	if members[0].UserID != owner.ID || members[0].Role != RoleOwner {
		t.Fatalf("members[0] = %+v, want owner", members[0])
	}
	if members[1].UserID != invitee.ID || members[1].Role != RoleEditor || members[1].Username != "bob" {
		t.Fatalf("members[1] = %+v, want invitee as editor", members[1])
	}
}

func TestStore_RemoveMember(t *testing.T) {
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

	store := NewStore(conn)
	g, err := store.Create(ctx, "Shared Group", owner.ID)
	if err != nil {
		t.Fatalf("create group: %v", err)
	}
	if err := store.AddMember(ctx, g.ID, member.ID, RoleViewer); err != nil {
		t.Fatalf("AddMember: %v", err)
	}
	if err := store.RemoveMember(ctx, g.ID, member.ID); err != nil {
		t.Fatalf("RemoveMember: %v", err)
	}

	members, err := store.ListMembers(ctx, g.ID)
	if err != nil {
		t.Fatalf("ListMembers: %v", err)
	}
	if len(members) != 1 || members[0].UserID != owner.ID {
		t.Fatalf("ListMembers after removal = %+v, want only the owner", members)
	}
}

// TestStore_RemoveMember_RefusesLastOwner is the important one: a
// group must never end up with zero owners, or no one could ever
// administer it again.
func TestStore_RemoveMember_RefusesLastOwner(t *testing.T) {
	conn := dbtest.New(t)
	ctx := context.Background()
	owner, err := users.NewStore(conn).Create(ctx, "alice", "hash", "Alice")
	if err != nil {
		t.Fatalf("create owner: %v", err)
	}

	store := NewStore(conn)
	g, err := store.Create(ctx, "Solo Group", owner.ID)
	if err != nil {
		t.Fatalf("create group: %v", err)
	}

	if err := store.RemoveMember(ctx, g.ID, owner.ID); !errors.Is(err, ErrLastOwner) {
		t.Fatalf("RemoveMember(last owner) error = %v, want ErrLastOwner", err)
	}

	// Confirm it's still actually there -- the refusal wasn't just a
	// reported error while the row got deleted anyway.
	members, err := store.ListMembers(ctx, g.ID)
	if err != nil {
		t.Fatalf("ListMembers: %v", err)
	}
	if len(members) != 1 {
		t.Fatalf("ListMembers = %+v, want the owner still present", members)
	}
}

// TestStore_RemoveMember_AllowsRemovingOneOfTwoOwners confirms the
// last-owner guard is specifically about being the *last* one, not
// owners in general.
func TestStore_RemoveMember_AllowsRemovingOneOfTwoOwners(t *testing.T) {
	conn := dbtest.New(t)
	ctx := context.Background()
	userStore := users.NewStore(conn)
	owner, err := userStore.Create(ctx, "alice", "hash", "Alice")
	if err != nil {
		t.Fatalf("create owner: %v", err)
	}
	coOwner, err := userStore.Create(ctx, "bob", "hash", "Bob")
	if err != nil {
		t.Fatalf("create co-owner: %v", err)
	}

	store := NewStore(conn)
	g, err := store.Create(ctx, "Co-owned Group", owner.ID)
	if err != nil {
		t.Fatalf("create group: %v", err)
	}
	if err := store.AddMember(ctx, g.ID, coOwner.ID, RoleOwner); err != nil {
		t.Fatalf("AddMember: %v", err)
	}

	if err := store.RemoveMember(ctx, g.ID, owner.ID); err != nil {
		t.Fatalf("RemoveMember(one of two owners): %v", err)
	}
}

func TestStore_UpdateMemberRole_RefusesToDemoteLastOwner(t *testing.T) {
	conn := dbtest.New(t)
	ctx := context.Background()
	owner, err := users.NewStore(conn).Create(ctx, "alice", "hash", "Alice")
	if err != nil {
		t.Fatalf("create owner: %v", err)
	}

	store := NewStore(conn)
	g, err := store.Create(ctx, "Solo Group", owner.ID)
	if err != nil {
		t.Fatalf("create group: %v", err)
	}

	err = store.UpdateMemberRole(ctx, g.ID, owner.ID, RoleAdmin)
	if !errors.Is(err, ErrLastOwner) {
		t.Fatalf("UpdateMemberRole(demote last owner) error = %v, want ErrLastOwner", err)
	}

	role, err := store.GetMemberRole(ctx, g.ID, owner.ID)
	if err != nil {
		t.Fatalf("GetMemberRole: %v", err)
	}
	if role != RoleOwner {
		t.Fatalf("role after refused demotion = %q, want still %q", role, RoleOwner)
	}
}

func TestStore_UpdateMemberRole(t *testing.T) {
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

	store := NewStore(conn)
	g, err := store.Create(ctx, "Shared Group", owner.ID)
	if err != nil {
		t.Fatalf("create group: %v", err)
	}
	if err := store.AddMember(ctx, g.ID, member.ID, RoleViewer); err != nil {
		t.Fatalf("AddMember: %v", err)
	}
	if err := store.UpdateMemberRole(ctx, g.ID, member.ID, RoleAdmin); err != nil {
		t.Fatalf("UpdateMemberRole: %v", err)
	}

	role, err := store.GetMemberRole(ctx, g.ID, member.ID)
	if err != nil {
		t.Fatalf("GetMemberRole: %v", err)
	}
	if role != RoleAdmin {
		t.Fatalf("role = %q, want %q", role, RoleAdmin)
	}
}

func TestStore_UpdateName_UnknownIsNotFound(t *testing.T) {
	conn := dbtest.New(t)
	_, err := NewStore(conn).UpdateName(context.Background(), "does-not-exist", "New Name")
	if !errors.Is(err, db.ErrNotFound) {
		t.Fatalf("UpdateName(unknown) error = %v, want wrapping db.ErrNotFound", err)
	}
}

func TestStore_UpdateIcon_UnknownIsNotFound(t *testing.T) {
	conn := dbtest.New(t)
	_, err := NewStore(conn).UpdateIcon(context.Background(), "does-not-exist", "🚀")
	if !errors.Is(err, db.ErrNotFound) {
		t.Fatalf("UpdateIcon(unknown) error = %v, want wrapping db.ErrNotFound", err)
	}
}

func TestStore_Archive_EmptyGroup(t *testing.T) {
	conn := dbtest.New(t)
	ctx := context.Background()
	owner, err := users.NewStore(conn).Create(ctx, "alice", "hash", "Alice")
	if err != nil {
		t.Fatalf("create owner: %v", err)
	}

	store := NewStore(conn)
	g, err := store.Create(ctx, "Empty Group", owner.ID)
	if err != nil {
		t.Fatalf("create group: %v", err)
	}

	if err := store.Archive(ctx, g.ID); err != nil {
		t.Fatalf("Archive: %v", err)
	}

	list, err := store.ListForUser(ctx, owner.ID)
	if err != nil {
		t.Fatalf("ListForUser: %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("expected the archived group to be excluded, got %+v", list)
	}
}

// TestStore_Archive_RefusesNonEmptyGroup is the important one: archiving
// a group with live spaces in it would otherwise strand them in a
// half-reachable state (still directly owned, but invisible in the
// sidebar since GroupSection only renders a space under a group that's
// actually in the caller's own group list) -- see Archive's own comment.
func TestStore_Archive_RefusesNonEmptyGroup(t *testing.T) {
	conn := dbtest.New(t)
	ctx := context.Background()
	owner, err := users.NewStore(conn).Create(ctx, "alice", "hash", "Alice")
	if err != nil {
		t.Fatalf("create owner: %v", err)
	}

	store := NewStore(conn)
	g, err := store.Create(ctx, "Non-Empty Group", owner.ID)
	if err != nil {
		t.Fatalf("create group: %v", err)
	}
	if _, err := spaces.NewStore(conn).Create(ctx, "A Space", spaces.TypePrivate, owner.ID, g.ID); err != nil {
		t.Fatalf("create space: %v", err)
	}

	if err := store.Archive(ctx, g.ID); !errors.Is(err, ErrGroupHasSpaces) {
		t.Fatalf("Archive(non-empty group) error = %v, want ErrGroupHasSpaces", err)
	}

	// Confirm it's genuinely still there, not archived-but-erroring.
	list, err := store.ListForUser(ctx, owner.ID)
	if err != nil {
		t.Fatalf("ListForUser: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("ListForUser = %+v, want the group still present", list)
	}
}

func TestStore_Archive_UnknownIsNotFound(t *testing.T) {
	conn := dbtest.New(t)
	err := NewStore(conn).Archive(context.Background(), "does-not-exist")
	if !errors.Is(err, db.ErrNotFound) {
		t.Fatalf("Archive(unknown) error = %v, want wrapping db.ErrNotFound", err)
	}
}
