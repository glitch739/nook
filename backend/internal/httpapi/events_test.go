package httpapi

import (
	"bufio"
	"context"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestEventsStreamSignalsTreeChanges(t *testing.T) {
	e := newTestEnv(t, RegOpen)
	alice := e.client()
	e.do(t, alice, "POST", "/api/auth/register", m{"email": "alice@example.hu", "username": "alice", "password": "hosszujelszo"}, nil)
	var g, sp idResp
	e.do(t, alice, "POST", "/api/groups", m{"name": "G"}, &g)
	e.do(t, alice, "POST", "/api/spaces", m{"name": "S", "type": "private", "group_id": g.ID}, &sp)

	// Not signed in: no stream.
	if res, _ := http.Get(e.srv.URL + "/api/events"); res.StatusCode != 401 {
		t.Fatalf("anonymous events = %d", res.StatusCode)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", e.srv.URL+"/api/events", nil)
	res, err := alice.Do(req)
	if err != nil || res.StatusCode != 200 || !strings.HasPrefix(res.Header.Get("Content-Type"), "text/event-stream") {
		t.Fatalf("events = %v %v", res, err)
	}
	defer res.Body.Close()
	got := make(chan string, 10)
	go func() {
		sc := bufio.NewScanner(res.Body)
		for sc.Scan() {
			if strings.HasPrefix(sc.Text(), "event: ") {
				got <- sc.Text()
			}
		}
	}()

	// A failed write signals nothing; a successful page create does.
	e.do(t, alice, "POST", "/api/spaces/nope/pages", m{"title": "x", "kind": "document"}, nil)
	select {
	case ev := <-got:
		t.Fatalf("failed request produced %q", ev)
	case <-time.After(300 * time.Millisecond):
	}
	e.do(t, alice, "POST", "/api/spaces/"+sp.ID+"/pages", m{"title": "New", "kind": "document"}, nil)
	select {
	case ev := <-got:
		if ev != "event: tree" {
			t.Fatalf("event = %q", ev)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no tree event after creating a page")
	}
}

// Database writes (cells, columns, options) signal "data", not "tree",
// so open database views and row pages re-fetch without the sidebar
// reloading on every cell edit.
func TestEventsStreamSignalsDataChanges(t *testing.T) {
	e := newTestEnv(t, RegOpen)
	alice := e.client()
	e.do(t, alice, "POST", "/api/auth/register", m{"email": "alice@example.hu", "username": "alice", "password": "hosszujelszo"}, nil)
	var g, sp, dbp, row, prop idResp
	e.do(t, alice, "POST", "/api/groups", m{"name": "G"}, &g)
	e.do(t, alice, "POST", "/api/spaces", m{"name": "S", "type": "private", "group_id": g.ID}, &sp)
	e.do(t, alice, "POST", "/api/spaces/"+sp.ID+"/pages", m{"title": "Tasks", "kind": "database"}, &dbp)
	e.do(t, alice, "POST", "/api/spaces/"+sp.ID+"/pages", m{"title": "Row", "parent_page_id": dbp.ID, "kind": "document"}, &row)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", e.srv.URL+"/api/events", nil)
	res, err := alice.Do(req)
	if err != nil || res.StatusCode != 200 {
		t.Fatalf("events = %v %v", res, err)
	}
	defer res.Body.Close()
	got := make(chan string, 10)
	go func() {
		sc := bufio.NewScanner(res.Body)
		for sc.Scan() {
			if strings.HasPrefix(sc.Text(), "event: ") {
				got <- sc.Text()
			}
		}
	}()
	expect := func(what, want string) {
		t.Helper()
		select {
		case ev := <-got:
			if ev != want {
				t.Fatalf("%s: event = %q, want %q", what, ev, want)
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("%s: no %q event", what, want)
		}
	}

	e.do(t, alice, "POST", "/api/pages/"+dbp.ID+"/properties", m{"name": "Status", "type": "select"}, &prop)
	expect("new column", "event: data")
	e.do(t, alice, "PUT", "/api/properties/"+prop.ID, m{"options": []m{{"id": "o1", "label": "Done", "color": "#4ade80"}}}, nil)
	expect("options", "event: data")
	e.do(t, alice, "PUT", "/api/pages/"+row.ID+"/properties/"+prop.ID, m{"value": m{"option_id": "o1"}}, nil)
	expect("cell", "event: data")

	// A rejected cell write signals nothing.
	e.do(t, alice, "PUT", "/api/pages/"+row.ID+"/properties/nope", m{"value": "x"}, nil)
	select {
	case ev := <-got:
		t.Fatalf("failed cell write produced %q", ev)
	case <-time.After(300 * time.Millisecond):
	}
}
