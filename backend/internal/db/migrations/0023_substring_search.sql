-- 0023_substring_search.sql
-- Search inside words, not only from their beginning: "db01" finds
-- "srvdb01", "körüli" finds "Házkörüli" (ADR 0007's accepted gap, now
-- closed; ADR 0009).
--
-- Two more FTS5 tables with the trigram tokenizer, mirroring the word
-- indexes row for row: search_trigram next to search_index (pages, rowid
-- = search_docs.doc_id), file_search_trigram next to file_search_index
-- (attachments, rowid = file_text.chunk_id). remove_diacritics 1 keeps
-- matching accent- and case-insensitive. internal/search asks the word
-- index first and the trigram index for whatever that missed.
--
-- A virtual table can't carry triggers of its own, so every trigger
-- that writes search_index / file_search_index is recreated here with
-- the same write to the trigram twin.

CREATE VIRTUAL TABLE search_trigram USING fts5(
    title,
    body,
    tokenize = 'trigram remove_diacritics 1'
);

CREATE VIRTUAL TABLE file_search_trigram USING fts5(
    name,
    body,
    tokenize = 'trigram remove_diacritics 1'
);

DROP TRIGGER pages_search_ai;
DROP TRIGGER pages_search_au;
DROP TRIGGER pages_search_ad;
DROP TRIGGER cells_search_ai;
DROP TRIGGER cells_search_au;
DROP TRIGGER cells_search_ad;
DROP TRIGGER columns_search_au;
DROP TRIGGER file_text_search_ai;
DROP TRIGGER file_text_search_ad;

CREATE TRIGGER pages_search_ai AFTER INSERT ON pages BEGIN
    DELETE FROM search_index WHERE rowid IN (SELECT doc_id FROM search_docs WHERE page_id = NEW.id);
    DELETE FROM search_trigram WHERE rowid IN (SELECT doc_id FROM search_docs WHERE page_id = NEW.id);
    INSERT INTO search_docs (page_id) SELECT id FROM pages
        WHERE id = NEW.id AND NOT EXISTS (SELECT 1 FROM search_docs WHERE page_id = NEW.id);
    INSERT INTO search_index (rowid, title, body)
        SELECT d.doc_id, s.title, s.body
        FROM page_search_source AS s JOIN search_docs AS d ON d.page_id = s.page_id
        WHERE s.page_id = NEW.id;
    INSERT INTO search_trigram (rowid, title, body)
        SELECT rowid, title, body FROM search_index
        WHERE rowid IN (SELECT doc_id FROM search_docs WHERE page_id = NEW.id);
END;

CREATE TRIGGER pages_search_au AFTER UPDATE OF title, content_json, archived_at ON pages BEGIN
    DELETE FROM search_index WHERE rowid IN (SELECT doc_id FROM search_docs WHERE page_id = NEW.id);
    DELETE FROM search_trigram WHERE rowid IN (SELECT doc_id FROM search_docs WHERE page_id = NEW.id);
    INSERT INTO search_docs (page_id) SELECT id FROM pages
        WHERE id = NEW.id AND NOT EXISTS (SELECT 1 FROM search_docs WHERE page_id = NEW.id);
    INSERT INTO search_index (rowid, title, body)
        SELECT d.doc_id, s.title, s.body
        FROM page_search_source AS s JOIN search_docs AS d ON d.page_id = s.page_id
        WHERE s.page_id = NEW.id;
    INSERT INTO search_trigram (rowid, title, body)
        SELECT rowid, title, body FROM search_index
        WHERE rowid IN (SELECT doc_id FROM search_docs WHERE page_id = NEW.id);
END;

CREATE TRIGGER pages_search_ad AFTER DELETE ON pages BEGIN
    DELETE FROM search_index WHERE rowid IN (SELECT doc_id FROM search_docs WHERE page_id = OLD.id);
    DELETE FROM search_trigram WHERE rowid IN (SELECT doc_id FROM search_docs WHERE page_id = OLD.id);
    DELETE FROM search_docs WHERE page_id = OLD.id;
END;

CREATE TRIGGER cells_search_ai AFTER INSERT ON database_property_values BEGIN
    DELETE FROM search_index WHERE rowid IN (SELECT doc_id FROM search_docs WHERE page_id = NEW.page_id);
    DELETE FROM search_trigram WHERE rowid IN (SELECT doc_id FROM search_docs WHERE page_id = NEW.page_id);
    INSERT INTO search_docs (page_id) SELECT id FROM pages
        WHERE id = NEW.page_id AND NOT EXISTS (SELECT 1 FROM search_docs WHERE page_id = NEW.page_id);
    INSERT INTO search_index (rowid, title, body)
        SELECT d.doc_id, s.title, s.body
        FROM page_search_source AS s JOIN search_docs AS d ON d.page_id = s.page_id
        WHERE s.page_id = NEW.page_id;
    INSERT INTO search_trigram (rowid, title, body)
        SELECT rowid, title, body FROM search_index
        WHERE rowid IN (SELECT doc_id FROM search_docs WHERE page_id = NEW.page_id);
