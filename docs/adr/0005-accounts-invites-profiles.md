# ADR 0005: Accounts, self-registration, email, profiles, activity

Status: Accepted — implemented 2026-09-28 (revised 2026-09-27: public
sign-up + email-based reset replace the invite-only first draft)

## Context

Phase 2 makes Nook a normal multi-user app: anyone reaching the login
page can sign up, log in, reset a forgotten password, and work with
others in shared groups, seeing who changed what. Accounts are tied to
an email address. The address lives only in this server's SQLite DB.
Nook is used on the home LAN first and later exposed to the internet,
so every decision below must hold up in the public case too.

Today accounts come only from `bootstrapAdmin` and from
`POST /api/groups/{id}/members`, where any group admin creates an
account with a password they type for the other person. There's no
instance-level admin, no profile editing, no session management and no
account disabling. Session IDs sit in the DB unhashed.

This revises the roadmap's original "no public sign-up, no email
required" goal at the user's request.

## Decision

### 1. Registration modes

`NOOK_REGISTRATION` controls the sign-up form on the login page:

| Mode | Behavior | Intended for |
|---|---|---|
| `open` (default) | Sign-up creates an `active` account immediately | Home LAN |
| `approval` | Sign-up creates a `pending` account; an instance admin approves it | Internet exposure |
| `invite` | Sign-up needs an invite link from an admin | Internet, stricter |
| `closed` | No sign-up; admins create accounts | Lockdown |

The frontend reads the mode from a public `GET /api/auth/config` and
shows or hides the "Regisztráció" link accordingly.

Sign-up fields: email, username, display name, password. The account
gets its personal group + personal space (existing behavior), then is
logged in, unless it's `pending`.

### 2. Email

- Required for new accounts and unique, stored lowercased. Login
  accepts username **or** email.
- Stored only in Nook's DB. The API never returns another user's email.
  Group member lists show username + display name only.
- Sending mail needs an SMTP relay: `NOOK_SMTP_HOST`, `_PORT`, `_USER`,
  `_PASS`, `_FROM`. Whatever relay is configured sees the recipient
  address of each message it delivers. That's unavoidable for email.
  The address is never shared with anyone else.
- **No SMTP configured**: the app still works. Emails are unverified,
  "forgot password" shows "ask your administrator", and the admin
  generates a reset link from the admin panel. With `NOOK_DEV_MAIL=log`
  outgoing mail is written to the server log instead, for LAN testing.
- **SMTP configured**: sign-up sends a verification link. Unverified
  accounts can log in, but password reset by email only works for
  verified addresses. That keeps someone from registering your address
  and then receiving your reset links.

### 3. Forgot password

Login page → "Elfelejtett jelszó" → enter email → always the same
response ("if an account exists, we sent a link"), so it can't be used
to probe which addresses have accounts. Rate-limited per IP and per
address. The link is valid for 1 hour and single use. Using it sets the
new password and revokes all of that user's sessions.

### 4. One-time tokens

One `auth_tokens` table for `verify_email`, `reset` and `invite`: 32
random bytes, base64url, only `sha256(token)` stored, expiry + `used_at`.
Consumed inside the same transaction as the action it authorizes.

### 5. Roles

- **Instance role** `users.is_admin`: approve/disable accounts, promote
  admins, generate reset links, create invites. The bootstrap account is
  admin. The last active admin can't be demoted or disabled.
- **Group/space roles** (owner/admin/editor/viewer) are unchanged and
  govern content only. Group admins **stop creating accounts**. The
  group "invite" becomes "add an existing user by username", so
  people sign up themselves and then get added to shared groups.

### 6. Account status

