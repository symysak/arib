package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/symysak/stdt86/internal/std-t86/fec"
	"github.com/symysak/stdt86/internal/std-t86/g7221"
	"github.com/symysak/stdt86/internal/std-t86/iq"
	"github.com/symysak/stdt86/internal/std-t86/server"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	switch os.Args[1] {
	case "live":
		os.Exit(cmdLive(os.Args[2:]))
	case "-h", "--help", "help":
		usage()
		os.Exit(0)
	default:
		fmt.Fprintf(os.Stderr, "不明なサブコマンド: %s\n\n", os.Args[1])
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `ARIB STD-T86 受信デコーダ

  stdt86 live <source> [オプション]   ライブ受信 + Web モニタ

<source>:
  録音ファイル (.wav / .cu8 / .cf32)      実時間ペースで再生（回帰確認用）
  tcp://host:port                          生 I/Q ストリーム（SDR# / SDR++）
  rtltcp://host:port                       rtl_tcp

`)
}

func cmdLive(argv []string) int {
	fs := flag.NewFlagSet("live", flag.ExitOnError)
	municipalCode := fs.Int("municipal-code", 0,
		"市区町村コード（例 40225）。指定するとスクランブル値を固定する")
	seedFlag := fs.Int("seed", 0,
		"スクランブル値を直接指定（1..511）。--municipal-code より優先")
	offsetKHz := fs.Float64("offset", 0,
		"チャネルオフセット [kHz]（省略で 0 = 同調済みの前提）")
	sampleRate := fs.Float64("fs", 0,
		"サンプルレート [Hz]（ネットワークソース / 生 I/Q ファイルで必須）")
	freq := fs.Float64("freq", 0, "rtl_tcp のチューナ中心周波数 [Hz]")
	format := fs.String("fmt", "auto", "I/Q 形式 (auto/cu8/cs16/cf32)")
	syncThresh := fs.Float64("sync-thresh", 0.6, "同期ワード相関のしきい値")
	fullSpeed := fs.Bool("full-speed", false,
		"ファイル再生を実時間ペーシングせず全速で処理する")
	speed := fs.Float64("speed", 1.0, "ファイル再生の速度倍率")
	logDir := fs.String("log-dir", "logs",
		"通報音声 WAV / IQ 録音の保存先（空文字で保存しない）")
	host := fs.String("host", "127.0.0.1", "Web サーバの bind アドレス")
	port := fs.Int("port", 8000, "Web サーバのポート")
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "使い方: stdt86 live <source> [オプション]\n\n")
		fs.PrintDefaults()
	}
	var positional []string
	rest := argv
	for {
		if err := fs.Parse(rest); err != nil {
			return 2
		}
		if fs.NArg() == 0 {
			break
		}
		positional = append(positional, fs.Arg(0))
		rest = fs.Args()[1:]
	}
	if len(positional) < 1 {
		fs.Usage()
		return 2
	}
	source := positional[0]

	seed := 0
	switch {
	case *seedFlag != 0:
		seed = *seedFlag
	case *municipalCode != 0:
		s, err := fec.MunicipalCodeToSeed(*municipalCode)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 2
		}
		seed = s
	default:
		fmt.Println("スクランブル値: 自動判定モード（制御スロット蓄積後に確定します）")
	}
	offsetGiven := false
	for _, a := range argv {
		if a == "-offset" || a == "--offset" ||
			strings.HasPrefix(a, "-offset=") || strings.HasPrefix(a, "--offset=") {
			offsetGiven = true
		}
	}
	if !offsetGiven {
		fmt.Println("チャネルオフセット: 0 kHz（未指定。SDR# 等で同調済みの前提。" +
			"ベースバンドにオフセットがある録音は --offset で指定）")
	}

	isNetwork := strings.HasPrefix(source, "tcp://") || strings.HasPrefix(source, "rtltcp://")
	if isNetwork && *sampleRate <= 0 {
		fmt.Fprintln(os.Stderr, "ネットワークソースには --fs（サンプルレート [Hz]）が必須です。")
		return 2
	}

	src, err := iq.OpenSource(source, *sampleRate, *freq, *format, !*fullSpeed, *speed)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ソースを開けません: %v\n", err)
		return 1
	}

	seedDesc := "自動"
	if seed != 0 {
		seedDesc = fmt.Sprint(seed)
	}
	offDesc := "0(未指定)"
	if offsetGiven {
		offDesc = fmt.Sprintf("%+.1fkHz", *offsetKHz)
	}
	cfg := server.Config{
		F0Hz:          *offsetKHz * 1e3,
		Seed:          seed,
		MunicipalCode: *municipalCode,
		SyncThresh:    *syncThresh,
		LogDir:        *logDir,
		SourceDesc: fmt.Sprintf("%s (fs=%.4gMS/s, offset=%s, seed=%s)",
			source, src.SampleRate()/1e6, offDesc, seedDesc),
	}
	if !isNetwork {
		cfg.SourcePath = source
	}
	pipeline := server.NewPipeline(src, cfg)
	srv := server.NewServer(pipeline)

	if err := g7221.Available(true); err != nil {
		fmt.Fprintf(os.Stderr, "注意: %v\n（制御チャネルの受信は可能です。音声デコードのみ失敗します）\n", err)
	}
	if d := pipeline.AudioLogDir(); d != "" {
		fmt.Printf("デコード音声の保存先: %s (通報検出時に WAV を書き出します)\n", d)
	}

	addr := net.JoinHostPort(*host, fmt.Sprint(*port))
	httpSrv := &http.Server{Addr: addr, Handler: srv.Handler()}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ポートを開けません: %v\n", err)
		return 1
	}

	srv.Start()
	fmt.Printf("http://%s/ でライブモニタを開けます (Ctrl-C で終了)\n", addr)

	errCh := make(chan error, 1)
	go func() { errCh <- httpSrv.Serve(ln) }()

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	select {
	case <-sig:
	case err := <-errCh:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			fmt.Fprintf(os.Stderr, "HTTP サーバ: %v\n", err)
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_ = httpSrv.Shutdown(ctx)
	srv.Stop()
	return 0
}
