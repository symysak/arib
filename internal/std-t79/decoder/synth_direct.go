package decoder

import (
	"math"
	"math/cmplx"
	"math/rand"

	"github.com/symysak/arib/internal/std-t79/dsp"
	"github.com/symysak/arib/internal/std-t79/l1"
)

const (
	SynthDirectA       = 1<<19 | 1234
	SynthDirectB       = 1<<19 | 5678
	SynthDirectGroup   = 55
	SynthDirectSCD     = 13101
	SynthDirectPattern = 0x1A
)

type SynthDirectCarrier struct {
	IQ    []complex64
	Fs    float64
	Voice map[string]bool
	DACCH []byte
}

func SynthDirect(frames, sps int, r *rand.Rand) SynthDirectCarrier {
	fs := dsp.SymbolRate * float64(sps)
	c := SynthDirectCarrier{Fs: fs, Voice: map[string]bool{}}
	c.DACCH = []byte{0x11, 0xFF, 0x03, 0x04, 0x46, 0x01, 0, 0, 0, 0, 0, 0}
	dy := l1.EncodeFACCH(l1.W{First: true, Last: true, WO: 6}, bytesBits(c.DACCH, 96))
	total := (frames + 2) * 4 * 160 * sps
	sig := make([]complex128, total)
	place := func(bits []uint8, frame, slot int, cfo, amp float64) {
		x := dsp.Modulate(bits, sps)
		at := (frame*4+slot)*160*sps + 3*sps
		for i, v := range x {
			if at+i < total {
				sig[at+i] += complex(amp, 0) * complex128(v) * cmplx.Rect(1, 2*math.Pi*cfo*float64(at+i)/fs)
			}
		}
	}
	initA := l1.InitFromPattern(SynthDirectPattern)
	var pichA, pichB [][]uint8
	for f := 1; f <= frames; f++ {
		k := f - 1
		switch {
		case k < 2:
			place(l1.BuildDirectSB(l1.ModeDirect, l1.DirectSB{BI: 1, IC: 1, SCD: SynthDirectSCD, ID: SynthDirectA}), f, 0, 200, 1)
		default:
			u := (k - 2) % l1.PICHUnits
			if u == 0 {
				if (k-2)/l1.PICHUnits%2 == 0 {
					pichA = l1.EncodePICH(1, 0, SynthDirectGroup)
				} else {
					pichA = l1.EncodePICH(1, 1, SynthDirectA)
				}
			}
			data := randBits(r, 256)
			sw := l1.DSW{OK: true, Mode: l1.ModeDirect, Voice: true, First: u == 0, N: map[bool]int{true: 9, false: 10}[u == 0]}
			if k == 8 || k == 9 {
				l1.PlaceFACCH(data, dy, k == 9)
				sw.Ctl = true
			} else {
				c.Voice[string(data)] = true
			}
			place(l1.BuildDirect(sw, data, pichA[u], initA), f, 0, 200, 1)
		}
		if k >= 4 {
			u := (k - 4) % l1.PICHUnits
			if u == 0 {
				pichB = l1.EncodePICH(1, 1, SynthDirectB)
			}
			data := randBits(r, 256)
			c.Voice[string(data)] = true
			sw := l1.DSW{OK: true, Mode: l1.ModeDirect, Voice: true, First: u == 0, N: map[bool]int{true: 9, false: 10}[u == 0]}
			place(l1.BuildDirect(sw, data, pichB[u], l1.InitAllOnes), f, 2, -300, 0.6)
		}
	}
	c.IQ = make([]complex64, total)
	for i, v := range sig {
		c.IQ[i] = complex64(v + complex(r.NormFloat64()*0.02, r.NormFloat64()*0.02))
	}
	return c
}
