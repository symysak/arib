package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"math"
	"os"
	"strconv"
	"strings"

	"github.com/symysak/arib/internal/cliflag"
	"github.com/symysak/arib/internal/credit"
	"github.com/symysak/arib/internal/iqsrc"
	"github.com/symysak/arib/internal/rcr39qpsk/decoder"
	"github.com/symysak/arib/internal/rcr39qpsk/l1"
	"github.com/symysak/arib/internal/rcr39qpsk/l2"
	"github.com/symysak/arib/internal/std-39/qpsk"
	"github.com/symysak/arib/internal/std-39/qpsk/cli"
	"github.com/symysak/arib/internal/std-39/qpsk/l3"
	"github.com/symysak/arib/internal/std-39/qpsk/nonvoice"
)

const bin = "std-39-qpsk"

func main() {
	if len(os.Args) > 1 && (os.Args[1] == "version" || os.Args[1] == "-version") {
		fmt.Print(credit.Text(bin))
		return
	}
	if len(os.Args) > 1 && os.Args[1] == "live" {
		ctx, stop := interruptContext()
		code := cli.Live(ctx, bin+" live", os.Args[2:], os.Stderr)
		stop()
		os.Exit(code)
	}
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(argv []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet(bin, flag.ContinueOnError)
	fs.SetOutput(stderr)
	sampleRate := fs.Float64("fs", 0, "サンプルレート [Hz]（WAV なら省略可。tcp:// では必須）")
	format := fs.String("fmt", "s16", "tcp:// の標本形式（s16 / cf32 / cu8）")
	offset := fs.Float64("offset", 0, "チャネルオフセット [kHz]（信号中心が +f kHz にあるなら +f。±6 kHz 以内は起動時に自動で寄せる）")
	startSec := fs.Float64("start", 0, "開始位置 [s]")
	durSec := fs.Float64("duration", 0, "処理長 [s]（0 = 最後まで）")
	quiet := fs.Bool("quiet", false, "スロット毎の行を出さず、メッセージと集計だけ表示")
	all := fs.Bool("all", false, "同じ内容の連続スロットも畳まずに全行出す")
	upFlag := fs.Bool("up", false, "上り（移動局の送信）の録音として読む（STD-39 は上りと下りの周波数の対応を規定しないので、録音が上りかは利用者が決める。制御用のバーストはどのスロットでも試す）")
	nvFlag := fs.String("nonvoice", "", "非音声の参考規格の方式で読むスロット: <polling|contention>[:<スロット>,…]（polling = 付録 1 / contention = 付録 2。スロットを省くと ST#0〜3 全部 = 制御チャネルの無い通信キャリアの録音。指定したスロットは CAC として読まない）。省略時は無線チャネル指定か伝達能力が名乗ったスロットで読み始める（スクランブルは指定から、無ければ当てる）")
	foMHz := fs.Float64("fo", 0, "周波数コードの基準 Fo [MHz]（規格は「未定」。0 なら CAC の報知の制御チャネル構造情報から当てる）")
	fs.Usage = func() {
		fmt.Fprint(stderr, `ARIB RCR STD-39 第 2 編（π/4 シフト QPSK）受信デコーダ

  std-39-qpsk <入力> [オプション]
  std-39-qpsk live [オプション] -input <入力>[@<中心 MHz>]…   ライブ Web モニタ（std-39-qpsk live -h）

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
		fmt.Fprintln(stderr, err)
		return 1
	}
	defer src.Close()
	rate := src.SampleRate()
	if sk, ok := src.(iqsrc.Skipper); ok && *startSec > 0 {
		if err := sk.Skip(int64(*startSec * rate)); err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
	}
	fmt.Fprintf(stderr, "入力: %s\n", src.Describe())
	var feed func([]complex64) []decoder.Slot
	var setPacket func(slot int, on bool)
	var summary func(total int64) string
	if *upFlag {
		u := decoder.NewUp(decoder.UpConfig{SampleRate: rate, OffsetHz: *offset * 1000, StartSec: *startSec, AnyControl: true})
		feed, setPacket = u.Feed, u.SetPacket
		summary = func(total int64) string {
			st := u.Stats
			return fmt.Sprintf("処理 %.1f 秒（上り）: バースト %d 制御 %d（CAC %d）同期バースト %d/%d SACCH %d/%d FACCH %d 通信 %d",
				float64(total)/rate, st.Bursts, st.Control, st.CACOK, st.SBOK, st.SBTotal, st.SACCHOK, st.SACCHTotal, st.FACCHOK, st.Traffic)
		}
	} else {
		d := decoder.New(decoder.Config{SampleRate: rate, OffsetHz: *offset * 1000, StartSec: *startSec, Dialect: qpsk.Dialect()})
		feed, setPacket = d.Feed, d.SetPacket
		summary = func(total int64) string {
			st := d.Stats
			return fmt.Sprintf("処理 %.1f 秒: スロット %d（同期ワード %d）捕捉 %d 回 外れ %d 回 CAC %d/%d SACCH %d/%d FACCH %d 同期バースト %d/%d 搬送波ずれ %.0f Hz",
				float64(total)/rate, st.Slots, st.SWFound, st.Locks, st.Losses, st.CACOK, st.CACTotal, st.SACCHOK, st.SACCHTotal,
				st.FACCHOK, st.SBOK, st.SBTotal, d.CoarseHz()+d.CFOHz())
		}
	}
	var asm [4]struct{ sacch, facch l2.Reassembler }
	rd := newReader(*foMHz, cliflag.FreqFromName(fs.Arg(0)), *upFlag, setPacket)
	if *nvFlag != "" {
		method, slots, err := parseNonvoice(*nvFlag)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 2
		}
		rd.nvs.Restrict(slots)
		for _, n := range slots {
			rd.nvStart(n, method, -1, true)
		}
	}
	var last [4]string
	var rep [4]int
	var lastT [4]float64
	flush := func(n int) {
		if rep[n] > 1 {
			fmt.Fprintf(stdout, "%9.3fs ST#%d   ×%d（%.3fs まで同じ）\n", lastT[n], n, rep[n]-1, lastT[n])
		}
		rep[n] = 0
	}
	chunk := int(rate * 0.1)
	var total int64
	limit := int64(*durSec * rate)
	for limit <= 0 || total < limit {
		iq, err := src.Read(chunk)
		total += int64(len(iq))
		for _, s := range feed(iq) {
			if !*quiet {
				line := describe(s)
				if !*all && line == last[s.Num] {
					rep[s.Num]++
					lastT[s.Num] = s.TSec
				} else {
					flush(s.Num)
					fmt.Fprintf(stdout, "%9.3fs ST#%d %s\n", s.TSec, s.Num, line)
					last[s.Num], rep[s.Num] = line, 1
				}
			}
			for _, m := range messages(s, &asm[s.Num].sacch, &asm[s.Num].facch, rd) {
				fmt.Fprintf(stdout, "%9.3fs ST#%d   %s\n", s.TSec, s.Num, m)
			}
			for _, m := range rd.nvSlot(s, *quiet) {
				fmt.Fprintf(stdout, "%9.3fs ST#%d   %s\n", s.TSec, s.Num, m)
			}

		}
		if err != nil {
			if !errors.Is(err, io.EOF) {
				fmt.Fprintln(stderr, err)
			}
			break
		}
	}
	for n := range rep {
		flush(n)
	}
	fmt.Fprintln(stderr, summary(total))
	return 0
}

func describe(s decoder.Slot) string {
	kind := "通信"
	switch {
	case s.SW.Kind == l1.SWNone:
		return "（慣性）"
	case s.SW.Kind == l1.SWBurst:
		return fmt.Sprintf("%-4s 同期バースト", s.SW.Name())
	case s.Control:
		kind = "制御"
	case s.SW.Kind == l1.SWIdle:
		kind = "空線"
	}
	extra := ""
	switch {
	case s.Control && s.CACInit < 0:
		extra = " スクランブル 未確定"
	case s.Control && s.CACInit == l1.InitAllOnes:
		extra = " スクランブル オール 1"
	case s.Control && s.CACInit == l1.InitFromCC(ccOf(s)):
		extra = fmt.Sprintf(" スクランブル CC から 0x%03X", s.CACInit)
	case s.Control:
		extra = fmt.Sprintf(" スクランブル 0x%03X", s.CACInit)
	case s.SW.Kind != l1.SWIdle && s.InitKnown:
		extra = fmt.Sprintf(" スクランブル パターン 0x%02X", l1.PatternOf(s.Init))
	case s.SW.Kind != l1.SWIdle:
		extra = " スクランブル 未確定"
	}
	return fmt.Sprintf("%-4s %s CI %02b（%s）CC %d%s", s.SW.Name(), kind, s.CI, qpsk.CIName(s.CI), s.CC, extra)
}

func ccOf(s decoder.Slot) int {
	if s.CCDetected >= 0 {
		return s.CCDetected
	}
	return s.CC
}

func parseNonvoice(v string) (method string, slots []int, err error) {
	name, list, _ := strings.Cut(v, ":")
	switch name {
	case "polling":
		method = nonvoice.MethodPolling
	case "contention":
		method = nonvoice.MethodContention
	default:
		return "", nil, fmt.Errorf("-nonvoice は polling / contention[:<スロット>,…]: %q", v)
	}
	if list == "" {
		return method, []int{0, 1, 2, 3}, nil
	}
	for _, f := range strings.Split(list, ",") {
		n, e := strconv.Atoi(strings.TrimSpace(f))
		if e != nil || n < 0 || n > 3 {
			return "", nil, fmt.Errorf("-nonvoice のスロットは 0〜3: %q", f)
		}
		slots = append(slots, n)
	}
	return method, slots, nil
}

type l3Reader struct {
	fo, mhz float64
	fixed   bool
	up      bool
	nvs    nonvoice.Slots
	method string
	out    []string
	ctl    [4]bool
}

func newReader(fo, mhz float64, up bool, setPacket func(int, bool)) *l3Reader {
	r := &l3Reader{fo: fo, fixed: fo > 0, mhz: mhz, up: up}
	r.nvs.Mark = setPacket
	r.nvs.Log = func(_ int, text string) { r.out = append(r.out, text) }
	r.nvs.CtxOf = func() l3.Ctx { return l3.Ctx{Dir: r.dir(), FreqMHz: r.freq} }
	return r
}

func (r *l3Reader) nvStart(n int, method string, scramble int, sure bool) {
	if method == "" {
		return
	}
	r.method = method
	r.nvs.Start(n, method, scramble, sure, "録音")
}

func (r *l3Reader) nvStop(n int) { r.nvs.Stop(n) }

func (r *l3Reader) nvSlot(s decoder.Slot, quiet bool) []string {
	n := s.Num
	if n >= 0 && n < 4 && s.Control {
		r.ctl[n] = true
	}
	idle := s.SW.Kind == l1.SWIdle
	var fs []nonvoice.Frame
	if n >= 0 && n < 4 && !s.Control && len(s.Hard) >= l1.SlotBits && (idle || s.SW.Kind == l1.SWNormal || s.SW.Kind == l1.SWSuper) {
		if !idle && !r.nvs.Active(n) && r.up {
			r.nvStart(n, r.method, -1, false)
		}
		fs = r.nvs.Feed(n, r.up, idle, s.Hard, s.TSec)
	}
	out := r.out
	r.out = nil
	for _, f := range fs {
		if f.Kind == nonvoice.KindUnit && quiet {
			continue
		}
		out = append(out, "非音声 "+f.Text)
		if f.Kind == nonvoice.KindMessage {
			for _, x := range f.Fields {
				out = append(out, fmt.Sprintf("  %s = %s", x.Name, x.Value))
			}
		}
	}
	return out
}

func (r *l3Reader) dir() int {
	if r.up {
		return l3.SeqUp
	}
	return l3.SeqDown
}

func (r *l3Reader) freq(fc int) float64 {
	if r.fo == 0 || fc < 0 {
		return 0
	}
	return math.Round((r.fo+0.025*float64(fc))*1e6) / 1e6
}

func (r *l3Reader) lines(s decoder.Slot, ch string, info []byte) []string {
	sig := nonvoice.ParseSignal(info, l3.Ctx{Dir: r.dir(), FreqMHz: r.freq})
	if m := nonvoice.MethodFromSignal(sig); m != "" {
		r.method = m
		for _, msg := range sig.Messages {
			if a := msg.Assign; a != nil {
				same, known := false, a.MHz > 0 && r.mhz > 0
				if known {
					same = math.Abs(a.MHz-r.mhz) < 0.003
				}
				for _, n := range []int{a.Slot, a.Slot2} {
					switch {
					case n < 0 || n > 3 || (known && !same):
					case same:
						r.nvStart(n, m, a.Scramble, true)
					case !r.ctl[n]:
						r.nvStart(n, m, a.Scramble, false)
					}
				}
			}
		}
		if mc := nonvoice.MethodOnChannel(sig); mc != "" && ch != "CAC" {
			r.nvStart(s.Num, mc, -1, true)
		}
	} else {
		for _, msg := range sig.Messages {
			if a := msg.Assign; a != nil && a.MHz > 0 && r.mhz > 0 && math.Abs(a.MHz-r.mhz) < 0.003 {
				for _, n := range []int{a.Slot, a.Slot2} {
					r.nvStop(n)
				}
			}
		}
	}
	if ch != "CAC" {
		r.nvs.Observe(s.Num, sig)
	}
	out := r.out
	r.out = nil
	if sig.Err != "" {
		out = append(out, "        L3 の誤り: "+sig.Err)
	}
	for _, m := range sig.Messages {
		line := fmt.Sprintf("        L3 %s 0x%02X %s", m.Entity, m.Type, m.Name)
		if m.Err != "" {
			line += "（" + m.Err + "）"
		}
		out = append(out, line)
		for _, f := range m.Fields {
			out = append(out, fmt.Sprintf("          %s = %s", f.Name, f.Value))
		}
		if b := m.Broadcast; b != nil && b.Structure != nil && ch == "CAC" && !r.fixed && r.mhz > 0 {
			st := b.Structure
			for i, fc := range st.FC {
				slot := -1
				if i < len(st.Slots) {
					slot = st.Slots[i]
				}
				if (len(st.FC) == 1 && slot < 0) || slot == s.Num {
					if fo := math.Round((r.mhz-0.025*float64(fc))*1e4) / 1e4; fo != r.fo {
						r.fo = fo
						out = append(out, fmt.Sprintf("        周波数コードの基準 Fo = %.4f MHz（制御チャネル構造情報の F%d = %d がこの録音の %.4f MHz）", fo, i+1, fc, r.mhz))
					}
					break
				}
			}
		}
	}
	return out
}

func messages(s decoder.Slot, sacch, facch *l2.Reassembler, rd *l3Reader) []string {
	var out []string
	hexOf := func(b []byte) string { return strings.ToUpper(fmt.Sprintf("% x", b)) }
	frame := func(ch string, b []byte) {
		dir := l2.DirDown
		if rd != nil && rd.up {
			dir = l2.DirUp
		}
		f := l2.ParseDir(b, dir)
		out = append(out, fmt.Sprintf("%-5s %s", ch, f.Summary()))
		if len(f.Info) > 0 {
			out = append(out, "        情報 "+hexOf(f.Info))
			if f.CarriesInfo() && rd != nil {
				out = append(out, rd.lines(s, strings.Fields(ch)[0], f.Info)...)
			}
		}
		if f.Err != "" {
			out = append(out, "        L2 の誤り: "+f.Err)
		}
	}
	unit := func(ch string, u l1.Unit, r *l2.Reassembler) {
		out = append(out, fmt.Sprintf("%-5s ユニット %s %s", ch, u.W, hexOf(u.Bytes())))
		if m := r.Push(l2.Unit{First: u.W.First, Last: u.W.Last, WO: u.W.WO, Data: u.Bytes()}); m != nil {
			frame(ch, m)
		}
	}
	if s.Message != nil {
		frame("CAC", s.Message)
	}
	if s.SB != nil && s.SB.OK {
		out = append(out, fmt.Sprintf("同期バースト CI %02b CC %d B %d TA %d SSC %d 呼出符号 %d", s.SB.CI, s.SB.CC, s.SB.B, s.SB.TA, s.SB.SSC, s.SB.IDNumber()))
	}
	if s.SACCH != nil {
		unit("SACCH", *s.SACCH, sacch)
	}
	if s.FACCH != nil {
		unit("FACCH", *s.FACCH, facch)
	}
	for _, l := range s.FACCHLate {
		unit(fmt.Sprintf("FACCH（%.3fs から遡って）", l.TSec), l.Unit, facch)
	}
	if s.RCH != nil && s.RCH.OK {
		out = append(out, fmt.Sprintf("RCH   %v", s.RCH.Info))
	}
	return out
}
