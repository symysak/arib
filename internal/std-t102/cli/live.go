package cli

import (
	"context"
	"flag"
	"fmt"
	"io"

	"github.com/symysak/arib/internal/cliflag"
	"github.com/symysak/arib/internal/credit"
	"github.com/symysak/arib/internal/data"
	"github.com/symysak/arib/internal/rec"
	"github.com/symysak/arib/internal/std-t102/l1"
	"github.com/symysak/arib/internal/std-t102/server"
	"github.com/symysak/arib/internal/voice"
)

const DefaultAddr = "127.0.0.1:8140"

func Live(ctx context.Context, prog string, argv []string, stderr io.Writer) int {
	fs := flag.NewFlagSet(prog, flag.ContinueOnError)
	fs.SetOutput(stderr)
	var l cliflag.Live
	l.Register(fs, cliflag.LiveDefaults{System: server.System, Addr: DefaultAddr, GainDB: 40.2,
		Lock:   "ホワイトニングの初期値を当てて RICH が規定どおり読めるか（4 枚以上）",
		Preset: server.B54Channels(), PresetName: "ARIB STD-B54 付録 1 の 160 MHz 帯の割当周波数（低群・高群。入力の帯域に入る波だけ）"})
	pre := fs.Float64("pre", 2, "送信の I/Q 録音のプリロール [s]（送信と決まった瞬間より前を遡って残す）")
	noIQ := fs.Bool("no-iq", false, "送信の I/Q を書かない（サイドカー・calls.txt・TCH・音声だけ残す。I/Q は 24 kHz 2ch int16 = 96 kB/s）")
	noTCH := fs.Bool("no-tch", false, "TCH の生ビット（tch.bin / tch.idx）を書かない")
	codec := fs.String("codec", "auto", "音声の符号器（auto / off / "+server.CodecNames()+"）。auto は TCH で各符号器の伝送路の検査を数えて選ぶ")
	dataFmt := fs.String("data", data.Auto, "データ（UDCH1 / FACCH2）の書式（auto / off / "+data.Names(l1.DataChannel)+"）。auto は書式の検査で選ぶ")
	audioAGC := fs.Bool("audio-agc", true, "復号した音声の音量を局ごとにそろえる（自動利得調整。送出レベルの低い局も聞こえるよう、ゲート −70 dBFS・最大 +42 dB。false で素のまま）")
	initStr := fs.String("init", "", "ホワイトニングの初期値（0〜511、16 進は 0x 付き。§4.1.13 の S8…S0。空なら CRC で当てる。局ごとは -channels <MHz>:init=）")
	codecOpt := voice.Options{}
	fs.Func("codec-opt", "名前で指定した符号器の設定 key=value（何度でも）", codecOpt.ParseOption)
	var b54Keys []string
	fs.Func("b54-key", "ARIB STD-B54 のスクランブル暗号の鍵 <鍵番号 1〜63>=<暗号鍵 1〜32767>（何度でも。無ければデータは CRC で、音声は符号器が当てる）",
		func(s string) error { b54Keys = append(b54Keys, s); return nil })
	fs.Usage = func() {
		fmt.Fprintf(stderr, `ARIB STD-T102 第 2 編（狭帯域デジタル通信方式 SCPC/4 値 FSK）ライブ Web モニタ

  %[1]s [オプション] -input <入力>[@<中心 MHz>][,gain=<dB>]…

<入力>:
  録音ファイル (.wav / .cf32 / .cu8)   実時間ペースで再生（-full-speed で全速。@<中心> は名前から読めれば省略可）
  sdrsharp://host:port                  同梱の SDR# プラグイン（レート・形式・周波数は接続の中で分かる）
  tcp://host:port                       生 I/Q（,fs= と ,fmt= が要る）
  rtltcp://host:port                    rtl_tcp（@<中心> に同調する。,gain= で利得）

例:
  %[1]s -input sdrsharp://127.0.0.1:5555                                1 局（SDR# の VFO に付いて行く）
  %[1]s -channels <局1>,<局2>:init=0x0E4 -input rtltcp://127.0.0.1:1234@<中心 MHz>
  %[1]s -scan -input rtltcp://127.0.0.1:1234@<中心 MHz>          帯域を見張って局を足す

1 局 = 1 チャネル（6.25 kHz・4 値 FSK 2400 シンボル/s・80 ms フレーム）。同期ワード CDF59 でフレームを切り出し、
ホワイトニングの初期値（規格は網ごとに任せている）を CRC と AMBE+2 の検査で当て、RICH・SACCH・FACCH1・TCH・UDCH1/FACCH2 を読む。
送信（バースト）ごとに録音する: %[2]s（I/Q）・%[3]s（サイドカー）・%[4]s（送信の出入り・メッセージ・データ）・
%[5]s + %[6]s（TCH 2 × 144 ビット/80 ms、ホワイトニングを外したもの）・音声 %[7]s（-codec の符号器で鳴ったとき）。
データの出来事（位置・電文）は <log-dir>/%[8]s/data/<日付>_<MHz>MHz.tsv にも書く。
ブラウザで http://<addr>/ を開くと、局一覧・送信・地図・L1 / メッセージのインスペクタを実時刻で表示する。
入力の終端で受信は止まり、画面は見られるまま残る。Ctrl-C で録音を閉じて終了する。

オプション:
`, prog, "<基名>."+string(rec.IQ), "."+string(rec.Text), "."+string(rec.Calls), "."+string(rec.TCH), "."+string(rec.TCHIdx),
			"."+string(rec.Audio), server.System)
		fs.PrintDefaults()
	}
	if err := fs.Parse(cliflag.ReorderArgs(fs, argv)); err != nil {
		return 2
	}
	if err := cliflag.CheckNoArgs(fs); err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	plan, err := l.Plan("init")
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
	opts, err := server.OptsFromChannels(plan.Channels)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	var init *int
	if *initStr != "" {
		v, err := server.ParseInit(*initStr)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 2
		}
		init = &v
	}
	notes, err := server.CheckCodec(*codec, codecOpt)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	keys, err := server.ParseB54Keys(b54Keys)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	if err := data.Valid(l1.DataChannel, *dataFmt); err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	for _, n := range notes {
		fmt.Fprintln(stderr, n)
	}
	ec := l.Engine(plan)
	ec.LogOut, ec.PreS, ec.NoIQ = stderr, *pre, *noIQ
	m := server.NewMonitor(server.Config{Engine: ec, Codec: *codec, CodecOpt: codecOpt, Data: *dataFmt, Init: init, Opts: opts,
		NoTCH: *noTCH, AudioAGC: *audioAGC, LedgerDir: l.LedgerDir(), B54Keys: keys})
	fmt.Fprintln(stderr, credit.Line())
	fmt.Fprintf(stderr, "ARIB STD-T102 モニタ: http://%s/\n", l.Addr)
	fmt.Fprintf(stderr, "記録の保存先: %s/\n", rec.SystemDir(l.LogDir, server.System))
	if err := m.ListenAndServe(ctx, l.Addr); err != nil {
		fmt.Fprintln(stderr, "終了:", err)
		return 1
	}
	return 0
}
