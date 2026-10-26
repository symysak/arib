package main

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
)

func main() {
	if len(os.Args) != 3 {
		fmt.Fprintln(os.Stderr, "usage: g7221patch <normalize|scodec> <dir>")
		os.Exit(2)
	}
	dir := os.Args[2]
	var err error
	switch os.Args[1] {
	case "normalize":
		err = normalize(dir)
	case "scodec":
		err = scodec(dir)
	default:
		err = fmt.Errorf("不明なサブコマンド: %q", os.Args[1])
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "g7221patch:", err)
		os.Exit(1)
	}
}


var (
	reRound = regexp.MustCompile(`\bround\b`)
	reKRMain = regexp.MustCompile(`(?m)^[ \t]*main[ \t]*\([ \t]*Word16[ \t]+argc[ \t]*,[ \t]*char[ \t]*\*argv\[\][ \t]*\)`)
)

func normalize(dir string) error {
	files, err := sources(dir)
	if err != nil {
		return err
	}
	changed := 0
	for _, path := range files {
		orig, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		b := bytes.ReplaceAll(orig, []byte("\r\n"), []byte("\n"))
		b = bytes.TrimSuffix(b, []byte("\r"))
		b = reRound.ReplaceAll(b, []byte("g722_round"))
		if filepath.Ext(path) == ".c" {
			b = reKRMain.ReplaceAll(b, []byte("int main(int argc,char *argv[])"))
		}
		if filepath.Base(path) == "typedef.h" {
			b = widen(b, "defined(__unix__)", "defined(__unix__) || defined(__APPLE__)")
			b = widen(b, "defined(_MSC_VER)", "defined(_MSC_VER) || defined(_WIN32)")
		}
		if bytes.Equal(b, orig) {
			continue
		}
		if err := os.WriteFile(path, b, 0o644); err != nil {
			return err
		}
		changed++
	}
	fmt.Printf("    normalize: %d / %d ファイルを書き換え\n", changed, len(files))
	return nil
}

func widen(b []byte, old, new string) []byte {
	if bytes.Contains(b, []byte(new)) {
		return b
	}
	return bytes.ReplaceAll(b, []byte(old), []byte(new))
}

func sources(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if ext := filepath.Ext(e.Name()); ext == ".c" || ext == ".h" {
			out = append(out, filepath.Join(dir, e.Name()))
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("%s に .c/.h がありません", dir)
	}
	return out, nil
}


func scodec(dir string) error {
	for _, step := range []struct {
		name string
		fn   func(string) error
	}{
		{"decoder.c", patchDecoder},
		{"decode.c", patchDecodeMain},
		{"encode.c", patchEncodeMain},
	} {
		if err := step.fn(filepath.Join(dir, step.name)); err != nil {
			return fmt.Errorf("%s: %w", step.name, err)
		}
	}
	return nil
}

func patchDecoder(path string) error {
	t, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if bytes.Contains(t, []byte("sc_mode")) {
		fmt.Println("    decoder.c: パッチ済みなので飛ばす")
		return nil
	}

	if t, err = replaceOnce(t, `#include "count.h"`, scGlobals, "include"); err != nil {
		return err
	}

	if t, err = replaceOnce(t, decodeEnvelopeAnchor, decodeEnvelopeNew, "decode_envelope"); err != nil {
		return err
	}

	if t, err = replaceOnce(t, rateAdjustAnchor, rateAdjustAnchor+rateAdjustAdd, "rate_adjust"); err != nil {
		return err
	}

	if t, err = replaceOnce(t, "    Word16 j,n;", "    Word16 j,n;\n    Word16 _kk_;", "locals"); err != nil {
		return err
	}

	loopDone := false
	for _, trail := range []string{" ", ""} {
		old := fmt.Sprintf(loopOldFormat, trail)
		if bytes.Count(t, []byte(old)) == 1 {
			t = bytes.Replace(t, []byte(old), []byte(loopNew), 1)
			loopDone = true
			break
		}
	}
	if !loopDone {
		return errors.New("アンカー 'region loop' が見つかりません")
	}

	for _, r := range []struct{ old, new, label string }{
		{huffBitOld, huffBitNew, "huffman bit"},
		{huffLeftOld, huffLeftNew, "huffman bits_left"},
		{signBitOld, signBitNew, "sign bit"},
		{signLeftOld, signLeftNew, "sign bits_left"},
	} {
		if t, err = replaceOnce(t, r.old, r.new, r.label); err != nil {
			return err
		}
	}

	if t, err = replaceOnce(t, tailOld, tailNew, "tail check"); err != nil {
		return err
	}

	if err := os.WriteFile(path, t, 0o644); err != nil {
		return err
	}
	fmt.Println("    decoder.c: パッチ適用")
	return nil
}

