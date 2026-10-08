package httpapi

import "testing"

func TestPersonColumn(t *testing.T) {
	e := newTestEnv(t, RegOpen)
	alice, bob, carol := e.client(), e.client(), e.client()
	e.do(t, alice, "POST", "/api/auth/register", m{"email": "alice@example.hu", "username": "alice", "password": "hosszujelszo", "display_name": "Alice"}, nil)
	e.do(t, bob, "POST", "/api/auth/register", m{"email": "bob@example.hu", "username": "bob", "password": "hosszujelszo", "display_name": "Bob"}, nil)
	e.do(t, carol, "POST", "/api/auth/register", m{"email": "carol@example.hu", "username": "carol", "password": "hosszujelszo"}, nil)

	var g, sp, dbPage, row, prop idResp
	e.do(t, alice, "POST", "/api/groups", m{"name": "Team"}, &g)
	if code := e.do(t, alice, "POST", "/api/groups/"+g.ID+"/members", m{"username": "bob", "role": "editor"}, nil); code != 201 && code != 200 {
		t.Fatalf("add bob = %d", code)
	}
	e.do(t, alice, "POST", "/api/spaces", m{"name": "S", "type": "shared", "group_id": g.ID}, &sp)
	e.do(t, alice, "POST", "/api/spaces/"+sp.ID+"/pages", m{"title": "Tasks", "kind": "database"}, &dbPage)
	e.do(t, alice, "POST", "/api/spaces/"+sp.ID+"/pages", m{"title": "Row", "parent_page_id": dbPage.ID, "kind": "document"}, &row)
	if code := e.do(t, alice, "POST", "/api/pages/"+dbPage.ID+"/properties", m{"name": "Owner", "type": "person"}, &prop); code != 201 {
		t.Fatalf("create person column = %d", code)
	}

	var people []struct {
		ID          string `json:"id"`
		Username    string `json:"username"`
		DisplayName string `json:"display_name"`
		Email       string `json:"email"`
	}
	if code := e.do(t, alice, "GET", "/api/pages/"+dbPage.ID+"/people", nil, &people); code != 200 {
		t.Fatalf("people = %d", code)
	}
	ids := map[string]string{}
	for _, p := range people {
		ids[p.Username] = p.ID
		if p.Email != "" {
			t.Fatal("people must not expose email addresses")
		}
	}
	if len(people) != 2 || ids["alice"] == "" || ids["bob"] == "" {
		t.Fatalf("people = %+v, want alice and bob only", people)
	}
	if code := e.do(t, carol, "GET", "/api/pages/"+dbPage.ID+"/people", nil, nil); code != 404 && code != 403 {
		t.Fatalf("outsider people = %d", code)
	}

	set := func(v any) int {
		return e.do(t, alice, "PUT", "/api/pages/"+row.ID+"/properties/"+prop.ID, m{"value": v}, nil)
	}
	if code := set(m{"user_id": ids["bob"]}); code != 204 {
		t.Fatalf("set bob = %d", code)
	}
	if code := set(nil); code != 204 {
		t.Fatalf("clear = %d", code)
	}
	var carolID struct {
		ID string `json:"id"`
	}
	e.do(t, carol, "GET", "/api/me", nil, &carolID)
	if code := set(m{"user_id": carolID.ID}); code != 400 {
		t.Fatalf("set someone outside the space = %d, want 400", code)
	}
	if code := set("free text"); code != 400 {
		t.Fatalf("set a plain string = %d, want 400", code)
	}

	var table struct {
		Properties []struct {
			PageID string `json:"page_id"`
		} `json:"properties"`
	}
	e.do(t, alice, "GET", "/api/pages/"+dbPage.ID+"/rows", nil, &table)
	if len(table.Properties) != 1 || table.Properties[0].PageID != dbPage.ID {
		t.Fatalf("property page_id = %+v", table.Properties)
	}
}
