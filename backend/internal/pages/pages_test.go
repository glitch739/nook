package pages

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"nook/internal/db"
	"nook/internal/dbtest"
	"nook/internal/groups"
	"nook/internal/spaces"
	"nook/internal/users"
)

// setup creates a user + a space they own, returning both plus a ready
// pages.Store -- the fixture every test below starts from.
func setup(t *testing.T) (*sql.DB, users.User, spaces.Space, *Store) {
	t.Helper()
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
	return conn, user, space, NewStore(conn)
}

func TestCreate_TopLevelPage(t *testing.T) {
	_, user, space, store := setup(t)
	ctx := context.Background()

	page, err := store.Create(ctx, space.ID, nil, "Notes", user.ID, KindDocument)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if page.Title != "Notes" || page.SpaceID != space.ID || page.CreatedBy != user.ID {
		t.Fatalf("unexpected page: %+v", page)
	}
	if page.ParentPageID != nil {
		t.Fatalf("expected a nil ParentPageID for a top-level page, got %v", *page.ParentPageID)
	}
	if page.ContentJSON != EmptyDoc {
		t.Fatalf("ContentJSON = %q, want the empty-doc default %q", page.ContentJSON, EmptyDoc)
	}
	if page.ArchivedAt != nil {
		t.Fatal("a freshly created page should not be archived")
	}
	if page.Icon != DefaultDocumentIcon {
		t.Fatalf("Icon = %q, want the default %q", page.Icon, DefaultDocumentIcon)
	}
	if page.Kind != KindDocument {
		t.Fatalf("Kind = %q, want %q", page.Kind, KindDocument)
	}
}

func TestCreate_DatabasePage(t *testing.T) {
	_, user, space, store := setup(t)
	ctx := context.Background()

	page, err := store.Create(ctx, space.ID, nil, "Tasks", user.ID, KindDatabase)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if page.Kind != KindDatabase {
		t.Fatalf("Kind = %q, want %q", page.Kind, KindDatabase)
	}
	if page.Icon != "lucide:table" {
		t.Fatalf("Icon = %q, want the database-page default %q", page.Icon, "lucide:table")
	}
}

func TestListChildren(t *testing.T) {
	_, user, space, store := setup(t)
	ctx := context.Background()

	parent, err := store.Create(ctx, space.ID, nil, "Tasks", user.ID, KindDatabase)
	if err != nil {
		t.Fatalf("create parent: %v", err)
	}
	first, err := store.Create(ctx, space.ID, &parent.ID, "Row 1", user.ID, KindDocument)
	if err != nil {
		t.Fatalf("create first child: %v", err)
	}
	second, err := store.Create(ctx, space.ID, &parent.ID, "Row 2", user.ID, KindDocument)
	if err != nil {
		t.Fatalf("create second child: %v", err)
	}
	// An unrelated top-level page shouldn't show up as a child of parent.
	if _, err := store.Create(ctx, space.ID, nil, "Unrelated", user.ID, KindDocument); err != nil {
		t.Fatalf("create unrelated page: %v", err)
	}
	if err := store.Archive(ctx, second.ID); err != nil {
		t.Fatalf("archive second child: %v", err)
	}

	children, err := store.ListChildren(ctx, parent.ID)
	if err != nil {
		t.Fatalf("ListChildren: %v", err)
	}
	if len(children) != 1 || children[0].ID != first.ID {
		t.Fatalf("ListChildren = %+v, want exactly [Row 1] (archived and unrelated pages excluded)", children)
	}
}

func TestUpdateIcon(t *testing.T) {
	_, user, space, store := setup(t)
	ctx := context.Background()

	page, err := store.Create(ctx, space.ID, nil, "Notes", user.ID, KindDocument)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	updated, err := store.UpdateIcon(ctx, page.ID, "🚀")
	if err != nil {
		t.Fatalf("UpdateIcon: %v", err)
	}
	if updated.Icon != "🚀" {
		t.Fatalf("Icon = %q, want %q", updated.Icon, "🚀")
	}
}

