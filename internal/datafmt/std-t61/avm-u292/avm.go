package avmu292

import (
	"fmt"
	"strings"

	"github.com/symysak/arib/internal/bitpack"
	"github.com/symysak/arib/internal/data"
	"github.com/symysak/arib/internal/geo"
	"github.com/symysak/arib/internal/sjis"
	scpca1u292 "github.com/symysak/arib/internal/std-t61/scpc-a1u292"
)

const Name = "avm-u292"

func init() {
	data.Register(data.Format{
		Name:  Name,
		Desc:  "付録 1 の U（u292）の車両位置の通報・基地局の応答・定時の放送",
		Ch:    scpca1u292.Channel,
		New:   func(data.Options) (data.Reader, error) { return newReader(), nil },
		Probe: probe,
	})
}

const (
	RepeatSec = 300
	PartSec = 3
	MaxParts = 30
)

const (
	splitFirst  = 0b00
	splitMid    = 0b01
	splitLast   = 0b11
	splitSingle = 0b10
)

func bits(b []uint8, from, to int) uint64 {
	var v uint64
	for _, x := range b[from : to+1] {
		v = v<<1 | uint64(x&1)
	}
	return v
}

func bitStr(b []uint8, from, to int) string {
	var sb strings.Builder
	for _, x := range b[from : to+1] {
		sb.WriteByte('0' + x&1)
	}
	return sb.String()
}

func kind(b []uint8) uint64  { return bits(b, 0, 3) }
func split(b []uint8) uint64 { return bits(b, 0, 1) }

func isZero(b []uint8) bool {
	for _, x := range b {
		if x&1 != 0 {
			return false
		}
	}
	return true
}

type body struct {
	c     []uint8
	L     int
	fits  bool
	sumOK bool
	xorOK bool
	form  int
}

func octet(c []uint8, i int) uint64 { return bits(c, 8*i, 8*i+7) }

func parseBody(c []uint8) body {
	m := body{c: c}
	if len(c) < 24 {
		return m
	}
	m.L = int(bits(c, 12, 23))
	if 8*(m.L+3) > len(c) {
		return m
	}
	m.fits = true
	var sum, x uint64
	for i := 0; i <= m.L; i++ {
		v := octet(c, i)
		sum += v
		x ^= v
	}
	m.sumOK = sum&0xFF == octet(c, m.L+1)
	m.xorOK = x == octet(c, m.L+2)
	switch {
	case m.sumOK && m.xorOK:
		m.form = 1
	case octet(c, m.L+1) == octet(c, 0)^octet(c, 1)^octet(c, 2) && octet(c, m.L+2) == 3:
		m.form = 2
	default:
		m.form = 3
	}
	return m
}

func (m body) kind1() uint64 { return bits(m.c, 24, 29) }

func (m body) sumRow(off int) data.Row {
	if !m.fits {
		return data.Row{Name: "長さ・和・XOR", Text: fmt.Sprintf("長さ %d オクテットが本体を超える", m.L)}
	}
	at := 8 * (m.L + 1)
	return data.Row{Name: fmt.Sprintf("和（%d〜%d）", off+at, off+at+7), Bits: bitStr(m.c, at, at+7), Text: okText(m.sumOK)}
}

func okText(ok bool) string {
	if ok {
		return "合う"
	}
	return "合わない"
}

func (m body) summary() string {
	if !m.fits {
		return fmt.Sprintf("長さ %d オクテット（本体を超える）", m.L)
	}
	t := fmt.Sprintf("長さ %d オクテット・形 %d", m.L, m.form)
	if m.form == 1 {
		t += fmt.Sprintf("・種別 0x%02X", m.kind1())
	}
	return t
}

func (m body) ack() (uint64, bool) {
	if m.form != 1 || m.kind1() != 0x16 || len(m.c) < 48 || m.c[65]&1 != 0 {
		return 0, false
	}
	return bits(m.c, 36, 47), true
}

type message struct {
	No        uint64
	Sep       string
	Text      string
	Pos       *data.Position
	Truncated bool
}

