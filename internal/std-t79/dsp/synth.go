package dsp

import (
	"math"
	"math/cmplx"
)

func Modulate(bits []uint8, sps int) []complex64 {
	if len(bits)%2 == 1 {
		bits = append(append([]uint8(nil), bits...), 0)
	}
	n := len(bits) / 2
	sym := make([]complex128, n+1)
	sym[0] = 1
	for k := 0; k < n; k++ {
		sym[k+1] = sym[k] * cmplx.Rect(1, DeltaPhase(bits[2*k], bits[2*k+1]))
	}
	h := RootRaisedCosine(sps, RollOff, rrcSpan)
	out := make([]complex128, len(sym)*sps+len(h))
	for k, s := range sym {
		base := k * sps
		for i, v := range h {
			out[base+i] += s * complex(v, 0)
		}
	}
	res := make([]complex64, len(out))
	for i, v := range out {
		res[i] = complex64(v)
	}
	return res
}

func DeltaPhase(x, y uint8) float64 {
	switch (x&1)<<1 | (y & 1) {
	case 0b00:
		return math.Pi / 4
	case 0b01:
		return 3 * math.Pi / 4
	case 0b11:
		return -3 * math.Pi / 4
	default:
		return -math.Pi / 4
	}
}

func ApplyCFO(x []complex64, fs, hz float64) []complex64 {
	out := make([]complex64, len(x))
	for i, v := range x {
		out[i] = complex64(c128(v) * cmplx.Rect(1, 2*math.Pi*hz*float64(i)/fs))
	}
	return out
}
