# ADR 0002: Page content is a single Tiptap/ProseMirror JSON document, no separate blocks table

Status: Accepted — 2026-09-02

## Context

Two candidate data models for a page's editable content were compared:

**A — normalized `blocks` table.** One row per block
(`id, page_id, parent_block_id, type, content_json, position`), tree
structure via `parent_block_id`. This mirrors Notion's own internal data
model.

**B — single document.** `pages.content_json` holds the entire
Tiptap/ProseMirror document for that page as one JSON tree. Page-to-page
hierarchy still uses `parent_page_id` in SQL; only the content *within* a
page is a single JSON blob.

The frontend editor is Tiptap (ProseMirror). Notion built model A *and*
its own editor to match it. We are not writing a custom editor — adopting
model A here would mean maintaining a second, hand-rolled document model
that every ProseMirror node type has to be serialized into and
deserialized out of, kept in sync by hand as the editor schema grows
(nested lists, table cells, etc. don't flatten cleanly into independent
rows).

The deciding factor is realtime collaboration (Phase 3). Tiptap's
supported collaboration path is `y-prosemirror`, which binds **one Y.Doc /
Y.XmlFragment to one whole ProseMirror document** — not one CRDT per
block. Model B is what that library expects; model A would require
either per-block CRDT documents (unsupported by the library, substantial
custom infrastructure) or treating the blocks table as a derived cache
over the real Yjs state, which is an unnecessary extra layer.

Search (Phase 4, SQLite FTS5) does not require block granularity either:
plain text can be extracted from `content_json` on save into an FTS
index, at page granularity.

## Decision

- No `blocks` table in Phase 1.
- `pages.content_json` stores the full Tiptap/ProseMirror `doc` JSON.
- Page hierarchy (nesting in the sidebar) stays relational via
  `pages.parent_page_id` — this ADR only concerns *content within* a page.
- To keep a cheap door open for block-level addressing later (comments,
  permalinks — Phase 5+), give top-level ProseMirror nodes a stable `id`
  attribute via a small custom Tiptap extension, rather than normalizing
  storage. Not needed for Phase 1; noted here so the schema isn't designed
  against it.

## Consequences

- Editing a page reads/writes one JSON blob — matches Tiptap's own
  getJSON()/setContent() shape exactly, no translation layer.
- Phase 3 realtime: the natural persistence shape becomes a per-page Yjs
  update log/snapshot (e.g. `page_ydoc_state(page_id, state BLOB,
  updated_at)`), with `content_json` as a derived, easily-queried snapshot
  kept in sync for search/export/plain API reads. This is additive to the
  current schema, not a rewrite.
- Ordering of content *within* a page is whatever order ProseMirror stores
  nodes in — no per-block `position` column needed. `pages.position` still
  orders sibling pages in the tree.
