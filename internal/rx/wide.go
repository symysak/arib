package rx


import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/symysak/arib/internal/iqsrc"
)

type InputConfig struct {
	Label      string
	Source     string
	SampleRate float64
	Format     string
	CenterHz   float64
	GainDB     float64
	AGC        bool
	Scan ScanConfig
	Open func(o iqsrc.Options) (iqsrc.Source, error)
}

type Wide struct {
	std Standard
	cfg Config
	ids *idTable
	ins []*wideInput

	mu      sync.Mutex
	running bool
	done    bool
}

type wideInput struct {
	idx   int
	label string
	e     *Engine
	ss    []*station
	open  func() (iqsrc.Source, error)
	lossy bool
	ended atomic.Bool
	token chan struct{}
	brec  *bandRecorder
	last  float64
}

type wideMsg struct {
	in     int
	iq     []complex64
	data   bool
	tuning *iqsrc.Tuning
	src    iqsrc.Source
	end    bool
}

func NewWide(std Standard, cfg Config) *Wide {
	w := &Wide{std: std, cfg: cfg, ids: newIDTable(cfg.IDBase)}
	first := InputConfig{Label: cfg.Label, Source: cfg.Source, SampleRate: cfg.SampleRate, Format: cfg.Format, CenterHz: cfg.CenterHz,
		GainDB: cfg.GainDB, AGC: cfg.AGC, Scan: cfg.Scan, Open: cfg.Open}
	all := append([]InputConfig{first}, cfg.Inputs...)
	for i, ic := range all {
		c := cfg
		c.Source, c.SampleRate, c.Format, c.CenterHz, c.GainDB, c.AGC, c.Scan, c.Open =
			ic.Source, ic.SampleRate, ic.Format, ic.CenterHz, ic.GainDB, ic.AGC, ic.Scan, ic.Open
		c.Channels, c.Ledger, c.Inputs, c.Preset = nil, nil, nil, nil
		if i > 0 {
			c.FollowVFO = false
		}
		label := ic.Label
		if label == "" {
			label = fmt.Sprintf("入力 %d", i+1)
		}
		e := New(std, c)
		e.shared = w.ids
		in := &wideInput{idx: i, label: label, e: e}
		w.ins = append(w.ins, in)
	}
	for _, in := range w.ins {
		i := in.idx
		in.e.claimed = func(mhz float64) bool { return w.claimedBy(i, mhz) }
		in.e.homeless = func(mhz float64, kind int) bool { return w.rehome(i, mhz, kind) }
	}
	return w
}

func (w *Wide) SetLabel(i int, label string) {
	if i >= 0 && i < len(w.ins) {
		w.ins[i].label = label
	}
}

func (w *Wide) NumInputs() int          { return len(w.ins) }
func (w *Wide) Input(i int) *Engine     { return w.ins[i].e }
func (w *Wide) InputLabel(i int) string { return w.ins[i].label }

func (w *Wide) Config() Config { return w.cfg }

func margin(e *Engine, mhz float64) float64 {
	e.mu.Lock()
	center, rate := e.cfg.CenterHz, e.rate
	if rate == 0 {
		rate = e.cfg.SampleRate
	}
	e.mu.Unlock()
	if rate == 0 {
		return math.Inf(-1)
	}
	return rate/2*0.9 - math.Abs(mhz*1e6-center)
}

func (w *Wide) best(mhz float64, skip int) int {
	b, bm := -1, 0.0
	for i, in := range w.ins {
		if i == skip || in.ended.Load() {
			continue
		}
		if m := margin(in.e, mhz); m >= 0 && (b < 0 || m > bm) {
			b, bm = i, m
		}
	}
	return b
}

func (w *Wide) bandsText() string {
	var p []string
	for _, in := range w.ins {
		in.e.mu.Lock()
		c, r := in.e.cfg.CenterHz, in.e.rate
		in.e.mu.Unlock()
		p = append(p, fmt.Sprintf("%s %.4f±%.0f kHz", in.label, c/1e6, r/2*0.9/1e3))
	}
	return strings.Join(p, "・")
}

