package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"sort"

	"github.com/symysak/arib/internal/std-t115/qpsknarrow/amrwbp"
	dec "github.com/symysak/arib/internal/std-t115/qpsknarrow/decoder"
	"github.com/symysak/arib/internal/std-t115/qpsknarrow/dsp"
	"github.com/symysak/arib/internal/std-t115/qpsknarrow/iqsrc"
	"github.com/symysak/arib/internal/std-t115/qpsknarrow/municipality"
)

func main() {
	if len(os.Args) >= 2 && os.Args[1] == "live" {
		os.Exit(cmdLive(os.Args[2:]))
	}
	args := os.Args[1:]
	if len(args) >= 1 && args[0] == "decode" {
		args = args[1:]
	}
	os.Exit(cmdDecode(args))
}

func cmdDecode(argv []string) int {
	fs := flag.NewFlagSet("std-t115-qpsknarrow", flag.ExitOnError)
	sampleRate := fs.Float64("fs", 0, "サンプルレート [Hz]（WAV なら省略可）")
	offset := fs.Float64("offset", 0, "チャネルオフセット [kHz]")
	startSec := fs.Float64("start", 0, "開始位置 [s]")
	durSec := fs.Float64("duration", 0, "処理長 [s]（0 = 最後まで）")
	blockSec := fs.Float64("block", 0, "処理ブロック長 [s]（0 = 既定 0.5。長くすると境界で取りこぼす）")
	overlapSec := fs.Float64("overlap", 0.25, "ブロックの重なり [s]（0 でフレーム取りこぼしが増える）")
	scramble := fs.Int("scramble", 0, "スクランブル値を固定する（0 = 自動判定。自治体ごとに違う）")
	quiet := fs.Bool("quiet", false, "フレーム毎の行を出さず集計だけ表示")
	wavOut := fs.String("wav", "", "復号音声の書き出し先（16kHz モノラル WAV）")
	logPath := fs.String("log", "", "復号ログの書き出し先（空で書かない）")
	fs.Usage = func() {
		fmt.Fprint(os.Stderr, `ARIB STD-T115 QPSK ナロー方式 受信デコーダ

  std-t115-qpsknarrow <file> [オプション]        制御チャネルを復号して一覧表示
  std-t115-qpsknarrow live <file> [オプション]   ライブ Web モニタ（http://127.0.0.1:8000/）

STD-T115 の 3 方式のうち QPSK ナロー方式（Volume 2, チャネル間隔 7.5kHz,
11.25kbps, 80ms/900bit）専用。STD-T86 とも他 2 方式とも互換性は無い。

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
	return run(fs.Arg(0), runOpts{
		sampleRate: *sampleRate,
		offsetHz:   *offset * 1000,
		startSec:   *startSec,
		durSec:     *durSec,
		blockSec:   *blockSec,
		overlapSec: *overlapSec,
		scramble:   *scramble,
		quiet:      *quiet,
		wavOut:     *wavOut,
		logPath:    *logPath,
	})
}

type runOpts struct {
	sampleRate float64
	offsetHz   float64
	startSec   float64
	durSec     float64
	blockSec   float64
	overlapSec float64
	scramble   int
	quiet      bool
	wavOut     string
	logPath    string
}

func run(path string, o runOpts) int {
	r, err := iqsrc.OpenFile(path, o.sampleRate)
	if err != nil {
		fmt.Fprintf(os.Stderr, "入力を開けません: %v\n", err)
		return 1
	}
	defer r.Close()
	rate := r.SampleRate()

	fmt.Printf("入力: %s\n", path)
	fmt.Printf("  %.0f Hz, %.1f s, オフセット %+.1f kHz\n",
		rate, float64(r.Len())/rate, o.offsetHz/1000)
	fmt.Printf("方式: STD-T115 QPSK ナロー（%.0f sym/s, %d bit/フレーム, RRC α=%.1f）\n",
		dsp.SymbolRate, dsp.FrameBits, dsp.RollOff)
	fmt.Printf("同期ワード: SB0下り SW1=%s / SC下り SW3=%s\n\n", dsp.SW1Hex, dsp.SW3Hex)

	start := int64(o.startSec * rate)
	if start > 0 {
		if err := r.Skip(start); err != nil {
			fmt.Fprintf(os.Stderr, "シークできません: %v\n", err)
			return 1
		}
	}
	var scrInfo dec.ScrambleInfo
	d := dec.New(dec.Config{
		SampleRate:   rate,
		OffsetHz:     o.offsetHz,
		ScrambleInit: o.scramble,
		BlockSec:     o.blockSec,
		OverlapSec:   o.overlapSec,
		OnScramble: func(si dec.ScrambleInfo) {
			scrInfo = si
			if !o.quiet {
				fmt.Printf("スクランブル値を自動判定: 0x%04X（信頼度 %.3f, %s %d 枚, t=%.2fs）→ %s\n\n",
					si.Init, si.Confidence, si.Source, si.Frames, si.TimeSec,
					municipality.FromScramble(si.Init).Label())
			}
		},
	})
	d.SetStartSample(start)

	limit := int64(-1)
	if o.durSec > 0 {
		limit = start + int64(o.durSec*rate)
	}
	chunk := int(0.5 * rate)

	var logw *os.File
	if o.logPath != "" {
		f, err := os.Create(o.logPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "ログを作れません: %v\n", err)
			return 1
		}
		defer f.Close()
		logw = f
		fmt.Fprintf(logw, "# ARIB STD-T115 QPSK ナロー方式 復号ログ\n")
		fmt.Fprintf(logw, "# 入力: %s（%.0f Hz, オフセット %+.1f kHz）\n",
			path, rate, o.offsetHz/1000)
	}

	var nSB0, nSC, nCRC, nTCH, nTCHOK int
	msgCount := map[string]int{}
	var asm amrwbp.Assembler
	var superframes [][]amrwbp.Frame
	report := func(frames []dec.Frame) {
		for _, f := range frames {
			switch f.Kind {
			case dec.KindSB0:
				nSB0++
			case dec.KindSC:
				nSC++
			}
			if f.CRCOK {
				nCRC++
			}
			if f.Kind == dec.KindSC {
				nTCH++
				if f.TCHCRCOK {
					nTCHOK++
				}
			}
			if f.Message != nil {
				msgCount[f.Message.Header.TypeName]++
			}
			if f.TCHMessage != nil {
				msgCount[f.TCHMessage.Header.TypeName]++
			}
			if f.Kind == dec.KindSC && f.AMRSN >= 0 {
				asm.Push(f.TimeSec, f.AMRSN, f.Voice)
				superframes = append(superframes, asm.Superframes()...)
			}
			if o.quiet && logw == nil {
				continue
			}
			line := fmt.Sprintf("t=%8.3fs %-3s 相関 %.2f", f.TimeSec, f.Kind, f.CorrPeak)
			if f.Kind == dec.KindSC {
				if f.TCHMessage != nil {
					line += fmt.Sprintf("  %s SN=%d",
						f.TCHMessage.Header.TypeName, f.AMRSN)
				} else {
					line += "  TCH CRC NG"
				}
			}
			if f.Kind == dec.KindSB0 {
				switch {
				case f.Message != nil:
					line += "  " + f.Message.String()
				case f.CRCOK:
					line += "  CRC OK（解釈できないメッセージ）"
				default:
					line += "  CRC NG"
				}
			}
			if !o.quiet {
				fmt.Println(line)
			}
			if logw != nil {
				fmt.Fprintln(logw, line)
			}
		}
	}

	pos := start
	for {
		if limit > 0 && pos >= limit {
			break
		}
		n := chunk
		if limit > 0 && pos+int64(n) > limit {
			n = int(limit - pos)
		}
		x, err := r.Read(n)
		if len(x) == 0 || errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			fmt.Fprintf(os.Stderr, "読み取りエラー: %v\n", err)
			break
		}
		pos += int64(len(x))
		report(d.Feed(x))
	}
	report(d.Flush())

	fmt.Printf("\n=== 集計 ===\n")
	total := nSB0 + nSC
	fmt.Printf("フレーム: %d 枚（SB0 %d / SC %d）\n", total, nSB0, nSC)
	switch {
	case o.scramble != 0:
		fmt.Printf("スクランブル値: 0x%04X（固定）\n", o.scramble)
	case scrInfo.Init != 0:
		fmt.Printf("スクランブル値: 0x%04X（自動判定 信頼度 %.3f / %s %d 枚）\n",
			scrInfo.Init, scrInfo.Confidence, scrInfo.Source, scrInfo.Frames)
	default:
		fmt.Printf("スクランブル値: 判定できず（バーストが取れていない）\n")
	}
	if scr := max(o.scramble, scrInfo.Init); scr != 0 {
		fmt.Printf("市区町村: %s\n", municipality.FromScramble(scr).Label())
	}
	if nSB0 > 0 {
		fmt.Printf("CCH CRC16 一致: %d/%d (%.1f%%)\n",
			nCRC, nSB0, 100*float64(nCRC)/float64(nSB0))
	}
	if nTCH > 0 {
		fmt.Printf("TCH CRC16 一致: %d/%d (%.1f%%)\n",
			nTCHOK, nTCH, 100*float64(nTCHOK)/float64(nTCH))
	}
	fmt.Printf("音声: スーパーフレーム %d 個（%.1f 秒相当）, 組立不能 %d, 欠落補間 %d 枚（%.2f 秒）",
		len(superframes), float64(len(superframes))*0.160, asm.Dropped(),
		asm.Filled(), float64(asm.Filled())*0.080)
	if asm.Capped() > 0 {
		fmt.Printf(", 上限超過で詰めた %d 枚", asm.Capped())
	}
	fmt.Println()
	if o.wavOut != "" {
		if err := writeVoice(o.wavOut, superframes); err != nil {
			fmt.Fprintf(os.Stderr, "音声の書き出しに失敗: %v\n", err)
		}
	}
	if len(msgCount) > 0 {
		fmt.Println("制御メッセージ:")
		keys := make([]string, 0, len(msgCount))
		for k := range msgCount {
			keys = append(keys, k)
		}
		sort.Slice(keys, func(i, j int) bool { return msgCount[keys[i]] > msgCount[keys[j]] })
		for _, k := range keys {
			fmt.Printf("  %-28s %d\n", k, msgCount[k])
		}
	}
	return 0
}

func writeVoice(path string, sf [][]amrwbp.Frame) error {
	if len(sf) == 0 {
		return fmt.Errorf("音声フレームがありません")
	}
	if !amrwbp.Available() {
		return fmt.Errorf("AMR-WB+ デコーダが未ビルドです（bash scripts/std-t115/build_amrwbplus.sh）")
	}
	pcm, err := amrwbp.Decode(sf)
	if err != nil {
		return err
	}
	if err := writeWav16(path, pcm, amrwbp.OutputRate); err != nil {
		return err
	}
	fmt.Printf("音声を書き出しました: %s（%.1f 秒, %d 標本, RMS %.0f）\n",
		path, float64(len(pcm))/float64(amrwbp.OutputRate), len(pcm), amrwbp.RMS(pcm))
	return nil
}

func writeWav16(path string, pcm []int16, rate int) error {
	n := len(pcm) * 2
	h := make([]byte, 0, 44+n)
	le32 := func(v uint32) { h = append(h, byte(v), byte(v>>8), byte(v>>16), byte(v>>24)) }
	le16 := func(v uint16) { h = append(h, byte(v), byte(v>>8)) }
	h = append(h, "RIFF"...)
	le32(uint32(36 + n))
	h = append(h, "WAVEfmt "...)
	le32(16)
	le16(1)
	le16(1)
	le32(uint32(rate))
	le32(uint32(rate * 2))
	le16(2)
	le16(16)
	h = append(h, "data"...)
	le32(uint32(n))
	for _, v := range pcm {
		h = append(h, byte(uint16(v)), byte(uint16(v)>>8))
	}
	return os.WriteFile(path, h, 0o644)
}
