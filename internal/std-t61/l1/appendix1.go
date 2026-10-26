package l1

import (
	"fmt"

	"github.com/symysak/arib/internal/std-t61/fec"
)

const (
	A1FrameBits = 384
	A1SubFrameBits = 192

	a1RStart   = 0
	a1SWStart  = 8
	a1SWEnd    = 28
	A1RICHBits = 52
	A1TCH1Bits = 292
	A1TCH2Bits = 152

	A1DataStart = a1SWEnd
)

const (
	A1SW1 = 0x1E56F
	A1SW2 = 0x31BAF
)

var a1RICHSlot = buildA1RICHSlot()

func buildA1RICHSlot() [A1RICHBits]uint8 {
	const rows, cols = 7, 8
	var out [A1RICHBits]uint8
	s := 0
	for row := 0; row < rows; row++ {
		for col := 0; col < cols; col++ {
			r := col*rows + row
			if r >= A1RICHBits {
				continue
			}
			out[s] = uint8(r)
			s++
		}
	}
	return out
}

type A1RICH struct {
	OK  bool
	CFN uint8
	FN  uint8
	IT  uint8
	LCFEMP uint8
	REP    uint8
	Undef  uint8
	Raw    uint16
	Metric float64
	CRCVariant string
}

func (r A1RICH) IsVoice() bool { return r.OK && r.IT == 0 }

func (r A1RICH) IsIdleLine() bool { return r.OK && r.IT == 1 && r.LCFEMP == 1 }

func (r A1RICH) Describe() string {
	if !r.OK {
		return "RICH NG"
	}
	kind := "Vフレーム(音声)"
	last := ""
	if r.IT == 1 {
		kind = "Uフレーム(非音声)"
		if r.LCFEMP == 1 {
			last = " 空線信号"
		}
	} else if r.LCFEMP == 1 {
		last = " 最終クラスタ"
	}
	rep := "直接通信"
	if r.REP == 1 {
		rep = "基地局経由"
	}
	return fmt.Sprintf("CFN=%d FN=%d %s%s %s 未定義=%02b", r.CFN, r.FN, kind, last, rep, r.Undef)
}

type a1CRCVariant struct {
	name string
	c4   func(data []uint8, init uint8) uint8
	c8   func(data []uint8, init uint8) uint8
}

var a1CRCVariants = []a1CRCVariant{
	{"相反の反転形",
		func(d []uint8, init uint8) uint8 { return uint8(fec.CRCLSB(d, 12, uint32(init), 4)) },
		func(d []uint8, init uint8) uint8 { return uint8(fec.CRCLSB(d, 131, uint32(init), 8)) }},
	{"印字形", fec.CRC4, fec.CRC8},
}

var A1RICHCRCInit = []uint8{0xFF, 0x00}

func DecodeA1RICH(code []float32) A1RICH {
	if len(code) != A1RICHBits {
		return A1RICH{}
	}
	de := make([]float32, A1RICHBits)
	for s, r := range a1RICHSlot {
		de[r] = code[s]
	}
	bits, metric := fec.ViterbiDecode(de)
	if len(bits) != 26 {
		return A1RICH{}
	}
	info, c4, c8 := bits[0:9], bits[9:13], bits[13:21]
	for _, v := range a1CRCVariants {
		for _, init := range A1RICHCRCInit {
			if v.c4(info, init) != uint8(fec.BitsToUint(c4)) {
				continue
			}
			if v.c8(bits[0:13], init) != uint8(fec.BitsToUint(c8)) {
				continue
			}
			x := uint16(fec.BitsToUint(info))
			return A1RICH{
				OK: true, Metric: metric, Raw: x, CRCVariant: v.name,
				CFN:    uint8(x>>7) & 3,
				FN:     uint8(x>>5) & 3,
				IT:     uint8(x>>4) & 1,
				LCFEMP: uint8(x>>3) & 1,
				REP:    uint8(x>>2) & 1,
				Undef:  uint8(x) & 3,
			}
		}
	}
	return A1RICH{Metric: metric}
}

func EncodeA1RICH(r A1RICH, init uint8) []uint8 { return EncodeA1RICHVariant(r, init, 0) }

func EncodeA1RICHVariant(r A1RICH, init uint8, variant int) []uint8 {
	cv := a1CRCVariants[variant]
	x := uint16(r.CFN&3)<<7 | uint16(r.FN&3)<<5 | uint16(r.IT&1)<<4 |
		uint16(r.LCFEMP&1)<<3 | uint16(r.REP&1)<<2 | uint16(r.Undef&3)
	info := fec.UintToBits(uint64(x), 9)
	in := append([]uint8(nil), info...)
	in = append(in, fec.UintToBits(uint64(cv.c4(info, init)), 4)...)
	in = append(in, fec.UintToBits(uint64(cv.c8(in[0:13], init)), 8)...)
	in = append(in, make([]uint8, fec.TailBits)...)
	coded := fec.ConvEncode(in)
	out := make([]uint8, A1RICHBits)
	for s, rr := range a1RICHSlot {
		out[s] = coded[rr]
	}
	return out
}

func A1Descramble(tch []float32, pattern int) []float32 {
	return fec.DescrambleSoft(tch, pattern)
}

const A1Patterns = 512

func A1TCH1Mask(pattern int) []uint8 { return fec.PN95(pattern, A1TCH1Bits) }

func A1DescrambleBits(b []uint8, pattern int) []uint8 { return fec.Descramble(b, pattern) }

func A1SplitFrame(frame []float32) (rich, tch1 []float32, ok bool) {
	if len(frame) < A1DataStart+A1RICHBits+A1TCH1Bits {
		return nil, nil, false
	}
	b := frame[A1DataStart:]
	return b[:A1RICHBits], b[A1RICHBits : A1RICHBits+A1TCH1Bits], true
}

func A1SubFrameTCH2(frame []float32) ([]float32, bool) {
	if len(frame) < A1DataStart+A1TCH2Bits {
		return nil, false
	}
	return frame[A1DataStart : A1DataStart+A1TCH2Bits], true
}
