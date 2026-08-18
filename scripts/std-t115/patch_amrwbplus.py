import pathlib
import re
import sys

STUB = r'''/* STD-T115 QPSK ナロー方式デコーダ用のスタブ。
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
'''

INCLUDE_RE = re.compile(r'#\s*include\s+"([^"]*)"')


def fix_includes(root: pathlib.Path) -> int:
    changed = 0
    for path in list(root.rglob("*.c")) + list(root.rglob("*.h")):
        text = path.read_text(encoding="utf-8", errors="surrogateescape")

        def repl(m: "re.Match[str]") -> str:
            return '#include "%s"' % m.group(1).replace("\\", "/")

        new = INCLUDE_RE.sub(repl, text)
        if new != text:
            path.write_text(new, encoding="utf-8", errors="surrogateescape")
            changed += 1
    return changed


def main() -> int:
    if len(sys.argv) != 2:
        sys.stderr.write(__doc__)
        return 2
    root = pathlib.Path(sys.argv[1])
    if not (root / "decoder").is_dir() or not (root / "lib_amr").is_dir():
        sys.stderr.write("c-code ディレクトリに見えません: %s\n" % root)
        return 1

    n = fix_includes(root)
    print("  include のパス区切りを直したファイル: %d" % n)

    stub = root / "stub3gp.c"
    if not stub.exists() or stub.read_text(encoding="utf-8") != STUB:
        stub.write_text(STUB, encoding="utf-8")
        print("  3GP スタブを書き出し: %s" % stub.name)
    else:
        print("  3GP スタブは最新")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
