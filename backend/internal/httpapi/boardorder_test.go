package httpapi

import (
	"fmt"
	"reflect"
	"testing"
)

// The board's card order: stored apart from the rows' own order, unknown
// ids and repeats dropped, editors only, and read back with the rows.
func TestBoardOrder(t *testing.T) {
	e := newTestEnv(t, RegOpen)
	alice, bob := e.client(), e.client()
	e.do(t, alice, "POST", "/api/auth/register", m{"email": "alice@example.hu", "username": "alice", "password": "hosszujelszo"}, nil)
	e.do(t, bob, "POST", "/api/auth/register", m{"email": "bob@example.hu", "username": "bob", "password": "hosszujelszo"}, nil)

	var team, sp, db, r1, r2 idResp
	e.do(t, alice, "POST", "/api/groups", m{"name": "Team"}, &team)
	e.do(t, alice, "POST", "/api/groups/"+team.ID+"/members", m{"username": "bob", "role": "viewer"}, nil)
	e.do(t, alice, "POST", "/api/spaces", m{"name": "Shared", "type": "shared", "group_id": team.ID}, &sp)
	e.do(t, alice, "POST", "/api/spaces/"+sp.ID+"/pages", m{"title": "Tasks", "kind": "database"}, &db)
	e.do(t, alice, "POST", "/api/spaces/"+sp.ID+"/pages", m{"title": "One", "parent_page_id": db.ID}, &r1)
	e.do(t, alice, "POST", "/api/spaces/"+sp.ID+"/pages", m{"title": "Two", "parent_page_id": db.ID}, &r2)

	var rows struct {
		BoardOrder []string `json:"board_order"`
	}
	e.do(t, alice, "GET", "/api/pages/"+db.ID+"/rows", nil, &rows)
	if rows.BoardOrder == nil || len(rows.BoardOrder) != 0 {
		t.Fatalf("initial board order = %#v, want []", rows.BoardOrder)
	}

	if code := e.do(t, alice, "PUT", "/api/pages/"+db.ID+"/board-order", m{"row_ids": []string{r2.ID, "nope", r1.ID, r2.ID}}, nil); code != 204 {
		t.Fatalf("set board order = %d", code)
	}
	e.do(t, bob, "GET", "/api/pages/"+db.ID+"/rows", nil, &rows)
	if !reflect.DeepEqual(rows.BoardOrder, []string{r2.ID, r1.ID}) {
		t.Fatalf("board order = %v, want [two one]", rows.BoardOrder)
	}
	// A big database's full order (~3000 ids) fits; unknown ids are dropped.
	many := make([]string, 3000)
	for i := range many {
		many[i] = fmt.Sprintf("%036d", i)
	}
	if code := e.do(t, alice, "PUT", "/api/pages/"+db.ID+"/board-order", m{"row_ids": append(many, r1.ID, r2.ID)}, nil); code != 204 {
		t.Fatalf("set a long board order = %d, want 204", code)
	}
	if code := e.do(t, bob, "PUT", "/api/pages/"+db.ID+"/board-order", m{"row_ids": []string{r1.ID}}, nil); code != 404 {
		t.Fatalf("viewer sets board order = %d, want 404", code)
	}
	// Column order, per grouping property; layout only.
	var status, owner, title idResp
	e.do(t, alice, "POST", "/api/pages/"+db.ID+"/properties", m{"name": "Status", "type": "select"}, &status)
	e.do(t, alice, "POST", "/api/pages/"+db.ID+"/properties", m{"name": "Owner", "type": "person"}, &owner)
	e.do(t, alice, "POST", "/api/pages/"+db.ID+"/properties", m{"name": "Note", "type": "text"}, &title)
	opts := []m{{"id": "todo", "label": "Todo", "color": ""}, {"id": "done", "label": "Done", "color": ""}}
	e.do(t, alice, "PUT", "/api/properties/"+status.ID, m{"options": opts}, nil)
	if code := e.do(t, alice, "PUT", "/api/pages/"+db.ID+"/board-columns", m{"property_id": status.ID, "keys": []string{"done", "", "todo", "done"}}, nil); code != 204 {
		t.Fatalf("set board columns = %d", code)
	}
	e.do(t, alice, "PUT", "/api/pages/"+db.ID+"/board-columns", m{"property_id": owner.ID, "keys": []string{"u2", "u1"}}, nil)
	var cols struct {
		BoardColumns map[string][]string `json:"board_columns"`
		Properties   []struct {
			ID      string                `json:"id"`
			Options []struct{ ID string } `json:"options"`
		} `json:"properties"`
	}
	e.do(t, bob, "GET", "/api/pages/"+db.ID+"/rows", nil, &cols)
	if !reflect.DeepEqual(cols.BoardColumns[status.ID], []string{"done", "", "todo"}) || !reflect.DeepEqual(cols.BoardColumns[owner.ID], []string{"u2", "u1"}) {
		t.Fatalf("board columns = %v", cols.BoardColumns)
	}
	for _, p := range cols.Properties {
		if p.ID == status.ID && (len(p.Options) != 2 || p.Options[0].ID != "todo") {
			t.Fatalf("option order changed: %+v", p.Options)
		}
	}
	if code := e.do(t, alice, "PUT", "/api/pages/"+db.ID+"/board-columns", m{"property_id": title.ID, "keys": []string{"x"}}, nil); code != 400 {
		t.Fatalf("board columns for a text column = %d, want 400", code)
	}
	if code := e.do(t, bob, "PUT", "/api/pages/"+db.ID+"/board-columns", m{"property_id": status.ID, "keys": []string{"todo"}}, nil); code != 404 {
		t.Fatalf("viewer sets board columns = %d, want 404", code)
	}

	if !affectsData("/api/pages/x/board-order") || affectsTree("/api/pages/x/board-order") {
		t.Fatal("board order must signal data, not the sidebar tree")
	}
}
