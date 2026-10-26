package fec

import "math/bits"

const (
	ConvK     = 6
	ConvPoly1 = 0o53
	ConvPoly2 = 0o75
	TailBits  = 5
)

func ConvEncode(in []uint8) []uint8 {
	out := make([]uint8, 0, 2*len(in))
	reg := 0
	for _, b := range in {
		reg = ((reg << 1) | int(b&1)) & ((1 << ConvK) - 1)
		out = append(out, uint8(bits.OnesCount(uint(reg&ConvPoly1))&1))
		out = append(out, uint8(bits.OnesCount(uint(reg&ConvPoly2))&1))
	}
	return out
}

var (
	vitOut  [1 << (ConvK - 1)][2][2]int8
	vitNext [1 << (ConvK - 1)][2]int
)

func init() {
	for s := 0; s < 1<<(ConvK-1); s++ {
		for b := 0; b < 2; b++ {
			reg := (s << 1) | b
			o1 := bits.OnesCount(uint(reg&ConvPoly1)) & 1
			o2 := bits.OnesCount(uint(reg&ConvPoly2)) & 1
			vitOut[s][b] = [2]int8{int8(1 - 2*o1), int8(1 - 2*o2)}
			vitNext[s][b] = reg & ((1 << (ConvK - 1)) - 1)
		}
	}
}

func ViterbiDecode(soft []float32) ([]uint8, float64) {
	n := len(soft) / 2
	const S = 1 << (ConvK - 1)
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
			if pm[s] <= ninf/2 {
				continue
			}
			for b := 0; b < 2; b++ {
				o := vitOut[s][b]
				m := pm[s] + float64(o[0])*r1 + float64(o[1])*r2
				ns := vitNext[s][b]
				if m > npm[ns] {
					npm[ns] = m
					bp[i][ns] = uint8(b)
					prev[i][ns] = uint8(s)
				}
			}
		}
		pm = npm
	}
	out := make([]uint8, n)
	s := 0
	for i := n - 1; i >= 0; i-- {
		out[i] = bp[i][s]
		s = int(prev[i][s])
	}
	return out, pm[0]
}

func ViterbiFree(soft []float32, k, g1, g2 int) ([]uint8, float64) {
	n := len(soft) / 2
	if n == 0 || k < 2 || k > 12 {
		return nil, 0
	}
	S := 1 << uint(k-1)
	out1 := make([][2]int8, S)
	out2 := make([][2]int8, S)
	next := make([][2]int, S)
	for st := 0; st < S; st++ {
		for b := 0; b < 2; b++ {
			reg := (st << 1) | b
			out1[st][b] = int8(1 - 2*(bits.OnesCount(uint(reg&g1))&1))
			out2[st][b] = int8(1 - 2*(bits.OnesCount(uint(reg&g2))&1))
			next[st][b] = reg & (S - 1)
		}
	}
	const ninf = -1e30
	pm := make([]float64, S)
	npm := make([]float64, S)
	bp := make([][]uint8, n)
	prev := make([][]uint8, n)
	for i := range bp {
		bp[i] = make([]uint8, S)
		prev[i] = make([]uint8, S)
	}
	for i := 0; i < n; i++ {
		r1, r2 := float64(soft[2*i]), float64(soft[2*i+1])
		for s := range npm {
			npm[s] = ninf
		}
		for s := 0; s < S; s++ {
			for b := 0; b < 2; b++ {
				m := pm[s] + float64(out1[s][b])*r1 + float64(out2[s][b])*r2
				ns := next[s][b]
				if m > npm[ns] {
					npm[ns] = m
					bp[i][ns] = uint8(b)
					prev[i][ns] = uint8(s)
				}
			}
		}
		copy(pm, npm)
	}
	best, bestM := 0, pm[0]
	for s := 1; s < S; s++ {
		if pm[s] > bestM {
			best, bestM = s, pm[s]
		}
	}
	res := make([]uint8, n)
	s := best
	for i := n - 1; i >= 0; i-- {
		res[i] = bp[i][s]
		s = int(prev[i][s])
	}
	return res, bestM
}

func ConvEncodeG(in []uint8, k, g1, g2 int) []uint8 {
	out := make([]uint8, 0, 2*len(in))
	reg := 0
	for _, b := range in {
		reg = ((reg << 1) | int(b&1)) & (1<<uint(k) - 1)
		out = append(out, uint8(bits.OnesCount(uint(reg&g1))&1))
		out = append(out, uint8(bits.OnesCount(uint(reg&g2))&1))
	}
	return out
}

func HardToSoft(b []uint8) []float32 {
	out := make([]float32, len(b))
	for i, v := range b {
		if v&1 == 0 {
			out[i] = 1
		} else {
			out[i] = -1
		}
	}
	return out
}

func CRC6(data []uint8) uint8 {
	reg := uint8(0x3F)
	for _, b := range data {
		fb := ((reg >> 5) ^ (b & 1)) & 1
		reg = (reg << 1) & 0x3F
		if fb == 1 {
			reg ^= 0x03
		}
	}
	return reg
}

func CRCLSB(data []uint8, feedback, init uint32, nbits int) uint32 {
	mask := uint32(1)<<uint(nbits) - 1
	reg := init & mask
	for _, b := range data {
		fb := (reg ^ uint32(b&1)) & 1
		reg >>= 1
		if fb == 1 {
			reg ^= feedback & mask
		}
	}
	return reg & mask
}

