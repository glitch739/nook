#!/bin/sh
# Nightly Nook backup, run from cron in the run folder (docs/DEPLOY.md).
# Writes data/backups/nook-backup-<UTC time>.tar.gz (database + uploads,
# consistent while Nook runs) and keeps the newest 14.
set -eu
cd "$(dirname "$0")"
docker compose exec -T nook nook backup -dir /data/backups -keep 14
# Then remove uploaded images/files nothing references any more (only
# after a fresh backup exists; see `nook sweep-uploads -h`).
docker compose exec -T nook nook sweep-uploads
