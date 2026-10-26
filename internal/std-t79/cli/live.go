package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/symysak/arib/internal/cliflag"
	"github.com/symysak/arib/internal/credit"
	"github.com/symysak/arib/internal/rec"
	"github.com/symysak/arib/internal/std-t79/l1"
	"github.com/symysak/arib/internal/std-t79/server"
	"github.com/symysak/arib/internal/voice"
	"net/http"
	_ "net/http/pprof"
)

const DefaultAddr = "127.0.0.1:8130"

func Live(ctx context.Context, prog string, argv []string, stderr io.Writer) int {
	fs := flag.NewFlagSet(prog, flag.ContinueOnError)
	fs.SetOutput(stderr)
	var l cliflag.Live
	l.Register(fs, cliflag.LiveDefaults{System: server.System, Addr: DefaultAddr, GainDB: 49.6,
		Offsets: []float64{0, server.UpOffsetMHz}, CentersName: fmt.Sprintf("2 本目は 1 本目の中心 %+.0f MHz（上り。§3.2）", server.UpOffsetMHz),
		Lock: "同期ワードが 0.2 秒ぶん以上・スロットの半分以上で出るか"})
	pre := fs.Float64("pre", 3, "通話の I/Q 録音のプリロール [s]（通話と決まった瞬間より前を遡って残す）")
	noIQ := fs.Bool("no-iq", false, "通話の I/Q を書かない（サイドカー・calls.txt・TCH だけ残す。I/Q は 1 スロットの通話につき 64 kHz 2ch int16 ≒ 256 kB/s）")
	noTCH := fs.Bool("no-tch", false, "TCH の生ビット（tch.bin / tch.idx）を書かない")
	idleTCH := fs.Bool("idle-tch", false, "スロットが繰り返す無音の TCH（誰も話していない間も「音声」で送り続ける網の符号器の無音フレーム）を"+
		"通話に数えない: 通話を始めない・続けば閉じる・鳴らさない・TCH を残さない（既定は使わない）")
	codec := fs.String("codec", "auto", "音声の符号器（auto / off / "+server.CodecNames()+"）。auto は通話の TCH で各符号器の伝送路の検査を数えて選ぶ")
	direct := fs.String("direct", "", "上りの入力に張る移動局間直接通信・専用チャネル中継の局（<MHz>[:ctl][:auto][:relay-down|relay-up][:scr=<16 進>] をカンマ区切り。"+
		"ctl = 自動選択の制御用、auto = 自動選択の通信用（通話を無通信 30 秒で閉じる）、relay-down = 中継局→移動局、relay-up = 移動局→中継局、scr = 通信用のスクランブルコード。全国共通波の値は規格に無いので与える。上りの帯（260〜266 MHz）の -input が要る）")
	directScan := fs.Bool("direct-scan", false, "上りの帯の入力も走査し、直接通信の語（S9・S10・S11・SS1）のバーストが出た周波数を直接通信の局として監視する"+
		"（全国共通波の値は規格に無いので探す。上りの帯の -input も -scan に倣って走査する）")
	pprofAddr := fs.String("pprof", "", "CPU などの計測（net/http/pprof）を待ち受けるアドレス（例 127.0.0.1:6060。空なら出さない。調べもの用）")
	codecOpt := voice.Options{}
	fs.Func("codec-opt", "名前で指定した符号器の設定 key=value（何度でも。例 mode=fr。鍵は符号器ごとに違う）", codecOpt.ParseOption)
	fs.Usage = func() {
		fmt.Fprintf(stderr, `ARIB STD-T79（都道府県・市町村デジタル移動通信システム TYPE1）ライブ Web モニタ

  %[1]s [オプション] -input <入力>[@<中心 MHz>][,gain=<dB>]…

<入力>:
  録音ファイル (.wav / .cf32 / .cu8)   実時間ペースで再生（-full-speed で全速。@<中心> は名前から読めれば省略可）
  sdrsharp://host:port                  同梱の SDR# プラグイン（レート・形式・周波数は接続の中で分かる）
  tcp://host:port                       生 I/Q（SDR++ の Network Sink など。,fs= と ,fmt= が要る）
  rtltcp://host:port                    rtl_tcp（@<中心> に同調する。,gain= で利得）
  2 本目の -input に上りの帯（260〜266 MHz）の SDR を渡すと上りも受ける（中心を省くと 1 本目 − 9 MHz）。
  局が上りか下りかは周波数で決める。下りの局と無線チャネル指定の周波数 − 9 MHz に上りの局を足す。

例:
  %[1]s -input sdrsharp://127.0.0.1:5555                                  1 局（SDR# の VFO に付いて行く）
  %[1]s -channels <局1>,<局2> -input rtltcp://127.0.0.1:1234@<中心 MHz>
  %[1]s -scan -input rtltcp://127.0.0.1:1234@<中心 MHz>            帯域を見張ってキャリアを足す
  %[1]s -scan -input rtltcp://127.0.0.1:1234@<中心 MHz> -input rtltcp://127.0.0.1:1235,gain=19.7   下りと上り（2 台目の SDR、中心 − 9 MHz）

1 局 = 1 キャリア（25 kHz、4 スロット）。スロットが空線信号（S9〜S12）から通常の同期ワードへ変わったら
通話とみて、スロットごとに録音する: %[2]s（I/Q）・%[3]s（サイドカー）・%[4]s（通話の出入り・FACCH・同期バースト）・
%[5]s + %[6]s（TCH 256 ビット/40 ms、デスクランブル済み）・音声 %[7]s（-codec の符号器で鳴ったとき）。
**音声の符号化は規格が規定しない**ので、-codec auto は登録してある符号器（%[8]s）の伝送路の検査（CRC・ビタビの距離など）で選ぶ。
ブラウザで http://<addr>/ を開くと、局一覧・スロットの状態・メッセージ（CAC・SACCH・FACCH・同期バースト）を実時刻で表示する。
入力の終端で受信は止まり、画面は見られるまま残る。Ctrl-C で録音を閉じて終了する。

オプション:
`, prog, "<基名>."+string(rec.IQ), "."+string(rec.Text), "."+string(rec.Calls), "."+string(rec.TCH), "."+string(rec.TCHIdx), "."+string(rec.Audio), server.CodecNames())
		fs.PrintDefaults()
	}
	if err := fs.Parse(cliflag.ReorderArgs(fs, argv)); err != nil {
		return 2
	}
	if err := cliflag.CheckNoArgs(fs); err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	plan, err := l.Plan()
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	if plan.Warn != "" {
		fmt.Fprintln(stderr, plan.Warn)
	}
	if l.LogDir == "" {
		fmt.Fprintln(stderr, "-log-dir は空にできない（受信のログと録音の置き場所）")
		return 2
	}
	notes, err := server.CheckCodec(*codec, codecOpt)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	for _, n := range notes {
		fmt.Fprintln(stderr, n)
	}
	ec := l.Engine(plan)
	for _, in := range ec.Inputs {
		if server.IsUplinkMHz(in.CenterHz / 1e6) {
			fmt.Fprintf(stderr, "上り: %s（中心 %.4f MHz）\n", in.Source, in.CenterHz/1e6)
		}
	}
	ec.LogOut, ec.PreS, ec.NoIQ = stderr, *pre, *noIQ
	if *pprofAddr != "" {
		go func() {
			if err := http.ListenAndServe(*pprofAddr, nil); err != nil {
				fmt.Fprintln(stderr, "pprof:", err)
			}
		}()
		fmt.Fprintf(stderr, "pprof: http://%s/debug/pprof/\n", *pprofAddr)
	}
	ds, err := parseDirect(*direct)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	m := server.NewMonitor(server.Config{Engine: ec, NoTCH: *noTCH, LedgerDir: l.LedgerDir(),
		Codec: *codec, CodecOpt: codecOpt, Direct: ds, DirectScan: *directScan, IdleTCH: *idleTCH})
	fmt.Fprintln(stderr, credit.Line())
	fmt.Fprintf(stderr, "ARIB STD-T79 モニタ: http://%s/\n", l.Addr)
	fmt.Fprintf(stderr, "記録の保存先: %s/\n", rec.SystemDir(l.LogDir, server.System))
	if err := m.ListenAndServe(ctx, l.Addr); err != nil {
		fmt.Fprintln(stderr, "終了:", err)
		return 1
	}
	return 0
}

