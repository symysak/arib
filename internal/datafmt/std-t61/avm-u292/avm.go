package avmu292

import (
	"fmt"
	"strings"

	"github.com/symysak/arib/internal/bitpack"
	"github.com/symysak/arib/internal/data"
	"github.com/symysak/arib/internal/geo"
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
	BroadcastSec = 3
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

func kind(b []uint8) uint64 { return bits(b, 0, 3) }

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
	parts []data.Frame
	lastB string
	sameB int
}

func newReader() *reader { return &reader{seen: map[string]float64{}, cars: map[uint64]uint64{}} }

func (r *reader) Read(f data.Frame) []data.Event {
	b := f.Bits
	if len(b) != scpca1u292.BodyBits {
		return nil
	}
	raw := bitpack.ToBytes(b)
	h := fmt.Sprintf("%X", raw)
	for k, t := range r.seen {
		if f.TimeSec-t > RepeatSec || f.TimeSec < t {
			delete(r.seen, k)
		}
	}
	switch kind(b) {
	case 0b0010, 0b0100, 0b1110:
		return r.broadcast(f)
	}
	if _, dup := r.seen[h]; dup {
		r.seen[h] = f.TimeSec
		return nil
	}
	r.seen[h] = f.TimeSec
	switch {
	case isReport(b):
		r.cars[bits(b, 29, 47)&63] = bits(b, 29, 47)
		return []data.Event{report(f, raw)}
	case isReply(b):
		return []data.Event{r.reply(f, raw)}
	}
	return []data.Event{{Kind: data.KindMessage, No: f.No, TimeSec: f.TimeSec, Format: Name,
		Label: fmt.Sprintf("種別 %s（読めない形）%s", bitStr(b, 0, 3), f.Route), Raw: raw}}
}

var speedText = map[uint64]string{0: "止まっている", 1: "低速（時速 10〜30 km 前後）", 2: "時速 20〜50 km 前後", 3: "時速 50 km 前後より速い"}

func report(f data.Frame, raw []byte) data.Event {
	b := f.Bits
	id := bits(b, 29, 47)
	ev := data.Event{Kind: data.KindPosition, No: f.No, TimeSec: f.TimeSec, Format: Name, From: fmt.Sprintf("車 %d", id), Raw: raw}
	head := bits(b, 74, 76) * 45
	sp := bits(b, 72, 73)
	flags := bitStr(b, 48, 48) + bitStr(b, 50, 50) + bitStr(b, 61, 62) + bitStr(b, 68, 68)
	ev.Rows = []data.Row{
		{Name: "車の識別", Bits: bitStr(b, 29, 47), Text: fmt.Sprintf("%d", id)},
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
	ev.Rows = append(ev.Rows, data.Row{Name: "124〜130", Bits: bitStr(b, 124, 130), Text: fmt.Sprintf("%d（未詳）", bits(b, 124, 130))})
	if f.Idle {
		ev.Label += "（繰り返しで受けた）"
	}
	return ev
}

func (r *reader) reply(f data.Frame, raw []byte) data.Event {
	b := f.Bits
	low := bits(b, 47, 52)
	to := fmt.Sprintf("車 下位 6 ビット %d", low)
	if id, ok := r.cars[low]; ok {
		to = fmt.Sprintf("車 %d", id)
	}
	return data.Event{Kind: data.KindMessage, No: f.No, TimeSec: f.TimeSec, Format: Name, From: "基地局",
		Label: fmt.Sprintf("基地局の応答 → %s %s", to, f.Route), Raw: raw,
		Rows: []data.Row{
			{Name: "識別", Bits: bitStr(b, 29, 47), Text: fmt.Sprintf("%d", bits(b, 29, 47))},
			{Name: "宛先（47〜52 = 車の識別の下位 6 ビット）", Bits: bitStr(b, 47, 52), Text: to},
			{Name: "108〜115", Bits: bitStr(b, 108, 115), Text: "乱数のような 1 バイト"},
		}}
}

func (r *reader) broadcast(f data.Frame) []data.Event {
	k := kind(f.Bits)
	if k == 0b0010 || (len(r.parts) > 0 && f.TimeSec-r.parts[0].TimeSec > BroadcastSec) {
		r.parts = nil
	}
	if k != 0b0010 && len(r.parts) == 0 {
		return nil
	}
	if n := len(r.parts); n > 0 && string(r.parts[n-1].Bits) == string(f.Bits) {
		return nil
	}
	r.parts = append(r.parts, f)
	if k != 0b1110 {
		return nil
	}
	parts := r.parts
	r.parts = nil
	var rows []data.Row
	var raw []byte
	var key strings.Builder
	for i, p := range parts {
		pb := bitpack.ToBytes(p.Bits)
		raw = append(raw, pb...)
		fmt.Fprintf(&key, "%X/", pb)
		rows = append(rows, data.Row{Name: fmt.Sprintf("%d 枚目（種別 %s）", i+1, bitStr(p.Bits, 0, 3)), Text: fmt.Sprintf("%X", pb)})
	}
	if key.String() == r.lastB {
		r.sameB++
		return nil
	}
	r.lastB, r.sameB = key.String(), 0
	label := fmt.Sprintf("定時の放送（%d 枚）", len(parts))
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
		label += " " + strings.Join(n, "·") + " 旗 " + flags
	}
	if len(parts) != 5 {
		label += "（欠けあり）"
	}
	return []data.Event{{Kind: data.KindMessage, No: parts[0].No, TimeSec: parts[0].TimeSec, Format: Name, From: "基地局",
		Label: label, Rows: rows, Raw: raw}}
}