func TestUpdateIcon_UnknownIsNotFound(t *testing.T) {
	_, _, _, store := setup(t)
	_, err := store.UpdateIcon(context.Background(), "does-not-exist", "🚀")
	if !errors.Is(err, db.ErrNotFound) {
		t.Fatalf("UpdateIcon(unknown) error = %v, want wrapping db.ErrNotFound", err)
	}
}

func TestCreate_ChildPage(t *testing.T) {
	_, user, space, store := setup(t)
	ctx := context.Background()

	parent, err := store.Create(ctx, space.ID, nil, "Projects", user.ID, KindDocument)
	if err != nil {
		t.Fatalf("create parent: %v", err)
	}
	child, err := store.Create(ctx, space.ID, &parent.ID, "Project A", user.ID, KindDocument)
	if err != nil {
		t.Fatalf("create child: %v", err)
	}
	if child.ParentPageID == nil || *child.ParentPageID != parent.ID {
		t.Fatalf("child.ParentPageID = %v, want %q", child.ParentPageID, parent.ID)
	}
}

func TestCreate_SiblingsGetIncreasingPositions(t *testing.T) {
	_, user, space, store := setup(t)
	ctx := context.Background()

	first, err := store.Create(ctx, space.ID, nil, "First", user.ID, KindDocument)
	if err != nil {
		t.Fatalf("create first: %v", err)
	}
	second, err := store.Create(ctx, space.ID, nil, "Second", user.ID, KindDocument)
	if err != nil {
		t.Fatalf("create second: %v", err)
	}
	if !(first.Position < second.Position) {
		t.Fatalf("expected first.Position (%q) < second.Position (%q)", first.Position, second.Position)
	}

	// The real point of zero-padding: without it, "10" would sort before
	// "2" as plain strings. Create ten siblings and check strict
	// lexicographic ordering holds all the way past the single-digit
	// boundary.
	var positions []string
	positions = append(positions, first.Position, second.Position)
	for i := 0; i < 9; i++ {
		p, err := store.Create(ctx, space.ID, nil, "More", user.ID, KindDocument)
		if err != nil {
			t.Fatalf("create sibling %d: %v", i, err)
		}
		positions = append(positions, p.Position)
	}
	for i := 1; i < len(positions); i++ {
		if !(positions[i-1] < positions[i]) {
			t.Fatalf("position ordering broken at index %d: %q is not < %q", i, positions[i-1], positions[i])
		}
	}
}

func TestGetByID_UnknownIsNotFound(t *testing.T) {
	_, _, _, store := setup(t)

	_, err := store.GetByID(context.Background(), "does-not-exist")
	if !errors.Is(err, db.ErrNotFound) {
		t.Fatalf("GetByID(unknown) error = %v, want wrapping db.ErrNotFound", err)
	}
}

func TestListForSpace_ExcludesArchived(t *testing.T) {
	_, user, space, store := setup(t)
	ctx := context.Background()

	keep, err := store.Create(ctx, space.ID, nil, "Keep", user.ID, KindDocument)
	if err != nil {
		t.Fatalf("create keep: %v", err)
	}
	archived, err := store.Create(ctx, space.ID, nil, "Archived", user.ID, KindDocument)
	if err != nil {
		t.Fatalf("create archived: %v", err)
	}
	if err := store.Archive(ctx, archived.ID); err != nil {
		t.Fatalf("archive: %v", err)
	}

	list, err := store.ListForSpace(ctx, space.ID)
	if err != nil {
		t.Fatalf("ListForSpace: %v", err)
	}
	if len(list) != 1 || list[0].ID != keep.ID {
		t.Fatalf("ListForSpace = %+v, want exactly [Keep]", list)
	}
}

