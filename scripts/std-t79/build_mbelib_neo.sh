#!/usr/bin/env bash
set -euo pipefail
here=$(cd "$(dirname "$0")" && pwd)
if [ "$(basename "$here")" = std-t79 ] && [ "$(basename "$(dirname "$here")")" = scripts ]; then
  root=$(dirname "$(dirname "$here")")
else
  root=$here
fi
out=${MBELIB_NEO_OUT_DIR:-$root/build/mbelib-neo}
repo=https://github.com/symysak/mbelib-neo.git
commit=c37a2e60acbdf987bd9511a1a3523d8fe7fe8506
src=$out/src
mkdir -p "$out/include/mbelib-neo"
if [ ! -d "$src/.git" ]; then
  git clone --quiet "$repo" "$src"
fi
git -C "$src" fetch --quiet origin feat/ambe4000-fec-probes || true
git -C "$src" checkout --quiet "$commit"

sed 's/@[A-Z_]*@/0.0.0/g' "$src/include/mbelib-neo/version.h.in" > "$out/include/mbelib-neo/version.h"

cat > "$out/ambeplus_decode.c" <<'CEOF'
/* mbelib-neo を使った AMBE+ 4000/2400 デコーダ CLI。
   優先順の 80 ビット（10 バイト、MSB 先頭）を受け取り、8kHz int16 LE を毎フレーム flush して返す。 */
#include <stdio.h>
#include <string.h>
#include <mbelib-neo/mbelib.h>
#ifdef _WIN32
#include <fcntl.h>
#include <io.h>
#endif

int main (int argc, char **argv)
{
#ifdef _WIN32
  /* 標準入出力はバイナリ。テキストのままだと PCM の 0x0A が 0x0D 0x0A に、入力の 0x1A が EOF になる */
  _setmode (_fileno (stdin), _O_BINARY);
  _setmode (_fileno (stdout), _O_BINARY);
#endif
  int verbose = argc > 1 && !strcmp (argv[1], "-v");
  mbe_ambe4000_decoder *dec = mbe_ambe4000DecoderAlloc ();
  if (dec == NULL) return 1;
  unsigned char in[10];
  char bits[80];
  short pcm[160];
  long n = 0;
  while (fread (in, 1, 10, stdin) == 10) {
    for (int i = 0; i < 80; i++) bits[i] = (char) ((in[i >> 3] >> (7 - (i & 7))) & 1);
    mbe_process_result res;
    if (mbe_processAmbe4000Data (pcm, &res, bits, dec) < 0) memset (pcm, 0, sizeof pcm);
    fwrite (pcm, sizeof (short), 160, stdout);
    fflush (stdout);            /* 毎フレーム流さないと呼び出し側が待ち続ける */
    if (verbose) fprintf (stderr, "frame %ld flags=0x%x\n", n, (unsigned) res.flags);
    n++;
  }
  mbe_ambe4000DecoderFree (dec);
  return 0;
}
CEOF

cc -O2 -std=gnu11 -DMBE_BUILDING=1 -I"$src/include" -I"$out/include" -I"$src" -I"$src/src/internal" -I"$src/src/external/pffft" \
   -o "$out/ambeplus_decode" "$out/ambeplus_decode.c" \
   "$src/src/ambe/ambe3600x2400.c" "$src/src/ambe/ambe6400x4000.c" "$src/src/ambe/ambe4000_decode.c" \
   "$src/src/ambe/ambe3600x2400_enc.c" "$src/src/ambe/dstar_frame.c" "$src/src/ambe/ambe3600x2450.c" \
   "$src/src/ambe/ambe_common.c" "$src/src/ecc/ecc.c" "$src/src/ecc/ecc_const.c" \
   "$src/src/imbe/imbe7100x4400.c" "$src/src/imbe/imbe7200x4400.c" "$src/src/core/mbelib.c" \
   "$src/src/core/mbe_pcm_convert.c" "$src/src/core/mbe_adaptive.c" "$src/src/core/mbe_unvoiced_fft.c" \
   "$src/src/external/pffft/pffft.c" "$src/src/external/pffft/fftpack.c" -lm
echo "できました: $out/ambeplus_decode"
