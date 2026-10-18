package server

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"io/fs"
	"math"
	"net/http"
	"sort"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/symysak/arib/internal/credit"
	"github.com/symysak/arib/internal/iqsrc"
	"github.com/symysak/arib/internal/rx"
	"github.com/symysak/arib/internal/std-t115/qpsknarrow/amrwbp"
	"github.com/symysak/arib/internal/std-t115/qpsknarrow/dsp"
	"github.com/symysak/arib/internal/webui"
)

type Config struct {
	Source string
	SampleRate float64
	Format     string
	CenterHz float64
	GainDB   float64
	AGC      bool
	Realtime bool
	Speed    float64
	Channels []float64
	Opts     map[int64]ChannelOpt
	Scramble int
	Scan rx.ScanConfig
	LogDir string
	NoIQ bool
	PreS float64
	LogOut io.Writer
	FollowVFO bool
	Open func(o iqsrc.Options) (iqsrc.Source, error)
	LedgerDir string
	AudioTap func(idx int, pcm []int16, rate int)
}

const maxLogs = 400

type Monitor struct {
	cfg   Config
	std   *Standard
	eng   *rx.Wide
	w     *webui.Server
	audio *webui.Hub
	led   *rx.Ledger

	selected atomic.Int64

	mu       sync.Mutex
	meta     Meta
	chans    map[int]*Channel
	logs     []LogLine
	audioSeq map[int]int64
	t0       time.Time
}

func NewMonitor(cfg Config) *Monitor {
	if cfg.PreS <= 0 {
		cfg.PreS = iqPrerollSec
	}
	t0, est := TimeOrigin(cfg.Source)
	if iqsrc.IsNetwork(cfg.Source) {
		t0, est = time.Now(), false
	}
	m := &Monitor{cfg: cfg, chans: map[int]*Channel{}, audioSeq: map[int]int64{}, t0: t0, audio: webui.NewHub()}
	m.selected.Store(-1)
	m.std = NewStandard(StdConfig{Scramble: cfg.Scramble, Opts: cfg.Opts, NoIQ: cfg.NoIQ, Origin: t0, LogDir: cfg.LogDir})
	m.std.hooks = &stdHooks{opened: m.opened, closed: m.closed, selected: m.isSelected}
	chans := append([]float64{}, cfg.Channels...)
	sort.Float64s(chans)
	m.meta = Meta{
		Input: cfg.Source, CenterMHz: cfg.CenterHz / 1e6, SampleRate: cfg.SampleRate, GainDB: cfg.GainDB,
		Channels: chans, LogDir: cfg.LogDir, T0WallMS: t0.UnixMilli(), Estimated: est,
		Network: iqsrc.IsNetwork(cfg.Source), Scan: cfg.Scan.Enabled,
		System:     "ARIB STD-T115 QPSK ナロー方式（Volume 2, 7.5kHz 間隔）",
		SymbolRate: dsp.SymbolRate, FrameBits: dsp.FrameBits, RollOff: dsp.RollOff,
		SW1Hex: dsp.SW1Hex, SW3Hex: dsp.SW3Hex,
		AudioAvailable: amrwbp.Available(), AudioRate: amrwbp.OutputRate,
	}
	ec := rx.Config{
		Source: cfg.Source, SampleRate: cfg.SampleRate, Format: cfg.Format,
		CenterHz: cfg.CenterHz, GainDB: cfg.GainDB, AGC: cfg.AGC,
		Realtime: cfg.Realtime, Speed: cfg.Speed,
		LogDir: cfg.LogDir, System: System, LogOut: cfg.LogOut,
		PreS: cfg.PreS, MaxLagS: 2.5, PostS: 5, MaxS: broadcastMaxS,
		NoIQ: cfg.NoIQ, Channels: cfg.Channels, Scan: cfg.Scan,
		TimeOrigin: t0,
		AllowEmpty: true,
		OnStart:    m.onStart,
		OnEmit:     m.onEmit,
		OnAudio:    m.onAudio,
		FollowVFO:  cfg.FollowVFO,
		Open:       cfg.Open,
	}
	var notes []string
	if led, err := rx.OpenLedger(cfg.LedgerDir, m.std.Grid()); err != nil {
		notes = append(notes, fmt.Sprintf("台帳を開けませんでした（台帳なしで続けます）: %v", err))
	} else if led != nil {
		m.led = led
		if n := led.Restore(&ec); n > 0 {
			notes = append(notes, fmt.Sprintf("台帳: %d 局を起動時から監視します（%s）", n, led.Path()))
		}
	}
	m.eng = rx.NewWide(m.std, ec)
	m.w = webui.New(webui.Config{
		Panes:    []fs.FS{m.std.Pane()},
		Snapshot: func() any { s := m.Snapshot(); return Event{Type: EvSnapshot, Idx: -1, Snapshot: &s} },
		Status:   func() any { return m.Snapshot() },
		FileDir:  func() string { return cfg.LogDir },
		Control:  monitorControl{Control: webui.WideControl(func() *rx.Wide { return m.eng }), m: m},
		Inputs:   webui.WideInputs(func() *rx.Wide { return m.eng }, 1+len(ec.Inputs)),
	})
	mux := m.w.Mux()
	mux.HandleFunc("/ws/audio", func(w http.ResponseWriter, r *http.Request) { m.w.Serve(m.audio, w, r, true) })
	mux.HandleFunc("/api/select", m.handleSelect)
	m.std.Routes(mux, m.eng.Channel)
	for _, n := range notes {
		m.addLog("info", "system", "%s", n)
	}
	return m
}

