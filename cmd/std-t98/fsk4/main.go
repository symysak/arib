package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/symysak/arib/internal/cliflag"
	"github.com/symysak/arib/internal/credit"
	"github.com/symysak/arib/internal/iqsrc"
	"github.com/symysak/arib/internal/nxdn/decoder"
	nxl1 "github.com/symysak/arib/internal/nxdn/l1"
	"github.com/symysak/arib/internal/std-t98/fsk4/cli"
	"github.com/symysak/arib/internal/std-t98/fsk4/l1"
	"github.com/symysak/arib/internal/std-t98/fsk4/l3"
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
	p := &printer{asm: l3.NewAsm(), quiet: *quiet, all: *all}
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
	ps := nxl1.Parse(nxl1.Dewhiten(f.Body[:], l1.WhiteningInit), l1.Dialect)
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
			if u.First {
				call, uc, maker := l3.First(s.Data[:])
				line += fmt.Sprintf(" UC %d 製造者 %d %s", uc, maker, l3.CallName(call))
			} else {
				line += fmt.Sprintf("（続き・残り %d）", u.Remain)
			}
			if m, ok := p.asm.Push(s.Head, s.Data[:]); ok {
				out = append(out, "SACCH "+m.Summary())
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

func (p *printer) summary() {
	p.flush()
	fmt.Fprintf(os.Stderr, "フレーム %d・RICH が規定どおり %d・CRC %d/%d・メッセージ %d\n", p.frames, p.rich, p.crc, p.crcAll, p.msgs)
}
