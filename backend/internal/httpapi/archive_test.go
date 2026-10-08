package httpapi

import (
	"testing"
)

type idResp struct {
	ID string `json:"id"`
}

type archiveResp struct {
	Pages []struct {
		ID          string `json:"id"`
		Title       string `json:"title"`
		ParentTitle string `json:"parent_title"`
		SubPages    int    `json:"sub_pages"`
	} `json:"pages"`
	Spaces []struct {
		ID            string `json:"id"`
		GroupArchived bool   `json:"group_archived"`
	} `json:"spaces"`
	Groups []struct {
		ID string `json:"id"`
	} `json:"groups"`
}

type treeNode struct {
	ID       string     `json:"id"`
	Title    string     `json:"title"`
	Children []treeNode `json:"children"`
}

func flatten(ns []treeNode, depth int, out map[string]int) {
	for _, n := range ns {
		out[n.Title] = depth
		flatten(n.Children, depth+1, out)
	}
}

func TestArchiveAndRestore(t *testing.T) {
	e := newTestEnv(t, RegOpen)
	alice := e.client()
	if code := e.do(t, alice, "POST", "/api/auth/register", m{"email": "alice@example.hu", "username": "alice", "password": "hosszujelszo"}, nil); code != 201 {
		t.Fatalf("register = %d", code)
	}
	var grps []idResp
	e.do(t, alice, "GET", "/api/groups", nil, &grps)
	var g1 idResp
	e.do(t, alice, "POST", "/api/groups", m{"name": "Work"}, &g1)
	var sp idResp
	if code := e.do(t, alice, "POST", "/api/spaces", m{"name": "Notes", "type": "private", "group_id": g1.ID}, &sp); code != 201 {
		t.Fatalf("create space = %d", code)
	}
	mk := func(title string, parent *string) string {
		var p idResp
		if code := e.do(t, alice, "POST", "/api/spaces/"+sp.ID+"/pages", m{"title": title, "parent_page_id": parent, "kind": "document"}, &p); code != 201 {
			t.Fatalf("create page %s = %d", title, code)
		}
		return p.ID
	}
	a := mk("A", nil)
	b := mk("B", &a)
	c := mk("C", &b)
	tree := func() map[string]int {
		var ns []treeNode
		e.do(t, alice, "GET", "/api/spaces/"+sp.ID+"/pages", nil, &ns)
		out := map[string]int{}
		flatten(ns, 0, out)
		return out
	}
	list := func() archiveResp {
		var ar archiveResp
		if code := e.do(t, alice, "GET", "/api/archive", nil, &ar); code != 200 {
			t.Fatalf("archive list = %d", code)
		}
		return ar
	}

	// C archived on its own first, then A (taking B with it).
	e.do(t, alice, "DELETE", "/api/pages/"+c, nil, nil)
	e.do(t, alice, "DELETE", "/api/pages/"+a, nil, nil)
	if got := tree(); len(got) != 0 {
		t.Fatalf("tree after archiving A: %v", got)
	}
	ar := list()
	if len(ar.Pages) != 2 {
		t.Fatalf("archived batches = %d, want 2 (A with B, and C)", len(ar.Pages))
	}
	for _, p := range ar.Pages {
		if p.ID == a && p.SubPages != 1 {
			t.Fatalf("A sub_pages = %d, want 1 (B; C was archived separately)", p.SubPages)
		}
		if p.ID == c && p.ParentTitle != "B" {
			t.Fatalf("C parent_title = %q", p.ParentTitle)
		}
	}

	// C's parent is still archived: C comes back at the top level.
	if code := e.do(t, alice, "POST", "/api/pages/"+c+"/restore", nil, nil); code != 204 {
		t.Fatalf("restore C = %d", code)
	}
	if got := tree(); got["C"] != 0 || len(got) != 1 {
		t.Fatalf("tree after restoring C: %v", got)
	}
	// A brings B back, nested as before; C stays where it was restored.
	if code := e.do(t, alice, "POST", "/api/pages/"+a+"/restore", nil, nil); code != 204 {
		t.Fatalf("restore A = %d", code)
	}
	if got := tree(); got["A"] != 0 || got["B"] != 1 || got["C"] != 0 {
		t.Fatalf("tree after restoring A: %v", got)
	}
	if code := e.do(t, alice, "POST", "/api/pages/"+a+"/restore", nil, nil); code != 409 {
		t.Fatalf("restore live page = %d, want 409", code)
	}

	// Someone else sees none of it and can't restore it.
	bob := e.client()
	e.do(t, bob, "POST", "/api/auth/register", m{"email": "bob@example.hu", "username": "bob", "password": "hosszujelszo"}, nil)
	e.do(t, alice, "DELETE", "/api/pages/"+a, nil, nil)
	var bobList archiveResp
	e.do(t, bob, "GET", "/api/archive", nil, &bobList)
	if len(bobList.Pages)+len(bobList.Spaces)+len(bobList.Groups) != 0 {
		t.Fatalf("bob sees %+v", bobList)
	}
	if code := e.do(t, bob, "POST", "/api/pages/"+a+"/restore", nil, nil); code != 403 && code != 404 {
		t.Fatalf("bob restore page = %d", code)
	}

	// Space archived, then its (now empty) group: restoring the space
	// brings the group back too.
	e.do(t, alice, "DELETE", "/api/spaces/"+sp.ID, nil, nil)
	if code := e.do(t, alice, "DELETE", "/api/groups/"+g1.ID, nil, nil); code != 204 {
		t.Fatalf("archive group = %d", code)
	}
	ar = list()
	if len(ar.Spaces) != 1 || !ar.Spaces[0].GroupArchived || len(ar.Groups) != 0 {
		t.Fatalf("archive after space+group: %+v", ar)
	}
	if len(ar.Pages) != 0 {
		t.Fatalf("pages in an archived space must not be listed on their own: %+v", ar.Pages)
	}
	if code := e.do(t, bob, "POST", "/api/spaces/"+sp.ID+"/restore", nil, nil); code != 403 {
		t.Fatalf("bob restore space = %d", code)
	}
	if code := e.do(t, alice, "POST", "/api/spaces/"+sp.ID+"/restore", nil, nil); code != 204 {
		t.Fatalf("restore space = %d", code)
	}
	var gs []idResp
	e.do(t, alice, "GET", "/api/groups", nil, &gs)
	found := false
	for _, g := range gs {
		found = found || g.ID == g1.ID
	}
	if !found {
		t.Fatal("group not restored with its space")
	}
	if got := tree(); got["C"] != 0 {
		t.Fatalf("space pages not back: %v", got)
	}

	// An empty archived group on its own.
	var g2 idResp
	e.do(t, alice, "POST", "/api/groups", m{"name": "Old"}, &g2)
	e.do(t, alice, "DELETE", "/api/groups/"+g2.ID, nil, nil)
	if ar = list(); len(ar.Groups) != 1 || ar.Groups[0].ID != g2.ID {
		t.Fatalf("archived empty group not listed: %+v", ar.Groups)
	}
	if code := e.do(t, alice, "POST", "/api/groups/"+g2.ID+"/restore", nil, nil); code != 204 {
		t.Fatalf("restore group = %d", code)
	}

	// Permanent delete: only archived things; a separately archived child
	// survives its parent's batch being deleted.
	var x idResp
	e.do(t, alice, "POST", "/api/spaces/"+sp.ID+"/pages", m{"title": "X", "kind": "document"}, &x)
	xid := x.ID
	var y idResp
	e.do(t, alice, "POST", "/api/spaces/"+sp.ID+"/pages", m{"title": "Y", "parent_page_id": xid, "kind": "document"}, &y)
	var z idResp
	e.do(t, alice, "POST", "/api/spaces/"+sp.ID+"/pages", m{"title": "Z", "parent_page_id": y.ID, "kind": "document"}, &z)
	if code := e.do(t, alice, "DELETE", "/api/archive/pages/"+xid, nil, nil); code != 409 {
		t.Fatalf("delete live page = %d, want 409", code)
	}
	e.do(t, alice, "DELETE", "/api/pages/"+z.ID, nil, nil)
	e.do(t, alice, "DELETE", "/api/pages/"+xid, nil, nil)
	if code := e.do(t, bob, "DELETE", "/api/archive/pages/"+xid, nil, nil); code != 403 && code != 404 {
		t.Fatalf("bob delete = %d", code)
	}
	if code := e.do(t, alice, "DELETE", "/api/archive/pages/"+xid, nil, nil); code != 204 {
		t.Fatalf("delete archived page = %d", code)
	}
	if code := e.do(t, alice, "GET", "/api/pages/"+y.ID, nil, nil); code != 404 {
		t.Fatalf("Y should be gone with X, got %d", code)
	}
	ar = list()
	zListed := false
	for _, p := range ar.Pages {
		zListed = zListed || p.ID == z.ID
	}
	if !zListed {
		t.Fatal("separately archived Z must survive and stay in the archive")
	}
	if code := e.do(t, alice, "DELETE", "/api/archive/groups/"+g1.ID, nil, nil); code != 409 {
		t.Fatalf("delete live group = %d", code)
	}
	e.do(t, alice, "DELETE", "/api/spaces/"+sp.ID, nil, nil)
	if code := e.do(t, alice, "DELETE", "/api/archive/spaces/"+sp.ID, nil, nil); code != 204 {
		t.Fatalf("delete archived space = %d", code)
	}
	if code := e.do(t, alice, "GET", "/api/pages/"+z.ID, nil, nil); code != 404 {
		t.Fatal("pages of a deleted space must be gone")
	}
	e.do(t, alice, "DELETE", "/api/groups/"+g1.ID, nil, nil)
	if code := e.do(t, alice, "DELETE", "/api/archive/groups/"+g1.ID, nil, nil); code != 204 {
		t.Fatalf("delete archived empty group = %d", code)
	}

	// A page without a cover answers 204, not 404.
	var nc idResp
	var sp2 idResp
	e.do(t, alice, "POST", "/api/spaces", m{"name": "Covers", "type": "private", "group_id": g2.ID}, &sp2)
	e.do(t, alice, "POST", "/api/spaces/"+sp2.ID+"/pages", m{"title": "No cover", "kind": "document"}, &nc)
	if code := e.do(t, alice, "GET", "/api/pages/"+nc.ID+"/cover/meta", nil, nil); code != 204 {
		t.Fatalf("cover meta without a cover = %d, want 204", code)
	}
}
