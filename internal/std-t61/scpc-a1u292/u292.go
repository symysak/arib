package scpca1u292

import (
	"math"
	"slices"

	"github.com/symysak/arib/internal/data"
	"github.com/symysak/arib/internal/std-t61/fec"
	"github.com/symysak/arib/internal/std-t61/l1"
)

const (
	Bits = l1.A1TCH1Bits
	BodyBits = 174
	CRCBits = 16
	Steps = BodyBits + CRCBits + 5
	Patterns = l1.A1Patterns

	rows, cols = 18, 17
	k          = 5
	states     = 1 << (k - 1)
	g0, g1     = 0b10011, 0b11101
)

const Channel data.Channel = "T61-A1-U292"

const Name = "u292"

type cell struct {
	step int
	out  int
}

var (
	tx [Bits]int
	punct [Bits]cell
	next    [states][2]int
	outBits [states][2][2]uint8
	idle [Bits]uint8
)

func init() {
	n := 0
	for t := 0; t < Steps && n < Bits; t++ {
		for o := 0; o < 2 && n < Bits; o++ {
			if t%2 == 1 && o == 1 {
				continue
			}
			punct[n] = cell{t, o}
			n++
		}
	}
	p := 0
	for c := 0; c < cols; c++ {
		for r := 0; r < rows; r++ {
			if i := r*cols + c; i < Bits {
				tx[p] = i
				p++
			}
		}
	}
	for s := 0; s < states; s++ {
		for b := 0; b < 2; b++ {
			reg := s<<1 | b
			outBits[s][b] = [2]uint8{parity(reg & g0), parity(reg & g1)}
			next[s][b] = reg & (states - 1)
		}
	}
	code := encodeCode(make([]uint8, BodyBits))
	for q := range idle {
		idle[q] = code[tx[q]]
	}
}

func parity(x int) uint8 {
	var v uint8
	for ; x != 0; x &= x - 1 {
		v ^= 1
	}
	return v
}

func CRC(body []uint8) uint16 { return fec.CRC16(body) ^ 0xFFFF }

func Mask(wc int) []uint8 { return fec.PN95(wc, Bits) }

func encodeCode(body []uint8) []uint8 {
	u := make([]uint8, 0, Steps)
	u = append(u, body...)
	u = append(u, fec.UintToBits(uint64(CRC(body)), CRCBits)...)
	u = append(u, make([]uint8, Steps-len(u))...)
	full := make([][2]uint8, Steps)
	s := 0
	for t, b := range u {
		full[t] = outBits[s][b]
		s = next[s][b]
	}
	code := make([]uint8, Bits)
	for i, c := range punct {
		code[i] = full[c.step][c.out]
	}
	return code
}

func Encode(body []uint8, wc int) []uint8 {
	code := encodeCode(body)
	m := Mask(wc)
	out := make([]uint8, Bits)
	for p := range out {
		out[p] = code[tx[p]] ^ m[p]
	}
	return out
}

type Result struct {
	Body []uint8
	OK   bool
	Corrected int
	WC        int
}

func (r Result) IsIdle() bool {
	for _, b := range r.Body {
		if b != 0 {
			return false
		}
	}
	return r.OK
}

func Decode(tch1 []uint8, soft []float32, wc int) Result {
	if len(tch1) != Bits || (soft != nil && len(soft) != Bits) {
		return Result{WC: wc}
	}
	if soft != nil {
		if r := decode(tch1, clip(soft), wc); r.OK {
			return r
		}
	}
	return decode(tch1, nil, wc)
}

func clip(soft []float32) []float64 {
	a := make([]float64, len(soft))
	for i, v := range soft {
		a[i] = math.Abs(float64(v))
	}
	m := slices.Clone(a)
	slices.Sort(m)
	lim := 2 * m[len(m)/2]
	for i := range a {
		if lim > 0 && a[i] > lim {
			a[i] = lim
		}
	}
	return a
}

func decode(tch1 []uint8, mag []float64, wc int) Result {
	res := Result{WC: wc}
	m := Mask(wc)
	var rx [Steps][2]float64
	for p := 0; p < Bits; p++ {
		v := 1.0
		if mag != nil {
			v = mag[p]
		}
		if tch1[p]^m[p] == 1 {
			v = -v
		}
		c := punct[tx[p]]
		rx[c.step][c.out] = v
	}
	inf := math.Inf(1)
	var pm [states]float64
	for s := 1; s < states; s++ {
		pm[s] = -inf
	}
	var from [Steps][states]uint8
	for t := 0; t < Steps; t++ {
		var np [states]float64
		for s := range np {
			np[s] = -inf
		}
		maxB := 1
		if t >= Steps-5 {
			maxB = 0
		}
		for s := 0; s < states; s++ {
			if pm[s] == -inf {
				continue
			}
			for b := 0; b <= maxB; b++ {
				o := outBits[s][b]
				mt := pm[s]
				for j := 0; j < 2; j++ {
					if o[j] == 0 {
						mt += rx[t][j]
					} else {
						mt -= rx[t][j]
					}
				}
				ns := next[s][b]
				if mt > np[ns] {
					np[ns] = mt
					from[t][ns] = uint8(s<<1 | b)
				}
			}
		}
		pm = np
	}
	u := make([]uint8, Steps)
	s := 0
	for t := Steps - 1; t >= 0; t-- {
		f := from[t][s]
		u[t] = f & 1
		s = int(f >> 1)
	}
	res.Body = u[:BodyBits]
	res.OK = CRC(res.Body) == uint16(fec.BitsToUint(u[BodyBits:BodyBits+CRCBits]))
	re := Encode(res.Body, wc)
	for p := range re {
		if re[p] != tch1[p]&1 {
			res.Corrected++
		}
	}
	return res
}

func KeyFromIdle(tch1 []uint8, maxErr int) (int, bool) {
	if len(tch1) != Bits {
		return 0, false
	}
	wc := 0
	for j := 0; j < 9; j++ {
		wc |= int((tch1[j]^idle[j])&1) << j
	}
	m := Mask(wc)
	e := 0
	for p := range m {
		if (tch1[p]^idle[p])&1 != m[p] {
			if e++; e > maxErr {
				return 0, false
			}
		}
	}
	return wc, true
}

func KeyByCRC(tch1 []uint8, soft []float32) []int {
	var out []int
	for wc := 0; wc < Patterns; wc++ {
		if Decode(tch1, soft, wc).OK {
			out = append(out, wc)
		}
	}
	return out
}
