# ADR 0003: CSRF strategy — SameSite=Lax + JSON-only API, no CSRF token

Status: Accepted — 2026-09-02

## Context

Nook authenticates via a session cookie (`internal/auth`). Cookie-based
auth is the classic setup CSRF protection exists for: a malicious page on
another origin gets the browser to fire a request at Nook, the browser
attaches the session cookie automatically, and the request executes as
the logged-in user without them intending it.

This had been left implicit rather than decided. Writing it down now,
while the API surface is still small, rather than after CSRF tokens
would be a bigger retrofit.

## Decision

No separate CSRF token. The protection is two properties already true of
the API, held together deliberately rather than by accident:

1. **`SameSite=Lax` on the session cookie** (`internal/auth/http.go`).
   A cross-site `POST`/`PUT`/`DELETE` (`fetch`, `XMLHttpRequest`, a
   cross-origin `<form>` submit) does not attach a `SameSite=Lax`
   cookie. Only top-level navigations (a real link click, typing a URL)
   still send it — and those are `GET`s, which Nook's API never treats
   as a mutation.
2. **JSON-only request bodies, `Content-Type: application/json`.**
   `LoginHandler`/`createSpaceHandler` (and everything after them)
   decode JSON, not form-encoded bodies. A plain cross-origin `<form>`
   submission can't set an arbitrary `Content-Type` to `application/json`
   without triggering a CORS preflight — which Nook doesn't answer with
   an `Access-Control-Allow-Origin` for any origin — so even a same-site
   navigation quirk that got a cookie through still couldn't produce a
   body the handler would accept.

Together: no cross-origin request can both carry the session cookie
*and* arrive as a JSON body Nook will parse.

## Consequences

- If Nook ever accepts a plain HTML form POST (`application/
  x-www-form-urlencoded` or `multipart/form-data` — the likely case is a
  future file-upload endpoint), this protection does not cover it. Forms
  submit fine cross-origin and do carry `SameSite=Lax` cookies for the
  navigation case in some browsers' interpretations. That endpoint needs
  its own explicit CSRF token (or an upload flow that doesn't rely on
  form-post + cookie auth, e.g. a short-lived signed URL) — revisit this
  ADR when file uploads (ROADMAP, Phase 5) land, don't assume the
  current protection extends to it silently.
- If a client ever needs `SameSite=None` (a legitimate embedded/
  cross-site use case), this protection stops applying and a real CSRF
  token becomes necessary at that point.
- This isn't "no CSRF work ever" — it's "no CSRF token needed for the
  API surface that exists today," recorded so the next endpoint that
  changes the shape gets checked against it explicitly instead of the
  assumption just carrying over unexamined.
