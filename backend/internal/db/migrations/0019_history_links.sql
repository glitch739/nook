-- 0019_history_links.sql
-- Page history and links between pages, both kept up to date from the
-- realtime editor's snapshots (collab.Store.Compact).
--
-- page_versions: the document (content_json) as it stood at the end of
-- an editing stretch. A snapshot updates the newest version while it is
-- less than 10 minutes old and starts a new one after that, so the state
-- before any change is always in the version before it. Capped per page.
--
-- page_links: which pages a page links to (its pageMention nodes), for
-- the "linked from" list under a page.

CREATE TABLE page_versions (
    id           TEXT PRIMARY KEY,
    page_id      TEXT NOT NULL REFERENCES pages(id) ON DELETE CASCADE,
    content_json TEXT NOT NULL,
    user_id      TEXT REFERENCES users(id) ON DELETE SET NULL,
    created_at   TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_at   TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE INDEX page_versions_page ON page_versions (page_id, created_at);

CREATE TABLE page_links (
    from_page_id TEXT NOT NULL REFERENCES pages(id) ON DELETE CASCADE,
    to_page_id   TEXT NOT NULL REFERENCES pages(id) ON DELETE CASCADE,
    PRIMARY KEY (from_page_id, to_page_id)
);

CREATE INDEX page_links_to ON page_links (to_page_id);
