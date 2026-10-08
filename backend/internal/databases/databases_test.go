package databases

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"nook/internal/db"
	"nook/internal/dbtest"
	"nook/internal/groups"
	"nook/internal/pages"
	"nook/internal/spaces"
	"nook/internal/users"
)

// setup creates a user, a space they own, and a database page in it --
// the fixture every test below starts from.
func setup(t *testing.T) (*sql.DB, pages.Page, *pages.Store, *Store) {
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
	pagesStore := pages.NewStore(conn)
	dbPage, err := pagesStore.Create(ctx, space.ID, nil, "Tasks", user.ID, pages.KindDatabase)
	if err != nil {
		t.Fatalf("create database page: %v", err)
	}
	return conn, dbPage, pagesStore, NewStore(conn)
}

func TestCreateProperty(t *testing.T) {
	_, dbPage, _, store := setup(t)
	ctx := context.Background()

	prop, err := store.CreateProperty(ctx, dbPage.ID, "Státusz", TypeSelect)
	if err != nil {
		t.Fatalf("CreateProperty: %v", err)
	}
	if prop.Name != "Státusz" || prop.Type != TypeSelect || prop.PageID != dbPage.ID {
		t.Fatalf("unexpected property: %+v", prop)
	}
	if len(prop.Options) != 0 {
		t.Fatalf("Options = %+v, want empty on creation", prop.Options)
	}
}

func TestCreateProperty_InvalidType(t *testing.T) {
	_, dbPage, _, store := setup(t)
	_, err := store.CreateProperty(context.Background(), dbPage.ID, "Bad", "not-a-real-type")
	if !errors.Is(err, ErrInvalidType) {
		t.Fatalf("CreateProperty(invalid type) error = %v, want wrapping ErrInvalidType", err)
	}
}

func TestListProperties_OrderedByPosition(t *testing.T) {
	_, dbPage, _, store := setup(t)
	ctx := context.Background()

	first, err := store.CreateProperty(ctx, dbPage.ID, "Név", TypeText)
	if err != nil {
		t.Fatalf("create first property: %v", err)
	}
	second, err := store.CreateProperty(ctx, dbPage.ID, "Státusz", TypeSelect)
	if err != nil {
		t.Fatalf("create second property: %v", err)
	}

	list, err := store.ListProperties(ctx, dbPage.ID)
	if err != nil {
		t.Fatalf("ListProperties: %v", err)
	}
	if len(list) != 2 || list[0].ID != first.ID || list[1].ID != second.ID {
		t.Fatalf("ListProperties = %+v, want [Név, Státusz] in creation order", list)
	}
}

func TestUpdateProperty_RenameAndOptions(t *testing.T) {
	_, dbPage, _, store := setup(t)
	ctx := context.Background()

	prop, err := store.CreateProperty(ctx, dbPage.ID, "Státusz", TypeSelect)
	if err != nil {
		t.Fatalf("CreateProperty: %v", err)
	}

	newName := "Állapot"
	options := []SelectOption{
		{ID: "todo", Label: "El nem kezdett", Color: "#888888"},
		{ID: "done", Label: "Kész", Color: "#22c55e"},
	}
	updated, err := store.UpdateProperty(ctx, prop.ID, &newName, &options)
	if err != nil {
		t.Fatalf("UpdateProperty: %v", err)
	}
	if updated.Name != "Állapot" {
		t.Fatalf("Name = %q, want %q", updated.Name, "Állapot")
	}
	if len(updated.Options) != 2 || updated.Options[1].Label != "Kész" {
		t.Fatalf("Options = %+v, want the two options just set", updated.Options)
	}

	// A nil field leaves that side unchanged.
	onlyNameAgain := "Állapot v2"
	updated2, err := store.UpdateProperty(ctx, prop.ID, &onlyNameAgain, nil)
	if err != nil {
		t.Fatalf("UpdateProperty (name only): %v", err)
	}
	if len(updated2.Options) != 2 {
		t.Fatalf("Options = %+v, want unchanged (2 options) when only renaming", updated2.Options)
	}
}

func TestReorderColumns(t *testing.T) {
	_, dbPage, _, store := setup(t)
	ctx := context.Background()

	a, err := store.CreateProperty(ctx, dbPage.ID, "A", TypeText)
	if err != nil {
		t.Fatalf("create A: %v", err)
	}
	b, err := store.CreateProperty(ctx, dbPage.ID, "B", TypeText)
	if err != nil {
		t.Fatalf("create B: %v", err)
	}
	c, err := store.CreateProperty(ctx, dbPage.ID, "C", TypeText)
	if err != nil {
		t.Fatalf("create C: %v", err)
	}

	if err := store.ReorderColumns(ctx, dbPage.ID, []string{c.ID, a.ID, b.ID}); err != nil {
		t.Fatalf("ReorderColumns: %v", err)
	}

	list, err := store.ListProperties(ctx, dbPage.ID)
	if err != nil {
		t.Fatalf("ListProperties: %v", err)
	}
	if len(list) != 3 || list[0].ID != c.ID || list[1].ID != a.ID || list[2].ID != b.ID {
		t.Fatalf("ListProperties = %+v, want [C, A, B]", list)
	}
}

