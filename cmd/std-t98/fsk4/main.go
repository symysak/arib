package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/symysak/arib/internal/cliflag"
	"github.com/symysak/arib/internal/credit"
	"github.com/symysak/arib/internal/iqsrc"
	"github.com/symysak/arib/internal/nxdn/decoder"
	nxl1 "github.com/symysak/arib/internal/nxdn/l1"
	"github.com/symysak/arib/internal/std-t98/fsk4/cli"
	"github.com/symysak/arib/internal/std-t98/fsk4/l1"
	"github.com/symysak/arib/internal/std-t98/fsk4/l3"
	t98 "github.com/symysak/arib/internal/std-t98/fsk4/server"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "live" {
		ctx, stop := interruptContext()
		code := cli.Live(ctx, "std-t98-fsk4 live", os.Args[2:], os.Stderr)
		stop()
		os.Exit(code)
	}
	if len(os.Args) > 1 && (os.Args[1] == "version" || os.Args[1] == "-version") {
		fmt.Print(credit.Text("std-t98-fsk4"))
		return
	}
	os.Exit(run(os.Args[1:]))
}

func run(argv []string) int {
	fs := flag.NewFlagSet("std-t98-fsk4", flag.ContinueOnError)
	sampleRate := fs.Float64("fs", 0, "サンプルレート [Hz]（WAV なら省略可。tcp:// と .cu8 では必須）")
	format := fs.String("fmt", "s16", "tcp:// の標本形式（s16 / cf32 / cu8）")
	offset := fs.Float64("offset", 0, "チャネルオフセット [kHz]（信号中心が +f kHz にあるなら +f）")
	startSec := fs.Float64("start", 0, "開始位置 [s]")
	durSec := fs.Float64("duration", 0, "処理長 [s]（0 = 最後まで）")
	quiet := fs.Bool("quiet", false, "フレーム毎の行を出さず、メッセージと集計だけ表示")
	all := fs.Bool("all", false, "同じ内容の連続フレームも畳まずに全行出す")
	initStr := fs.String("init", "", "ホワイトニングの初期値（WC。0x001〜0x1FF か 1〜511。既定は規格の値 0x0E4）")
	var keyList []string
	fs.Func("key", "秘話鍵（<鍵 1〜32767> か <ユーザコード>:<鍵>。何度でも。非音声 2 を外すのに使う）", func(v string) error {
		keyList = append(keyList, v)
		return nil
	})
	fs.Usage = func() {
		fmt.Fprint(os.Stderr, `ARIB STD-T98 第 3 編（デジタル簡易無線・4 値 FSK）受信デコーダ

  std-t98-fsk4 <入力> [オプション]
  std-t98-fsk4 live [オプション] -input <入力>[@<中心 MHz>][,gain=<dB>]…   ライブ Web モニタ（std-t98-fsk4 live -h）

オプション:
`)
		fs.PrintDefaults()
	}
	if err := fs.Parse(cliflag.ReorderArgs(fs, argv)); err != nil {
		return 2
	}
	if fs.NArg() != 1 {
		fs.Usage()
		return 2
	}
	src, err := iqsrc.Open(fs.Arg(0), iqsrc.Options{SampleRate: *sampleRate, Format: *format})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer src.Close()
	rate := src.SampleRate()
	if sk, ok := src.(iqsrc.Skipper); ok && *startSec > 0 {
		if err := sk.Skip(int64(*startSec * rate)); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
	}
	fmt.Fprintf(os.Stderr, "入力: %s\n", src.Describe())
	d := decoder.New(decoder.Config{SampleRate: rate, OffsetHz: *offset * 1000, StartSec: *startSec})
	keys, err := t98.ParseKeys(keyList)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	init := l1.WhiteningInit
	if *initStr != "" {
		n, err := strconv.ParseUint(strings.TrimSpace(*initStr), 0, 16)
		if err != nil || n < 1 || n > 0x1FF {
			fmt.Fprintf(os.Stderr, "-init %q は 0x001〜0x1FF\n", *initStr)
			return 2
		}
		init = int(n)
	}
	p := &printer{asm: l3.NewAsm(), quiet: *quiet, all: *all, init: init, keys: keys, makers: map[int]l3.MakerReader{},
		ctx: l3.MakerCtx{Maker: -1, UC: -1, Call: -1}, pend: [3]int{-1, -1, -1}}
	chunk := int(rate * 0.1)
	var total int64
	limit := int64(*durSec * rate)
	for limit <= 0 || total < limit {
		iq, err := src.Read(chunk)
		total += int64(len(iq))
		for _, f := range d.Feed(iq) {
			p.frame(f)
		}
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
	}
	for _, f := range d.Flush() {
		p.frame(f)
	}
	p.summary()
	return 0
}

type printer struct {
	asm        *l3.Asm
	quiet, all bool
	last       string
	rep        int
	lastT      float64
	frames     int
	rich, crc  int
	crcAll     int
	msgs       int
	init   int
	keys   map[int]int
	found  map[int]int
	makers map[int]l3.MakerReader
	ctx  l3.MakerCtx
	pend [3]int
}

func (p *printer) flush() {
	if p.rep > 1 {
		fmt.Printf("%9.3fs   ×%d（%.3fs まで同じ）\n", p.lastT, p.rep-1, p.lastT)
	}
	p.rep = 0
}

