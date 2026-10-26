package cliflag

import (
	"errors"
	"flag"
	"fmt"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/symysak/arib/internal/iqsrc"
	"github.com/symysak/arib/internal/notice"
	"github.com/symysak/arib/internal/rec"
	"github.com/symysak/arib/internal/rx"
)

type Live struct {
	SampleRate float64
	Format     string
	FreqMHz    float64
	Channels   string
	Scan       rx.ScanConfig
	LogDir     string
	Addr       string
	Ledger     string
	System    string
	FullSpeed bool
	Speed     float64
	GainDB    float64
	AGC       bool
	Preset    []float64
	UsePreset bool
	Centers []float64
	Offsets []float64
	Inputs []InputSpec
	AcceptNotice bool

	defGain     float64
	defAGC      bool
	scanDefault bool
}

type InputSpec struct {
	Source string
	CenterMHz  float64
	GainDB     float64
	GainSet    bool
	AGC        bool
	SampleRate float64
	Format     string
	Scan  *bool
	Label string
}

func ParseInput(v string) (InputSpec, error) {
	var in InputSpec
	parts := strings.Split(v, ",")
	at := strings.LastIndex(parts[0], "@")
	switch {
	case parts[0] == "" || at == 0:
		return in, fmt.Errorf("-input %q: <入力>[@<中心 MHz>] の形で（例 rtltcp://127.0.0.1:1235@399.1,gain=30）", v)
	case at < 0:
		in.Source = parts[0]
	default:
		in.Source = parts[0][:at]
		f, err := strconv.ParseFloat(parts[0][at+1:], 64)
		if err != nil || f <= 0 {
			return in, fmt.Errorf("-input %q: 中心の周波数 %q が読めない", v, parts[0][at+1:])
		}
		in.CenterMHz = f
	}
	for _, p := range parts[1:] {
		k, val, _ := strings.Cut(strings.TrimSpace(p), "=")
		switch k {
		case "gain":
			g, err := strconv.ParseFloat(val, 64)
			if err != nil {
				return in, fmt.Errorf("-input %q: gain=%q が読めない", v, val)
			}
			in.GainDB, in.GainSet = g, true
		case "agc":
			in.AGC = true
		case "fs":
			r, err := strconv.ParseFloat(val, 64)
			if err != nil || r <= 0 {
				return in, fmt.Errorf("-input %q: fs=%q が読めない", v, val)
			}
			in.SampleRate = r
		case "fmt":
			in.Format = val
		case "scan", "noscan":
			b := k == "scan"
			in.Scan = &b
		case "label":
			in.Label = val
		default:
			return in, fmt.Errorf("-input %q: %q が分からない（gain= / agc / fs= / fmt= / scan / noscan / label=）", v, k)
		}
	}
	return in, nil
}

func inputFormat(in InputSpec) string {
	if in.Format == "" {
		return "cf32"
	}
	return in.Format
}

func (l *Live) InputConfigs() []rx.InputConfig {
	var out []rx.InputConfig
	for i, in := range l.Inputs {
		if i == 0 {
			continue
		}
		ic := rx.InputConfig{Label: in.Label, Source: in.Source, SampleRate: in.SampleRate, Format: inputFormat(in),
			CenterHz: in.CenterMHz * 1e6, GainDB: l.defGain, AGC: in.AGC || (!in.GainSet && l.defAGC), Scan: l.Scan}
		if in.GainSet {
			ic.GainDB = in.GainDB
		}
		ic.Scan.Enabled = l.scanDefault
		if in.Scan != nil {
			ic.Scan.Enabled = *in.Scan
		}
		out = append(out, ic)
	}
	return out
}

func (l *Live) Label() string {
	if len(l.Inputs) == 0 {
		return ""
	}
	return l.Inputs[0].Label
}

type LiveDefaults struct {
	System string
	LogDir, Addr string
	GainDB float64
	AGC    bool
	Preset     []float64
	PresetName string
	Centers     []float64
	Offsets     []float64
	CentersName string
	Lock string
}

