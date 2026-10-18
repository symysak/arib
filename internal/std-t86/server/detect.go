package server

import (
	"math"
	"sort"

	"github.com/symysak/arib/internal/std-t86/dsp"
)

type detector struct {
	fs    float64
	snrDB float64
	acc   []float64
	have  bool
	buf   []complex128
	win   []float64
}

const (
	gridHz = 15000.0
	detFFT = 4096
	detFramesPerFeed = 8
	detAvg = 0.15
	detEdge = 0.45
	detDupGrids = 0.7
	detSNRdB   = 6.0
	detRollOff = 0.5
)

func newDetector(fs, snrDB float64) *detector {
	if snrDB <= 0 {
		snrDB = detSNRdB
	}
	w := make([]float64, detFFT)
	for i := range w {
		w[i] = 0.5 - 0.5*math.Cos(2*math.Pi*float64(i)/float64(detFFT))
	}
	return &detector{fs: fs, snrDB: snrDB, acc: make([]float64, detFFT), buf: make([]complex128, detFFT), win: w}
}

func (d *detector) Edge() float64 { return d.fs * detEdge }

func (d *detector) Feed(iq []complex64) {
	if len(iq) < detFFT {
		return
	}
	n := min(detFramesPerFeed, len(iq)/detFFT)
	step := len(iq) / n
	p := make([]float64, detFFT)
	for f := 0; f < n; f++ {
		seg := iq[f*step : f*step+detFFT]
		for i, v := range seg {
			d.buf[i] = complex(float64(real(v))*d.win[i], float64(imag(v))*d.win[i])
		}
		dsp.FFT(d.buf)
		for i, v := range d.buf {
			p[i] += real(v)*real(v) + imag(v)*imag(v)
		}
	}
	for i := range p {
		p[i] /= float64(n)
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

func (d *detector) Candidates() []float64 {
	if !d.have {
		return nil
	}
	binHz := d.fs / detFFT
	occHalf := (1 + detRollOff) * dsp.SymbolRate / 2
	coreHalf := max(1, int(math.Round((1-detRollOff)*dsp.SymbolRate/2/binHz)))
	full := d.smooth(max(1, int(math.Round(occHalf/binHz))))
	core := d.smooth(coreHalf)
	fine := d.smooth(max(1, int(math.Round(250/binHz))))
	sorted := append([]float64(nil), full...)
	sort.Float64s(sorted)
	floor := sorted[len(sorted)/2]
	if floor <= 0 {
		return nil
	}
	thr := floor * math.Pow(10, d.snrDB/10)
	win := max(1, int(math.Floor((gridHz-occHalf)/binHz))-coreHalf)
	at := func(i int) float64 { return core[((i%detFFT)+detFFT)%detFFT] }
	freqOf := func(i float64) float64 {
		if i > detFFT/2 {
			i -= detFFT
		}
		return i * binHz
	}
	type pk struct{ off, p float64 }
	var peaks []pk
	edge := d.fs * detEdge
	for i := 0; i < detFFT; i++ {
		if math.Abs(freqOf(float64(i))) > edge || full[i] < thr {
			continue
		}
		local := true
		for k := -win; k <= win && local; k++ {
			if k != 0 && at(i+k) > core[i] {
				local = false
			}
		}
		if !local {
			continue
		}
		pos := float64(i)
		if l, r, ok := halfEdges(fine, i, coreHalf, floor, binHz); ok {
			pos = (l + r) / 2
		}
		if pos < 0 {
			pos += detFFT
		}
		peaks = append(peaks, pk{freqOf(pos), full[i]})
	}
	sort.Slice(peaks, func(a, b int) bool { return peaks[a].p > peaks[b].p })
	out := make([]float64, 0, len(peaks))
	for _, p := range peaks {
		dup := false
		for _, q := range out {
			if math.Abs(p.off-q) < detDupGrids*gridHz {
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

func halfEdges(sp []float64, i, coreHalf int, floor, binHz float64) (float64, float64, bool) {
	at := func(k int) float64 { return sp[((k%detFFT)+detFFT)%detFFT] }
	core := make([]float64, 0, 2*coreHalf+1)
	for k := -coreHalf; k <= coreHalf; k++ {
		core = append(core, at(i+k))
	}
	sort.Float64s(core)
	half := floor + (core[len(core)/2]-floor)/4
	maxOut := int(math.Ceil((1+detRollOff)*dsp.SymbolRate/binHz)) + 2
	edge := func(dir int) (float64, bool) {
		for k := 1; k <= maxOut; k++ {
			a, b := at(i+dir*(k-1)), at(i+dir*k)
			if b < half && a >= half {
				return float64(i) + float64(dir)*(float64(k-1)+(a-half)/(a-b)), true
			}
		}
		return 0, false
	}
	l, okL := edge(-1)
	r, okR := edge(1)
	if !okL || !okR {
		return 0, 0, false
	}
	if w := (r - l) * binHz; w < 0.6*dsp.SymbolRate || w > (1+detRollOff)*dsp.SymbolRate {
		return 0, 0, false
	}
	return l, r, true
}

func (d *detector) smooth(half int) []float64 {
	sm := make([]float64, detFFT)
	var sum float64
	for k := -half; k <= half; k++ {
		sum += d.acc[((k%detFFT)+detFFT)%detFFT]
	}
	n := float64(2*half + 1)
	for i := 0; i < detFFT; i++ {
		sm[i] = sum / n
		sum += d.acc[(i+half+1)%detFFT] - d.acc[((i-half)%detFFT+detFFT)%detFFT]
	}
	return sm
}
