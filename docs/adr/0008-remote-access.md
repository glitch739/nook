# ADR 0008: Remote access — Cloudflare Tunnel + Cloudflare Access, app hardened for the internet

Status: Accepted — 2026-10-02

## Context

Until now Nook was reachable only on the home LAN
(`http://<server-ip>:8080`). The user decided (2026-10-02) that this
blocks real use: notes you can't reach from your phone away from home
aren't worth writing. Nook goes on the internet now, ahead of the
Raspberry Pi / Docker steps in ROADMAP Phase 6, with "every reasonable
security step" taken first.

Constraints from earlier decisions:

- Single process, no extra services in the app itself (ADR 0001).
- A volumetric DDoS can't be stopped by app code; a CDN/edge must sit
  in front before any public domain (ROADMAP, "Security posture").
- The server is a home machine. Exposing the home IP or opening router
  ports widens the attack surface to the whole box (SSH, other
  services) and to the home network behind it.

## Options considered

1. **Port forwarding + reverse proxy (Caddy, Let's Encrypt).** Classic
   and fully self-owned. But it opens a port on the router, publishes
   the home IP, puts no edge in front (so no DDoS absorption, against
   the requirement above), and depends on the ISP giving a reachable
   public IPv4 (often CGNAT on Hungarian residential lines).
2. **VPN only (Tailscale / WireGuard).** The safest: nothing is public
   at all. But every device needs the VPN app connected, it doesn't
   work from a borrowed computer, and sharing with a non-technical
   person means installing and explaining a VPN. It also isn't the
   "Nook on the internet" the user asked for.
3. **Cloudflare Tunnel + Cloudflare Access.** `cloudflared` on the
   server makes an *outbound* connection to Cloudflare; the edge serves
   `https://notes.<domain>` and forwards through it. No router port, no
   published home IP, TLS and DDoS absorption at the edge, free tier.
   Cloudflare Access adds an identity check (email one-time code, or an
   SSO provider) in front of the whole hostname, so only listed people
   ever reach Nook's own login.

## Decision

Option 3. Concretely (full steps in [docs/DEPLOY.md](../DEPLOY.md)):

- Nook runs as a Docker container (`nook:latest`, built from the repo)
  with a read-only filesystem, no capabilities and no privilege
  escalation; data in `~/applications/nook/data`. Its port is
  published on `127.0.0.1` only, since Docker-published ports bypass
  ufw. (First drafted as a systemd service; switched to Docker the
  same day at the user's request: source in `~/src/nook`, the running
  instance in `~/applications/nook`.)
- `cloudflared` runs as a second container in the same compose project
  and reaches Nook as `http://nook:8080`; Nook trusts
  `X-Forwarded-For` only from its fixed address.
- The tunnel is dashboard-managed; only its token lives on the server
  (in `.env`).
- A Cloudflare Access application covers the hostname: allow-list of
  email addresses, one-time PIN login, 1-month Access session.
- Nook's own login stays fully in force behind it. Two independent
  gates: compromising one doesn't open the other.

App-side changes made for this (all on by default where it's safe):

- **Per-account login throttling** (`auth.AccountGuard`): 5 free wrong
  passwords per account name, then 1, 2, 4 … up to 30 minutes locked.
  The per-IP limiter can't see a distributed guess spread across many
  IPs; this can. Same answer for real and nonexistent names.
- **Absolute session lifetime** of 90 days on top of the 30-day sliding
  expiry, so a stolen cookie can't be kept alive forever by use.
- **Cross-site write rejection** (`sameOriginWrites`): state-changing
  requests whose `Sec-Fetch-Site` isn't `same-origin` (or whose
  `Origin` doesn't match) get 403. Closes the gap ADR 0003 left open
  for multipart uploads and for sibling subdomains.
- **HSTS, Permissions-Policy, COOP** headers; HSTS only when
  `NOOK_PUBLIC_URL` is https, never on the plain-HTTP LAN setup.
- **Server timeouts** (`ReadHeaderTimeout`, `IdleTimeout`,
  `MaxHeaderBytes`) instead of `http.ListenAndServe`'s none; graceful
  shutdown on SIGTERM.
- **Safe defaults for public URLs**: with an https `NOOK_PUBLIC_URL`,
  registration defaults to `approval` (anyone may sign up, an admin
  approves each account before it can log in; it stays `open` only on
  a LAN/dev setup, or if set explicitly, with a warning), and
  `NOOK_COOKIE_SECURE=false` refuses to start.
- **`nook backup`**: consistent snapshot (SQLite `VACUUM INTO`) of the
  database plus uploads into one `.tar.gz`, with pruning; cron runs it
  daily through the container. Offline/off-machine copies are a documented
  manual step.
- Dependencies scanned (`govulncheck`, `npm audit`);
  `golang.org/x/crypto` upgraded past two advisories.

## Consequences

- **Cloudflare sees the traffic.** TLS ends at Cloudflare's edge, so it
  can technically read requests, notes included. Accepted for now in
  exchange for no open ports and DDoS protection; revisit (e.g. a
  Tailscale-only mode) if that trade stops being acceptable.
- **Dependency on Cloudflare's free tier.** If it changes, the app is
  unaffected; only the access path changes (options 1 or 2 remain).
- **LAN access goes through the internet too**, since Nook no longer
  listens on the LAN address. Simpler and one consistent URL; slightly
  more latency at home.
- **Access lockout risk:** losing the email account on the allow-list
  means losing remote access until the policy is edited in the
  Cloudflare dashboard. The server itself stays reachable on the LAN
  (SSH), so this is recoverable.
- **Account-lock nuisance:** someone who knows a username could keep it
  locked for up to 30 minutes at a time. With Access in front,
  strangers can't reach the login form, so this needs an allow-listed
  attacker.
- The Raspberry Pi / Docker step (ROADMAP Phase 6) is unaffected: the
  same tunnel and Access setup moves with the app.

## Amendment — 2026-10-02: approval instead of invite-only

Changed at the user's request the same evening: sign-ups on a public
instance default to `approval`, not `invite`. Anyone can register; the
account stays "awaiting approval" and cannot log in until an admin
approves (or rejects) it. Admins see a "N waiting" badge in the tab bar
(polled every minute and on window focus) that opens Settings → Users.
The user's reasoning: invite links are friction, and with Cloudflare
Access in front only allow-listed emails reach the sign-up form at all.