var reDecodeMain = regexp.MustCompile(`int main\(int argc,char \*argv\[\]\)\s*\{`)

func patchDecodeMain(path string) error {
	s, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if bytes.Contains(s, []byte("STDT86_SCODEC")) {
		fmt.Println("    decode.c: パッチ済みなので飛ばす")
		return nil
	}
	loc := reDecodeMain.FindIndex(s)
	if loc == nil {
		return errors.New("main() が見つかりません（normalize を先に走らせること）")
	}
	inject := []byte("\n    { extern int sc_mode; const char *_e = getenv(\"STDT86_SCODEC\");" +
		" if(_e) sc_mode=atoi(_e); }\n")
	out := make([]byte, 0, len(s)+len(inject))
	out = append(out, s[:loc[1]]...)
	out = append(out, inject...)
	out = append(out, s[loc[1]:]...)
	out = ensureStdlib(out)
	if err := os.WriteFile(path, out, 0o644); err != nil {
		return err
	}
	fmt.Println("    decode.c: パッチ適用")
	return nil
}

func patchEncodeMain(path string) error {
	t, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if bytes.Contains(t, []byte("STDT86_MLT_OUT")) {
		fmt.Println("    encode.c: パッチ済みなので飛ばす")
		return nil
	}
	if t, err = replaceOnce(t, mltAnchor, mltAnchor+mltDump, "mlt"); err != nil {
		return err
	}
	t = ensureStdlib(t)
	if err := os.WriteFile(path, t, 0o644); err != nil {
		return err
	}
	fmt.Println("    encode.c: パッチ適用（MLT ダンプ）")
	return nil
}

func ensureStdlib(b []byte) []byte {
	if bytes.Contains(b, []byte("#include <stdlib.h>")) {
		return b
	}
	return bytes.Replace(b, []byte("#include <stdio.h>"),
		[]byte("#include <stdio.h>\n#include <stdlib.h>"), 1)
}

func replaceOnce(t []byte, old, new, label string) ([]byte, error) {
	if n := bytes.Count(t, []byte(old)); n != 1 {
		return nil, fmt.Errorf("アンカー %q が %d 個一致しました（1 個であること）", label, n)
	}
	return bytes.Replace(t, []byte(old), []byte(new), 1), nil
}


