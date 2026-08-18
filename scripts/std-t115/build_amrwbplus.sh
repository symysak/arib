#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
WORK="$ROOT/build/amrwbplus"
SRC="$WORK/src"
ZIP="$WORK/26304-c00.zip"
URL="https://www.3gpp.org/ftp/Specs/archive/26_series/26.304/26304-c00.zip"

mkdir -p "$WORK"

if [ ! -f "$ZIP" ]; then
  echo "==> 3GPP TS 26.304 (Rel-12) を取得: $URL"
  curl -fsSL -A "Mozilla/5.0" -o "$ZIP" "$URL"
fi
echo "==> zip: $(wc -c <"$ZIP") bytes"

if [ ! -d "$SRC" ]; then
  echo "==> 展開"
  TMP="$WORK/unzip"
  rm -rf "$TMP"; mkdir -p "$TMP"
  unzip -oq "$ZIP" -d "$TMP"
  INNER="$(find "$TMP" -name '*ANSI-C_source_code.zip' | head -1)"
  [ -n "$INNER" ] || { echo "ANSI-C ソースの zip が見つかりません" >&2; exit 1; }
  unzip -oq "$INNER" -d "$TMP/code"
  CDIR="$(find "$TMP/code" -type d -name c-code | head -1)"
  [ -n "$CDIR" ] || { echo "c-code ディレクトリが見つかりません" >&2; exit 1; }
  mkdir -p "$SRC"
  cp -R "$CDIR"/. "$SRC/"
  rm -rf "$TMP"
fi

echo "==> パッチ"
python3 "$ROOT/scripts/std-t115/patch_amrwbplus.py" "$SRC"

echo "==> ビルド"
CC="${CC:-cc}"
CFLAGS="${CFLAGS:--O2 -w}"
OBJ="$WORK/obj"
rm -rf "$OBJ"; mkdir -p "$OBJ"

SOURCES=""
for f in "$SRC"/common/*.c "$SRC"/decoder/*.c "$SRC"/lib_amr/*.c; do
  case "$(basename "$f")" in
    3gpp_mod.c) continue ;;
  esac
  SOURCES="$SOURCES $f"
done
SOURCES="$SOURCES $SRC/stub3gp.c"

(cd "$OBJ" && $CC $CFLAGS -c $SOURCES -I "$SRC/include" -I "$SRC/lib_amr")
$CC $CFLAGS "$OBJ"/*.o -lm -o "$WORK/amrwbp_decoder"

echo "==> 完成: $WORK/amrwbp_decoder"
"$WORK/amrwbp_decoder" 2>&1 | head -3 || true
