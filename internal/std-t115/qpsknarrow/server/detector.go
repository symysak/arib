package server

import (
	"math"
	"sort"

	"github.com/symysak/arib/internal/std-t115/qpsknarrow/dsp"
)

type detector struct {
	fs    float64
	snrDB float64
	n     int
	acc   []float64
	have  bool
}

const (
	chanSpacingHz = 7500.0
	detBinHz = 500.0
	detMaxFFT = 4096
	detMinFFT = 256
	detFramesPerFeed = 8
	detAvg = 0.25
	detEdge = 0.45
	detDupChans = 0.7
	detSNRdB = 6
)

func newDetector(fs, snrDB float64) *detector {
	if snrDB <= 0 {
		snrDB = detSNRdB
	}
	n := dsp.NextPow2(int(fs / detBinHz))
	n = min(max(n, detMinFFT), detMaxFFT)
	return &detector{fs: fs, snrDB: snrDB, n: n, acc: make([]float64, n)}
}

func (d *detector) Feed(iq []complex64) {
	if need := d.n * (detFramesPerFeed + 1) / 2; len(iq) > need {
		iq = iq[:need]
	}
	p := dsp.Spectrum(iq, d.n)
	if p == nil {
		return
	}
	if !d.have {
		copy(d.acc, p)
		d.have = true
		return
	}
	for i, v := range p {
		d.acc[i] = d.acc[i]*(1-detAvg) + v*detAvg
	}
}

func (d *detector) Edge() float64 { return d.fs * detEdge }

func (d *detector) Candidates() []float64 {
	if !d.have {
		return nil
	}
	binHz := d.fs / float64(d.n)
	occHalf := (1 + dsp.RollOff) * dsp.SymbolRate / 2
	coreHalf := max(1, int(math.Round((1-dsp.RollOff)*dsp.SymbolRate/2/binHz)))
	full := d.smooth(max(1, int(math.Round(occHalf/binHz))))
	core := d.smooth(coreHalf)
	sorted := append([]float64(nil), full...)
	sort.Float64s(sorted)
	floor := sorted[len(sorted)/2]
	if floor <= 0 {
		return nil
	}
	thr := floor * math.Pow(10, d.snrDB/10)
	win := max(1, int(math.Floor((chanSpacingHz-occHalf)/binHz))-coreHalf)
	freqOf := func(i int) float64 {
		if i > d.n/2 {
			i -= d.n
		}
		return float64(i) * binHz
	}
	type pk struct{ off, p float64 }
	var peaks []pk
	edge := d.Edge()
	for i := 0; i < d.n; i++ {
		off := freqOf(i)
		if math.Abs(off) > edge || full[i] < thr {
			continue
		}
		local := true
		for k := -win; k <= win && local; k++ {
			if k != 0 && core[((i+k)%d.n+d.n)%d.n] > core[i] {
				local = false
			}
		}
		if local {
			peaks = append(peaks, pk{off, full[i]})
		}
	}
	sort.Slice(peaks, func(a, b int) bool { return peaks[a].p > peaks[b].p })
	out := make([]float64, 0, len(peaks))
	for _, p := range peaks {
		dup := false
		for _, q := range out {
			if math.Abs(p.off-q) < detDupChans*chanSpacingHz {
				dup = true
				break
			}
		}
		if !dup {
			out = append(out, p.off)
		}
	}
	return out
}

func (d *detector) smooth(half int) []float64 {
	sm := make([]float64, d.n)
	for i := range sm {
		var sum float64
		for k := -half; k <= half; k++ {
			sum += d.acc[((i+k)%d.n+d.n)%d.n]
		}
		sm[i] = sum / float64(2*half+1)
	}
	return sm
}
