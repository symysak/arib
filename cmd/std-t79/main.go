package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/symysak/arib/internal/cliflag"
	"github.com/symysak/arib/internal/credit"
	"github.com/symysak/arib/internal/iqsrc"
	"github.com/symysak/arib/internal/std-t79/cli"
	"github.com/symysak/arib/internal/std-t79/decoder"
	"github.com/symysak/arib/internal/std-t79/l1"
	"github.com/symysak/arib/internal/std-t79/l2"
	"github.com/symysak/arib/internal/std-t79/l3"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "live" {
		ctx, stop := interruptContext()
		code := cli.Live(ctx, "std-t79 live", os.Args[2:], os.Stderr)
		stop()
		os.Exit(code)
	}
	if len(os.Args) > 1 && (os.Args[1] == "version" || os.Args[1] == "-version") {
		fmt.Print(credit.Text("std-t79"))
		return
	}
	os.Exit(run(os.Args[1:]))
}

func run(argv []string) int {
	fs := flag.NewFlagSet("std-t79", flag.ContinueOnError)
	sampleRate := fs.Float64("fs", 0, "サンプルレート [Hz]（WAV なら省略可。tcp:// では必須）")
	format := fs.String("fmt", "s16", "tcp:// の標本形式（s16 / cf32 / cu8）")
	offset := fs.Float64("offset", 0, "チャネルオフセット [kHz]（信号中心が +f kHz にあるなら +f。±6 kHz 以内は起動時に自動で寄せる）")
	startSec := fs.Float64("start", 0, "開始位置 [s]")
	durSec := fs.Float64("duration", 0, "処理長 [s]（0 = 最後まで）")
	quiet := fs.Bool("quiet", false, "スロット毎の行を出さず、メッセージと集計だけ表示")
	all := fs.Bool("all", false, "同じ内容の連続スロットも畳まずに全行出す")
	maker := fs.Int("maker", 0, "網の製造者番号（独自の読みに使う。0 = 報知情報・システム情報の網番号から決める）")
	fs.Usage = func() {
		fmt.Fprint(os.Stderr, `ARIB STD-T79 受信デコーダ

  std-t79 <入力> [オプション]
  std-t79 live [オプション] -input <入力>[@<中心 MHz>][,gain=<dB>]…   ライブ Web モニタ（std-t79 live -h）

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
	asm := assembler{maker: *maker}
	var last [4]string
	var rep [4]int
	var lastT [4]float64
	flush := func(n int) {
		if rep[n] > 1 {
			fmt.Printf("%9.3fs ST#%d   ×%d（%.3fs まで同じ）\n", lastT[n], n, rep[n]-1, lastT[n])
		}
		rep[n] = 0
	}
	chunk := int(rate * 0.1)
	var total int64
	limit := int64(*durSec * rate)
	for limit <= 0 || total < limit {
		iq, err := src.Read(chunk)
		total += int64(len(iq))
		for _, s := range d.Feed(iq) {
			line := describe(s)
			if !*quiet {
				if !*all && line == last[s.Num] {
					rep[s.Num]++
					lastT[s.Num] = s.TSec
				} else {
					flush(s.Num)
					fmt.Printf("%9.3fs ST#%d %s\n", s.TSec, s.Num, line)
					last[s.Num], rep[s.Num] = line, 1
				}
			}
			for _, m := range messages(s, &asm) {
				fmt.Printf("%9.3fs ST#%d   %s\n", s.TSec, s.Num, m)
			}
		}
		if err != nil {
			if !errors.Is(err, io.EOF) {
				fmt.Fprintln(os.Stderr, err)
			}
			break
		}
	}
	for n := range rep {
		flush(n)
	}
	st := d.Stats
	fmt.Fprintf(os.Stderr, "処理 %.1f 秒: スロット %d（同期ワード %d）捕捉 %d 回 外れ %d 回 CAC %d/%d SACCH %d/%d FACCH %d 同期バースト %d/%d 搬送波ずれ %.0f Hz\n",
		float64(total)/rate, st.Slots, st.SWFound, st.Locks, st.Losses, st.CACOK, st.CACTotal, st.SACCHOK, st.SACCHTotal,
		st.FACCHOK, st.SBOK, st.SBTotal, d.CoarseHz()+d.CFOHz())
	return 0
}

func describe(s decoder.Slot) string {
	kind := "通信"
	switch {
	case s.SW.Kind == l1.SWNone:
		return "（慣性）"
	case s.Control:
		kind = "制御"
	case s.SW.Kind == l1.SWBurst:
		return fmt.Sprintf("%-4s 同期バースト", s.SW.Name())
	}
	scr := "未確定"
	if s.InitKnown {
		scr = fmt.Sprintf("パターン 0x%02X", l1.PatternOf(s.Init))
	}
	extra := ""
	if !s.Control && s.SW.Kind != l1.SWIdle {
		extra = " スクランブル " + scr
	}
	return fmt.Sprintf("%-4s %s CI %02b CC %d%s", s.SW.Name(), kind, s.CI, s.CC, extra)
}

type assembler struct {
	sacch, facch [4]l2.Reassembler
	maker        int
}

func (a *assembler) parse(info []byte) l3.Signal {
	sig := l3.ParseCtx(info, l3.Ctx{Dir: 1, Maker: a.maker})
	if a.maker != 0 {
		return sig
	}
	for _, m := range sig.Messages {
		if b := m.Broadcast; b != nil && b.System != nil && b.System.Maker > 0 {
			a.maker = b.System.Maker
			fmt.Fprintf(os.Stderr, "網の製造者 %d %s（網番号から）\n", a.maker, l3.MakerName(a.maker))
			return l3.ParseCtx(info, l3.Ctx{Dir: 1, Maker: a.maker})
		}
	}
	return sig
}

func messages(s decoder.Slot, a *assembler) []string {
	var out []string
	hexOf := func(b []byte) string { return strings.ToUpper(fmt.Sprintf("% x", b)) }
	frame := func(ch string, b []byte) {
		f := l2.Parse(b)
		line := fmt.Sprintf("%-5s %s", ch, f.Summary())
		if f.CarriesInfo() && len(f.Info) > 0 {
			sig := a.parse(f.Info)
			line += "  " + sig.Summary()
			out = append(out, line)
			for _, m := range sig.Messages {
				for _, fd := range m.Fields {
					out = append(out, fmt.Sprintf("        %s %s: %s", m.Entity, fd.Name, fd.Value))
				}
				if m.Err != "" {
					out = append(out, fmt.Sprintf("        %s 誤り: %s", m.Entity, m.Err))
				}
			}
			out = append(out, "        "+hexOf(b))
			return
		}
		out = append(out, line+"  "+hexOf(b))
	}
	if s.Message != nil {
		frame("CAC", s.Message)
	}
	if u := s.SACCH; u != nil {
		if b := a.sacch[s.Num].Push(l2.Unit{First: u.W.First, Last: u.W.Last, WO: u.W.WO, Data: u.Bytes()}); b != nil {
			frame("SACCH", b)
		}
	}
	if u := s.FACCH; u != nil {
		if b := a.facch[s.Num].Push(l2.Unit{First: u.W.First, Last: u.W.Last, WO: u.W.WO, Data: u.Bytes()}); b != nil {
			frame("FACCH", b)
		}
	}
	if s.SB != nil {
		sb := s.SB
		ok := "CRC NG"
		if sb.OK {
			ok = "CRC OK"
		}
		out = append(out, fmt.Sprintf("SB    %s CI %02b CC %d B %d TA %d SSC %d ID %d（識別子 %d・個別番号 %d）",
			ok, sb.CI, sb.CC, sb.B, sb.TA, sb.SSC, sb.ID, sb.ID>>19, sb.IDNumber()))
	}
	return out
}
