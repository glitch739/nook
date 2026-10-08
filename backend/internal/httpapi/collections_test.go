package httpapi

import "testing"

func TestCollections(t *testing.T) {
	e := newTestEnv(t, RegOpen)
	alice, bob := e.client(), e.client()
	e.do(t, alice, "POST", "/api/auth/register", m{"email": "alice@example.hu", "username": "alice", "password": "hosszujelszo"}, nil)
	e.do(t, bob, "POST", "/api/auth/register", m{"email": "bob@example.hu", "username": "bob", "password": "hosszujelszo"}, nil)
	var g, sp idResp
	e.do(t, alice, "POST", "/api/groups", m{"name": "Work"}, &g)
	e.do(t, alice, "POST", "/api/spaces", m{"name": "Notes", "type": "private", "group_id": g.ID}, &sp)
	pagesURL := "/api/spaces/" + sp.ID + "/pages"

	var coll struct {
		ID   string `json:"id"`
		Kind string `json:"kind"`
		Icon string `json:"icon"`
	}
	if code := e.do(t, alice, "POST", pagesURL, m{"title": "Meetings", "kind": "collection"}, &coll); code != 201 || coll.Kind != "collection" || coll.Icon != "lucide:library-big" {
		t.Fatalf("create collection = %d %+v", code, coll)
	}

	// Documents and databases go in; another collection doesn't.
	var doc, db, other, loose idResp
	e.do(t, alice, "POST", pagesURL, m{"title": "Weekly", "parent_page_id": coll.ID, "kind": "document"}, &doc)
	e.do(t, alice, "POST", pagesURL, m{"title": "Tasks", "parent_page_id": coll.ID, "kind": "database"}, &db)
	if code := e.do(t, alice, "POST", pagesURL, m{"title": "Nested", "parent_page_id": coll.ID, "kind": "collection"}, nil); code != 400 {
		t.Fatalf("collection in collection = %d", code)
	}
	if code := e.do(t, alice, "POST", pagesURL, m{"title": "In db", "parent_page_id": db.ID, "kind": "collection"}, nil); code != 400 {
		t.Fatalf("collection in database = %d", code)
	}
	e.do(t, alice, "POST", pagesURL, m{"title": "Other", "kind": "collection"}, &other)
	if code := e.do(t, alice, "PUT", "/api/pages/"+other.ID+"/move", m{"space_id": sp.ID, "parent_page_id": coll.ID}, nil); code != 400 {
		t.Fatalf("move collection into collection = %d", code)
	}
	e.do(t, alice, "POST", pagesURL, m{"title": "Loose", "kind": "document"}, &loose)
	if code := e.do(t, alice, "PUT", "/api/pages/"+loose.ID+"/move", m{"space_id": sp.ID, "parent_page_id": coll.ID}, nil); code != 200 {
		t.Fatalf("move document into collection = %d", code)
	}

	// The list: all three, most recently edited first.
	e.do(t, alice, "PUT", "/api/pages/"+doc.ID, m{"title": "Weekly (edited)"}, nil)
	var raw []map[string]any
	if code := e.do(t, alice, "GET", "/api/pages/"+coll.ID+"/items", nil, &raw); code != 200 || len(raw) != 3 {
		t.Fatalf("items = %d %v", code, raw)
	}
	if raw[0]["id"] != doc.ID || raw[0]["title"] != "Weekly (edited)" {
		t.Fatalf("first item = %v, want the just-edited document", raw[0])
	}

	// Only collections have items; outsiders see nothing.
	if code := e.do(t, alice, "GET", "/api/pages/"+doc.ID+"/items", nil, nil); code != 404 {
		t.Fatalf("items of a document = %d", code)
	}
	if code := e.do(t, bob, "GET", "/api/pages/"+coll.ID+"/items", nil, nil); code != 404 {
		t.Fatalf("bob items = %d", code)
	}
}
