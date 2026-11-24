package scpca1u264

import (
	"sync"

	"github.com/symysak/arib/internal/data"
	"github.com/symysak/arib/internal/std-t61/fec"
	scpca1f40 "github.com/symysak/arib/internal/std-t61/scpc-a1f40"
)


const (
	DataBits = 212
	DataChannel data.Channel = "T61-A1-U212"
	DataName = "u212"
	DataMaxErrs = 8

	infoBits  = 236
	pnBits    = 228
	codedBits = 2 * infoBits
	keptBits  = Bits + 3
)

func KeyFromWC(wc int) int {
	s := fec.PN95(wc, 511)
	k := 0
	for i := 0; i < 9; i++ {
		k |= int(s[(4*i+181)%511]) << i
	}
	return k
}

var wcOfKey = sync.OnceValue(func() (t [Patterns]int) {
	for i := range t {
		t[i] = -1
	}
	for wc := 1; wc < Patterns; wc++ {
		t[KeyFromWC(wc)] = wc
	}
	return
})

func WCFromKey(key int) (int, bool) {
	if key <= 0 || key >= Patterns {
		return 0, false
	}
	wc := wcOfKey()[key]
	return wc, wc > 0
}

func crc16x(bits []uint8) uint16 {
	var reg uint16
	for _, b := range bits {
		fb := (reg>>15 ^ uint16(b&1)) & 1
		reg <<= 1
		if fb == 1 {
			reg ^= 0x1021
		}
	}
	return reg
}

func DecodeData(tch1 []uint8, wc int) (d []uint8, errs int, ok bool) {
	if len(tch1) != Bits || wc <= 0 || wc >= Patterns {
		return nil, 0, false
	}
	kept := make([]float32, keptBits)
	for i, p := range order {
		kept[i] = 1 - 2*float32(tch1[p]&1)
	}
	for i := Bits; i < keptBits; i++ {
		kept[i] = 2
	}
	coded := make([]float32, codedBits)
	for c, j := 0, 0; c < codedBits; c++ {
		if c%2 == 0 || c%8 == 7 {
			coded[c] = kept[j]
			j++
		}
	}
	info, errs := scpca1f40.Viterbi(coded, true)
	pn := fec.PN95(wc, pnBits)
	for i := range pn {
		info[i] ^= pn[i]
	}
	return info[:DataBits], errs, crc16x(info[:DataBits]) == uint16(fec.BitsToUint(info[DataBits:DataBits+16]))
}

func EncodeData(d []uint8, wc int) []uint8 {
	info := make([]uint8, infoBits)
	copy(info, d[:DataBits])
	copy(info[DataBits:], fec.UintToBits(uint64(crc16x(d[:DataBits])), 16))
	pn := fec.PN95(wc, pnBits)
	for i := range pn {
		info[i] ^= pn[i]
	}
	coded := fec.ConvEncodeG(info, 7, 0o155, 0o117)
	kept := make([]uint8, 0, keptBits)
	for c, b := range coded {
		if c%2 == 0 || c%8 == 7 {
			kept = append(kept, b)
		}
	}
	tch1 := make([]uint8, Bits)
	for i, p := range order {
		tch1[p] = kept[i]
	}
	return tch1
}
