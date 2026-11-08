#!/usr/bin/env bash
set -euo pipefail
here=$(cd "$(dirname "$0")" && pwd)
if [ "$(basename "$here")" = std-t61 ] && [ "$(basename "$(dirname "$here")")" = scripts ]; then
  root=$(dirname "$(dirname "$here")")
else
  root=$here
fi
out=${AMBE2C_OUT_DIR:-$root/build/ambe2c}
repo=https://github.com/symysak/ambe2-c.git
commit=70e32cbfc1cf5a08f3e9a3fbe41f6a9873cc4ca6
src=$out/src
mkdir -p "$out"
if [ ! -d "$src/.git" ]; then
  git clone --quiet "$repo" "$src"
fi
git -C "$src" fetch --quiet origin || true
git -C "$src" checkout --quiet "$commit"

cat > "$out/ambe2c_decode.c" <<'CEOF'
/* ambe2-c を使った AMBE 系デコーダ CLI。引数でレートを選ぶ（AMBE-3000R の RATET）:
     0   … AMBE+ 2400 bps、誤り訂正なし（48 ビット = 6 バイト）
     22  … AMBE+ 2400 bps + 誤り訂正 1600 bps（80 ビット = 10 バイト）
     17  … AMBE+ 4000 bps、誤り訂正なし（80 ビット = 10 バイト。インタリーブ済みのチャネルの並び）
     26  … AMBE+ 4000 bps + 誤り訂正 2400 bps（128 ビット = 16 バイト）
     31  … AMBE+ 2000 bps（40 ビット = 5 バイト）
     33  … AMBE+2 3600x2450（72 ビット = 9 バイト。誤り訂正込み、AMBE-3000R のチャネルの並び）
     34  … AMBE+2 2450 bps、誤り訂正なし（49 ビット = 7 バイト。AMBE-3000R / ThumbDV の並び）
   印 1 バイト + フレーム（MSB 先頭）を受け取り、8kHz int16 LE を毎フレーム flush して返す。印が 1 なら消失。 */
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include "blip25.h"
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
  const char *arg = argc > 1 ? argv[1] : "31";
  int rate = atoi (arg);
  size_t nb = rate == 0 && strcmp (arg, "0") == 0 ? 6 : rate == 22 || rate == 17 ? 10 : rate == 26 ? 16
            : rate == 31 ? 5 : rate == 33 ? 9 : rate == 34 ? 7 : 0;
  if (nb == 0) {
    fprintf (stderr, "usage: ambe2c_decode 0|22|17|26|31|33|34\n");
    return 2;
  }
  static blip25_decoder dec;
  unsigned char in[1 + 16];
  int16_t pcm[160];
  int errs;
  blip25_init (&dec);
  while (fread (in, 1, 1 + nb, stdin) == 1 + nb) {
    if (in[0] == 1) blip25_decode_lost (&dec, pcm);
    else if (rate == 0) blip25_decode_ambeplus_r0 (&dec, in + 1, pcm);
    else if (rate == 22) blip25_decode_ambeplus_r22 (&dec, in + 1, pcm, &errs);
    else if (rate == 17) blip25_decode_ambeplus_r17 (&dec, in + 1, pcm);
    else if (rate == 26) blip25_decode_ambeplus_r26 (&dec, in + 1, pcm, &errs);
    else if (rate == 33) blip25_decode_3600 (&dec, in + 1, pcm, &errs);
    else if (rate == 34) blip25_decode_2450_thumbdv (&dec, in + 1, pcm);
    else blip25_decode_ambeplus_r31 (&dec, in + 1, pcm);
    fwrite (pcm, sizeof (int16_t), 160, stdout);
    fflush (stdout);            /* 毎フレーム流さないと呼び出し側が待ち続ける */
  }
  return 0;
}
CEOF

d=$src/decoder
cc -O2 -std=c99 -I"$d/include" -I"$d/src" -o "$out/ambe2c_decode" "$out/ambe2c_decode.c" \
   "$d/src/b25_dsp_a.c" "$d/src/b25_dsp_b.c" "$d/src/b25_decode_c.c" "$d/src/b25_decode_d.c" \
   "$d/src/b25_tables.c" "$d/src/b25_golay.c" "$d/src/blip25.c" "$d/src/mbelib_compat.c" \
   "$d/src/ambeplus.c" "$d/src/ambeplus_tables.c" "$d/src/ambeplus_fec.c" "$d/src/ambeplus4000_tables.c" \
   "$d/src/ambeplus_layer2.c" -lm
echo "できました: $out/ambe2c_decode"
