-- 0014_search.sql
-- Full-text search for the sidebar search box (ADR 0007).
--
-- search_index is an FTS5 table over every live page: its title, plus a
-- plain-text body made of the Tiptap document's text nodes and the
-- page's text/number/select database cells (so a database row is
-- findable by its cell values, not just its title). The unicode61
-- tokenizer with remove_diacritics 2 makes matching case- and
-- accent-insensitive: "hazkoruli" finds "házkörüli".
--
-- Kept in sync entirely by triggers, so no Go write path (pages,
-- databases, collab snapshots, a future import) can forget to update
-- it. Archived pages are simply absent from page_search_source, so the
-- same refresh that indexes a page also drops it when archived and
-- re-adds it if it is ever restored.

-- FTS5 rows are keyed by an integer rowid, pages by a TEXT id. A plain
-- pages.rowid would work until the next VACUUM, which is free to
-- renumber implicit rowids; this table's INTEGER PRIMARY KEY is stable.
CREATE TABLE search_docs (
    doc_id  INTEGER PRIMARY KEY,
    page_id TEXT NOT NULL UNIQUE
);

CREATE VIRTUAL TABLE search_index USING fts5(
    title,
    body,
    tokenize = 'unicode61 remove_diacritics 2'
);

-- What gets indexed for one page. json_valid guards keep a malformed
-- content_json/value_json from making the trigger -- and with it the
-- user's own save -- fail.
CREATE VIEW page_search_source AS
SELECT
    p.id    AS page_id,
    p.title AS title,
    trim(
        COALESCE((
            SELECT group_concat(t.value, ' ')
            FROM json_tree(CASE WHEN json_valid(p.content_json) THEN p.content_json ELSE '{}' END) AS t
            WHERE t.key = 'text' AND t.type = 'text'
        ), '')
        || ' ' ||
        COALESCE((
            SELECT group_concat(c.txt, ' ')
            FROM (
                SELECT CASE dp.type
                    WHEN 'select' THEN (
                        SELECT o.value ->> 'label'
                        FROM json_each(CASE WHEN json_valid(dp.options_json) THEN dp.options_json ELSE '[]' END) AS o
                        WHERE o.value ->> 'id' = (v.value_json ->> 'option_id')
                    )
                    ELSE v.value_json ->> '$'
                END AS txt
                FROM database_property_values AS v
                JOIN database_properties AS dp ON dp.id = v.property_id
                WHERE v.page_id = p.id
                  AND dp.type IN ('text', 'number', 'select')
                  AND json_valid(v.value_json)
            ) AS c
        ), '')
    ) AS body
FROM pages AS p
WHERE p.archived_at IS NULL;

