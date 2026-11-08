package scpca1f40

import (
	"math/bits"

	"github.com/symysak/arib/internal/std-t61/fec"
	"github.com/symysak/arib/internal/std-t61/l1"
)

const (
	FrameBits = 40
	Sections = 3
	MaxErrors = 3

	codeK  = 7
	codeG1 = 0o155
	codeG2 = 0o117
	tail   = codeK - 1
	sec0   = 92
	secN   = 92
	padLen = l1.A1TCH1Bits - sec0 - 2*secN
)

var Pad = [padLen]uint8{1, 0, 0, 1, 1, 0, 0, 1, 1, 0, 0, 1, 1, 0, 0, 1}

var Silence = [FrameBits]uint8{
	1, 0, 1, 0, 1, 0, 1, 1, 1, 1, 0, 1, 1, 1, 0, 1, 1, 0, 0, 0,
	1, 0, 0, 0, 1, 0, 1, 1, 0, 0, 0, 1, 0, 0, 0, 1, 0, 1, 0, 0,
}

func VoiceSections(fn int) []int {
	switch fn {
	case 1:
		return []int{1, 2}
	case 2, 3:
		return []int{0, 1, 2}
	}
	return nil
}

type Section struct {
	Info []uint8
	Errors int
	Terminated bool
}

func (s Section) Voice() bool { return s.Errors <= MaxErrors && s.Terminated }

func (s Section) Frame() [FrameBits]uint8 {
	var f [FrameBits]uint8
	copy(f[:], s.Info)
	return f
}

func Split(tch1 []float32) ([Sections]Section, bool) {
	var out [Sections]Section
	if len(tch1) != l1.A1TCH1Bits {
		return out, false
	}
	info, errs := decode(tch1[:sec0], false)
	out[0] = Section{Info: info[:len(info)-tail], Errors: errs, Terminated: allZero(info[len(info)-tail:])}
	for i := 1; i < Sections; i++ {
		seg := tch1[sec0+(i-1)*secN : sec0+i*secN]
		info, errs := decode(seg, true)
		out[i] = Section{Info: info[:len(info)-tail], Errors: errs, Terminated: true}
	}
	return out, true
}

func decode(soft []float32, terminated bool) ([]uint8, int) {
	const S = 1 << (codeK - 1)
	n := len(soft) / 2
	var o1, o2 [S][2]float64
	for st := 0; st < S; st++ {
		for b := 0; b < 2; b++ {
			reg := st<<1 | b
			o1[st][b] = float64(1 - 2*(bits.OnesCount(uint(reg&codeG1))&1))
			o2[st][b] = float64(1 - 2*(bits.OnesCount(uint(reg&codeG2))&1))
		}
	}
	const ninf = -1e30
	var pm, npm [S]float64
	for s := range pm {
		pm[s] = ninf
	}
	pm[0] = 0
	bp := make([][S]uint8, n)
	prev := make([][S]uint8, n)
	for i := 0; i < n; i++ {
		r1, r2 := float64(soft[2*i]), float64(soft[2*i+1])
		for s := range npm {
			npm[s] = ninf
		}
		for s := 0; s < S; s++ {
			if pm[s] == ninf {
				continue
			}
			for b := 0; b < 2; b++ {
				m := pm[s] + o1[s][b]*r1 + o2[s][b]*r2
				ns := (s<<1 | b) & (S - 1)
				if m > npm[ns] {
					npm[ns], bp[i][ns], prev[i][ns] = m, uint8(b), uint8(s)
				}
			}
		}
		pm = npm
	}
	best := 0
	if !terminated {
		for s := 1; s < S; s++ {
			if pm[s] > pm[best] {
				best = s
			}
		}
	}
	info := make([]uint8, n)
	for i, s := n-1, best; i >= 0; i-- {
		info[i] = bp[i][s]
		s = int(prev[i][s])
	}
	re := fec.ConvEncodeG(info, codeK, codeG1, codeG2)
	errs := 0
	for i, v := range soft {
		if v == 0 {
			continue
		}
		hard := uint8(0)
		if v < 0 {
			hard = 1
		}
		if hard != re[i] {
			errs++
		}
	}
	return info, errs
}

func allZero(b []uint8) bool {
	for _, x := range b {
		if x != 0 {
			return false
		}
	}
	return true
}

func Mask(wc int) []uint8 { return fec.PN95(wc, Sections*FrameBits) }

func Descramble(f [FrameBits]uint8, sec, wc int) [FrameBits]uint8 {
	m := Mask(wc)[sec*FrameBits:]
	for i := range f {
		f[i] ^= m[i]
	}
	return f
}

var keyTable = func() (t [Sections]map[uint64]int) {
	for s := range t {
		t[s] = make(map[uint64]int, l1.A1Patterns)
	}
	for wc := 0; wc < l1.A1Patterns; wc++ {
		m := Mask(wc)
		for s := range t {
			t[s][pack(m[s*FrameBits:(s+1)*FrameBits])] = wc
		}
	}
	return
}()

func pack(b []uint8) uint64 {
	var v uint64
	for _, x := range b {
		v = v<<1 | uint64(x&1)
	}
	return v
}

func KeyFromSilence(f [FrameBits]uint8, sec int) (wc int, ok bool) {
	if sec < 0 || sec >= Sections {
		return 0, false
	}
	var x [FrameBits]uint8
	for i := range x {
		x[i] = f[i] ^ Silence[i]
	}
	wc, ok = keyTable[sec][pack(x[:])]
	return wc, ok
}

func Encode(fn int, voice [][FrameBits]uint8, ctl [ControlBytes]byte, wc int) []uint8 {
	info := [Sections][]uint8{make([]uint8, FrameBits+tail), make([]uint8, FrameBits+tail), make([]uint8, FrameBits+tail)}
	for k, sec := range VoiceSections(fn) {
		f := Descramble(voice[k], sec, wc)
		copy(info[sec], f[:])
	}
	out := make([]uint8, 0, l1.A1TCH1Bits)
	c0 := fec.ConvEncodeG(info[0], codeK, codeG1, codeG2)
	if fn == 1 {
		c0 = EncodeControl(ctl, wc)
	}
	out = append(out, c0...)
	out = append(out, fec.ConvEncodeG(info[1], codeK, codeG1, codeG2)...)
	out = append(out, fec.ConvEncodeG(info[2], codeK, codeG1, codeG2)...)
	return append(out, Pad[:]...)
}
