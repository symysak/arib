package decoder

import (
	"math"

	"github.com/symysak/arib/internal/nxdn/dsp"
	"github.com/symysak/arib/internal/nxdn/l1"
)

const (
	sps = dsp.SamplesPerSymbol
	frameLen = l1.FrameSymbols * sps
	trackWin = 2 * sps
	searchWin = frameLen / 2
	acqMinR = 0.80
	acqMaxErr = 1
	confirmMaxErr = 2
	soloR = 0.95
	trackMaxErr = 3
	maxInertia = 2
	minDev = 80.0
	maxDev = 900.0
)

var swNorm = func() float64 {
	var s float64
	for _, v := range l1.FSW {
		s += float64(v * v)
	}
	return math.Sqrt(s)
}()

func corrAt(d []float32, p int) float64 {
	var c, s1, s2 float64
	for k, w := range l1.FSW {
		v := float64(d[p+k*sps])
		c += float64(w) * v
		s1 += v
		s2 += v * v
	}
	e := s2 - s1*s1/float64(len(l1.FSW))
	if e <= 0 {
		return 0
	}
	return c / (swNorm * math.Sqrt(e))
}

func fit(d []float32, p float64) (a, b float64) {
	var sv, ss, sum float64
	for k, w := range l1.FSW {
		v := interp(d, p+float64(k*sps))
		sv += float64(w) * v
		ss += float64(w * w)
		sum += v
	}
	return sv / ss, sum / float64(len(l1.FSW))
}

func fswErrors(d []float32, p, a, b float64) int {
	n := 0
	for k, w := range l1.FSW {
		if Slice((interp(d, p+float64(k*sps))-b)/a) != w {
			n++
		}
	}
	return n
}

func Slice(v float64) int {
	switch {
	case v >= 2:
		return 3
	case v >= 0:
		return 1
	case v > -2:
		return -1
	}
	return -3
}

func interp(d []float32, p float64) float64 {
	i := int(math.Floor(p))
	if i < 0 {
		return float64(d[0])
	}
	if i+1 >= len(d) {
		return float64(d[len(d)-1])
	}
	f := p - float64(i)
	return float64(d[i])*(1-f) + float64(d[i+1])*f
}

func peak(d []float32, lo, hi int) (best int, frac, r float64) {
	r = -2
	for p := lo; p <= hi; p++ {
		if c := corrAt(d, p); c > r {
			best, r = p, c
		}
	}
	frac = float64(best)
	if best > lo && best < hi {
		y0, y1, y2 := corrAt(d, best-1), r, corrAt(d, best+1)
		if den := y0 - 2*y1 + y2; den < 0 {
			frac += 0.5 * (y0 - y2) / den
		}
	}
	return
}