func (w *Wide) claimedBy(i int, mhz float64) bool {
	for j, in := range w.ins {
		if j != i && nearStation(in.ss, mhz, w.std.Grid()/4) {
			return true
		}
	}
	return false
}

func (w *Wide) rehome(i int, mhz float64, kind int) bool {
	j := w.best(mhz, i)
	if j < 0 || w.claimedBy(-1, mhz) {
		return false
	}
	in := w.ins[j]
	in.ss = append(in.ss, in.e.add(mhz, kind, nil))
	return true
}

func (w *Wide) Covers(mhz float64) bool { return w.best(mhz, -1) >= 0 }

func (w *Wide) Logf(format string, a ...any) {
	if lg := w.ins[0].e.lg; lg != nil {
		lg.Printf(format, a...)
	}
}

func (w *Wide) Add(mhz float64, opt any) error {
	w.mu.Lock()
	if w.done {
		w.mu.Unlock()
		return ErrNotRunning
	}
	if !w.running {
		w.cfg.Channels = append(w.cfg.Channels, mhz)
		if opt != nil {
			if w.ins[0].e.preOpt == nil {
				w.ins[0].e.preOpt = map[float64]any{}
			}
			w.ins[0].e.preOpt[mhz] = opt
		}
		w.mu.Unlock()
		return nil
	}
	w.mu.Unlock()
	for _, in := range w.ins {
		in.e.mu.Lock()
		has := false
		for _, s := range in.e.byID {
			if math.Abs(s.mhz-mhz) < 1e-6 {
				has = true
			}
		}
		in.e.mu.Unlock()
		if has {
			return in.e.Add(mhz, opt)
		}
	}
	j := w.best(mhz, -1)
	if j < 0 {
		return fmt.Errorf("%.4f MHz はどの入力の帯域にも入らない（%s）", mhz, w.bandsText())
	}
	return w.ins[j].e.Add(mhz, opt)
}

func (w *Wide) Remove(mhz float64) {
	w.mu.Lock()
	if !w.running && !w.done {
		w.cfg.Channels = removeMHz(w.cfg.Channels, mhz)
		w.mu.Unlock()
		return
	}
	w.mu.Unlock()
	for _, in := range w.ins {
		in.e.Remove(mhz)
	}
}

func (w *Wide) Channel(id int) Channel {
	for _, in := range w.ins {
		if c := in.e.Channel(id); c != nil {
			return c
		}
	}
	return nil
}

func (w *Wide) Status() Status {
	st := w.ins[0].e.Status()
	w.mu.Lock()
	st.Running, st.Done = w.running, w.done
	pre := !w.running && !w.done
	chans := append([]float64(nil), w.cfg.Channels...)
	ledger := append([]float64(nil), w.cfg.Ledger...)
	w.mu.Unlock()
	if pre {
		st.Channels = []ChannelInfo{}
		for _, f := range append(chans, ledger...) {
			st.Channels = append(st.Channels, ChannelInfo{ID: -1, MHz: f})
		}
	} else {
		for _, in := range w.ins[1:] {
			st.Channels = append(st.Channels, in.e.Status().Channels...)
		}
	}
	sort.Slice(st.Channels, func(i, j int) bool { return st.Channels[i].MHz < st.Channels[j].MHz })
	return st
}

func (w *Wide) Tune(centerHz float64) error        { return w.ins[0].e.Tune(centerHz) }
func (w *Wide) SetGain(db float64, agc bool) error { return w.ins[0].e.SetGain(db, agc) }

