package netm10

import (
	"fmt"
	"strings"

	"github.com/symysak/arib/internal/bitpack"
	"github.com/symysak/arib/internal/data"
	"github.com/symysak/arib/internal/std-t102/l1"
)

const Name = "net-m10"

func init() {
	data.Register(data.Format{
		Name:  Name,
		Desc:  "製造者番号 10 の網の定時の放送と無線機の通報（UDCH1。中身は難読化されていて位置は読めない）",
		Ch:    l1.DataChannel,
		New:   func(data.Options) (data.Reader, error) { return newReader(), nil },
		Probe: probe,
	})
}

const bodyLen = 22

const (
	RepeatSec = 10
	BeaconSec = 600
)

func body(f data.Frame) ([]byte, bool) {
	if len(f.Bits) != 8+8*bodyLen {
		return nil, false
	}
	return bitpack.ToBytes(f.Bits[8:]), true
}

func kind(b []byte) byte { return b[0] & 0x3F }

func IsBeacon(b []byte) bool {
	return len(b) == bodyLen && (kind(b) == 0x28 || kind(b) == 0x24) && b[1] == 0 && b[2] == 0x04
}

func IsReport(b []byte) bool {
	if len(b) != bodyLen || b[21] != 0 || b[7]&0xFC != 0x38 {
		return false
	}
	switch kind(b) {
	case 0x09:
		return ((b[1] == 0x80 && b[3] == 0x10 && b[4] == 0x10) || (b[1] == 0x40 && b[3] == 0x04 && b[4] == 0xF0)) && CheckBytesOK(b)
	case 0x0B:
		return b[1] == 0xFF && b[2] == 0xFF && b[3] == 0x00 && b[4] == 0x01
	}
	return false
}

func addressee(b []byte) (byte, bool) {
	for i := 12; i+2 < 20; i++ {
		if b[i+1] == 0x10 && b[i+2] == 0x10 || b[i+1] == 0x00 && b[i+2] == 0x10 && b[i+3] == 0x10 {
			return b[i], true
		}
	}
	return 0, false
}

func CheckBytesOK(b []byte) bool {
	if len(b) != bodyLen {
		return false
	}
	borrow := byte(0)
	if b[11] >= b[12] {
		borrow = 1
	}
	return b[19] == b[12]-b[11]-1 && b[20] == b[8]+b[12] && b[16] == b[9]+b[15]+borrow && b[17] == ^(b[9]^b[10]^b[16])
}

func probe(frames []data.Frame) (ok, n int) {
	for _, f := range frames {
		b, good := body(f)
		if !good {
			continue
		}
		n++
		if IsBeacon(b) || IsReport(b) {
			ok++
		}
	}
	return ok, n
}

type reader struct {
	seen   map[string]float64
	last   map[byte]float64
	beacon string
	beaT   float64
}

func newReader() *reader { return &reader{seen: map[string]float64{}, last: map[byte]float64{}} }

func hexs(b []byte) string { return strings.ToUpper(fmt.Sprintf("% x", b)) }

func (r *reader) Read(f data.Frame) []data.Event {
	b, ok := body(f)
	if !ok {
		return nil
	}
	switch {
	case IsBeacon(b):
		k := hexs(b[1:])
		if k == r.beacon && f.TimeSec-r.beaT < BeaconSec && f.TimeSec >= r.beaT {
			return nil
		}
		r.beacon, r.beaT = k, f.TimeSec
		return []data.Event{{Kind: data.KindMessage, No: f.No, TimeSec: f.TimeSec, Format: Name, From: "基地局",
			Label: fmt.Sprintf("定時の放送（種別 %02X）%s", kind(b), f.Route), Raw: b,
			Rows: []data.Row{{Name: "計数（オクテット 0 の上位 2 ビット）", Text: fmt.Sprint(b[0] >> 6)}, {Name: "本体", Text: hexs(b[1:])}}}}
	case IsReport(b):
		k := hexs(b[1:])
		if t, dup := r.seen[k]; dup && f.TimeSec-t < RepeatSec && f.TimeSec >= t {
			return nil
		}
		r.seen[k] = f.TimeSec
		for kk, t := range r.seen {
			if f.TimeSec-t > RepeatSec || f.TimeSec < t {
				delete(r.seen, kk)
			}
		}
		return []data.Event{r.report(f, b)}
	}
	return nil
}

func (r *reader) report(f data.Frame, b []byte) data.Event {
	from, label := "全局あて", "全局あての通報"
	if kind(b) == 0x09 {
		from = fmt.Sprintf("無線機 %02X", b[2])
		label = from + " の通報"
	}
	rows := []data.Row{
		{Name: "種別・計数", Text: fmt.Sprintf("%02X・%d", kind(b), b[0]>>6)},
		{Name: "送り元（オクテット 2 = 無線機の番号）", Text: from},
	}
	if kind(b) == 0x09 {
		if t, ok := r.last[b[2]]; ok && f.TimeSec > t {
			rows = append(rows, data.Row{Name: "前の通報から", Text: fmt.Sprintf("%.0f 秒", f.TimeSec-t)})
		}
		r.last[b[2]] = f.TimeSec
	}
	chk := "合う（16・17・19・20）"
	if !CheckBytesOK(b) {
		chk = "合わない（形の違う通報）"
		if id, ok := addressee(b); ok {
			label = fmt.Sprintf("無線機 %02X あての指示（未詳）", id)
		}
	}
	rows = append(rows,
		data.Row{Name: "検査オクテット", Text: chk},
		data.Row{Name: "データ 7 の下位 2 ビット・8", Text: fmt.Sprintf("%d・%02X", b[7]&3, b[8])},
		data.Row{Name: "データ 9・10・14・15（6 ビットずつ）", Text: fmt.Sprintf("%d・%d・%d・%d", b[9]&0x3F, b[10]&0x3F, b[14]&0x3F, b[15]&0x3F)},
		data.Row{Name: "データ 13 の中の 4 ビット", Text: fmt.Sprint(b[13] >> 2 & 0xF)},
		data.Row{Name: "データ 11・12・18", Text: fmt.Sprintf("%02X・%02X・%02X", b[11], b[12], b[18])},
		data.Row{Name: "中身（オクテット 7〜20。通報ごとに無関係で、平文の位置ではない）", Text: hexs(b[7:21])},
	)
	return data.Event{Kind: data.KindMessage, No: f.No, TimeSec: f.TimeSec, Format: Name, From: from,
		Label: fmt.Sprintf("%s %s", label, f.Route), Raw: b, Rows: rows}
}
