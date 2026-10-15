-- 0021_board_columns.sql
-- The board (kanban) view's column order, per column the board groups
-- by: a JSON object {property_id: [column keys]}, a key being an option
-- id or a person's user id, "" for the "No <name>" column. Layout only:
-- the property's own option order (the table's dropdowns) and the rows'
-- values are untouched. Columns not listed follow in their usual order.
ALTER TABLE database_settings ADD COLUMN board_columns TEXT NOT NULL DEFAULT '{}';
