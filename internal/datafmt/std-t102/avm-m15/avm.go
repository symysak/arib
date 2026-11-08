package avmm15

import (
	"encoding/binary"
	"fmt"
	"math"

	"github.com/symysak/arib/internal/bitpack"
	"github.com/symysak/arib/internal/data"
	"github.com/symysak/arib/internal/std-t102/l1"
	"github.com/symysak/arib/internal/std-t102/l3"
)

const Name = "avm-m15"

const Maker = 15

func init() {
	data.Register(data.Format{
		Name:  Name,
		Desc:  "製造者番号 15 の網の車両位置の通報（UDCH1 / FACCH2。NMEA の度分の緯度経度）",
		Ch:    l1.DataChannel,
		New:   func(data.Options) (data.Reader, error) { return newReader(), nil },
		Probe: probe,
	})
}

const (
	bodyLen = 22
	burstGapSec = 2.0
)

func body(f data.Frame) ([]byte, bool) {
	if len(f.Bits) != 8+8*bodyLen {
		return nil, false
	}
	return bitpack.ToBytes(f.Bits[8:]), true
}

func isHeader(b []byte) bool { return b[0] == 0x0F && b[1] == 0x00 && b[2] == 0x80 }

func isAddr(b []byte) bool { return b[0] == 0x09 && b[1] == 0x00 && b[2] == 0x80 }
func isReport(b []byte) bool {
	return b[0] == 0x0B && b[1] == 0x11 && b[2] == 0xF0 && b[3] == 0x02 && b[4] == 0x03
}
func isEnd(b []byte) bool { return b[0] == 0x88 && b[1]&0x7F == Maker }

func DegMin(v uint32) (deg float64, ok bool) {
	hi, frac := v>>16, (v&0xFFFF)>>1
	if frac >= 10000 {
		return 0, false
	}
	d, m := hi/100, float64(hi%100)+float64(frac)/10000
	if m >= 60 {
		return 0, false
	}
	return float64(d) + m/60, true
}

func position(b []byte) (lat, lon float64, ok bool) {
	la, ok1 := DegMin(binary.LittleEndian.Uint32(b[11:15]))
	lo, ok2 := DegMin(binary.LittleEndian.Uint32(b[15:19]))
	if !ok1 || !ok2 || la < 20 || la > 46 || lo < 122 || lo > 154 {
		return 0, 0, false
	}
	return la, lo, true
}

func probe(frames []data.Frame) (ok, n int) {
	for _, f := range frames {
		b, good := body(f)
		if !good || b[0] != 0x0B || b[1] != 0x11 {
			continue
		}
		n++
		if _, _, p := position(b); p && isReport(b) {
			ok++
		}
	}
	return ok, n
}

type burst struct {
	t0, last float64
	no       int64
	from     uint16
	to       uint16
	haveHdr  bool
	report   []byte
	extra    [][]byte
	ended    bool
	pkt *l3.B54Event
}

type reader struct {
	cur *burst
	b54 *l3.B54Asm
}

func newReader() *reader { return &reader{b54: l3.NewB54Asm(nil)} }

func (r *reader) Read(f data.Frame) []data.Event {
	b, ok := body(f)
	if !ok {
		return nil
	}
	var out []data.Event
	if r.cur != nil && (f.TimeSec-r.cur.last > burstGapSec || f.TimeSec < r.cur.last || (isHeader(b) && (r.cur.report != nil || r.cur.ended))) {
		out = append(out, r.flush()...)
	}
	if r.cur == nil {
		r.cur = &burst{t0: f.TimeSec, no: f.No}
	}
	c := r.cur
	c.last = f.TimeSec
	m := l3.Parse(f.Route, b)
	for _, e := range r.b54.Push(m) {
		if e.Kind == "message" {
			e := e
			c.pkt = &e
		}
	}
	switch {
	case isHeader(b):
		c.haveHdr = true
		c.from = binary.BigEndian.Uint16(b[3:5])
		c.to = binary.BigEndian.Uint16(b[5:7])
	case isAddr(b) && !c.haveHdr:
		c.haveHdr = true
		c.from = binary.BigEndian.Uint16(b[3:5])
		c.to = binary.BigEndian.Uint16(b[5:7])
		c.extra = append(c.extra, append([]byte(nil), b...))
	case isReport(b):
		c.report = append([]byte(nil), b...)
	case isEnd(b):
		c.ended = true
		out = append(out, r.flush()...)
	case m.B54 != nil:
	default:
		c.extra = append(c.extra, append([]byte(nil), b...))
	}
	return out
}

