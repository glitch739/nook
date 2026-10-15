package collab

import (
	"context"
	"testing"

	"nook/internal/dbtest"
	"nook/internal/groups"
	"nook/internal/pages"
	"nook/internal/spaces"
	"nook/internal/users"
)

func TestVersionsAndLinks(t *testing.T) {
	ctx := context.Background()
	conn := dbtest.New(t)
	u, _ := users.NewStore(conn).Create(ctx, "anna", "h", "Anna")
	g, _ := groups.NewStore(conn).Create(ctx, "G", u.ID)
	sp, _ := spaces.NewStore(conn).Create(ctx, "S", spaces.TypePrivate, u.ID, g.ID)
	ps := pages.NewStore(conn)
	a, _ := ps.Create(ctx, sp.ID, nil, "A", u.ID, pages.KindDocument)
	b, _ := ps.Create(ctx, sp.ID, nil, "B", u.ID, pages.KindDocument)
	s := NewStore(conn)

	doc := func(text, link string) string {
		mention := ""
		if link != "" {
			mention = `,{"type":"pageMention","attrs":{"id":"` + link + `"}}`
		}
		return `{"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"` + text + `"}` + mention + `]}]}`
	}

	// First snapshot: one version. Within the window: same version updated.
	if err := s.Compact(ctx, a.ID, 1, []byte{1}, doc("one", ""), u.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.Compact(ctx, a.ID, 2, []byte{1}, doc("two", b.ID), u.ID); err != nil {
		t.Fatal(err)
	}
	vs, err := s.Versions(ctx, a.ID)
	if err != nil || len(vs) != 1 || vs[0].UserName != "Anna" {
		t.Fatalf("versions = %+v, %v", vs, err)
	}
	if c, _ := s.VersionContent(ctx, a.ID, vs[0].ID); c != doc("two", b.ID) {
		t.Fatalf("version content = %s", c)
	}
	// Another page's version id is not readable through this page.
	if _, err := s.VersionContent(ctx, b.ID, vs[0].ID); err == nil {
		t.Fatal("version of another page readable")
	}

	// An older newest version: the next change starts a new one.
	conn.Exec(`UPDATE page_versions SET created_at = '2000-01-01T00:00:00.000Z'`)
	if err := s.Compact(ctx, a.ID, 3, []byte{1}, doc("three", b.ID), u.ID); err != nil {
		t.Fatal(err)
	}
	// Unchanged content adds nothing.
	conn.Exec(`UPDATE page_versions SET created_at = '2000-01-01T00:00:00.000Z'`)
	if err := s.Compact(ctx, a.ID, 4, []byte{1}, doc("three", b.ID), u.ID); err != nil {
		t.Fatal(err)
	}
	if vs, _ := s.Versions(ctx, a.ID); len(vs) != 2 {
		t.Fatalf("want 2 versions, got %d", len(vs))
	}

	// Links: A -> B shows as a backlink of B; a link to a missing page and
	// to itself are ignored; removing the mention removes the link.
	bl, err := s.Backlinks(ctx, b.ID)
	if err != nil || len(bl) != 1 || bl[0].ID != a.ID {
		t.Fatalf("backlinks = %+v, %v", bl, err)
	}
	if err := s.Compact(ctx, a.ID, 5, []byte{1}, doc("x", "no-such-page"), u.ID); err != nil {
		t.Fatalf("missing link target failed the snapshot: %v", err)
	}
	if bl, _ := s.Backlinks(ctx, b.ID); len(bl) != 0 {
		t.Fatalf("stale backlink: %+v", bl)
	}

	// Béla edits, Anna's open editor sends the snapshot: Béla is the author.
	bela, _ := users.NewStore(conn).Create(ctx, "bela", "h", "Béla")
	if err := s.Append(ctx, b.ID, 1, []byte{1}, bela.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.Compact(ctx, b.ID, 1, []byte{1}, doc("by bela", ""), u.ID); err != nil {
		t.Fatal(err)
	}
	if vs, _ := s.Versions(ctx, b.ID); len(vs) != 1 || vs[0].UserName != "Béla" {
		t.Fatalf("author = %+v, want Béla", vs)
	}
}
