package server

import (
	"context"
	"encoding/binary"
	"fmt"
	"io/fs"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/symysak/arib/internal/credit"
	"github.com/symysak/arib/internal/data"
	"github.com/symysak/arib/internal/iqsrc"
	"github.com/symysak/arib/internal/nxdn/dsp"
	"github.com/symysak/arib/internal/rec"
	"github.com/symysak/arib/internal/rx"
	"github.com/symysak/arib/internal/voice"
	"github.com/symysak/arib/internal/webui"
)

type Config struct {
	Engine rx.Config
	Codec    string
	CodecOpt voice.Options
	Data string
	Init *int
	Opts map[int64]ChannelOpt
	NoTCH bool
	AudioAGC bool
	AudioTap func(idx int, pcm []int16, rate int)
	LedgerDir string
}

const (
	maxLogs   = 400
	maxRecs   = 200
	maxFrames = 2000
	maxMsgs   = 500
	maxTxs    = 400
	maxData   = 500
)

type Monitor struct {
	cfg   Config
	std   *Standard
	eng   *rx.Wide
	w     *webui.Server
	led   *rx.Ledger
	audio *webui.Hub

	mu       sync.Mutex
	meta     Meta
	chans    map[int]*Channel
	logs     []LogLine
	recs     []Rec
	frames   []FrameRec
	msgs     []MsgRec
	txs      []TxRec
	signals  map[int]Signal
	dataEvs  map[string][]*DataEvent
	audioSeq map[int]int64
}

func NewMonitor(spec Spec, cfg Config) *Monitor {
	ec := cfg.Engine
	t0, est := timeOrigin(ec.Source)
	m := &Monitor{cfg: cfg, chans: map[int]*Channel{}, audio: webui.NewHub(), audioSeq: map[int]int64{},
		signals: map[int]Signal{}, dataEvs: map[string][]*DataEvent{}}
	init := -1
	if cfg.Init != nil {
		init = *cfg.Init
	}
	m.std = NewStandard(spec, StdConfig{LogDir: ec.LogDir, Codec: cfg.Codec, CodecOpt: cfg.CodecOpt, Data: cfg.Data, Init: init,
		Opts: cfg.Opts, Lossless: !iqsrc.IsNetwork(ec.Source), NoTCH: cfg.NoTCH, AudioAGC: cfg.AudioAGC})
	m.std.audio = m.onAudio
	m.std.hooks = &stdHooks{opened: m.opened, closed: m.closed}
	chans := append([]float64{}, ec.Channels...)
	sort.Float64s(chans)
	dataMode := cfg.Data
	if dataMode == "" {
		dataMode = data.Auto
	}
	if spec.DataChannel == "" {
		dataMode = "off"
	}
	m.meta = Meta{
		Input: ec.Source, CenterMHz: ec.CenterHz / 1e6, SampleRate: ec.SampleRate, GainDB: ec.GainDB,
		Channels: chans, LogDir: ec.LogDir, T0WallMS: t0.UnixMilli(), Estimated: est,
		Network: iqsrc.IsNetwork(ec.Source), Scan: ec.Scan.Enabled,
		System: spec.Title, Ext: spec.Meta,
		SymbolRate: dsp.SymbolRate, Codecs: CodecNames(spec.VoiceChannel), CodecMode: cfg.Codec, DataMode: dataMode, AudioRate: voice.SampleRate,
	}
	ec.System = spec.System
	ec.TimeOrigin = t0
	ec.OnStart = m.onStart
	ec.OnEmit = m.onEmit
	if ec.PreS <= 0 {
		ec.PreS = 3
	}
	if ec.MaxLagS <= 0 {
		ec.MaxLagS = 1
	}
	ec.AllowEmpty = true
	var notes []string
	if led, err := rx.OpenLedger(cfg.LedgerDir, m.std.Grid()); err != nil {
		notes = append(notes, fmt.Sprintf("台帳を開けませんでした（台帳なしで続けます）: %v", err))
	} else if led != nil {
		m.led = led
		m.std.ledgerCodec = led.Codec
		if n := led.Restore(&ec); n > 0 {
			notes = append(notes, fmt.Sprintf("台帳: %d 局を起動時から監視します（%s）", n, led.Path()))
		}
	}
	m.eng = rx.NewWide(m.std, ec)
	for i := 0; i < m.eng.NumInputs(); i++ {
		m.meta.Inputs = append(m.meta.Inputs, InputMeta{ID: fmt.Sprintf("in%d", i), Label: m.eng.InputLabel(i)})
	}
	m.refreshInputs()
	m.w = webui.New(webui.Config{
		Panes:    append(append([]fs.FS{}, spec.Panes...), m.std.Pane()),
		Snapshot: func() any { s := m.Snapshot(); return Event{Type: EvSnapshot, Idx: -1, Snapshot: &s} },
		Status:   func() any { return m.Snapshot() },
		FileDir:  func() string { return ec.LogDir },
		Control:  monitorControl{Control: webui.WideControl(func() *rx.Wide { return m.eng }), m: m},
		Inputs:   webui.WideInputs(func() *rx.Wide { return m.eng }, m.eng.NumInputs()),
	})
	mux := m.w.Mux()
	mux.HandleFunc("/ws/audio", func(w http.ResponseWriter, r *http.Request) { m.w.Serve(m.audio, w, r, true) })
	mux.HandleFunc("/api/select", func(w http.ResponseWriter, r *http.Request) {
		v, err := strconv.Atoi(r.URL.Query().Get("idx"))
		if err != nil {
			http.Error(w, "idx が不正です", http.StatusBadRequest)
			return
		}
		m.std.selected.Store(int64(v))
		w.WriteHeader(http.StatusNoContent)
	})
	m.std.Routes(mux, m.eng.Channel)
	for _, n := range notes {
		m.addLog("info", "system", "%s", n)
	}
	return m
}

