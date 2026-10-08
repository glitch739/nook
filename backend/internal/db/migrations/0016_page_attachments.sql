-- 0016_page_attachments.sql
-- Files attached inside a page's text (PDF, Word, Excel, ...). Same
-- shape as page_images: the bytes live in the uploads directory under
-- the row id, the document references them by URL, and they are always
-- served as a download (Content-Disposition: attachment).
CREATE TABLE page_attachments (
    id          TEXT PRIMARY KEY,
    page_id     TEXT NOT NULL REFERENCES pages(id) ON DELETE CASCADE,
    filename    TEXT NOT NULL,
    mime_type   TEXT NOT NULL,
    size_bytes  INTEGER NOT NULL,
    uploaded_by TEXT NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    created_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE INDEX page_attachments_page_id ON page_attachments (page_id);
