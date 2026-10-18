package decoder

import (
	"math"
	"math/cmplx"
	"math/rand"

	"github.com/symysak/arib/internal/std-t79/dsp"
	"github.com/symysak/arib/internal/std-t79/l1"
)


const (
	SynthUpID = 1<<19 | 777
	SynthUpCC = 21
)

type SynthUpCarrier struct {
	IQ      []complex64
	Fs      float64
	Voice   map[string]bool
	FACCH   []uint8
	SACCH   []uint8
	Message []byte
}

func SynthUp(frames, sps int, r *rand.Rand) SynthUpCarrier {
	fs := dsp.SymbolRate * float64(sps)
	c := SynthUpCarrier{Fs: fs, Voice: map[string]bool{}}
	c.SACCH = randBits(r, 48)
	sacchTx := l1.EncodeSACCH(l1.W{First: true, Last: true, WO: 6}, c.SACCH)
	c.FACCH = randBits(r, 96)
	facchY := l1.EncodeFACCH(l1.W{First: true, Last: true, WO: 12}, c.FACCH)
	rchA, rchB := l1.EncodeRCH(SynthRCHUp)
	c.Message = []byte("UPLINK SCCH 0123")
	init := l1.InitFromPattern(SynthPattern)
	total := (frames + 2) * 4 * 160 * sps
	sig := make([]complex128, total)
	place := func(bits []uint8, frame, slot int, cfo, amp float64) {
		x := dsp.Modulate(bits, sps)
		at := (frame*4 + slot) * 160 * sps
		for i, v := range x {
			if at+i < total {
				sig[at+i] += complex(amp, 0) * complex128(v) * cmplx.Rect(1, 2*math.Pi*cfo*float64(at+i)/fs)
			}
		}
	}
	nv := 0
	for f := 1; f <= frames; f++ {
		k := f - 1
		sf := (f - 5) % l1.FramesPerSuper
		switch {
		case k < 2:
			place(l1.BuildSyncBurstUp(2, l1.SyncBurst{CC: SynthUpCC, B: 0, ID: SynthUpID}), f, 1, 250, 1)
		case k < 4:
			place(l1.BuildSyncBurstUp(2, l1.SyncBurst{CC: SynthUpCC, B: 1, TA: 2, SSC: k, ID: SynthUpID}), f, 1, 250, 1)
		default:
			sw := l1.SW{Kind: l1.SWNormal, N: 2}
			if sf == 0 {
				sw = l1.SW{Kind: l1.SWSuper, N: 6}
			}
			tch := randBits(r, l1.TCHBits)
			if k == 6 || k == 7 {
				l1.PlaceFACCH(tch, facchY, k == 7)
				sw.Inverted = true
			} else {
				c.Voice[string(tch)] = true
				nv++
			}
			sa := make([]uint8, 20)
			switch u, kk := l1.SACCHPart(sf); {
			case u >= 0:
				sa = sacchTx[20*kk : 20*kk+20]
			case sf == 0:
				sa = rchA
			default:
				sa = rchB
			}
			place(l1.BuildTrafficUp(sw, SynthUpCC, tch, sa, init), f, 1, 250, 1)
		}
		if f == 10 || f == 11 {
			msg := append([]byte(nil), c.Message...)
			first := f == 10
			var data []uint8
			var w l1.W
			if first {
				data, w = bytesBits(msg[:7], l1.UpCACFirstBits), l1.W{First: true, WO: 1}
			} else {
				data, w = bytesBits(msg[7:], l1.UpCACNextBits), l1.W{Last: true, WO: len(msg) - 7}
			}
			place(l1.BuildUpCAC(l1.SW{Kind: l1.SWNormal, N: 4}, SynthUpCC, first, w, data), f, 3, -400, 0.5)
		}
	}
	c.IQ = make([]complex64, total)
	for i, v := range sig {
		c.IQ[i] = complex64(v + complex(r.NormFloat64()*0.02, r.NormFloat64()*0.02))
	}
	_ = nv
	return c
}

func randBits(r *rand.Rand, n int) []uint8 {
	b := make([]uint8, n)
	for i := range b {
		b[i] = uint8(r.Intn(2))
	}
	return b
}

func bytesBits(b []byte, n int) []uint8 {
	out := make([]uint8, n)
	for i := 0; i < n && i/8 < len(b); i++ {
		out[i] = b[i/8] >> (7 - i%8) & 1
	}
	return out
}
