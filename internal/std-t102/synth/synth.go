package synth

import (
	"math"
	"math/cmplx"
	"math/rand"

	"github.com/symysak/arib/internal/std-t102/dsp"
	"github.com/symysak/arib/internal/std-t102/l1"
	"github.com/symysak/arib/internal/std-t102/l3"
	"github.com/symysak/arib/internal/vocoder/ambe"
)

var Preamble = []float32{3, 3, 3, -3, 3, -3, 3, 3, -3, -3, -3, 3}

type VoiceFrame struct {
	M, O, D uint8
	S, U    uint8
	SACCH   [18]uint8
	FACCH1 [2][]uint8
	TCH    [2][]uint8
}

type DataFrame struct {
	O, D uint8
	U    uint8
	Body [22]byte
}

func (v VoiceFrame) Bits() []uint8 {
	out := l1.RICHBitsOf(2, v.M, v.O, v.D)
	out = append(out, l1.SACCH.Encode(l1.SACCHInfo(v.S, v.U, v.SACCH[:]))...)
	for h := 0; h < 2; h++ {
		if v.FACCH1[h] != nil {
			out = append(out, l1.FACCH1.Encode(v.FACCH1[h])...)
		} else {
			t := v.TCH[h]
			if t == nil {
				t = make([]uint8, l1.TCHBits)
			}
			out = append(out, t...)
		}
	}
	return out
}

func (d DataFrame) Bits() []uint8 {
	out := l1.RICHBitsOf(2, 1, d.O, d.D)
	info := append(l1.SACCHInfo(0, d.U, make([]uint8, 18))[:8], l1.BytesToBits(d.Body[:])...)
	return append(out, l1.UDCH1.Encode(info)...)
}

func Symbols(body []uint8, init int) []float32 {
	sym := make([]float32, 0, l1.FrameSymbols)
	for _, v := range l1.FSW {
		sym = append(sym, float32(v))
	}
	b := l1.Symbols(body)
	pn := l1.Whitening(init, len(b))
	for i, s := range b {
		if pn[i] == 1 {
			s = -s
		}
		sym = append(sym, s)
	}
	return sym
}

func Burst(init int, frames ...[]uint8) []float32 {
	out := append([]float32(nil), Preamble...)
	for _, f := range frames {
		out = append(out, Symbols(f, init)...)
	}
	return out
}

func Modulate(sym []float32, fs, offsetHz, amp, silenceSec float64) []complex64 {
	sps := fs / dsp.SymbolRate
	n := int(float64(len(sym))*sps) + 1
	span := 8.0
	freq := make([]float64, n)
	for i := range freq {
		t := float64(i) / sps
		k0 := int(math.Floor(t - span))
		var s float64
		for k := max(k0, 0); k <= int(t+span) && k < len(sym); k++ {
			s += float64(sym[k]) * rrc(t-float64(k)-0.5)
		}
		freq[i] = s * dsp.Dev1
	}
	pad := int(silenceSec * fs)
	out := make([]complex64, pad, 2*pad+n)
	var ph float64
	for i := 0; i < n; i++ {
		ph += 2 * math.Pi * (freq[i] + offsetHz) / fs
		out = append(out, complex64(cmplx.Rect(amp, ph)))
	}
	out = append(out, make([]complex64, pad)...)
	return out
}

func rrc(t float64) float64 {
	a := dsp.RollOff
	switch {
	case math.Abs(t) < 1e-9:
		return 1 - a + 4*a/math.Pi
	case math.Abs(math.Abs(4*a*t)-1) < 1e-9:
		return a / math.Sqrt2 * ((1+2/math.Pi)*math.Sin(math.Pi/(4*a)) + (1-2/math.Pi)*math.Cos(math.Pi/(4*a)))
	}
	return (math.Sin(math.Pi*t*(1-a)) + 4*a*t*math.Cos(math.Pi*t*(1+a))) / (math.Pi * t * (1 - 16*a*a*t*t))
}

func Call(rng *rand.Rand, u uint8, superframes int) (frames [][]uint8, tch [][]uint8) {
	start := VoiceFrame{M: 0, O: 0, U: u, SACCH: idle18()}
	msg := make([]uint8, 80)
	copy(msg, l1.BytesToBits([]byte{l3.TypeVoice, 0x05}))
	start.FACCH1 = [2][]uint8{msg, msg}
	frames = append(frames, start.Bits())
	voiceMsg := l1.BytesToBits([]byte{l3.TypeVoice, 0x05, 1, 2, 3, 4, 5, 6, 7})
	for sf := 0; sf < superframes; sf++ {
		for k := 0; k < 4; k++ {
			v := VoiceFrame{M: 2, O: 3, S: uint8(3 - k), U: u}
			copy(v.SACCH[:], voiceMsg[18*k:18*k+18])
			for h := 0; h < 2; h++ {
				var t []uint8
				for j := 0; j < 2; j++ {
					var rest [25]uint8
					for i := range rest {
						rest[i] = uint8(rng.Intn(2))
					}
					f := ambe.MakeFrame3600(uint16(rng.Intn(4096)), uint16(rng.Intn(4096)), rest)
					t = append(t, f[:]...)
				}
				v.TCH[h] = t
				tch = append(tch, t)
			}
			frames = append(frames, v.Bits())
		}
	}
	end := VoiceFrame{M: 0, O: 0, U: u, SACCH: idle18()}
	em := make([]uint8, 80)
	copy(em, l1.BytesToBits([]byte{l3.TypeEnd, 0x05}))
	end.FACCH1 = [2][]uint8{em, em}
	frames = append(frames, end.Bits())
	return frames, tch
}

func idle18() (s [18]uint8) {
	copy(s[:], l1.BytesToBits([]byte{l3.TypeIdle, 0x05})[:16])
	return
}
