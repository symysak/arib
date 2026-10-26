package main

import (
	"fmt"
	"os"

	"github.com/symysak/arib/internal/credit"
	"github.com/symysak/arib/internal/std-t86/cli"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	switch os.Args[1] {
	case "live":
		os.Exit(cmdLive(os.Args[2:]))
	case "version", "-version", "--version":
		fmt.Print(credit.Text("std-t86"))
		os.Exit(0)
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

  std-t86 live [オプション] -input <入力>[@<中心 MHz>][,gain=<dB>]…    ライブ受信 + Web モニタ（局は 1 本でも N 本でも）
  std-t86 version                      バージョンと作者表示

<入力>:
  録音ファイル (.wav / .cu8 / .cf32)      実時間ペースで再生（-full-speed で全速。名前から周波数が読めなければ @<MHz>）
  sdrsharp://host:port                     SDR# の同梱プラグイン（レート・形式・周波数は接続の中で伝わる）
  tcp://host:port                          生 I/Q ストリーム（SDR++ など。,fs= と ,fmt= が要る）
  rtltcp://host:port                       rtl_tcp（@<中心 MHz> に同調する。,gain= で利得）

`)
	fmt.Fprintln(os.Stderr, credit.Line())
}

func cmdLive(argv []string) int {
	ctx, stop := interruptContext()
	defer stop()
	return cli.Live(ctx, "std-t86 live", argv, os.Stderr)
}
