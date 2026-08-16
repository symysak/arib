#!/usr/bin/env bash
set -euo pipefail

root="$(cd "$(dirname "$0")/.." && pwd)"
static="$root/internal/server/static"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

fail=0
count=0
while IFS= read -r f; do
  rel="${f#"$static"/}"
  dst="$tmp/${rel//\//_}.mjs"
  cp "$f" "$dst"
  if ! node --check "$dst" 2>"$tmp/err"; then
    echo "NG $rel"
    sed "s|$dst|$rel|g" "$tmp/err" >&2
    fail=1
  fi
  count=$((count + 1))
done < <(find "$static" -name '*.js' | sort)

if [ "$fail" -eq 0 ]; then
  echo "OK $count files"
fi
exit "$fail"
