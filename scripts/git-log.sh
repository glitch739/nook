#!/usr/bin/env bash
# Dumps the repo's commit history in a readable form, oldest first (so it
# reads in the same order as DEVLOG.md).
#
# Usage:
#   scripts/git-log.sh              # hash, date, message
#   scripts/git-log.sh --stat       # + which files changed, +/- line counts
#   scripts/git-log.sh --full       # + full diffs (every line changed)
set -euo pipefail
cd "$(dirname "$0")/.."

case "${1:-}" in
  --full)
    git log --reverse --date=format:'%Y-%m-%d %H:%M %Z' -p
    ;;
  --stat)
    git log --reverse --date=format:'%Y-%m-%d %H:%M %Z' --stat \
      --pretty=format:'%n## %h — %ad%n%n%s%n%n%b'
    ;;
  "")
    git log --reverse --date=format:'%Y-%m-%d %H:%M %Z' \
      --pretty=format:'## %h — %ad%n%n%s%n%n%b%n---'
    ;;
  *)
    echo "usage: $0 [--stat|--full]" >&2
    exit 1
    ;;
esac