func (l *Live) Register(fs *flag.FlagSet, d LiveDefaults) {
	l.defGain, l.defAGC = d.GainDB, d.AGC
	l.Centers = append([]float64(nil), d.Centers...)
	l.Offsets = append([]float64(nil), d.Offsets...)
	defaults := fmt.Sprintf("gain= を書かなければ %.1f dB", d.GainDB)
	if d.AGC {
		defaults = "gain= を書かなければ受信機の AGC"
	}
	centers := ""
	if d.CentersName != "" {
		centers = "。中心を省くと" + d.CentersName
	}
	fs.Func("input", "入力（何度でも。1 本目が主の入力、2 本目以降の SDR で帯域を広げる）: <入力>[@<中心 MHz>][,gain=<dB>][,agc][,fs=<Hz>][,fmt=<形式>][,scan|noscan][,label=<名前>]。"+
		"<入力> は rtltcp://host:port（rtl_tcp。中心に同調する）・sdrsharp://host:port（SDR# プラグイン。中心は省ける）・tcp://host:port（生 I/Q。fs= と fmt= が要る）・録音ファイル"+
		"（中心は名前の …_<Hz>Hz / …_<MHz>MHz から読めれば省ける）。fmt は cf32（既定）/ s16 / cu8。"+defaults+"。"+
		"-channels も -scan も無く入力が 1 本なら、その中心が受信する局"+centers+"。"+
		"局は帯域に入る入力のうち端から余裕のあるもので受ける。走査は書かなければ -scan に倣う", func(v string) error {
		in, err := ParseInput(v)
		if err != nil {
			return err
		}
		l.Inputs = append(l.Inputs, in)
		return nil
	})
	fs.StringVar(&l.Channels, "channels", "", "監視する局 [MHz]（カンマ区切り。局ごとの設定は <MHz>:<鍵>=<値> で付ける）。"+
		"省略すると主の入力の中心の 1 局、none なら局 0 本で起動して画面の「設定」から足す")
	fs.BoolVar(&l.Scan.Enabled, "scan", false, "帯域全体を見張って局を自動で見つける（-channels は常時監視として併用できる）")
	fs.IntVar(&l.Scan.Max, "scan-max", 0, "走査で同時に張り付ける局の上限（既定 128。復号器 1 本の値段がそのまま CPU になるので機械に合わせて決めること）")
	how := d.Lock
	if how == "" {
		how = "復号の検査"
	}
	fs.Float64Var(&l.Scan.VerifySec, "scan-verify", 0, "候補を復号して確かめる時間 [s]（既定 30。"+how+"で決まれば満了を待たずに確定）")
	fs.Float64Var(&l.Scan.SNRdB, "scan-snr", 0, "雑音床から何 dB 上を候補にするか（既定 6。弱い局は 3 まで下げる）")
	fs.Float64Var(&l.Scan.IntervalSec, "scan-interval", 0, "候補を見直す周期 [s]（既定 2）")
	fs.Float64Var(&l.Scan.QuietMin, "scan-quiet", 0, "当たった局を何分の無反応で手放すか（0 = 手放さない）")
	fs.Float64Var(&l.Scan.EvictIdleMin, "scan-evict", 0, "枠が埋まったとき、何分なにも録っていない台帳・走査の局を新しい候補に譲らせるか（既定 30、負で譲らせない）")
	l.System = d.System
	if d.LogDir == "" {
		d.LogDir = "logs"
	}
	sys := "<系統>"
	if d.System != "" {
		sys = d.System
	}
	fs.StringVar(&l.LogDir, "log-dir", d.LogDir, "記録の置き場所の根（受信のログ watch.log は <log-dir>/"+sys+"/、録音・音声・サイドカーはその下の日付の階層 <YYYY-MM-DD>/）")
	fs.StringVar(&l.Addr, "addr", d.Addr, "Web モニタの待ち受け")
	fs.StringVar(&l.Ledger, "ledger", "", "台帳の置き場所（既定は <log-dir>/"+sys+"/ledger。none で使わない）")
	fs.BoolVar(&l.AcceptNotice, "accept-notice", false, "受信を始める前の法的注意（電波法 第 59 条など。LEGAL.md）に同意する。同意は一度だけ記録し、次からは聞かない")
	fs.BoolVar(&l.FullSpeed, "full-speed", false, "録音ファイルを実時間で流さず全速で処理する（回帰の確認用）")
	fs.Float64Var(&l.Speed, "speed", 1, "録音ファイルを流す速さの倍率（-full-speed でないとき）")
	if len(d.Preset) > 0 {
		l.Preset = append([]float64(nil), d.Preset...)
		fs.BoolVar(&l.UsePreset, "preset", true, "-channels が無ければ"+d.PresetName+"（"+strconv.Itoa(len(d.Preset))+" 波）を常時監視する。"+
			"入力帯域に 2 波以上入るときだけ使い（入る波だけ）、狭帯域の 1 局の入力では主の入力の中心の 1 局のまま。false で使わない")
	}
}

func (l *Live) PresetFor(p Plan) []float64 {
	if !l.UsePreset || p.Empty || strings.TrimSpace(l.Channels) != "" {
		return nil
	}
	return append([]float64(nil), l.Preset...)
}

func (l *Live) LedgerDir() string {
	switch l.Ledger {
	case "none":
		return ""
	case "":
		if l.LogDir == "" {
			return ""
		}
		return filepath.Join(rec.SystemDir(l.LogDir, l.System), "ledger")
	}
	return l.Ledger
}

type Plan struct {
	Source string
	CenterHz float64
	Channels []Channel
	FollowVFO bool
	Empty bool
	Warn string
}

func (p Plan) MHz() []float64 { return MHzList(p.Channels) }

var ErrNoInput = errors.New("入力が要る: -input <入力>[@<中心 MHz>][,gain=<dB>]…（位置引数・-freq・-gain は廃止した）")

func (l *Live) Plan(keys ...string) (Plan, error) {
	p, err := l.plan(keys...)
	if err != nil {
		return Plan{}, err
	}
	if err := notice.Ensure(l.AcceptNotice); err != nil {
		return Plan{}, err
	}
	return p, nil
}

