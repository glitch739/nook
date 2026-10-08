package search

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	"nook/internal/databases"
	"nook/internal/dbtest"
	"nook/internal/groups"
	"nook/internal/pages"
	"nook/internal/spaces"
	"nook/internal/users"
)

type fixture struct {
	conn   *sql.DB
	userID string
	space  spaces.Space
	pages  *pages.Store
	search *Store
}

func setup(t *testing.T) fixture {
	t.Helper()
	conn := dbtest.New(t)
	ctx := context.Background()
	user, err := users.NewStore(conn).Create(ctx, "alice", "hash", "Alice")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	group, err := groups.NewStore(conn).Create(ctx, "Csoport", user.ID)
	if err != nil {
		t.Fatalf("create group: %v", err)
	}
	space, err := spaces.NewStore(conn).Create(ctx, "Tér", spaces.TypePrivate, user.ID, group.ID)
	if err != nil {
		t.Fatalf("create space: %v", err)
	}
	return fixture{conn: conn, userID: user.ID, space: space, pages: pages.NewStore(conn), search: NewStore(conn)}
}

func (f fixture) page(t *testing.T, title string) pages.Page {
	t.Helper()
	p, err := f.pages.Create(context.Background(), f.space.ID, nil, title, f.userID, pages.KindDocument)
	if err != nil {
		t.Fatalf("create page: %v", err)
	}
	return p
}

func (f fixture) ids(t *testing.T, userID, q string) []string {
	t.Helper()
	res, err := f.search.Search(context.Background(), userID, q, 0)
	if err != nil {
		t.Fatalf("Search(%q): %v", q, err)
	}
	out := make([]string, 0, len(res))
	for _, r := range res {
		out = append(out, r.PageID)
	}
	return out
}

func contains(ids []string, id string) bool {
	for _, x := range ids {
		if x == id {
			return true
		}
	}
	return false
}

func TestBuildMatchQuery(t *testing.T) {
	cases := map[string]string{
		"":                       "",
		"   ":                    "",
		"ház":                    `"ház"*`,
		"Go kód":                 `"Go"* "kód"*`,
		`"x" OR title:y NEAR(z)`: `"x"* "OR"* "title"* "y"* "NEAR"* "z"*`,
		"a-b_c":                  `"a"* "b"* "c"*`,
	}
	for in, want := range cases {
		if got := BuildMatchQuery(in); got != want {
			t.Errorf("BuildMatchQuery(%q) = %q, want %q", in, got, want)
		}
	}
	if got := BuildMatchQuery(strings.Repeat("w ", 50)); strings.Count(got, "*") != MaxQueryTerms {
		t.Errorf("term cap not applied: %q", got)
	}
}

func TestSearch_TitleIsAccentAndCaseInsensitivePrefix(t *testing.T) {
	f := setup(t)
	p := f.page(t, "Házkörüli teendők")
	for _, q := range []string{"házkörüli", "HAZKORULI", "hazk", "teendo", "ház teend"} {
		if got := f.ids(t, f.userID, q); !contains(got, p.ID) {
			t.Errorf("query %q: got %v, want page %s", q, got, p.ID)
		}
	}
	if got := f.ids(t, f.userID, "kert"); len(got) != 0 {
		t.Errorf("unrelated query matched: %v", got)
	}
}

func TestSearch_ContentUpdatesAreIndexedWithSnippet(t *testing.T) {
	f := setup(t)
	p := f.page(t, "Jegyzet")
	doc := `{"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"A tanúbeszéd vázlata készül."}]}]}`
	if _, err := f.pages.UpdateContent(context.Background(), p.ID, doc); err != nil {
		t.Fatalf("UpdateContent: %v", err)
	}
	res, err := f.search.Search(context.Background(), f.userID, "tanubeszed", 0)
	if err != nil || len(res) != 1 {
		t.Fatalf("Search: %v %+v", err, res)
	}
	if !strings.Contains(res[0].Snippet, MarkStart+"tanúbeszéd"+MarkEnd) {
		t.Errorf("snippet not highlighted: %q", res[0].Snippet)
	}
	// JSON structure itself ("paragraph", "type") must not be searchable.
	if got := f.ids(t, f.userID, "paragraph"); len(got) != 0 {
		t.Errorf("JSON keys leaked into the index: %v", got)
	}
	// A title-only hit carries no snippet.
	res, _ = f.search.Search(context.Background(), f.userID, "jegyzet", 0)
	if len(res) != 1 || res[0].Snippet != "" || res[0].TitleMarked != MarkStart+"Jegyzet"+MarkEnd {
		t.Errorf("title-only hit: %+v", res)
	}
}

func TestSearch_InvalidContentJSONDoesNotBreakSaves(t *testing.T) {
	f := setup(t)
	p := f.page(t, "Rossz")
	if _, err := f.pages.UpdateContent(context.Background(), p.ID, "not json"); err != nil {
		t.Fatalf("UpdateContent with invalid JSON failed: %v", err)
	}
	if got := f.ids(t, f.userID, "rossz"); !contains(got, p.ID) {
		t.Errorf("title no longer searchable: %v", got)
	}
}

