#!/usr/bin/env bash
# Runs the UI end-to-end suites (frontend/e2e/ui) in Chromium and WebKit
# against a throwaway Nook: a fresh build of this tree (no Docker image),
# a temporary database, the browsers from the Playwright Docker image.
#
#   scripts/ui-e2e.sh                 # every suite, both browsers
#   scripts/ui-e2e.sh database        # one suite (tabs, workspace, history, database)
#   BROWSERS=chromium scripts/ui-e2e.sh tabs
#
# Note: Playwright emulates HTML5 drag-and-drop, so a passing HTML5 drag
# here doesn't prove a native one starts in Safari; the board and tab
# drags use pointer events and are driven with a real mouse.
set -euo pipefail
cd "$(dirname "$0")/.."
PW_IMAGE=mcr.microsoft.com/playwright:v1.56.0-noble # matches frontend's playwright
PORT=${PORT:-18099}
TMP=$(mktemp -d)

(cd frontend && npm run build --silent >/dev/null)
rm -rf backend/internal/webui/dist && mkdir -p backend/internal/webui/dist && cp -r frontend/dist/. backend/internal/webui/dist/
(cd backend && go build -o "$TMP/nook" ./cmd/nook)
git checkout -q -- backend/internal/webui/dist/index.html # the tracked placeholder

NOOK_ADDR=127.0.0.1:$PORT NOOK_DB_PATH="$TMP/nook.db" NOOK_UPLOADS_DIR="$TMP/uploads" NOOK_COOKIE_SECURE=false \
  NOOK_ADMIN_USERNAME=admin NOOK_ADMIN_PASSWORD=e2e-password-123 "$TMP/nook" >"$TMP/server.log" 2>&1 &
PID=$!
NAME=nook-ui-e2e-$$
trap 'docker rm -f "$NAME" >/dev/null 2>&1; kill $PID 2>/dev/null; rm -rf "$TMP"' EXIT
for _ in $(seq 50); do curl -sf "http://127.0.0.1:$PORT/healthz" >/dev/null && break; sleep 0.1; done

docker run --rm --name "$NAME" --network host --ipc=host --user "$(id -u):$(id -g)" \
  -v "$PWD/frontend:/w" -v "$TMP:$TMP" -w /w \
  -e NOOK_URL="http://127.0.0.1:$PORT" -e NOOK_DB="$TMP/nook.db" -e BROWSERS="${BROWSERS:-chromium,webkit}" -e JOBS="${JOBS:-3}" \
  "$PW_IMAGE" node e2e/ui/run.mjs "$@" || { echo "--- server log"; tail -20 "$TMP/server.log"; exit 1; }
