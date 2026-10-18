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
	"strings"
	"sync"
	"time"

	"github.com/symysak/arib/internal/credit"
	"github.com/symysak/arib/internal/iqsrc"
	"github.com/symysak/arib/internal/rec"
	"github.com/symysak/arib/internal/rx"
	"github.com/symysak/arib/internal/std-t79/dsp"
	"github.com/symysak/arib/internal/std-t79/l1"
	"github.com/symysak/arib/internal/voice"
	"github.com/symysak/arib/internal/webui"
)

type Config struct {
	Engine rx.Config
	NoTCH bool
	DirectScan bool
	IdleTCH bool
	Codec    string
	CodecOpt voice.Options
	AudioTap func(idx int, pcm []int16, rate int)
	LedgerDir string
	Uplink *rx.Config
	Direct []DirectStation
}

const UpOffsetMHz = -9.0

func IsUplinkMHz(mhz float64) bool { return mhz >= 259.5 && mhz < 267 }

const SystemUp = System + "-up"

const (
	maxLogs  = 400
	maxRecs  = 200
	maxL1    = 2000
	maxL2    = 500
	maxL3    = 500
	maxCalls = 400
)

func putByNo[T any](a []T, r T, no func(T) int64, limit int) []T {
	n := no(r)
	for i := len(a) - 1; i >= 0 && i >= len(a)-200; i-- {
		if no(a[i]) == n {
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

type Monitor struct {
	cfg Config
	std *Standard
	eng *rx.Wide
	hasUp bool
	w     *webui.Server
	led   *rx.Ledger
	audio    *webui.Hub
	audioSeq map[int]int64
	added    map[int64]bool
	l1       []L1Rec
	l2       []L2Rec
	l3       []L3Rec
	calls    []CallRec
	nets     map[int]NetInfo

	mu    sync.Mutex
	meta  Meta
	chans map[int]*Channel
	logs  []LogLine
	recs  []Rec
}

func NewMonitor(cfg Config) *Monitor {
	ec := cfg.Engine
	t0, est := timeOrigin(ec.Source)
	m := &Monitor{cfg: cfg, chans: map[int]*Channel{}, audio: webui.NewHub(), audioSeq: map[int]int64{}, nets: map[int]NetInfo{}}
	m.std = NewStandard(StdConfig{NoTCH: cfg.NoTCH, DirectScan: cfg.DirectScan, IdleTCH: cfg.IdleTCH, LogDir: ec.LogDir, Codec: cfg.Codec, CodecOpt: cfg.CodecOpt,
		Lossless: !iqsrc.IsNetwork(ec.Source)})
	m.std.audio = m.onAudio
	m.std.hooks = &stdHooks{opened: m.opened, closed: m.closed, assigned: m.onAssigned, direct: m.onDirect, toDirect: m.onToDirect,
		downPaired: m.downPaired}
	chans := append([]float64{}, ec.Channels...)
	sort.Float64s(chans)
	m.meta = Meta{
		Input: ec.Source, CenterMHz: ec.CenterHz / 1e6, SampleRate: ec.SampleRate, GainDB: ec.GainDB,
		Channels: chans, LogDir: ec.LogDir, T0WallMS: t0.UnixMilli(), Estimated: est,
		Network: iqsrc.IsNetwork(ec.Source), Scan: ec.Scan.Enabled,
		System:     "ARIB STD-T79（都道府県・市町村デジタル移動通信システム TYPE1。π/4 QPSK 16 ksym/s・4 スロット TDMA）",
		SymbolRate: dsp.SymbolRate, SlotBits: l1.SlotBits, Codecs: CodecNames(), CodecMode: cfg.Codec,
	}
	ec.System = System
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
		m.std.ledgerOrgs = led.Orgs
		if n := led.Restore(&ec); n > 0 {
			notes = append(notes, fmt.Sprintf("台帳: %d 局を起動時から監視します（%s）", n, led.Path()))
		}
	}
	if cfg.Uplink != nil {
		uc := *cfg.Uplink
		ec.Inputs = append(ec.Inputs, rx.InputConfig{Label: "上り", Source: uc.Source, SampleRate: uc.SampleRate, Format: uc.Format,
			CenterHz: uc.CenterHz, GainDB: uc.GainDB, AGC: uc.AGC, Open: uc.Open, Scan: uc.Scan})
		ec.Channels = append(ec.Channels, uc.Channels...)
	}
	ec.OnInputStart = m.onInputStart
	m.eng = rx.NewWide(m.std, ec)
	for i := 0; i < m.eng.NumInputs(); i++ {
		in := m.eng.Input(i).Status()
		up := IsUplinkMHz(in.CenterHz / 1e6)
		label := m.eng.InputLabel(i)
		if i == 0 || strings.HasPrefix(label, "入力 ") {
			label = "下り"
			if up {
				label = "上り"
			}
			if m.eng.NumInputs() == 1 {
				label = "入力"
			}
			m.eng.SetLabel(i, label)
		}
		if up {
			m.hasUp = true
			m.meta.UpInput, m.meta.UpCenterMHz = in.Input, in.CenterHz/1e6
			notes = append(notes, fmt.Sprintf("上りも受けます: %s（%s、中心 %.4f MHz。下りの局・無線チャネル指定の周波数 %+.0f MHz に上りの局を足す）",
				in.Input, label, in.CenterHz/1e6, UpOffsetMHz))
		}
		m.meta.Inputs = append(m.meta.Inputs, InputMeta{ID: inputID(i), Label: label, Input: in.Input, CenterMHz: in.CenterHz / 1e6,
			SampleRate: in.SampleRate, GainDB: in.GainDB, AGC: in.AGC, Up: up})
	}
	if m.hasUp {
		for _, f := range append(append([]float64(nil), ec.Channels...), ec.Ledger...) {
			if !IsUplinkMHz(f) {
				m.addUp(f)
			}
		}
	}
	for _, d := range cfg.Direct {
		o := d.Opt
		if err := m.eng.Add(d.MHz, &o); err != nil {
			notes = append(notes, fmt.Sprintf("直接通信の %.4f MHz は監視しない: %v", d.MHz, err))
		} else {
			kind := o.Mode.String()
			if o.Control {
				kind = "直接通信（制御用）"
			}
			notes = append(notes, fmt.Sprintf("%.4f MHz を%sとして監視します", d.MHz, kind))
		}
	}
	m.w = webui.New(webui.Config{
		Panes:    []fs.FS{m.std.Pane()},
		Snapshot: func() any { s := m.Snapshot(); return Event{Type: EvSnapshot, Idx: -1, Snapshot: &s} },
		Status:   func() any { return m.Snapshot() },
		FileDir:  func() string { return ec.LogDir },
		Inputs:   m.inputsOf(),
	})
	m.w.Mux().HandleFunc("/ws/audio", func(w http.ResponseWriter, r *http.Request) { m.w.Serve(m.audio, w, r, true) })
	m.w.Mux().HandleFunc("/api/select", func(w http.ResponseWriter, r *http.Request) {
		v, err := strconv.Atoi(r.URL.Query().Get("idx"))
		if err != nil {
			http.Error(w, "idx が不正です", http.StatusBadRequest)
			return
		}
		m.std.selected.Store(int64(v))
		w.WriteHeader(http.StatusNoContent)
	})
	m.w.Mux().HandleFunc("/api/audio-slots", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		idx, err := strconv.Atoi(q.Get("idx"))
		if err != nil {
			http.Error(w, "idx が不正です", http.StatusBadRequest)
			return
		}
		var slots [4]bool
		for _, f := range strings.Split(q.Get("slots"), ",") {
			if f == "" {
				continue
			}
			n, err := strconv.Atoi(f)
			if err != nil || n < 0 || n > 3 {
				http.Error(w, "slots は 0〜3 のカンマ区切り", http.StatusBadRequest)
				return
			}
			slots[n] = true
		}
		m.mu.Lock()
		c := m.chans[idx]
		m.mu.Unlock()
		if c == nil {
			http.Error(w, "その局はありません", http.StatusNotFound)
			return
		}
		if err := c.SetAudioSlots(q.Get("mode"), slots); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		m.onEmit(idx, Event{Type: EvT79, Data: &Detail{Carrier: c.snapshot().Carrier}})
		stn := c.station()
		m.onEmit(idx, Event{Type: EvStation, Station: &stn})
		w.WriteHeader(http.StatusNoContent)
	})
	m.w.Mux().HandleFunc("/api/l1all", func(w http.ResponseWriter, r *http.Request) {
		on := r.URL.Query().Get("on") == "1"
		m.std.l1All.Store(on)
		m.setMeta(func(mt *Meta) { mt.L1All = on })
		w.WriteHeader(http.StatusNoContent)
	})
	for _, n := range notes {
		m.addLog("info", "system", "%s", n)
	}
	return m
}

func (m *Monitor) HeardCodec() string {
	m.mu.Lock()
	chans := make([]*Channel, 0, len(m.chans))
	for _, c := range m.chans {
		chans = append(chans, c)
	}
	m.mu.Unlock()
	sort.Slice(chans, func(i, j int) bool { return chans[i].ID() < chans[j].ID() })
	for _, c := range chans {
		c.mu.Lock()
		n := c.codecName
		c.mu.Unlock()
		if n != "" {
			return n
		}
	}
	return ""
}

func (m *Monitor) onAssigned(mhz float64) {
	m.mu.Lock()
	for _, c := range m.chans {
		if math.Abs(c.MHz()-mhz) < matchMHz {
			m.mu.Unlock()
			return
		}
	}
	if m.added == nil {
		m.added = map[int64]bool{}
	}
	k := int64(math.Round(mhz * 1e4))
	if m.added[k] {
		m.mu.Unlock()
		return
	}
	m.added[k] = true
	m.mu.Unlock()
	if err := m.eng.Add(mhz, nil); err == nil {
		m.addLog("info", "call", "無線チャネル指定の %.4f MHz を監視に加えた", mhz)
	}
	m.addUp(mhz)
}

func (m *Monitor) downPaired(up float64) bool {
	down := up - UpOffsetMHz
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, c := range m.chans {
		if !c.isUp && math.Abs(c.MHz()-down) < matchMHz {
			return true
		}
	}
	return false
}

func (m *Monitor) addUp(down float64) {
	if !m.hasUp {
		return
	}
	up := down + UpOffsetMHz
	m.mu.Lock()
	for _, c := range m.chans {
		if c.isUp && math.Abs(c.MHz()-up) < matchMHz {
			m.mu.Unlock()
			return
		}
	}
	if m.added == nil {
		m.added = map[int64]bool{}
	}
	k := -int64(math.Round(up * 1e4))
	if m.added[k] {
		m.mu.Unlock()
		return
	}
	m.added[k] = true
	m.mu.Unlock()
	if err := m.eng.Add(up, nil); err != nil {
		m.addLog("info", "system", "上りの %.4f MHz（下り %.4f MHz の対）は監視しない: %v", up, down, err)
	}
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

func (m *Monitor) Handler() http.Handler { return m.w }

func (m *Monitor) Engine() *rx.Wide { return m.eng }

func (m *Monitor) Addr() string { return m.w.Addr() }

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
	m.setMeta(func(mt *Meta) {
		for i := range mt.Inputs {
			mt.Inputs[i].Finished = true
		}
	})
	if lerr := m.led.Finish(m.eng); lerr != nil {
		m.addLog("warn", "system", "台帳を書けませんでした: %v", lerr)
	}
	m.setMeta(func(mt *Meta) { mt.Finished = true })
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
		Recs: append([]Rec{}, m.recs...), Stations: []Station{}, T79: map[string]ChannelSnapshot{},
		L1: append([]L1Rec{}, m.l1...), L2: append([]L2Rec{}, m.l2...), L3: append([]L3Rec{}, m.l3...),
		Calls: append([]CallRec{}, m.calls...), Nets: []NetInfo{}}
	for _, n := range m.nets {
		s.Nets = append(s.Nets, n)
	}
	sort.Slice(s.Nets, func(i, j int) bool { return s.Nets[i].MHz < s.Nets[j].MHz })
	s.Meta.Channels = append([]float64{}, m.meta.Channels...)
	m.mu.Unlock()
	sort.Slice(chans, func(i, j int) bool { return chans[i].ID() < chans[j].ID() })
	for _, c := range chans {
		s.Stations = append(s.Stations, c.station())
		s.T79[strconv.Itoa(c.ID())] = c.snapshot()
	}
	return s
}


