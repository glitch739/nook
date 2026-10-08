-- 0015_page_images.sql
-- Images inside a page's text (pasted, dropped, or picked via "/image").
-- Same on-disk layout as page covers (0005): the file lives in the
-- uploads directory under its own id, this table only holds metadata.
-- Unlike a cover, a page can have any number of these. The Tiptap
-- document references one by URL (/api/pages/{page_id}/images/{id}),
-- never by embedding the bytes, so content_json and the Yjs history
-- stay small no matter how many images a page has.
--
-- An image deleted from the text keeps its row and file for now:
-- undo, and other collaborators' offline edits, can still bring the
-- node back. Sweeping images no document references any more is a
-- separate cleanup job (see ROADMAP).

CREATE TABLE page_images (
    id          TEXT PRIMARY KEY,
    page_id     TEXT NOT NULL REFERENCES pages(id) ON DELETE CASCADE,
    filename    TEXT NOT NULL,
    mime_type   TEXT NOT NULL,
    size_bytes  INTEGER NOT NULL,
    uploaded_by TEXT NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    created_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE INDEX page_images_page_id ON page_images (page_id);