`users.status`: `pending` → `active` → `disabled`. Login for a
non-active account fails with a generic message (pending says "awaiting
approval" only *after* a correct password). `RequireAuth` rejects
sessions of non-active users, and disabling deletes their sessions.
Content is never deleted with the account.

### 7. Sessions

- The cookie carries the raw token. `sessions.id` stores `sha256(token)`.
  A follow-up migration wipes existing sessions when this switches over.
- `last_seen_at`, `user_agent`, `ip` → "Active sessions" in the profile,
  with per-session revoke and "sign out everywhere else".
- Sliding expiry: `expires_at` is pushed forward at most once a day.
  The TTL stays 30 days.
- Password change revokes the other sessions. Reset and disable revoke
  all of them.

### 8. Profile

Self-service in a Settings modal: display name, email (change → new
verification), password (requires the current one), active sessions.
Avatar and TOTP come later in Phase 2. Usernames are immutable,
lowercase, `^[a-z0-9._-]{3,32}$`.

Passwords must be 8–256 characters, with no composition rules
(NIST 800-63B). One shared validator lives in `internal/auth`.

### 9. Who changed what

- `pages.updated_by` → "Last edited by X, 5 min ago" in the page header.
- `activity` table, append-only: `actor, space, page, action, meta`.
  It's written by the handlers (page create/edit/move/archive, database
  row edits, member changes). Editor autosaves are coalesced: one
  `page.edit` per user per page per 10 minutes, so typing doesn't flood
  the feed.
- UI: a per-page "Activity" panel and a per-space feed.
- Not in this step: content diffs and restoring old versions. That's a
  `page_revisions` snapshot table later, or it falls out of Yjs in
  Phase 3.

### 10. Internet exposure checklist

These are gates before the public-domain step in Phase 6, not before
LAN use:

- TLS via a reverse proxy (Caddy, automatic certificates),
  `NOOK_COOKIE_SECURE=true`, HSTS.
- `NOOK_REGISTRATION=approval` or `invite`.
- SMTP configured, so email verification and reset actually work.
- TOTP 2FA available, at least enforced for admins.
- Rate limits on login, sign-up, forgot-password and token endpoints.
  Login already has one.
- Trusted-proxy config so rate limits see the real client IP, not the
  proxy's.
- Encrypted off-box backups of `nook.db`, since it now holds emails.
- A CDN or tunnel in front for DDoS (see the deployment notes), which
  also avoids opening home router ports.

## API sketch

```
GET    /api/auth/config                → {registration, smtp_enabled}          public
POST   /api/auth/register              {email, username, display_name, password, invite_token?}
POST   /api/auth/verify-email          {token}
POST   /api/auth/forgot                {email}                                 always 202
POST   /api/auth/reset                 {token, password}
POST   /api/auth/login                 {login, password}                       login = username or email

GET    /api/me                         (+ email, email_verified, is_admin, must_change_password)
PUT    /api/me                         {display_name?, email?}
PUT    /api/me/password                {current_password, new_password}
GET    /api/me/sessions
DELETE /api/me/sessions/{id} | DELETE /api/me/sessions

GET    /api/admin/users
PUT    /api/admin/users/{id}           {status?, is_admin?}
POST   /api/admin/users/{id}/reset     → {url}
GET|POST|DELETE /api/admin/invites

GET    /api/pages/{id}/activity
GET    /api/spaces/{id}/activity
```

## Consequences

- Schema: `0011_accounts.sql`, plus `0012_hashed_sessions.sql` (wipes
  sessions for the hash switch).
- New config: `NOOK_REGISTRATION`, `NOOK_SMTP_HOST/PORT/USER/PASS/FROM`
  (port 465 = implicit TLS, otherwise STARTTLS is required),
  `NOOK_PUBLIC_URL` (base for links in emails; required with SMTP),
  `NOOK_DEV_MAIL=log`, `NOOK_TRUSTED_PROXIES`.
- Implementation notes: admin-generated links (reset, invite) are
  returned as paths and completed with the browser's origin in the UI.
  Invites skip approval mode. Group "add member" takes a username only
  (not an email, so it can't be used to map emails to accounts).
- New packages: `internal/authtokens`, `internal/mail` (an interface
  with SMTP and log implementations), `internal/activity`.
- Frontend: sign-up / forgot / reset / verify screens next to the
  login form, a Settings modal (Profile / Security / Admin), a
  "last edited by" line and activity panel, and the GroupMembers dialog
  becomes a user picker.