func TestListForSpace_DoesNotLeakOtherSpaces(t *testing.T) {
	conn, user, spaceA, store := setup(t)
	ctx := context.Background()

	spaceB, err := spaces.NewStore(conn).Create(ctx, "Another Space", spaces.TypePrivate, user.ID, spaceA.GroupID)
	if err != nil {
		t.Fatalf("create second space: %v", err)
	}

	if _, err := store.Create(ctx, spaceA.ID, nil, "In A", user.ID, KindDocument); err != nil {
		t.Fatalf("create page in A: %v", err)
	}
	if _, err := store.Create(ctx, spaceB.ID, nil, "In B", user.ID, KindDocument); err != nil {
		t.Fatalf("create page in B: %v", err)
	}

	list, err := store.ListForSpace(ctx, spaceA.ID)
	if err != nil {
		t.Fatalf("ListForSpace(A): %v", err)
	}
	if len(list) != 1 || list[0].Title != "In A" {
		t.Fatalf("ListForSpace(A) = %+v, want exactly [In A]", list)
	}
}

func TestUpdateTitle(t *testing.T) {
	_, user, space, store := setup(t)
	ctx := context.Background()

	page, err := store.Create(ctx, space.ID, nil, "Old Title", user.ID, KindDocument)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	updated, err := store.UpdateTitle(ctx, page.ID, "New Title")
	if err != nil {
		t.Fatalf("UpdateTitle: %v", err)
	}
	if updated.Title != "New Title" {
		t.Fatalf("Title = %q, want %q", updated.Title, "New Title")
	}
	if updated.UpdatedAt == page.CreatedAt && updated.UpdatedAt == page.UpdatedAt {
		t.Fatal("UpdatedAt should have changed after UpdateTitle")
	}
}

func TestUpdateTitle_UnknownIsNotFound(t *testing.T) {
	_, _, _, store := setup(t)
	_, err := store.UpdateTitle(context.Background(), "does-not-exist", "X")
	if !errors.Is(err, db.ErrNotFound) {
		t.Fatalf("UpdateTitle(unknown) error = %v, want wrapping db.ErrNotFound", err)
	}
}

func TestUpdateContent(t *testing.T) {
	_, user, space, store := setup(t)
	ctx := context.Background()

	page, err := store.Create(ctx, space.ID, nil, "Doc", user.ID, KindDocument)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	const newContent = `{"type":"doc","content":[{"type":"paragraph"}]}`
	updated, err := store.UpdateContent(ctx, page.ID, newContent)
	if err != nil {
		t.Fatalf("UpdateContent: %v", err)
	}
	if updated.ContentJSON != newContent {
		t.Fatalf("ContentJSON = %q, want %q", updated.ContentJSON, newContent)
	}
}

func TestArchive_IsIdempotentlyRejectedTwice(t *testing.T) {
	_, user, space, store := setup(t)
	ctx := context.Background()

	page, err := store.Create(ctx, space.ID, nil, "Doc", user.ID, KindDocument)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := store.Archive(ctx, page.ID); err != nil {
		t.Fatalf("first archive: %v", err)
	}
	// Second archive of an already-archived page should fail (matches
	// "archived_at IS NULL" in the query) -- distinguishes "there was
	// nothing left to archive" from silently no-op-ing on a bad ID,
	// same as GetByID would report db.ErrNotFound for.
	if err := store.Archive(ctx, page.ID); !errors.Is(err, db.ErrNotFound) {
		t.Fatalf("second archive error = %v, want wrapping db.ErrNotFound", err)
	}
}

func TestArchive_UnknownIsNotFound(t *testing.T) {
	_, _, _, store := setup(t)
	err := store.Archive(context.Background(), "does-not-exist")
	if !errors.Is(err, db.ErrNotFound) {
		t.Fatalf("Archive(unknown) error = %v, want wrapping db.ErrNotFound", err)
	}
}

