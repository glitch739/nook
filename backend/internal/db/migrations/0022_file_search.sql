-- 0022_file_search.sql
-- Full-text search inside attached files (ADR 0009).
--
-- A background worker (internal/extract) sends each page_attachments entry to
-- Apache Tika, which returns its text -- OCR included for scanned PDFs
-- and images -- and stores it here, split per PDF page so a hit can
-- point at "page 12". file_search_index is a second FTS5 table next to
-- search_index (0014), same tokenizer; internal/search queries both and
-- applies the same access filter to each.
--
-- Every file always has at least one file_text row, even before (or
-- without) extraction: an empty-bodied one, so the file is findable by
-- its name from the moment it is uploaded.

-- One row per attachment: where it is in the extraction queue.
CREATE TABLE file_extractions (
    file_id    TEXT PRIMARY KEY REFERENCES page_attachments(id) ON DELETE CASCADE,
    status     TEXT NOT NULL DEFAULT 'pending'
               CHECK (status IN ('pending', 'processing', 'done', 'failed', 'skipped')),
    -- 1 when any of the text came from OCR (scanned page, photo).
    used_ocr   INTEGER NOT NULL DEFAULT 0,
    -- Why the last attempt failed or was skipped; shown to users.
    error      TEXT NOT NULL DEFAULT '',
    attempts   INTEGER NOT NULL DEFAULT 0,
    -- A failed attempt that will be retried waits until this time.
    not_before TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE INDEX file_extractions_queue ON file_extractions (status, not_before);

-- The extracted text, in chunks: one per PDF page, one for a whole
-- document without pages (Word, e-mail, ...). part is the path of an
-- embedded document ("/report.docx" inside a zip), '' for the file itself.
CREATE TABLE file_text (
    chunk_id INTEGER PRIMARY KEY,
    file_id  TEXT NOT NULL REFERENCES page_attachments(id) ON DELETE CASCADE,
    page_no  INTEGER,
    part     TEXT NOT NULL DEFAULT '',
    -- 1 when this chunk's text was read off an image (scan, photo).
    ocr      INTEGER NOT NULL DEFAULT 0,
    body     TEXT NOT NULL
);

CREATE INDEX file_text_file_id ON file_text (file_id);

-- rowid = file_text.chunk_id. The file name is repeated on every chunk
-- so "name word + content word" still matches within one chunk.
CREATE VIRTUAL TABLE file_search_index USING fts5(
    name,
    body,
    tokenize = 'unicode61 remove_diacritics 2'
);

CREATE TRIGGER file_text_search_ai AFTER INSERT ON file_text BEGIN
    INSERT INTO file_search_index (rowid, name, body)
        SELECT NEW.chunk_id, f.filename, NEW.body FROM page_attachments AS f WHERE f.id = NEW.file_id;
END;

-- Also fires for the ON DELETE CASCADE from page_attachments.
CREATE TRIGGER file_text_search_ad AFTER DELETE ON file_text BEGIN
    DELETE FROM file_search_index WHERE rowid = OLD.chunk_id;
END;

-- A new attachment: queue it, and make its name searchable right away.
CREATE TRIGGER page_attachments_extract_ai AFTER INSERT ON page_attachments BEGIN
    INSERT INTO file_extractions (file_id) VALUES (NEW.id);
    INSERT INTO file_text (file_id, body) VALUES (NEW.id, '');
END;

-- Backfill: every file uploaded before this migration.
INSERT INTO file_extractions (file_id) SELECT id FROM page_attachments;
INSERT INTO file_text (file_id, body) SELECT id, '' FROM page_attachments;
