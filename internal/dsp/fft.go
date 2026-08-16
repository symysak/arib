package dsp

import "math"

func FFT(x []complex128) {
	n := len(x)
	if n&(n-1) != 0 {
		panic("FFT: 長さが 2 の冪ではありません")
	}
	for i, j := 1, 0; i < n; i++ {
		bit := n >> 1
		for ; j&bit != 0; bit >>= 1 {
			j ^= bit
		}
		j |= bit
		if i < j {
			x[i], x[j] = x[j], x[i]
		}
	}
	for length := 2; length <= n; length <<= 1 {
		ang := -2 * math.Pi / float64(length)
		wl := complex(math.Cos(ang), math.Sin(ang))
		for i := 0; i < n; i += length {
			w := complex(1, 0)
			for k := 0; k < length/2; k++ {
				u := x[i+k]
				v := x[i+k+length/2] * w
				x[i+k] = u + v
				x[i+k+length/2] = u - v
				w *= wl
			}
		}
	}
}

func NextPow2(v int) int {
	n := 1
	for n < v {
		n <<= 1
	}
	return n
}

func FFTShiftedMagnitude(x []complex128, nfft int) []float64 {
	buf := make([]complex128, nfft)
	copy(buf, x)
	FFT(buf)
	out := make([]float64, nfft)
	half := nfft / 2
	for i := 0; i < nfft; i++ {
		out[i] = cabs(buf[(i+half)%nfft])
	}
	return out
}

func FFTShiftedFreq(nfft int, fs float64) []float64 {
	out := make([]float64, nfft)
	half := nfft / 2
	for i := 0; i < nfft; i++ {
		k := (i + half) % nfft
		if k >= half {
			k -= nfft
		}
		out[i] = float64(k) * fs / float64(nfft)
	}
	return out
}

func cabs(z complex128) float64 {
	re, im := real(z), imag(z)
	return math.Sqrt(re*re + im*im)
}

func median(v []float64) float64 {
	if len(v) == 0 {
		return 0
	}
	s := make([]float64, len(v))
	copy(s, v)
	quickSelectSort(s)
	n := len(s)
	if n%2 == 1 {
		return s[n/2]
	}
	return 0.5 * (s[n/2-1] + s[n/2])
}

func quickSelectSort(s []float64) {
	insertionThreshold := 12
	var sortRange func(lo, hi int)
	sortRange = func(lo, hi int) {
		if hi-lo < insertionThreshold {
			for i := lo + 1; i < hi; i++ {
				for j := i; j > lo && s[j] < s[j-1]; j-- {
					s[j], s[j-1] = s[j-1], s[j]
				}
			}
			return
		}
		pivot := s[(lo+hi)/2]
		i, j := lo, hi-1
		for i <= j {
			for s[i] < pivot {
				i++
			}
			for s[j] > pivot {
				j--
			}
			if i <= j {
				s[i], s[j] = s[j], s[i]
				i++
				j--
			}
		}
		sortRange(lo, j+1)
		sortRange(i, hi)
	}
	sortRange(0, len(s))
}

func argmax(v []float64) int {
	best := 0
	for i := 1; i < len(v); i++ {
		if v[i] > v[best] {
			best = i
		}
	}
	return best
}
