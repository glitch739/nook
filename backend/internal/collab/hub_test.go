package collab

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"nook/internal/dbtest"
	"nook/internal/groups"
	"nook/internal/pages"
	"nook/internal/spaces"
	"nook/internal/users"
)

type client struct {
	t  *testing.T
	ws *websocket.Conn
}

func dial(t *testing.T, url string) *client {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ws, _, err := websocket.Dial(ctx, strings.Replace(url, "http", "ws", 1), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ws.CloseNow() })
	return &client{t, ws}
}

// next returns the next message: a control map, or an update (seq, data).
func (c *client) next() (map[string]any, int64, []byte) {
	c.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	typ, data, err := c.ws.Read(ctx)
	if err != nil {
		c.t.Fatalf("read: %v", err)
	}
	if typ == websocket.MessageText {
		var m map[string]any
		json.Unmarshal(data, &m)
		return m, 0, nil
	}
	if data[0] == msgUpdate {
		return nil, int64(binary.BigEndian.Uint64(data[1:9])), data[9:]
	}
	return map[string]any{"t": "awareness"}, 0, data[1:]
}

func (c *client) expect(t string) map[string]any {
	c.t.Helper()
	m, _, _ := c.next()
	if m == nil || m["t"] != t {
		c.t.Fatalf("want %q, got %v", t, m)
	}
	return m
}

func (c *client) expectUpdate(seq int64, data string) {
	c.t.Helper()
	m, s, d := c.next()
	if m != nil || s != seq || string(d) != data {
		c.t.Fatalf("want update %d %q, got %v %d %q", seq, data, m, s, d)
	}
}

func (c *client) sendUpdate(data string) {
	c.ws.Write(context.Background(), websocket.MessageBinary, append([]byte{msgUpdate}, data...))
}

func (c *client) sendText(v any) {
	b, _ := json.Marshal(v)
	c.ws.Write(context.Background(), websocket.MessageText, b)
}

func TestHubProtocol(t *testing.T) {
	ctx := context.Background()
	conn := dbtest.New(t)
	u, _ := users.NewStore(conn).Create(ctx, "anna", "h", "Anna")
	g, _ := groups.NewStore(conn).Create(ctx, "G", u.ID)
	sp, _ := spaces.NewStore(conn).Create(ctx, "S", spaces.TypePrivate, u.ID, g.ID)
	ps := pages.NewStore(conn)
	p, _ := ps.Create(ctx, sp.ID, nil, "Doc", u.ID, pages.KindDocument)

	store := NewStore(conn)
	edits := make(chan string, 10)
	hub := NewHub(store, Hooks{OnEdit: func(_ context.Context, userID, pageID string) { edits <- pageID }})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hub.Serve(w, r, p.ID, u.ID, r.URL.Query().Get("ro") == "1")
	}))
	defer srv.Close()

	a := dial(t, srv.URL)
	if m := a.expect("hello"); m["init"] != true {
		t.Fatalf("first writer should initialize: %v", m)
	}
	a.expect("synced")
	b := dial(t, srv.URL)
	if m := b.expect("hello"); m["init"] != false {
		t.Fatalf("second writer must not initialize: %v", m)
	}
	b.expect("synced") // no "ready" yet

	a.sendUpdate("u1")
	a.sendText(map[string]any{"t": "init_done"})
	a.expect("ack")
	b.expectUpdate(1, "u1")
	a.expect("ready")
	b.expect("ready")
	<-edits

	b.sendUpdate("u2")
	b.expect("ack")
	a.expectUpdate(2, "u2")

	// Viewer: sees everything, its writes are ignored.
	v := dial(t, srv.URL+"?ro=1")
	if m := v.expect("hello"); m["readonly"] != true {
		t.Fatalf("viewer hello: %v", m)
	}
	v.expectUpdate(1, "u1")
	v.expectUpdate(2, "u2")
	v.expect("synced")
	v.expect("ready")
	v.sendUpdate("evil")

	// Stale/premature snapshots are ignored; a valid one compacts.
	a.sendText(map[string]any{"t": "snap", "upto": 5, "state": base64.StdEncoding.EncodeToString([]byte("S")), "content": `{"type":"doc"}`})
	a.sendText(map[string]any{"t": "snap", "upto": 2, "state": base64.StdEncoding.EncodeToString([]byte("S12")), "content": `{"type":"doc","content":[]}`})
	a.expect("snap")
	b.expect("snap")

	// A fresh client gets snapshot (as seq 2) and nothing else.
	c := dial(t, srv.URL)
	c.expect("hello")
	c.expectUpdate(2, "S12")
	if m := c.expect("synced"); m["seq"] != float64(2) {
		t.Fatalf("synced seq: %v", m)
	}
	c.expect("ready")

	got, _ := ps.GetByID(ctx, p.ID)
	if got.ContentJSON != `{"type":"doc","content":[]}` {
		t.Fatalf("content_json = %s", got.ContentJSON)
	}
	if live, _ := store.HasState(ctx, p.ID); !live {
		t.Fatal("HasState = false")
	}
	_, _, ups, _ := store.Load(ctx, p.ID)
	if len(ups) != 0 {
		t.Fatalf("updates after compaction = %d (viewer write must not be stored)", len(ups))
	}
}

func TestInitHandoff(t *testing.T) {
	ctx := context.Background()
	conn := dbtest.New(t)
	u, _ := users.NewStore(conn).Create(ctx, "anna", "h", "Anna")
	g, _ := groups.NewStore(conn).Create(ctx, "G", u.ID)
	sp, _ := spaces.NewStore(conn).Create(ctx, "S", spaces.TypePrivate, u.ID, g.ID)
	p, _ := pages.NewStore(conn).Create(ctx, sp.ID, nil, "Doc", u.ID, pages.KindDocument)
	hub := NewHub(NewStore(conn), Hooks{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hub.Serve(w, r, p.ID, u.ID, false)
	}))
	defer srv.Close()

	a := dial(t, srv.URL)
	a.expect("hello")
	a.expect("synced")
	b := dial(t, srv.URL)
	b.expect("hello")
	b.expect("synced")
	a.ws.Close(websocket.StatusNormalClosure, "")
	b.expect("init")
}
