#!/bin/sh
# Writes name.br and name.gz next to every compressible file (JS, CSS,
# HTML, SVG, JSON) over 1 KB in the given directory, keeping a variant
# only when it is actually smaller. internal/webui serves these to
# browsers that accept them and falls back to the plain file otherwise.
# Needs: gzip, brotli.  Usage: scripts/precompress.sh frontend/dist
set -eu
dir="${1:?usage: precompress.sh <dir>}"
find "$dir" -type f \( -name '*.js' -o -name '*.css' -o -name '*.html' -o -name '*.svg' -o -name '*.json' \) -size +1k |
while IFS= read -r f; do
  size=$(wc -c < "$f")
  gzip -9 -k -n -f "$f"
  [ "$(wc -c < "$f.gz")" -lt "$size" ] || rm -f "$f.gz"
  brotli -q 11 -k -f "$f"
  [ "$(wc -c < "$f.br")" -lt "$size" ] || rm -f "$f.br"
done
