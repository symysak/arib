package ambe2c

import (
	"fmt"

	"github.com/symysak/arib/internal/vocoder/ambe"
	"github.com/symysak/arib/internal/voice"
)


const PairBits = 2 * ambe.Bits4000FEC

const PairMaxDist = 3

func PairFrames(bits []uint8, oddFirst bool) (f [2][]uint8) {
	for k := 0; k < 2; k++ {
		first := k
		if oddFirst {
			first = 1 - k
		}
		f[k] = make([]uint8, ambe.Bits4000FEC)
		for i := range f[k] {
			f[k][i] = bits[2*i+first]
		}
	}
	return f
}

func PairOK(bits []uint8, oddFirst bool) bool {
	f := PairFrames(bits, oddFirst)
	for _, ch := range f {
		if _, d := ambe.Decode4000(ch); d > PairMaxDist {
			return false
		}
	}
	return true
}

type PairUnit struct {
	ch     [2][]uint8
	params [2][]uint8
	good   [2]bool
}

func (u PairUnit) Params(k int) []uint8 { return u.params[k] }

type Pair4000 struct {
	OddFirst bool
	stream   *Stream
}

func (p *Pair4000) Split(bits []uint8) (PairUnit, voice.Check, []byte) {
	var u PairUnit
	chk := voice.Check{Valid: true, OK: true}
	f := PairFrames(bits, p.OddFirst)
	for k := 0; k < 2; k++ {
		params, d := ambe.Decode4000(f[k])
		u.ch[k], u.params[k], u.good[k] = f[k], params, d <= PairMaxDist
		chk.OK = chk.OK && u.good[k]
		chk.Corrected += d
	}
	raw := make([]byte, 0, 2*ambe.Params4000/8)
	for _, params := range u.params {
		for i := 0; i < len(params); i += 8 {
			var b byte
			for j := 0; j < 8; j++ {
				b = b<<1 | params[i+j]
			}
			raw = append(raw, b)
		}
	}
	return u, chk, raw
}

func (p *Pair4000) Transport(f voice.Frame) (voice.Unit, bool, error) {
	if len(f.Bits) != PairBits {
		return voice.Unit{}, false, fmt.Errorf("TCH が %d ビットではなく %d", PairBits, len(f.Bits))
	}
	u, chk, raw := p.Split(f.Bits)
	return voice.Unit{Data: u, Check: chk, Raw: raw}, true, nil
}

func (p *Pair4000) Erase() (voice.Unit, bool) {
	return voice.Unit{Data: PairUnit{}}, true
}

func (p *Pair4000) Decode(u voice.Unit) ([]int16, error) {
	var units []PairUnit
	switch d := u.Data.(type) {
	case PairUnit:
		units = []PairUnit{d}
	case []PairUnit:
		units = d
	default:
		return nil, fmt.Errorf("AMBE+ 4000/2400 の単位ではない: %T", u.Data)
	}
	out := make([]int16, 0, 2*ambe.SamplesPerFrame*len(units))
	for _, d := range units {
		for k := 0; k < 2; k++ {
			switch {
			case d.good[k] && allZero(d.params[k]), !d.good[k] && p.stream == nil:
				out = append(out, make([]int16, ambe.SamplesPerFrame)...)
				continue
			case !d.good[k]:
				pcm, err := p.stream.Lost()
				if err != nil {
					return nil, err
				}
				out = append(out, pcm...)
				continue
			}
			if p.stream == nil {
				s, err := NewStream(ambe.Rate4000FEC)
				if err != nil {
					return nil, err
				}
				p.stream = s
			}
			pcm, err := p.stream.Push(d.ch[k])
			if err != nil {
				return nil, err
			}
			out = append(out, pcm...)
		}
	}
	return out, nil
}

func (p *Pair4000) Started() bool { return p.stream != nil }

func (p *Pair4000) Close() error {
	if p.stream == nil {
		return nil
	}
	err := p.stream.Close()
	p.stream = nil
	return err
}

func allZero(p []uint8) bool {
	for _, b := range p {
		if b != 0 {
			return false
		}
	}
	return true
}
