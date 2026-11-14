package l1

const (
	FrameSymbols = 192
	FrameBits = 2 * FrameSymbols
	FSWSymbols = 10
	BodySymbols = FrameSymbols - FSWSymbols
	BodyBits = 2 * BodySymbols
	RICHBits = 16
	SACCHBits = 60
	TCHBits   = 144
	DataBits  = 348
	FrameSec = 0.08
)

var FSW = [FSWSymbols]int{-3, 1, -3, 3, -3, -3, 3, 3, -1, 3}

const FSWHex = "CDF59"

func DibitBits(s float32) (hi, lo float32) {
	a := s
	if a < 0 {
		a = -a
	}
	return s, 2 - a
}

func SoftBits(sym []float32) []float32 {
	out := make([]float32, 2*len(sym))
	for i, s := range sym {
		out[2*i], out[2*i+1] = DibitBits(s)
	}
	return out
}

func Hard(soft []float32) []uint8 {
	out := make([]uint8, len(soft))
	for i, v := range soft {
		if v < 0 {
			out[i] = 1
		}
	}
	return out
}

func Whitening(init, n int) []uint8 {
	reg := uint32(init) & 0x1FF
	out := make([]uint8, n)
	for k := range out {
		out[k] = uint8(reg & 1)
		fb := (reg ^ (reg >> 4)) & 1
		reg = (reg >> 1) | (fb << 8)
	}
	return out
}

const WhiteningInits = 512

func Dewhiten(body []float32, init int) []float32 {
	pn := Whitening(init, len(body))
	out := make([]float32, len(body))
	for i, s := range body {
		if pn[i] == 1 {
			s = -s
		}
		out[i] = s
	}
	return out
}