func (m *Monitor) refreshInputs() {
	for i := range m.meta.Inputs {
		in := m.eng.Input(i).Status()
		mi := &m.meta.Inputs[i]
		mi.Input, mi.CenterMHz, mi.SampleRate, mi.GainDB, mi.AGC = in.Input, in.CenterHz/1e6, in.SampleRate, in.GainDB, in.AGC
	}
}

func (m *Monitor) Handler() http.Handler { return m.w }

func (m *Monitor) Engine() *rx.Wide { return m.eng }

func (m *Monitor) Addr() string { return m.w.Addr() }

func (m *Monitor) HeardCodec() string {
	m.mu.Lock()
	chans := make([]*Channel, 0, len(m.chans))
	for _, c := range m.chans {
		chans = append(chans, c)
	}
	m.mu.Unlock()
	sort.Slice(chans, func(i, j int) bool { return chans[i].ID() < chans[j].ID() })
	for _, c := range chans {
		if c.vs.chosen != nil {
			return c.vs.chosen.Name
		}
	}
	return ""
}

func (m *Monitor) ListenAndServe(ctx context.Context, addr string) error {
	return m.w.ListenAndServe(ctx, addr, func(ctx context.Context) { _ = m.Run(ctx) })
}

func (m *Monitor) Run(ctx context.Context) error {
	kctx, stop := context.WithCancel(ctx)
	defer stop()
	if m.led != nil {
		go m.led.Keep(kctx, m.eng, func(f string, v ...any) { m.addLog("warn", "system", f, v...) })
	}
	err := m.eng.Run(ctx)
	if lerr := m.led.Finish(m.eng); lerr != nil {
		m.addLog("warn", "system", "台帳を書けませんでした: %v", lerr)
	}
	m.setMeta(func(mt *Meta) {
		mt.Finished = true
		for i := range mt.Inputs {
			mt.Inputs[i].Finished = true
		}
	})
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
	m.refreshInputs()
	s := Snapshot{Generator: credit.Generator, Meta: m.meta, Logs: append([]LogLine{}, m.logs...), Recs: append([]Rec{}, m.recs...),
		Frames: append([]FrameRec{}, m.frames...), Msgs: append([]MsgRec{}, m.msgs...), Txs: append([]TxRec{}, m.txs...),
		Signals: map[string]Signal{}, Ext: map[string]any{}, Stations: []Station{}}
	s.Meta.Channels = append([]float64{}, m.meta.Channels...)
	for k, v := range m.signals {
		s.Signals[strconv.Itoa(k)] = v
	}
	for k, v := range m.dataEvs {
		s.Ext[k] = append([]*DataEvent{}, v...)
	}
	m.mu.Unlock()
	sort.Slice(chans, func(i, j int) bool { return chans[i].ID() < chans[j].ID() })
	for _, c := range chans {
		s.Stations = append(s.Stations, c.station())
	}
	return s
}