func parseDirect(v string) ([]server.DirectStation, error) {
	var out []server.DirectStation
	for _, item := range strings.Split(v, ",") {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		parts := strings.Split(item, ":")
		f, err := strconv.ParseFloat(parts[0], 64)
		if err != nil || f <= 0 {
			return nil, fmt.Errorf("-direct の周波数 %q が読めない", parts[0])
		}
		d := server.DirectStation{MHz: f, Opt: server.DirectOpt{Mode: l1.ModeDirect, Scramble: -1}}
		for _, p := range parts[1:] {
			switch {
			case p == "ctl":
				d.Opt.Control = true
			case p == "auto":
				d.Opt.Auto = true
			case p == "relay-down":
				d.Opt.Mode = l1.ModeRelayDown
			case p == "relay-up":
				d.Opt.Mode = l1.ModeRelayUp
			case strings.HasPrefix(p, "scr="):
				x, err := strconv.ParseUint(strings.TrimPrefix(strings.TrimPrefix(p[4:], "0x"), "0X"), 16, 8)
				if err != nil {
					return nil, fmt.Errorf("-direct のスクランブルコード %q が読めない", p)
				}
				d.Opt.Scramble = int(x)
			default:
				return nil, fmt.Errorf("-direct の %q が分からない（ctl / auto / relay-down / relay-up / scr=<16 進>）", p)
			}
		}
		out = append(out, d)
	}
	return out, nil
}
