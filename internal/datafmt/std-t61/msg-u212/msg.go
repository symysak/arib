package msgu212

import (
	"fmt"
	"strings"

	"github.com/symysak/arib/internal/bitpack"
	"github.com/symysak/arib/internal/data"
	"github.com/symysak/arib/internal/geo"
	"github.com/symysak/arib/internal/sjis"
	scpca1u264 "github.com/symysak/arib/internal/std-t61/scpc-a1u264"
)

const Name = "msg-u212"

const (
	RegionRepeatSec = 300
	RepeatSec = 60
	MaxParts = 31
	PartSec = 60
	recentMessages = 16
	streamBits = 184
)

func init() {
	data.Register(data.Format{
		Name:  Name,
		Desc:  "付録 1 の U（データ 212 ビット + CRC-16）の地域の整数度・了解・分割文",
		Ch:    scpca1u264.DataChannel,
		New:   func(data.Options) (data.Reader, error) { return newReader(), nil },
		Probe: probe,
	})
}

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

func probe(frames []data.Frame) (ok, n int) {
	for _, f := range frames {
		if len(f.Bits) != scpca1u264.DataBits {
			continue
		}
		n++
		switch bits(f.Bits, 0, 3) {
		case 1, 2, 5, 6, 9, 10:
			ok++
		}
	}
	return ok, n
}

type part struct {
	n     int
	id    uint64
	parts map[int][]uint8
	t     float64
}

type reader struct {
	regionKey string
	regionT   float64
	seen      map[string]float64
	asm       map[uint64]*part
	recent    []uint64
	held      map[uint64]string
}

func newReader() *reader {
	return &reader{regionT: -1e9, seen: map[string]float64{}, asm: map[uint64]*part{}, held: map[uint64]string{}}
}

func (r *reader) Read(f data.Frame) []data.Event {
	b := f.Bits
	if len(b) != scpca1u264.DataBits {
		return nil
	}
	switch typ := bits(b, 0, 3); typ {
	case 9, 10:
		if bits(b, 4, 11) == 0 {
			return r.region(f)
		}
	case 5, 6:
		return r.split(f)
	case 1, 2:
		if evs, ok := r.ack(f); ok {
			return evs
		}
	}
	return r.other(f)
}

func (r *reader) region(f data.Frame) []data.Event {
	b := f.Bits
	none := bits(b, 150, 211) == 0
	lat, lon := 23+bits(b, 207, 211), 123+bits(b, 199, 203)
	key := fmt.Sprintf("%d/%d/%v", lat, lon, none)
	if key == r.regionKey && f.TimeSec-r.regionT < RegionRepeatSec && f.TimeSec >= r.regionT {
		return nil
	}
	r.regionKey, r.regionT = key, f.TimeSec
	ev := data.Event{Kind: data.KindMessage, No: f.No, TimeSec: f.TimeSec, Format: Name, From: "基地局", Raw: bitpack.ToBytes(b)}
	ev.Rows = []data.Row{
		{Name: "種別", Bits: bitStr(b, 0, 3), Text: fmt.Sprint(bits(b, 0, 3))},
		{Name: "計数（14〜15）", Bits: bitStr(b, 14, 15), Text: fmt.Sprint(bits(b, 14, 15))},
	}
	if none {
		ev.Label = fmt.Sprintf("地域の整数度なし（150〜211 が全部 0）%s", f.Route)
		return []data.Event{ev}
	}
	ev.Label = fmt.Sprintf("地域の整数度 北緯 %d°・東経 %d° %s", lat, lon, f.Route)
	ev.Rows = append(ev.Rows,
		data.Row{Name: "緯度（23 + 207〜211）", Bits: bitStr(b, 207, 211), Text: fmt.Sprintf("%d°", lat)},
		data.Row{Name: "経度（123 + 199〜203）", Bits: bitStr(b, 199, 203), Text: fmt.Sprintf("%d°", lon)})
	return []data.Event{ev}
}

func (r *reader) ack(f data.Frame) (evs []data.Event, ok bool) {
	b := f.Bits
	for k := 0; k < 4; k++ {
		o := 4 + 52*k
		if bits(b, o, o+3) != 2 || bits(b, o+44, o+47) != 0 {
			continue
		}
		no := bits(b, o+4, o+17)
		label := fmt.Sprintf("了解 → 番号 %04d", no)
		if no >= 10000 {
			label = "了解 → 番号 ----"
		}
		evs = append(evs, data.Event{Kind: data.KindMessage, No: f.No, TimeSec: f.TimeSec, Format: Name, From: "基地局", Label: label + " " + f.Route,
			Raw: bitpack.ToBytes(b), Rows: []data.Row{{Name: fmt.Sprintf("小区画 %d の番号", k), Bits: bitStr(b, o+4, o+17), Text: fmt.Sprint(no)}}})
	}
	if len(evs) == 0 {
		return nil, false
	}
	if r.dup(b, f.TimeSec) {
		return nil, true
	}
	return evs, true
}

