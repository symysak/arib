package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"strconv"

	"github.com/symysak/arib/internal/cliflag"
	"github.com/symysak/arib/internal/credit"
	"github.com/symysak/arib/internal/std-t61/server"
	t61voice "github.com/symysak/arib/internal/std-t61/voice"
	"github.com/symysak/arib/internal/std-t61/watch"
	"github.com/symysak/arib/internal/voice"
)

type Flags struct {
	cliflag.Live
	PreS, PostS, MaxS    float64
	ControlS, ControlGap float64
	RecordAll            bool
	VoiceOnly, FollowControl, BandIQ bool
	NoUI bool
	IdleTCH bool
	FollowAssign bool
}

type Defaults struct {
	cliflag.LiveDefaults
	PostS float64
	PostHelp string
}

func (f *Flags) Register(fs *flag.FlagSet, d Defaults) {
	if d.Lock == "" {
		d.Lock = "RICH の CRC6 の通過率"
	}
	f.Live.Register(fs, d.LiveDefaults)
	if d.PostS <= 0 {
		d.PostS = 15
	}
	fs.Float64Var(&f.PreS, "pre", 10, "プリロール [s]")
	fs.Float64Var(&f.PostS, "post", d.PostS, "空線へ戻ってから録音を閉じるまで [s]"+d.PostHelp)
	fs.Float64Var(&f.MaxS, "max", 900, "1 録音の上限 [s]")
	fs.Float64Var(&f.ControlS, "control-sec", 180, "制御チャネルを見つけたときの録音長 [s]")
	fs.Float64Var(&f.ControlGap, "control-gap", 3600, "制御チャネルを録り直す間隔 [s]")
	fs.BoolVar(&f.RecordAll, "record-all", false, "空線でも録る（動作確認用）")
	fs.BoolVar(&f.VoiceOnly, "voice-only", false, "I/Q の録音を通話（音声・FACCH・非音声）だけにする。制御チャネルの待機と通信チャネルの空線では録らない")
	fs.BoolVar(&f.FollowControl, "follow-control", true, "帯域内で通話が始まったら制御チャネルの I/Q も録る（呼設定は通話開始の瞬間に制御チャネルを通る）")
	fs.BoolVar(&f.BandIQ, "iq-band", false, "I/Q を局ごとではなく入力ベースバンドのまま録る（全局が丸ごと残り、容量も局別より小さい）")
	fs.BoolVar(&f.NoUI, "no-ui", false, "画面（Web モニタ）を出さずに回す。受信・録音・ログ・台帳・音声のファイルは同じ。端末に受信のログが 1 行ずつ出る")
	fs.BoolVar(&f.IdleTCH, "idle-tch", false, "局が繰り返す無音の TCH（誰も話していない通話の符号器の無音フレーム）を通話に数えない: "+
		"録音の引き金・制御チャネルの追随録音・音声から外す。無音の通話を開けたままにする網向け（局ごとに同じフレームが半分以上を占めたら覚える）")
	fs.BoolVar(&f.FollowAssign, "follow-assign", true, "グループ通信呼出・無線チャネル指定・切替先無線チャネル指定が指す通信チャネルを監視に足す"+
		"（周波数は報知情報の制御チャネル番号から Fo が分かったときだけ。走査で拾えない弱い通信チャネルも通話の始まりで追える）")
}

func (f *Flags) Watch() (watch.Config, cliflag.Plan, error) { return f.WatchKeys() }

func (f *Flags) WatchKeys(keys ...string) (watch.Config, cliflag.Plan, error) {
	plan, err := f.Plan(keys...)
	if err != nil {
		return watch.Config{}, plan, err
	}
	if plan.Empty && f.NoUI {
		return watch.Config{}, plan, fmt.Errorf("-no-ui には局を足す画面が無いので -channels none は使えない（-channels か -scan で局を決めること）")
	}
	sc := f.Scan
	cfg := watch.Config{
		SampleRate: f.SampleRate, Format: f.Format, CenterHz: plan.CenterHz, ChannelsMHz: plan.MHz(),
		LogDir: f.LogDir, System: f.System, PreS: f.PreS, PostS: f.PostS, MaxS: f.MaxS, ControlS: f.ControlS, ControlGap: f.ControlGap,
		GainDB: f.GainDB, AGC: f.AGC, RecordAll: f.RecordAll,
		VoiceOnly: f.VoiceOnly, FollowControl: f.FollowControl, BandIQ: f.BandIQ, IdleTCH: f.IdleTCH,
		Scan: sc.Enabled, ScanMax: sc.Max, ScanVerifySec: sc.VerifySec, ScanSNRdB: sc.SNRdB,
		ScanIntervalSec: sc.IntervalSec, ScanQuietMin: sc.QuietMin, ScanEvictIdleMin: sc.EvictIdleMin,
		Realtime: !f.FullSpeed, Speed: f.Speed, FollowVFO: plan.FollowVFO && len(f.Inputs) <= 1, Preset: f.PresetFor(plan),
		Inputs: f.InputConfigs(),
	}
	return cfg, plan, nil
}

func StationWC(plan cliflag.Plan) (map[float64]int, error) {
	out := map[float64]int{}
	for _, c := range plan.Channels {
		v, ok := c.Opts["wc"]
		if !ok {
			continue
		}
		n, err := strconv.ParseInt(v, 0, 32)
		if err != nil || n < 0 || n > 511 {
			return nil, fmt.Errorf("%.4f MHz: wc は 0..511（0x 付きは 16 進）: %q", c.MHz, v)
		}
		out[c.MHz] = int(n)
	}
	return out, nil
}

