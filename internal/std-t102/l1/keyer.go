package l1

import (
	"github.com/symysak/arib/internal/vocoder/ambe"
)

const (
	keyDecide = 8
	keyHintDecide = 4
	keyHold = 25
	keyLoseAfter = 25
)

type Keyer struct {
	init     int
	fixed    bool
	hints    []int
	score    [WhiteningInits]int
	held     [][]float32
	bad      int
	lastWhy  string
	decided  bool
	richMask [WhiteningInits]uint8
}

func NewKeyer(fixed int, hints ...int) *Keyer {
	k := &Keyer{init: -1}
	for i := 0; i < WhiteningInits; i++ {
		pn := Whitening(i, 8)
		var m uint8
		for j, b := range pn {
			m |= b << (7 - j)
		}
		k.richMask[i] = m
	}
	if fixed >= 0 {
		k.init, k.fixed, k.decided, k.lastWhy = fixed&0x1FF, true, true, "指定"
	}
	seen := map[int]bool{}
	for _, h := range hints {
		if h >= 0 && h < WhiteningInits && !seen[h] {
			k.hints = append(k.hints, h)
			seen[h] = true
		}
	}
	return k
}

func (k *Keyer) Init() (init int, ok bool) { return k.init, k.init >= 0 }

func (k *Keyer) Fixed() bool { return k.fixed }

func (k *Keyer) Why() string { return k.lastWhy }

type Keyed struct {
	Init   int
	Body   []float32
	Parsed Parsed
}

func (k *Keyer) Push(body []float32) []Keyed {
	if k.init >= 0 {
		kd := k.read(body, k.init)
		if kd.Parsed.RICH.Valid() {
			k.bad = 0
		} else if k.bad++; !k.fixed && k.bad >= keyLoseAfter {
			k.init, k.decided, k.bad, k.lastWhy = -1, false, 0, ""
			k.score = [WhiteningInits]int{}
			k.held = nil
		}
		return []Keyed{kd}
	}
	b := append([]float32(nil), body...)
	k.held = append(k.held, b)
	if len(k.held) > keyHold {
		k.held = k.held[len(k.held)-keyHold:]
	}
	k.learn(b)
	if !k.decide() {
		return nil
	}
	out := make([]Keyed, 0, len(k.held))
	for _, h := range k.held {
		out = append(out, k.read(h, k.init))
	}
	k.held = nil
	return out
}

func (k *Keyer) read(body []float32, init int) Keyed {
	d := Dewhiten(body, init)
	return Keyed{Init: init, Body: d, Parsed: Parse(d)}
}

func richRaw(body []float32) uint8 {
	var r uint8
	for i := 0; i < 8; i++ {
		r <<= 1
		if body[i] < 0 {
			r |= 1
		}
	}
	return r
}

func (k *Keyer) learn(body []float32) {
	raw := richRaw(body)
	for i := 0; i < WhiteningInits; i++ {
		r := raw ^ k.richMask[i]
		rich := RICH{F: r >> 6, M: r >> 4 & 3, O: r >> 2 & 3, D: r >> 1 & 1, P: r & 1, Raw: r}
		if !rich.Valid() {
			continue
		}
		k.score[i] += Score(Parse(Dewhiten(body, i)))
	}
}

func Score(p Parsed) int {
	s := 0
	if p.SACCH != nil && p.SACCH.CRCOK {
		s++
	}
	for _, h := range p.Half {
		switch {
		case h.FACCH1 != nil && h.FACCH1.CRCOK:
			s += 4
		case h.Kind == HalfTCH && len(h.TCH) == TCHBits:
			for j := 0; j < 2; j++ {
				var a [ambe.Bits3600]uint8
				copy(a[:], h.TCH[72*j:72*j+72])
				if ambe.Valid3600(a) {
					s++
				}
			}
		}
	}
	if p.Data != nil && p.Data.CRCOK {
		s += 4
	}
	return s
}

func (k *Keyer) decide() bool {
	for _, h := range k.hints {
		if k.score[h] >= keyHintDecide {
			k.init, k.decided, k.lastWhy = h, true, "候補（台帳・ほかの局・既定）"
			return true
		}
	}
	best, second := -1, 0
	for i, s := range k.score {
		if best < 0 || s > k.score[best] {
			if best >= 0 {
				second = max(second, k.score[best])
			}
			best = i
		} else if s > second {
			second = s
		}
	}
	if best >= 0 && k.score[best] >= keyDecide && 4*second <= k.score[best] {
		k.init, k.decided, k.lastWhy = best, true, "CRC・AMBE+2 の検査から"
		return true
	}
	return false
}
