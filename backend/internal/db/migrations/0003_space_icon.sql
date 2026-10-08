-- 0003_space_icon.sql
-- Adds an icon column to spaces (previously the frontend derived one
-- client-side from `type` -- see sidebarData.ts's spaceIcon(), whose
-- values this backfill matches exactly) plus an archived_at column so a
-- whole space can be soft-deleted the same way pages already are (see
-- migrations/0001_init.sql's pages.archived_at and
-- internal/pages.Store.Archive). Both were explicitly requested once a
-- user could have more than one space -- see migrations/0002_page_icon.sql's
-- comment for why spaces didn't get an icon column originally.

ALTER TABLE spaces ADD COLUMN icon TEXT NOT NULL DEFAULT '';
ALTER TABLE spaces ADD COLUMN archived_at TEXT;

UPDATE spaces SET icon = '🏠' WHERE type = 'private' AND icon = '';
UPDATE spaces SET icon = '👥' WHERE type = 'shared' AND icon = '';
