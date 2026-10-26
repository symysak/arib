package watch

import (
	"math"
	"sort"

	"github.com/symysak/arib/internal/std-t61/dsp"
)

type scanner struct {
	fs       float64
	centerHz float64
	snrDB    float64

	n    int
	acc  []float64
	have bool
}

const (
	scanFFT = 4096
	scanMaxBinHz = 300.0
	scanSnapHz = 1500.0
	scanGuardHz = 600.0
	scanFramesPerFeed = 8
	scanAvg = 0.25
	scanEdge = 0.45
	scanSlotHz = 6250.0
	scanDupSlots = 0.7
)

const scanSNRdB = 6

func newScanner(fs, centerHz, snrDB float64) *scanner {
	if snrDB <= 0 {
		snrDB = scanSNRdB
	}
	n := scanFFTFor(fs)
	return &scanner{fs: fs, centerHz: centerHz, snrDB: snrDB, n: n, acc: make([]float64, n)}
}

func scanFFTFor(fs float64) int {
	n := scanFFT
	for fs/float64(n) > scanMaxBinHz && n < 32768 {
		n *= 2
	}
	return n
}

func (s *scanner) Feed(iq []complex64)   { s.feed(iq) }
func (s *scanner) Candidates() []float64 { return s.candidates() }
func (s *scanner) Edge() float64         { return s.fs * scanEdge }

func (s *scanner) feed(iq []complex64) {
	need := s.n * (scanFramesPerFeed + 1) / 2
	if len(iq) < s.n {
		return
	}
	if len(iq) > need {
		iq = iq[:need]
	}
	p := dsp.Spectrum(iq, s.n)
	if p == nil {
		return
	}
	if !s.have {
		copy(s.acc, p)
		s.have = true
		return
	}
	for i, v := range p {
		s.acc[i] = s.acc[i]*(1-scanAvg) + v*scanAvg
	}
}

func (s *scanner) candidates() []float64 {
	if !s.have {
		return nil
	}
	binHz := s.fs / float64(s.n)
	occHz := (1 + dsp.RollOff) * dsp.SymbolRate / 2
	coreHalf := max(1, int(math.Round((1-dsp.RollOff)*dsp.SymbolRate/2/binHz)))
	full := s.smooth(max(1, int(math.Round(occHz/binHz))))
	core := s.smooth(coreHalf)
	sorted := append([]float64(nil), full...)
	sort.Float64s(sorted)
	floor := sorted[len(sorted)/2]
	if floor <= 0 {
		return nil
	}
	thr := floor * math.Pow(10, s.snrDB/10)
	win := max(1, int(math.Floor((scanSlotHz-occHz-scanGuardHz)/binHz))-coreHalf)
	freqOf := func(i int) float64 {
		if i > s.n/2 {
			i -= s.n
		}
		return float64(i) * binHz
	}
	type pk struct {
		off float64
		p   float64
	}
	var peaks []pk
	edge := s.fs * scanEdge
	for i := 0; i < s.n; i++ {
		off := freqOf(i)
		if math.Abs(off) > edge {
			continue
		}
		v := full[i]
		if v < thr {
			continue
		}
		local := true
		for k := -win; k <= win && local; k++ {
			if k == 0 {
				continue
			}
			if core[((i+k)%s.n+s.n)%s.n] > core[i] {
				local = false
			}
		}
		if local {
			peaks = append(peaks, pk{s.snap(off), v})
		}
	}
	sort.Slice(peaks, func(a, b int) bool { return peaks[a].p > peaks[b].p })
	out := make([]float64, 0, len(peaks))
	for _, p := range peaks {
		dup := false
		for _, q := range out {
			if math.Abs(p.off-q) < scanDupSlots*scanSlotHz {
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

func (s *scanner) snap(off float64) float64 {
	abs := s.centerHz + off
	g := math.Round(abs/scanSlotHz) * scanSlotHz
	if math.Abs(abs-g) <= scanSnapHz {
		return g - s.centerHz
	}
	return off
}

func (s *scanner) smooth(half int) []float64 {
	sm := make([]float64, s.n)
	for i := range sm {
		var sum float64
		for k := -half; k <= half; k++ {
			sum += s.acc[((i+k)%s.n+s.n)%s.n]
		}
		sm[i] = sum / float64(2*half+1)
	}
	return sm
}

const (
	scanLockRatePct   = 8
	scanLockMinFrames = 25
	scanLockMinOK     = 3
)

func scanLockedNow(frames, richOK int) bool {
	return frames >= scanLockMinFrames && richOK >= scanLockMinOK &&
		richOK*100 >= frames*scanLockRatePct
}