func (m *Monitor) onStart(string, float64) {}

func (m *Monitor) onInputStart(i int, describe string, rate float64) {
	m.setInput(inputID(i), func(in *InputMeta) { in.Input, in.SampleRate = describe, rate })
	st := m.eng.Input(i).Status()
	m.addLog("info", "system", "%s の受信開始: %s（%.0f Hz、中心 %.5f MHz）", m.eng.InputLabel(i), describe, rate, st.CenterHz/1e6)
}

func (m *Monitor) onEmit(ch int, ev any) {
	e, ok := ev.(Event)
	if !ok {
		return
	}
	e.Idx = ch
	switch {
	case e.Log != nil:
		m.mu.Lock()
		m.logs = append(m.logs, *e.Log)
		if len(m.logs) > maxLogs {
			m.logs = m.logs[len(m.logs)-maxLogs:]
		}
		m.mu.Unlock()
	case e.L1 != nil:
		m.mu.Lock()
		m.l1 = putByNo(m.l1, *e.L1, func(r L1Rec) int64 { return r.No }, maxL1)
		m.mu.Unlock()
	case e.L2 != nil:
		m.mu.Lock()
		m.l2 = putByNo(m.l2, *e.L2, func(r L2Rec) int64 { return r.No }, maxL2)
		m.mu.Unlock()
	case e.L3 != nil:
		m.mu.Lock()
		m.l3 = putByNo(m.l3, *e.L3, func(r L3Rec) int64 { return r.No }, maxL3)
		m.mu.Unlock()
	case e.Call != nil:
		m.mu.Lock()
		m.calls = append(m.calls, *e.Call)
		if len(m.calls) > maxCalls {
			m.calls = m.calls[len(m.calls)-maxCalls:]
		}
		m.mu.Unlock()
	case e.Net != nil:
		m.mu.Lock()
		m.nets[e.Net.Idx] = *e.Net
		m.mu.Unlock()
	case e.Rec != nil:
		m.mu.Lock()
		hit := false
		for i := range m.recs {
			if m.recs[i].Base == e.Rec.Base {
				m.recs[i], hit = *e.Rec, true
			}
		}
		if !hit {
			m.recs = append(m.recs, *e.Rec)
			if len(m.recs) > maxRecs {
				m.recs = m.recs[len(m.recs)-maxRecs:]
			}
		}
		m.mu.Unlock()
	}
	m.w.Publish(e)
}