func TestReorderColumns_MovesTitleColumn(t *testing.T) {
	_, dbPage, _, store := setup(t)
	ctx := context.Background()

	a, err := store.CreateProperty(ctx, dbPage.ID, "A", TypeText)
	if err != nil {
		t.Fatalf("create A: %v", err)
	}
	b, err := store.CreateProperty(ctx, dbPage.ID, "B", TypeText)
	if err != nil {
		t.Fatalf("create B: %v", err)
	}

	before, err := store.GetTitleColumnPosition(ctx, dbPage.ID)
	if err != nil {
		t.Fatalf("GetTitleColumnPosition (before): %v", err)
	}
	if before != "000000" {
		t.Fatalf("GetTitleColumnPosition (no reorder yet) = %q, want the default %q", before, "000000")
	}

	// Move the title column to sit between A and B.
	if err := store.ReorderColumns(ctx, dbPage.ID, []string{a.ID, TitleColumnID, b.ID}); err != nil {
		t.Fatalf("ReorderColumns: %v", err)
	}

	list, err := store.ListProperties(ctx, dbPage.ID)
	if err != nil {
		t.Fatalf("ListProperties: %v", err)
	}
	titlePos, err := store.GetTitleColumnPosition(ctx, dbPage.ID)
	if err != nil {
		t.Fatalf("GetTitleColumnPosition: %v", err)
	}
	if len(list) != 2 || list[0].ID != a.ID || list[1].ID != b.ID {
		t.Fatalf("ListProperties = %+v, want [A, B]", list)
	}
	if !(list[0].Position < titlePos && titlePos < list[1].Position) {
		t.Fatalf("title position %q not sorted between A (%q) and B (%q)", titlePos, list[0].Position, list[1].Position)
	}
}

func TestReorderColumns_UnknownIDIsNotFound(t *testing.T) {
	_, dbPage, _, store := setup(t)
	ctx := context.Background()

	a, err := store.CreateProperty(ctx, dbPage.ID, "A", TypeText)
	if err != nil {
		t.Fatalf("create A: %v", err)
	}

	err = store.ReorderColumns(ctx, dbPage.ID, []string{a.ID, "does-not-exist"})
	if !errors.Is(err, db.ErrNotFound) {
		t.Fatalf("ReorderColumns(unknown id) error = %v, want wrapping db.ErrNotFound", err)
	}
}

func TestReorderColumns_CrossDatabaseIDIsNotFound(t *testing.T) {
	_, dbPage, pagesStore, store := setup(t)
	ctx := context.Background()

	a, err := store.CreateProperty(ctx, dbPage.ID, "A", TypeText)
	if err != nil {
		t.Fatalf("create A: %v", err)
	}

	otherDBPage, err := pagesStore.Create(ctx, dbPage.SpaceID, nil, "Other DB", dbPage.CreatedBy, pages.KindDatabase)
	if err != nil {
		t.Fatalf("create other database page: %v", err)
	}
	otherProp, err := store.CreateProperty(ctx, otherDBPage.ID, "Other", TypeText)
	if err != nil {
		t.Fatalf("create other property: %v", err)
	}

	// otherProp belongs to a different database page -- reordering
	// dbPage's columns must not be able to touch it.
	err = store.ReorderColumns(ctx, dbPage.ID, []string{a.ID, otherProp.ID})
	if !errors.Is(err, db.ErrNotFound) {
		t.Fatalf("ReorderColumns(cross-database id) error = %v, want wrapping db.ErrNotFound", err)
	}

	// And otherProp's own position must be untouched by the attempt.
	unchanged, err := store.GetProperty(ctx, otherProp.ID)
	if err != nil {
		t.Fatalf("GetProperty(otherProp): %v", err)
	}
	if unchanged.Position != otherProp.Position {
		t.Fatalf("otherProp.Position changed from %q to %q", otherProp.Position, unchanged.Position)
	}
}

func TestUpdateProperty_UnknownIsNotFound(t *testing.T) {
	_, _, _, store := setup(t)
	name := "New Name"
	_, err := store.UpdateProperty(context.Background(), "does-not-exist", &name, nil)
	if !errors.Is(err, db.ErrNotFound) {
		t.Fatalf("UpdateProperty(unknown) error = %v, want wrapping db.ErrNotFound", err)
	}
}

