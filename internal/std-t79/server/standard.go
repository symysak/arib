package server

import (
	"github.com/symysak/arib/internal/std-t79/l1"
	"io/fs"
	"math"
	"net/http"
	"sort"

	"github.com/symysak/arib/internal/rx"
	"github.com/symysak/arib/internal/std-t79/dsp"
	"github.com/symysak/arib/internal/std-t79/pane"
	"github.com/symysak/arib/internal/voice"
)

const System = "std-t79"

const GridHz = 25000.0

const RasterHz = 12500.0

type Standard struct {
	cfg   StdConfig
	hooks *stdHooks
	audio func(idx int, pcm []int16, rate int)
	hints *hintBoard
	nets *netBoard
	echoes *echoBoard
	ledgerOrgs func(mhz float64) []string
	*stdShared
}

type StdConfig struct {
	LogDir string
	NoTCH bool
	Codec    string
	CodecOpt voice.Options
	Lossless bool
	DirectScan bool
	IdleTCH bool
}

type stdHooks struct {
	opened func(*Channel)
	closed func(c *Channel, dropped bool)
	assigned func(mhz float64)
	direct func(mhz float64, scramble int, mode l1.DirectMode)
	toDirect func(mhz float64)
	downPaired func(mhz float64) bool
}

func NewStandard(cfg StdConfig) *Standard {
	s := &Standard{cfg: cfg, hints: &hintBoard{}, nets: &netBoard{}, echoes: &echoBoard{}, stdShared: &stdShared{}}
	s.selected.Store(-1)
	return s
}

func (*Standard) Name() string { return "STD-T79" }

func (*Standard) Grid() float64 { return GridHz }

func (s *Standard) NewChannel(c rx.ChannelConfig, sink rx.Sink) rx.Channel {
	ch := newChannel(s, c, sink)
	if s.hooks != nil && s.hooks.opened != nil {
		s.hooks.opened(ch)
	}
	return ch
}

func (s *Standard) NewDetector(c rx.DetectorConfig) rx.Detector {
	if (IsUplinkMHz(c.CenterHz/1e6) && !s.cfg.DirectScan) || c.FsIn < 3*GridHz {
		return nil
	}
	return newScanner(c.FsIn, c.SNRdB)
}

func (*Standard) Routes(*http.ServeMux, func(int) rx.Channel) {}

func (*Standard) Pane() fs.FS { return pane.FS() }


const (
	scanFFT           = 4096
	scanFramesPerFeed = 8
	scanAvg           = 0.25
	scanEdge          = 0.45
	scanDup           = 0.7
	scanSNRdB         = 6
)

type scanner struct {
	fs    float64
	snrDB float64
	acc   []float64
	have  bool
}

func newScanner(fs, snr float64) *scanner {
	if snr <= 0 {
		snr = scanSNRdB
	}
	return &scanner{fs: fs, snrDB: snr, acc: make([]float64, scanFFT)}
}

func (s *scanner) Edge() float64 { return s.fs * scanEdge }

func (s *scanner) Feed(iq []complex64) {
	need := scanFFT * (scanFramesPerFeed + 1) / 2
	if len(iq) < scanFFT {
		return
	}
	if len(iq) > need {
		iq = iq[:need]
	}
	p := dsp.Spectrum(iq, scanFFT)
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

func (s *scanner) Candidates() []float64 {
	if !s.have {
		return nil
	}
	binHz := s.fs / scanFFT
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
	win := max(1, int(math.Floor((GridHz-occHz)/binHz))-coreHalf)
	type pk struct{ off, p float64 }
	var peaks []pk
	edge := s.Edge()
	for i := 0; i < scanFFT; i++ {
		k := i
		if k > scanFFT/2 {
			k -= scanFFT
		}
		off := float64(k) * binHz
		if math.Abs(off) > edge || full[i] < thr {
			continue
		}
		local := true
		for j := -win; j <= win && local; j++ {
			if j != 0 && core[((i+j)%scanFFT+scanFFT)%scanFFT] > core[i] {
				local = false
			}
		}
		if local {
			peaks = append(peaks, pk{off, full[i]})
		}
	}
	sort.Slice(peaks, func(a, b int) bool { return peaks[a].p > peaks[b].p })
	out := []float64{}
	for _, p := range peaks {
		dup := false
		for _, q := range out {
			if math.Abs(p.off-q) < scanDup*GridHz {
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

func (s *scanner) smooth(half int) []float64 {
	sm := make([]float64, scanFFT)
	for i := range sm {
		var sum float64
		for k := -half; k <= half; k++ {
			sum += s.acc[((i+k)%scanFFT+scanFFT)%scanFFT]
		}
		sm[i] = sum / float64(2*half+1)
	}
	return sm
}
