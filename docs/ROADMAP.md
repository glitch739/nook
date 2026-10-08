# Roadmap

## Current priorities (updated 2026-10-03)

Live at the owner's domain since 2026-10-03 (Cloudflare Tunnel + Access,
ADR 0008). Done since the 2026-10-02 list: export (Markdown/ZIP/JSON),
tab restore, live sidebar (SSE), command palette (Cmd/Ctrl+K), Move
up/down for touch, file attachments, Person column, archive view.

1. **Publish on GitHub** -- plan and decisions in
   [PUBLISHING.md](PUBLISHING.md); CI workflow prepared.
2. **2FA (TOTP)** on Nook's own login.
3. **Raspberry Pi** -- the image already builds for arm64 in CI.
4. Open review items: [REVIEW-2026-10.md](REVIEW-2026-10.md).

## Phase 1 — Core MVP (done)

- [x] Dev environment: Go (latest stable, user-local install), Node LTS (nvm)
- [x] Repo skeleton (`backend/`, `frontend/`, `docs/`)
- [x] SQLite schema + embedded migration runner (users, sessions, spaces,
      space_members, pages)
- [x] Auth: password hashing, login/logout, session middleware
      (`internal/auth`, routes wired, admin bootstrap via env vars —
      see DEVLOG 2026-09-02)
- [x] Space CRUD (`internal/spaces`, `POST/GET /api/spaces`) — done and
      reviewed, see DEVLOG 2026-09-02 16:38
- [x] Auto-created personal space on user creation — done, see DEVLOG
      2026-09-02 22:48. Wired into `bootstrapAdmin` (the only user-
      creation path today); Phase 2's real user-management flow needs to
      make the same `spaces.Create` call.
- [x] `internal/permissions` — `Checker.CanAccessSpace(user, space,
      minRole)`, done and tested (see DEVLOG 2026-09-02 22:36). Not
      wired into any route yet — nothing needs it until `pages` does;
      built now so that lands with it ready from day one.
- [x] **Rate limiting on `/api/auth/login` + a general baseline on every
      route** — done, see DEVLOG 2026-09-02 22:11. `internal/ratelimit`:
      per-IP token-bucket (`golang.org/x/time/rate`, in-memory), two
      independent instances (strict login limiter: 5 burst / 12s refill;
      looser general limiter: 30 burst / 10 req/s) wired as middleware in
      `httpapi.NewMux`. 429 + `Retry-After` over the limit. Verified live
      against a running server, not just unit tests. Static, content-hashed
      `GET /assets/*` files are exempt from the general limiter (2026-09-27,
      see DEVLOG) -- the icon picker's search legitimately fetches hundreds.
- [x] Context timeouts on every DB-touching handler — done via
      `http.TimeoutHandler` wrapping the whole mux (`config.RequestTimeout`,
      default 5s, `NOOK_REQUEST_TIMEOUT`), see DEVLOG 2026-09-02 22:36.
      A stuck/slow request can no longer pile up and exhaust SQLite's
      single-writer connection indefinitely.
- [x] Request body size limits (`http.MaxBytesReader`, per-route via
      `httpapi.maxBody`) on every write endpoint — done, see DEVLOG
      2026-09-02 22:48. Oversized bodies get a proper 413, not a generic
      400 (`httpx.DecodeJSONBody` distinguishes `*http.MaxBytesError`).
- [x] Structured logging — done, `log/slog` JSON-to-stdout (see DEVLOG
      2026-09-02 22:48). Failed *and* successful logins logged (ip,
      username/user_id, event), rate-limit rejections logged (ip, path).
      This is the self-hosted-appropriate "log collector": structured
      lines to stdout, deploy-time choice how they're captured
      (`docker logs`, journald, piped to a real aggregator) — Nook's code
      doesn't hardcode that choice.
