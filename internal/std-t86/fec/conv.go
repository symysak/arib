package fec

import "math/bits"

const (
	ControlPoly1 = 0o53
	ControlPoly2 = 0o75
	ControlK     = 6
)

var ControlPolys = []int{ControlPoly1, ControlPoly2}

const Erasure uint8 = 2

func ConvEncode(input []uint8, polys []int, K int) []uint8 {
	n := len(polys)
	mask := (1 << K) - 1
	out := make([]uint8, 0, (len(input)+K-1)*n)
	reg := 0
	for i := 0; i < len(input)+K-1; i++ {
		var b int
		if i < len(input) {
			b = int(input[i] & 1)
		}
		reg = ((reg << 1) | b) & mask
		for _, p := range polys {
			out = append(out, uint8(bits.OnesCount(uint(reg&p))&1))
		}
	}
	return out
}

func ControlConvEncode(input []uint8) []uint8 {
	return ConvEncode(input, ControlPolys, ControlK)
}

type viterbiTable struct {
	out []uint16
	n   int
	S   int
}

func newViterbiTable(polys []int, K int) *viterbiTable {
	n := len(polys)
	S := 1 << (K - 1)
	t := &viterbiTable{out: make([]uint16, S*2), n: n, S: S}
	mask := (1 << K) - 1
	for s := 0; s < S; s++ {
		for b := 0; b <= 1; b++ {
			reg := ((s << 1) | b) & mask
			var v uint16
			for j, p := range polys {
				if bits.OnesCount(uint(reg&p))&1 == 1 {
					v |= 1 << uint(j)
				}
			}
			t.out[s*2+b] = v
		}
	}
	return t
}

func ViterbiDecode(coded []uint8, polys []int, K int, terminated bool) []uint8 {
	tab := newViterbiTable(polys, K)
	n, S := tab.n, tab.S
	nSteps := len(coded) / n
	half := S >> 1

	const inf = int32(1) << 30
	pm := make([]int32, S)
	next := make([]int32, S)
	for i := range pm {
		pm[i] = inf
	}
	pm[0] = 0
	prev := make([]uint8, nSteps*S)

	for t := 0; t < nSteps; t++ {
		var sym, valid uint16
		for j := 0; j < n; j++ {
			c := coded[t*n+j]
			if c == Erasure {
				continue
			}
			valid |= 1 << uint(j)
			if c&1 == 1 {
				sym |= 1 << uint(j)
			}
		}
		for ns := 0; ns < S; ns++ {
			p0 := ns >> 1
			p1 := p0 | half
			b := ns & 1
			bm0 := int32(bits.OnesCount16((tab.out[p0*2+b] ^ sym) & valid))
			bm1 := int32(bits.OnesCount16((tab.out[p1*2+b] ^ sym) & valid))
			c0 := pm[p0] + bm0
			c1 := pm[p1] + bm1
			if c1 < c0 {
				next[ns] = c1
				prev[t*S+ns] = 1
			} else {
				next[ns] = c0
				prev[t*S+ns] = 0
			}
		}
		pm, next = next, pm
	}

	state := 0
	if !terminated {
		best := pm[0]
		for s := 1; s < S; s++ {
			if pm[s] < best {
				best, state = pm[s], s
			}
		}
	}
	dec := make([]uint8, nSteps)
	for t := nSteps - 1; t >= 0; t-- {
		dec[t] = uint8(state & 1)
		p0 := state >> 1
		if prev[t*S+state] == 1 {
			state = p0 | half
		} else {
			state = p0
		}
	}
	if terminated {
		dec = dec[:nSteps-(K-1)]
	}
	return dec
}

func ControlViterbiDecode(coded []uint8) []uint8 {
	return ViterbiDecode(coded, ControlPolys, ControlK, true)
}

func CRCCheck(input []uint8, poly int, width int) int {
	reg := 0
	topbit := 1 << width
	full := topbit | poly
	for _, b := range input {
		reg = (reg << 1) | int(b&1)
		if reg&topbit != 0 {
			reg ^= full
		}
	}
	return reg & (topbit - 1)
}