func CRC4(data []uint8, init uint8) uint8 {
	reg := init & 0x0F
	for _, b := range data {
		fb := ((reg >> 3) ^ (b & 1)) & 1
		reg = (reg << 1) & 0x0F
		if fb == 1 {
			reg ^= 0x09
		}
	}
	return reg
}

func CRC8(data []uint8, init uint8) uint8 {
	reg := init
	for _, b := range data {
		fb := ((reg >> 7) ^ (b & 1)) & 1
		reg <<= 1
		if fb == 1 {
			reg ^= 0x07
		}
	}
	return reg
}

func CRC16(data []uint8) uint16 {
	reg := uint16(0xFFFF)
	for _, b := range data {
		fb := ((reg >> 15) ^ uint16(b&1)) & 1
		reg <<= 1
		if fb == 1 {
			reg ^= 0x1021
		}
	}
	return reg
}

func BitsToUint(b []uint8) uint64 {
	var v uint64
	for _, x := range b {
		v = (v << 1) | uint64(x&1)
	}
	return v
}

func UintToBits(v uint64, n int) []uint8 {
	out := make([]uint8, n)
	for i := 0; i < n; i++ {
		out[i] = uint8((v >> uint(n-1-i)) & 1)
	}
	return out
}

func Interleave(in []uint8, rows, cols int) []uint8 {
	out := make([]uint8, 0, len(in))
	for c := 0; c < cols; c++ {
		for r := 0; r < rows; r++ {
			out = append(out, in[r*cols+c])
		}
	}
	return out
}

func Deinterleave(in []uint8, rows, cols int) []uint8 {
	out := make([]uint8, len(in))
	k := 0
	for c := 0; c < cols; c++ {
		for r := 0; r < rows; r++ {
			out[r*cols+c] = in[k]
			k++
		}
	}
	return out
}

func DeinterleaveSoft(in []float32, rows, cols int) []float32 {
	out := make([]float32, len(in))
	k := 0
	for c := 0; c < cols; c++ {
		for r := 0; r < rows; r++ {
			out[r*cols+c] = in[k]
			k++
		}
	}
	return out
}

const ScrambleAllOnes = 0x1FF

func PN95(init int, n int) []uint8 {
	reg := uint32(init) & 0x1FF
	out := make([]uint8, n)
	for k := range out {
		out[k] = uint8(reg & 1)
		fb := (reg ^ (reg >> 4)) & 1
		reg = (reg >> 1) | (fb << 8)
	}
	return out
}

func DescrambleSoft(in []float32, init int) []float32 {
	pn := PN95(init, len(in))
	out := make([]float32, len(in))
	for i, v := range in {
		if pn[i] == 1 {
			out[i] = -v
		} else {
			out[i] = v
		}
	}
	return out
}

func Descramble(in []uint8, init int) []uint8 {
	pn := PN95(init, len(in))
	out := make([]uint8, len(in))
	for i, v := range in {
		out[i] = (v & 1) ^ pn[i]
	}
	return out
}

type Block struct {
	Name    string
	Info    int
	CRCBits int
	Rows    int
}

func (b Block) Coded() int { return (b.Info + b.CRCBits + TailBits) * 2 }

func (b Block) Cols() int { return b.Coded() / b.Rows }

var (
	BlockRICH = Block{"RICH", 17, 6, 8}
	BlockSyncBurst = Block{"SB", 41, 6, 13}
	BlockPCCDown = Block{"PCC↓", 115, 16, 17}
	BlockPCCUp1 = Block{"PCC↑1", 79, 16, 20}
	BlockPCCUp2 = Block{"PCC↑2", 111, 16, 22}
	BlockFACCH = Block{"FACCH", 107, 16, 16}
	BlockSACCH = Block{"SACCH", 59, 16, 16}
	BlockRCH = Block{"RCH", 9, 6, 8}
)

func (b Block) Encode(info []uint8) []uint8 {
	if len(info) != b.Info {
		panic("fec: 情報ビット数が違う")
	}
	seq := make([]uint8, 0, b.Info+b.CRCBits+TailBits)
	seq = append(seq, info...)
	if b.CRCBits == 6 {
		seq = append(seq, UintToBits(uint64(CRC6(info)), 6)...)
	} else {
		seq = append(seq, UintToBits(uint64(CRC16(info)), 16)...)
	}
	seq = append(seq, make([]uint8, TailBits)...)
	return Interleave(ConvEncode(seq), b.Rows, b.Cols())
}

type Result struct {
	Info   []uint8
	CRCOK  bool
	Metric float64
	Corrected int
}

func (b Block) Decode(soft []float32) Result {
	if len(soft) != b.Coded() {
		panic("fec: 符号ビット数が違う")
	}
	y := DeinterleaveSoft(soft, b.Rows, b.Cols())
	dec, metric := ViterbiDecode(y)
	info := dec[:b.Info]
	crc := dec[b.Info : b.Info+b.CRCBits]
	var ok bool
	if b.CRCBits == 6 {
		ok = uint8(BitsToUint(crc)) == CRC6(info)
	} else {
		ok = uint16(BitsToUint(crc)) == CRC16(info)
	}
	reenc := ConvEncode(dec)
	corr := 0
	for i := range reenc {
		hard := uint8(0)
		if y[i] < 0 {
			hard = 1
		}
		if reenc[i] != hard {
			corr++
		}
	}
	return Result{Info: info, CRCOK: ok, Metric: metric, Corrected: corr}
}
