package dsp

import (
	"math"
	"math/bits"
)

var (
	tchSym1 = symbolRange(4, 69, 10)
	tchSym2 = symbolRange(80, 145, 138)
)

func symbolRange(lo, hi, skip int) []int {
	out := make([]int, 0, hi-lo-1)
	for s := lo; s < hi; s++ {
		if s != skip {
			out = append(out, s)
		}
	}
	return out
}

const cSym = 79

var channelTypes = map[int]string{
	0b1010: "TCH(I)", 0b0010: "FACCH", 0b1000: "TCH(B)", 0b0000: "空線",
}

var channelTypeOrder = []int{0b0000, 0b0010, 0b1000, 0b1010}

func IsVoiceType(t string) bool { return t == "TCH(B)" || t == "TCH(I)" }

type TchBurst struct {
	Pos   int
	Bits  []uint8
	CType string
	CDist int
}

func ClassifyChannelType(cBits []uint8) (string, int) {
	v := 0
	for _, b := range cBits[:4] {
		v = (v << 1) | int(b&1)
	}
	best, bestDist := channelTypeOrder[0], 5
	for _, c := range channelTypeOrder {
		if d := bits.OnesCount(uint(c ^ v)); d < bestDist {
			best, bestDist = c, d
		}
	}
	return channelTypes[best], bestDist
}

func TchBurstFromSlot(slot []complex64, pos int) *TchBurst {
	if len(slot) < 145 {
		return nil
	}
	syms1 := pick(slot, tchSym1)
	syms2 := pick(slot, tchSym2)
	b := make([]uint8, 0, 512)
	b = append(b, SymbolsToBitsThreshold(syms1, nil)...)
	b = append(b, SymbolsToBitsThreshold(syms2, nil)...)
	if len(b) != 512 {
		return nil
	}
	var tr, tq float64
	for _, s := range syms1 {
		tr += math.Abs(float64(real(s)))
		tq += math.Abs(float64(imag(s)))
	}
	for _, s := range syms2 {
		tr += math.Abs(float64(real(s)))
		tq += math.Abs(float64(imag(s)))
	}
	n := float64(len(syms1) + len(syms2))
	thr := [2]float64{tr / n, tq / n}
	cBits := SymbolsToBitsThreshold(slot[cSym:cSym+1], &thr)
	ctype, dist := ClassifyChannelType(cBits)
	return &TchBurst{Pos: pos, Bits: b, CType: ctype, CDist: dist}
}

func pick(v []complex64, idx []int) []complex64 {
	out := make([]complex64, len(idx))
	for i, k := range idx {
		out[i] = v[k]
	}
	return out
}

func SlotBits(slot []complex64) []uint8 {
	b := SymbolsToBits(slot)
	if len(b) > BitsPerSlot {
		b = b[:BitsPerSlot]
	}
	return b
}

func TCHSymbolIndices() []int {
	out := make([]int, 0, len(tchSym1)+len(tchSym2))
	out = append(out, tchSym1...)
	out = append(out, tchSym2...)
	return out
}