func (m body) message() (message, bool) {
	var t message
	if m.form != 1 || m.kind1() != 0x0F || len(m.c) < 128 {
		return t, false
	}
	noAt, lenAt, from := 80, 73, 392
	t.Sep = ":"
	withPos := m.c[30]&1 == 1
	if withPos {
		noAt, lenAt, from = 128, 121, 440
		t.Sep = ">"
	}
	if len(m.c) < noAt+12 {
		return t, false
	}
	t.No = bits(m.c, noAt, noAt+11)
	end := from + 16*int(bits(m.c, lenAt, lenAt+6))
	if end > len(m.c) {
		end = from + (len(m.c)-from)/8*8
		t.Truncated = true
	}
	if from < end {
		raw, _ := sjis.Decode(bitpack.ToBytes(m.c[from:end]))
		t.Text = strings.TrimRight(strings.Map(func(r rune) rune {
			if r < 0x20 {
				return ' '
			}
			return r
		}, raw), " ")
	}
	if withPos {
		lonV, latV := bits(m.c, 90, 109), bits(m.c, 67, 86)
		if lonV != 1<<20-1 || latV != 1<<20-1 {
			lat, lon := geo.TokyoToWGS84(26+float64(latV)/36000, 127+float64(lonV)/36000)
			t.Pos = &data.Position{Lat: lat, Lon: lon, Heading: -1}
		}
	}
	return t, true
}

func lrcOK(b []uint8, from, n, at int) bool {
	var x uint64
	for i := 0; i < n; i++ {
		x ^= bits(b, from+8*i, from+8*i+7)
	}
	return x == bits(b, at, at+7)
}

func isReport(b []uint8) bool {
	return len(b) == scpca1u292.BodyBits && kind(b) == 0b1000 && lrcOK(b, 4, 15, 132)
}
func isReply(b []uint8) bool {
	return len(b) == scpca1u292.BodyBits && kind(b) == 0b1000 && lrcOK(b, 4, 13, 116)
}

func probe(frames []data.Frame) (ok, n int) {
	for _, f := range frames {
		if len(f.Bits) != scpca1u292.BodyBits || kind(f.Bits) != 0b1000 {
			continue
		}
		n++
		if isReport(f.Bits) || isReply(f.Bits) {
			ok++
		}
	}
	return ok, n
}

type reader struct {
	cars  map[uint64]uint64
	seen  map[string]float64
	seenE map[string]data.Event
	parts []data.Frame
	partN int
	lastB string
	lastE data.Event
	sameB int
}

func newReader() *reader {
	return &reader{seen: map[string]float64{}, seenE: map[string]data.Event{}, cars: map[uint64]uint64{}}
}

func (r *reader) keep(h string, ev data.Event) []data.Event {
	r.seenE[h] = ev
	return []data.Event{ev}
}

func (r *reader) built(ev data.Event, n int) []data.Event {
	ev.Count = n
	r.lastE = ev
	return []data.Event{ev}
}

func (r *reader) Read(f data.Frame) []data.Event {
	b := f.Bits
	if len(b) != scpca1u292.BodyBits {
		return nil
	}
	for k, t := range r.seen {
		if f.TimeSec-t > RepeatSec || f.TimeSec < t {
			delete(r.seen, k)
			delete(r.seenE, k)
		}
	}
	if !isZero(b) && split(b) != splitSingle {
		return r.assemble(f)
	}
	raw := bitpack.ToBytes(b)
	h := fmt.Sprintf("%X", raw)
	if _, dup := r.seen[h]; dup {
		r.seen[h] = f.TimeSec
		if ev, ok := r.seenE[h]; ok {
			return []data.Event{data.AgainOf(ev, f, 1)}
		}
		return nil
	}
	r.seen[h] = f.TimeSec
	switch {
	case isReport(b):
		r.cars[bits(b, 29, 47)&63] = bits(b, 29, 47)
		return r.keep(h, report(f, raw))
	case isReply(b):
		return r.keep(h, r.reply(f, raw))
	}
	m := parseBody(b[4:])
	if no, ok := m.ack(); ok {
		return r.keep(h, data.Event{Kind: data.KindMessage, No: f.No, TimeSec: f.TimeSec, Format: Name,
			Label: fmt.Sprintf("了解 → 番号 %d %s", no, f.Route), Raw: raw,
			Rows: []data.Row{{Name: "列", Text: m.summary()}, {Name: "了解の番号（40〜51）", Bits: bitStr(b, 40, 51), Text: fmt.Sprint(no)}}})
	}
	return r.keep(h, data.Event{Kind: data.KindMessage, No: f.No, TimeSec: f.TimeSec, Format: Name,
		Label: fmt.Sprintf("種別 %s（読めない形）%s", bitStr(b, 0, 3), f.Route), Raw: raw,
		Rows: []data.Row{{Name: "列", Text: m.summary()}}})
}

