# ADR 0007: Sidebar search (SQLite FTS5, trigger-maintained)

Status: Accepted — 2026-09-28

## Context

The sidebar has had a "Keresés…" box since Phase 1 that did nothing.
Users need to find pages by title and by what is written in them,
including database rows by their cell values. Most content is
Hungarian, so "hazkoruli" has to find "Házkörüli", and typing a word's
beginning has to be enough. Target hardware is a Raspberry Pi: no
separate search service.

## Decision

**An FTS5 virtual table inside the existing SQLite database, kept in
sync by SQL triggers.** (`0014_search.sql`, `internal/search`,
`GET /api/search?q=`, `frontend/src/SidebarSearch.tsx`.)

### Index

- `search_index(title, body)` — FTS5, tokenizer
  `unicode61 remove_diacritics 2`: case- and accent-insensitive
  (á→a, ő→o, ű→u, …).
- `body` = the text nodes of the Tiptap document (`json_tree` over
  `content_json`, key `text`) + the page's `text`, `number` and `select`
  database cells (select cells index the option's *label*). JSON
  structure (`"paragraph"`, `"type"`) is never indexed.
- `search_docs(doc_id INTEGER PRIMARY KEY, page_id UNIQUE)` maps FTS
  rowids to page ids. `pages.rowid` is not used because `VACUUM` may
  renumber implicit rowids.
- `page_search_source` (a view) is the one definition of "what gets
  indexed for a page". Archived pages are simply absent from it.

### Why triggers, not Go code

Title, content, cell values and option labels are written from several
places (pages API, databases API, collab snapshots, future imports).
Triggers on `pages`, `database_property_values` and
`database_properties` make the index impossible to forget. Each one
runs the same refresh: delete the page's FTS row, ensure a `doc_id`,
re-insert from the view. `json_valid` guards make sure a malformed
blob can never make a user's save fail.

Gotcha found in testing: SQLite lets the *outer* statement's conflict
policy override a trigger's `INSERT OR IGNORE`, so under the cells
upsert (`ON CONFLICT DO UPDATE`) it turned back into an ABORT. The
refresh therefore uses `INSERT … WHERE NOT EXISTS` instead.

### Query

- User text is split into letter/digit runs; each becomes a quoted
  prefix term (`"ház"*`), ANDed, at most 8 terms. No FTS5 syntax
  (`OR`, `NEAR`, `col:`) can reach the engine. `q` is capped at 200
  characters.
- Ranking: `bm25` with a title hit worth 10× a body hit. Limit 30.
- Access is filtered inside the SQL, mirroring
  `permissions.CanAccessSpace`: a direct space membership, or
  membership of the space's non-archived group; archived spaces and
  pages are excluded.
- Matches come back wrapped in `\u0002 … \u0003` (title via
  `highlight()`, body excerpt via `snippet()`). The frontend splits on
  them and renders `<mark>` elements, so no server-built HTML is ever
  injected.

### Frontend

- Groups and spaces are filtered client-side with the same folding
  (NFD + strip marks + lowercase). They are already loaded and few, so
  a request would add nothing.
- While the box has text, the tree is replaced by the result list:
  pages first (icon, highlighted title, `Csoport › Tér › Szülő` path
  built from the loaded tree, highlighted excerpt), then groups/spaces.
- 150 ms debounce, stale requests aborted, previous list shown dimmed
  while a new one loads. ↑/↓/Enter/Esc; Cmd/Ctrl+K focuses the box.
- Clicking a page opens it and keeps the results (it's a filter, not a
  one-shot palette). Clicking a group/space clears the search and
  scrolls the tree to it with a brief highlight.

## Consequences

- Pages being edited live are indexed as of their latest collab
  snapshot (the `content_json` projection, ADR 0006), not every
  keystroke. Fine for search.
- Matching is word-prefix, not arbitrary substring: "körüli" does not
  find "házkörüli". Accepted (same as Notion); a trigram index could be
  added later if needed.
- Every content save now also re-tokenizes that page. Negligible at
  this scale; the work is proportional to one page.
- Person/date/checkbox cells are not indexed.
