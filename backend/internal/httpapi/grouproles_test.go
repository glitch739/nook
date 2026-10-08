package httpapi

import (
	"net/http"
	"testing"
)

func TestGroupRoleHierarchy(t *testing.T) {
	e := newTestEnv(t, RegOpen)
	users := map[string]*http.Client{}
	for _, n := range []string{"alice", "bob", "carol", "dave"} {
		c := e.client()
		e.do(t, c, "POST", "/api/auth/register", m{"email": n + "@example.hu", "username": n, "password": "hosszujelszo"}, nil)
		users[n] = c
	}
	id := func(n string) string {
		var me struct {
			ID string `json:"id"`
		}
		e.do(t, users[n], "GET", "/api/me", nil, &me)
		return me.ID
	}
	alice := users["alice"]
	var g idResp
	e.do(t, alice, "POST", "/api/groups", m{"name": "Team"}, &g)
	for n, role := range map[string]string{"bob": "admin", "carol": "owner", "dave": "viewer"} {
		e.do(t, alice, "POST", "/api/groups/"+g.ID+"/members", m{"username": n, "role": "viewer"}, nil)
		if code := e.do(t, alice, "PUT", "/api/groups/"+g.ID+"/members/"+id(n), m{"role": role}, nil); code != 204 {
			t.Fatalf("alice sets %s=%s: %d", n, role, code)
		}
	}
	bob := users["bob"]
	if code := e.do(t, bob, "PUT", "/api/groups/"+g.ID+"/members/"+id("carol"), m{"role": "viewer"}, nil); code != 403 {
		t.Fatalf("admin demotes an owner = %d, want 403", code)
	}
	if code := e.do(t, bob, "DELETE", "/api/groups/"+g.ID+"/members/"+id("carol"), nil, nil); code != 403 {
		t.Fatalf("admin removes an owner = %d, want 403", code)
	}
	if code := e.do(t, bob, "PUT", "/api/groups/"+g.ID+"/members/"+id("dave"), m{"role": "editor"}, nil); code != 204 {
		t.Fatalf("admin promotes a viewer = %d, want 204", code)
	}
	if code := e.do(t, bob, "DELETE", "/api/groups/"+g.ID+"/members/"+id("dave"), nil, nil); code != 204 {
		t.Fatalf("admin removes an editor = %d, want 204", code)
	}
	if code := e.do(t, alice, "PUT", "/api/groups/"+g.ID+"/members/"+id("carol"), m{"role": "admin"}, nil); code != 204 {
		t.Fatalf("owner changes another owner = %d, want 204", code)
	}
}