func (w *Wide) Run(ctx context.Context) error {
	o := &w.cfg
	if err := os.MkdirAll(o.Dir(), 0o755); err != nil {
		return err
	}
	lg, err := NewLogger(filepath.Join(o.Dir(), "watch.log"), o.LogOut)
	if err != nil {
		return err
	}
	defer lg.Close()
	origin := o.TimeOrigin
	if origin.IsZero() {
		origin = time.Now()
	}
	srcs := make([]iqsrc.Source, len(w.ins))
	defer func() {
		for _, s := range srcs {
			if s != nil {
				s.Close()
			}
		}
	}()
	for i, in := range w.ins {
		e := in.e
		ec := &e.cfg
		if strings.HasPrefix(ec.Source, "rtltcp://") && ec.SampleRate == 0 {
			ec.SampleRate = 2400000
		}
		ecCopy := ec
		in.open = func() (iqsrc.Source, error) {
			opts := iqsrc.Options{SampleRate: ecCopy.SampleRate, Format: ecCopy.Format, FreqHz: ecCopy.CenterHz, GainDB: ecCopy.GainDB,
				AGC: ecCopy.AGC, Realtime: ecCopy.Realtime, Speed: ecCopy.Speed}
			if ecCopy.Open != nil {
				return ecCopy.Open(opts)
			}
			return iqsrc.Open(ecCopy.Source, opts)
		}
		src, err := in.open()
		if err != nil {
			if i == 0 {
				return err
			}
			return fmt.Errorf("%s: %w", in.label, err)
		}
		srcs[i] = src
		e.lg, e.origin = lg, origin
		e.mu.Lock()
		e.rate = src.SampleRate()
		e.mu.Unlock()
		in.lossy = src.Lossy()
		if t, ok := takeTuning(src); ok {
			e.tuning, e.haveTuning = t, true
			e.mu.Lock()
			if t.CenterHz > 0 {
				ec.CenterHz = t.CenterHz
			}
			if t.Rate > 0 {
				e.rate = t.Rate
			}
			e.mu.Unlock()
			lg.Printf("%s の同調: 中心 %.4f MHz・VFO %.4f MHz・%.0f Hz", in.label, t.CenterHz/1e6, t.VfoHz/1e6, t.Rate)
		}
	}
	w.mu.Lock()
	chans, ledger := append([]float64(nil), o.Channels...), append([]float64(nil), o.Ledger...)
	w.mu.Unlock()
	if w.ins[0].e.cfg.FollowVFO && w.ins[0].e.haveTuning && len(w.ins) == 1 {
		chans = []float64{vfoMHz(w.ins[0].e.tuning)}
	}
	var presetIn []float64
	for _, in := range w.ins {
		presetIn = mergeMHz(presetIn, presetInBand(o.Preset, in.e.cfg.CenterHz, in.e.rate), 1e-6)
	}
	if len(presetIn) >= 2 {
		if w.ins[0].e.cfg.FollowVFO {
			chans, w.ins[0].e.cfg.FollowVFO = nil, false
		}
		chans = mergeMHz(chans, presetIn, 1e-6)
		ledger = dropNear(ledger, presetIn, presetTol(o.Preset))
		lg.Printf("プリセットの波 %d 本のうち入力帯域に入る %d 本を監視する", len(o.Preset), len(presetIn))
	}
	for i, in := range w.ins {
		lg.Printf("監視開始 %s=%s 中心=%.4f MHz %.0f Hz", in.label, srcs[i].Describe(), in.e.cfg.CenterHz/1e6, in.e.rate)
		if i == 0 && o.OnStart != nil {
			o.OnStart(srcs[i].Describe(), in.e.rate)
		}
		if o.OnInputStart != nil {
			o.OnInputStart(i, srcs[i].Describe(), in.e.rate)
		}
	}
	place := func(mhz float64, kind int) {
		j := w.best(mhz, -1)
		if j < 0 {
			lg.Printf("%.4f MHz はどの入力の帯域にも入らないので監視しない（%s）", mhz, w.bandsText())
			return
		}
		if w.claimedBy(-1, mhz) {
			return
		}
		in := w.ins[j]
		opt := w.ins[0].e.preOpt[mhz]
		in.ss = append(in.ss, in.e.add(mhz, kind, opt))
	}
	for _, f := range chans {
		place(f, scanPinned)
	}
	for _, f := range ledger {
		place(f, scanLedger)
	}
	anyScan := false
	for _, in := range w.ins {
		e := in.e
		if !e.cfg.Scan.Enabled {
			continue
		}
		e.cfg.Scan.applyDefaults()
		e.det = w.std.NewDetector(DetectorConfig{FsIn: e.rate, CenterHz: e.cfg.CenterHz, SNRdB: e.cfg.Scan.SNRdB})
		if e.det == nil {
			if in.idx > 0 {
				lg.Printf("%s は走査しない（%s はこの入力の帯域の走査に対応していない）", in.label, w.std.Name())
				e.cfg.Scan.Enabled = false
				continue
			}
			w.closeAll()
			return fmt.Errorf("%s は走査（-scan）に対応していない", w.std.Name())
		}
		anyScan = true
		e.blocked = newBlocklist(w.std.Grid())
		if e.cfg.Scan.HeadS > 0 {
			e.head = NewHeadRing(int(math.Ceil(e.cfg.Scan.HeadS / 0.1)))
		}
		lg.Printf("帯域走査（%s）: 中心 %.4f MHz ± %.0f kHz を見張り、当たりに最大 %d 局まで張り付ける",
			in.label, e.cfg.CenterHz/1e6, e.det.Edge()/1e3, e.cfg.Scan.Max)
	}
	if !anyScan {
		for _, in := range w.ins {
			for _, s := range in.ss {
				if s.kind == scanLedger {
					s.kind = scanPinned
				}
			}
		}
	}
	nst := 0
	for _, in := range w.ins {
		nst += len(in.ss)
	}
	if nst == 0 && !anyScan && !o.AllowEmpty {
		w.closeAll()
		return errors.New("監視するチャネルが無い")
	}
	w.mu.Lock()
	w.running = true
	w.mu.Unlock()
	for _, in := range w.ins {
		in.e.mu.Lock()
		in.e.running = true
		in.e.mu.Unlock()
		if o.BandIQ {
			in.brec = newBandRecorder(&in.e.cfg, lg, in.e.rate, in.e.cfg.CenterHz/1e6, in.e.band)
		}
	}
	defer func() {
		w.mu.Lock()
		w.running, w.done = false, true
		w.mu.Unlock()
		for _, in := range w.ins {
			in.e.mu.Lock()
			in.e.running, in.e.done = false, true
			in.e.cmds = nil
			in.e.mu.Unlock()
		}
	}()

	var stopping atomic.Bool
	stopCh := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			lg.Printf("停止の合図を受けた。録音を閉じてから終わる")
			stopping.Store(true)
			close(stopCh)
		case <-stopCh:
		}
	}()
	all := make(chan wideMsg, 8*len(w.ins))
	var rwg sync.WaitGroup
	for i, in := range w.ins {
		if !in.lossy {
			in.token = make(chan struct{}, 1)
		}
		rwg.Add(1)
		go func(in *wideInput, src iqsrc.Source) {
			defer rwg.Done()
			w.reader(in, src, all, &stopping, stopCh, lg)
		}(in, srcs[i])
		srcs[i] = nil
	}

	lastReport := time.Now()
	tick := time.NewTicker(200 * time.Millisecond)
	defer tick.Stop()
	active := len(w.ins)
	granted := make([]bool, len(w.ins))
	pending := 0
	for active > 0 {
		if pending == 0 {
			for i, in := range w.ins {
				if in.token != nil && !in.ended.Load() {
					in.token <- struct{}{}
					granted[i] = true
					pending++
				}
			}
		}
		var m wideMsg
		select {
		case m = <-all:
		case <-tick.C:
			for _, in := range w.ins {
				if !in.ended.Load() {
					in.ss = in.e.applyCmds(in.ss)
				}
			}
			continue
		}
		in := w.ins[m.in]
		w.handle(in, m)
		if granted[m.in] && (m.data || m.end) {
			granted[m.in] = false
			pending--
		}
		if m.end && !in.ended.Load() {
			in.ended.Store(true)
			active--
			if in.brec != nil {
				in.brec.close()
			}
			in.e.closeAll(in.ss)
			for _, s := range in.ss {
				lg.Printf("%s", s.summary())
			}
			in.ss = nil
			in.e.mu.Lock()
			in.e.running, in.e.done = false, true
			in.e.mu.Unlock()
			lg.Printf("%s の受信を終えた", in.label)
		}
		if time.Since(lastReport) > 5*time.Minute {
			for _, x := range w.ins {
				for _, s := range x.ss {
					lg.Printf("%s", s.summary())
				}
			}
			lastReport = time.Now()
		}
	}
	if !stopping.Load() {
		close(stopCh)
	}
	rwg.Wait()
	lg.Printf("監視終了")
	return nil
}