func (r *reader) flush() []data.Event {
	c := r.cur
	r.cur = nil
	if c == nil || (!c.haveHdr && c.report == nil) {
		return nil
	}
	from := fmt.Sprintf("不明 #%d", c.no)
	if c.haveHdr {
		from = fmt.Sprintf("%d", c.from)
	}
	rows := []data.Row{}
	if c.haveHdr {
		rows = append(rows, data.Row{Name: "送り元", Text: fmt.Sprintf("%d（0x%04X）", c.from, c.from)},
			data.Row{Name: "宛先", Text: fmt.Sprintf("%d（0x%04X）", c.to, c.to)})
	}
	for _, x := range c.extra {
		rows = append(rows, data.Row{Name: fmt.Sprintf("種別 0x%02X（未詳）", x[0]), Text: fmt.Sprintf("% X", x)})
	}
	if c.report == nil {
		return []data.Event{{Kind: data.KindMessage, No: c.no, TimeSec: c.t0, Format: Name, From: from,
			Label: fmt.Sprintf("車 %s の送信（位置の通報なし）", from), Rows: rows}}
	}
	b := c.report
	ev := data.Event{Kind: data.KindPosition, No: c.no, TimeSec: c.t0, Format: Name, From: from, Raw: b}
	lat, lon, ok := position(b)
	fix := b[10] == 0
	sec := binary.LittleEndian.Uint16(b[5:7])
	rows = append(rows,
		data.Row{Name: "通しの秒", Text: fmt.Sprintf("%d", sec)},
		data.Row{Name: "状態", Text: fmt.Sprintf("0x%02X", b[8])},
		data.Row{Name: "測位", Text: map[bool]string{true: "できている", false: fmt.Sprintf("できていない（旗 0x%02X。緯度経度は前の値）", b[10])}[fix]},
		data.Row{Name: "緯度", Text: degText(b[11:15], lat, ok, "N")},
		data.Row{Name: "経度", Text: degText(b[15:19], lon, ok, "E")},
		data.Row{Name: "未詳", Text: fmt.Sprintf("% X", b[19:22])},
		data.Row{Name: "測地系", Text: "世界測地系（実波の軌跡が道路に乗ることで確かめた）"})
	switch p := c.pkt; {
	case p == nil:
		rows = append(rows, data.Row{Name: "枠（ARIB STD-B54 のメッセージモード）", Text: "組めない（ヘッダか後半のブロックを取りこぼした）。32 ビット CRC は確かめられない"})
	case !p.CRCOK:
		rows = append(rows, data.Row{Name: "枠（ARIB STD-B54 のメッセージモード）", Text: "32 ビット CRC 誤り（位置は前半のブロックだけで読んだ）"})
	default:
		rows = append(rows, data.Row{Name: "枠（ARIB STD-B54 のメッセージモード）",
			Text: fmt.Sprintf("32 ビット CRC OK・コマンド %s・ユーザデータ %d バイト", l3.CommandName(p.Command), len(p.Data))})
		if len(p.Data) > 18 {
			rows = append(rows, data.Row{Name: "未詳（後半のブロック）", Text: fmt.Sprintf("% X", p.Data[18:])})
		}
	}
	ev.Rows = rows
	label := fmt.Sprintf("車 %s", from)
	switch {
	case ok && fix:
		ev.Pos = &data.Position{Lat: lat, Lon: lon, Heading: -1}
		label += fmt.Sprintf(" %.5f, %.5f", lat, lon)
	case ok:
		label += fmt.Sprintf(" 測位なし（前の値 %.5f, %.5f）", lat, lon)
	default:
		label += " 位置が読めない"
	}
	ev.Label = label
	return []data.Event{ev}
}

func degText(b []byte, v float64, ok bool, hemi string) string {
	raw := binary.LittleEndian.Uint32(b)
	if !ok {
		return fmt.Sprintf("読めない（0x%08X）", raw)
	}
	d := math.Floor(v)
	return fmt.Sprintf("%s %.0f°%.4f′ = %.6f°（0x%08X）", hemi, d, (v-d)*60, v, raw)
}
