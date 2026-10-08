-- Per-user ordering for the sidebar's team space list. Kept on
-- space_members (not spaces itself) because order is naturally a
-- per-viewer preference, not a global fact about the space -- once
-- space visibility becomes per-user (Phase 2), a global column
-- couldn't represent "user A's order" and "user B's order" separately
-- for spaces they both belong to.

ALTER TABLE space_members ADD COLUMN position TEXT NOT NULL DEFAULT '';

-- Backfill: one sequential, zero-padded position per user, in the same
-- alphabetical-by-name order the app already showed before this column
-- existed (spaces.Store.ListForUser's old "ORDER BY s.name"), so
-- migrating doesn't visibly reshuffle anyone's existing sidebar.
UPDATE space_members
SET position = printf('%06d', 1 + (
    SELECT COUNT(*)
    FROM space_members AS other
    JOIN spaces AS other_space ON other_space.id = other.space_id
    JOIN spaces AS this_space ON this_space.id = space_members.space_id
    WHERE other.user_id = space_members.user_id
      AND other_space.name < this_space.name
));