END;

CREATE TRIGGER cells_search_au AFTER UPDATE ON database_property_values BEGIN
    DELETE FROM search_index WHERE rowid IN (SELECT doc_id FROM search_docs WHERE page_id = NEW.page_id);
    DELETE FROM search_trigram WHERE rowid IN (SELECT doc_id FROM search_docs WHERE page_id = NEW.page_id);
    INSERT INTO search_docs (page_id) SELECT id FROM pages
        WHERE id = NEW.page_id AND NOT EXISTS (SELECT 1 FROM search_docs WHERE page_id = NEW.page_id);
    INSERT INTO search_index (rowid, title, body)
        SELECT d.doc_id, s.title, s.body
        FROM page_search_source AS s JOIN search_docs AS d ON d.page_id = s.page_id
        WHERE s.page_id = NEW.page_id;
    INSERT INTO search_trigram (rowid, title, body)
        SELECT rowid, title, body FROM search_index
        WHERE rowid IN (SELECT doc_id FROM search_docs WHERE page_id = NEW.page_id);
END;

CREATE TRIGGER cells_search_ad AFTER DELETE ON database_property_values BEGIN
    DELETE FROM search_index WHERE rowid IN (SELECT doc_id FROM search_docs WHERE page_id = OLD.page_id);
    DELETE FROM search_trigram WHERE rowid IN (SELECT doc_id FROM search_docs WHERE page_id = OLD.page_id);
    INSERT INTO search_docs (page_id) SELECT id FROM pages
        WHERE id = OLD.page_id AND NOT EXISTS (SELECT 1 FROM search_docs WHERE page_id = OLD.page_id);
    INSERT INTO search_index (rowid, title, body)
        SELECT d.doc_id, s.title, s.body
        FROM page_search_source AS s JOIN search_docs AS d ON d.page_id = s.page_id
        WHERE s.page_id = OLD.page_id;
    INSERT INTO search_trigram (rowid, title, body)
        SELECT rowid, title, body FROM search_index
        WHERE rowid IN (SELECT doc_id FROM search_docs WHERE page_id = OLD.page_id);
END;

CREATE TRIGGER columns_search_au AFTER UPDATE OF options_json, type ON database_properties BEGIN
    DELETE FROM search_index WHERE rowid IN (
        SELECT d.doc_id FROM search_docs AS d
        JOIN database_property_values AS v ON v.page_id = d.page_id
        WHERE v.property_id = NEW.id
    );
    DELETE FROM search_trigram WHERE rowid IN (
        SELECT d.doc_id FROM search_docs AS d
        JOIN database_property_values AS v ON v.page_id = d.page_id
        WHERE v.property_id = NEW.id
    );
    INSERT INTO search_index (rowid, title, body)
        SELECT d.doc_id, s.title, s.body
        FROM page_search_source AS s JOIN search_docs AS d ON d.page_id = s.page_id
        WHERE s.page_id IN (SELECT page_id FROM database_property_values WHERE property_id = NEW.id);
    INSERT INTO search_trigram (rowid, title, body)
        SELECT rowid, title, body FROM search_index
        WHERE rowid IN (
            SELECT d.doc_id FROM search_docs AS d
            JOIN database_property_values AS v ON v.page_id = d.page_id
            WHERE v.property_id = NEW.id
        );
END;

CREATE TRIGGER file_text_search_ai AFTER INSERT ON file_text BEGIN
    INSERT INTO file_search_index (rowid, name, body)
        SELECT NEW.chunk_id, f.filename, NEW.body FROM page_attachments AS f WHERE f.id = NEW.file_id;
    INSERT INTO file_search_trigram (rowid, name, body)
        SELECT NEW.chunk_id, f.filename, NEW.body FROM page_attachments AS f WHERE f.id = NEW.file_id;
END;

-- Also fires for the ON DELETE CASCADE from page_attachments.
CREATE TRIGGER file_text_search_ad AFTER DELETE ON file_text BEGIN
    DELETE FROM file_search_index WHERE rowid = OLD.chunk_id;
    DELETE FROM file_search_trigram WHERE rowid = OLD.chunk_id;
END;

-- Backfill from the word indexes, which already hold exactly the live rows.
INSERT INTO search_trigram (rowid, title, body) SELECT rowid, title, body FROM search_index;
INSERT INTO file_search_trigram (rowid, name, body) SELECT rowid, name, body FROM file_search_index;
