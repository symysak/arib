package msgu264

import (
	"fmt"
	"slices"
	"strings"

	"github.com/symysak/arib/internal/bitpack"
	"github.com/symysak/arib/internal/data"
	scpca1u264 "github.com/symysak/arib/internal/std-t61/scpc-a1u264"
)

const Name = "msg-u264"

func init() {
	data.Register(data.Format{
		Name:  Name,
		Desc:  "付録 1 の U（u264）の 16 ビットの語（欄の意味は未詳）",
		Ch:    scpca1u264.Channel,
		New:   func(data.Options) (data.Reader, error) { return newReader(), nil },
		Probe: probe,
	})
}

const (
	Bits = scpca1u264.Bits
	ConfirmSec = 30
	IdleRepeatSec = 600
	RepeatSec = 60
)

var countWords = []uint16{0x6A93, 0x01B3, 0x37D3, 0x5CF3}

func words(b []uint8) []uint16 {
	out := make([]uint16, 0, 17)
	for i := 0; i < scpca1u264.BodyBits; i += 16 {
		var v uint16
		for _, x := range b[i:min(i+16, scpca1u264.BodyBits)] {
			v = v<<1 | uint16(x&1)
		}
		out = append(out, v)
	}
	return out
}

func short(w []uint16) bool {
	return !slices.ContainsFunc(w[4:11], func(v uint16) bool { return v != 0 })
}

func counting(w []uint16) bool {
	t := byte(w[0])
	return (t == 0xA0 || t == 0x30) && short(w) && slices.Contains(countWords, w[1])
}

func probe(frames []data.Frame) (ok, n int) {
	for _, f := range frames {
		if len(f.Bits) != Bits {
			continue
		}
		n++
		if short(words(f.Bits)) || scpca1u264.IsLong(f.Bits) {
			ok++
		}
	}
	return ok, n
}

type reader struct {
	pending map[string]*pend
	sent    map[string]float64
	sentEv  map[string]data.Event
}

type pend struct {
	t float64
	counted bool
}

func newReader() *reader {
	return &reader{pending: map[string]*pend{}, sent: map[string]float64{}, sentEv: map[string]data.Event{}}
}

func expire(m map[string]float64, now, sec float64) {
	for k, t := range m {
		if now-t > sec || now < t {
			delete(m, k)
		}
	}
}

func (r *reader) Read(f data.Frame) []data.Event {
	b := f.Bits
	if len(b) != Bits {
		return nil
	}
	raw := bitpack.ToBytes(b)
	h := fmt.Sprintf("%X", raw)
	for k, p := range r.pending {
		if f.TimeSec-p.t > ConfirmSec || f.TimeSec < p.t {
			delete(r.pending, k)
		}
	}
	expire(r.sent, f.TimeSec, IdleRepeatSec)
	for k := range r.sentEv {
		if _, ok := r.sent[k]; !ok {
			delete(r.sentEv, k)
		}
	}
	w := words(b)
	long := scpca1u264.IsLong(b)
	key, repeat := h, float64(RepeatSec)
	if counting(w) {
		key, repeat = "count "+hexWords(append([]uint16{w[0]}, w[2:]...)), IdleRepeatSec
	}
	again := func(n int) []data.Event {
		if t, dup := r.sent[key]; dup && f.TimeSec-t < repeat {
			if ev, ok := r.sentEv[key]; ok {
				return []data.Event{data.AgainOf(ev, f, n)}
			}
		}
		return nil
	}
	first := 0
	switch {
	case long:
	case short(w):
		p := r.pending[h]
		if p == nil {
			evs := again(1)
			r.pending[h] = &pend{t: f.TimeSec, counted: evs != nil}
			return evs
		}
		if !p.counted {
			first = 1
		}
		p.counted = true
	default:
		return nil
	}
	if t, dup := r.sent[key]; dup && f.TimeSec-t < repeat {
		return again(1 + first)
	}
	r.sent[key] = f.TimeSec
	ev := event(f, w, long, raw)
	ev.Count = 1 + first
	r.sentEv[key] = ev
	return []data.Event{ev}
}

func hexWords(w []uint16) string {
	p := make([]string, len(w))
	for i, v := range w {
		p[i] = fmt.Sprintf("%04X", v)
	}
	return strings.Join(p, " ")
}

func event(f data.Frame, w []uint16, long bool, raw []byte) data.Event {
	b := f.Bits
	size := "短い形"
	if long {
		size = "長い形"
	}
	label := fmt.Sprintf("形 %02X %s %s", byte(w[0]), size, hexWords(w[:4]))
	if counting(w) {
		label = fmt.Sprintf("形 %02X %s 語0=%04X（語 1 は回る 4 値）", byte(w[0]), size, w[0])
	}
	if f.Route != "" {
		label += " " + f.Route
	}
	return data.Event{Kind: data.KindMessage, No: f.No, TimeSec: f.TimeSec, Format: Name, Label: label, Raw: raw,
		Rows: []data.Row{
			{Name: "語 0（下位のバイトが形）", Bits: bitStr(b, 0, 15), Text: fmt.Sprintf("%04X", w[0])},
			{Name: "語 1〜3", Bits: bitStr(b, 16, 63), Text: hexWords(w[1:4])},
			{Name: "語 4〜16", Text: hexWords(w[4:16]) + fmt.Sprintf(" %02X", w[16])},
			{Name: "検査 28", Bits: bitStr(b, scpca1u264.BodyBits, Bits-1), Text: "作りは未詳"},
		}}
}

func bitStr(b []uint8, from, to int) string {
	var sb strings.Builder
	for _, x := range b[from : to+1] {
		sb.WriteByte('0' + x&1)
	}
	return sb.String()
}
