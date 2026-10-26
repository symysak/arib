package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/symysak/arib/internal/credit"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	switch os.Args[1] {
	case "live":
		os.Exit(cmdLive(os.Args[2:]))
	case "probe":
		os.Exit(cmdProbe(os.Args[2:]))
	case "version", "-version", "--version":
		fmt.Print(credit.Text("arib"))
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
	fmt.Fprintf(os.Stderr, `arib — 規格をまたいだ受信デコーダ

  arib live --std <%s> [オプション] -input <入力>[@<中心 MHz>][,gain=<dB>]…
      規格 1 つのライブ Web モニタ（局 1 本 / -channels で N 本 / -scan。各規格のバイナリの live と同じ）
  arib probe [オプション] -input <入力>[@<中心 MHz>][,gain=<dB>]…
      SDR# の VFO の I/Q（狭帯域の 1 局）を全規格で同時に試し、当たったものを鳴らして台帳へ残す
  arib version
      バージョンと作者表示

<入力>: 録音ファイル (.wav / .cf32 / .cu8)・sdrsharp://host:port・tcp://host:port・rtltcp://host:port

`, names())
	fmt.Fprintln(os.Stderr, credit.Line())
}

func cmdLive(argv []string) int {
	std, rest := "", make([]string, 0, len(argv))
	for i := 0; i < len(argv); i++ {
		a := argv[i]
		name := strings.TrimLeft(a, "-")
		switch {
		case a == "--":
			rest = append(rest, argv[i:]...)
			i = len(argv)
		case strings.HasPrefix(a, "-") && name == "std" && i+1 < len(argv):
			std = argv[i+1]
			i++
		case strings.HasPrefix(a, "-") && strings.HasPrefix(name, "std="):
			std = strings.TrimPrefix(name, "std=")
		default:
			rest = append(rest, a)
		}
	}
	s := lookup(std)
	if s == nil || s.Live == nil {
		if std == "" {
			fmt.Fprintf(os.Stderr, "arib live には --std <%s> が要る\n", names())
		} else {
			fmt.Fprintf(os.Stderr, "知らない規格 %q（%s）\n", std, names())
		}
		return 2
	}
	ctx, stop := interruptContext()
	defer stop()
	return s.Live(ctx, "arib live --std "+s.Name, rest, os.Stderr)
}
