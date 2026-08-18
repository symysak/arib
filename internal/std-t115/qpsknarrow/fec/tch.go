package fec

const (
	TCHHeaderBits = 24
	TCHVoiceBits  = 480
	TCHInfoBits   = TCHHeaderBits + TCHVoiceBits
	TCHTurboIn    = TCHInfoBits + 16
	TCHTurboOut   = 3*TCHTurboIn + 12
	TCHCodedBits  = 804
)

func TCHTxMap() []int {
	x := TCHTurboOut / 3
	dn := TCHTurboOut - TCHCodedBits
	dn2 := dn / 2
	dn3 := dn - dn2
	keep2 := PuncturePattern(x, x, 2*x, 2*dn2)
	keep3 := PuncturePattern(x, x, 1*x, 1*dn3)
	in2 := make([]bool, x)
	for _, k := range keep2 {
		in2[k] = true
	}
	in3 := make([]bool, x)
	for _, k := range keep3 {
		in3[k] = true
	}
	out := make([]int, 0, TCHCodedBits)
	for k := 0; k < x; k++ {
		out = append(out, 3*k)
		if in2[k] {
			out = append(out, 3*k+1)
		}
		if in3[k] {
			out = append(out, 3*k+2)
		}
	}
	return out
}

func TCHExpandLLR(llr []float64, txMap []int) []float64 {
	out := make([]float64, TCHTurboOut)
	for i, src := range txMap {
		if i < len(llr) {
			out[src] = llr[i]
		}
	}
	return out
}
