#!/usr/bin/env bash
set -euo pipefail

GOOS="${1:?goos}"
GOARCH="${2:?goarch}"
BIN="${3:?本体バイナリのパス}"

REPO_ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
cd "$REPO_ROOT"

if [ "${GITHUB_REF_TYPE:-}" = "tag" ] && [ -n "${GITHUB_REF_NAME:-}" ]; then
    VERSION="$GITHUB_REF_NAME"
else
    VERSION="$(git describe --tags --always --dirty 2>/dev/null || echo dev)"
fi

NAME="stdt86-${VERSION}-${GOOS}-${GOARCH}"
PKG="pkg/$NAME"
ITU_SRC="third_party/T-REC-G.722.1-200505-I!!SOFT-ZST-E/Software/Fixed-200505-Rel.2.1"

rm -rf pkg "$NAME.zip"
mkdir -p "$PKG"

install -m 0755 "$BIN" "$PKG/$(basename "$BIN")"

mkdir -p "$PKG/build"
cp -R build/g7221 "$PKG/build/g7221"
cp "$ITU_SRC/Readme.txt" "$PKG/build/g7221/ITU-G.722.1-Readme.txt"

cp readme.md "$PKG/readme.md"

( cd pkg && zip -q -r "../$NAME.zip" "$NAME" )
rm -rf pkg

echo "作成: $NAME.zip"
unzip -l "$NAME.zip"
