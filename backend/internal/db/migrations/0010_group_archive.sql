-- Groups can be archived (soft-deleted), same as spaces already could
-- -- everything inside stays in the database, just stops appearing in
-- ListForUser/being grantable access through.
ALTER TABLE groups ADD COLUMN archived_at TEXT;
