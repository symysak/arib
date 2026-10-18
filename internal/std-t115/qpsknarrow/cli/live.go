package cli

import (
	"context"
	"flag"
	"fmt"
	"io"

	"github.com/symysak/arib/internal/cliflag"
	"github.com/symysak/arib/internal/credit"
	"github.com/symysak/arib/internal/rec"
	"github.com/symysak/arib/internal/std-t115/qpsknarrow/server"
)

func Live(ctx context.Context, prog string, argv []string, stderr io.Writer) int {
	fs := flag.NewFlagSet(prog, flag.ContinueOnError)
	fs.SetOutput(stderr)
	var l cliflag.Live
	l.Register(fs, cliflag.LiveDefaults{System: server.System, Addr: "127.0.0.1:8000", GainDB: 30,
		Lock: "CRC16 が 3 枚通るか"})
	scramble := fs.String("scramble", "0", "全局の既定のスクランブル値（0 = 自動判定。自治体ごとに違う。16 進は 0x 付き）")
	noIQ := fs.Bool("no-iq", false, "通報区間の I/Q 録音をしない（既定は保存する。1.024 MS/s 入力で約 200kB/s）")
	pre := fs.Float64("pre", 5, "I/Q 録音のプリロール [s]（通報開始より前を遡って残す）")
	fs.Usage = func() {
		fmt.Fprintf(stderr, `ARIB STD-T115 QPSK ナロー方式 ライブ Web モニタ

  %[1]s [オプション] -input <入力>[@<中心 MHz>][,gain=<dB>]…

<入力>:
  録音ファイル (.wav / .cf32 / .cu8)   実時間ペースで再生（-full-speed で全速。@<中心> は名前から読めれば省略可）
  sdrsharp://host:port                  同梱の SDR# プラグイン（レート・形式・周波数は接続の中で分かる）
  tcp://host:port                       生 I/Q（SDR++ の Network Sink など。,fs= と ,fmt= が要る）
  rtltcp://host:port                    rtl_tcp（@<中心> に同調する。,gain= で利得）

例:
  %[1]s -input sdrsharp://127.0.0.1:5555                                  1 局（SDR# の VFO に付いて行く）
  %[1]s -channels 60.0925,60.1075:scramble=0x1234 -input rtltcp://127.0.0.1:1234@60.1
  %[1]s -scan -input rtltcp://127.0.0.1:1234@60.1                   帯域を見張って局を足す

ブラウザで http://<addr>/ を開くと、局一覧・制御メッセージ・通報・信号・音声を
実時刻で表示する（局を選ぶと下のペインがその局になる。局は画面の「設定」からも足せる）。
音声は AMR-WB+（bash scripts/std-t115/build_amrwbplus.sh でビルド）。
入力の終端で受信は止まり、画面は見られるまま残る。Ctrl-C で録音を閉じて終了する。

オプション:
`, prog)
		fs.PrintDefaults()
	}
	if err := fs.Parse(cliflag.ReorderArgs(fs, argv)); err != nil {
		return 2
	}
	if err := cliflag.CheckNoArgs(fs); err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	scr, err := server.ParseScramble(*scramble)
	if err != nil {
		fmt.Fprintln(stderr, "-scramble:", err)
		return 2
	}
	plan, err := l.Plan("scramble")
	src := plan.Source
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	opts, err := server.OptsFromChannels(plan.Channels)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	if plan.Warn != "" {
		fmt.Fprintln(stderr, plan.Warn)
	}
	if l.LogDir == "" {
		fmt.Fprintln(stderr, "-log-dir は空にできない（受信のログと通報の記録の置き場所）")
		return 2
	}
	m := server.NewMonitor(server.Config{
		Source: src, SampleRate: l.SampleRate, Format: l.Format,
		CenterHz: plan.CenterHz, GainDB: l.GainDB, AGC: l.AGC, Realtime: !l.FullSpeed, Speed: l.Speed,
		Channels: plan.MHz(), Opts: opts, Scramble: scr, Scan: l.Scan, FollowVFO: plan.FollowVFO,
		LogDir: l.LogDir, NoIQ: *noIQ, PreS: *pre, LogOut: stderr, LedgerDir: l.LedgerDir(),
	})
	fmt.Fprintln(stderr, credit.Line())
	fmt.Fprintf(stderr, "ARIB STD-T115 QPSK ナロー方式 モニタ: http://%s/\n", l.Addr)
	fmt.Fprintf(stderr, "記録の保存先: %s/\n", rec.SystemDir(l.LogDir, server.System))
	if err := m.ListenAndServe(ctx, l.Addr); err != nil {
		fmt.Fprintln(stderr, "終了:", err)
		return 1
	}
	return 0
}