const scGlobals = `#include "count.h"
#include <stdio.h>
#include <stdlib.h>
int sc_mode = 0;
static Word16 g_frame_bits[320];
static int g_fwd, g_rev, g_dir, g_sc_alt;
static int g_dirs[14], g_starts[14];
static Word16 g_sorted_region[14];
static int g_span[15];
#define SC_PROTECTED_BITS 190
#define SC_REV_POS(p) (((p) & ~15) + (15 - ((p) & 15)))
static void sc_next_bit(Bit_Obj *bitobj){
    if(g_dir==0){ bitobj->next_bit = g_frame_bits[g_fwd++]; }
    else        { bitobj->next_bit = g_frame_bits[SC_REV_POS(g_rev)]; g_rev--; }
}
static void sc_emit_mux(int number_of_regions, int consumed){
    static FILE *fp = 0;
    Word16 mi[320];
    int used[320];
    int k, j, r, st, len, dir, fwd = g_fwd, rev = 319, alt = 1;
    g_span[number_of_regions] = consumed;
    int F, alt2 = 1, trunc = 0;
    for(j=0;j<320;j++){ mi[j]=0; used[j]=0; }
    for(j=0;j<g_fwd;j++){ mi[j]=g_frame_bits[j]; used[j]=1; }
    F = g_fwd;
    for(k=0;k<number_of_regions;k++){
        r = g_sorted_region[k];
        len = g_span[r+1]-g_span[r];
        if(F < SC_PROTECTED_BITS){ dir=0; } else { dir=alt2; alt2^=1; }
        if(dir==0) F += len;
    }
    for(k=0;k<number_of_regions && !trunc;k++){
        r = g_sorted_region[k];
        st = g_span[r]; len = g_span[r+1]-g_span[r];
        if(fwd < SC_PROTECTED_BITS){ dir=0; } else { dir=alt; alt^=1; }
        if(dir==0){ for(j=0;j<len;j++){ mi[fwd+j]=g_frame_bits[st+j]; used[fwd+j]=1; } fwd+=len; }
        else{
            for(j=0;j<len;j++){
                int ph = SC_REV_POS(rev-j);
                if(ph < F){ trunc = 1; break; }
                mi[ph]=g_frame_bits[st+j]; used[ph]=1;
            }
            if(!trunc) rev-=len;
        }
    }
    for(j=0,k=0; j<320; j++) if(!used[j]) mi[j]=g_frame_bits[g_span[number_of_regions]+(k++)];
    if(!fp){ const char *p = getenv("STDT86_MUX_OUT"); fp = fopen(p?p:"mux_out.txt","w"); }
    for(j=0;j<320;j++) fputc(mi[j]?'1':'0',fp);
    fputc('\n',fp); fflush(fp);
}`

const decodeEnvelopeAnchor = "        decode_envelope(bitobj,"

const decodeEnvelopeNew = `        if(sc_mode){ int _i,_j; for(_i=0;_i<20;_i++){ Word16 _w=bitobj->code_word_ptr[_i]; for(_j=0;_j<16;_j++) g_frame_bits[_i*16+_j]=(_w>>(15-_j))&1; } }
        decode_envelope(bitobj,`

const rateAdjustAnchor = "        rate_adjust_categories(categorization_control,\n\t\t\t                   decoder_power_categories,\n\t\t\t                   decoder_category_balances);"

const rateAdjustAdd = `

        if(sc_mode){ int _a,_b,_t; for(_a=0;_a<number_of_regions;_a++) g_sorted_region[_a]=_a;
          for(_a=0;_a<number_of_regions-1;_a++) for(_b=0;_b<number_of_regions-1-_a;_b++)
            if(decoder_power_categories[g_sorted_region[_b]]>decoder_power_categories[g_sorted_region[_b+1]]){_t=g_sorted_region[_b];g_sorted_region[_b]=g_sorted_region[_b+1];g_sorted_region[_b+1]=_t;}
          g_fwd = 320 - bitobj->number_of_bits_left; g_rev = 319; g_sc_alt = 1; }`

const loopOldFormat = "    for (region=0; region<number_of_regions; region++)%s\n    {\n        category = (Word16)decoder_power_categories[region];"

const loopNew = `    for (_kk_=0; _kk_<number_of_regions; _kk_++)
    {
        if(sc_mode==1){ region=g_sorted_region[_kk_];
            if(g_fwd < SC_PROTECTED_BITS){ g_dir=0; } else { g_dir=g_sc_alt; g_sc_alt^=1; }
            g_dirs[region]=g_dir;
            g_starts[region] = (g_dir==0) ? g_fwd : g_rev; }
        else { region=_kk_;
            if(sc_mode==2) g_span[region] = 320 - bitobj->number_of_bits_left; }
        category = (Word16)decoder_power_categories[region];`

