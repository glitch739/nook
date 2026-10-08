-- Groups: the new top tier above spaces (ADR-less decision, discussed
-- 2026-09-22/24 -- see DEVLOG). A group is the real invite/tenant
-- boundary for multi-user: inviting someone to a group gives them
-- access to every space inside it, mirrored exactly on spaces'
-- existing membership machinery (group_members looks just like
-- space_members) rather than replacing it -- a space can still carry
-- its own space_members row too, for a future finer-grained override
-- (e.g. a guest who should see only one space, not the whole group).
-- internal/permissions.Checker.CanAccessSpace checks both and takes
-- whichever grants the stronger role.

CREATE TABLE groups (
    id         TEXT PRIMARY KEY,
    name       TEXT NOT NULL,
    icon       TEXT NOT NULL DEFAULT '👥',
    owner_id   TEXT NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

-- position lives here, not on groups itself, for the identical reason
-- space_members.position does (see 0008_space_member_position.sql):
-- ordering is a per-viewer preference, and two different members of the
-- same group need to be able to order it differently among their own
-- other groups.
CREATE TABLE group_members (
    group_id TEXT NOT NULL REFERENCES groups(id) ON DELETE CASCADE,
    user_id  TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    role     TEXT NOT NULL CHECK (role IN ('owner', 'admin', 'editor', 'viewer')),
    position TEXT NOT NULL DEFAULT '',
    added_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    PRIMARY KEY (group_id, user_id)
);

CREATE INDEX idx_group_members_user_id ON group_members(user_id);

-- Nullable at the schema level (SQLite can't add a NOT NULL column
-- without a per-row default, and the default here has to be a
-- freshly-created group id, different per existing space owner -- see
-- the backfill below). internal/spaces.Store.Create requires a groupID
-- argument from here on, so in practice every space this application
-- ever creates has one; this column is never left NULL by anything
-- except pre-migration rows, which the backfill immediately fixes.
ALTER TABLE spaces ADD COLUMN group_id TEXT REFERENCES groups(id);
CREATE INDEX idx_spaces_group_id ON spaces(group_id);

-- Backfill: one default group per existing space owner, containing
-- every space they already own, so nothing becomes orphaned or
-- invisible. lower(hex(randomblob(...))...) is SQLite's standard
-- no-extension way to generate a UUIDv4-shaped id (there's no native
-- UUID() function) -- fine for a one-time migration backfill; every
-- id created by the application itself afterward still goes through
-- Go's db.NewID().
INSERT INTO groups (id, name, icon, owner_id)
SELECT
    lower(
        hex(randomblob(4)) || '-' || hex(randomblob(2)) || '-4' ||
        substr(hex(randomblob(2)), 2) || '-' ||
        substr('89ab', abs(random()) % 4 + 1, 1) || substr(hex(randomblob(2)), 2) || '-' ||
        hex(randomblob(6))
    ),
    'Saját csoport',
    '👥',
    owner_id
FROM (SELECT DISTINCT owner_id FROM spaces);

INSERT INTO group_members (group_id, user_id, role, position)
SELECT id, owner_id, 'owner', '000001' FROM groups;

UPDATE spaces
SET group_id = (SELECT id FROM groups WHERE groups.owner_id = spaces.owner_id)
WHERE group_id IS NULL;