func (m *Monitor) opened(c *Channel) {
	m.std.selected.CompareAndSwap(-1, int64(c.ID()))
	if !c.isUp {
		defer m.addUp(c.MHz())
	}
	m.mu.Lock()
	m.chans[c.ID()] = c
	m.meta.Channels = channelList(m.chans)
	meta := m.meta
	m.mu.Unlock()
	m.w.Publish(Event{Type: EvMeta, Idx: -1, Meta: &meta})
	st := c.station()
	m.w.Publish(Event{Type: EvStation, Idx: c.ID(), Station: &st})
	m.addLogFor(c.ID(), "info", "system", "%.5f MHz の監視を始めた（オフセット %+.1f kHz）", c.MHz(), c.cfg.OffHz/1e3)
}

func (m *Monitor) closed(c *Channel, dropped bool) {
	if !dropped {
		return
	}
	locked, _, _ := c.Lock()
	n, known := c.NominalMHz()
	dup := known && math.Abs(n-c.MHz()) > 1e-7
	c.mu.Lock()
	keep := c.car.SWFound > 0 && !c.forget && !dup && (!c.isUp || locked)
	c.mu.Unlock()
	if keep {
		st := c.station()
		m.w.Publish(Event{Type: EvStation, Idx: c.ID(), Station: &st})
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
	if !c.isUp && m.hasUp {
		up := c.MHz() + UpOffsetMHz
		m.mu.Lock()
		for _, u := range m.chans {
			if u.isUp && math.Abs(u.MHz()-up) < matchMHz {
				u.mu.Lock()
				u.forget = u.car.SWFound == 0
				u.mu.Unlock()
			}
		}
		delete(m.added, -int64(math.Round(up*1e4)))
		m.mu.Unlock()
		m.eng.Remove(up)
	}
	if c.isUp && m.hasUp {
		var again []float64
		m.mu.Lock()
		for _, d := range m.chans {
			p := d.MHz() + UpOffsetMHz
			if !d.isUp && math.Abs(p-c.MHz()) < matchMHz && math.Abs(p-c.MHz()) > 1e-6 {
				again = append(again, d.MHz())
			}
		}
		m.mu.Unlock()
		for _, f := range again {
			m.addUp(f)
		}
	}
}

func (m *Monitor) addLog(level, group, format string, a ...any) {
	m.addLogFor(-1, level, group, format, a...)
}

func (m *Monitor) addLogFor(idx int, level, group, format string, a ...any) {
	m.mu.Lock()
	var t float64
	for _, c := range m.chans {
		c.mu.Lock()
		t = math.Max(t, c.nowT)
		c.mu.Unlock()
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
