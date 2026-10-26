#!/usr/bin/env bash
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
SRC="$REPO_ROOT/third_party/T-REC-G.722.1-200505-I!!SOFT-ZST-E/Software/Fixed-200505-Rel.2.1"
OUT_DIR="$REPO_ROOT/build/g7221"
WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT

echo "[1/5] ITU-T G.722.1 参考実装を展開..."
[ -d "$SRC" ] || { echo "ERROR: G.722.1 ソースが見つかりません: $SRC"; exit 1; }
B="$WORK/build"; mkdir -p "$B"
cp "$SRC"/common/*.c "$SRC"/common/*.h \
   "$SRC"/encode/*.c "$SRC"/encode/*.h \
   "$SRC"/decode/*.c "$SRC"/decode/*.h "$B/"
unzip -oq "$SRC/common/stl-files.zip" -d "$B"
cd "$B"

echo "[2/5] パッチ適用（modern C 互換）..."
(cd "$REPO_ROOT" && go run ./scripts/std-t86/g7221patch normalize "$B")

echo "[3/5] ビルド（パッチ前の decode）..."
LIBS="basop32.c common.c dct4_a.c dct4_s.c huff_tab.c tables.c coef2sam.c sam2coef.c decoder.c encoder.c count.c"
mkdir -p "$OUT_DIR"
CC="${CC:-cc}"
echo "    compiler: $CC"
"$CC" -O2 -w -o "$OUT_DIR/g7221_decode" decode.c $LIBS -lm

echo "[4/5] S-Codec 適応分離パッチ（ARIB STD-T86 §5.6）→ g7221_sep_decode / g7221_encode..."
(cd "$REPO_ROOT" && go run ./scripts/std-t86/g7221patch scodec "$B")
"$CC" -O2 -w -o "$OUT_DIR/g7221_sep_decode" decode.c $LIBS -lm
"$CC" -O2 -w -o "$OUT_DIR/g7221_encode" encode.c $LIBS -lm

echo "[5/5] 完了: $OUT_DIR"
ls -la "$OUT_DIR"