func (l *Live) plan(keys ...string) (Plan, error) {
	if len(l.Inputs) == 0 {
		return Plan{}, ErrNoInput
	}
	pri := l.Inputs[0]
	src := pri.Source
	l.SampleRate, l.Format, l.GainDB, l.AGC = pri.SampleRate, inputFormat(pri), l.defGain, pri.AGC || (!pri.GainSet && l.defAGC)
	if pri.GainSet {
		l.GainDB = pri.GainDB
	}
	l.scanDefault = l.Scan.Enabled
	if pri.Scan != nil {
		l.Scan.Enabled = *pri.Scan
	}
	empty := strings.TrimSpace(l.Channels) == "none"
	var chans []Channel
	var err error
	if !empty {
		chans, err = ParseChannels(l.Channels)
	}
	if err == nil {
		err = CheckKeys(chans, keys...)
	}
	if err != nil {
		return Plan{}, err
	}
	f := pri.CenterMHz
	if f == 0 && !iqsrc.IsNetwork(src) {
		f = FreqFromName(src)
	}
	if f == 0 && strings.HasPrefix(src, "rtltcp://") && len(l.Centers) > 0 {
		f = l.Centers[0]
	}
	if f == 0 && strings.HasPrefix(src, "rtltcp://") && l.UsePreset && !empty && len(chans) == 0 {
		lo, hi := l.Preset[0], l.Preset[0]
		for _, p := range l.Preset {
			lo, hi = min(lo, p), max(hi, p)
		}
		f = (lo + hi) / 2
	}
	if strings.HasPrefix(src, "rtltcp://") && f == 0 {
		return Plan{}, fmt.Errorf("-input %s: rtl_tcp には中心の周波数が要る（%s@<中心 MHz>）", src, src)
	}
	l.FreqMHz = f
	for i := 1; i < len(l.Inputs); i++ {
		in := &l.Inputs[i]
		switch {
		case in.CenterMHz > 0:
		case i < len(l.Centers):
			in.CenterMHz = l.Centers[i]
		case i < len(l.Offsets) && f > 0:
			in.CenterMHz = f + l.Offsets[i]
		case !iqsrc.IsNetwork(in.Source) && FreqFromName(in.Source) > 0:
			in.CenterMHz = FreqFromName(in.Source)
		default:
			return Plan{}, fmt.Errorf("-input %s: 中心の周波数が要る（%s@<中心 MHz>）", in.Source, in.Source)
		}
	}
	if empty || len(chans) > 0 || l.Scan.Enabled {
		if f == 0 && !iqsrc.KnowsFreq(src) {
			return Plan{}, fmt.Errorf("-channels / -scan には主の入力の中心が要る（-input %s@<中心 MHz>）", src)
		}
		return Plan{Source: src, CenterHz: f * 1e6, Channels: chans, Empty: empty && !l.Scan.Enabled}, nil
	}
	p := Plan{Source: src, CenterHz: f * 1e6, Channels: []Channel{{MHz: f}}, FollowVFO: true}
	if f == 0 && iqsrc.KnowsFreq(src) {
		p.Channels = nil
	} else if f == 0 {
		p.Warn = "注意: 主の入力の中心が無く名前からも読めないので、局の周波数を 0 MHz として扱う（表示とファイル名だけの話で、復号には要らない）"
	}
	return p, nil
}

func (l *Live) Engine(p Plan) rx.Config {
	return rx.Config{Source: p.Source, Label: l.Label(), SampleRate: l.SampleRate, Format: l.Format, CenterHz: p.CenterHz,
		GainDB: l.GainDB, AGC: l.AGC, LogDir: l.LogDir, System: l.System, Channels: p.MHz(), Scan: l.Scan,
		Realtime: !l.FullSpeed, Speed: l.Speed, FollowVFO: p.FollowVFO && len(l.Inputs) <= 1, AllowEmpty: p.Empty, Preset: l.PresetFor(p),
		Inputs: l.InputConfigs()}
}

func CheckNoArgs(fs *flag.FlagSet) error {
	if fs.NArg() > 0 {
		return fmt.Errorf("位置引数 %q は使わない（入力は -input %s で書く。位置引数・-freq・-gain は廃止した）", fs.Arg(0), fs.Arg(0))
	}
	return nil
}

var (
	reFreqHz  = regexp.MustCompile(`(?:^|_)(\d{6,11})Hz(?:_|\.|$)`)
	reFreqMHz = regexp.MustCompile(`(?:^|_)(\d{1,5}\.\d+)MHz(?:_|\.|$)`)
)

func FreqFromName(path string) float64 {
	base := filepath.Base(path)
	if m := reFreqMHz.FindStringSubmatch(base); m != nil {
		if v, err := strconv.ParseFloat(m[1], 64); err == nil && v > 0 {
			return v
		}
	}
	if m := reFreqHz.FindStringSubmatch(base); m != nil {
		if v, err := strconv.ParseFloat(m[1], 64); err == nil && v > 0 {
			return v / 1e6
		}
	}
	return 0
}
