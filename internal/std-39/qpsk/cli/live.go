package cli

import (
	"context"
	"flag"
	"fmt"
	"io"

	"github.com/symysak/arib/internal/cliflag"
	"github.com/symysak/arib/internal/credit"
	"github.com/symysak/arib/internal/rec"
	"github.com/symysak/arib/internal/std-39/qpsk/server"
	"github.com/symysak/arib/internal/voice"
)

const DefaultAddr = "127.0.0.1:8139"

func Live(ctx context.Context, prog string, argv []string, stderr io.Writer) int {
	fs := flag.NewFlagSet(prog, flag.ContinueOnError)
	fs.SetOutput(stderr)
	var l cliflag.Live
	l.Register(fs, cliflag.LiveDefaults{System: server.System, Addr: DefaultAddr, GainDB: 49.6,
		Lock: "同期ワードが 0.2 秒ぶん以上・スロットの半分以上で出るか"})
	pre := fs.Float64("pre", 3, "通話の I/Q 録音のプリロール [s]（通話と決まった瞬間より前を遡って残す）")
	noIQ := fs.Bool("no-iq", false, "通話の I/Q を書かない（サイドカー・calls.txt・TCH だけ残す）")
	noTCH := fs.Bool("no-tch", false, "TCH の生ビット（tch.bin / tch.idx）を書かない")
	idleTCH := fs.Bool("idle-tch", false, "スロットが繰り返す無音の TCH を通話に数えない（既定は使わない）")
	codec := fs.String("codec", "auto", "音声の符号器（auto / off / "+server.CodecNames()+"）。auto は通話の TCH で各符号器の伝送路の検査を数えて選ぶ")
	fo := fs.Float64("fo", 0, "周波数コードの基準 Fo [MHz]（基地局送信 = Fo + 0.025·周波数コード。§4.3.5.3.3.6 で「未定」= 網ごと。"+
		"0 なら報知情報の制御チャネル構造情報と受けている局の周波数から覚える）。無線チャネル指定の周波数を出し、指定先の局を足すのに使う")
	codecOpt := voice.Options{}
	fs.Func("codec-opt", "名前で指定した符号器の設定 key=value（何度でも）", codecOpt.ParseOption)
	fs.Usage = func() {
		fmt.Fprintf(stderr, `ARIB RCR STD-39 第 2 編（狭帯域デジタル通信方式 TDMA・π/4 シフト QPSK）ライブ Web モニタ

  %[1]s [オプション] -input <入力>[@<中心 MHz>][,gain=<dB>]…

<入力>:
  録音ファイル (.wav / .cf32 / .cu8)   実時間ペースで再生（-full-speed で全速。@<中心> は名前から読めれば省略可）
  sdrsharp://host:port                  同梱の SDR# プラグイン（レート・形式・周波数は接続の中で分かる）
  tcp://host:port                       生 I/Q（,fs= と ,fmt= が要る）
  rtltcp://host:port                    rtl_tcp（@<中心> に同調する。,gain= で利得）

例:
  %[1]s -input sdrsharp://127.0.0.1:5555                       1 局（SDR# の VFO に付いて行く）
  %[1]s -scan -input rtltcp://127.0.0.1:1234@<中心 MHz>        帯域を見張ってキャリアを足す

1 局 = 1 キャリア（25 kHz、4 スロット）。スロットが空線信号（S9〜S12）から通常の同期ワードへ変わったら通話とみて、スロットごとに録音する:
%[2]s（I/Q）・%[3]s（サイドカー）・%[4]s（通話の出入り）・%[5]s + %[6]s（TCH 256 ビット/40 ms、デスクランブル済み）。
**音声の符号化は規格が規定しない**（第 5 章）ので、鳴らすのは STD-39 の TCH に登録した符号器（%[7]s）があるときだけ。
L3（§4.3 の RT・MM・CC）を読み、無線チャネル指定のスクランブルを指定先の局へ渡す（周波数は -fo か報知情報から）。上りは受けない
（規格に上りの帯の差が無い。送受信周波数間隔 18 MHz 以上）。

オプション:
`, prog, "<基名>."+string(rec.IQ), "."+string(rec.Text), "."+string(rec.Calls), "."+string(rec.TCH), "."+string(rec.TCHIdx), server.CodecNames())
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
	ec.LogOut, ec.PreS, ec.NoIQ = stderr, *pre, *noIQ
	m := server.NewMonitorWith(*fo, server.Config{Engine: ec, NoTCH: *noTCH, LedgerDir: l.LedgerDir(), Codec: *codec, CodecOpt: codecOpt, IdleTCH: *idleTCH})
	fmt.Fprintln(stderr, credit.Line())
	fmt.Fprintf(stderr, "RCR STD-39 第 2 編 モニタ: http://%s/\n", l.Addr)
	fmt.Fprintf(stderr, "記録の保存先: %s/\n", rec.SystemDir(l.LogDir, server.System))
	if err := m.ListenAndServe(ctx, l.Addr); err != nil {
		fmt.Fprintln(stderr, "終了:", err)
		return 1
	}
	return 0
}
