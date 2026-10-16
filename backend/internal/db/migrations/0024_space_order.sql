-- Sidebar position for spaces a user reaches only through a group
-- (no space_members row of their own). Kept apart from space_members:
-- a row there would grant direct access that outlives the group
-- membership, and ordering must never change who can see what.
CREATE TABLE space_order (
    space_id TEXT NOT NULL REFERENCES spaces(id) ON DELETE CASCADE,
    user_id  TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    position TEXT NOT NULL,
    PRIMARY KEY (space_id, user_id)
);