func (w *Wide) handle(in *wideInput, m wideMsg) {
	e := in.e
	if m.src != nil && m.src != e.src {
		e.src = m.src
		_, ctl := m.src.(iqsrc.Controller)
		e.mu.Lock()
		e.controllable, e.srcKnown = ctl, true
		e.mu.Unlock()
	}
	if in.ended.Load() {
		return
	}
	in.ss = e.applyCmds(in.ss)
	if m.tuning != nil {
		var moved bool
		if in.ss, moved = e.followTuning(*m.tuning, in.ss); moved && in.brec != nil {
			in.brec.close()
			in.brec = newBandRecorder(&e.cfg, e.lg, e.rate, e.cfg.CenterHz/1e6, e.band)
		}
		return
	}
	if len(m.iq) == 0 {
		return
	}
	if in.brec != nil {
		in.brec.feed(m.iq)
	}
	if e.det != nil {
		e.elapsed += float64(len(m.iq)) / e.rate
		e.det.Feed(m.iq)
		if e.elapsed-in.last >= e.cfg.Scan.IntervalSec {
			in.last = e.elapsed
			in.ss = e.scanStep(in.ss, e.elapsed)
		}
	}
	e.distribute(in.ss, m.iq, in.lossy)
	e.read += int64(len(m.iq))
	if e.head != nil {
		e.head.Push(m.iq)
	}
}

