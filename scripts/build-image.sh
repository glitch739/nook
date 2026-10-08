#!/bin/sh
# Builds the Nook Docker image from this source tree and tags it
#   nook:<YYYYMMDD>-<commit>[-dirty]   and   nook:latest
# "-dirty" marks a build with uncommitted changes, so `docker ps` /
# `docker images` always tell you what is actually running.
set -eu
cd "$(dirname "$0")/.."
tag="$(date +%Y%m%d)-$(git rev-parse --short HEAD)"
if ! git diff --quiet HEAD -- || [ -n "$(git ls-files --others --exclude-standard)" ]; then
  tag="$tag-dirty"
fi
docker build -t "nook:$tag" -t nook:latest .
echo "built nook:$tag (also tagged nook:latest)"
