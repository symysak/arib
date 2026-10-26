package main

import (
	"errors"
	"flag"
	"fmt"
	"github.com/symysak/arib/internal/bitpack"
	"github.com/symysak/arib/internal/cliflag"
	"github.com/symysak/arib/internal/credit"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/symysak/arib/internal/iqsrc"
	dec "github.com/symysak/arib/internal/std-t61/decoder"
	"github.com/symysak/arib/internal/std-t61/l1"
)

func main() {
	if len(os.Args) >= 2 {
		switch os.Args[1] {
		case "version", "-version", "--version":
			fmt.Print(credit.Text("std-t61"))
			os.Exit(0)
		}
	}
	if len(os.Args) >= 2 && os.Args[1] == "live" {
		os.Exit(cmdLive(os.Args[2:]))
	}
	os.Exit(cmdDecode(os.Args[1:]))
}

func cmdDecode(argv []string) int {
	fs := flag.NewFlagSet("std-t61", flag.ExitOnError)
	sampleRate := fs.Float64("fs", 0, "サンプルレート [Hz]（WAV なら省略可。tcp:// では必須）")
	format := fs.String("fmt", "s16", "tcp:// の標本形式（s16 / cf32 / cu8）")
	offset := fs.Float64("offset", 0, "チャネルオフセット [kHz]（信号中心が +f kHz にあるなら +f。±5 kHz 以内は起動時に自動で寄せる）")
	startSec := fs.Float64("start", 0, "開始位置 [s]")
	durSec := fs.Float64("duration", 0, "処理長 [s]（0 = 最後まで）")
	edition := fs.String("edition", "auto", "編の固定（auto / scpc / fdma / a1）。a1 = 第 1 編 付録 1（同期ワード SW1 は FDMA 編の S4 と同じ値なので、auto は付録 1 の RICH の CRC で決める）")
	scramble := fs.Int("scramble", 0, "スクランブル初期値（1〜511。0 = 全 1 = FDMA 基地局の既定）")
	quiet := fs.Bool("quiet", false, "フレーム毎の行を出さず集計だけ表示")
	all := fs.Bool("all", false, "同じ内容の連続フレームも畳まずに全行出す")
	tchOut := fs.String("tch", "", "音声フレーム（RICH M=音声）の TCH ビットの書き出し先（1 フレーム 32 バイト、MSB 先頭で詰める）")
	fs.Usage = func() {
		fmt.Fprint(os.Stderr, `ARIB STD-T61 受信デコーダ（SCPC / FDMA 共通）

  std-t61 <入力> [オプション]          1 チャネルを復号して一覧表示
  std-t61 live <入力> [...]             ライブ Web モニタと多局監視（1 局 / 多局 / 走査、-no-ui で画面なし。live -h）
  std-t61 version                      バージョンと作者表示

オプション:
`)
		fs.PrintDefaults()
		fmt.Fprintln(os.Stderr, "\n"+credit.Line())
	}
	if err := fs.Parse(cliflag.ReorderArgs(fs, argv)); err != nil {
		return 2
	}
	if fs.NArg() != 1 {
		fs.Usage()
		return 2
	}
	var ed l1.Edition
	switch strings.ToLower(*edition) {
	case "auto":
		ed = l1.EditionAuto
	case "scpc":
		ed = l1.EditionSCPC
	case "fdma":
		ed = l1.EditionFDMA
	case "a1":
		ed = l1.EditionA1
	default:
		fmt.Fprintln(os.Stderr, "-edition は auto / scpc / fdma / a1")
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
	var tchW io.WriteCloser
	if *tchOut != "" {
		f, err := os.Create(*tchOut)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		defer f.Close()
		tchW = f
	}
	fmt.Fprintf(os.Stderr, "入力: %s\n", src.Describe())
	d := dec.New(dec.Config{SampleRate: rate, OffsetHz: *offset * 1000, Edition: ed, Scramble: *scramble})
	pr := &printer{w: os.Stdout, quiet: *quiet, all: *all, t0: *startSec}
	chunk := int(rate * 0.1)
	var total int64
	limit := int64(*durSec * rate)
	for {
		if limit > 0 && total >= limit {
			break
		}
		iq, err := src.Read(chunk)
		if len(iq) > 0 {
			total += int64(len(iq))
			for _, f := range d.Feed(iq) {
				pr.frame(d, f)
				if tchW != nil && f.TCH != nil && f.RICH.IsVoice() && len(f.TCH) == 256 {
					tchW.Write(bitpack.ToBytes(f.TCH))
				}
			}
		}
		if err != nil {
			if !errors.Is(err, io.EOF) {
				fmt.Fprintln(os.Stderr, err)
			}
			break
		}
	}
	pr.flush()
	printSummary(d, rate, total)
	return 0
}

type printer struct {
	w         io.Writer
	quiet     bool
	all       bool
	t0        float64
	lastLine  string
	lastFirst dec.Frame
	repeat    int
}

func (p *printer) frame(d *dec.Decoder, f dec.Frame) {
	if p.quiet {
		return
	}
	body := describe(d, f)
	if !p.all && body == p.lastLine {
		p.repeat++
		return
	}
	p.flush()
	p.lastLine, p.lastFirst, p.repeat = body, f, 1
}

func (p *printer) flush() {
	if p.repeat == 0 {
		return
	}
	f := p.lastFirst
	rep := ""
	if p.repeat > 1 {
		rep = fmt.Sprintf(" ×%d", p.repeat)
	}
	fmt.Fprintf(p.w, "%9.3fs #%-5d %s%s\n", p.t0+f.TimeSec, f.Number, p.lastLine, rep)
	p.repeat = 0
}

func describe(d *dec.Decoder, f dec.Frame) string {
	var sb strings.Builder
	name := f.SW.Name
	if f.Flywheel {
		name = "(" + name + ")"
	}
	fmt.Fprintf(&sb, "%-5s", name)
	if f.SFPos >= 0 {
		fmt.Fprintf(&sb, " sf%02d", f.SFPos)
	} else {
		sb.WriteString(" sf--")
	}
	if r := f.A1RICH; r != nil {
		if r.OK {
			fmt.Fprintf(&sb, " 付録1 RICH[%09b] %s", r.Raw, r.Describe())
		} else {
			sb.WriteString(" 付録1 RICH NG")
		}
		fmt.Fprintf(&sb, " | TCH1 %X", bitpack.ToBytes(f.TCH1))
		return sb.String()
	}
	fmt.Fprintf(&sb, " RICH[%s] %s", f.RICH.Bits17(), f.RICH.Describe(d.Edition()))
	if f.Control != nil {
		u := f.Control
		if u.Uplink {
			unit := "第1"
			if u.Second {
				unit = "第2"
			}
			fmt.Fprintf(&sb, " | PCC↑%s %s W=%s %s", unit, okng(u.OK), u.W, l1.HexOctets(u.Data))
		} else {
			fmt.Fprintf(&sb, " | PCC %s W=%s %s E(%s)", okng(u.OK), u.W, l1.HexOctets(u.Data), u.EText())
		}
	}
	if f.SB != nil {
		fmt.Fprintf(&sb, " | SB %s %s raw=%s", okng(f.SB.OK), f.SB.Describe(d.Edition()), l1.HexOctets(f.SB.Raw))
	}
	if f.RCH != nil {
		fmt.Fprintf(&sb, " | RCH %s m=%.0f/40 %s (%02X)", unitStatus(f.RCH.OK, f.RCH.Corrected, f.RCH.Raw == 0), f.RCH.Metric, f.RCH.Describe(f.Uplink), f.RCH.Raw)
	}
	if f.SACCH != nil {
		fmt.Fprintf(&sb, " | SACCH %s m=%.0f/160 W=%s %s", unitStatus(f.SACCH.OK, f.SACCH.Corrected, l1.AllZero(f.SACCH.Data)), f.SACCH.Metric, f.SACCH.W, l1.HexOctets(f.SACCH.Data))
	}
	if f.FACCH != nil {
		fmt.Fprintf(&sb, " | FACCH %s m=%.0f/256 W=%s %s", unitStatus(f.FACCH.OK, f.FACCH.Corrected, l1.AllZero(f.FACCH.Data)), f.FACCH.Metric, f.FACCH.W, l1.HexOctets(f.FACCH.Data))
	}
	if f.NV2 != nil {
		fmt.Fprintf(&sb, " | 非音声2 OK W=%s %s", f.NV2.W, l1.HexOctets(f.NV2.Data))
	}
	if f.NV2Msg != nil {
		fmt.Fprintf(&sb, "\n      非音声2 %X [%s]", f.NV2Msg, printable(f.NV2Msg))
	}
	for _, m := range f.Messages {
		fmt.Fprintf(&sb, "\n      L2[%s] %X | %s | %s", m.Channel, m.L2.Raw, m.L2.Address, m.L2.Control)
		if m.L2.Err != "" {
			fmt.Fprintf(&sb, " ! %s (%X)", m.L2.Err, m.L2.Raw)
		} else if m.L3 == nil {
			if len(m.L2.Info) > 0 {
				fmt.Fprintf(&sb, " 余り=%X", m.L2.Info)
			}
		} else {
			fmt.Fprintf(&sb, " | L3 %s", m.L3.Summary())
			for _, msg := range m.L3.Messages {
				for i, fld := range msg.Fields {
					fmt.Fprintf(&sb, "\n        %s.%s: %s", msg.Entity, fld.Name, fld.Value)
					if n := msg.Note(i); n != "" {
						fmt.Fprintf(&sb, "  〔%s〕", n)
					}
				}
				if msg.Err != "" {
					fmt.Fprintf(&sb, "\n        %s ! %s (%X)", msg.Entity, msg.Err, msg.Raw)
				}
			}
			for _, n := range m.L3.Notes {
				fmt.Fprintf(&sb, "\n        注記: %s", n)
			}
		}
	}
	return sb.String()
}

func printable(b []byte) string {
	out := make([]byte, len(b))
	for i, c := range b {
		if c < 0x20 || c > 0x7e {
			c = '.'
		}
		out[i] = c
	}
	return string(out)
}

func okng(ok bool) string {
	if ok {
		return "OK"
	}
	return "NG"
}

func unitStatus(ok bool, corrected int, allZero bool) string {
	if ok {
		return "OK"
	}
	if l1.IsFiller(ok, corrected, allZero) {
		return "空き枠"
	}
	return "NG"
}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}

