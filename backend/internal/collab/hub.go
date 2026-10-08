package collab

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/coder/websocket"
)

// Wire protocol (ADR 0006). Binary frames carry Yjs data, text frames
// carry JSON control messages.
//
//	binary C→S  [1][update]              document update
//	binary S→C  [1][seq uint64 BE][update]
//	binary both [2][awareness update]    cursors/presence, relayed, never stored
//
//	text S→C {"t":"hello","readonly":bool,"init":bool}
//	         {"t":"synced","seq":N}      initial load done
//	         {"t":"ready"}               page initialized; editing allowed
//	         {"t":"init"}                you must now initialize the page
//	         {"t":"ack","seq":N}         your update was stored as seq N
//	         {"t":"snap","upto":N}       a snapshot up to N was stored
//	text C→S {"t":"init_done"}
//	         {"t":"snap","upto":N,"state":base64,"content":"<content_json>"}
const (
	msgUpdate    = 1
	msgAwareness = 2
)

const (
	maxMessage   = 16 << 20
	sendQueue    = 512
	writeTimeout = 10 * time.Second
	pingEvery    = 25 * time.Second
	// activityEvery throttles "user edited page" activity writes per connection.
	activityEvery = time.Minute
)

// Hooks connect the hub to the rest of the app without importing it.
type Hooks struct {
	// OnEdit is called (throttled) when userID changes pageID.
	OnEdit func(ctx context.Context, userID, pageID string)
}

type Hub struct {
	store *Store
	hooks Hooks

	mu    sync.Mutex
	rooms map[string]*room
}

func NewHub(store *Store, hooks Hooks) *Hub {
	return &Hub{store: store, hooks: hooks, rooms: map[string]*room{}}
}

type room struct {
	id     string
	mu     sync.Mutex
	loaded bool
	maxSeq int64
	upto   int64 // last stored snapshot
	inited bool  // has Yjs data or an initializer finished
	initBy *conn // connection currently initializing, if any
	conns  map[*conn]struct{}
	refs   int // guarded by Hub.mu
}

type conn struct {
	userID   string
	readonly bool
	send     chan frame
	closed   chan struct{}
	once     sync.Once
	lastAct  time.Time
}

type frame struct {
	typ  websocket.MessageType
	data []byte
}

func (c *conn) enqueue(f frame) {
	select {
	case c.send <- f:
	case <-c.closed:
	default:
		// Slow consumer: drop the connection; the client reconnects and
		// resyncs from the log, and resends its unacknowledged updates.
		c.close()
	}
}

func (c *conn) close() { c.once.Do(func() { close(c.closed) }) }

func (c *conn) text(v any) {
	b, _ := json.Marshal(v)
	c.enqueue(frame{websocket.MessageText, b})
}

func updateFrame(seq int64, data []byte) frame {
	b := make([]byte, 9+len(data))
	b[0] = msgUpdate
	binary.BigEndian.PutUint64(b[1:9], uint64(seq))
	copy(b[9:], data)
	return frame{websocket.MessageBinary, b}
}

func (h *Hub) room(id string) *room {
	h.mu.Lock()
	defer h.mu.Unlock()
	r := h.rooms[id]
	if r == nil {
		r = &room{id: id, conns: map[*conn]struct{}{}}
		h.rooms[id] = r
	}
	r.refs++
	return r
}

func (h *Hub) leave(r *room, c *conn) {
	r.mu.Lock()
	delete(r.conns, c)
	if r.initBy == c {
		r.initBy = nil
		// Hand initialization to another writer, if any is waiting.
		for o := range r.conns {
			if !o.readonly {
				r.initBy = o
				o.text(map[string]any{"t": "init"})
				break
			}
		}
	}
	r.mu.Unlock()

	h.mu.Lock()
	r.refs--
	if r.refs == 0 {
		delete(h.rooms, r.id)
	}
	h.mu.Unlock()
}

// Serve runs one client connection for pageID until it closes. The
// caller has already authenticated the user and checked access.
func (h *Hub) Serve(w http.ResponseWriter, r *http.Request, pageID, userID string, readonly bool) {
	ws, err := websocket.Accept(w, r, nil) // default: same-origin only
	if err != nil {
		return
	}
	ws.SetReadLimit(maxMessage)
	c := &conn{userID: userID, readonly: readonly, send: make(chan frame, sendQueue), closed: make(chan struct{})}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	rm := h.room(pageID)
	defer h.leave(rm, c)
	if err := h.join(ctx, rm, c); err != nil {
		slog.Error("collab join failed", "err", err, "page_id", pageID)
		ws.Close(websocket.StatusInternalError, "load failed")
		return
	}

	go func() { // writer
		t := time.NewTicker(pingEvery)
		defer t.Stop()
		for {
			select {
			case f := <-c.send:
				wctx, wc := context.WithTimeout(ctx, writeTimeout)
				err := ws.Write(wctx, f.typ, f.data)
				wc()
				if err != nil {
					c.close()
					return
				}
			case <-t.C:
				pctx, pc := context.WithTimeout(ctx, writeTimeout)
				err := ws.Ping(pctx)
				pc()
				if err != nil {
					c.close()
					return
				}
			case <-c.closed:
				return
			}
		}
	}()

	go func() {
		<-c.closed
		ws.Close(websocket.StatusNormalClosure, "")
		cancel()
	}()

	for {
		typ, data, err := ws.Read(ctx)
		if err != nil {
			c.close()
			return
		}
		if err := h.handle(ctx, rm, c, typ, data); err != nil {
			slog.Warn("collab message rejected", "err", err, "page_id", pageID, "user_id", userID)
			c.close()
			return
		}
	}
}

