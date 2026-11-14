package synth

import (
	"math"
	"math/cmplx"

	"github.com/symysak/arib/internal/nxdn/dsp"
	"github.com/symysak/arib/internal/nxdn/l1"
)

var Preamble = []float32{3, 3, 3, -3, 3, -3, 3, 3, -3, -3, -3, 3}

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
