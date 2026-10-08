-- 0018_collections.sql
-- A third page kind, 'collection': a page that gathers documents and
-- databases as its children and lists them, instead of the sidebar
-- showing every one of them as its own row.
--
-- kind has a CHECK constraint (0004), and SQLite can only change one by
-- rebuilding the table: the documented 12-step procedure. That needs
-- foreign key enforcement off, otherwise dropping the old table would
-- cascade-delete every row referencing it (cells, covers, images,
-- attachments, history, ...). The marker below makes the migration
-- runner switch it off for this script only and run PRAGMA
-- foreign_key_check before committing.
--
-- nook:foreign-keys-off
--
-- The search view and the cell/column triggers read pages too; they are dropped
-- first (a rename refuses to run while they point at a missing table)
-- and recreated unchanged at the end, together with pages' own indexes
-- and triggers, which DROP TABLE removes.

DROP TRIGGER cells_search_ai;
DROP TRIGGER cells_search_au;
DROP TRIGGER cells_search_ad;
DROP TRIGGER columns_search_au;
DROP VIEW page_search_source;

CREATE TABLE pages_new (
    id             TEXT PRIMARY KEY,
    space_id       TEXT NOT NULL REFERENCES spaces(id) ON DELETE CASCADE,
    parent_page_id TEXT REFERENCES pages(id) ON DELETE CASCADE,
    title          TEXT NOT NULL DEFAULT '',
    content_json   TEXT NOT NULL DEFAULT '{"type":"doc","content":[]}',
    position       TEXT NOT NULL,
    created_by     TEXT NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    archived_at    TEXT,
    created_at     TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_at     TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    icon           TEXT NOT NULL DEFAULT '📄',
    kind           TEXT NOT NULL DEFAULT 'document'
        CHECK (kind IN ('document', 'database', 'collection')),
    updated_by     TEXT REFERENCES users(id) ON DELETE SET NULL
);

INSERT INTO pages_new (id, space_id, parent_page_id, title, content_json, position,
                       created_by, archived_at, created_at, updated_at, icon, kind, updated_by)
SELECT id, space_id, parent_page_id, title, content_json, position,
       created_by, archived_at, created_at, updated_at, icon, kind, updated_by
FROM pages;

DROP TABLE pages;
ALTER TABLE pages_new RENAME TO pages;

CREATE INDEX idx_pages_space_id       ON pages(space_id);
CREATE INDEX idx_pages_parent_page_id ON pages(parent_page_id);

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
