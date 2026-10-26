package dsp

const (
	SymbolRate = 5625.0

	BitRate = 11250.0

	RollOff = 0.2

	FrameBits    = 900
	FrameSymbols = FrameBits / 2

	FrameDurationSec = 0.080
)

const (
	SB0PaBits   = 96
	SB0CCH1Bits = 306
	SB0SWBits   = 96
	SB0CCH2Bits = 306
	SB0PbBits   = 96
)

const (
	SCFirstBits = 402
	SCSWBits    = 96
	SCLastBits  = 402
)

const (
	SW1Hex = "22E221ED1D1D211EDDEEDDEE"
	SW3Hex = "1EDDD1D2D222E12D2DE111E2"
	SW2Hex = ""
	SW4Hex = ""
)

const (
	Pa1Hex = "112EDDE21E1E2D1E1D2111EE"
	Pb1Hex = "E2D1EED2EDED2D22DD1DE1EE"
	Pa2Hex = "1D111E2D11DED2E112E1E212"
	Pb2Hex = "DE2EDD1D12E1E22DE1221D2E"
)

func HexToBits(s string) []uint8 {
	out := make([]uint8, 0, len(s)*4)
	for _, c := range s {
		var v int
		switch {
		case c >= '0' && c <= '9':
			v = int(c - '0')
		case c >= 'A' && c <= 'F':
			v = int(c-'A') + 10
		case c >= 'a' && c <= 'f':
			v = int(c-'a') + 10
		default:
			return nil
		}
		for k := 3; k >= 0; k-- {
			out = append(out, uint8((v>>uint(k))&1))
		}
	}
	return out
}

func BitsToHex(b []uint8) string {
	const hexd = "0123456789ABCDEF"
	out := make([]byte, 0, len(b)/4)
	for i := 0; i+3 < len(b); i += 4 {
		out = append(out, hexd[b[i]<<3|b[i+1]<<2|b[i+2]<<1|b[i+3]])
	}
	return string(out)
}

func BitsToSymbols(bits []uint8) []complex128 {
	n := len(bits) / 2
	out := make([]complex128, n)
	for i := 0; i < n; i++ {
		re := 1.0 - 2.0*float64(bits[2*i]&1)
		im := 1.0 - 2.0*float64(bits[2*i+1]&1)
		out[i] = complex(re, im)
	}
	return out
}

func SymbolsToBits(sym []complex128) []uint8 {
	out := make([]uint8, 0, len(sym)*2)
	for _, s := range sym {
		var a, b uint8
		if real(s) < 0 {
			a = 1
		}
		if imag(s) < 0 {
			b = 1
		}
		out = append(out, a, b)
	}
	return out
}

func SymbolsToLLR(sym []complex128, scale float64) []float64 {
	out := make([]float64, 0, len(sym)*2)
	for _, s := range sym {
		out = append(out, scale*real(s), scale*imag(s))
	}
	return out
}
