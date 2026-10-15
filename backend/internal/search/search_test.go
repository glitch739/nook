package search

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	"nook/internal/assets"
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

// attach stores a file on p, puts its block in p's content (as the
// editor would) and gives it extracted text, one chunk per page.
func (f fixture) attach(t *testing.T, p pages.Page, name string, pageTexts ...string) string {
	t.Helper()
	ctx := context.Background()
	store, err := assets.NewStore(f.conn, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	file, err := store.AddAttachment(ctx, p.ID, strings.NewReader("data"), name, f.userID)
	if err != nil {
		t.Fatalf("AddFile: %v", err)
	}
	doc := `{"type":"doc","content":[{"type":"fileAttachment","attrs":{"src":"/api/pages/` + p.ID + `/files/` + file.ID + `"}}]}`
	if _, err := f.conn.ExecContext(ctx, `UPDATE pages SET content_json = ? WHERE id = ?`, doc, p.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.conn.ExecContext(ctx, `DELETE FROM file_text WHERE file_id = ?`, file.ID); err != nil {
		t.Fatal(err)
	}
	for i, txt := range pageTexts {
		if _, err := f.conn.ExecContext(ctx, `INSERT INTO file_text (file_id, page_no, body) VALUES (?, ?, ?)`, file.ID, i+1, txt); err != nil {
			t.Fatal(err)
		}
	}
	if len(pageTexts) == 0 {
		f.conn.ExecContext(ctx, `INSERT INTO file_text (file_id, body) VALUES (?, '')`, file.ID)
	}
	return file.ID
}

func TestSearch_FileTextFindsPageWithPageNumber(t *testing.T) {
	f := setup(t)
	p := f.page(t, "Runbook")
	fileID := f.attach(t, p, "kezikonyv.pdf", "bevezetés", "A tűzfal szabályait évente felülvizsgáljuk")

	res, err := f.search.Search(context.Background(), f.userID, "tuzfal", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != 1 || res[0].PageID != p.ID || len(res[0].Files) != 1 {
		t.Fatalf("results = %+v, want the page with one file hit", res)
	}
	hit := res[0].Files[0]
	if hit.FileID != fileID || hit.PageNo != 2 || !strings.Contains(hit.Snippet, MarkStart) {
		t.Fatalf("file hit = %+v, want page 2 with a marked snippet", hit)
	}

	// By name, before or without any extracted text.
	f.attach(t, f.page(t, "Másik"), "halozati-topologia.zip")
	if got := f.ids(t, f.userID, "topologia"); len(got) != 1 {
		t.Fatalf("name search = %v, want one page", got)
	}
}

func TestSearch_FileHitsRespectAccess(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	p := f.page(t, "Privát")
	f.attach(t, p, "szerzodes.pdf", "titkos szerződés szövege")

	bob, err := users.NewStore(f.conn).Create(ctx, "bob", "hash", "Bob")
	if err != nil {
		t.Fatal(err)
	}
	if got := f.ids(t, bob.ID, "szerzodes"); len(got) != 0 {
		t.Fatalf("outsider finds a file in a group they are not in: %v", got)
	}

	g := groups.NewStore(f.conn)
	if err := g.AddMember(ctx, f.space.GroupID, bob.ID, "viewer"); err != nil {
		t.Fatal(err)
	}
	if got := f.ids(t, bob.ID, "szerzodes"); !contains(got, p.ID) {
		t.Fatalf("member cannot find the file: %v", got)
	}

	// Removed again: the very next search shows nothing, no reindexing.
	if err := g.RemoveMember(ctx, f.space.GroupID, bob.ID); err != nil {
		t.Fatal(err)
	}
	if got := f.ids(t, bob.ID, "szoveg"); len(got) != 0 {
		t.Fatalf("removed member still finds the file: %v", got)
	}

	// An instance admin who is not a member sees nothing either.
	if _, err := f.conn.ExecContext(ctx, `UPDATE users SET is_admin = 1 WHERE id = ?`, bob.ID); err != nil {
		t.Fatal(err)
	}
	if got := f.ids(t, bob.ID, "szerzodes"); len(got) != 0 {
		t.Fatalf("instance admin outside the group finds the file: %v", got)
	}

	// Archived page: gone for its owner too.
	if err := f.pages.Archive(ctx, p.ID); err != nil {
		t.Fatalf("archive: %v", err)
	}
	if got := f.ids(t, f.userID, "szerzodes"); len(got) != 0 {
		t.Fatalf("file of an archived page still found: %v", got)
	}
}

func TestSearch_FileRemovedFromPageIsNotFound(t *testing.T) {
	f := setup(t)
	p := f.page(t, "Oldal")
	f.attach(t, p, "regi.pdf", "elavult tartalom")
	if _, err := f.conn.Exec(`UPDATE pages SET content_json = '{"type":"doc","content":[]}' WHERE id = ?`, p.ID); err != nil {
		t.Fatal(err)
	}
	if got := f.ids(t, f.userID, "elavult"); len(got) != 0 {
		t.Fatalf("file no longer on the page still found: %v", got)
	}
}

func TestSearch_PageAndFileHitsMerge(t *testing.T) {
	f := setup(t)
	p := f.page(t, "Backup eljárás")
	f.attach(t, p, "backup.pdf", "backup minden éjjel")
	res, err := f.search.Search(context.Background(), f.userID, "backup", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != 1 || len(res[0].Files) != 1 || res[0].TitleMarked == res[0].Title {
		t.Fatalf("results = %+v, want one page, title marked, with its file", res)
	}
}

func TestBuildSubstringQuery(t *testing.T) {
	cases := []struct{ in, sub, short string }{
		{"db01", `"db01"`, ""},
		{"srv db01", `"srv" "db01"`, ""},
		{"ip db01", `"db01"`, `"ip"*`},
		{"ip 10", "", `"ip"* "10"*`},
		// FTS5 syntax in the input stays plain text.
		{`a"b OR c NEAR(x)`, `"NEAR"`, `"a"* "b"* "OR"* "c"* "x"*`},
	}
	for _, c := range cases {
		sub, short := BuildSubstringQuery(c.in)
		if sub != c.sub || short != c.short {
			t.Errorf("BuildSubstringQuery(%q) = %q, %q; want %q, %q", c.in, sub, short, c.sub, c.short)
		}
	}
}

func TestSearch_InsideWords(t *testing.T) {
	f := setup(t)
	title := f.page(t, "Házkörüli munkák")
	body := f.page(t, "Szerverek")
	if _, err := f.pages.UpdateContent(context.Background(), body.ID, `{"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"Az adatbázis gépe srvdb01, a tartalék srvdb02."}]}]}`); err != nil {
		t.Fatalf("UpdateContent: %v", err)
	}

	if got := f.ids(t, f.userID, "körüli"); !contains(got, title.ID) {
		t.Fatalf("körüli = %v, want the Házkörüli page", got)
	}
	if got := f.ids(t, f.userID, "korul"); !contains(got, title.ID) {
		t.Fatalf("korul (no accents) = %v, want the Házkörüli page", got)
	}
	res, err := f.search.Search(context.Background(), f.userID, "db01", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != 1 || res[0].PageID != body.ID || !strings.Contains(res[0].Snippet, MarkStart+"db01"+MarkEnd) {
		t.Fatalf("db01 = %+v, want the Szerverek page with db01 marked", res)
	}
	// A short term still has to match too.
	if got := f.ids(t, f.userID, "db01 xy"); len(got) != 0 {
		t.Fatalf("db01 xy = %v, want nothing (no word starting with xy)", got)
	}
	if got := f.ids(t, f.userID, "db01 a"); !contains(got, body.ID) {
		t.Fatalf("db01 a = %v, want the page (\"a\" is a word in it)", got)
	}
}

func TestSearch_WholeWordHitsRankFirst(t *testing.T) {
	f := setup(t)
	inside := f.page(t, "Biztonsági mentés")
	word := f.page(t, "Mentés napló")
	_ = inside
	res, err := f.search.Search(context.Background(), f.userID, "mentés", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != 2 {
		t.Fatalf("results = %+v, want both pages", res)
	}
	// Both start a word with "mentés"; now one only inside a word:
	alarm := f.page(t, "Riasztásmentés")
	res, _ = f.search.Search(context.Background(), f.userID, "mentés", 0)
	if len(res) != 3 || res[2].PageID != alarm.ID {
		t.Fatalf("results = %+v, want the inside-a-word hit last", res)
	}
	if res[0].PageID != word.ID && res[1].PageID != word.ID {
		t.Fatalf("results = %+v, want a word hit first", res)
	}
}

func TestSearch_InsideWordsInFilesRespectsAccess(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	p := f.page(t, "Hálózat")
	f.attach(t, p, "topologia.pdf", "core switch: swcore02, uplink srvfw01")

	res, err := f.search.Search(ctx, f.userID, "fw01", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != 1 || len(res[0].Files) != 1 || res[0].Files[0].PageNo != 1 {
		t.Fatalf("fw01 = %+v, want the file hit on page 1", res)
	}
	if got := f.ids(t, f.userID, "pologi"); !contains(got, p.ID) {
		t.Fatalf("pologi (inside the file name) = %v", got)
	}

	bob, err := users.NewStore(f.conn).Create(ctx, "bob", "hash", "Bob")
	if err != nil {
		t.Fatal(err)
	}
	if got := f.ids(t, bob.ID, "fw01"); len(got) != 0 {
		t.Fatalf("outsider finds a file by a word part: %v", got)
	}
	if got := f.ids(t, bob.ID, "lózat"); len(got) != 0 {
		t.Fatalf("outsider finds a page by a word part: %v", got)
	}
}

func TestSearchIn_LimitsToCollectionChildren(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	coll, err := f.pages.Create(ctx, f.space.ID, nil, "Igények", f.userID, pages.KindCollection)
	if err != nil {
		t.Fatal(err)
	}
	inside, err := f.pages.Create(ctx, f.space.ID, &coll.ID, "Belső lap", f.userID, pages.KindDocument)
	if err != nil {
		t.Fatal(err)
	}
	f.attach(t, inside, "doksi.pdf", "PyInstaller EXE build lépések")
	outside := f.page(t, "Külső lap")
	f.attach(t, outside, "masik.pdf", "PyInstaller máshol")

	res, err := f.search.SearchIn(ctx, f.userID, "pyinstaller", coll.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != 1 || res[0].PageID != inside.ID || len(res[0].Files) != 1 {
		t.Fatalf("results = %+v, want only the collection's page with its file hit", res)
	}
	// Inside a word (trigram path) stays scoped too.
	res, err = f.search.SearchIn(ctx, f.userID, "installer", coll.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != 1 || res[0].PageID != inside.ID {
		t.Fatalf("substring results = %+v, want only the collection's page", res)
	}
	// The unscoped search still finds both.
	if got := f.ids(t, f.userID, "pyinstaller"); len(got) != 2 {
		t.Fatalf("global search = %v, want both pages", got)
	}
}