func (m *Monitor) Handler() http.Handler { return m.w }

func (m *Monitor) Engine() *rx.Wide { return m.eng }

func (m *Monitor) Addr() string { return m.w.Addr() }

func (m *Monitor) ListenAndServe(ctx context.Context, addr string) error {
	return m.w.ListenAndServe(ctx, addr, func(ctx context.Context) { _ = m.Run(ctx) })
}

func (m *Monitor) Run(ctx context.Context) error {
	if !m.meta.AudioAvailable {
		m.addLog("warn", "system", "AMR-WB+ デコーダが未ビルドのため音声は出ません（bash scripts/std-t115/build_amrwbplus.sh）")
	}
	kctx, stop := context.WithCancel(ctx)
	defer stop()
	if m.led != nil {
		go m.led.Keep(kctx, m.eng, func(f string, v ...any) { m.addLog("warn", "system", f, v...) })
	}
	err := m.eng.Run(ctx)
	if lerr := m.led.Finish(m.eng); lerr != nil {
		m.addLog("warn", "system", "台帳を書けませんでした: %v", lerr)
	}
	m.mu.Lock()
	m.meta.Finished = true
	meta := m.meta
	m.mu.Unlock()
	m.w.Publish(Event{Type: EvMeta, Idx: -1, Meta: &meta})
	if err != nil {
		m.addLog("error", "system", "受信が止まりました: %v", err)
	} else {
		m.addLog("info", "system", "入力が終端に達しました（画面はこのまま見られます）")
	}
	return err
}

func (m *Monitor) Snapshot() Snapshot {
	m.mu.Lock()
	chans := make([]*Channel, 0, len(m.chans))
	for _, c := range m.chans {
		chans = append(chans, c)
	}
	s := Snapshot{Generator: credit.Generator, Meta: m.meta, Logs: append([]LogLine{}, m.logs...),
		Recs: []any{}, Stations: []Station{}, T115: map[string]ChannelSnapshot{}}
	s.Meta.Channels = append([]float64{}, m.meta.Channels...)
	m.mu.Unlock()
	sort.Slice(chans, func(i, j int) bool { return chans[i].ID() < chans[j].ID() })
	for _, c := range chans {
		s.Stations = append(s.Stations, c.station())
		s.T115[strconv.Itoa(c.ID())] = c.snapshot()
	}
	return s
}


func (m *Monitor) onStart(describe string, rate float64) {
	m.mu.Lock()
	m.meta.Input, m.meta.SampleRate = describe, rate
	if !m.cfg.NoIQ {
		m.meta.IQRate = rate / float64(iqDecimation(rate))
	}
	meta := m.meta
	m.mu.Unlock()
	m.w.Publish(Event{Type: EvMeta, Idx: -1, Meta: &meta})
	m.addLog("info", "system", "受信開始: %s（%.0f Hz、中心 %.5f MHz）", describe, rate, m.cfg.CenterHz/1e6)
}

func (m *Monitor) onEmit(ch int, ev any) {
	e, ok := ev.(Event)
	if !ok {
		return
	}
	e.Idx = ch
	if e.Log != nil {
		m.mu.Lock()
		m.logs = append(m.logs, *e.Log)
		if len(m.logs) > maxLogs {
			m.logs = m.logs[len(m.logs)-maxLogs:]
		}
		m.mu.Unlock()
	}
	m.w.Publish(e)
}

func (m *Monitor) onAudio(ch, _ int, pcm []int16, rate int) {
	if m.cfg.AudioTap != nil {
		m.cfg.AudioTap(ch, pcm, rate)
	}
	m.mu.Lock()
	seq := m.audioSeq[ch]
	m.audioSeq[ch] = seq + int64(len(pcm))
	m.mu.Unlock()
	b := make([]byte, 12+2*len(pcm))
	binary.LittleEndian.PutUint64(b[0:8], uint64(seq))
	binary.LittleEndian.PutUint16(b[8:10], uint16(ch))
	binary.LittleEndian.PutUint16(b[10:12], uint16(rate))
	for i, v := range pcm {
		binary.LittleEndian.PutUint16(b[12+2*i:], uint16(v))
	}
	m.audio.Broadcast(b)
}