func printSummary(d *dec.Decoder, rate float64, total int64) {
	s := d.Stats
	fmt.Printf("\n== 集計 ==\n")
	fmt.Printf("処理 %.1f 秒  編=%s  粗捕捉 %+.0f Hz  残留CFO %+.0f Hz  同期外れ %d 回\n", float64(total)/rate, d.Edition(), d.CoarseHz(), d.CFOHz(), s.SyncLoss)
	fmt.Printf("フレーム %d（同期ワード検出 %d / 慣性 %d）  RICH CRC OK %d\n", s.Frames, s.SWDetected, s.Flywheel, s.RICHOK)
	names := make([]string, 0, len(s.SWCount))
	for k := range s.SWCount {
		names = append(names, k)
	}
	sort.Strings(names)
	for _, k := range names {
		fmt.Printf("  %-4s %d\n", k, s.SWCount[k])
	}
	if s.ControlUnits > 0 {
		fmt.Printf("制御CH ユニット %d / CRC16 OK %d\n", s.ControlUnits, s.ControlOK)
	}
	if s.UpControlUnits > 0 {
		fmt.Printf("上り制御CH ユニット %d / CRC16 OK %d\n", s.UpControlUnits, s.UpControlOK)
	}
	if s.NV2OK > 0 {
		fmt.Printf("非音声2 ユニット CRC16 OK %d\n", s.NV2OK)
	}
	if s.SBUnits > 0 {
		fmt.Printf("同期バースト %d / CRC6 OK %d\n", s.SBUnits, s.SBOK)
	}
	if s.RCHUnits > 0 || s.SACCHUnits > 0 || s.FACCHUnits > 0 {
		fmt.Printf("RCH %d/%d  SACCH %d/%d  FACCH %d/%d（OK/メッセージ。空き枠 RCH %d・SACCH %d・FACCH %d は別）\n",
			s.RCHOK, s.RCHUnits-s.RCHFiller, s.SACCHOK, s.SACCHUnits-s.SACCHFiller, s.FACCHOK, s.FACCHUnits-s.FACCHFiller,
			s.RCHFiller, s.SACCHFiller, s.FACCHFiller)
	}
	if s.L2Frames > 0 {
		fmt.Printf("レイヤ2 フレーム %d（形式異常 %d）  レイヤ3 メッセージ %d\n", s.L2Frames, s.L2Errors, s.L3Messages)
	}
}