func TestBuildTree_NestsChildrenUnderParents(t *testing.T) {
	_, user, space, store := setup(t)
	ctx := context.Background()

	root, err := store.Create(ctx, space.ID, nil, "Root", user.ID, KindDocument)
	if err != nil {
		t.Fatalf("create root: %v", err)
	}
	child, err := store.Create(ctx, space.ID, &root.ID, "Child", user.ID, KindDocument)
	if err != nil {
		t.Fatalf("create child: %v", err)
	}
	grandchild, err := store.Create(ctx, space.ID, &child.ID, "Grandchild", user.ID, KindDocument)
	if err != nil {
		t.Fatalf("create grandchild: %v", err)
	}
	otherRoot, err := store.Create(ctx, space.ID, nil, "Other Root", user.ID, KindDocument)
	if err != nil {
		t.Fatalf("create other root: %v", err)
	}

	list, err := store.ListForSpace(ctx, space.ID)
	if err != nil {
		t.Fatalf("ListForSpace: %v", err)
	}
	tree := BuildTree(list)

	if len(tree) != 2 {
		t.Fatalf("expected 2 root nodes, got %d", len(tree))
	}
	var rootNode, otherRootNode *TreeNode
	for _, n := range tree {
		switch n.ID {
		case root.ID:
			rootNode = n
		case otherRoot.ID:
			otherRootNode = n
		}
	}
	if rootNode == nil || otherRootNode == nil {
		t.Fatalf("missing expected root nodes in tree: %+v", tree)
	}
	if len(rootNode.Children) != 1 || rootNode.Children[0].ID != child.ID {
		t.Fatalf("root's children = %+v, want exactly [child]", rootNode.Children)
	}
	if len(rootNode.Children[0].Children) != 1 || rootNode.Children[0].Children[0].ID != grandchild.ID {
		t.Fatalf("child's children = %+v, want exactly [grandchild]", rootNode.Children[0].Children)
	}
	if len(otherRootNode.Children) != 0 {
		t.Fatalf("otherRoot should have no children, got %+v", otherRootNode.Children)
	}
}

func TestReorderSiblings(t *testing.T) {
	_, user, space, store := setup(t)
	ctx := context.Background()

	a, err := store.Create(ctx, space.ID, nil, "A", user.ID, KindDocument)
	if err != nil {
		t.Fatalf("create A: %v", err)
	}
	b, err := store.Create(ctx, space.ID, nil, "B", user.ID, KindDocument)
	if err != nil {
		t.Fatalf("create B: %v", err)
	}
	c, err := store.Create(ctx, space.ID, nil, "C", user.ID, KindDocument)
	if err != nil {
		t.Fatalf("create C: %v", err)
	}

	// Creation order (and therefore position) is A, B, C -- reorder to
	// C, A, B and confirm ListForSpace reflects it.
	if err := store.ReorderSiblings(ctx, space.ID, nil, []string{c.ID, a.ID, b.ID}); err != nil {
		t.Fatalf("ReorderSiblings: %v", err)
	}

	list, err := store.ListForSpace(ctx, space.ID)
	if err != nil {
		t.Fatalf("ListForSpace: %v", err)
	}
	if len(list) != 3 || list[0].ID != c.ID || list[1].ID != a.ID || list[2].ID != b.ID {
		t.Fatalf("ListForSpace after reorder = %+v, want [C, A, B]", list)
	}
}

// TestReorderSiblings_ScopedToParentAndSpace confirms the UPDATE's own
// WHERE clause (space_id + parent_page_id), not just the HTTP layer's
// permutation check, is what keeps a reorder from touching a page that
// isn't actually a sibling in that group -- passing a real page id that
// belongs to a *different* parent affects zero rows.
func TestReorderSiblings_ScopedToParentAndSpace(t *testing.T) {
	_, user, space, store := setup(t)
	ctx := context.Background()

	root, err := store.Create(ctx, space.ID, nil, "Root", user.ID, KindDocument)
	if err != nil {
		t.Fatalf("create root: %v", err)
	}
	outsider, err := store.Create(ctx, space.ID, nil, "Outsider", user.ID, KindDocument)
	if err != nil {
		t.Fatalf("create outsider: %v", err)
	}

	// outsider is a root page, not a child of root -- reordering root's
	// (empty) children list with outsider's id should fail, not
	// silently reparent it.
	err = store.ReorderSiblings(ctx, space.ID, &root.ID, []string{outsider.ID})
	if !errors.Is(err, db.ErrNotFound) {
		t.Fatalf("ReorderSiblings(wrong parent) error = %v, want wrapping db.ErrNotFound", err)
	}
}

