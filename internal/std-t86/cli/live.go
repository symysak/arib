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
	"github.com/symysak/arib/internal/std-t86/decoder"
	"github.com/symysak/arib/internal/std-t86/fec"
	"github.com/symysak/arib/internal/std-t86/g7221"
	"github.com/symysak/arib/internal/std-t86/server"
)

func Live(ctx context.Context, prog string, argv []string, stderr io.Writer) int {
	fs := flag.NewFlagSet(prog, flag.ContinueOnError)
	fs.SetOutput(stderr)
	var l cliflag.Live
	l.Register(fs, cliflag.LiveDefaults{System: server.System, Addr: "127.0.0.1:8000", AGC: true,
		Lock: "制御メッセージの CRC16 が 3 通通るか"})
	municipalCode := fs.Int("municipal-code", 0, "全局の既定の市区町村コード（例 13101）。スクランブル値を固定する")
	seedFlag := fs.Int("seed", decoder.SeedAuto, "全局の既定のスクランブル値（0..511。-1 で自動判定）。-municipal-code より優先")
	syncThresh := fs.Float64("sync-thresh", 0.6, "同期ワード相関のしきい値")
	fs.Usage = func() {
		fmt.Fprintf(stderr, `使い方: %[1]s [オプション] -input <入力>[@<中心 MHz>][,gain=<dB>]…

  1 局（SDR# で同調済み）:  %[1]s -input sdrsharp://127.0.0.1:5555
  録音ファイル（1 局）:     %[1]s -input <録音.wav>      （名前に周波数が無ければ <録音.wav>@<MHz>）
  録音のオフセットを指定:   %[1]s -channels 58.505 -input <録音.wav>@58.6
  多局（局ごとの seed）:    %[1]s -channels 58.445:muni=13101,58.505:seed=302 -input rtltcp://127.0.0.1:1234@58.5
  走査:                     %[1]s -scan -input rtltcp://127.0.0.1:1234@58.5

  局の決め方: -channels も -scan も無ければ入力の中心の 1 局（中心 = 局。sdrsharp:// なら VFO に付いて行く）。
  -channels があれば入力の中心から各局を切り出す。-scan は帯域を見張って局を足す。
  局ごとの設定: :seed=<0..511|0x…|auto>（スクランブル値）・:muni=<市区町村コード>

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
	plan, err := l.Plan("seed", "muni")
	src := plan.Source
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	if plan.Warn != "" {
		fmt.Fprintln(stderr, plan.Warn)
	}
	if strings.HasPrefix(src, "tcp://") && l.SampleRate <= 0 {
		fmt.Fprintln(stderr, "tcp:// には -input の ,fs=（サンプルレート [Hz]）が必須です。")
		return 2
	}

	defaults := server.ChannelOpts{SyncThresh: *syncThresh, MunicipalCode: *municipalCode}
	switch {
	case *seedFlag >= 0:
		if *seedFlag >= fec.NSeeds {
			fmt.Fprintf(stderr, "-seed は 0..%d です\n", fec.NSeeds-1)
			return 2
		}
		defaults.SeedFixed = server.SeedPtr(*seedFlag)
	case *municipalCode != 0:
		s, err := fec.MunicipalCodeToSeed(*municipalCode)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 2
		}
		defaults.SeedFixed = &s
	}
	perMHz := map[float64]server.ChannelOpts{}
	for _, c := range plan.Channels {
		o, err := ChannelOpts(defaults, c)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 2
		}
		perMHz[c.MHz] = o
	}
	ec := l.Engine(plan)
	ec.PreS = server.IQPrerollS
	ec.LogOut = stderr

	if err := g7221.Available(true); err != nil {
		fmt.Fprintf(stderr, "注意: %v\n（制御チャネルの受信は可能です。音声デコードのみ失敗します）\n", err)
	}
	seedDesc := "seed=自動"
	if defaults.SeedFixed != nil {
		seedDesc = fmt.Sprintf("seed=%d", *defaults.SeedFixed)
	}
	app := server.New(server.Config{Engine: ec, Defaults: defaults, Channels: perMHz, Title: seedDesc,
		LedgerDir: l.LedgerDir()})
	fmt.Fprintln(stderr, credit.Line())
	fmt.Fprintf(stderr, "Web モニタ: http://%s/ （Ctrl-C で終了。通報の I/Q と音声は %s へ）\n", l.Addr, ec.Dir())
	if err := app.ListenAndServe(ctx, l.Addr); err != nil {
		fmt.Fprintln(stderr, "終了:", err)
		return 1
	}
	return 0
}

func parseInt(v string) (int, error) {
	if h, ok := strings.CutPrefix(strings.ToLower(v), "0x"); ok {
		n, err := strconv.ParseInt(h, 16, 64)
		return int(n), err
	}
	return strconv.Atoi(v)
}

func ChannelOpts(def server.ChannelOpts, c cliflag.Channel) (server.ChannelOpts, error) {
	o := def
	if v, ok := c.Opts["muni"]; ok {
		code, err := strconv.Atoi(v)
		if err != nil {
			return o, fmt.Errorf("-channels の %g MHz: muni=%q が読めない（市区町村コード。例 13101）", c.MHz, v)
		}
		s, err := fec.MunicipalCodeToSeed(code)
		if err != nil {
			return o, fmt.Errorf("-channels の %g MHz: %v", c.MHz, err)
		}
		o.MunicipalCode, o.SeedFixed = code, &s
	}
	if v, ok := c.Opts["seed"]; ok {
		if v == "auto" || v == "-1" {
			o.SeedFixed = nil
		} else {
			s, err := parseInt(v)
			if err != nil || s < 0 || s >= fec.NSeeds {
				return o, fmt.Errorf("-channels の %g MHz: seed=%q は 0..%d（0x 付きは 16 進）か auto", c.MHz, v, fec.NSeeds-1)
			}
			o.SeedFixed = &s
		}
	}
	return o, nil
}