var speedText = map[uint64]string{0: "止まっている", 1: "低速（時速 10〜30 km 前後）", 2: "時速 20〜50 km 前後", 3: "時速 50 km 前後より速い"}

func report(f data.Frame, raw []byte) data.Event {
	b := f.Bits
	m := parseBody(b[4:])
	id := bits(b, 29, 47)
	ev := data.Event{Kind: data.KindPosition, No: f.No, TimeSec: f.TimeSec, Format: Name, From: fmt.Sprintf("車 %d", id), Raw: raw}
	head := bits(b, 74, 76) * 45
	sp := bits(b, 72, 73)
	flags := bitStr(b, 48, 48) + bitStr(b, 50, 50) + bitStr(b, 61, 62) + bitStr(b, 68, 68)
	ev.Rows = []data.Row{
		{Name: "車の識別", Bits: bitStr(b, 29, 47), Text: fmt.Sprintf("%d", id)},
		{Name: "別の切り方（種別 28〜33・番号 36〜46・旗 47〜48）", Bits: bitStr(b, 28, 48),
			Text: fmt.Sprintf("種別 0x%02X・番号 %d・旗 %s", bits(b, 28, 33), bits(b, 36, 46), bitStr(b, 47, 48))},
		{Name: "状態の旗（48・50・61・62・68）", Bits: flags, Text: "意味は未詳"},
		{Name: "速さの区分", Bits: bitStr(b, 72, 73), Text: speedText[sp]},
		{Name: "進行方向", Bits: bitStr(b, 74, 76), Text: fmt.Sprintf("%d°", head)},
	}
	latV, lonV := bits(b, 77, 96), bits(b, 97, 116)
	if latV == 1<<20-1 && lonV == 1<<20-1 {
		ev.Label = fmt.Sprintf("%s 測位できていない %s", ev.From, f.Route)
		ev.Rows = append(ev.Rows, data.Row{Name: "位置", Bits: bitStr(b, 77, 116), Text: "測位できていない（全部 1）"})
	} else {
		tlat, tlon := 26+float64(latV)/36000, 127+float64(lonV)/36000
		lat, lon := geo.TokyoToWGS84(tlat, tlon)
		ev.Pos = &data.Position{Lat: lat, Lon: lon, Heading: float64(head)}
		if sp == 0 {
			ev.Pos.Heading = -1
		}
		ev.Label = fmt.Sprintf("%s %.5f, %.5f %s %d° %s", ev.From, lat, lon, speedText[sp], head, f.Route)
		ev.Rows = append(ev.Rows,
			data.Row{Name: "緯度", Bits: bitStr(b, 77, 96), Text: fmt.Sprintf("%.6f（日本測地系 %.6f）", lat, tlat)},
			data.Row{Name: "経度", Bits: bitStr(b, 97, 116), Text: fmt.Sprintf("%.6f（日本測地系 %.6f）", lon, tlon)})
	}
	ev.Rows = append(ev.Rows, data.Row{Name: "長さ（16〜27）", Bits: bitStr(b, 16, 27), Text: fmt.Sprintf("%d オクテット", m.L)}, m.sumRow(4))
	if m.fits && !m.sumOK {
		ev.Label += "（和が合わない）"
	}
	if f.Idle {
		ev.Label += "（繰り返しで受けた）"
	}
	return ev
}

func (r *reader) reply(f data.Frame, raw []byte) data.Event {
	b := f.Bits
	m := parseBody(b[4:])
	low := bits(b, 47, 52)
	to := fmt.Sprintf("車 下位 6 ビット %d", low)
	if id, ok := r.cars[low]; ok {
		to = fmt.Sprintf("車 %d", id)
	}
	what := "基地局の応答"
	rows := []data.Row{
		{Name: "識別", Bits: bitStr(b, 29, 47), Text: fmt.Sprintf("%d", bits(b, 29, 47))},
		{Name: "宛先（47〜52 = 車の識別の下位 6 ビット）", Bits: bitStr(b, 47, 52), Text: to},
	}
	if no, ok := m.ack(); ok {
		what = "基地局の了解"
		rows = append(rows, data.Row{Name: "了解の番号（40〜51）", Bits: bitStr(b, 40, 51), Text: fmt.Sprint(no)})
	}
	rows = append(rows, data.Row{Name: "長さ（16〜27）", Bits: bitStr(b, 16, 27), Text: fmt.Sprintf("%d オクテット", m.L)}, m.sumRow(4))
	label := fmt.Sprintf("%s → %s %s", what, to, f.Route)
	if m.fits && !m.sumOK {
		label += "（和が合わない）"
	}
	return data.Event{Kind: data.KindMessage, No: f.No, TimeSec: f.TimeSec, Format: Name, From: "基地局", Label: label, Raw: raw, Rows: rows}
}

