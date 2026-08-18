package fec


func RepetitionPattern(nIn, nOut int) []int {
	if nOut < nIn {
		panic("t115/fec: RepetitionPattern は nOut >= nIn 専用")
	}
	dN := nOut - nIn
	const a = 2
	ePlus := a * nIn
	eMinus := a * dN
	e := 1
	src := make([]int, 0, nOut)
	for m := 0; m < nIn; m++ {
		e -= eMinus
		src = append(src, m)
		for e <= 0 {
			src = append(src, m)
			e += ePlus
		}
	}
	return src
}

func PuncturePattern(nIn, eIni, ePlus, eMinus int) []int {
	e := eIni
	keep := make([]int, 0, nIn)
	for m := 0; m < nIn; m++ {
		e -= eMinus
		if e <= 0 {
			e += ePlus
			continue
		}
		keep = append(keep, m)
	}
	return keep
}

func ApplyPattern(bits []uint8, src []int) []uint8 {
	out := make([]uint8, len(src))
	for i, s := range src {
		out[i] = bits[s]
	}
	return out
}

func CollapseLLR(llr []float64, src []int, nIn int) []float64 {
	out := make([]float64, nIn)
	for i, s := range src {
		out[s] += llr[i]
	}
	return out
}

func ExpandPuncturedLLR(llr []float64, keep []int, nIn int) []float64 {
	out := make([]float64, nIn)
	for i, k := range keep {
		out[k] = llr[i]
	}
	return out
}

const (
	CCHInfoBits  = 96
	CCHTurboIn   = 112
	CCHTurboOut  = 348
	CCHCodedBits = 612
)

func CCHPattern() []int { return RepetitionPattern(CCHTurboOut, CCHCodedBits) }
