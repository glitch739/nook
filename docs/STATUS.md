# Status — 2026-10-11

A short snapshot. The detail lives in [ROADMAP.md](ROADMAP.md).

## Live

Running at my own domain since 2026-10-03, behind Cloudflare Tunnel +
Access (ADR 0008), with nightly `nook backup` and upload sweep.

- Pages with a collaborative editor (Yjs over WebSocket): text, lists,
  to-dos, code, tables, images, file attachments, covers, icons.
- Database pages (text, number, date, checkbox, select, person columns)
  and collections.
- Groups → spaces → page tree, drag and drop plus Move up/down, archive
  with restore, Markdown/ZIP/JSON export.
- Accounts: registration modes, invites, email verification, password
  reset, sessions, admin panel, roles (viewer/editor/admin/owner).
- Live sidebar and database updates (SSE), search (FTS5), command
  palette, tab restore, themes, phone layout, installable as a web app.

## Fixed today

Backend (from a full audit):

- Moving a page to another space now moves its whole subtree.
- Moving a space needs owner, like archiving it.
- The upload sweep checks every page before deleting an image.
- Open live-editing connections re-check access every 25 seconds and
  close when the session, account, role or page changes.
- Pages in an archived space no longer open by direct link.
- Changing your email needs your current password.
- Cover uploads are checked by their bytes; the client-address logic
  behind the tunnel has a test.

Frontend:

- Sidebar crash after a refused page move; reorder no longer sent twice.
- Read-only and not-yet-seeded pages can't be changed from the toolbar.
- Tabs of archived sub-pages close; no-op renames, icon picks and cell
  saves are skipped.
- The UI follows your role: viewers don't get edit controls.
- Errors show as toasts; archiving offers Undo.
- Unsaved page text is kept in IndexedDB and resent after a reload.
- Service worker for the app shell; an offline screen instead of an
  endless "Loading…".
- Database sort and filter; column widths remembered.
- Keyboard navigation in menus; sidebar page titles are real buttons;
  "Archive" instead of "Delete" for spaces and groups.
- The database sort/filter bar sits above the table, styled like the
  sidebar search.

## Still open

- **Deferred:** TOTP (Cloudflare Access is the second factor for now).
- **From the backend audit:** off-machine backups (destination to pick),
  account setup in one transaction, a one-off check for pages split
  across spaces before the move fix.
- **Needs a decision:** page history (retention, restore vs live
  editing), shared saved database views.
- **Mobile:** real-device testing (iOS Safari), toolbar above the
  on-screen keyboard, a phone layout for databases, a visual check of the
  maskable icon. Reading pages fully offline would need cached API data.
- **Not started:** comments and @mentions, notifications, per-page
  sharing and public links, import, avatars, storage quotas, metrics,
  virtualized rows for very large databases.
- From earlier lists: publishing on GitHub, the Raspberry Pi target.