func (r *reader) assemble(f data.Frame) []data.Event {
	pos := split(f.Bits)
	if pos == splitFirst || (len(r.parts) > 0 && f.TimeSec-r.parts[0].TimeSec > PartSec) {
		r.parts, r.partN = nil, 0
	}
	if pos != splitFirst && len(r.parts) == 0 {
		return nil
	}
	r.partN++
	if n := len(r.parts); n > 0 && string(r.parts[n-1].Bits) == string(f.Bits) {
		return nil
	}
	r.parts = append(r.parts, f)
	if pos != splitLast {
		if len(r.parts) >= MaxParts {
			r.parts, r.partN = nil, 0
		}
		return nil
	}
	parts, partN := r.parts, r.partN
	r.parts, r.partN = nil, 0
	var rows []data.Row
	var raw []byte
	var col []uint8
	var key strings.Builder
	for i, p := range parts {
		pb := bitpack.ToBytes(p.Bits)
		raw = append(raw, pb...)
		col = append(col, p.Bits[4:]...)
		fmt.Fprintf(&key, "%X/", pb)
		rows = append(rows, data.Row{Name: fmt.Sprintf("%d 枚目（種別 %s）", i+1, bitStr(p.Bits, 0, 3)), Text: fmt.Sprintf("%X", pb)})
	}
	if key.String() == r.lastB {
		r.sameB++
		return []data.Event{data.AgainOf(r.lastE, f, partN)}
	}
	r.lastB, r.sameB = key.String(), 0
	m := parseBody(col)
	rows = append([]data.Row{{Name: fmt.Sprintf("組み上げた本体の列（%d 枚・%d ビット）", len(parts), len(col)), Text: m.summary()}}, rows...)
	ev := data.Event{Kind: data.KindMessage, No: parts[0].No, TimeSec: parts[0].TimeSec, Format: Name, Raw: raw}
	if t, ok := m.message(); ok {
		ev.From = fmt.Sprintf("番号 %d", t.No)
		ev.Label = fmt.Sprintf("文字 %d%s%s", t.No, t.Sep, t.Text)
		ev.Pos = t.Pos
		rows = append([]data.Row{{Name: "本文", Text: t.Text}}, rows...)
		if t.Truncated {
			ev.Label += "（途中で切れている）"
		}
		ev.Rows = rows
		return r.built(ev, partN)
	}
	if no, ok := m.ack(); ok {
		ev.Label = fmt.Sprintf("了解 → 番号 %d", no)
		ev.Rows = rows
		return r.built(ev, partN)
	}
	ev.From = "基地局"
	ev.Label = fmt.Sprintf("組み上げた本体（%d 枚）%s", len(parts), m.summary())
	if head := parts[0].Bits; kind(head) == 0b0010 {
		var n []string
		for i := 0; i < 4; i++ {
			n = append(n, fmt.Sprint(bits(head, 88+4*i, 91+4*i)))
		}
		var on []string
		for i := 104; i < scpca1u292.BodyBits; i++ {
			if head[i] == 1 {
				on = append(on, fmt.Sprint(i))
			}
		}
		flags := "無し"
		if len(on) > 0 {
			flags = strings.Join(on, "・")
		}
		rows = append([]data.Row{
			{Name: "4 ビット × 4（88〜103。区域ごとの台数か）", Bits: bitStr(head, 88, 103), Text: strings.Join(n, " · ")},
			{Name: "立っている旗（104〜。区域ごとの状態か）", Text: flags},
		}, rows...)
		ev.Label = fmt.Sprintf("定時の放送（%d 枚） %s 旗 %s", len(parts), strings.Join(n, "·"), flags)
		if len(parts) != 5 {
			ev.Label += "（欠けあり）"
		}
	}
	ev.Rows = rows
	return r.built(ev, partN)
}