func TestMove_ReparentWithinSpace(t *testing.T) {
	_, user, space, store := setup(t)
	ctx := context.Background()

	a, err := store.Create(ctx, space.ID, nil, "A", user.ID, KindDocument)
	if err != nil {
		t.Fatalf("create A: %v", err)
	}
	b, err := store.Create(ctx, space.ID, nil, "B", user.ID, KindDocument)
	if err != nil {
		t.Fatalf("create B: %v", err)
	}

	if err := store.Move(ctx, b.ID, space.ID, &a.ID); err != nil {
		t.Fatalf("Move: %v", err)
	}

	moved, err := store.GetByID(ctx, b.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if moved.ParentPageID == nil || *moved.ParentPageID != a.ID {
		t.Fatalf("B's parent = %v, want %q", moved.ParentPageID, a.ID)
	}
	if moved.SpaceID != space.ID {
		t.Fatalf("B's space changed unexpectedly: %q", moved.SpaceID)
	}
}

func TestMove_ToAnotherSpace(t *testing.T) {
	conn, user, space, store := setup(t)
	ctx := context.Background()

	group, err := groups.NewStore(conn).Create(ctx, "Another Group", user.ID)
	if err != nil {
		t.Fatalf("create another group: %v", err)
	}
	otherSpace, err := spaces.NewStore(conn).Create(ctx, "Other Space", spaces.TypePrivate, user.ID, group.ID)
	if err != nil {
		t.Fatalf("create other space: %v", err)
	}

	page, err := store.Create(ctx, space.ID, nil, "Page", user.ID, KindDocument)
	if err != nil {
		t.Fatalf("create page: %v", err)
	}

	if err := store.Move(ctx, page.ID, otherSpace.ID, nil); err != nil {
		t.Fatalf("Move: %v", err)
	}

	moved, err := store.GetByID(ctx, page.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if moved.SpaceID != otherSpace.ID {
		t.Fatalf("SpaceID = %q, want %q", moved.SpaceID, otherSpace.ID)
	}
	if moved.ParentPageID != nil {
		t.Fatalf("ParentPageID = %v, want nil", moved.ParentPageID)
	}
}

func TestMove_ToAnotherSpaceTakesChildren(t *testing.T) {
	conn, user, space, store := setup(t)
	ctx := context.Background()

	group, err := groups.NewStore(conn).Create(ctx, "Another Group", user.ID)
	if err != nil {
		t.Fatalf("create another group: %v", err)
	}
	otherSpace, err := spaces.NewStore(conn).Create(ctx, "Other Space", spaces.TypePrivate, user.ID, group.ID)
	if err != nil {
		t.Fatalf("create other space: %v", err)
	}
	parent, err := store.Create(ctx, space.ID, nil, "Parent", user.ID, KindDocument)
	if err != nil {
		t.Fatalf("create parent: %v", err)
	}
	child, err := store.Create(ctx, space.ID, &parent.ID, "Child", user.ID, KindDocument)
	if err != nil {
		t.Fatalf("create child: %v", err)
	}

	if err := store.Move(ctx, parent.ID, otherSpace.ID, nil); err != nil {
		t.Fatalf("Move: %v", err)
	}

	moved, err := store.GetByID(ctx, child.ID)
	if err != nil {
		t.Fatalf("GetByID(child): %v", err)
	}
	if moved.SpaceID != otherSpace.ID {
		t.Fatalf("child SpaceID = %q, want %q", moved.SpaceID, otherSpace.ID)
	}
	if moved.ParentPageID == nil || *moved.ParentPageID != parent.ID {
		t.Fatalf("child ParentPageID = %v, want %q", moved.ParentPageID, parent.ID)
	}
}

func TestMove_RefusesSelfParent(t *testing.T) {
	_, user, space, store := setup(t)
	ctx := context.Background()

	a, err := store.Create(ctx, space.ID, nil, "A", user.ID, KindDocument)
	if err != nil {
		t.Fatalf("create A: %v", err)
	}

	err = store.Move(ctx, a.ID, space.ID, &a.ID)
	if !errors.Is(err, ErrSelfParent) {
		t.Fatalf("Move(self-parent) error = %v, want ErrSelfParent", err)
	}
}

// TestMove_RefusesCircularHierarchy is the important one: A is B's
// parent, so moving A under B (or under any of B's own descendants)
// must be refused, or the tree would have a cycle with no root.
func TestMove_RefusesCircularHierarchy(t *testing.T) {
	_, user, space, store := setup(t)
	ctx := context.Background()

	a, err := store.Create(ctx, space.ID, nil, "A", user.ID, KindDocument)
	if err != nil {
		t.Fatalf("create A: %v", err)
	}
	b, err := store.Create(ctx, space.ID, &a.ID, "B", user.ID, KindDocument)
	if err != nil {
		t.Fatalf("create B (child of A): %v", err)
	}
	c, err := store.Create(ctx, space.ID, &b.ID, "C", user.ID, KindDocument)
	if err != nil {
		t.Fatalf("create C (child of B): %v", err)
	}

	if err := store.Move(ctx, a.ID, space.ID, &b.ID); !errors.Is(err, ErrCircularHierarchy) {
		t.Fatalf("Move(A under its child B) error = %v, want ErrCircularHierarchy", err)
	}
	// Also refused at any depth, not just the immediate child.
	if err := store.Move(ctx, a.ID, space.ID, &c.ID); !errors.Is(err, ErrCircularHierarchy) {
		t.Fatalf("Move(A under its grandchild C) error = %v, want ErrCircularHierarchy", err)
	}

	// Confirm none of the refused attempts actually moved anything.
	reloaded, err := store.GetByID(ctx, a.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if reloaded.ParentPageID != nil {
		t.Fatalf("A's parent changed despite the refusal: %v", reloaded.ParentPageID)
	}
}

func TestMove_RefusesParentInDifferentSpace(t *testing.T) {
	conn, user, space, store := setup(t)
	ctx := context.Background()

	group, err := groups.NewStore(conn).Create(ctx, "Another Group", user.ID)
	if err != nil {
		t.Fatalf("create another group: %v", err)
	}
	otherSpace, err := spaces.NewStore(conn).Create(ctx, "Other Space", spaces.TypePrivate, user.ID, group.ID)
	if err != nil {
		t.Fatalf("create other space: %v", err)
	}

	page, err := store.Create(ctx, space.ID, nil, "Page", user.ID, KindDocument)
	if err != nil {
		t.Fatalf("create page: %v", err)
	}
	parentInOtherSpace, err := store.Create(ctx, otherSpace.ID, nil, "Parent", user.ID, KindDocument)
	if err != nil {
		t.Fatalf("create parent in other space: %v", err)
	}

	// Claiming space.ID as the destination while pointing at a parent
	// that's actually in otherSpace must be refused, not silently
	// "fixed" by adopting the parent's real space.
	err = store.Move(ctx, page.ID, space.ID, &parentInOtherSpace.ID)
	if !errors.Is(err, ErrParentInDifferentSpace) {
		t.Fatalf("Move(parent in different space) error = %v, want ErrParentInDifferentSpace", err)
	}
}

func TestMove_UnknownParentIsNotFound(t *testing.T) {
	_, user, space, store := setup(t)
	ctx := context.Background()

	page, err := store.Create(ctx, space.ID, nil, "Page", user.ID, KindDocument)
	if err != nil {
		t.Fatalf("create page: %v", err)
	}

	missing := "does-not-exist"
	err = store.Move(ctx, page.ID, space.ID, &missing)
	if !errors.Is(err, db.ErrNotFound) {
		t.Fatalf("Move(unknown parent) error = %v, want wrapping db.ErrNotFound", err)
	}
}