func (m *Monitor) opened(c *Channel) {
	m.mu.Lock()
	m.chans[c.ID()] = c
	m.meta.Channels = channelList(m.chans)
	if m.selected.Load() < 0 {
		m.selected.Store(int64(c.ID()))
	}
	meta := m.meta
	m.mu.Unlock()
	m.w.Publish(Event{Type: EvMeta, Idx: -1, Meta: &meta})
	m.addLogFor(c.ID(), "info", "system", "%.5f MHz の監視を始めた（オフセット %+.1f kHz・スクランブル %s）",
		c.MHz(), c.cfg.OffHz/1e3, scrambleText(c.station().Scramble))
}

func (m *Monitor) closed(c *Channel, dropped bool) {
	if !dropped {
		return
	}
	c.mu.Lock()
	keep := c.quality.Frames > 0 && !c.forget
	c.mu.Unlock()
	if keep {
		m.addLogFor(c.ID(), "info", "system", "%.5f MHz の監視をやめた（行は残す）", c.MHz())
		return
	}
	m.mu.Lock()
	if m.chans[c.ID()] == c {
		delete(m.chans, c.ID())
	}
	m.meta.Channels = channelList(m.chans)
	meta := m.meta
	m.mu.Unlock()
	m.w.Publish(Event{Type: EvStationGone, Idx: c.ID(), Station: &Station{Idx: c.ID(), MHz: c.MHz()}})
	m.w.Publish(Event{Type: EvMeta, Idx: -1, Meta: &meta})
	m.addLogFor(c.ID(), "info", "system", "%.5f MHz の監視をやめた", c.MHz())
}

func (m *Monitor) isSelected(id int) bool { return int(m.selected.Load()) == id }

func (m *Monitor) handleSelect(w http.ResponseWriter, r *http.Request) {
	v, err := strconv.Atoi(r.URL.Query().Get("idx"))
	if err != nil {
		http.Error(w, "idx が不正です", http.StatusBadRequest)
		return
	}
	m.selected.Store(int64(v))
	w.WriteHeader(http.StatusNoContent)
}

func (m *Monitor) addLog(level, group, format string, a ...any) {
	m.addLogFor(-1, level, group, format, a...)
}

func (m *Monitor) addLogFor(idx int, level, group, format string, a ...any) {
	m.mu.Lock()
	var t float64
	for _, c := range m.chans {
		t = math.Max(t, c.nowSec())
	}
	l := LogLine{TSec: t, Idx: idx, Group: group, Level: level, Text: fmt.Sprintf(format, a...)}
	m.logs = append(m.logs, l)
	if len(m.logs) > maxLogs {
		m.logs = m.logs[len(m.logs)-maxLogs:]
	}
	m.mu.Unlock()
	m.w.Publish(Event{Type: EvLog, Idx: idx, Log: &l})
}

func (m *Monitor) setMeta(f func(*Meta)) {
	m.mu.Lock()
	f(&m.meta)
	meta := m.meta
	m.mu.Unlock()
	m.w.Publish(Event{Type: EvMeta, Idx: -1, Meta: &meta})
}

func channelList(chans map[int]*Channel) []float64 {
	out := []float64{}
	for _, c := range chans {
		out = append(out, c.MHz())
	}
	sort.Float64s(out)
	return out
}

type monitorControl struct {
	webui.Control
	m *Monitor
}

func (c monitorControl) Add(mhz float64) error { return c.Control.Add(mhz) }

func (c monitorControl) Remove(mhz float64) error {
	c.m.mu.Lock()
	for _, ch := range c.m.chans {
		if math.Abs(ch.MHz()-mhz) < 1e-6 {
			ch.mu.Lock()
			ch.forget = true
			ch.mu.Unlock()
		}
	}
	c.m.mu.Unlock()
	return c.Control.Remove(mhz)
}

func (c monitorControl) Tune(hz float64) error {
	if err := c.Control.Tune(hz); err != nil {
		return err
	}
	c.m.setMeta(func(m *Meta) { m.CenterMHz = hz / 1e6 })
	return nil
}

func (c monitorControl) SetGain(db float64, agc bool) error {
	if err := c.Control.SetGain(db, agc); err != nil {
		return err
	}
	c.m.setMeta(func(m *Meta) {
		m.GainDB = db
		if agc {
			m.GainDB = 0
		}
	})
	return nil
}
