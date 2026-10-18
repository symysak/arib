package synth

import (
	"math"
	"math/rand"

	"github.com/symysak/arib/internal/std-t115/qpsknarrow/control"
	"github.com/symysak/arib/internal/std-t115/qpsknarrow/dsp"
	"github.com/symysak/arib/internal/std-t115/qpsknarrow/fec"
)

type Broadcast struct {
	Scramble int
	SubStation int
	CallNo int
	Manufacturer int
	NotifyFrames, VoiceFrames, DisconnectFrames int
	Seed int64
}

func (b Broadcast) Bits() []uint8 {
	rnd := rand.New(rand.NewSource(b.Seed))
	var out []uint8
	for i := 0; i < b.NotifyFrames; i++ {
		timing := min(b.NotifyFrames-1-i, 15)
		out = append(out, b.sb0(timing)...)
	}
	voiceType := control.MsgVoiceGroupIndiv
	if b.SubStation == 0 {
		voiceType = control.MsgVoiceSimultaneous
	}
	for i := 0; i < b.VoiceFrames; i++ {
		voice := make([]uint8, fec.TCHVoiceBits)
		for j := range voice {
			voice[j] = uint8(rnd.Intn(2))
		}
		out = append(out, b.sc(voiceType, uint8(i%2)<<7|control.ChIDTCH<<5, voice)...)
	}
	for i := 0; i < b.DisconnectFrames; i++ {
		out = append(out, b.sc(control.MsgDisconnectFACCH, control.ChIDFACCH<<5, make([]uint8, fec.TCHVoiceBits))...)
	}
	return out
}

func (b Broadcast) sb0(timing int) []uint8 {
	oct := make([]uint8, fec.CCHInfoBits/8)
	oct[0] = control.MsgNotifyStart
	oct[1] = uint8(b.Manufacturer & 0x3F)
	oct[2] = control.ChIDCCH<<5 | 0x10 | uint8(timing&0x0F)
	oct[3] = uint8(b.CallNo & 0x07)
	oct[6] = uint8(b.SubStation >> 8)
	oct[7] = uint8(b.SubStation)
	info := octetsToBits(oct)
	u := append(append([]uint8{}, info...), fec.CRC16Bits(info)...)
	cch := fec.ApplyPattern(fec.TurboEncode(u), fec.CCHPattern())
	pn := fec.Scramble(b.Scramble, fec.CCHCodedBits)
	for i := range cch {
		cch[i] ^= pn[i]
	}
	var f []uint8
	f = append(f, dsp.HexToBits(dsp.Pa1Hex)...)
	f = append(f, cch[:dsp.SB0CCH1Bits]...)
	f = append(f, dsp.HexToBits(dsp.SW1Hex)...)
	f = append(f, cch[dsp.SB0CCH1Bits:]...)
	f = append(f, dsp.HexToBits(dsp.Pb1Hex)...)
	return f
}

func (b Broadcast) sc(typ int, oct3 uint8, body []uint8) []uint8 {
	info := octetsToBits([]uint8{uint8(typ), uint8(b.Manufacturer & 0x3F), oct3})
	info = append(info, body...)
	u := append(append([]uint8{}, info...), fec.CRC16Bits(info)...)
	enc := fec.TurboEncode(u)
	pn := fec.Scramble(b.Scramble, fec.TCHCodedBits)
	tch := make([]uint8, fec.TCHCodedBits)
	for k, src := range fec.TCHTxMap() {
		tch[k] = enc[src] ^ pn[k]
	}
	var f []uint8
	f = append(f, tch[:dsp.SCFirstBits]...)
	f = append(f, dsp.HexToBits(dsp.SW3Hex)...)
	f = append(f, tch[dsp.SCFirstBits:]...)
	return f
}

func octetsToBits(o []uint8) []uint8 {
	out := make([]uint8, 0, 8*len(o))
	for _, v := range o {
		for b := 7; b >= 0; b-- {
			out = append(out, (v>>uint(b))&1)
		}
	}
	return out
}

func Modulate(bits []uint8, fs float64) []complex64 {
	sym := dsp.BitsToSymbols(bits)
	const span = 8
	sps := fs / dsp.SymbolRate
	n := int(math.Ceil(float64(len(sym)) * sps))
	out := make([]complex64, n)
	reach := int(math.Ceil(span * sps))
	for k, s := range sym {
		c := float64(k) * sps
		lo, hi := max(0, int(math.Ceil(c))-reach), min(n-1, int(math.Floor(c))+reach)
		for i := lo; i <= hi; i++ {
			h := rrc((float64(i) - c) / sps)
			out[i] += complex(float32(real(s)*h), float32(imag(s)*h))
		}
	}
	return out
}

func rrc(x float64) float64 {
	const a = dsp.RollOff
	switch {
	case math.Abs(x) < 1e-9:
		return 1 - a + 4*a/math.Pi
	case math.Abs(math.Abs(x)-1/(4*a)) < 1e-9:
		return a / math.Sqrt2 * ((1+2/math.Pi)*math.Sin(math.Pi/(4*a)) + (1-2/math.Pi)*math.Cos(math.Pi/(4*a)))
	}
	return (math.Sin(math.Pi*x*(1-a)) + 4*a*x*math.Cos(math.Pi*x*(1+a))) / (math.Pi * x * (1 - math.Pow(4*a*x, 2)))
}

func Silence(fs, sec float64) []complex64 { return make([]complex64, int(sec*fs)) }

type Station struct {
	OffsetHz, Gain float64
	IQ             []complex64
}

func Mix(fs float64, noise float64, seed int64, st ...Station) []complex64 {
	n := 0
	for _, s := range st {
		n = max(n, len(s.IQ))
	}
	out := make([]complex64, n)
	for _, s := range st {
		for i, v := range s.IQ {
			sn, cs := math.Sincos(2 * math.Pi * s.OffsetHz / fs * float64(i))
			out[i] += v * complex(float32(cs*s.Gain), float32(sn*s.Gain))
		}
	}
	if noise > 0 {
		rnd := rand.New(rand.NewSource(seed))
		for i := range out {
			out[i] += complex(float32(rnd.NormFloat64()*noise), float32(rnd.NormFloat64()*noise))
		}
	}
	return out
}

func Int16(x []complex64) []int16 {
	var peak float64
	for _, v := range x {
		peak = math.Max(peak, math.Max(math.Abs(float64(real(v))), math.Abs(float64(imag(v)))))
	}
	scale := 1.0
	if peak > 0 {
		scale = 30000 / peak
	}
	out := make([]int16, 2*len(x))
	for i, v := range x {
		out[2*i] = int16(math.Round(float64(real(v)) * scale))
		out[2*i+1] = int16(math.Round(float64(imag(v)) * scale))
	}
	return out
}
