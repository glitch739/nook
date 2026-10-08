-- 0001_init.sql
-- Core schema: users, sessions, spaces, space membership, pages.
--
-- Notes:
--   * Timestamps are stored as ISO-8601 UTC strings (TEXT), consistent and
--     sortable, no timezone ambiguity.
--   * IDs are UUIDv4 strings (TEXT), generated in Go before insert.
--   * "position" is a plain TEXT sort key (lexicographic). Phase 1 assigns
--     positions in simple increasing order; a fractional-indexing scheme
--     for cheap drag & drop reordering is introduced in Phase 4 without
--     requiring a column/type change.
--   * There is deliberately no separate "blocks" table: a page's content is
--     stored as a single Tiptap/ProseMirror document (content_json). See
--     docs/adr/0002-page-content-as-prosemirror-json.md for the rationale.

CREATE TABLE users (
    id            TEXT PRIMARY KEY,
    username      TEXT NOT NULL UNIQUE,
    password_hash TEXT NOT NULL,
    display_name  TEXT NOT NULL,
    created_at    TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE TABLE sessions (
    id         TEXT PRIMARY KEY,
    user_id    TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    expires_at TEXT NOT NULL,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE INDEX idx_sessions_user_id    ON sessions(user_id);
CREATE INDEX idx_sessions_expires_at ON sessions(expires_at);

CREATE TABLE spaces (
    id         TEXT PRIMARY KEY,
    name       TEXT NOT NULL,
    type       TEXT NOT NULL CHECK (type IN ('private', 'shared')),
    owner_id   TEXT NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE INDEX idx_spaces_owner_id ON spaces(owner_id);

CREATE TABLE space_members (
    space_id  TEXT NOT NULL REFERENCES spaces(id) ON DELETE CASCADE,
    user_id   TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    role      TEXT NOT NULL CHECK (role IN ('owner', 'admin', 'editor', 'viewer')),
    added_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    PRIMARY KEY (space_id, user_id)
);

CREATE INDEX idx_space_members_user_id ON space_members(user_id);

CREATE TABLE pages (
    id             TEXT PRIMARY KEY,
    space_id       TEXT NOT NULL REFERENCES spaces(id) ON DELETE CASCADE,
    parent_page_id TEXT REFERENCES pages(id) ON DELETE CASCADE,
    title          TEXT NOT NULL DEFAULT '',
    content_json   TEXT NOT NULL DEFAULT '{"type":"doc","content":[]}',
    position       TEXT NOT NULL,
    created_by     TEXT NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    archived_at    TEXT,
    created_at     TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_at     TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE INDEX idx_pages_space_id       ON pages(space_id);
CREATE INDEX idx_pages_parent_page_id ON pages(parent_page_id);
