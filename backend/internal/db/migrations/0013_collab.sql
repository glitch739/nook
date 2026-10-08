-- 0013_collab.sql
-- Realtime collaborative editing (ADR 0006): each document page's
-- content is a Yjs document. The server stores it as opaque binary:
--   page_ydoc      a compacted snapshot covering every update up to `upto`
--   page_yupdates  the append-only log of updates after that snapshot
-- Loading a page = snapshot + updates with seq > upto. Yjs updates are
-- idempotent and commutative, so replaying overlaps is harmless.
-- pages.content_json stays as the plain-JSON projection (written from
-- client snapshots) for anything that isn't the live editor.

CREATE TABLE page_ydoc (
    page_id    TEXT PRIMARY KEY REFERENCES pages(id) ON DELETE CASCADE,
    state      BLOB NOT NULL,
    upto       INTEGER NOT NULL,
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE TABLE page_yupdates (
    page_id    TEXT NOT NULL REFERENCES pages(id) ON DELETE CASCADE,
    seq        INTEGER NOT NULL,
    data       BLOB NOT NULL,
    user_id    TEXT REFERENCES users(id) ON DELETE SET NULL,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    PRIMARY KEY (page_id, seq)
);