func (h *Hub) join(ctx context.Context, rm *room, c *conn) error {
	rm.mu.Lock()
	defer rm.mu.Unlock()
	state, upto, updates, err := h.store.Load(ctx, rm.id)
	if err != nil {
		return err
	}
	if !rm.loaded {
		rm.loaded = true
		rm.upto = upto
		rm.maxSeq = upto
		if n := len(updates); n > 0 {
			rm.maxSeq = updates[n-1].Seq
		}
		rm.inited = state != nil || len(updates) > 0
	}
	init := false
	if !rm.inited && rm.initBy == nil && !c.readonly {
		rm.initBy = c
		init = true
	}
	rm.conns[c] = struct{}{}
	c.text(map[string]any{"t": "hello", "readonly": c.readonly, "init": init})
	if state != nil {
		c.enqueue(updateFrame(upto, state))
	}
	for _, u := range updates {
		c.enqueue(updateFrame(u.Seq, u.Data))
	}
	c.text(map[string]any{"t": "synced", "seq": rm.maxSeq})
	if rm.inited {
		c.text(map[string]any{"t": "ready"})
	}
	return nil
}

var errBadMessage = errors.New("collab: malformed message")

func (h *Hub) handle(ctx context.Context, rm *room, c *conn, typ websocket.MessageType, data []byte) error {
	if typ == websocket.MessageBinary {
		if len(data) < 1 {
			return errBadMessage
		}
		switch data[0] {
		case msgAwareness:
			rm.mu.Lock()
			for o := range rm.conns {
				if o != c {
					o.enqueue(frame{websocket.MessageBinary, data})
				}
			}
			rm.mu.Unlock()
			return nil
		case msgUpdate:
			if c.readonly {
				return nil // viewers can't write; ignore quietly
			}
			return h.applyUpdate(ctx, rm, c, data[1:])
		}
		return errBadMessage
	}

	var m struct {
		T       string `json:"t"`
		Upto    int64  `json:"upto"`
		State   string `json:"state"`
		Content string `json:"content"`
	}
	if err := json.Unmarshal(data, &m); err != nil {
		return errBadMessage
	}
	if c.readonly {
		return nil
	}
	switch m.T {
	case "init_done":
		rm.mu.Lock()
		if rm.initBy == c {
			rm.initBy = nil
			rm.inited = true
			for o := range rm.conns {
				o.text(map[string]any{"t": "ready"})
			}
		}
		rm.mu.Unlock()
	case "snap":
		state, err := base64.StdEncoding.DecodeString(m.State)
		if err != nil || len(state) == 0 || (m.Content != "" && !json.Valid([]byte(m.Content))) {
			return errBadMessage
		}
		rm.mu.Lock()
		defer rm.mu.Unlock()
		if !rm.inited || m.Upto <= rm.upto || m.Upto > rm.maxSeq {
			return nil // stale or premature; another snapshot will come
		}
		if err := h.store.Compact(ctx, rm.id, m.Upto, state, m.Content); err != nil {
			if errors.Is(err, ErrStaleSnapshot) {
				return nil
			}
			return err
		}
		rm.upto = m.Upto
		for o := range rm.conns {
			o.text(map[string]any{"t": "snap", "upto": m.Upto})
		}
	}
	return nil
}

func (h *Hub) applyUpdate(ctx context.Context, rm *room, c *conn, update []byte) error {
	if len(update) == 0 {
		return errBadMessage
	}
	rm.mu.Lock()
	seq := rm.maxSeq + 1
	if err := h.store.Append(ctx, rm.id, seq, update, c.userID); err != nil {
		rm.mu.Unlock()
		return err
	}
	rm.maxSeq = seq
	if !rm.inited && rm.initBy != c {
		// Someone wrote before initialization finished (shouldn't happen:
		// clients stay read-only until "ready"). Accept it; the page is
		// now under Yjs control either way.
		rm.inited = true
	}
	f := updateFrame(seq, update)
	for o := range rm.conns {
		if o == c {
			o.text(map[string]any{"t": "ack", "seq": seq})
		} else {
			o.enqueue(f)
		}
	}
	rm.mu.Unlock()

	if h.hooks.OnEdit != nil && time.Since(c.lastAct) > activityEvery {
		c.lastAct = time.Now()
		h.hooks.OnEdit(ctx, c.userID, rm.id)
	}
	return nil
}
