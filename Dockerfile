# syntax=docker/dockerfile:1
#
# Nook: one image, one static binary with the web UI embedded.
# Data (SQLite database, uploads, backups) lives in the /data volume.
#
#   scripts/build-image.sh   (tags nook:<date>-<commit> and nook:latest)
#
# Run it with the compose file in deploy/, see docs/DEPLOY.md.

# ---- 1. Web UI ----
FROM node:lts-alpine AS web
WORKDIR /src/frontend
COPY frontend/package.json frontend/package-lock.json ./
RUN npm ci --no-audit --no-fund
COPY frontend/ ./
# Tests gate the image: a failing test means no image at all.
RUN npm test
RUN npm run build
# Precompressed .br/.gz variants, served by internal/webui when the
# browser accepts them (no per-request compression work).
COPY scripts/precompress.sh /usr/local/bin/precompress.sh
RUN apk add --no-cache brotli && precompress.sh dist

# ---- 2. Server binary (pure Go: no C toolchain, fully static) ----
FROM golang:1-alpine AS server
# Pure Go throughout (SQLite included): no C toolchain in this image.
ENV CGO_ENABLED=0
WORKDIR /src/backend
COPY backend/go.mod backend/go.sum ./
RUN go mod download
COPY backend/ ./
RUN go vet ./... && go test ./...
# Replace the "frontend not built" placeholder with the real build.
RUN rm -rf internal/webui/dist && mkdir -p internal/webui/dist
COPY --from=web /src/frontend/dist/ internal/webui/dist/
RUN go build -trimpath -ldflags="-s -w" -o /out/nook ./cmd/nook

# ---- 3. Runtime ----
FROM alpine:3
RUN apk add --no-cache ca-certificates tzdata \
 && addgroup -S nook && adduser -S -G nook -h /data nook \
 && mkdir -p /data && chown nook:nook /data
COPY --from=server /out/nook /usr/local/bin/nook
USER nook
WORKDIR /data
ENV NOOK_ADDR=:8080 \
    NOOK_DB_PATH=/data/nook.db \
    NOOK_UPLOADS_DIR=/data/uploads
EXPOSE 8080
VOLUME /data
HEALTHCHECK --interval=30s --timeout=5s --start-period=20s \
  CMD wget -qO /dev/null http://127.0.0.1:8080/healthz || exit 1
ENTRYPOINT ["nook"]
