# Security

Nook is young software (see [docs/ROADMAP.md](docs/ROADMAP.md)). This
document describes the current security posture
honestly, including what isn't built yet, rather than overstating it.

## Reporting a vulnerability

Please open a private report rather than a public GitHub issue for
anything that looks like a real vulnerability (not just a hardening
suggestion). Until this project has a dedicated security contact, open
an issue asking to be pointed to one, or reach the maintainer through
the repository's listed contact — do not post exploit details publicly
before that conversation happens.

## What's implemented

- Passwords: argon2id, salted, constant-time verification
  (`backend/internal/auth/password.go`).
- Sessions: server-side, random (crypto/rand-backed) tokens, stored
  only as sha256 hashes (a leaked DB/backup holds no usable cookies),
  30-day sliding expiry, listable/revocable per device; password
  change/reset and account disabling revoke sessions. `HttpOnly`,
  `SameSite=Lax` cookies; `Secure` is configurable
  (`NOOK_COOKIE_SECURE`, defaults `true` — only disable for local
  plain-HTTP development, never in a real deployment).
- Login responses are deliberately indistinguishable between "wrong
  password" and "username doesn't exist" (same status, same body,
  same computational cost) to resist username enumeration.
- Registration mode is configurable (`NOOK_REGISTRATION`: `open` /
  `approval` / `invite` / `closed`, ADR 0005). Use `approval` or
  `invite` for any internet-facing instance.
- One-time links (email verification, password reset, invites) are
  single-use, expiring, and stored only as sha256 hashes. "Forgot
  password" always answers the same way and sends asynchronously, so it
  can't be used to discover which addresses have accounts; it only
  sends to verified addresses. Emailed links are built only from
  `NOOK_PUBLIC_URL` (never the request's Host header).
- Email addresses are never returned to other users by the API.
- `X-Forwarded-For` is only trusted from `NOOK_TRUSTED_PROXIES`.
- Login throttling at two levels: per IP (token bucket) and per account
  name (5 free failures, then an exponential lock from 1 up to 30
  minutes), so guesses spread over many IPs are still slowed down.
- Sessions also end 90 days after login, however actively used.
- State-changing requests that the browser marks as cross-site
  (`Sec-Fetch-Site`, or a foreign `Origin`) are rejected, in addition
  to the `SameSite=Lax` + JSON-only design of ADR 0003.
- Response headers: CSP (same-origin only), `frame-ancestors 'none'`,
  `nosniff`, `Referrer-Policy`, `Permissions-Policy`, COOP, and HSTS
  when served over https.
- Server timeouts against slow-client resource exhaustion; request
  body size limits per route; per-request processing timeout.
- With an https `NOOK_PUBLIC_URL`, unsafe settings are refused
  (`NOOK_COOKIE_SECURE=false`) or default safely (new accounts need an
  admin's approval before they can log in, unless set otherwise).
- `nook backup`: consistent database + uploads snapshots; the
  recommended deployment runs it daily.
- Authorization is centralized, not scattered per-handler (in progress —
  see `internal/permissions` in the roadmap): every space/page access is
  meant to go through one membership check, not an ad hoc one per route.

## What's not implemented yet (see ROADMAP.md for the plan)

- **2FA** — planned, not built yet. On the recommended deployment,
  Cloudflare Access (email one-time code) acts as the extra gate until
  then.
- **DDoS protection.** Nook's own code can resist
  application-layer abuse. It cannot, and no self-hosted application
  code can, absorb a volumetric network flood — that requires a CDN/edge
  network in front. The recommended deployment
  ([docs/DEPLOY.md](docs/DEPLOY.md), ADR 0008) uses Cloudflare's edge
  through a tunnel for exactly this.

## Scope note

This is a self-hosted project you run yourself. Nook's own security
posture matters, but so does the environment you run it in: keep the
host OS patched, don't expose the SQLite database file or its directory,
use a real TLS certificate (not `NOOK_COOKIE_SECURE=false`) for anything
beyond local development, and put a reverse proxy / CDN in front before
exposing it to the public internet.