func (r *reader) other(f data.Frame) []data.Event {
	b := f.Bits
	if r.dup(b, f.TimeSec) {
		return nil
	}
	return []data.Event{{Kind: data.KindMessage, No: f.No, TimeSec: f.TimeSec, Format: Name,
		Label: fmt.Sprintf("種別 %d（読まない形）%s", bits(b, 0, 3), f.Route), Raw: bitpack.ToBytes(b)}}
}

func (r *reader) dup(b []uint8, t float64) bool {
	for k, s := range r.seen {
		if t-s > RepeatSec || t < s {
			delete(r.seen, k)
		}
	}
	h := fmt.Sprintf("%X", bitpack.ToBytes(b))
	if _, ok := r.seen[h]; ok {
		r.seen[h] = t
		return true
	}
	r.seen[h] = t
	return false
}

func (r *reader) split(f data.Frame) []data.Event {
	b := f.Bits
	for k, a := range r.asm {
		if f.TimeSec-a.t > PartSec || f.TimeSec < a.t {
			delete(r.asm, k)
		}
	}
	seq := int(bits(b, 20, 25))
	stream := b[28:]
	no := bits(b, 8, 17)
	if seq == 0 {
		no = bits(stream, 0, 9)
	}
	for _, x := range r.recent {
		if x == no {
			return nil
		}
	}
	a := r.asm[no]
	if a == nil {
		a = &part{parts: map[int][]uint8{}, t: f.TimeSec}
		r.asm[no] = a
	}
	a.parts[seq] = stream
	if seq == 0 {
		L := int(bits(stream, 21, 30))
		a.n, a.id = (L+29)/23, bits(b, 4, 17)
		if a.n > MaxParts {
			delete(r.asm, no)
			return nil
		}
		a.n = max(a.n, 1)
	}
	if a.n == 0 {
		return nil
	}
	for i := 0; i < a.n; i++ {
		if a.parts[i] == nil {
			return nil
		}
	}
	delete(r.asm, no)
	r.recent = append(r.recent, no)
	if len(r.recent) > recentMessages {
		r.recent = r.recent[1:]
	}
	full := make([]uint8, 0, a.n*streamBits)
	for i := 0; i < a.n; i++ {
		full = append(full, a.parts[i]...)
	}
	return r.message(f, a.id, no, full)
}

func (r *reader) message(f data.Frame, id, no uint64, s []uint8) []data.Event {
	L := int(bits(s, 21, 30))
	aux, sep := bits(s, 17, 18), bits(s, 34, 35)
	end := min(48+8*(L+1), len(s)/8*8)
	raw := bitpack.ToBytes(s[48:end])
	if (L == 0 && len(raw) > 0 && raw[0] == 0x20) || (L == 1 && len(raw) > 1 && raw[0] == 0x81 && raw[1] == 0x40) {
		return nil
	}
	text, _ := sjis.Decode(raw)
	text = clean(text)
	ev := data.Event{Kind: data.KindMessage, No: f.No, TimeSec: f.TimeSec, Format: Name, From: fmt.Sprintf("番号 %d", id),
		Raw: bitpack.ToBytes(s), Rows: []data.Row{{Name: "文の番号", Text: fmt.Sprint(no)}}}
	delim := ">"
	switch {
	case sep == 2:
		delim = ":"
	case sep == 1 && aux == 2:
		text = r.held[id] + text
		delete(r.held, id)
	case sep == 1:
		r.held[id] = text
		return nil
	default:
		if t, pos, ok := position(text); ok {
			text, ev.Pos = t, pos
		}
	}
	ev.Label = fmt.Sprintf("文字 %d%s%s", id, delim, text)
	ev.Rows = append([]data.Row{{Name: "本文", Text: text}}, ev.Rows...)
	return []data.Event{ev}
}

func clean(s string) string {
	var sb strings.Builder
	quoted := false
	for _, c := range s {
		switch {
		case c == '"':
			quoted = !quoted
		case quoted, c == '!', c == '\'':
		case c == ',', c < 0x20:
			sb.WriteRune(' ')
		default:
			sb.WriteRune(c)
		}
	}
	return strings.TrimRight(sb.String(), " ")
}

func position(text string) (string, *data.Position, bool) {
	rs := []rune(text)
	a := -1
	for i, c := range rs {
		if c != '^' {
			continue
		}
		if a < 0 {
			a = i
			continue
		}
		if i-a-1 != 20 {
			a = i
			continue
		}
		p := rs[a+1 : i]
		for _, d := range p {
			if d < '0' || d > '9' {
				return text, nil, false
			}
		}
		num := func(from, to int) float64 {
			v := 0.0
			for _, d := range p[from : to+1] {
				v = v*10 + float64(d-'0')
			}
			return v
		}
		lon := num(1, 3) + num(4, 5)/60 + (num(6, 7)+num(8, 10)/1000)/3600
		lat := num(11, 12) + num(13, 14)/60 + (num(15, 16)+num(17, 19)/1000)/3600
		wlat, wlon := geo.TokyoToWGS84(lat, lon)
		return strings.TrimSpace(string(rs[:a]) + string(rs[i+1:])), &data.Position{Lat: wlat, Lon: wlon, Heading: -1}, true
	}
	return text, nil, false
}
