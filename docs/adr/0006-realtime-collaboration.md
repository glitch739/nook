# ADR 0006: Realtime collaborative editing (Yjs over a Go relay)

Status: Accepted — 2026-09-28

## Context

Several people now edit the same pages (ADR 0005). Until now every
save sent the whole `content_json`, last write wins: two people on one
page silently overwrote each other. The requirement: Google-Docs-style
simultaneous editing, **no data loss** — including flaky connections
and closing a tab mid-edit. ADR 0002 already anticipated Yjs.

## Decision

**Yjs CRDT in the browser, a dumb-but-durable relay in Go.** The server
never parses Yjs; it orders, stores, relays and compacts opaque binary
updates. Yjs updates are idempotent and commutative, which is what
makes this safe: replaying, duplicating or reordering them always
converges to the same document.

### Storage (`0013_collab.sql`)

- `page_yupdates(page_id, seq, data, user_id)` — append-only log.
- `page_ydoc(page_id, state, upto)` — compacted snapshot covering every
  update `<= upto`.
- Load = snapshot + updates with `seq > upto`.
- `pages.content_json` stays, as a *projection* written with each
  snapshot (for anything that isn't the live editor: search, export,
  REST reads). Once a page has Yjs data, `PUT content_json` is refused
  with 409 so a stale client can't clobber the live doc.

### Protocol (`internal/collab`, `GET /api/pages/{id}/collab`, WebSocket)

Binary frames: `[1][update]` (server adds a `uint64` seq when relaying),
`[2][awareness]` (cursors/presence, relayed, never stored). JSON text
frames: `hello`, `synced`, `ready`, `init`, `ack`, `snap`, `init_done`.
Per page a room serializes all writes under one lock, so every client
sees seqs in order.

### Guarantees against data loss

1. **Ack before forget.** Each client keeps every local change in a
   pending list until the server acks it (acked = committed to SQLite).
   After any reconnect, all pending changes are resent (merged).
2. **Unmount ≠ discard.** Closing a page's editor only releases the
   provider; it stays alive (and keeps reconnecting when offline) until
   everything is acked, then sends a final snapshot and closes.
3. **Leaving the tab** with unacked changes shows the browser's "leave
   page?" prompt.
4. **Single seeding.** A page's first-ever open converts its existing
   `content_json` into Yjs. The server picks exactly one writer to do
   it (handing off if that client disconnects); everyone else stays
   read-only until `ready`. Two clients can never both insert the seed.
5. **Safe compaction.** A client sends `snap {upto, state, content}`
   only for updates it has seen; the server accepts it only if
   `upto` is newer than the stored snapshot and `<= maxSeq`, and
   replaces + prunes in one transaction. Changes not covered by a
   snapshot are still in the log or in some client's pending list.
6. **Slow consumer** → connection dropped → client reconnects and
   resyncs; nothing is skipped.

### Other

- Viewers connect read-only (server drops their writes).
- Undo is Yjs-aware (only your own changes).
- Presence: colored carets with names, avatars + save status in the
  editor toolbar ("Mentve" / "Mentés…" / "Offline – a módosítások
  mentésre várnak").
- Activity (ADR 0005) records `page.edit` from the relay, throttled per
  connection.
- The WebSocket route bypasses `http.TimeoutHandler`; same-origin is
  enforced by the WebSocket library; CSP `connect-src` lists ws(s).

## Consequences

- `scripts/collab-e2e.sh` runs the real browser provider (in Node)
  against a real server: single seeding, concurrent convergence,
  offline edits surviving reconnect, snapshot completeness, 409 on
  stale writes, identical fresh load.
- Not realtime yet: page titles/icons in the sidebar and database
  property cells (single-field writes, so no content loss, but others
  see them after a reload).
- No offline persistence across a *browser restart* (IndexedDB) — the
  leave-page warning covers that case instead.
