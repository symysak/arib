package server

import (
	"fmt"
	"math"
	"net/url"
	"strings"

	"github.com/symysak/arib/internal/rec"
	"github.com/symysak/arib/internal/std-t115/qpsknarrow/dsp"
	"github.com/symysak/arib/internal/wavio"
)

const (
	iqTargetRate = 8 * dsp.SymbolRate
	iqPrerollSec = 5.0
	iqTailSec = 2.0
)

func iqDecimation(fsIn float64) int {
	max := int(fsIn / iqTargetRate)
	if max < 1 {
		return 1
	}
	for f := max; f >= 1; f-- {
		if math.Mod(fsIn, float64(f)) == 0 && math.Mod(fsIn/float64(f), 1) == 0 {
			return f
		}
	}
	return max
}

type recDDC struct {
	nco   *dsp.NCO
	dec   *dsp.Decimator
	fsOut float64
}

func newRecDDC(fsIn, offsetHz float64) *recDDC {
	factor := iqDecimation(fsIn)
	r := &recDDC{dec: dsp.NewDecimator(fsIn, factor), fsOut: fsIn / float64(factor)}
	if offsetHz != 0 {
		r.nco = dsp.NewNCO(offsetHz, fsIn)
	}
	return r
}

func (r *recDDC) Process(x []complex64) []complex64 {
	if r.nco != nil {
		x = r.nco.Mix(x)
	}
	return r.dec.Process(x)
}


func recLabel(id int, target string) string {
	return fmt.Sprintf("broadcast%d%s", id, targetPart(target))
}

func fileURL(api, logDir, path string) string {
	return "api/" + api + "/" + (&url.URL{Path: rec.Rel(logDir, path)}).EscapedPath()
}

func targetPart(target string) string {
	t := strings.TrimSpace(target)
	switch {
	case t == "":
		return ""
	case strings.Contains(t, "一斉"):
		return "_一斉"
	}
	t = strings.ReplaceAll(t, "群/個別", "群個別")
	t = strings.ReplaceAll(t, " ", "")
	t = strings.ReplaceAll(t, "#", "")
	t = sanitize(t)
	if t == "" {
		return ""
	}
	return "_" + t
}

func sanitize(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch r {
		case '/', '\\', ':', '*', '?', '"', '<', '>', '|', 0:
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

func sidecarLines(b BroadcastInfo, mhz float64, extra [][2]string) []string {
	l := []string{
		"ARIB STD-T115 QPSK ナロー方式 通報記録",
		"（規格書 Volume 2 / チャネル間隔 7.5kHz / 11.25kbps / SCPC）",
		"",
		fmt.Sprintf("周波数        : %.5f MHz", mhz),
		fmt.Sprintf("通報 ID       : %d", b.ID),
		fmt.Sprintf("報知対象      : %s", b.Target),
		fmt.Sprintf("呼番号        : %d", b.CallNo),
		fmt.Sprintf("緊急          : %v", b.Emergency),
		fmt.Sprintf("途中参加      : %v", b.MidJoin),
	}
	for _, kv := range extra {
		if kv[1] != "" {
			l = append(l, fmt.Sprintf("%-14s: %s", kv[0], kv[1]))
		}
	}
	return l
}

func writeAudio(path string, pcm []int16, rate int) error {
	return wavio.Write(path, pcm, 1, rate)
}
