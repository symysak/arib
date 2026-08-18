package fec

import "math"


type ScrambleFinder struct {
	masks []uint16
	pairs [][2]int
	acc []float64
	frames int
	totalW float64
}

func NewScrambleFinder() *ScrambleFinder {
	f := &ScrambleFinder{acc: make([]float64, 1<<16)}
	f.pairs = cchRepetitionPairs()
	basis := scrambleBasis()
	f.masks = make([]uint16, len(f.pairs))
	for k, p := range f.pairs {
		var m uint16
		for b := 0; b < 16; b++ {
			if basis[b][p[0]] != basis[b][p[1]] {
				m |= 1 << uint(15-b)
			}
		}
		f.masks[k] = m
	}
	return f
}

func cchRepetitionPairs() [][2]int {
	src := CCHPattern()
	first := make([]int, CCHTurboOut)
	for i := range first {
		first[i] = -1
	}
	var out [][2]int
	for pos, in := range src {
		if first[in] < 0 {
			first[in] = pos
			continue
		}
		out = append(out, [2]int{first[in], pos})
	}
	return out
}

func scrambleBasis() [16][]uint8 {
	var b [16][]uint8
	for i := 0; i < 16; i++ {
		b[i] = Scramble(1<<uint(15-i), CCHCodedBits)
	}
	return b
}

func (f *ScrambleFinder) NumEquations() int { return len(f.pairs) }

func (f *ScrambleFinder) Frames() int { return f.frames }

func (f *ScrambleFinder) Reset() {
	for i := range f.acc {
		f.acc[i] = 0
	}
	f.frames = 0
	f.totalW = 0
}

func (f *ScrambleFinder) Add(llr []float64) bool {
	if len(llr) != CCHCodedBits {
		return false
	}
	for k, p := range f.pairs {
		a, b := llr[p[0]], llr[p[1]]
		w := math.Min(math.Abs(a), math.Abs(b))
		if (a < 0) != (b < 0) {
			w = -w
		}
		f.acc[f.masks[k]] += w
		f.totalW += math.Abs(w)
	}
	f.frames++
	return true
}

type Result struct {
	Init int
	Score float64
	Confidence float64
	Prominence float64
	Frames int
}

func (f *ScrambleFinder) Best() Result {
	if f.frames == 0 {
		return Result{}
	}
	sc := make([]float64, len(f.acc))
	copy(sc, f.acc)
	walshHadamard(sc)

	best, second, bestIdx := math.Inf(-1), math.Inf(-1), 0
	for s := 1; s < len(sc); s++ {
		v := sc[s]
		if v > best {
			second, best, bestIdx = best, v, s
		} else if v > second {
			second = v
		}
	}
	prom := math.Inf(1)
	if second > 0 {
		prom = best / second
	}
	conf := 0.0
	if f.totalW > 0 {
		conf = best / f.totalW
	}
	return Result{Init: bestIdx, Score: best, Confidence: conf,
		Prominence: prom, Frames: f.frames}
}

func walshHadamard(x []float64) {
	for length := 1; length < len(x); length <<= 1 {
		for i := 0; i < len(x); i += length << 1 {
			for j := i; j < i+length; j++ {
				a, b := x[j], x[j+length]
				x[j], x[j+length] = a+b, a-b
			}
		}
	}
}
