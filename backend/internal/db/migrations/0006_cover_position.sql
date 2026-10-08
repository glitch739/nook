-- 0006_cover_position.sql
-- Notion-style "drag to reposition" for a page's cover image: which
-- vertical slice of the image shows through the fixed-height crop
-- window (frontend renders it as object-position: center <value>%).
-- 50 = centered, matching object-fit: cover's own default and what a
-- freshly uploaded cover starts at.

ALTER TABLE page_covers ADD COLUMN position_y REAL NOT NULL DEFAULT 50;
