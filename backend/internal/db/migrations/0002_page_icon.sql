-- 0002_page_icon.sql
-- Adds an icon to pages, so the sidebar has something real to show and
-- edit instead of a client-only cosmetic that never persisted. Noted as
-- a cheap follow-on back in docs/ROADMAP.md when the sidebar work
-- started; adding it now that the frontend is being wired to real data.
--
-- Spaces deliberately do NOT get an icon column here: nothing in the UI
-- lets a user change a space's icon (only pages have that affordance),
-- so the frontend just derives a fixed icon from `spaces.type` instead
-- of storing one — no reason to add a column with no way to edit it.

ALTER TABLE pages ADD COLUMN icon TEXT NOT NULL DEFAULT '📄';