func (m *Monitor) onStart(describe string, rate float64) {
	m.setMeta(func(mt *Meta) { mt.Input, mt.SampleRate = describe, rate })
	m.addLog("info", "system", "受信開始: %s（%.0f Hz）", describe, rate)
}

func putBy[T any](a []T, r T, same func(T) bool, limit int) []T {
	for i := len(a) - 1; i >= 0 && i >= len(a)-200; i-- {
		if same(a[i]) {
			a[i] = r
			return a
		}
	}
	a = append(a, r)
	if len(a) > limit {
		a = append(a[:0:0], a[len(a)-limit:]...)
	}
	return a
}

func (m *Monitor) onEmit(idx int, ev any) {
	e, ok := ev.(Event)
	if !ok {
		return
	}
	e.Idx = idx
	m.mu.Lock()
	switch {
	case e.Log != nil:
		m.logs = append(m.logs, *e.Log)
		if len(m.logs) > maxLogs {
			m.logs = m.logs[len(m.logs)-maxLogs:]
		}
	case e.Rec != nil:
		r := *e.Rec
		m.recs = putBy(m.recs, r, func(x Rec) bool { return x.Base == r.Base }, maxRecs)
	case e.Frame != nil:
		r := *e.Frame
		m.frames = putBy(m.frames, r, func(x FrameRec) bool { return x.No == r.No }, maxFrames)
	case e.Msg != nil:
		r := *e.Msg
		m.msgs = putBy(m.msgs, r, func(x MsgRec) bool { return x.No == r.No }, maxMsgs)
	case e.Tx != nil:
		r := *e.Tx
		m.txs = putBy(m.txs, r, func(x TxRec) bool { return x.Idx == r.Idx && x.ID == r.ID }, maxTxs)
	case e.Signal != nil:
		m.signals[idx] = *e.Signal
	case e.Data != nil:
		l := append(m.dataEvs[e.Type], e.Data)
		if len(l) > maxData {
			l = l[len(l)-maxData:]
		}
		m.dataEvs[e.Type] = l
	}
	m.mu.Unlock()
	if e.Data != nil {
		m.w.Publish(struct {
			Type string     `json:"type"`
			Idx  int        `json:"idx"`
			Data *DataEvent `json:"data"`
		}{e.Type, idx, e.Data})
		return
	}
	m.w.Publish(e)
}

func (m *Monitor) onAudio(idx int, pcm []int16, rate int) {
	if m.cfg.AudioTap != nil {
		m.cfg.AudioTap(idx, pcm, rate)
	}
	m.mu.Lock()
	seq := m.audioSeq[idx]
	m.audioSeq[idx] = seq + int64(len(pcm))
	m.mu.Unlock()
	b := make([]byte, 12+2*len(pcm))
	binary.LittleEndian.PutUint64(b[0:8], uint64(seq))
	binary.LittleEndian.PutUint16(b[8:10], uint16(idx))
	binary.LittleEndian.PutUint16(b[10:12], uint16(rate))
	for i, v := range pcm {
		binary.LittleEndian.PutUint16(b[12+2*i:], uint16(v))
	}
	m.audio.Broadcast(b)
}

