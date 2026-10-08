-- 0005_database_pages.sql
-- Adds a second page kind: 'database' pages hold typed, tabular data
-- instead of a Tiptap document -- see docs/adr/0004-database-pages.md.
-- A database's *rows* are themselves ordinary 'document' pages
-- (parent_page_id = the database page), so this migration only adds
-- what's genuinely new: the kind marker itself, and the columns/cells
-- layer on top.

ALTER TABLE pages ADD COLUMN kind TEXT NOT NULL DEFAULT 'document'
    CHECK (kind IN ('document', 'database'));

-- One row per column on a database page. options_json only means
-- something for type = 'select' (a Status column and friends): a JSON
-- array of {"id", "label", "color"} -- freely added/renamed/recolored/
-- removed by the user, never a fixed enum.
CREATE TABLE database_properties (
    id           TEXT PRIMARY KEY,
    page_id      TEXT NOT NULL REFERENCES pages(id) ON DELETE CASCADE,
    name         TEXT NOT NULL,
    type         TEXT NOT NULL CHECK (type IN
                     ('text', 'select', 'date', 'person', 'number', 'checkbox')),
    options_json TEXT NOT NULL DEFAULT '[]',
    position     TEXT NOT NULL,
    created_at   TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE INDEX idx_database_properties_page_id ON database_properties(page_id);

-- One cell. page_id here is a *row* (a normal page whose parent_page_id
-- is the database page) -- property_id ties it back to the column it
-- belongs to. value_json's shape depends on the column's type: a plain
-- string for text, {"option_id": "..."} for select, an ISO date string
-- for date, a user_id string for person, a number for number, a bool
-- for checkbox.
CREATE TABLE database_property_values (
    page_id     TEXT NOT NULL REFERENCES pages(id) ON DELETE CASCADE,
    property_id TEXT NOT NULL REFERENCES database_properties(id) ON DELETE CASCADE,
    value_json  TEXT NOT NULL,
    PRIMARY KEY (page_id, property_id)
);

CREATE INDEX idx_database_property_values_property_id ON database_property_values(property_id);
