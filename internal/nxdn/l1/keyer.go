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

type KeyerConfig struct {
	Dialect Dialect
	Fixed    int
	FixedWhy string
	Hints []int
	Candidates []int
}

type Keyer struct {
	dialect  Dialect
	cand     [WhiteningInits]bool
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

func NewKeyer(cfg KeyerConfig) *Keyer {
	k := &Keyer{init: -1, dialect: cfg.Dialect}
	for i := 0; i < WhiteningInits; i++ {
		pn := Whitening(i, 8)
		var m uint8
		for j, b := range pn {
			m |= b << (7 - j)
		}
		k.richMask[i] = m
		k.cand[i] = cfg.Candidates == nil
	}
	for _, c := range cfg.Candidates {
		if c >= 0 && c < WhiteningInits {
			k.cand[c] = true
		}
	}
	if cfg.Fixed >= 0 {
		why := cfg.FixedWhy
		if why == "" {
			why = "指定"
		}
		k.init, k.fixed, k.decided, k.lastWhy = cfg.Fixed&0x1FF, true, true, why
	}
	seen := map[int]bool{}
	for _, h := range cfg.Hints {
		if h >= 0 && h < WhiteningInits && k.cand[h] && !seen[h] {
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
		if kd.Parsed.Layout.Valid {
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
	return Keyed{Init: init, Body: d, Parsed: Parse(d, k.dialect)}
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
		if !k.cand[i] || !k.dialect(raw^k.richMask[i]).Valid {
			continue
		}
		k.score[i] += Score(Parse(Dewhiten(body, i), k.dialect))
	}
}

func Score(p Parsed) int {
	s := 0
	if p.SACCH != nil && p.SACCH.CRCOK {
		s++
	}
	for _, h := range p.Half {
		switch {
		case h.Coded != nil && h.Coded.CRCOK:
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
