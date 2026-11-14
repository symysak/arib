package server

import (
	"fmt"
	"io/fs"
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/symysak/arib/internal/cliflag"
	"github.com/symysak/arib/internal/nxdn/dsp"
	"github.com/symysak/arib/internal/nxdn/l1"
	"github.com/symysak/arib/internal/nxdn/pane"
	"github.com/symysak/arib/internal/rx"
	"github.com/symysak/arib/internal/voice"
)

const GridHz = dsp.ChannelSpacing

type Standard struct {
	spec  Spec
	cfg   StdConfig
	hooks *stdHooks
	audio func(idx int, pcm []int16, rate int)
	selected atomic.Int64
	keys keyBoard
	ledgerCodec func(mhz float64) string
}

func (s *Standard) ledgerCodecOf(mhz float64) string {
	if s.ledgerCodec == nil {
		return ""
	}
	return s.ledgerCodec(mhz)
}

type StdConfig struct {
	LogDir string
	Codec    string
	CodecOpt voice.Options
	Data string
	Init int
	Opts map[int64]ChannelOpt
	Lossless bool
	NoTCH bool
	AudioAGC bool
}

type ChannelOpt struct {
	Init int
}

type stdHooks struct {
	opened func(*Channel)
	closed func(c *Channel, dropped bool)
}

type keyBoard struct {
	mu   sync.Mutex
	seen []int
}

func (k *keyBoard) add(v int) {
	k.mu.Lock()
	defer k.mu.Unlock()
	for _, x := range k.seen {
		if x == v {
			return
		}
	}
	k.seen = append([]int{v}, k.seen...)
}

func (k *keyBoard) list() []int {
	k.mu.Lock()
	defer k.mu.Unlock()
	return append([]int(nil), k.seen...)
}

func NewStandard(spec Spec, cfg StdConfig) *Standard {
	s := &Standard{spec: spec, cfg: cfg}
	s.selected.Store(-1)
	return s
}

func (s *Standard) Name() string { return s.spec.Name }

func (s *Standard) Spec() Spec { return s.spec }

func (s *Standard) inits() []int {
	out := s.keys.list()
	if s.spec.Inits != nil {
		out = append(out, s.spec.Inits()...)
	}
	return out
}

func (s *Standard) defaultInit(init int) bool {
	if s.spec.Init != nil && *s.spec.Init == init {
		return true
	}
	if s.spec.Inits == nil {
		return false
	}
	for _, v := range s.spec.Inits() {
		if v == init {
			return true
		}
	}
	return false
}

func (s *Standard) keyer(fixed int) *l1.Keyer {
	cfg := l1.KeyerConfig{Dialect: s.spec.Dialect, Fixed: fixed, Hints: s.inits(), Candidates: s.spec.Candidates}
	if fixed < 0 && s.spec.Init != nil {
		cfg.Fixed, cfg.FixedWhy = *s.spec.Init, s.spec.InitWhy
	}
	return l1.NewKeyer(cfg)
}

func (*Standard) Grid() float64 { return GridHz }

func (s *Standard) NewChannel(c rx.ChannelConfig, sink rx.Sink) rx.Channel {
	opt, ok := c.Opt.(ChannelOpt)
	if !ok {
		opt, ok = s.cfg.Opts[OptKey(c.MHz)]
	}
	if !ok {
		opt = ChannelOpt{Init: s.cfg.Init}
	}
	ch := newChannel(s, c, sink, opt)
	if s.hooks != nil && s.hooks.opened != nil {
		s.hooks.opened(ch)
	}
	return ch
}

func (*Standard) NewDetector(c rx.DetectorConfig) rx.Detector {
	if c.FsIn < 3*GridHz {
		return nil
	}
	return newScanner(c.FsIn, c.SNRdB)
}

func (*Standard) Routes(*http.ServeMux, func(int) rx.Channel) {}

func (*Standard) Pane() fs.FS { return pane.FS() }

func OptKey(mhz float64) int64 { return int64(math.Round(mhz * 1e4)) }

func ParseInit(s string) (int, error) {
	s = strings.TrimSpace(s)
	v, err := strconv.ParseInt(s, 0, 32)
	if err != nil || v < 0 || v > 0x1FF {
		return 0, fmt.Errorf("ホワイトニングの初期値 %q は 0〜511（16 進なら 0x 付き。PN(9,5) の S8…S0）", s)
	}
	return int(v), nil
}

func OptsFromChannels(list []cliflag.Channel) (map[int64]ChannelOpt, error) {
	if err := cliflag.CheckKeys(list, "init"); err != nil {
		return nil, err
	}
	opts := map[int64]ChannelOpt{}
	for _, c := range list {
		v, ok := c.Opts["init"]
		if !ok {
			continue
		}
		n, err := ParseInit(v)
		if err != nil {
			return nil, fmt.Errorf("-channels の %g MHz: %v", c.MHz, err)
		}
		opts[OptKey(c.MHz)] = ChannelOpt{Init: n}
	}
	return opts, nil
}


const (
	scanAvg     = 0.3
	scanEdge    = 0.45
	scanDup     = 0.7
	scanSNRdB   = 6
	scanMaxBin  = 150.0
	scanOccHz   = 2900.0
	scanCoreHz  = 1200.0
	scanFramesN = 8
)

type scanner struct {
	fs    float64
	n     int
	snrDB float64
	acc   []float64
	have  bool
	last  time.Time
}

func newScanner(fs, snr float64) *scanner {
	if snr <= 0 {
		snr = scanSNRdB
	}
	n := 1024
	for fs/float64(n) > scanMaxBin {
		n *= 2
	}
	return &scanner{fs: fs, n: n, snrDB: snr, acc: make([]float64, n)}
}

func (s *scanner) Edge() float64 { return s.fs * scanEdge }

func (s *scanner) Feed(iq []complex64) {
	need := s.n * (scanFramesN + 1) / 2
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
		if v > s.acc[i] {
			s.acc[i] = s.acc[i]*(1-scanAvg) + v*scanAvg
		} else {
			s.acc[i] = s.acc[i]*(1-scanAvg/4) + v*scanAvg/4
		}
	}
}

func (s *scanner) Candidates() []float64 {
	if !s.have {
		return nil
	}
	binHz := s.fs / float64(s.n)
	full := s.smooth(max(1, int(math.Round(scanOccHz/binHz))))
	coreHalf := max(1, int(math.Round(scanCoreHz/binHz)))
	core := s.smooth(coreHalf)
	sorted := append([]float64(nil), full...)
	sort.Float64s(sorted)
	floor := sorted[len(sorted)/2]
	if floor <= 0 {
		return nil
	}
	thr := floor * math.Pow(10, s.snrDB/10)
	win := max(1, int(math.Floor((GridHz-scanOccHz)/binHz))-coreHalf)
	type pk struct{ off, p float64 }
	var peaks []pk
	edge := s.Edge()
	for i := 0; i < s.n; i++ {
		k := i
		if k > s.n/2 {
			k -= s.n
		}
		off := float64(k) * binHz
		if math.Abs(off) > edge || full[i] < thr {
			continue
		}
		local := true
		for j := -win; j <= win && local; j++ {
			if j != 0 && core[((i+j)%s.n+s.n)%s.n] > core[i] {
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