func (w *Wide) reader(in *wideInput, src iqsrc.Source, all chan<- wideMsg, stopping *atomic.Bool, stopCh <-chan struct{}, lg *Logger) {
	defer func() { src.Close() }()
	send := func(m wideMsg) {
		m.in = in.idx
		all <- m
	}
	in.e.mu.Lock()
	rate := in.e.rate
	in.e.mu.Unlock()
	chunk := int(rate * 0.1)
	stall := NewStallMeter(rate)
	var quietSince time.Time
	send(wideMsg{src: src})
	for {
		if in.token != nil {
			select {
			case <-in.token:
			case <-stopCh:
				send(wideMsg{end: true})
				return
			}
		}
		if stopping.Load() {
			send(wideMsg{end: true})
			return
		}
		if t, ok := takeTuning(src); ok {
			tt := t
			send(wideMsg{tuning: &tt})
			if t.Rate > 0 && math.Abs(t.Rate-rate) > 0.5 {
				rate = t.Rate
				chunk = int(rate * 0.1)
				stall = NewStallMeter(rate)
			}
		}
		iq, err := src.Read(chunk)
		if stopping.Load() {
			send(wideMsg{end: true})
			return
		}
		if slow, ratio := stall.Add(len(iq), time.Now()); slow && src.Lossy() {
			lg.Printf("%s が詰まっている（%.0f 秒で実時間の %.0f%% しか届かない）", in.label, StallWindow.Seconds(), ratio*100)
			if !ReopenSource(&src, in.open, lg, in.label+"の詰まり", stopping) {
				send(wideMsg{end: true})
				return
			}
			stall = NewStallMeter(rate)
			send(wideMsg{src: src, data: true})
			continue
		}
		if len(iq) > 0 {
			quietSince = time.Time{}
		} else if err == nil {
			if quietSince.IsZero() {
				quietSince = time.Now()
			} else if time.Since(quietSince) > 2*time.Second {
				lg.Printf("%s が途絶えました", in.label)
				quietSince = time.Now().Add(time.Hour)
			}
		}
		if err != nil {
			if !errors.Is(err, io.EOF) {
				lg.Printf("%s の入力エラー: %v", in.label, err)
			}
			if src.Lossy() && ReopenSource(&src, in.open, lg, in.label+"が切れた", stopping) {
				stall = NewStallMeter(rate)
				send(wideMsg{iq: iq, src: src, data: true})
				continue
			}
			send(wideMsg{iq: iq, data: true, end: true})
			return
		}
		send(wideMsg{iq: iq, data: true})
	}
}

func (w *Wide) closeAll() {
	for _, in := range w.ins {
		in.e.closeAll(in.ss)
		in.ss = nil
	}
}
