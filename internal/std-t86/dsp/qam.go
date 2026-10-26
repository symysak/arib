package dsp

import "math"


var Norm = 1.0 / math.Sqrt(10.0)

var Levels = [4]float64{-3 * (1.0 / math.Sqrt(10.0)), -1 * (1.0 / math.Sqrt(10.0)),
	1 * (1.0 / math.Sqrt(10.0)), 3 * (1.0 / math.Sqrt(10.0))}

var pairToLevel = [4]float64{3 * (1.0 / math.Sqrt(10.0)), 1 * (1.0 / math.Sqrt(10.0)),
	-3 * (1.0 / math.Sqrt(10.0)), -1 * (1.0 / math.Sqrt(10.0))}

var levelIdxToPair = [4][2]uint8{{1, 0}, {1, 1}, {0, 1}, {0, 0}}

func BitsToSymbols(bits []uint8) []complex64 {
	if len(bits)%4 != 0 {
		panic("16QAM は 4 bit/シンボルなので長さは 4 の倍数が必要です")
	}
	out := make([]complex64, len(bits)/4)
	for i := range out {
		q := bits[4*i : 4*i+4]
		iv := pairToLevel[q[0]*2+q[1]]
		qv := pairToLevel[q[2]*2+q[3]]
		out[i] = complex(float32(iv), float32(qv))
	}
	return out
}

func nearestLevel(v float64) int {
	best, bd := 0, math.Abs(v-Levels[0])
	for k := 1; k < 4; k++ {
		if d := math.Abs(v - Levels[k]); d < bd {
			best, bd = k, d
		}
	}
	return best
}

func SymbolsToBits(syms []complex64) []uint8 {
	out := make([]uint8, len(syms)*4)
	for i, s := range syms {
		pi := levelIdxToPair[nearestLevel(float64(real(s)))]
		pq := levelIdxToPair[nearestLevel(float64(imag(s)))]
		out[4*i], out[4*i+1] = pi[0], pi[1]
		out[4*i+2], out[4*i+3] = pq[0], pq[1]
	}
	return out
}

func SymbolsToBitsThreshold(syms []complex64, thr *[2]float64) []uint8 {
	var tr, tq float64
	if thr != nil {
		tr, tq = thr[0], thr[1]
	} else {
		for _, s := range syms {
			tr += math.Abs(float64(real(s)))
			tq += math.Abs(float64(imag(s)))
		}
		if n := float64(len(syms)); n > 0 {
			tr /= n
			tq /= n
		}
	}
	out := make([]uint8, len(syms)*4)
	for i, s := range syms {
		r, q := float64(real(s)), float64(imag(s))
		out[4*i] = b2u(r < 0)
		out[4*i+1] = b2u(math.Abs(r) < tr)
		out[4*i+2] = b2u(q < 0)
		out[4*i+3] = b2u(math.Abs(q) < tq)
	}
	return out
}

func b2u(b bool) uint8 {
	if b {
		return 1
	}
	return 0
}

func SliceSymbols(syms []complex64) []complex64 {
	out := make([]complex64, len(syms))
	for i, s := range syms {
		out[i] = complex(
			float32(Levels[nearestLevel(float64(real(s)))]),
			float32(Levels[nearestLevel(float64(imag(s)))]),
		)
	}
	return out
}

func EVMPercent(syms []complex64) float64 {
	dec := SliceSymbols(syms)
	var err, ref float64
	for i, s := range syms {
		d := dec[i]
		dr := float64(real(s)) - float64(real(d))
		di := float64(imag(s)) - float64(imag(d))
		err += dr*dr + di*di
		ref += float64(real(d))*float64(real(d)) + float64(imag(d))*float64(imag(d))
	}
	n := float64(len(syms))
	if n == 0 {
		return 0
	}
	return math.Sqrt((err/n)/(ref/n+1e-20)) * 100.0
}
