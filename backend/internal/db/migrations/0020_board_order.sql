-- 0020_board_order.sql
-- A database's board (kanban) view keeps its own card order, separate
-- from the table's row order: the row ids as a JSON array. One list per
-- database, whatever column the board groups by: a column shows its
-- cards in this order, rows not in the list after them.
ALTER TABLE database_settings ADD COLUMN board_order TEXT NOT NULL DEFAULT '[]';
