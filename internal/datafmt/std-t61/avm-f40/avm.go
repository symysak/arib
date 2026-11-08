package avmf40

import (
	"fmt"
	"math"

	"github.com/symysak/arib/internal/data"
	scpca1f40 "github.com/symysak/arib/internal/std-t61/scpc-a1f40"
)

const Name = "avm-f40"

func init() {
	data.Register(data.Format{
		Name:  Name,
		Desc:  "付録 1 の V の FN1 の制御語（f40-ctl）の移動局の位置",
		Ch:    scpca1f40.ControlChannel,
		New:   func(data.Options) (data.Reader, error) { return newReader(), nil },
		Probe: probe,
	})
}

const (
	Mobile = 0x03
	pairSec = 1.5
	RepeatSec = 30
	span = 1 << 14
)

const originLat, originLon = 31, 128

const unit = 1.0 / 36000

const group = Name

func probe(frames []data.Frame) (ok, n int) {
	for _, f := range frames {
		if len(f.Bits) != 32 {
			continue
		}
		n++
		if wellFormed(octets(f.Bits), f.Slot) {
			ok++
		}
	}
	return ok, n
}

func wellFormed(b [4]byte, slot int) bool {
	if b == [4]byte{0xFF, 0xFC, 0, 0} {
		return true
	}
	if b[0] != Mobile {
		return false
	}
	if b[2] == 0 && b[3] == 0 {
		return true
	}
	switch slot {
	case 0:
		return b[2]>>6 == 0b10
	case 1:
		return b[2]>>6 == 0b01
	case 2:
		return b[3] == 0xA8
	case 3:
		return b[2] == 0 && b[3] == 0
	}
	return false
}

func octets(bits []uint8) (o [4]byte) {
	for i, x := range bits[:32] {
		o[i/8] |= (x & 1) << (7 - i%8)
	}
	return
}

type part struct {
	v  uint32
	t  float64
	b1 byte
}

type reader struct {
	lat, lon *part
	flags    *part
	id       map[byte]byte
	lastKey  string
	lastT    float64
	seenMsg  map[[4]byte]float64
	talkT    map[byte]float64
}

func newReader() *reader {
	return &reader{id: map[byte]byte{}, seenMsg: map[[4]byte]float64{}, talkT: map[byte]float64{}}
}

func (r *reader) Read(f data.Frame) []data.Event {
	if len(f.Bits) != 32 {
		return nil
	}
	b := octets(f.Bits)
	if b[0] != Mobile {
		if b[0] == 0xFF && b[1] == 0xFC && b[2] == 0 && b[3] == 0 {
			return nil
		}
		if t, dup := r.seenMsg[b]; dup && f.TimeSec-t < 300 && f.TimeSec >= t {
			return nil
		}
		r.seenMsg[b] = f.TimeSec
		return []data.Event{{Kind: data.KindMessage, No: f.No, TimeSec: f.TimeSec, Format: Name,
			Label: fmt.Sprintf("制御語 %X（読めない形）%s", b[:], f.Route), Raw: b[:]}}
	}
	v14 := uint32(b[2]&0x3F)<<8 | uint32(b[3])
	switch {
	case f.Slot == 0 && b[2]>>6 == 0b10:
		r.lon = &part{v14, f.TimeSec, b[1]}
	case f.Slot == 1 && b[2]>>6 == 0b01:
		r.lat = &part{v14, f.TimeSec, b[1]}
	case f.Slot == 2:
		r.flags = &part{uint32(b[2]), f.TimeSec, b[1]}
		return nil
	case b[2] == 0 && b[3] == 0:
		r.id[b[1]&^3] = b[1]
		return r.talker(f, b)
	default:
		return nil
	}
	if r.lat == nil || r.lon == nil || math.Abs(r.lat.t-r.lon.t) > pairSec {
		return nil
	}
	key := fmt.Sprintf("%02X/%d/%d", r.lat.b1, r.lat.v, r.lon.v)
	if key == r.lastKey && f.TimeSec-r.lastT < RepeatSec && f.TimeSec >= r.lastT {
		return nil
	}
	r.lastKey, r.lastT = key, f.TimeSec
	return []data.Event{r.position(f, b)}
}

const TalkSec = 30

func (r *reader) talker(f data.Frame, b [4]byte) []data.Event {
	t, seen := r.talkT[b[1]]
	r.talkT[b[1]] = f.TimeSec
	if seen && f.TimeSec-t < TalkSec && f.TimeSec >= t {
		return nil
	}
	who := fmt.Sprintf("無線機 %02X", b[1])
	return []data.Event{{Kind: data.KindMessage, No: f.No, TimeSec: f.TimeSec, Format: Name, From: who,
		Label: fmt.Sprintf("%s が通話中 %s", who, f.Route), Raw: b[:],
		Rows: []data.Row{{Name: "識別（2 オクテット目）", Bits: fmt.Sprintf("%08b", b[1]), Text: fmt.Sprintf("%02X", b[1])}}}}
}

func (r *reader) position(f data.Frame, b [4]byte) data.Event {
	from := fmt.Sprintf("無線機 %02X", r.lat.b1&^3)
	if id, ok := r.id[r.lat.b1&^3]; ok {
		from = fmt.Sprintf("無線機 %02X", id)
	}
	lon, lat := r.lon.word(true), r.lat.word(false)
	ev := data.Event{Kind: data.KindPosition, No: f.No, TimeSec: f.TimeSec, Format: Name, From: from,
		Raw: append(lon[:], lat[:]...),
		Wrap: &data.Wrapped{Lat: int64(r.lat.v), Lon: int64(r.lon.v), Span: span, Unit: unit,
			OriginLat: originLat, OriginLon: originLon, Datum: data.DatumTokyo, Group: group}}
	ev.Label = fmt.Sprintf("%s 経度 %d・緯度 %d（下位 14 ビット。通話中）%s", from, r.lon.v, r.lat.v, f.Route)
	ev.Rows = []data.Row{
		{Name: "送り元の印（2 オクテット目）", Bits: fmt.Sprintf("%08b", r.lat.b1), Text: fmt.Sprintf("%02X", r.lat.b1)},
		{Name: "緯度（下位 14 ビット）", Bits: fmt.Sprintf("%014b", r.lat.v), Text: fmt.Sprintf("%d（(緯度 − %d°) × 36000 の下位。日本測地系）", r.lat.v, originLat)},
		{Name: "経度（下位 14 ビット）", Bits: fmt.Sprintf("%014b", r.lon.v), Text: fmt.Sprintf("%d（(経度 − %d°) × 36000 の下位。日本測地系）", r.lon.v, originLon)},
		{Name: "上位の桁", Text: "画面が基準点（現在地・地図の中心）に一番近い写しを選ぶ（27′ おき）"},
		{Name: "原点", Text: fmt.Sprintf("%d°N %d°E。実波の 1 つの網で確かめた値で、ほかの網では違うことがある（違うと点がまとめて数 km〜20 km ずれる）", originLat, originLon)},
	}
	if r.flags != nil && math.Abs(r.flags.t-f.TimeSec) < 2*pairSec {
		ev.Rows = append(ev.Rows, data.Row{Name: "旗（CFN 2）", Bits: fmt.Sprintf("%08b", r.flags.v), Text: "意味は未詳"})
	}
	return ev
}

func (p *part) word(lon bool) [4]byte {
	mark := byte(0x40)
	if lon {
		mark = 0x80
	}
	return [4]byte{Mobile, p.b1, mark | byte(p.v>>8)&0x3F, byte(p.v)}
}
