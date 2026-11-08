package scpca1f72

import (
	"github.com/symysak/arib/internal/std-t61/fec"
	"github.com/symysak/arib/internal/std-t61/l1"
	"github.com/symysak/arib/internal/vocoder/ambe"
)

const (
	FrameBits = 72
	FramesPerV = 4
	Patterns = l1.A1Patterns
)

func VoiceFN(fn int) bool { return fn == 0 || fn == 1 }

func Mask(wc int) []uint8 { return fec.PN95(wc, l1.A1TCH1Bits) }

func Split(tch1 []uint8, wc int) ([FramesPerV][FrameBits]uint8, bool) {
	var out [FramesPerV][FrameBits]uint8
	if len(tch1) != l1.A1TCH1Bits {
		return out, false
	}
	m := Mask(wc)
	for k := range out {
		for j := range out[k] {
			i := k*FrameBits + j
			out[k][j] = tch1[i] ^ m[i]
		}
	}
	return out, true
}

func Encode(frames [FramesPerV][FrameBits]uint8, wc int) []uint8 {
	out := make([]uint8, l1.A1TCH1Bits)
	m := Mask(wc)
	for i := range out {
		var b uint8
		if i < FramesPerV*FrameBits {
			b = frames[i/FrameBits][i%FrameBits]
		}
		out[i] = b ^ m[i]
	}
	return out
}

func KeyScores(tch1s [][]uint8) [Patterns]int {
	var sc [Patterns]int
	for wc := 0; wc < Patterns; wc++ {
		for _, t := range tch1s {
			fs, ok := Split(t, wc)
			if !ok {
				continue
			}
			for _, f := range fs {
				if ambe.Valid3600(f) {
					sc[wc]++
				}
			}
		}
	}
	return sc
}

func Best(sc [Patterns]int) (wc, n, second int) {
	for i, v := range sc {
		if v > n {
			wc, second, n = i, n, v
		} else if v > second {
			second = v
		}
	}
	return
}

func Decided(n, second, voiceFrames int) bool {
	return n >= 2 && 2*n >= voiceFrames*FramesPerV && 4*second <= n
}