func TestDeleteProperty_RemovesValuesToo(t *testing.T) {
	_, dbPage, pagesStore, store := setup(t)
	ctx := context.Background()

	prop, err := store.CreateProperty(ctx, dbPage.ID, "Státusz", TypeSelect)
	if err != nil {
		t.Fatalf("CreateProperty: %v", err)
	}
	row, err := pagesStore.Create(ctx, dbPage.SpaceID, &dbPage.ID, "Row 1", dbPage.CreatedBy, pages.KindDocument)
	if err != nil {
		t.Fatalf("create row: %v", err)
	}
	if err := store.SetValue(ctx, row.ID, prop.ID, `{"option_id":"done"}`); err != nil {
		t.Fatalf("SetValue: %v", err)
	}

	if err := store.DeleteProperty(ctx, prop.ID); err != nil {
		t.Fatalf("DeleteProperty: %v", err)
	}

	values, err := store.ValuesForDatabase(ctx, dbPage.ID)
	if err != nil {
		t.Fatalf("ValuesForDatabase: %v", err)
	}
	if len(values[row.ID]) != 0 {
		t.Fatalf("expected the row's value for the deleted property to be gone too, got %+v", values[row.ID])
	}
}

func TestDeleteProperty_UnknownIsNotFound(t *testing.T) {
	_, _, _, store := setup(t)
	err := store.DeleteProperty(context.Background(), "does-not-exist")
	if !errors.Is(err, db.ErrNotFound) {
		t.Fatalf("DeleteProperty(unknown) error = %v, want wrapping db.ErrNotFound", err)
	}
}

func TestSetValue_UpsertOverwrites(t *testing.T) {
	_, dbPage, pagesStore, store := setup(t)
	ctx := context.Background()

	prop, err := store.CreateProperty(ctx, dbPage.ID, "Név", TypeText)
	if err != nil {
		t.Fatalf("CreateProperty: %v", err)
	}
	row, err := pagesStore.Create(ctx, dbPage.SpaceID, &dbPage.ID, "Row 1", dbPage.CreatedBy, pages.KindDocument)
	if err != nil {
		t.Fatalf("create row: %v", err)
	}

	if err := store.SetValue(ctx, row.ID, prop.ID, `"first"`); err != nil {
		t.Fatalf("SetValue (first): %v", err)
	}
	if err := store.SetValue(ctx, row.ID, prop.ID, `"second"`); err != nil {
		t.Fatalf("SetValue (second): %v", err)
	}

	values, err := store.ValuesForDatabase(ctx, dbPage.ID)
	if err != nil {
		t.Fatalf("ValuesForDatabase: %v", err)
	}
	if values[row.ID][prop.ID] != `"second"` {
		t.Fatalf("value = %q, want the second SetValue to have overwritten the first", values[row.ID][prop.ID])
	}
}

func TestValuesForDatabase_OnlyIncludesRowsOfThisDatabase(t *testing.T) {
	_, dbPage, pagesStore, store := setup(t)
	ctx := context.Background()

	prop, err := store.CreateProperty(ctx, dbPage.ID, "Név", TypeText)
	if err != nil {
		t.Fatalf("CreateProperty: %v", err)
	}
	row, err := pagesStore.Create(ctx, dbPage.SpaceID, &dbPage.ID, "Row 1", dbPage.CreatedBy, pages.KindDocument)
	if err != nil {
		t.Fatalf("create row: %v", err)
	}
	if err := store.SetValue(ctx, row.ID, prop.ID, `"value"`); err != nil {
		t.Fatalf("SetValue: %v", err)
	}

	// A second, unrelated database page + row shouldn't leak into the
	// first database's values.
	otherDBPage, err := pagesStore.Create(ctx, dbPage.SpaceID, nil, "Other DB", dbPage.CreatedBy, pages.KindDatabase)
	if err != nil {
		t.Fatalf("create other database page: %v", err)
	}
	otherRow, err := pagesStore.Create(ctx, dbPage.SpaceID, &otherDBPage.ID, "Other Row", dbPage.CreatedBy, pages.KindDocument)
	if err != nil {
		t.Fatalf("create other row: %v", err)
	}
	otherProp, err := store.CreateProperty(ctx, otherDBPage.ID, "Egyéb", TypeText)
	if err != nil {
		t.Fatalf("create other property: %v", err)
	}
	if err := store.SetValue(ctx, otherRow.ID, otherProp.ID, `"other value"`); err != nil {
		t.Fatalf("SetValue (other): %v", err)
	}

	values, err := store.ValuesForDatabase(ctx, dbPage.ID)
	if err != nil {
		t.Fatalf("ValuesForDatabase: %v", err)
	}
	if len(values) != 1 || values[row.ID][prop.ID] != `"value"` {
		t.Fatalf("ValuesForDatabase leaked another database's rows: %+v", values)
	}
}
