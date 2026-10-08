-- 0017_user_preferences.sql
-- Per-account UI preferences (theme, accent color, ...) as one JSON
-- object, so they follow the person across devices. The server treats
-- the contents as opaque; the frontend owns the shape.
ALTER TABLE users ADD COLUMN preferences TEXT NOT NULL DEFAULT '{}';
