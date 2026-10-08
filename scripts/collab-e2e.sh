#!/usr/bin/env bash
# Runs frontend/e2e/collab.e2e.ts against a throwaway Nook server.
set -euo pipefail
cd "$(dirname "$0")/.."
TMP=$(mktemp -d)
PORT=${PORT:-18099}
(cd backend && go build -o "$TMP/nook" ./cmd/nook)
NOOK_ADDR=127.0.0.1:$PORT NOOK_DB_PATH="$TMP/nook.db" NOOK_UPLOADS_DIR="$TMP/uploads" NOOK_COOKIE_SECURE=false \
  "$TMP/nook" >"$TMP/server.log" 2>&1 &
PID=$!
trap 'kill $PID 2>/dev/null; rm -rf "$TMP"' EXIT
for _ in $(seq 50); do curl -sf "http://127.0.0.1:$PORT/healthz" >/dev/null && break; sleep 0.1; done
cd frontend && NOOK_URL="http://127.0.0.1:$PORT" npx --yes tsx e2e/collab.e2e.ts || { echo "--- server log"; tail -30 "$TMP/server.log"; exit 1; }