func (p *printer) frame(f decoder.Frame) {
	p.frames++
	if f.Inertia {
		return
	}
	if f.First {
		p.txStart()
	}
	ps := nxl1.Parse(nxl1.Dewhiten(f.Body[:], p.init), l1.Dialect)
	r := l1.ReadRICH(ps.RICH)
	mode := r.Mode()
	if ps.Layout.Valid {
		p.rich++
	}
	line := fmt.Sprintf("FSW誤り %d RICH %s", f.FSWErr, r)
	var out []string
	if s := ps.SACCH; s != nil {
		p.crcAll++
		if s.CRCOK {
			p.crc++
			u := l1.ReadUnit(s.Head)
			line += fmt.Sprintf(" | SACCH %s", l3.TypeName(u.Type))
			trust := true
			if u.First {
				call, uc, maker := l3.First(s.Data[:])
				line += fmt.Sprintf(" UC %d 製造者 %d %s", uc, maker, l3.CallName(call))
				if trust = p.accept(ps, mode, u, call, uc, maker); !trust {
					line += "（1 回だけ。まだ信じない）"
				}
			} else {
				line += fmt.Sprintf("（続き・残り %d）", u.Remain)
			}
			if !trust {
				p.asm.Reset()
			} else if m, ok := p.asm.Push(s.Head, s.Data[:]); ok {
				out = append(out, "SACCH "+m.Summary())
				if r := p.maker(m.Maker); r != nil {
					if info, ok := r.SACCH(m); ok {
						out = append(out, "  製造者定義 "+info.Summary)
					}
				}
			}
		} else {
			line += " | SACCH CRC誤り"
			p.asm.Reset()
		}
	}
	voice := false
	for i, h := range ps.Half {
		name := mode.HalfName(i)
		switch {
		case h.Coded != nil:
			p.crcAll++
			if !h.Coded.CRCOK {
				line += " | " + name + " CRC誤り"
				continue
			}
			p.crc++
			b := nxl1.CodedBytes(h.Coded)
			if mode == l1.ModeSync {
				pi := l3.ParsePICH(b)
				line += " | PICH " + pi.CSM
				out = append(out, "PICH "+pi.Summary())
			} else {
				line += fmt.Sprintf(" | %s 非音声 2 % X", name, b)
				out = append(out, fmt.Sprintf("%s 非音声 2 % X", name, b))
				out = append(out, p.nv2(b, i)...)
			}
		case h.Kind == nxl1.HalfTCH && h.TCH != nil:
			line += " | " + name + " 音声"
			voice = true
		}
	}
	if !p.quiet {
		key := line
		if voice {
			key = fmt.Sprintf("%s|%v", line, len(out))
		}
		if !p.all && key == p.last {
			p.rep++
			p.lastT = f.TimeSec
			return
		}
		p.flush()
		fmt.Printf("%9.3fs %s\n", f.TimeSec, line)
		p.last, p.rep = key, 1
	}
	for _, o := range out {
		p.msgs++
		fmt.Printf("%9.3fs   %s\n", f.TimeSec, o)
	}
}

func (p *printer) accept(ps nxl1.Parsed, mode l1.Mode, u l1.Unit, call, uc, maker int) bool {
	if call > l3.CallCipher || !(u.Type <= l3.TypeVoice || l3.IsMakerDef(u.Type) || u.Type == l3.TypeEnd) {
		return false
	}
	now := [3]int{call, uc, maker}
	prev := p.pend
	p.pend = now
	ok := now == [3]int{p.ctx.Call, p.ctx.UC, p.ctx.Maker} || now == prev || (mode == l1.ModeIdle && u.Type == l3.TypeEnd)
	for _, h := range ps.Half {
		ok = ok || (h.Coded != nil && h.Coded.CRCOK)
	}
	if ok {
		p.ctx = l3.MakerCtx{Maker: maker, UC: uc, Call: call}
	}
	return ok
}

func (p *printer) txStart() {
	p.ctx = l3.MakerCtx{Maker: -1, UC: -1, Call: -1}
	p.pend = [3]int{-1, -1, -1}
	p.asm.Reset()
	for _, r := range p.makers {
		r.TxStart()
	}
}

func (p *printer) maker(n int) l3.MakerReader {
	f, ok := l3.LookupMaker(n)
	if !ok {
		return nil
	}
	if p.makers[n] == nil {
		p.makers[n] = f.New()
	}
	return p.makers[n]
}

func (p *printer) nv2(b [10]byte, half int) []string {
	if len(l3.Makers()) == 0 {
		return nil
	}
	var out []string
	if p.ctx.Call == l3.CallCipher {
		key, ok := p.keys[p.ctx.UC]
		if !ok {
			key, ok = p.keys[t98.AnyUC]
		}
		if !ok {
			key, ok = p.found[p.ctx.UC]
		}
		if !ok {
			n, f, hit := l3.FindNV2Key(b, half, p.ctx)
			if !hit {
				return nil
			}
			if p.found == nil {
				p.found = map[int]int{}
			}
			key, p.found[p.ctx.UC] = n, n
			out = append(out, fmt.Sprintf("  秘話鍵 %d を非音声 2 の 1 枚から当てた（UC %d・%s）", n, p.ctx.UC, f.Format))
		}
		b = l3.DescrambleNV2(b, key, half)
		out = append(out, fmt.Sprintf("  外した非音声 2 % X（秘話鍵 %d）", b, key))
	}
	try := func(n int) bool {
		r := p.maker(n)
		if r == nil {
			return false
		}
		info, ok := r.NV2(b, p.ctx)
		if ok {
			out = append(out, "  製造者定義 "+info.Summary)
		}
		return ok
	}
	if !try(p.ctx.Maker) {
		for _, f := range l3.Makers() {
			if f.Maker != p.ctx.Maker && try(f.Maker) {
				break
			}
		}
	}
	return out
}

func (p *printer) summary() {
	p.flush()
	fmt.Fprintf(os.Stderr, "フレーム %d・RICH が規定どおり %d・CRC %d/%d・メッセージ %d\n", p.frames, p.rich, p.crc, p.crcAll, p.msgs)
}
