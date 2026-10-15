package httpapi

import (
	"context"
	"testing"
)

// History and backlinks over HTTP: a viewer reads a page's versions;
// backlinks only list pages in spaces the caller can see, so a link from
// someone's private page doesn't reveal its title.
func TestHistoryAndBacklinksAccess(t *testing.T) {
	e := newTestEnv(t, RegOpen)
	alice, bob := e.client(), e.client()
	e.do(t, alice, "POST", "/api/auth/register", m{"email": "alice@example.hu", "username": "alice", "password": "hosszujelszo"}, nil)
	e.do(t, bob, "POST", "/api/auth/register", m{"email": "bob@example.hu", "username": "bob", "password": "hosszujelszo"}, nil)

	var team, own, shared, private, target, secret, public idResp
	e.do(t, alice, "POST", "/api/groups", m{"name": "Team"}, &team)
	e.do(t, alice, "POST", "/api/groups/"+team.ID+"/members", m{"username": "bob", "role": "viewer"}, nil)
	e.do(t, alice, "POST", "/api/spaces", m{"name": "Shared", "type": "shared", "group_id": team.ID}, &shared)
	// A group Bob isn't in: its spaces are out of his reach.
	e.do(t, alice, "POST", "/api/groups", m{"name": "Alice's"}, &own)
	e.do(t, alice, "POST", "/api/spaces", m{"name": "Mine", "type": "private", "group_id": own.ID}, &private)
	e.do(t, alice, "POST", "/api/spaces/"+shared.ID+"/pages", m{"title": "Target"}, &target)
	e.do(t, alice, "POST", "/api/spaces/"+shared.ID+"/pages", m{"title": "Public notes"}, &public)
	e.do(t, alice, "POST", "/api/spaces/"+private.ID+"/pages", m{"title": "Secret plan"}, &secret)

	link := `{"type":"doc","content":[{"type":"paragraph","content":[{"type":"pageMention","attrs":{"id":"` + target.ID + `"}}]}]}`
	ctx := context.Background()
	for i, id := range []string{public.ID, secret.ID} {
		if err := e.collab.Compact(ctx, id, int64(i+1), []byte{1}, link, ""); err != nil {
			t.Fatal(err)
		}
	}

	var aliceSees, bobSees []struct{ ID, Title string }
	e.do(t, alice, "GET", "/api/pages/"+target.ID+"/backlinks", nil, &aliceSees)
	e.do(t, bob, "GET", "/api/pages/"+target.ID+"/backlinks", nil, &bobSees)
	if len(aliceSees) != 2 {
		t.Fatalf("alice backlinks = %+v, want both", aliceSees)
	}
	if len(bobSees) != 1 || bobSees[0].ID != public.ID {
		t.Fatalf("bob backlinks = %+v, want only the shared page", bobSees)
	}

	var versions []struct{ ID string }
	if code := e.do(t, bob, "GET", "/api/pages/"+public.ID+"/versions", nil, &versions); code != 200 || len(versions) != 1 {
		t.Fatalf("bob versions = %d %+v", code, versions)
	}
	var v struct {
		ContentJSON string `json:"content_json"`
	}
	if code := e.do(t, bob, "GET", "/api/pages/"+public.ID+"/versions/"+versions[0].ID, nil, &v); code != 200 || v.ContentJSON != link {
		t.Fatalf("bob version = %d %q", code, v.ContentJSON)
	}
	// Not through a page Bob can't see, nor through another page's id.
	if code := e.do(t, bob, "GET", "/api/pages/"+secret.ID+"/versions", nil, nil); code != 404 {
		t.Fatalf("bob versions of private page = %d, want 404", code)
	}
	if code := e.do(t, bob, "GET", "/api/pages/"+target.ID+"/versions/"+versions[0].ID, nil, nil); code != 404 {
		t.Fatalf("version through another page = %d, want 404", code)
	}
}
