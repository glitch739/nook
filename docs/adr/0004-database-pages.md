# ADR 0004: Database pages — a second page kind for structured, tabular data

Status: Accepted and implemented — 2026-09-20 (MVP through the deferred cover-image piece all built the same session; see DEVLOG 2026-09-20)

## Context

Requested directly, with reference screenshots of a ClickUp-style table:
a page that's a **table of rows** with typed, editable columns —
critically a **Status** column with user-defined values (not a fixed
enum: "Kész", "Folyamatban", "Válaszra várunk", "El nem kezdett" were
all invented by the user, colored per value), plus columns like
"Utoljára módosította", "Utolsó módosítás dátuma", "Prioritás". Clicking
a row opens a detail view: the row's properties at the top, then a
free-form rich-text body below (the second screenshot's "Leírások"
section — headings, bold text, nested lists). A page cover image/banner
was also requested, explicitly as the **lowest priority** part of this
ask — the table itself is "the main thing."

This is a genuinely different shape from every page Nook has today.
`internal/pages` currently models exactly one kind of page: a title plus
one Tiptap/ProseMirror JSON document (ADR 0002). A table needs typed
columns shared across many rows, per-cell values, and a stable schema
that isn't just "whatever's in this page's own JSON blob" — none of
which the current model has any of.

**This does not conflict with ADR 0002.** That ADR decided how the
*content of a document page* is stored (one JSON blob, not a blocks
table) — it says nothing about whether a *different kind of page* can
have a structured schema. A database page and a document page are two
different concepts; ADR 0002 only governs the second one.

**The key insight that keeps this small**: a database *row* doesn't need
new content infrastructure at all — it can just **be a page**
(`parent_page_id` = the database page, same `pages` table, same
`content_json`, same icon, same archiving). The row-detail view in the
second screenshot (title + properties + rich body) is then *exactly*
today's page view, with a properties strip rendered above the existing
`Editor.tsx`. Only the *columns and cell values* are new concepts; row
creation, deletion, icons, and the row's own rich-text body reuse
`internal/pages` and `Editor.tsx` completely unchanged.

## Decision

### Schema

```sql
-- Every existing page keeps working exactly as it does today --
-- 'document' is the default, and is what every current row already is.
ALTER TABLE pages ADD COLUMN kind TEXT NOT NULL DEFAULT 'document'
    CHECK (kind IN ('document', 'database'));

-- One row per column on a database page. options_json only means
-- something for type = 'select' (the Status column and friends):
-- [{"id": "...", "label": "Kész", "color": "#..."}, ...] -- the user
-- can add/rename/recolor/reorder/delete these freely, which is exactly
-- the "saját státusz editing" ask.
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

-- One cell. page_id here is a *row* (a normal 'document' page whose
-- parent_page_id is the database page) -- property_id ties it back to
-- the column it belongs to and, transitively, to which database it's
-- part of.
CREATE TABLE database_property_values (
    page_id     TEXT NOT NULL REFERENCES pages(id) ON DELETE CASCADE,
    property_id TEXT NOT NULL REFERENCES database_properties(id) ON DELETE CASCADE,
    value_json  TEXT NOT NULL,
    PRIMARY KEY (page_id, property_id)
);
```

`value_json`'s shape depends on the column's `type`: a plain string for
`text`, `{"option_id": "..."}` for `select`, an ISO date string for
`date`, a `user_id` for `person`, a number for `number`, a bool for
`checkbox`.

### API (additive — nothing existing changes shape)

- `POST /api/spaces/{spaceID}/pages` gains an optional `"kind"` field
  (`"document"` default, or `"database"`).
- `POST /api/pages/{pageID}/properties` — add a column.
- `PUT /api/pages/{pageID}/properties/{propertyID}` — rename, change
  type, or edit `options_json` (add/rename/recolor/remove a status
  value).
- `DELETE /api/pages/{pageID}/properties/{propertyID}`.
- `GET /api/pages/{pageID}/rows` — table-view-shaped read: every child
  page of a database page, each with its `database_property_values`
  joined in.