const (
	huffBitOld  = "    \t            get_next_bit(bitobj);"
	huffBitNew  = "    \t            if(sc_mode==1) sc_next_bit(bitobj); else get_next_bit(bitobj);"
	huffLeftOld = "\t                bitobj->number_of_bits_left = sub(bitobj->number_of_bits_left,1);"
	huffLeftNew = "\t                bitobj->number_of_bits_left = sc_mode==1 ? (g_rev-g_fwd+1) : sub(bitobj->number_of_bits_left,1);"
	signBitOld  = "\t\t                    get_next_bit(bitobj);"
	signBitNew  = "\t\t                    if(sc_mode==1) sc_next_bit(bitobj); else get_next_bit(bitobj);"
	signLeftOld = "\t\t                    bitobj->number_of_bits_left = sub(bitobj->number_of_bits_left,1);"
	signLeftNew = "\t\t                    bitobj->number_of_bits_left = sc_mode==1 ? (g_rev-g_fwd+1) : sub(bitobj->number_of_bits_left,1);"
)

const tailOld = `    test();
    if (ran_out_of_bits_flag)
        bitobj->number_of_bits_left = sub(bitobj->number_of_bits_left,1);`

const tailNew = `    if(sc_mode==2) sc_emit_mux(number_of_regions, 320 - bitobj->number_of_bits_left);
    if(sc_mode==1){
        const char *_p = getenv("STDT86_FE_OUT");
        if(_p){ static FILE *_fp = 0; static int _fr = 0;
            if(!_fp) _fp = fopen(_p, "w");
            { int _r; fprintf(_fp, "%d %d %d", _fr++, (int)ran_out_of_bits_flag,
                    g_rev - g_fwd + 1);
              for(_r=0;_r<number_of_regions;_r++)
                  fprintf(_fp, " %d", (int)decoder_power_categories[_r]);
              for(_r=0;_r<number_of_regions;_r++) fprintf(_fp, " %d", g_dirs[_r]);
              { int _g,_o=0; for(_g=g_fwd;_g<=g_rev;_g++) _o+=g_frame_bits[_g];
                fprintf(_fp, " %d %d %d", g_rev-g_fwd+1, _o, g_fwd);
                for(_g=0;_g<number_of_regions;_g++)
                    fprintf(_fp, " %d", (int)decoder_region_standard_deviation[_g]);
                for(_g=0;_g<number_of_regions;_g++) fprintf(_fp, " %d", g_starts[_g]); }
        { const char *_q = getenv("STDT86_MLT_OUT");
          if(_q){ static FILE *_fq = 0; int _g;
            if(!_fq) _fq = fopen(_q, "w");
            for(_g=0;_g<number_of_regions*REGION_SIZE;_g++)
                fprintf(_fq, "%d ", (int)decoder_mlt_coefs[_g]);
            fputc('\n', _fq); fflush(_fq); } }
              fprintf(_fp, "\n"); }
            fflush(_fp); } }
    test();
    if (ran_out_of_bits_flag)
        bitobj->number_of_bits_left = sub(bitobj->number_of_bits_left,1);
    if(sc_mode==1) bitobj->number_of_bits_left = 0;`

const mltAnchor = "        mag_shift = samples_to_rmlt_coefs(input, history, mlt_coefs, control.frame_size);"

const mltDump = `
        { const char *_q = getenv("STDT86_MLT_OUT");
          if(_q){ static FILE *_fq = 0; int _g;
            if(!_fq) _fq = fopen(_q, "w");
            fprintf(_fq, "%d ", (int)mag_shift);
            for(_g=0;_g<control.frame_size;_g++) fprintf(_fq, "%d ", (int)mlt_coefs[_g]);
            fputc(10, _fq); fflush(_fq); } }`
