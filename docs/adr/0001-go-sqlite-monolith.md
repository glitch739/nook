# ADR 0001: Single Go binary + SQLite, no microservices

Status: Accepted — 2026-09-02

## Context

Nook targets a single self-hosted server, initially modest hardware (4-core
x86_64, ~6 GB RAM in the current dev VM; ARM64/Raspberry Pi is a stated
future target). Before settling on this stack, two existing self-hosted
Notion alternatives were evaluated hands-on on this same machine:

- **AFFiNE** — Rust/TypeScript monorepo, built from source in a Docker
  container (no native Node toolchain was even installed for it).
- **AppFlowy-Cloud** — a Docker Compose stack of 11 containers: Postgres
  (pgvector), Redis, MinIO, GoTrue (auth), nginx, admin frontend, and five
  app services.

Both are considerably heavier than this project's stated goals: easy to
run on constrained hardware, minimal moving parts, no mandatory external
services (no Postgres, no Redis, no Kubernetes, no microservices).

## Decision

- Backend: Go, compiled to a single binary.
- Database: SQLite (via `modernc.org/sqlite`, a pure-Go/cgo-free driver,
  chosen specifically to keep cross-compilation to ARM64 simple).
- Frontend: built separately (Vite/React/TS), its static output embedded
  into the Go binary via `embed.FS` — one artifact to deploy.
- Docker is a supported deployment option, not a requirement; the binary
  runs natively with no other services.

## Consequences

- No horizontal scaling story beyond a single writer process — acceptable,
  this is an explicitly single-server, small-team tool, not a SaaS.
- SQLite's single-writer model means `db.SetMaxOpenConns(1)` in
  [backend/internal/db/db.go](../../backend/internal/db/db.go); WAL mode
  keeps reads unblocked during writes.
- If a future requirement genuinely needs Postgres, Redis, or a search
  service, that should be its own ADR with a concrete justification, not a
  default.
