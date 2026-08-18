#!/usr/bin/env bash
set -euo pipefail

TAG="${1:?tag}"
HERE="$(cd "$(dirname "$0")" && pwd)"

cat "$HERE/../release_notes.md"

echo
echo "### 対象"
echo
for z in dist/*.zip; do
    [ -e "$z" ] || continue
    base="$(basename "$z" .zip)"
    target="${base#"stdt86-$TAG-"}"
    printf -- '- `%s` — %s\n' "$(basename "$z")" "${target/-//}"
done

echo
echo "Windows on ARM 向けは配布していない（x64 版がエミュレーションで動く）。"
echo
echo "チェックサムは \`SHA256SUMS.txt\`。"
