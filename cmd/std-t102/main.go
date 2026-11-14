package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/symysak/arib/internal/cliflag"
	"github.com/symysak/arib/internal/credit"
	"github.com/symysak/arib/internal/data"
	"github.com/symysak/arib/internal/iqsrc"
	"github.com/symysak/arib/internal/nxdn/decoder"
	nxl1 "github.com/symysak/arib/internal/nxdn/l1"
	"github.com/symysak/arib/internal/std-t102/cli"
	"github.com/symysak/arib/internal/std-t102/l1"
	"github.com/symysak/arib/internal/std-t102/l3"
	"github.com/symysak/arib/internal/std-t102/server"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "live" {
		ctx, stop := interruptContext()
		code := cli.Live(ctx, "std-t102 live", os.Args[2:], os.Stderr)
		stop()
		os.Exit(code)
	}
	if len(os.Args) > 1 && (os.Args[1] == "version" || os.Args[1] == "-version") {
		fmt.Print(credit.Text("std-t102"))
		return
	}
	os.Exit(run(os.Args[1:]))
}

func run(argv []string) int {
	fs := flag.NewFlagSet("std-t102", flag.ContinueOnError)
	sampleRate := fs.Float64("fs", 0, "サンプルレート [Hz]（WAV なら省略可。tcp:// と .cu8 では必須）")
	format := fs.String("fmt", "s16", "tcp:// の標本形式（s16 / cf32 / cu8）")
	offset := fs.Float64("offset", 0, "チャネルオフセット [kHz]（信号中心が +f kHz にあるなら +f）")
	startSec := fs.Float64("start", 0, "開始位置 [s]")
	durSec := fs.Float64("duration", 0, "処理長 [s]（0 = 最後まで）")
	initStr := fs.String("init", "", "ホワイトニングの初期値（0〜511。空なら CRC で当てる）")
	quiet := fs.Bool("quiet", false, "フレーム毎の行を出さず、メッセージ・データと集計だけ表示")
	all := fs.Bool("all", false, "同じ内容の連続フレームも畳まずに全行出す")
	dataFmt := fs.String("data", data.Auto, "データの書式（auto / off / "+data.Names(l1.DataChannel)+"）")
	var b54Keys []string
	fs.Func("b54-key", "ARIB STD-B54 のスクランブル暗号の鍵 <鍵番号 1〜63>=<暗号鍵 1〜32767>（何度でも。無ければデータは CRC で当てる）",
		func(s string) error { b54Keys = append(b54Keys, s); return nil })
	fs.Usage = func() {
		fmt.Fprint(os.Stderr, `ARIB STD-T102 第 2 編 受信デコーダ

  std-t102 <入力> [オプション]
  std-t102 live [オプション] -input <入力>[@<中心 MHz>][,gain=<dB>]…   ライブ Web モニタ（std-t102 live -h）

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
	fixed := -1
	if *initStr != "" {
		v, err := server.ParseInit(*initStr)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 2
		}
		fixed = v
	}
	if err := data.Valid(l1.DataChannel, *dataFmt); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	keys, err := server.ParseB54Keys(b54Keys)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
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
	p := newPrinter(fixed, *dataFmt, *quiet, *all)
	p.b54 = l3.NewB54Asm(keys)
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
	for _, e := range p.b54.Flush() {
		fmt.Printf("%9.3fs   B54 %s\n", p.lastT, e.Label())
	}
	p.summary()
	return 0
}

type printer struct {
	keyer      *nxl1.Keyer
	pend       []decoder.Frame
	sacch      l3.SACCH
	data       *data.Station
	b54        *l3.B54Asm
	quiet, all bool
	last       string
	rep        int
	lastT      float64
	frames     int
	rich, crc  int
	crcAll     int
	msgs       int
	init       int
}

func newPrinter(fixed int, dataFmt string, quiet, all bool) *printer {
	return &printer{keyer: nxl1.NewKeyer(nxl1.KeyerConfig{Dialect: l1.Dialect, Fixed: fixed, Hints: server.DefaultInits}), data: data.NewStation(l1.DataChannel, dataFmt),
		quiet: quiet, all: all, init: -1}
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
	kds := p.keyer.Push(f.Body[:])
	p.pend = append(p.pend, f)
	if len(p.pend) > 25 {
		p.pend = p.pend[len(p.pend)-25:]
	}
	if len(kds) == 0 {
		return
	}
	if init, ok := p.keyer.Init(); ok && init != p.init {
		p.init = init
		fmt.Printf("# ホワイトニングの初期値 0x%03X（%s）\n", init, p.keyer.Why())
	}
	fs := p.pend[len(p.pend)-len(kds):]
	p.pend = nil
	for i, kd := range kds {
		p.keyed(fs[i], kd)
	}
}

func (p *printer) keyed(f decoder.Frame, kd nxl1.Keyed) {
	ps := kd.Parsed
	r := l1.ReadRICH(ps.RICH)
	if ps.Layout.Valid {
		p.rich++
	}
	line := fmt.Sprintf("FSW誤り %d RICH %s", f.FSWErr, r)
	var out []string
	msg := func(ch string, m l3.Message) {
		out = append(out, ch+" "+m.Summary())
		for _, e := range p.b54.Push(m) {
			out = append(out, "B54 "+e.Label())
		}
	}
	if s := ps.SACCH; s != nil {
		p.crcAll++
		if s.CRCOK {
			p.crc++
			ss, u := l1.SU(s.Head)
			line += fmt.Sprintf(" | SACCH S=%02b U=%d", ss, u)
			if b, ok := p.sacch.Push(r.Superframe(), ss, u, s.Data[:]); ok {
				if !r.Superframe() {
					b = b[:2]
				}
				msg("SACCH", l3.Parse("SACCH", b))
			}
		} else {
			line += " | SACCH CRC誤り"
			p.sacch.Reset()
		}
	}
	for i, h := range ps.Half {
		switch {
		case h.Coded != nil:
			p.crcAll++
			if h.Coded.CRCOK {
				p.crc++
				b := nxl1.CodedBytes(h.Coded)
				line += fmt.Sprintf(" | FACCH1 % X", b)
				if i == 0 || len(out) == 0 {
					msg("FACCH1", l3.Parse("FACCH1", b[:]))
				}
			} else {
				line += " | FACCH1 CRC誤り"
			}
		case h.Kind == nxl1.HalfTCH && h.TCH != nil:
			line += " | TCH"
		}
	}
	if dd := ps.Data; dd != nil {
		p.crcAll++
		kind := r.DataKind()
		_, u := l1.SU(dd.Head)
		if dd.CRCOK {
			p.crc++
			line += fmt.Sprintf(" | %s U=%d % X", kind, u, dd.Body)
			if m := l3.Parse(kind.String(), dd.Body[:]); kind == nxl1.DataFACCH2 || m.B54 != nil {
				msg(kind.String(), m)
			}
			bits := append(nxl1.HeadInfo(dd.Head, make([]uint8, 18))[:8], nxl1.BytesToBits(dd.Body[:])...)
			for _, ev := range p.data.Read(data.Frame{Ch: l1.DataChannel, No: f.No, TimeSec: f.TimeSec, Bits: bits, Route: kind.String()}) {
				out = append(out, "データ "+ev.Label)
			}
		} else {
			line += " | " + kind.String() + " CRC誤り"
		}
	}
	if !p.quiet {
		key := line
		if ps.Format == nxl1.FormatVoice {
			key = fmt.Sprintf("%s|%v", line, len(out))
		}
		if !p.all && key == p.last && len(out) == 0 {
			p.rep++
			p.lastT = f.TimeSec
		} else {
			p.flush()
			fmt.Printf("%9.3fs %s\n", f.TimeSec, line)
			p.last, p.rep = key, 1
		}
	}
	for _, o := range out {
		p.msgs++
		fmt.Printf("%9.3fs   %s\n", f.TimeSec, o)
	}
}

func (p *printer) summary() {
	p.flush()
	fmt.Fprintf(os.Stderr, "フレーム %d・RICH が規定どおり %d・CRC %d/%d・メッセージとデータ %d\n", p.frames, p.rich, p.crc, p.crcAll, p.msgs)
}