func TestSearch_ArchivedPagesDisappear(t *testing.T) {
	f := setup(t)
	p := f.page(t, "Elavult")
	if err := f.pages.Archive(context.Background(), p.ID); err != nil {
		t.Fatalf("Archive: %v", err)
	}
	if got := f.ids(t, f.userID, "elavult"); len(got) != 0 {
		t.Errorf("archived page still found: %v", got)
	}
}

func TestSearch_DatabaseCells(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	dbPage, err := f.pages.Create(ctx, f.space.ID, nil, "Feladatok", f.userID, pages.KindDatabase)
	if err != nil {
		t.Fatalf("create db: %v", err)
	}
	row, err := f.pages.Create(ctx, f.space.ID, &dbPage.ID, "Első sor", f.userID, pages.KindDocument)
	if err != nil {
		t.Fatalf("create row: %v", err)
	}
	dbs := databases.NewStore(f.conn)
	textCol, _ := dbs.CreateProperty(ctx, dbPage.ID, "Megjegyzés", "text")
	statusCol, _ := dbs.CreateProperty(ctx, dbPage.ID, "Állapot", "select")
	opts := []databases.SelectOption{{ID: "o1", Label: "Folyamatban", Color: "blue"}}
	if _, err := dbs.UpdateProperty(ctx, statusCol.ID, nil, &opts); err != nil {
		t.Fatalf("UpdateProperty: %v", err)
	}
	if err := dbs.SetValue(ctx, row.ID, textCol.ID, `"csempézés"`); err != nil {
		t.Fatalf("SetValue text: %v", err)
	}
	if err := dbs.SetValue(ctx, row.ID, statusCol.ID, `{"option_id":"o1"}`); err != nil {
		t.Fatalf("SetValue select: %v", err)
	}
	if got := f.ids(t, f.userID, "csempe"); !contains(got, row.ID) {
		t.Errorf("text cell not searchable: %v", got)
	}
	if got := f.ids(t, f.userID, "folyamat"); !contains(got, row.ID) {
		t.Errorf("select label not searchable: %v", got)
	}

	// Renaming the option re-indexes every row using it.
	opts[0].Label = "Kész"
	if _, err := dbs.UpdateProperty(ctx, statusCol.ID, nil, &opts); err != nil {
		t.Fatalf("UpdateProperty rename: %v", err)
	}
	if got := f.ids(t, f.userID, "folyamat"); contains(got, row.ID) {
		t.Errorf("old option label still indexed: %v", got)
	}
	if got := f.ids(t, f.userID, "kesz"); !contains(got, row.ID) {
		t.Errorf("renamed option label not indexed: %v", got)
	}

	// Deleting the column drops its cells from the index.
	if err := dbs.DeleteProperty(ctx, textCol.ID); err != nil {
		t.Fatalf("DeleteProperty: %v", err)
	}
	if got := f.ids(t, f.userID, "csempe"); contains(got, row.ID) {
		t.Errorf("deleted column's cell still indexed: %v", got)
	}
}

func TestSearch_OnlyAccessibleSpaces(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	p := f.page(t, "Titkos terv")

	bob, err := users.NewStore(f.conn).Create(ctx, "bob", "hash", "Bob")
	if err != nil {
		t.Fatalf("create bob: %v", err)
	}
	if got := f.ids(t, bob.ID, "titkos"); len(got) != 0 {
		t.Fatalf("outsider sees another user's page: %v", got)
	}

	// Joining the group grants access through it...
	if err := groups.NewStore(f.conn).AddMember(ctx, f.space.GroupID, bob.ID, "viewer"); err != nil {
		t.Fatalf("AddMember: %v", err)
	}
	if got := f.ids(t, bob.ID, "titkos"); !contains(got, p.ID) {
		t.Fatalf("group member cannot find page: %v", got)
	}

	// ...until the group is archived, same as CanAccessSpace.
	if _, err := f.conn.ExecContext(ctx, `UPDATE groups SET archived_at = 'x' WHERE id = ?`, f.space.GroupID); err != nil {
		t.Fatalf("archive group: %v", err)
	}
	if got := f.ids(t, bob.ID, "titkos"); len(got) != 0 {
		t.Fatalf("archived group still grants search access: %v", got)
	}
	// The owner keeps access through their direct space membership.
	if got := f.ids(t, f.userID, "titkos"); !contains(got, p.ID) {
		t.Fatalf("owner lost access: %v", got)
	}

	if err := spaces.NewStore(f.conn).Archive(ctx, f.space.ID); err != nil {
		t.Fatalf("archive space: %v", err)
	}
	if got := f.ids(t, f.userID, "titkos"); len(got) != 0 {
		t.Fatalf("archived space still searchable: %v", got)
	}
}

func TestSearch_HardDeleteCleansUp(t *testing.T) {
	f := setup(t)
	p := f.page(t, "Törlendő")
	if _, err := f.conn.Exec(`DELETE FROM pages WHERE id = ?`, p.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	var n int
	f.conn.QueryRow(`SELECT (SELECT COUNT(*) FROM search_docs) + (SELECT COUNT(*) FROM search_index)`).Scan(&n)
	if n != 0 {
		t.Errorf("index rows left behind after delete: %d", n)
	}
}