-- Every trigger below runs the same three-step refresh for one page id:
-- drop its index row, make sure it has a doc_id (only if the page still
-- exists, so a cascade-delete can't leave an orphan mapping behind),
-- and re-insert it from page_search_source.
--
-- The doc_id step is NOT EXISTS rather than INSERT OR IGNORE on
-- purpose: SQLite lets the outer statement's conflict policy override
-- a trigger's OR clause, so under databases.Store.SetValue's upsert an
-- OR IGNORE here would turn back into an ABORT.

CREATE TRIGGER pages_search_ai AFTER INSERT ON pages BEGIN
    DELETE FROM search_index WHERE rowid IN (SELECT doc_id FROM search_docs WHERE page_id = NEW.id);
    INSERT INTO search_docs (page_id) SELECT id FROM pages
        WHERE id = NEW.id AND NOT EXISTS (SELECT 1 FROM search_docs WHERE page_id = NEW.id);
    INSERT INTO search_index (rowid, title, body)
        SELECT d.doc_id, s.title, s.body
        FROM page_search_source AS s JOIN search_docs AS d ON d.page_id = s.page_id
        WHERE s.page_id = NEW.id;
END;

CREATE TRIGGER pages_search_au AFTER UPDATE OF title, content_json, archived_at ON pages BEGIN
    DELETE FROM search_index WHERE rowid IN (SELECT doc_id FROM search_docs WHERE page_id = NEW.id);
    INSERT INTO search_docs (page_id) SELECT id FROM pages
        WHERE id = NEW.id AND NOT EXISTS (SELECT 1 FROM search_docs WHERE page_id = NEW.id);
    INSERT INTO search_index (rowid, title, body)
        SELECT d.doc_id, s.title, s.body
        FROM page_search_source AS s JOIN search_docs AS d ON d.page_id = s.page_id
        WHERE s.page_id = NEW.id;
END;

CREATE TRIGGER pages_search_ad AFTER DELETE ON pages BEGIN
    DELETE FROM search_index WHERE rowid IN (SELECT doc_id FROM search_docs WHERE page_id = OLD.id);
    DELETE FROM search_docs WHERE page_id = OLD.id;
END;

CREATE TRIGGER cells_search_ai AFTER INSERT ON database_property_values BEGIN
    DELETE FROM search_index WHERE rowid IN (SELECT doc_id FROM search_docs WHERE page_id = NEW.page_id);
    INSERT INTO search_docs (page_id) SELECT id FROM pages
        WHERE id = NEW.page_id AND NOT EXISTS (SELECT 1 FROM search_docs WHERE page_id = NEW.page_id);
    INSERT INTO search_index (rowid, title, body)
        SELECT d.doc_id, s.title, s.body
        FROM page_search_source AS s JOIN search_docs AS d ON d.page_id = s.page_id
        WHERE s.page_id = NEW.page_id;
END;

CREATE TRIGGER cells_search_au AFTER UPDATE ON database_property_values BEGIN
    DELETE FROM search_index WHERE rowid IN (SELECT doc_id FROM search_docs WHERE page_id = NEW.page_id);
    INSERT INTO search_docs (page_id) SELECT id FROM pages
        WHERE id = NEW.page_id AND NOT EXISTS (SELECT 1 FROM search_docs WHERE page_id = NEW.page_id);
    INSERT INTO search_index (rowid, title, body)
        SELECT d.doc_id, s.title, s.body
        FROM page_search_source AS s JOIN search_docs AS d ON d.page_id = s.page_id
        WHERE s.page_id = NEW.page_id;
END;

CREATE TRIGGER cells_search_ad AFTER DELETE ON database_property_values BEGIN
    DELETE FROM search_index WHERE rowid IN (SELECT doc_id FROM search_docs WHERE page_id = OLD.page_id);
    INSERT INTO search_docs (page_id) SELECT id FROM pages
        WHERE id = OLD.page_id AND NOT EXISTS (SELECT 1 FROM search_docs WHERE page_id = OLD.page_id);
    INSERT INTO search_index (rowid, title, body)
        SELECT d.doc_id, s.title, s.body
        FROM page_search_source AS s JOIN search_docs AS d ON d.page_id = s.page_id
        WHERE s.page_id = OLD.page_id;
END;

-- Renaming a select option (or changing a column's type) changes the
-- indexed text of every row that has a value in that column.
CREATE TRIGGER columns_search_au AFTER UPDATE OF options_json, type ON database_properties BEGIN
    DELETE FROM search_index WHERE rowid IN (
        SELECT d.doc_id FROM search_docs AS d
        JOIN database_property_values AS v ON v.page_id = d.page_id
        WHERE v.property_id = NEW.id
    );
    INSERT INTO search_index (rowid, title, body)
        SELECT d.doc_id, s.title, s.body
        FROM page_search_source AS s JOIN search_docs AS d ON d.page_id = s.page_id
        WHERE s.page_id IN (SELECT page_id FROM database_property_values WHERE property_id = NEW.id);
END;

-- Backfill every existing page.
INSERT INTO search_docs (page_id) SELECT id FROM pages;
INSERT INTO search_index (rowid, title, body)
    SELECT d.doc_id, s.title, s.body
    FROM page_search_source AS s JOIN search_docs AS d ON d.page_id = s.page_id;
