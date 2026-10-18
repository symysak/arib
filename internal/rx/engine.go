package rx

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/symysak/arib/internal/iqsrc"
	"github.com/symysak/arib/internal/rec"
)

type Config struct {
	Source string
	Label string
	SampleRate float64
	Format     string
	CenterHz float64
	GainDB   float64
	AGC      bool
	LogDir string
	System string
	LogOut io.Writer
	PreS float64
	MaxLagS float64
	PostS, MaxS float64
	BandIQ bool
	BandHint func(centerMHz float64) string
	Channels []float64
	Ledger []float64
	Scan ScanConfig
	OnStart func(describe string, sampleRate float64)
	OnEmit  func(ch int, ev any)
	OnAudio func(ch, id int, pcm []int16, rate int)
	TimeOrigin time.Time
	Realtime bool
	Speed    float64
	NoIQ bool
	IDBase int
	AllowEmpty bool
	FollowVFO bool
	Preset []float64
	Inputs []InputConfig
	OnInputStart func(i int, describe string, sampleRate float64)
	Open func(o iqsrc.Options) (iqsrc.Source, error)
}

func (c *Config) Dir() string { return rec.SystemDir(c.LogDir, c.System) }

type Engine struct {
	std Standard
	cfg Config

	lg     *Logger
	band   *bandState
	rate   float64
	origin time.Time
	det    Detector
	head   *HeadRing
	blocked *blocklist
	preOpt map[float64]any

	shared *idTable
	claimed func(mhz float64) bool
	homeless func(mhz float64, kind int) bool

	elapsed float64
	read int64
	wg   sync.WaitGroup
	src iqsrc.Source
	controllable, srcKnown bool
	tuning     iqsrc.Tuning
	haveTuning bool

	mu      sync.Mutex
	ids     map[int64]int
	nextID  int
	byID    map[int]*station
	cmds    []func(ss []*station) []*station
	running bool
	done    bool
}

func New(std Standard, cfg Config) *Engine {
	return &Engine{std: std, cfg: cfg, band: &bandState{}, ids: map[int64]int{}, byID: map[int]*station{}, nextID: cfg.IDBase}
}

func (e *Engine) Add(mhz float64, opt any) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.done {
		return ErrNotRunning
	}
	if !e.running {
		e.cfg.Channels = append(e.cfg.Channels, mhz)
		if opt != nil {
			if e.preOpt == nil {
				e.preOpt = map[float64]any{}
			}
			e.preOpt[mhz] = opt
		}
		return nil
	}
	if off := mhz*1e6 - e.cfg.CenterHz; e.rate > 0 && math.Abs(off) > e.rate/2*0.9 {
		return fmt.Errorf("%.4f MHz は入力の帯域外（中心 %.4f MHz から %+.0f kHz、受けられるのは ±%.0f kHz）", mhz, e.cfg.CenterHz/1e6, off/1e3, e.rate/2*0.9/1e3)
	}
	e.cmds = append(e.cmds, func(ss []*station) []*station {
		for _, s := range ss {
			if math.Abs(s.mhz-mhz) < 1e-6 {
				s.kind = scanPinned
				return ss
			}
		}
		if off := mhz*1e6 - e.cfg.CenterHz; math.Abs(off) > e.rate/2*0.9 {
			e.lg.Printf("%.4f MHz は入力帯域外（オフセット %+.0f kHz）なので監視しない", mhz, off/1e3)
			return ss
		}
		return append(ss, e.add(mhz, scanPinned, opt))
	})
	return nil
}

func (e *Engine) Remove(mhz float64) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if !e.running {
		e.cfg.Channels = removeMHz(e.cfg.Channels, mhz)
		e.cfg.Ledger = removeMHz(e.cfg.Ledger, mhz)
		return
	}
	e.cmds = append(e.cmds, func(ss []*station) []*station {
		for _, s := range ss {
			if math.Abs(s.mhz-mhz) < 1e-6 {
				e.lg.Printf("%.4f MHz の監視をやめる", s.mhz)
				e.drop(s)
				return without(ss, s)
			}
		}
		return ss
	})
}

func removeMHz(list []float64, mhz float64) []float64 {
	out := list[:0]
	for _, f := range list {
		if math.Abs(f-mhz) >= 1e-6 {
			out = append(out, f)
		}
	}
	return out
}

