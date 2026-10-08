-- 0005_page_covers.sql
-- Minimal file storage for page cover images -- see
-- docs/adr/0004-database-pages.md's "deliberately last" scoping note
-- (cover images needed *some* file storage to exist first, and Nook
-- had none). Deliberately narrow: one cover image per page, stored on
-- local disk (internal/assets), this table holding only the metadata.
-- This is not the general "Attachments" feature from the ROADMAP's
-- Phase 5 (which needs multiple files per page, not just a cover
-- slot) -- that can extend this same on-disk layout and package later
-- rather than needing a separate one built from scratch.

CREATE TABLE page_covers (
    page_id     TEXT PRIMARY KEY REFERENCES pages(id) ON DELETE CASCADE,
    id          TEXT NOT NULL UNIQUE,
    filename    TEXT NOT NULL,
    mime_type   TEXT NOT NULL,
    size_bytes  INTEGER NOT NULL,
    uploaded_by TEXT NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    created_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);
