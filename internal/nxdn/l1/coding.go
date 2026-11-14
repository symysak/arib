package l1


type Channel struct {
	Name     string
	InfoBits int
	CRCBits  int
	CRCPoly  uint32
	Punct    [2][]uint8
	Rows     int
	Cols     int
}

var (
	SACCH = Channel{Name: "SACCH", InfoBits: 26, CRCBits: 6, CRCPoly: 0x27,
		Punct: [2][]uint8{{1, 1, 1, 1, 1, 1}, {1, 1, 0, 1, 1, 0}}, Rows: 5, Cols: 12}
	FACCH1 = Channel{Name: "FACCH1", InfoBits: 80, CRCBits: 12, CRCPoly: 0x80F,
		Punct: [2][]uint8{{1, 1}, {0, 1}}, Rows: 9, Cols: 16}
	UDCH1 = Channel{Name: "UDCH1/FACCH2", InfoBits: 184, CRCBits: 15, CRCPoly: 0x4CC5,
		Punct: [2][]uint8{{1, 1, 1, 1, 1, 1, 1}, {1, 0, 1, 1, 1, 0, 1}}, Rows: 29, Cols: 12}
)

const tailBits = 4

func (c Channel) convBits() int { return c.InfoBits + c.CRCBits + tailBits }

func (c Channel) CodedBits() int {
	n := 0
	p := len(c.Punct[0])
	for i := 0; i < c.convBits(); i++ {
		n += int(c.Punct[0][i%p] + c.Punct[1][i%p])
	}
	return n
}

func (c Channel) CRC(info []uint8) []uint8 {
	w := uint(c.CRCBits)
	mask := uint32(1)<<w - 1
	reg := mask
	for _, b := range info {
		fb := uint32(b&1) ^ (reg >> (w - 1) & 1)
		reg = (reg << 1) & mask
		if fb != 0 {
			reg ^= c.CRCPoly
		}
	}
	out := make([]uint8, w)
	for i := range out {
		out[i] = uint8(reg >> (w - 1 - uint(i)) & 1)
	}
	return out
}

func (c Channel) Encode(info []uint8) []uint8 {
	in := append(append(append([]uint8{}, info...), c.CRC(info)...), make([]uint8, tailBits)...)
	var reg uint8
	p := len(c.Punct[0])
	var y []uint8
	for i, u := range in {
		u &= 1
		d1, d2, d3, d4 := reg&1, reg>>1&1, reg>>2&1, reg>>3&1
		g1 := u ^ d3 ^ d4
		g2 := u ^ d1 ^ d2 ^ d4
		if c.Punct[0][i%p] == 1 {
			y = append(y, g1)
		}
		if c.Punct[1][i%p] == 1 {
			y = append(y, g2)
		}
		reg = (reg<<1 | u) & 0xF
	}
	out := make([]uint8, len(y))
	for k := range out {
		out[k] = y[(k%c.Rows)*c.Cols+k/c.Rows]
	}
	return out
}

type Result struct {
	Info []uint8
	CRCOK bool
	Corrected int
}

func (c Channel) Decode(soft []float32) Result {
	n := c.CodedBits()
	if len(soft) != n {
		return Result{}
	}
	y := make([]float32, n)
	for k, v := range soft {
		y[(k%c.Rows)*c.Cols+k/c.Rows] = v
	}
	L := c.convBits()
	g := make([]float32, 2*L)
	p := len(c.Punct[0])
	j := 0
	for i := 0; i < L; i++ {
		for r := 0; r < 2; r++ {
			if c.Punct[r][i%p] == 1 {
				g[2*i+r] = y[j]
				j++
			}
		}
	}
	dec := viterbi(g, L)
	info := dec[:c.InfoBits]
	crc := c.CRC(info)
	ok := true
	for i, b := range crc {
		if dec[c.InfoBits+i] != b {
			ok = false
			break
		}
	}
	re := c.Encode(info)
	diff := 0
	for k, v := range soft {
		if v != 0 && (v < 0) != (re[k] == 1) {
			diff++
		}
	}
	return Result{Info: append([]uint8(nil), info...), CRCOK: ok, Corrected: diff}
}

func viterbi(g []float32, L int) []uint8 {
	const ns = 16
	const inf = float32(-1e30)
	var pm, nm [ns]float32
	for s := range pm {
		pm[s] = inf
	}
	pm[0] = 0
	prev := make([][ns]uint8, L)
	for i := 0; i < L; i++ {
		for s := range nm {
			nm[s] = inf
		}
		s1, s2 := g[2*i], g[2*i+1]
		for s := 0; s < ns; s++ {
			if pm[s] == inf {
				continue
			}
			d1, d2, d3, d4 := uint8(s)&1, uint8(s)>>1&1, uint8(s)>>2&1, uint8(s)>>3&1
			for u := uint8(0); u < 2; u++ {
				o1 := u ^ d3 ^ d4
				o2 := u ^ d1 ^ d2 ^ d4
				m := pm[s]
				if o1 == 0 {
					m += s1
				} else {
					m -= s1
				}
				if o2 == 0 {
					m += s2
				} else {
					m -= s2
				}
				t := (s<<1 | int(u)) & 0xF
				if m > nm[t] {
					nm[t] = m
					prev[i][t] = uint8(s)
				}
			}
		}
		pm = nm
	}
	out := make([]uint8, L)
	s := 0
	for i := L - 1; i >= 0; i-- {
		out[i] = uint8(s & 1)
		s = int(prev[i][s])
	}
	return out
}
