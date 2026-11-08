package agc

import (
	"math"
	"sync"
)

const SampleRate = 8000

type AGC struct {
	TargetDB float64
	GateDB float64
	MinDB, MaxDB float64
	AttackSec, ReleaseSec float64
	Ceiling float64
	OnsetGapSec, OnsetSec, OnsetTauSec float64

	mu    sync.Mutex
	gain  float64
	quiet int
	fast  int
}

const blockLen = SampleRate / 50

func New() *AGC {
	a := &AGC{TargetDB: -18, GateDB: -45, MinDB: -12, MaxDB: 18, AttackSec: 0.05, ReleaseSec: 1.5, Ceiling: 30000,
		OnsetGapSec: 0.3, OnsetSec: 0.4, OnsetTauSec: 0.05, gain: 1}
	a.quiet = a.gapBlocks()
	return a
}

func NewQuiet() *AGC {
	a := New()
	a.GateDB, a.MaxDB = -70, 42
	return a
}

func (a *AGC) MarkOnset() {
	if a == nil {
		return
	}
	a.mu.Lock()
	a.quiet = a.gapBlocks()
	a.mu.Unlock()
}

func (a *AGC) gapBlocks() int { return int(math.Ceil(a.OnsetGapSec * SampleRate / blockLen)) }

func (a *AGC) Gain() float64 {
	a.mu.Lock()
	defer a.mu.Unlock()
	return 20 * math.Log10(a.gain)
}

func (a *AGC) Process(pcm []int16) []int16 {
	if a == nil || len(pcm) == 0 {
		return pcm
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	out := make([]int16, len(pcm))
	for i := 0; i < len(pcm); i += blockLen {
		j := min(i+blockLen, len(pcm))
		a.block(pcm[i:j], out[i:j])
	}
	return out
}

func (a *AGC) block(in, out []int16) {
	var sum, peak float64
	for _, v := range in {
		f := float64(v)
		sum += f * f
		if f < 0 {
			f = -f
		}
		peak = max(peak, f)
	}
	g0 := a.gain
	g1 := g0
	speech := false
	if n := len(in); n > 0 && sum > 0 {
		rmsDB := 10*math.Log10(sum/float64(n)) - 20*math.Log10(32768)
		if rmsDB >= a.GateDB {
			speech = true
			if a.OnsetSec > 0 && a.quiet >= a.gapBlocks() {
				a.fast = int(math.Ceil(a.OnsetSec * SampleRate / blockLen))
			}
			want := math.Pow(10, clamp(a.TargetDB-rmsDB, a.MinDB, a.MaxDB)/20)
			tc := a.ReleaseSec
			if want < g0 {
				tc = a.AttackSec
			}
			if a.fast > 0 {
				tc = math.Min(tc, a.OnsetTauSec)
				a.fast--
			}
			k := 1 - math.Exp(-float64(n)/SampleRate/tc)
			g1 = g0 + (want-g0)*k
		}
	}
	if speech {
		a.quiet = 0
	} else if a.quiet < 1<<30 {
		a.quiet++
	}
	if peak > 0 {
		if lim := a.Ceiling / peak; g1 > lim {
			g1 = lim
		}
		if lim := a.Ceiling / peak; g0 > lim {
			g0 = lim
		}
	}
	n := float64(len(in))
	for i, v := range in {
		g := g0 + (g1-g0)*float64(i+1)/n
		out[i] = sat(float64(v) * g)
	}
	a.gain = g1
}

func clamp(x, lo, hi float64) float64 { return math.Max(lo, math.Min(hi, x)) }

func sat(f float64) int16 {
	switch {
	case f > 32767:
		return 32767
	case f < -32768:
		return -32768
	}
	return int16(math.Round(f))
}
