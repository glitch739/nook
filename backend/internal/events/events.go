// Package events is a tiny in-process broadcast of "something changed"
// signals, pushed to every open browser over Server-Sent Events so they
// re-fetch their own (permission-filtered) data. Single process (ADR
// 0001), so an in-memory hub is all it takes.
//
// Two kinds exist: Tree (pages, spaces, groups: the sidebar) and Data
// (database cell values, columns, select options: open database views
// and row pages). A signal says only *that* something of that kind
// changed, never what, so nobody learns about spaces they can't see.
package events

import "sync"

// Kind is a bit set of change kinds.
type Kind uint8

const (
	Tree Kind = 1 << iota
	Data
)

// Subscription is one listener. C fires when at least one signal is
// pending; Take returns and clears the pending kinds.
type Subscription struct {
	C       <-chan struct{}
	ch      chan struct{}
	mu      sync.Mutex
	pending Kind
}

// Take returns the kinds signalled since the last Take and clears them.
func (s *Subscription) Take() Kind {
	s.mu.Lock()
	defer s.mu.Unlock()
	k := s.pending
	s.pending = 0
	return k
}

type Hub struct {
	mu   sync.Mutex
	subs map[*Subscription]struct{}
}

func NewHub() *Hub { return &Hub{subs: map[*Subscription]struct{}{}} }

// Subscribe returns a subscription and a func to stop it. Signals
// coalesce: a slow reader gets one wake-up with the union of pending
// kinds, never a backlog.
func (h *Hub) Subscribe() (*Subscription, func()) {
	ch := make(chan struct{}, 1)
	s := &Subscription{C: ch, ch: ch}
	h.mu.Lock()
	h.subs[s] = struct{}{}
	h.mu.Unlock()
	return s, func() {
		h.mu.Lock()
		delete(h.subs, s)
		h.mu.Unlock()
	}
}

// Signal marks kind as changed for every subscriber (non-blocking).
func (h *Hub) Signal(kind Kind) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for s := range h.subs {
		s.mu.Lock()
		s.pending |= kind
		s.mu.Unlock()
		select {
		case s.ch <- struct{}{}:
		default:
		}
	}
}

// TreeChanged signals a sidebar-tree change.
func (h *Hub) TreeChanged() { h.Signal(Tree) }

// DataChanged signals a database content change.
func (h *Hub) DataChanged() { h.Signal(Data) }

// Subscribers is the current count (for tests and logs).
func (h *Hub) Subscribers() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.subs)
}
