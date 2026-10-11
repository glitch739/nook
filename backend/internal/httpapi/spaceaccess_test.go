package httpapi

import "testing"

// An editor can't move a shared space out of its group, and an archived
// space's pages stop opening by direct link until the space is restored.
func TestSpaceMoveAndArchiveAccess(t *testing.T) {
	e := newTestEnv(t, RegOpen)
	alice, bob := e.client(), e.client()
	e.do(t, alice, "POST", "/api/auth/register", m{"email": "alice@example.hu", "username": "alice", "password": "hosszujelszo"}, nil)
	e.do(t, bob, "POST", "/api/auth/register", m{"email": "bob@example.hu", "username": "bob", "password": "hosszujelszo"}, nil)

	var team, bobs, sp, page idResp
	e.do(t, alice, "POST", "/api/groups", m{"name": "Team"}, &team)
	if code := e.do(t, alice, "POST", "/api/groups/"+team.ID+"/members", m{"username": "bob", "role": "editor"}, nil); code != 201 {
		t.Fatalf("invite bob = %d", code)
	}
	e.do(t, bob, "POST", "/api/groups", m{"name": "Bob's"}, &bobs)
	e.do(t, alice, "POST", "/api/spaces", m{"name": "Shared", "type": "shared", "group_id": team.ID}, &sp)
	e.do(t, alice, "POST", "/api/spaces/"+sp.ID+"/pages", m{"title": "Doc"}, &page)

	if code := e.do(t, bob, "PUT", "/api/spaces/"+sp.ID+"/move", m{"group_id": bobs.ID}, nil); code != 404 {
		t.Fatalf("editor moves space = %d, want 404", code)
	}
	if code := e.do(t, bob, "GET", "/api/pages/"+page.ID, nil, nil); code != 200 {
		t.Fatalf("bob still sees the page = %d, want 200", code)
	}

	if code := e.do(t, alice, "DELETE", "/api/spaces/"+sp.ID, nil, nil); code != 204 {
		t.Fatalf("archive space = %d", code)
	}
	if code := e.do(t, bob, "GET", "/api/pages/"+page.ID, nil, nil); code != 404 {
		t.Fatalf("page of archived space = %d, want 404", code)
	}
	if code := e.do(t, bob, "PUT", "/api/pages/"+page.ID, m{"title": "x"}, nil); code != 404 {
		t.Fatalf("edit page of archived space = %d, want 404", code)
	}
	if code := e.do(t, alice, "POST", "/api/spaces/"+sp.ID+"/restore", nil, nil); code != 204 {
		t.Fatalf("restore space = %d, want 204", code)
	}
	if code := e.do(t, bob, "GET", "/api/pages/"+page.ID, nil, nil); code != 200 {
		t.Fatalf("page after restore = %d, want 200", code)
	}

	var aliceGroup idResp
	e.do(t, alice, "POST", "/api/groups", m{"name": "Alice's"}, &aliceGroup)
	if code := e.do(t, alice, "PUT", "/api/spaces/"+sp.ID+"/move", m{"group_id": aliceGroup.ID}, nil); code != 200 {
		t.Fatalf("owner moves space = %d, want 200", code)
	}
}
