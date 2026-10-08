-- 0007_database_title_position.sql
-- Lets a database page's title ("Name") column be dragged and dropped
-- among its other columns too, instead of staying pinned first (see
-- ADR 0004 for the base feature). The title isn't a
-- database_properties row -- it lives on the page itself, not as a
-- column definition -- so its position needs its own small home.
--
-- One row per database page, created lazily (see
-- internal/databases.Store.GetTitleColumnPosition) rather than
-- backfilled for every existing database: "no row yet" already means
-- the same thing as the DEFAULT below (title stays first), which is
-- every database's actual behavior before this existed.
CREATE TABLE database_settings (
    page_id               TEXT PRIMARY KEY REFERENCES pages(id) ON DELETE CASCADE,
    title_column_position TEXT NOT NULL DEFAULT '000000'
);