func (e *Engine) Channel(id int) Channel {
	e.mu.Lock()
	defer e.mu.Unlock()
	if s := e.byID[id]; s != nil {
		return s.ch
	}
	return nil
}

func (e *Engine) idFor(mhz float64) int {
	if e.shared != nil {
		return e.shared.idFor(mhz)
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	k := int64(math.Round(mhz * 1e4))
	id, ok := e.ids[k]
	if !ok {
		id = e.nextID
		e.nextID++
		e.ids[k] = id
	}
	return id
}

func (e *Engine) add(mhz float64, kind int, opt any) *station {
	id := e.idFor(mhz)
	s := newStation(e, id, mhz)
	s.kind, s.since = kind, e.elapsed
	var replayed int64
	if kind == scanVerifying && e.head != nil {
		n, samples := e.head.replay(s.in)
		s.replayLeft.Store(int64(n))
		replayed = samples
	}
	var start float64
	if e.rate > 0 {
		start = float64(e.read-replayed) / e.rate
	}
	s.offHz = mhz*1e6 - e.cfg.CenterHz
	s.ch = e.std.NewChannel(ChannelConfig{ID: id, MHz: mhz, OffHz: s.offHz, FsIn: e.rate, Opt: opt,
		StartSec: start}, s)
	e.mu.Lock()
	e.byID[id] = s
	e.mu.Unlock()
	e.wg.Add(1)
	go func() { defer e.wg.Done(); defer close(s.done); s.run() }()
	return s
}

func (e *Engine) drop(s *station) {
	s.dropping = true
	close(s.in)
	e.mu.Lock()
	if e.byID[s.id] == s {
		delete(e.byID, s.id)
	}
	e.mu.Unlock()
}

func (e *Engine) Run(ctx context.Context) error {
	o := &e.cfg
	if err := os.MkdirAll(o.Dir(), 0o755); err != nil {
		return err
	}
	if strings.HasPrefix(o.Source, "rtltcp://") && o.SampleRate == 0 {
		o.SampleRate = 2400000
	}
	openSrc := func() (iqsrc.Source, error) {
		opts := iqsrc.Options{SampleRate: o.SampleRate, Format: o.Format,
			FreqHz: o.CenterHz, GainDB: o.GainDB, AGC: o.AGC, Realtime: o.Realtime, Speed: o.Speed}
		if o.Open != nil {
			return o.Open(opts)
		}
		return iqsrc.Open(o.Source, opts)
	}
	src, err := openSrc()
	if err != nil {
		return err
	}
	defer func() { src.Close() }()
	lg, err := NewLogger(filepath.Join(o.Dir(), "watch.log"), o.LogOut)
	if err != nil {
		return err
	}
	defer lg.Close()
	e.lg = lg
	e.mu.Lock()
	chans, ledger := append([]float64(nil), o.Channels...), append([]float64(nil), o.Ledger...)
	e.rate = src.SampleRate()
	e.mu.Unlock()
	if t, ok := takeTuning(src); ok {
		e.tuning, e.haveTuning = t, true
		e.mu.Lock()
		if t.CenterHz > 0 {
			o.CenterHz = t.CenterHz
		}
		if t.Rate > 0 {
			e.rate = t.Rate
		}
		e.mu.Unlock()
		if o.FollowVFO {
			chans = []float64{vfoMHz(t)}
		}
		lg.Printf("入力の同調: 中心 %.4f MHz・VFO %.4f MHz・%.0f Hz", t.CenterHz/1e6, t.VfoHz/1e6, t.Rate)
	}
	if in := presetInBand(o.Preset, o.CenterHz, e.rate); len(in) >= 2 {
		e.mu.Lock()
		if o.FollowVFO {
			chans, o.FollowVFO = nil, false
		}
		e.mu.Unlock()
		chans = mergeMHz(chans, in, 1e-6)
		ledger = dropNear(ledger, in, presetTol(o.Preset))
		lg.Printf("プリセットの波 %d 本のうち入力帯域に入る %d 本を監視する", len(o.Preset), len(in))
	}
	lg.Printf("監視開始 入力=%s 中心=%.4f MHz チャネル=%v", src.Describe(), o.CenterHz/1e6, chans)
	rate := e.rate
	e.origin = o.TimeOrigin
	if e.origin.IsZero() {
		e.origin = time.Now()
	}
	if o.OnStart != nil {
		o.OnStart(src.Describe(), rate)
	}
	var ss []*station
	for _, mhz := range chans {
		off := mhz*1e6 - o.CenterHz
		if math.Abs(off) > rate/2*0.9 {
			lg.Printf("%.4f MHz は入力帯域外（オフセット %+.0f kHz）なので監視しない", mhz, off/1e3)
			continue
		}
		e.mu.Lock()
		opt := e.preOpt[mhz]
		e.mu.Unlock()
		ss = append(ss, e.add(mhz, scanPinned, opt))
	}
	for _, mhz := range ledger {
		if off := mhz*1e6 - o.CenterHz; math.Abs(off) > rate/2*0.9 {
			lg.Printf("%.4f MHz は入力帯域外（オフセット %+.0f kHz）なので監視しない", mhz, off/1e3)
			continue
		}
		ss = append(ss, e.add(mhz, scanLedger, nil))
	}
	if o.Scan.Enabled {
		o.Scan.applyDefaults()
		e.det = e.std.NewDetector(DetectorConfig{FsIn: rate, CenterHz: o.CenterHz, SNRdB: o.Scan.SNRdB})
		if e.det == nil {
			e.closeAll(ss)
			return fmt.Errorf("%s は走査（-scan）に対応していない", e.std.Name())
		}
		e.blocked = newBlocklist(e.std.Grid())
		if o.Scan.HeadS > 0 {
			e.head = NewHeadRing(int(math.Ceil(o.Scan.HeadS / 0.1)))
			lg.Printf("走査で見つけた局には直前 %.1f 秒の帯域 I/Q を流し込んで頭を復元する", o.Scan.HeadS)
		}
		lg.Printf("帯域走査: 中心 %.4f MHz ± %.0f kHz（%.0f スロット）を見張り、当たりに最大 %d 局まで張り付ける",
			o.CenterHz/1e6, e.det.Edge()/1e3, e.det.Edge()*2/e.std.Grid(), o.Scan.Max)
	}
	if e.det == nil {
		for _, s := range ss {
			if s.kind == scanLedger {
				s.kind = scanPinned
			}
		}
	}
	if len(ss) == 0 && e.det == nil && !o.AllowEmpty {
		return errors.New("監視するチャネルが無い")
	}
	e.mu.Lock()
	e.running = true
	e.mu.Unlock()
	defer func() {
		e.mu.Lock()
		e.running, e.done = false, true
		e.cmds = nil
		e.mu.Unlock()
	}()
	var brec *bandRecorder
	if o.BandIQ {
		brec = newBandRecorder(o, lg, rate, o.CenterHz/1e6, e.band)
		lg.Printf("I/Q はベースバンドで録る（%.0f Hz、%.1f MB/s。局ごとの .wav は出さない）", rate, rate*4/1e6)
	}
	chunk := int(rate * 0.1)
	lastReport := time.Now()
	lastScan := 0.0
	var quietSince time.Time
	var stopping atomic.Bool
	stopWatch := make(chan struct{})
	defer close(stopWatch)
	go func() {
		select {
		case <-ctx.Done():
			lg.Printf("停止の合図を受けた。録音を閉じてから終わる")
			stopping.Store(true)
		case <-stopWatch:
		}
	}()
	stall := NewStallMeter(rate)
	for {
		if t, ok := takeTuning(src); ok {
			var moved bool
			if ss, moved = e.followTuning(t, ss); moved {
				rate = e.rate
				chunk = int(rate * 0.1)
				stall = NewStallMeter(rate)
				if brec != nil {
					brec.close()
					brec = newBandRecorder(o, lg, rate, o.CenterHz/1e6, e.band)
				}
			}
		}
		iq, err := src.Read(chunk)
		if stopping.Load() {
			break
		}
		if e.src != src {
			e.src = src
			_, ctl := src.(iqsrc.Controller)
			e.mu.Lock()
			e.controllable, e.srcKnown = ctl, true
			e.mu.Unlock()
		}
		ss = e.applyCmds(ss)
		if slow, ratio := stall.Add(len(iq), time.Now()); slow && src.Lossy() {
			lg.Printf("入力が詰まっている（%.0f 秒で実時間の %.0f%% しか届かない）", StallWindow.Seconds(), ratio*100)
			if !ReopenSource(&src, openSrc, lg, "入力の詰まり", &stopping) {
				break
			}
			stall = NewStallMeter(rate)
			continue
		}
		if len(iq) > 0 {
			quietSince = time.Time{}
			if brec != nil {
				brec.feed(iq)
			}
			if e.det != nil {
				e.elapsed += float64(len(iq)) / rate
				e.det.Feed(iq)
				if e.elapsed-lastScan >= o.Scan.IntervalSec {
					lastScan = e.elapsed
					ss = e.scanStep(ss, e.elapsed)
				}
			}
			e.distribute(ss, iq, src.Lossy())
			e.read += int64(len(iq))
			if e.head != nil {
				e.head.Push(iq)
			}
		} else if err == nil {
			if quietSince.IsZero() {
				quietSince = time.Now()
			} else if time.Since(quietSince) > 2*time.Second {
				lg.Printf("入力が途絶えました")
				quietSince = time.Now().Add(time.Hour)
			}
		}
		if err != nil {
			if !errors.Is(err, io.EOF) {
				lg.Printf("入力エラー: %v", err)
			}
			if src.Lossy() && ReopenSource(&src, openSrc, lg, "入力が切れた", &stopping) {
				stall = NewStallMeter(rate)
				continue
			}
			break
		}
		if time.Since(lastReport) > 5*time.Minute {
			for _, s := range ss {
				lg.Printf("%s", s.summary())
			}
			lastReport = time.Now()
		}
	}
	if brec != nil {
		brec.close()
	}
	e.closeAll(ss)
	for _, s := range ss {
		lg.Printf("%s", s.summary())
	}
	lg.Printf("監視終了")
	return nil
}

func (e *Engine) closeAll(ss []*station) {
	for _, s := range ss {
		close(s.in)
	}
	e.wg.Wait()
}

func (e *Engine) applyCmds(ss []*station) []*station {
	e.mu.Lock()
	cmds := e.cmds
	e.cmds = nil
	e.mu.Unlock()
	for _, c := range cmds {
		ss = c(ss)
	}
	return ss
}

func (e *Engine) distribute(ss []*station, iq []complex64, lossy bool) {
	pressure := lossy && anyBacklog(ss)
	for _, s := range ss {
		if !lossy {
			s.in <- iq
			continue
		}
		if pressure && s.backlog() >= shedLowWater && !s.recording.Load() && s.kind != scanPinned {
			s.dropped.Add(1)
			continue
		}
		select {
		case s.in <- iq:
		default:
			s.dropped.Add(1)
		}
	}
}

func presetInBand(preset []float64, centerHz, rate float64) []float64 {
	var in []float64
	for _, f := range preset {
		if math.Abs(f*1e6-centerHz) <= rate/2*0.9 {
			in = append(in, f)
		}
	}
	return in
}

func presetTol(preset []float64) float64 {
	tol := 0.0
	for i := range preset {
		for j := i + 1; j < len(preset); j++ {
			if d := math.Abs(preset[i]-preset[j]) / 2; d > 0 && (tol == 0 || d < tol) {
				tol = d
			}
		}
	}
	return tol
}

func mergeMHz(a, b []float64, tol float64) []float64 {
	out := append([]float64(nil), a...)
	for _, f := range b {
		if len(dropNear([]float64{f}, out, tol)) == 1 {
			out = append(out, f)
		}
	}
	return out
}

func dropNear(a, ref []float64, tol float64) []float64 {
	var out []float64
	for _, f := range a {
		near := false
		for _, r := range ref {
			if math.Abs(f-r) < tol || f == r {
				near = true
				break
			}
		}
		if !near {
			out = append(out, f)
		}
	}
	return out
}

type idTable struct {
	mu   sync.Mutex
	ids  map[int64]int
	next int
}

func newIDTable(base int) *idTable { return &idTable{ids: map[int64]int{}, next: base} }

func (t *idTable) idFor(mhz float64) int {
	t.mu.Lock()
	defer t.mu.Unlock()
	k := int64(math.Round(mhz * 1e4))
	id, ok := t.ids[k]
	if !ok {
		id = t.next
		t.next++
		t.ids[k] = id
	}
	return id
}
