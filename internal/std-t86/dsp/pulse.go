package dsp

import "math"

func RRCTaps(beta float64, sps, span int) []float64 {
	half := float64(span*sps) / 2
	n := span*sps + 1
	taps := make([]float64, n)
	energy := 0.0
	for i := 0; i < n; i++ {
		ti := (-half + float64(i)) / float64(sps)
		var v float64
		switch {
		case math.Abs(ti) < 1e-12:
			v = 1.0 - beta + 4.0*beta/math.Pi
		case beta > 0 && math.Abs(math.Abs(4.0*beta*ti)-1.0) < 1e-9:
			v = (beta / math.Sqrt2) *
				((1.0+2.0/math.Pi)*math.Sin(math.Pi/(4.0*beta)) +
					(1.0-2.0/math.Pi)*math.Cos(math.Pi/(4.0*beta)))
		default:
			num := math.Sin(math.Pi*ti*(1.0-beta)) +
				4.0*beta*ti*math.Cos(math.Pi*ti*(1.0+beta))
			den := math.Pi * ti * (1.0 - math.Pow(4.0*beta*ti, 2))
			v = num / den
		}
		taps[i] = v
		energy += v * v
	}
	norm := math.Sqrt(energy)
	for i := range taps {
		taps[i] /= norm
	}
	return taps
}
