#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"

if [ "$(basename "$SCRIPT_DIR")" = "std-t115" ] &&
   [ "$(basename "$(dirname "$SCRIPT_DIR")")" = "scripts" ]; then
  ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
else
  ROOT="$SCRIPT_DIR"
fi
ROOT="${AMRWBPLUS_OUT_DIR:-$ROOT}"

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

fixed=0
for f in $(grep -rl '#include[[:space:]]*"[^"]*\\' "$SRC" --include='*.c' --include='*.h' || true); do
  sed -e ':a' -e 's|^\([[:space:]]*#[[:space:]]*include[[:space:]]*"[^"]*\)\\|\1/|' -e 'ta' \
      "$f" > "$f.tmp" && mv "$f.tmp" "$f"
  fixed=$((fixed + 1))
done
echo "  include のパス区切りを直したファイル: $fixed"

ENC_IF="$SRC/lib_amr/enc_if.c"
if [ -f "$ENC_IF" ] &&
   grep -qE '^const[[:space:]]+Word16[[:space:]]*\*[[:space:]]*dhf[[:space:]]*\[[[:space:]]*10[[:space:]]*\][[:space:]]*;[[:space:]]*$' "$ENC_IF"; then
  sed -e 's|^\(const[[:space:]][[:space:]]*Word16[[:space:]]*\*[[:space:]]*dhf[[:space:]]*\[[[:space:]]*10[[:space:]]*\][[:space:]]*;[[:space:]]*\)$|extern \1|' \
      "$ENC_IF" > "$ENC_IF.tmp" && mv "$ENC_IF.tmp" "$ENC_IF"
  echo "  enc_if.c の dhf 仮定義を extern 宣言に直した"
else
  echo "  enc_if.c の dhf は宣言済み"
fi

cat > "$SRC/stub3gp.c.tmp" <<'STUB3GP_EOF'
/* STD-T115 QPSK ナロー方式デコーダ用のスタブ。
 *
 * 3GP コンテナ読み書きは Windows の er-libisomedia.dll 側にしか実装が無い。
 * こちらは生ビットストリーム（-ff raw）だけを使うので、呼ばれたら明示的に
 * 失敗させる。信号処理には関与しない。
 */
#include <stdio.h>
#include <stdlib.h>
#include "include/amr_plus.h"

static void amrwbp_no_3gp(const char *fn)
{
   fprintf(stderr, "%s: 3GP container is not supported in this build; use -ff raw\n", fn);
   exit(EXIT_FAILURE);
}

int Create3GPAMRWBPlus(void) { amrwbp_no_3gp("Create3GPAMRWBPlus"); return 0; }
int Create3GPAMRWB(void) { amrwbp_no_3gp("Create3GPAMRWB"); return 0; }
int WriteSamplesAMRWBPlus(EncoderConfig conf, void *Serial, int length)
{
   (void) conf; (void) Serial; (void) length;
   amrwbp_no_3gp("WriteSamplesAMRWBPlus"); return 0;
}
int Close3GP(char *filename) { (void) filename; amrwbp_no_3gp("Close3GP"); return 0; }
int GetNextFrame3GP(short *tfi, int *bfi, short *extension, short *mode,
                    short *st_mode, short *fst, void *serial, int init)
{
   (void) tfi; (void) bfi; (void) extension; (void) mode;
   (void) st_mode; (void) fst; (void) serial; (void) init;
   amrwbp_no_3gp("GetNextFrame3GP"); return 0;
}
int Open3GP(short *tfi, int *bfi, char *filename, int verbose, DecoderConfig *conf)
{
   (void) tfi; (void) bfi; (void) filename; (void) verbose; (void) conf;
   amrwbp_no_3gp("Open3GP"); return 0;
}
STUB3GP_EOF
# 中身が同じなら触らない（毎回書き換えると make の再ビルド判断が濁る）。
if cmp -s "$SRC/stub3gp.c.tmp" "$SRC/stub3gp.c" 2>/dev/null; then
  rm -f "$SRC/stub3gp.c.tmp"
  echo "  3GP スタブは最新"
else
  mv "$SRC/stub3gp.c.tmp" "$SRC/stub3gp.c"
  echo "  3GP スタブを書き出し: stub3gp.c"
fi

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
