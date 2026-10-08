# Architecture

See [ADR 0001](adr/0001-go-sqlite-monolith.md) and
[ADR 0002](adr/0002-page-content-as-prosemirror-json.md) for the reasoning
behind the two biggest structural decisions. This document is the current
map of the system; it's expected to grow as later phases land.

## Shape

Single Go binary. Layers, request flow:

```
HTTP handler (internal/httpapi)
    -> service / business logic (internal/{auth,users,spaces,pages,...})
    -> repository (SQL, same package as its domain)
    -> SQLite (internal/db)
```

The frontend (`frontend/`, Vite + React + TypeScript + Tiptap) builds to
static assets that the Go binary embeds via `embed.FS` — one binary, one
deployment artifact. No frontend build step is required to run a
pre-built release; it's only needed when frontend source changes.

## Backend packages (`backend/internal/`)

| Package | Responsibility |
|---|---|
| `db` | SQLite connection, pragmas, embedded migrations, ID generation |
| `config` | Environment-variable configuration |
| `auth` | Password hashing, sessions, login/logout |
| `users` | User accounts |
| `spaces` | Spaces + membership + roles |
| `permissions` | Central `CanAccessSpace(user, space, minRole)`-style checks — every handler that touches a space or its pages goes through this, never an ad hoc check |
| `pages` | Page tree + content |
| `httpapi` | Route wiring, request/response, middleware |

Not yet scaffolded (added when their phase starts): `realtime`
(WebSocket/Yjs, Phase 3), `search` (FTS5, ADR 0007).

## Data model (current — Phase 1)

```
users           (id, username, password_hash, display_name, created_at)
sessions        (id, user_id, expires_at, created_at)
spaces          (id, name, type[private|shared], owner_id, created_at)
space_members   (space_id, user_id, role[owner|admin|editor|viewer], added_at)
pages           (id, space_id, parent_page_id, title, content_json,
                  position, created_by, archived_at, created_at, updated_at)
```

`content_json` is a full Tiptap/ProseMirror document (see ADR 0002) — there
is no separate blocks table.

Migrations live at `backend/internal/db/migrations/*.sql`, embedded into
the binary and applied in filename order at startup, tracked in a
`schema_migrations` table. See `backend/internal/db/db.go`.

## Authorization rule

No query for a page or space's contents runs without checking
`space_members` for the requesting user first. This is enforced in one
place (`internal/permissions`), not scattered per-handler, so it can't be
silently skipped when a new endpoint is added. The frontend never makes an
authorization decision on the server's behalf.

## Running locally

```
cd backend
go run ./cmd/nook
```

Config via environment variables (see `internal/config/config.go`):

| Var | Default | Meaning |
|---|---|---|
| `NOOK_ADDR` | `:8080` | HTTP listen address |
| `NOOK_DB_PATH` | `nook.db` | SQLite file path |
