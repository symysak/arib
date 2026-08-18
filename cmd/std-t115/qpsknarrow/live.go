package main

import (
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/symysak/stdt86/internal/std-t115/qpsknarrow/server"
)

func cmdLive(argv []string) int {
	fs := flag.NewFlagSet("live", flag.ExitOnError)
	sampleRate := fs.Float64("fs", 0, "サンプルレート [Hz]（WAV なら省略可 / tcp:// では必須）")
	offset := fs.Float64("offset", 0, "チャネルオフセット [kHz]")
	addr := fs.String("addr", "127.0.0.1:8000", "HTTP の待ち受けアドレス")
	scramble := fs.Int("scramble", 0, "スクランブル値を固定する（0 = 自動判定。自治体ごとに違う）")
	fullSpeed := fs.Bool("full-speed", false, "録音ファイルを実時間ペースにせず全速で処理する")
	speed := fs.Float64("speed", 1, "実時間再生の倍率")
	format := fs.String("fmt", "cf32", "tcp:// の標本形式（cu8 / s16 / cf32）")
	freq := fs.Float64("freq", 0, "rtltcp:// の同調周波数 [Hz]")
	gain := fs.Float64("gain", 30, "rtltcp:// の手動利得 [dB]")
	agc := fs.Bool("agc", false, "rtltcp:// で自動利得にする")
	logDir := fs.String("log-dir", "logs",
		"記録の保存先（復号ログ・通報ごとの音声 WAV とサイドカー txt・通報区間の I/Q。空で保存しない）")
	noIQ := fs.Bool("no-iq", false,
		"通報区間の I/Q 録音をしない（既定は保存する。約 200kB/s）")
	fs.Usage = func() {
		fmt.Fprint(os.Stderr, `ARIB STD-T115 QPSK ナロー方式 ライブ Web モニタ

  qpsknarrow live <source> [オプション]

<source>:
  録音ファイル (.wav / .cf32 / .cu8)   実時間ペースで再生（-full-speed で全速）
  tcp://host:port                       生 I/Q ストリーム（SDR# / SDR++ プラグイン）
  rtltcp://host:port                    rtl_tcp

ブラウザで http://<addr>/ を開くと、制御メッセージ・通報・信号・音声を
実時刻で表示する。音声は AMR-WB+（bash scripts/std-t115/build_amrwbplus.sh でビルド）。

オプション:
`)
		fs.PrintDefaults()
	}
	if err := fs.Parse(argv); err != nil {
		return 2
	}
	if fs.NArg() != 1 {
		fs.Usage()
		return 2
	}

	p := server.NewPipeline(server.Config{
		Source:       fs.Arg(0),
		OffsetHz:     *offset * 1000,
		SampleRate:   *sampleRate,
		Format:       *format,
		FreqHz:       *freq,
		GainDB:       *gain,
		AGC:          *agc,
		Realtime:     !*fullSpeed,
		Speed:        *speed,
		ScrambleInit: *scramble,
		LogDir:       *logDir,
		NoIQ:         *noIQ,
	})
	srv := server.NewServer(p)
	bound, err := srv.ListenAndServe(*addr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		return 1
	}
	defer srv.Close()
	fmt.Printf("ARIB STD-T115 QPSK ナロー方式 モニタ: http://%s/\n", bound)
	if *logDir != "" {
		fmt.Printf("記録の保存先: %s/\n", *logDir)
	}

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	done := make(chan error, 1)
	go func() { done <- p.Run() }()

	select {
	case <-sig:
		fmt.Println("\n停止します…")
		p.Stop()
		return 0
	case err := <-done:
		if err != nil {
			fmt.Fprintf(os.Stderr, "処理エラー: %v\n", err)
			return 1
		}
		fmt.Println("ソース終端。Ctrl-C で終了します（画面は見られます）。")
		<-sig
		return 0
	}
}