- [x] Security response headers middleware — done
      (`httpapi.securityHeaders`), CSP (script-src 'self' only — that's
      the part that actually matters for XSS; style-src allows
      'unsafe-inline' for React's inline style attributes), X-Frame-
      Options, X-Content-Type-Options, Referrer-Policy. See DEVLOG
      2026-09-02 22:48.
- [x] ADR documenting the CSRF strategy — done,
      [ADR 0003](adr/0003-csrf-strategy.md): `SameSite=Lax` + JSON-only
      API is sufficient for the current surface; explicitly flagged as
      needing revisiting the day a plain HTML form endpoint (e.g. file
      upload) shows up.
- [x] Page CRUD + nested page tree queries — done and verified live
      (create, list-as-tree, get, update title/content, archive,
      permission-enforced), see DEVLOG 2026-09-08 21:08. `internal/pages`
      + `httpapi/pages.go`. Page icon added as a real, persisted column
      (`0002_page_icon.sql`) — done as part of the frontend wiring below,
      see DEVLOG 2026-09-08 21:31.
- [x] Minimal frontend: login (done); **sidebar with a teamspace/page
      tree, wired to the real API** — page selection, rename, click-icon
      to change icon, create (top-level and nested via per-row "+"),
      archive (per-row 🗑 with confirm) all persist through
      `internal/pages`, see DEVLOG 2026-09-08 21:31 (build on top of the
      original mock-data UI from DEVLOG 2026-09-03 00:18); **Notion-style
      tab bar** (sidebar toggle, open-page tabs, same entry — the old
      synthetic "new tab" button was removed once real page creation
      existed); block editor with paragraph/heading/list/todo/code/quote/
      divider (toolbar-only), content loads from and debounced-autosaves
      (800ms) to the real page. Drag-to-reorder + "turn into" via a block
      handle was built, verified broken against real screenshots twice,
      and removed (DEVLOG 2026-09-03 00:18) — a whole list only ever got
      one handle for every item in it, a real editor-architecture gap
      (list items need to be individually addressable blocks), not a CSS
      fix. Now that real page content exists, this is testable hands-on
      whenever it's revisited.
- [x] `go build` producing a single binary with the frontend embedded —
      done, see DEVLOG 2026-09-08 21:31. `internal/webui` (`go:embed
      dist`, served at `/` behind every `/api/*` route) + repo-root
      `Makefile` (`make build`/`frontend`/`embed`/`clean`). `dist/`
      ships a placeholder `index.html` in git so a fresh clone's
      `go build` doesn't need the frontend built first; real builds
      overwrite it locally, which is expected and not committed.
- [x] Manual end-to-end check: log in, build a nested page tree, edit a
      document, restart the server, confirm the data persisted — done
      against the actual compiled binary (not dev servers), see DEVLOG
      2026-09-08 21:31: 3-level nested tree, an icon change, and a full
      Hungarian-text Tiptap document all survived killing and restarting
      the process against the same database file, with the admin-
      bootstrap idempotency check also confirmed on a real restart.

**Phase 1 (Core MVP) is complete** — every item above is done.

**Known gap, queued for a future session (2026-09-08):** archiving a
page or space (`internal/pages.Store.Archive`,
`internal/spaces.Store.Archive`) only ever sets `archived_at` — the row
is never destroyed, matching the soft-delete design intent — but there
is currently no way to see or restore what's been archived. Neither
store has an "unarchive"/restore method, there's no
list-archived-items endpoint, and the sidebar has no "Archívum" section
to surface them from. A user asked directly whether archived items are
recoverable and, correctly, would like a fixed Archive area at the
bottom of the sidebar to reopen them from — deliberately deferred
(not urgent, not data-threatening) as a good first candidate for
returning to the pairing workflow (skeleton from me, implementation
from the user) rather than rushed in solo the same night as two other
fixes.

## Database pages (done — 2026-09-20)

Requested 2026-09-20, with reference screenshots of a ClickUp-style
table view (editable columns, a user-defined/colored Status column,
click-a-row-for-a-detail-panel-with-a-rich-text-body). Described as
**"the main thing"** — not a minor add-on. Full design + implementation
notes in [ADR 0004](adr/0004-database-pages.md): a second page `kind`
alongside document pages, where a database's *rows* are themselves
ordinary pages (reusing all of `internal/pages` and `Editor.tsx`
unchanged) and only the columns/cell-values layer (`internal/databases`)
is new. Built in one session, explicitly solo-implemented at the user's
request (overriding the usual pairing split for this one piece — see
DEVLOG 2026-09-20), all three of the originally-scoped stages:

- [x] Database page kind, `text`/`select`/`date` properties, table view
      (`DatabaseView.tsx`), inline cell editing, user-editable Status
      values (add/rename/recolor via a per-cell popover), row create/
      delete (reusing existing page CRUD), row detail view
      (`RowDetailPanel.tsx`, reusing `Editor.tsx` for the body).
- [x] `person`/`number`/`checkbox` property types — `person` is a plain
      display-name string, not a real per-user reference, since Nook
      has no space-membership listing API yet.
- [x] Page cover images (`PageCover.tsx`) — needed real file storage
      that didn't exist, so a new `internal/assets` package + one
      cover-per-page on local disk were built as this work's first
      slice of the Phase 5 "Attachments" item below, not a one-off
      upload path.

A `Popover.tsx` primitive was pulled out of `IconPicker.tsx` along the
way (shared by five popovers total by the end of this work) rather than
copying its position/portal logic a fourth time.

## Security posture (cross-cutting, not tied to one phase)

Recorded 2026-09-02 — the goal stated for this project is a genuinely
production-grade, publicly-reachable self-hosted app (see Phase 6), not
just a local hobby tool, so security is being treated as a continuous
thread through every phase above rather than a single task. Three
things worth being precise about, decided this session:

- **Redis: not adopted, on purpose, not by default-avoidance.** Redis
  earns its place when multiple app instances need to share state
  (rate-limit counters, sessions) across a load balancer. Nook's
  deployment target — this server today, a Raspberry Pi later, a public
  domain after that — is a **single process** the whole way through; an
  in-memory Go map gives the same guarantees Redis would, with one fewer
  moving part to operate and secure. Revisit if/when there's an actual
  plan to run more than one Nook instance at once.
- **NIS2 is a legal/regulatory framework for qualifying organizations**
  (specific sectors, size thresholds, formal risk-management and
  incident-reporting obligations to a national authority) — it is not a
  technical checklist a self-hosted app satisfies through code, and it
  almost certainly doesn't apply, in the legal sense, to a personal
  project run for oneself and a small user base. What *is* being adopted
  here is the same technical discipline NIS2-regulated organizations are
  expected to have — risk-aware design, access control, logging/
  monitoring, vulnerability management, incident-response readiness —
  documented honestly as "built to that standard of care," not marketed
  as "NIS2-compliant."
- **DDoS reality check:** a volumetric flood (bandwidth exhaustion)
  cannot be stopped by anything running in the Nook binary or even a
  reverse proxy on the same box — it saturates the network link before
  any of that code runs. The real answer is a CDN/edge network in front
  once there's a public domain (Phase 6). What Nook's own code *is*
  responsible for, and can actually defend, is application-layer abuse:
  endpoint flooding within normal bandwidth, credential stuffing,
  resource-exhaustion via expensive requests, malformed input — that's
  what the Phase 1 rate-limiting/timeout/validation work targets.

**Requested 2026-09-28: go beyond "too many attempts gets a 429" toward
a genuinely hardened posture** (intrusion, ransomware, data-theft
readiness) before any public launch. Status after the 2026-10-02
hardening pass (ADR 0008) is marked per item:
- [x] *Done 2026-10-02: `auth.AccountGuard`, per-account exponential
  lockout on top of per-IP, `login_locked` events in the log.*
  Smarter brute-force defense than the current flat per-IP token
  bucket: per-*account* lockout/backoff on top of per-IP (so distributed
  low-and-slow credential stuffing against one username still gets
  caught), and a real audit trail of failed-login patterns to act on —
  the login attempt is already logged (Phase 1), this is about *using*
  that log, not just writing it.
- [ ] *One-time scan done 2026-10-02 (`govulncheck`, `npm audit`;
  x/crypto upgraded). Still to do: make it recurring.*
  Automated dependency vulnerability scanning as a standing check
  (`go list -m -u all` / `npm audit` are already named as a one-time
  pre-launch gate in Phase 6 — this is making that a recurring/CI habit,
  not a single pre-launch checkbox).
- [ ] *Built 2026-10-02: `nook backup` + nightly cron via the container, restore
  procedure in DEPLOY.md. Still to do: the first real test restore and
  the offline copy habit.*
  Backup/restore drilled for real, including the ransomware scenario
  specifically: an *offline or immutable* backup copy (a live-disk
  backup a ransomware process can also encrypt/delete doesn't count),
  and an actual documented/tested restore procedure, not just "the
  files exist somewhere."
- [ ] *Absolute 90-day session lifetime done 2026-10-02. Rotation on
  privilege change not reviewed yet.*
  Session/cookie hardening review (rotation on privilege change,
  absolute session lifetime, not just the existing `SameSite=Lax` +
  httpOnly baseline from ADR 0003).
- [ ] *Incident-response steps written (DEPLOY.md, "If something looks
  wrong"). Reporting contact still missing.*
  A real `SECURITY.md` vulnerability-reporting contact (already named
  in Phase 6's pre-launch checklist) plus a basic incident-response
  note: what "we got popped" actually triggers (rotate secrets, which
  logs to pull, how to tell what was accessed).
- Worth a dedicated pass (possibly its own ADR) once Phase 6 packaging
  exists — hardening a not-yet-Dockerized, not-yet-publicly-reachable
  app is premature optimization; sequence matters here.

## Phase 2 — Multi-user

- Multiple accounts, private space per user, shared spaces
- Space membership management (add/remove users)
- Owner/Admin/Editor/Viewer enforcement throughout
- Basic user management (self-registration with configurable mode — see ADR 0005)
- **Accounts, registration, email, profiles, activity** — design in
  [ADR 0005](adr/0005-accounts-invites-profiles.md). Schema landed as
  `0011_accounts.sql` + `0012_hashed_sessions.sql`. All steps done
  2026-09-28 (implemented outright at the user's request):
  - [x] 1. `users`: new fields + scan, email/username normalization and
        validation, `List`, status/admin/profile/password updaters;
        shared password validator in `auth`
  - [x] 2. Login by username or email; `RequireAuth` rejects non-active
        users; `RequireAdmin`; `/api/me` extended; `GET /api/auth/config`
  - [x] 3. `POST /api/auth/register` with `NOOK_REGISTRATION` modes
        (open first; approval/invite/closed right after)
  - [x] 4. Profile: `PUT /api/me`, `PUT /api/me/password`
  - [x] 5. `internal/authtokens` + `internal/mail` (log impl first,
        SMTP after) → verify email, forgot/reset password
  - [x] 6. Admin panel endpoints: users (approve/disable/promote/reset
        link), invites; last-admin guard
  - [x] 7. Group "invite" → add existing user by username
  - [x] 8. `internal/activity` + `pages.updated_by` writes; activity
        endpoints
  - [x] 9. Sessions: hashed IDs, metadata, sliding expiry, list/revoke
  - [x] 10. Frontend: sign-up/forgot/reset/verify screens, Settings modal,
        "last edited by" + activity panel, GroupMembers user picker
- **2FA (TOTP)** — RFC 6238 time-based codes, `github.com/pquerna/otp`
  (no external service needed, just a per-user secret stored encrypted).
  Natural fit here since it's a per-account setting and this phase is
  already touching account management.
- **Optional SMTP for password reset** — email stays opt-in and
  admin-configured, not a required field or a login method; if no SMTP
  is set up, password resets stay an admin/CLI operation against the DB,
  same as account creation today. *Superseded 2026-09-27 by ADR 0005:*
  email is now part of the account; SMTP stays optional, the app
  degrades to admin-generated reset links without it.

## Phase 3 — Realtime

- [x] WebSocket transport — done 2026-09-28, [ADR 0006](adr/0006-realtime-collaboration.md)
- [x] Yjs (via Tiptap's y-tiptap), one Y.Doc per page, durable log +
      snapshots in SQLite
- [x] Presence (carets, avatars), conflict-free merging, ack-based
      no-data-loss sync
- [ ] Live sidebar: page titles/icons/tree changes pushed to others
- [ ] Live database cells

## Phase 4 — Search & structure

- [x] Page tree drag & drop (fractional-indexing `position` scheme) -- done with the sidebar refactor (2026-09-27)
- [x] SQLite FTS5 over page title + extracted plain text from `content_json`
      (+ database text/number/select cells) -- [ADR 0007](adr/0007-sidebar-search.md)
- [x] Sidebar search UI (results list, highlight, breadcrumbs, ⌘K)
- [ ] Global command palette (actions, not just navigation)

## Phase 5 — Polish

- Keyboard shortcuts, UI refinement
- Markdown export, JSON export
- [x] Database backup/restore (2026-10-02): `nook backup`, see DEPLOY.md
- Attachments (self-hosted file storage, no S3 requirement) — a narrow
  first slice of this already exists (`internal/assets`, one cover
  image per page, see "Database pages" above); this item is growing
  that into general multi-file attachments, not starting from scratch
  - [x] Images inside page text (2026-09-28): paste/drop/pick, stored
        in `page_images` + the uploads dir, referenced by URL
  - [ ] Sweep unreferenced images: files whose image node was deleted
        from the page (kept for now so undo still works), and files of
        pages deleted for good. Needs a job that walks `content_json`.
  - [ ] Per-user or per-instance storage quota, surfaced in the admin
        panel
- Error handling, logging, basic monitoring

## Phase 6 — Deployment

The intended validation path (stated 2026-09-02): this server first,
untouched functionality with real multi-user/shared-space usage, *then*
move to a Raspberry Pi as the "just `docker run` and it works" test,
*then* — only once that's solid — expose it publicly via a domain for
actual daily use in place of Notion. The items below assume that order;
the last two are gates before the public-domain step specifically, not
before the Pi move.

**Domain: `nook.example.com`, registered at your registrar**
(`nook.hu` turned out to be taken). Nook serves the apex,
`https://nook.example.com`. Earlier note, kept for history --
**Domain (tentative, decided 2026-09-22): `nook.hu`.** Not registered
yet on purpose — availability wasn't confirmed (one WHOIS check
suggested it might already be taken, another couldn't look up `.hu` at
all; needs a real check at
[domain.hu/domain-kereses](https://www.domain.hu/domain-kereses/), the
authoritative source). Nothing to act on until the public-domain step
above is actually reached — check availability and register for real
then, not before. `nook.app` was the discussed fallback if `.hu` turns
out to be taken (also gets HTTPS enforced by the TLD itself, a nice
fit for the Phase 6 TLS/security items below).

**Brought forward 2026-10-02:** going public now happens on the current
server, before the Pi/Docker steps -- see ADR 0008 and DEPLOY.md. The
tunnel + Access setup moves to the Pi unchanged later.

- Production build pipeline
- [x] Dockerfile + docker-compose (2026-10-02): `scripts/build-image.sh`,
  `deploy/docker-compose.yml`; Nook runs from it at home. Still open:
  multi-arch (ARM64) builds for the Raspberry Pi.
- Native (no-Docker) run path: dropped for now, Docker is the one
  supported way (a systemd draft existed for a few hours on 2026-10-02)
- README + install docs for a GitHub release
- [x] *Superseded by ADR 0008: Cloudflare Tunnel terminates TLS, no
  reverse proxy on the box.* **Recommended reverse-proxy setup documented** (e.g. Caddy) for TLS,
  connection limits, and timeouts — this is the baseline for exposing
  Nook to any network beyond localhost/LAN, Raspberry Pi included.
- [ ] *Planned in ADR 0008 (Cloudflare edge via the tunnel); set up by
  the user per DEPLOY.md.* **Before going public on a domain — CDN/edge layer in front (e.g.
  Cloudflare's free tier).** This is the actual defense against
  volumetric DDoS: no amount of application code can absorb a bandwidth
  flood, since it saturates the connection before ever reaching Nook —
  that's what a CDN/edge network in front is for. Not optional once
  there's a public domain in play; the app-level rate limiting (Phase 1)
  handles application-layer abuse (credential stuffing, endpoint abuse),
  it does not and cannot substitute for this.
- [ ] *Now the "Final checks" list in DEPLOY.md.* **Pre-public-launch checklist** (gate before the domain goes live, not
  before the Pi move): TLS working end-to-end, rate limiting verified
  live (not just unit-tested), backup/restore actually tested (not just
  implemented), dependencies up to date (`go list -m -u all`, `npm
  audit`), `NOOK_COOKIE_SECURE` correctly `true`, `SECURITY.md` in place
  with a real vulnerability-reporting contact.

## Phase 7 — Path to a real self-hosted release

Requested 2026-09-28, after the sidebar/groups work landed and the app
became genuinely usable day-to-day at home ("ez már használható
itthon"). Six items given directly, plus what I'm adding from what I
can see in the repo. Status (2026-10-02): password reset, English UI
and the bundle code-splitting are done; mobile has a first pass; the
backend review, the instance name and Docker are not started.

- [x] *Done 2026-09-28 (ADR 0005): admin-generated reset links with no
  mail config, SMTP email when configured.* **Self-service password reset, setup-and-use for self-hosting.**
  `internal/authtokens` + `httpapi/accounts.go` already exist (a reset
  *token* mechanism is there) but nothing sends the token anywhere —
  no SMTP wiring, so today a reset token has no delivery path. Given
  Phase 2's own SMTP note (optional, admin-configured, degrades to an
  admin-generated link without it), the "setup and use" bar for a
  self-hoster is: works with zero mail config out of the box (an admin-
  facing "generate a reset link" path, e.g. a CLI flag or an admin-only
  endpoint), *and* also supports real email if the self-hoster bothers
  to set SMTP env vars. Neither half exists yet.
- **Backend review/refactor pass for speed and stability.** Not yet
  audited with this specific lens this session — a dedicated pass
  (likely per-package: `pages`, `spaces`, `groups`, `httpapi`) makes
  sense once Phase 2/3 settle down, rather than mid-flight.
- [ ] *Code-splitting done 2026-09-28; jank pass partly done.* **Frontend "smooth and fluid" pass.** One concrete, already-visible
  data point: every `npm run build` this session has warned that
  `index-*.js` is a single ~1.58MB chunk (over Vite's 500KB guidance) —
  worth a real code-splitting pass (route-level or heavy-component
  lazy-loading, e.g. the icon picker's full lucide set, `DatabaseView`,
  the editor) rather than shipping the whole app as one bundle on first
  load. Beyond bundle size: an actual pass for jank (drag-and-drop
  frame timing, re-render scope on every keystroke/drag event) once a
  real browser is available for profiling (see DEVLOG 2026-09-27 for
  how that got unblocked in this environment).
- [x] *Done 2026-09-28.* **Full English UI.** Every user-facing string across every component
  is hardcoded Hungarian today ("Keresés", "Átnevezés", "Törlés",
  "Kijelentkezés", ...) — there is no i18n library or string-table
  layer in the frontend at all (`grep`-confirmed: no `react-i18next`/
  `i18next` dependency). This is a real, mechanical, cross-cutting job:
  either (a) a one-time straight swap to English strings in place, or
  (b) introduce an actual i18n layer (string table + a language
  switcher) if Hungarian should remain available too — worth deciding
  which *before* starting, since (a) is much less work but forecloses
  (b) later without redoing it. Given the user hasn't said Hungarian
  needs to stay available, (a) is the likely intent, but worth
  confirming once this phase is actually picked up. **Decided
  2026-09-28: option (a), straight swap, no i18n layer.**
- **Mobile-responsive frontend.** *First pass done 2026-09-30 (see
  DEVLOG): drawer sidebar, touch-visible controls, phone layout for
  editor/database/dialogs. Still open: touch reordering (drag-and-drop
  replacement), real-device Safari testing, PWA manifest.* Requested 2026-09-28. Confirmed
  ~zero mobile support today (`grep`: exactly one width breakpoint in
  the whole frontend, in `Settings.css`) — everything else assumes a
  fixed desktop side-by-side sidebar+workspace layout. Two things make
  this more than plain responsive CSS: (1) the hover-reveal UI pattern
  used throughout the sidebar (grip, "⋮" menu, "+" buttons) has no
  touch equivalent — there's no hover state on a touchscreen; (2) the
  HTML5 drag-and-drop the whole sidebar reorder/move system is built on
  is notoriously unreliable on touch — mobile likely needs a different
  interaction for reordering (long-press menu, or promoting the
  existing "Move to..." popover to the primary touch path instead of a
  drag-only fallback). Own line item, not folded into the smoothness
  pass above.
- **A real instance name instead of `ip:port`.** Wanting
  `nook.online:8000`-style access without a public domain means local
  DNS/hostname resolution, not routing code — nothing in Nook itself
  currently owns a "name" concept. Realistic self-hosted options: mDNS/
  Bonjour (`nook.local`, zero-config on the same LAN, no internet
  dependency), or documenting `/etc/hosts` / router-level local-DNS
  setup for a custom name like `nook.online` pointed at the LAN IP.
  True public-DNS `nook.online` is a different, later thing tangled up
  with the Phase 6 domain/TLS items above — worth being explicit with
  the user about which one they mean when this is picked up.
- **Docker packaging** — already scoped above (Phase 6: "Dockerfile +
  docker-compose, multi-arch ARM64/Raspberry Pi"); flagged again here
  since it's the concrete "final wrapper" the user's asking for. No
  `Dockerfile`/`docker-compose.yml` exists in the repo yet
  (`find`-confirmed). Natural to build once the single-binary build
  (Phase 1, already done) and the SQLite data directory/volume story
  are both settled — should land *after* the password-reset and backend
  pass above, not before, so the image being built is the stable one.

## Noted for later (not yet scoped into a phase)

- [x] *Done 2026-09-20 as database pages (ADR 0004).* **Structured page properties / database-style table views**, e.g. a
  page list with Status/Priority/Ticket columns, filterable views
  ("By Status", "Checklist", "My Tasks" — the shape of Notion's
  "databases"). Raised 2026-09-02 (DEVLOG) against a real Notion
  workspace as a reference. This is bigger than sidebar/icon styling: it
  needs its own data model (per-space or per-page-tree property schema,
  typed values, view definitions) on top of what `pages` currently has —
  not just Phase 4/5 frontend polish. Needs a proper design pass (own
  ADR) before starting; not committed to a phase yet.
- [x] *Done (page icons, emoji + Lucide).* Per-page icons (small, non-breaking schema addition — an `icon` column
  on `pages` — cheap to add whenever the sidebar work starts).
- Notion's `Page`/`Page in`, `Toggle heading`, `Callout`, `Columns`,
  `Synced block` turn-into options — each needs a genuinely new custom
  Tiptap node type, not just wiring an existing command. Raised
  2026-09-02 (DEVLOG 22:57) against the same Notion screenshot as the
  database/table-view note above; deliberately deferred, not dropped.