func (m *Monitor) opened(c *Channel) {
	m.mu.Lock()
	var gone []Station
	for id, o := range m.chans {
		o.mu.Lock()
		stale := !o.watched
		o.mu.Unlock()
		if stale && math.Abs(o.MHz()-c.MHz())*1e6 < GridHz/2 {
			delete(m.chans, id)
			gone = append(gone, Station{Idx: id, MHz: o.MHz()})
		}
	}
	m.chans[c.ID()] = c
	m.meta.Channels = channelList(m.chans)
	if m.std.selected.Load() < 0 {
		m.std.selected.Store(int64(c.ID()))
	}
	meta := m.meta
	m.mu.Unlock()
	for i := range gone {
		m.w.Publish(Event{Type: EvStationGone, Idx: gone[i].Idx, Station: &gone[i]})
	}
	m.w.Publish(Event{Type: EvMeta, Idx: -1, Meta: &meta})
	init := "当てる"
	if v, ok := c.keyer.Init(); ok && c.keyer.Fixed() {
		init = fmt.Sprintf("0x%03X（%s）", v, c.keyer.Why())
	}
	m.addLogFor(c.ID(), "info", "system", "%.5f MHz の監視を始めた（オフセット %+.1f kHz・ホワイトニングの初期値 %s）", c.MHz(), c.cfg.OffHz/1e3, init)
}

func (m *Monitor) closed(c *Channel, dropped bool) {
	if !dropped {
		return
	}
	c.mu.Lock()
	keep := c.st.RICHOK > 0 && !c.forget
	c.mu.Unlock()
	m.mu.Lock()
	for _, o := range m.chans {
		if o != c && math.Abs(o.MHz()-c.MHz())*1e6 < GridHz/2 {
			keep = false
		}
	}
	m.mu.Unlock()
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

func (m *Monitor) addLog(level, group, format string, a ...any) {
	m.addLogFor(-1, level, group, format, a...)
}

func (m *Monitor) addLogFor(idx int, level, group, format string, a ...any) {
	m.mu.Lock()
	var t float64
	for _, c := range m.chans {
		t = math.Max(t, c.tNow())
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
	m.refreshInputs()
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

var reSDRSharp = regexp.MustCompile(`(\d{2})-(\d{2})-(\d{2})_(\d{2})-(\d{2})-(\d{4})`)

func timeOrigin(src string) (time.Time, bool) {
	if src == "" || iqsrc.IsNetwork(src) {
		return time.Now(), false
	}
	if n, ok := rec.Parse(src); ok && !n.Time.IsZero() {
		return n.Time, false
	}
	if mm := reSDRSharp.FindStringSubmatch(filepath.Base(src)); mm != nil {
		a := make([]int, 6)
		for i := range a {
			a[i], _ = strconv.Atoi(mm[i+1])
		}
		return time.Date(a[5], time.Month(a[4]), a[3], a[0], a[1], a[2], 0, time.Local), false
	}
	if st, err := os.Stat(src); err == nil {
		return st.ModTime(), true
	}
	return time.Now(), true
}

type LockState struct {
	MHz   float64
	Lock  bool
	Score float64
	Label string
}

func (m *Monitor) Locks() []LockState {
	m.mu.Lock()
	chans := make([]*Channel, 0, len(m.chans))
	for _, c := range m.chans {
		chans = append(chans, c)
	}
	m.mu.Unlock()
	sort.Slice(chans, func(i, j int) bool { return chans[i].ID() < chans[j].ID() })
	out := make([]LockState, 0, len(chans))
	for _, c := range chans {
		lock, score, label := c.Lock()
		out = append(out, LockState{MHz: c.MHz(), Lock: lock, Score: score, Label: label})
	}
	return out
}
