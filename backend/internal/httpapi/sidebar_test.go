package httpapi

import (
	"encoding/json"
	"net/http"
	"testing"
)

// GET /api/sidebar must show exactly what the per-resource endpoints it
// replaces showed: the caller's groups and spaces in their order, each
// space's tree without archived pages, and nothing of other people's.
func TestSidebar(t *testing.T) {
	e := newTestEnv(t, RegOpen)
	alice, bob := e.client(), e.client()
	e.do(t, alice, "POST", "/api/auth/register", m{"email": "alice@example.hu", "username": "alice", "password": "hosszujelszo"}, nil)
	e.do(t, bob, "POST", "/api/auth/register", m{"email": "bob@example.hu", "username": "bob", "password": "hosszujelszo"}, nil)

	var g, sp, parent, child, gone idResp
	e.do(t, alice, "POST", "/api/groups", m{"name": "Work"}, &g)
	e.do(t, alice, "POST", "/api/spaces", m{"name": "Notes", "type": "private", "group_id": g.ID}, &sp)
	e.do(t, alice, "POST", "/api/spaces/"+sp.ID+"/pages", m{"title": "Plan", "kind": "document"}, &parent)
	e.do(t, alice, "POST", "/api/spaces/"+sp.ID+"/pages", m{"title": "Step", "parent_page_id": parent.ID, "kind": "document"}, &child)
	e.do(t, alice, "POST", "/api/spaces/"+sp.ID+"/pages", m{"title": "Old", "kind": "document"}, &gone)
	e.do(t, alice, "DELETE", "/api/pages/"+gone.ID, nil, nil)

	type node struct {
		ID       string `json:"id"`
		Title    string `json:"title"`
		Children []node `json:"children"`
	}
	type sidebar struct {
		Groups []struct {
			ID string `json:"id"`
		} `json:"groups"`
		Spaces []struct {
			ID    string `json:"id"`
			Pages []node `json:"pages"`
		} `json:"spaces"`
	}

	var got sidebar
	if code := e.do(t, alice, "GET", "/api/sidebar", nil, &got); code != 200 {
		t.Fatalf("sidebar = %d", code)
	}

	// Same groups and spaces, same order, as the old endpoints.
	var oldGroups, oldSpaces []struct {
		ID string `json:"id"`
	}
	e.do(t, alice, "GET", "/api/groups", nil, &oldGroups)
	e.do(t, alice, "GET", "/api/spaces", nil, &oldSpaces)
	if len(got.Groups) != len(oldGroups) || len(got.Spaces) != len(oldSpaces) {
		t.Fatalf("counts: sidebar %d/%d, old %d/%d", len(got.Groups), len(got.Spaces), len(oldGroups), len(oldSpaces))
	}
	for i := range oldGroups {
		if got.Groups[i].ID != oldGroups[i].ID {
			t.Fatalf("group order differs at %d", i)
		}
	}
	for i := range oldSpaces {
		if got.Spaces[i].ID != oldSpaces[i].ID {
			t.Fatalf("space order differs at %d", i)
		}
		var oldTree []node
		e.do(t, alice, "GET", "/api/spaces/"+oldSpaces[i].ID+"/pages", nil, &oldTree)
		a, _ := json.Marshal(oldTree)
		b, _ := json.Marshal(got.Spaces[i].Pages)
		if string(a) != string(b) {
			t.Fatalf("tree of %s differs:\nold %s\nnew %s", oldSpaces[i].ID, a, b)
		}
	}

	// The tree itself: Plan > Step, the archived page left out.
	for _, s := range got.Spaces {
		if s.ID != sp.ID {
			continue
		}
		if len(s.Pages) != 1 || s.Pages[0].ID != parent.ID || len(s.Pages[0].Children) != 1 || s.Pages[0].Children[0].ID != child.ID {
			t.Fatalf("tree = %+v", s.Pages)
		}
	}

	// Bob sees none of Alice's groups or spaces.
	var bobs sidebar
	e.do(t, bob, "GET", "/api/sidebar", nil, &bobs)
	for _, s := range bobs.Spaces {
		if s.ID == sp.ID {
			t.Fatal("bob sees alice's space")
		}
	}
	for _, gr := range bobs.Groups {
		if gr.ID == g.ID {
			t.Fatal("bob sees alice's group")
		}
	}

	// Signed out: no sidebar.
	if code := e.do(t, e.client(), "GET", "/api/sidebar", nil, nil); code != 401 {
		t.Fatalf("anonymous = %d", code)
	}
}

// Each space and group carries the caller's own role, so the UI only
// offers what the server will allow (a viewer gets no edit controls).
func TestSidebarRoles(t *testing.T) {
	e := newTestEnv(t, RegOpen)
	alice, bob := e.client(), e.client()
	e.do(t, alice, "POST", "/api/auth/register", m{"email": "alice@example.hu", "username": "alice", "password": "hosszujelszo"}, nil)
	e.do(t, bob, "POST", "/api/auth/register", m{"email": "bob@example.hu", "username": "bob", "password": "hosszujelszo"}, nil)

	var g, sp idResp
	e.do(t, alice, "POST", "/api/groups", m{"name": "Work"}, &g)
	e.do(t, alice, "POST", "/api/spaces", m{"name": "Notes", "type": "private", "group_id": g.ID}, &sp)
	if code := e.do(t, alice, "POST", "/api/groups/"+g.ID+"/members", m{"username": "bob", "role": "viewer"}, nil); code != 201 && code != 200 {
		t.Fatalf("add bob = %d", code)
	}

	type withRole struct {
		ID   string `json:"id"`
		Role string `json:"role"`
	}
	roles := func(c *http.Client) (map[string]string, map[string]string) {
		var got struct {
			Groups []withRole `json:"groups"`
			Spaces []withRole `json:"spaces"`
		}
		e.do(t, c, "GET", "/api/sidebar", nil, &got)
		gs, ss := map[string]string{}, map[string]string{}
		for _, x := range got.Groups {
			gs[x.ID] = x.Role
		}
		for _, x := range got.Spaces {
			ss[x.ID] = x.Role
		}
		return gs, ss
	}

	if gs, ss := roles(alice); gs[g.ID] != "owner" || ss[sp.ID] != "owner" {
		t.Fatalf("alice roles: group %q space %q", gs[g.ID], ss[sp.ID])
	}
	if gs, ss := roles(bob); gs[g.ID] != "viewer" || ss[sp.ID] != "viewer" {
		t.Fatalf("bob roles: group %q space %q", gs[g.ID], ss[sp.ID])
	}
}