func Usage(prog, title string) string {
	return fmt.Sprintf(`%[2]s: %[1]s [オプション] -input <入力>[@<中心 MHz>][,gain=<dB>]…

  1 局（SDR# で同調済み）:  %[1]s -input sdrsharp://127.0.0.1:5555        （VFO に付いて行く）
  1 局（SDR++ など）:       %[1]s -input tcp://127.0.0.1:7355@<局>,fs=11025,fmt=s16
  録音ファイル（1 局）:     %[1]s -input <録音.wav>@<局>      （名前に周波数があれば @<局> は省ける）
  多局:                     %[1]s -channels <局1>,<局2> -input rtltcp://127.0.0.1:1234@<中心>,gain=40
  走査:                     %[1]s -scan -input rtltcp://127.0.0.1:1234@<中心>
  SDR 2 本:                 %[1]s -scan -input rtltcp://127.0.0.1:1234@<中心1>,gain=40 -input rtltcp://127.0.0.1:1235@<中心2>,gain=19.7
  局 0 本で起動:            %[1]s -channels none -input rtltcp://127.0.0.1:1234@<中心>   （画面の「設定」から足す）
  画面なしで監視:           %[1]s -no-ui -channels <局1>,<局2> -input rtltcp://127.0.0.1:1234@<中心>
  付録 1 の局の wc を固定:   %[1]s -channels <局1>:wc=123,<局2> -input rtltcp://127.0.0.1:1234@<中心>
                            （第 1 編 付録 1 のスクランブルのパターン番号 0..511。無ければ音声の符号器が当てる）

  局の決め方: -channels も -scan も無く入力が 1 本なら、その中心の 1 局。-channels があれば入力の中心から
  各局を切り出す。-scan は帯域を見張って局を足す（-channels と併用できる）。局は画面の「設定」からも足せる。
  入力の終端で受信は止まり、画面は見られるまま残る（-no-ui なら終わる）。Ctrl-C で録音を閉じ、台帳を保存して終わる。
  録音は <log-dir>/<系統>/<YYYY-MM-DD>/<HHMMSS>_<MHz>MHz.iq.wav（+ サイドカー .txt）。音声が鳴れば .audio.wav と .calls.txt、
  -tch-dump なら .tch.bin / .tch.idx も隣に書く。

`, prog, title)
}

func Live(ctx context.Context, prog string, argv []string, stderr io.Writer) int {
	fs := flag.NewFlagSet(prog, flag.ContinueOnError)
	fs.SetOutput(stderr)
	var f Flags
	f.Register(fs, Defaults{LiveDefaults: cliflag.LiveDefaults{System: watch.DefaultSystem, Addr: "127.0.0.1:8080"}})
	codec := fs.String("codec", "auto", "音声の符号器（auto / off / "+voice.Names(t61voice.TCH, t61voice.TCH1)+"）。auto は呼設定の伝達能力と伝送路の検査で選ぶ"+
		"（登録された符号器が無ければ音声を扱わない）。off で音声を扱わない。"+
		"決まったら画面へ鳴らし、録音の隣に .audio.wav と .calls.txt を書く。T61 は符号器を規定していないので系統ごとに違う")
	codecOpt := voice.Options{}
	fs.Func("codec-opt", "名前で指定した符号器の設定 key=value（何度でも。例 mode=fr。鍵は符号器ごとに違う）", codecOpt.ParseOption)
	var stationWC map[float64]int
	tchDump := fs.Bool("tch-dump", false, "音声フレームの TCH 生ビットを録音の隣 <base>.tch.bin へ残す（番号は <base>.tch.idx。伝送路符号化が未知の系統では必須）")
	fs.Usage = func() {
		fmt.Fprint(stderr, Usage(prog, "STD-T61 ライブ Web モニタ"))
		fs.PrintDefaults()
	}
	if err := fs.Parse(cliflag.ReorderArgs(fs, argv)); err != nil {
		return 2
	}
	if err := cliflag.CheckNoArgs(fs); err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	cfg, plan, err := f.WatchKeys("wc")
	if err == nil {
		stationWC, err = StationWC(plan)
	}
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	if plan.Warn != "" {
		fmt.Fprintln(stderr, plan.Warn)
	}
	notes, err := server.CheckCodecOptions(*codec, codecOpt)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	for _, n := range notes {
		fmt.Fprintln(stderr, n)
	}
	if *codec == "off" || *codec == "auto" && len(voice.Codecs(t61voice.TCH, t61voice.TCH1)) == 0 {
		*codec = ""
	}
	return f.Serve(ctx, server.Config{Source: plan.Source, Watch: cfg, Codec: *codec, CodecOpt: codecOpt, TCHDump: *tchDump, StationWC: stationWC}, stderr)
}

func (f *Flags) Serve(ctx context.Context, sc server.Config, stderr io.Writer) int {
	sc.Watch.LogOut = stderr
	sc.LedgerDir = f.LedgerDir()
	sc.Headless = f.NoUI
	sc.NoFollowAssign = !f.FollowAssign
	fmt.Fprintln(stderr, credit.Line())
	p := server.New(sc)
	if f.NoUI {
		if err := p.Run(ctx, func(server.Event) {}); err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		return 0
	}
	s := server.NewServer(p)
	fmt.Fprintf(stderr, "Web モニタ: http://%s/\n", f.Addr)
	if err := s.ListenAndServe(ctx, f.Addr); err != nil {
		fmt.Fprintln(stderr, "終了:", err)
		return 1
	}
	return 0
}
