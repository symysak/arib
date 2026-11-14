package cli

import (
	"context"
	"flag"
	"fmt"
	"io"

	"github.com/symysak/arib/internal/cliflag"
	"github.com/symysak/arib/internal/credit"
	"github.com/symysak/arib/internal/rec"
	"github.com/symysak/arib/internal/std-t98/fsk4/chplan"
	"github.com/symysak/arib/internal/std-t98/fsk4/server"
	"github.com/symysak/arib/internal/voice"
)

const DefaultAddr = "127.0.0.1:8150"

func Live(ctx context.Context, prog string, argv []string, stderr io.Writer) int {
	fs := flag.NewFlagSet(prog, flag.ContinueOnError)
	fs.SetOutput(stderr)
	var l cliflag.Live
	l.Register(fs, cliflag.LiveDefaults{System: server.System, Addr: DefaultAddr, GainDB: 40.2,
		Lock:   "RICH が規定どおり読め（4 枚以上）、SACCH か PICH の CRC が通るか（2 枚以上）",
		Preset: chplan.Frequencies(), PresetName: "規格の割当周波数（登録局・上空・免許局・中継・150 MHz 帯。入力の帯域に入る波だけ）"})
	pre := fs.Float64("pre", 2, "送信の I/Q 録音のプリロール [s]（送信と決まった瞬間より前を遡って残す）")
	noIQ := fs.Bool("no-iq", false, "送信の I/Q を書かない（サイドカー・calls.txt・TCH・音声だけ残す。I/Q は 24 kHz 2ch int16 = 96 kB/s）")
	noTCH := fs.Bool("no-tch", false, "TCH の生ビット（tch.bin / tch.idx）を書かない")
	codec := fs.String("codec", "auto", "音声の符号器（auto / off / "+server.CodecNames()+"）。auto は TCH で各符号器の伝送路の検査を数えて選ぶ")
	audioAGC := fs.Bool("audio-agc", true, "復号した音声の音量を局ごとにそろえる（自動利得調整。ゲート −70 dBFS・最大 +42 dB。false で素のまま）")
	codecOpt := voice.Options{}
	fs.Func("codec-opt", "名前で指定した符号器の設定 key=value（何度でも）", codecOpt.ParseOption)
	var keys []string
	fs.Func("key", "秘話鍵 <1〜32767>（どのユーザコードにも）か <ユーザコード 0〜511>:<秘話鍵>（何度でも。無ければ秘話通信は符号器が通話の中で当てる）",
		func(s string) error { keys = append(keys, s); return nil })
	fs.Usage = func() {
		fmt.Fprintf(stderr, `ARIB STD-T98 第 3 編（デジタル簡易無線・四値周波数偏位変調）ライブ Web モニタ

  %[1]s [オプション] -input <入力>[@<中心 MHz>][,gain=<dB>]…

<入力>:
  録音ファイル (.wav / .cf32 / .cu8)   実時間ペースで再生（-full-speed で全速。@<中心> は名前から読めれば省略可）
  sdrsharp://host:port                  同梱の SDR# プラグイン（レート・形式・周波数は接続の中で分かる）
  tcp://host:port                       生 I/Q（,fs= と ,fmt= が要る）
  rtltcp://host:port                    rtl_tcp（@<中心> に同調する。,gain= で利得）

例:
  %[1]s -input rtltcp://127.0.0.1:1234@<中心 MHz>                 帯域に入る割当周波数を全部見張る（既定。走査は要らない）
  %[1]s -input sdrsharp://127.0.0.1:5555                         1 局（SDR# の VFO に付いて行く）
  %[1]s -key <ユーザコード>:<秘話鍵> -input rtltcp://127.0.0.1:1234@<中心 MHz>

周波数は規格で決まっている（登録局・上空・免許局・中継・150 MHz 帯の割当）ので、入力の帯域に入る割当を最初から全部監視する
（-preset が既定で有効。割当の外の周波数は -channels で足せる）。

1 局 = 1 チャネル（6.25 kHz・4 値 FSK 2400 シンボル/s・80 ms フレーム）。基地局も制御チャネルも無い直接通信で、送信ボタンを押した間しか
電波が出ない。同期ワード CDF59 でフレームを切り出し、ホワイトニング（規格の初期値）を外して、同期バーストの呼出名称（PICH）・SACCH の
通話識別とユーザコード・TCH（音声 / 非音声）を読む。秘話通信（秘話スクランブル。15 ビットの鍵）は -key の鍵で外し、無ければ通話の中で当てる。
送信ごとに録音する: %[2]s（I/Q）・%[3]s（サイドカー）・%[4]s（送信の出入り・メッセージ）・
%[5]s + %[6]s（TCH 2 × 144 ビット/80 ms、ホワイトニングを外したもの）・音声 %[7]s（-codec の符号器で鳴ったとき）。
ブラウザで http://<addr>/ を開くと、局一覧・チャネル表・無線機（呼出名称）・グループ（ユーザコード）・送信・L1 / メッセージのインスペクタを
実時刻で表示する。入力の終端で受信は止まり、画面は見られるまま残る。Ctrl-C で録音を閉じて終了する。

オプション:
`, prog, "<基名>."+string(rec.IQ), "."+string(rec.Text), "."+string(rec.Calls), "."+string(rec.TCH), "."+string(rec.TCHIdx),
			"."+string(rec.Audio))
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
	km, err := server.ParseKeys(keys)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	for _, n := range notes {
		fmt.Fprintln(stderr, n)
	}
	ec := l.Engine(plan)
	ec.LogOut, ec.PreS, ec.NoIQ = stderr, *pre, *noIQ
	m := server.NewMonitor(server.Config{Engine: ec, Codec: *codec, CodecOpt: codecOpt, Keys: km,
		NoTCH: *noTCH, AudioAGC: *audioAGC, LedgerDir: l.LedgerDir()})
	fmt.Fprintln(stderr, credit.Line())
	fmt.Fprintf(stderr, "ARIB STD-T98 第 3 編 モニタ: http://%s/\n", l.Addr)
	fmt.Fprintf(stderr, "記録の保存先: %s/\n", rec.SystemDir(l.LogDir, server.System))
	if err := m.ListenAndServe(ctx, l.Addr); err != nil {
		fmt.Fprintln(stderr, "終了:", err)
		return 1
	}
	return 0
}