- `PUT /api/pages/{rowID}/properties/{propertyID}` — set one cell.
- **Not new:** creating a row is `POST /api/spaces/{spaceID}/pages`
  with `parent_page_id` = the database page (already exists); deleting
  one is `DELETE /api/pages/{rowID}` (already exists, already an
  archive not a hard delete); reading/editing a row's rich-text body is
  `GET`/`PUT /api/pages/{rowID}` (already exists, unchanged).

### Frontend

- `Page`/`TreeNode` gain `kind`. Sidebar rendering is unaffected except
  a database page's icon convention (e.g. defaults to a table/grid icon
  instead of 📄).
- A new `DatabaseView.tsx`, rendered in `workspace-body` instead of
  `Editor` when `activePage.kind === 'database'`: columns from
  `GET .../properties`, rows from `GET .../rows`. Inline cell editing;
  a `select` cell reuses the same portal+anchor popover pattern
  `IconPicker.tsx` already has (a natural fit — same "small popover
  anchored to a clicked cell" shape).
- Clicking a row opens a detail panel: title, a properties strip built
  from the same column list, and the row's body rendered through the
  existing `Editor.tsx` — no new editor code.

### Scope, in the user's own priority order

1. **MVP:** database page kind, `text`/`select`/`date` properties,
   table view, inline cell editing, user-editable status values
   (add/rename/recolor), row create/delete (reusing page CRUD), row
   detail view reusing `Editor.tsx`.
2. `person`/`number`/`checkbox` property types, if the MVP proves the
   shape works.
3. **Deliberately last, per explicit request:** page cover
   images/banners. This needs file storage, which doesn't exist in
   Nook at all yet — it's the same missing piece as the "Attachments"
   line already on the ROADMAP (Phase 5), so cover images should ride
   on whatever that work builds, not get a one-off upload path of its
   own.

### Explicitly out of scope for now

Other database *view* types (board/kanban, gallery, calendar) —
table only. Formulas, rollups, relations between databases, filters/sorts
saved as view state — all genuine Notion/ClickUp features, all
meaningfully bigger than this MVP. Worth a follow-up ADR each if/when
they're actually wanted, not designed against speculatively here.

## Consequences

- Two new tables, one additive column on `pages` — no migration touches
  or reshapes anything that exists today; every current page keeps
  `kind = 'document'` and behaves identically.
- `internal/pages` stays the single source of truth for what a page
  *is* (title, icon, position, archiving, parent/child nesting) for
  both kinds; only a new `internal/databases` (or similar) package owns
  properties/values, the same layering `internal/pages` already has
  relative to `internal/spaces`.
- This is real feature-implementation work, not plumbing — per the
  established collaboration split, the schema/API/skeleton above is
  what gets pair-reviewed and then built together, not solo-implemented
  outright.

## Implementation notes (2026-09-20)

Explicitly re-requested solo-implemented outright, all three scope
stages in one session — overriding the collaboration-split note above
for this specific piece of work (see DEVLOG). What was built matches
this ADR closely, with the differences worth recording:

- `internal/databases` (not a parenthetical maybe) is the package name;
  `migrations/0004_database_pages.sql` is the schema exactly as
  designed above.
- Cover images needed real file storage that didn't exist, so a new
  `internal/assets` package + `page_covers` table
  (`migrations/0005_page_covers.sql`) were added — narrower than a
  general attachments system (one image per page, on local disk under
  `NOOK_UPLOADS_DIR`), exactly as scoped: "rides on whatever the
  Attachments work builds" became "this is that work's first slice,"
  not a separate one-off.
- Frontend: `DatabaseView.tsx` (table), `RowDetailPanel.tsx` (a row's
  side panel — title, properties, and its own rich-text body via
  `Editor.tsx`, unchanged), `PropertyCell.tsx` (per-type cell editors),
  `PageCover.tsx`. A `Popover.tsx` primitive was pulled out of
  `IconPicker.tsx` along the way (position/portal logic used by four
  more new popovers here) rather than copied a fourth time.
- `person` properties ended up as a plain display-name string, not a
  real per-user reference, exactly as scoped in "Explicitly out of
  scope for now" — Nook has no space-membership listing API yet.
