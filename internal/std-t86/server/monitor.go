package server

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/symysak/arib/internal/credit"
	"github.com/symysak/arib/internal/iqsrc"
	"github.com/symysak/arib/internal/rec"
	"github.com/symysak/arib/internal/rx"
	"github.com/symysak/arib/internal/std-t86/decoder"
	"github.com/symysak/arib/internal/webui"
)


type Config struct {
	Engine rx.Config
	Defaults ChannelOpts
	Channels map[float64]ChannelOpts
	Title string
	LedgerDir string
	AudioTap func(idx int, pcm []int16, rate int)
}

type Meta struct {
	Input      string    `json:"input"`
	CenterMHz  float64   `json:"center_mhz"`
	SampleRate float64   `json:"sample_rate"`
	GainDB     float64   `json:"gain_db"`
	Channels   []float64 `json:"channels"`
	LogDir     string    `json:"log_dir"`
	Method     string    `json:"method"`
	T0WallMS   int64     `json:"t0_wall_ms"`
	Estimated  bool      `json:"t0_estimated"`
	T0Source   string    `json:"t0_source"`
	Finished   bool      `json:"finished"`
}

type Station struct {
	Idx       int      `json:"idx"`
	MHz       float64  `json:"mhz"`
	Synced    bool     `json:"synced"`
	Recording bool     `json:"recording"`
	LevelDB   *float64 `json:"level_db"`
	LastTSec  float64  `json:"last_t_sec"`
	Watched   bool     `json:"watched"`
	Slots [slotCount]string `json:"slots"`
	Ext   map[string]any    `json:"ext"`
}

type LogLine struct {
	TSec  float64 `json:"t_sec"`
	Idx   int     `json:"idx"`
	Group string  `json:"group"`
	Text  string  `json:"text"`
}

type Rec struct {
	Idx  int    `json:"idx"`
	Base string `json:"base"`
	Name     string  `json:"name"`
	TSec     float64 `json:"t_sec"`
	Open     bool    `json:"open"`
	Reason   string  `json:"reason"`
	Sec      float64 `json:"sec"`
	WindowID int     `json:"window_id"`
	Audio    string  `json:"audio"`
	AudioSec float64 `json:"audio_sec"`
}

type webEvent struct {
	Type     string       `json:"type"`
	TSec     float64      `json:"t_sec,omitempty"`
	Snapshot *webSnapshot `json:"snapshot,omitempty"`
	Meta     *Meta        `json:"meta,omitempty"`
	Station  *Station     `json:"station,omitempty"`
	Rec      *Rec         `json:"rec,omitempty"`
	Log      *LogLine     `json:"log,omitempty"`
}

type t86Event struct {
	Type string `json:"type"`
	Idx  int    `json:"idx"`
	Ev   Event  `json:"ev"`
}

type webSnapshot struct {
	Generator string              `json:"generator"`
	Meta      Meta                `json:"meta"`
	Stations  []Station           `json:"stations"`
	Logs      []LogLine           `json:"logs"`
	Recs      []Rec               `json:"recs"`
	T86       map[string]snapshot `json:"t86"`
}

const (
	stationTick = 200 * time.Millisecond
	maxLogs     = 600
	maxRecs     = 200
)

type App struct {
	cfg   Config
	std   *Standard
	eng   *rx.Wide
	w     *webui.Server
	audio *webui.Hub
	led   *rx.Ledger

	mu       sync.Mutex
	meta     Meta
	stations map[int]*monStation
	logs     []LogLine
	recs     []*Rec
	done     chan struct{}
	err      error
	tick     sync.Once
	tap      func(idx int, ev any)
	tapAudio func(idx int, pcm []int16)
}

type monStation struct {
	ch       *Channel
	st       Station
	last     []byte
	audioSeq int64
	targets  map[int]string
	recs     map[int]*Rec
}

func New(cfg Config) *App {
	a := &App{cfg: cfg, stations: map[int]*monStation{}, done: make(chan struct{}), audio: webui.NewHub()}
	a.std = NewStandard(cfg.Defaults, cfg.Channels)
	origin := liveTimeOrigin()
	if !iqsrc.IsNetwork(cfg.Engine.Source) {
		origin = fileTimeOrigin(cfg.Engine.Source)
	}
	logDir := cfg.Engine.LogDir
	if abs, err := filepath.Abs(logDir); err == nil {
		logDir = abs
	}
	cfg.Engine.LogDir = logDir
	cfg.Engine.System = System
	a.meta = Meta{Input: cfg.Engine.Source, CenterMHz: cfg.Engine.CenterHz / 1e6, GainDB: cfg.Engine.GainDB,
		Channels: []float64{}, LogDir: logDir, Method: "STD-T86",
		T0WallMS: origin.WallMS, Estimated: origin.Estimated, T0Source: origin.Source}
	a.std.setEnv(func(e *channelEnv) { e.origin, e.sourceDesc = origin, cfg.Engine.Source })
	a.std.onNew = a.addStation
	a.std.onClose = a.dropStation

	ec := cfg.Engine
	ec.TimeOrigin = time.UnixMilli(origin.WallMS)
	ec.OnStart = func(describe string, rate float64) {
		a.mu.Lock()
		a.meta.Input, a.meta.SampleRate = describe, rate
		a.mu.Unlock()
		desc := fmt.Sprintf("%s (fs=%.4gMS/s)", describe, rate/1e6)
		if cfg.Title != "" {
			desc += " " + cfg.Title
		}
		a.std.setEnv(func(e *channelEnv) { e.sourceDesc = desc })
	}
	ec.OnEmit = a.onEmit
	ec.OnAudio = a.onAudio
	var notes []string
	if led, err := rx.OpenLedger(cfg.LedgerDir, a.std.Grid()); err != nil {
		notes = append(notes, fmt.Sprintf("台帳を開けませんでした（台帳なしで続けます）: %v", err))
	} else if led != nil {
		a.led = led
		if n := led.Restore(&ec); n > 0 {
			notes = append(notes, fmt.Sprintf("台帳: %d 局を起動時から監視します（%s）", n, led.Path()))
		}
	}
	a.eng = rx.NewWide(a.std, ec)

	a.w = webui.New(webui.Config{
		Panes:    []fs.FS{a.std.Pane()},
		Snapshot: func() any { s := a.Snapshot(); return webEvent{Type: "snapshot", Snapshot: &s} },
		Status:   func() any { return a.Snapshot() },
		FileDir:  func() string { return logDir },
		Control:  webui.WideControl(func() *rx.Wide { return a.eng }),
		Inputs:   webui.WideInputs(func() *rx.Wide { return a.eng }, 1+len(ec.Inputs)),
	})
	mux := a.w.Mux()
	mux.HandleFunc("/ws/audio", func(w http.ResponseWriter, r *http.Request) { a.w.Serve(a.audio, w, r, true) })
	a.std.Routes(mux, a.eng.Channel)
	for _, n := range notes {
		a.addLog(-1, "system", 0, n)
	}
	return a
}

func (a *App) Engine() *rx.Wide { return a.eng }

func (a *App) Handler() http.Handler { return a.w }

func (a *App) Addr() string { return a.w.Addr() }

func (a *App) Done() <-chan struct{} { return a.done }

func (a *App) Run(ctx context.Context) error {
	kctx, stop := context.WithCancel(ctx)
	defer stop()
	if a.led != nil {
		go a.led.Keep(kctx, a.eng, func(f string, v ...any) { a.addLog(-1, "system", 0, fmt.Sprintf(f, v...)) })
	}
	err := a.eng.Run(ctx)
	if err := a.led.Finish(a.eng); err != nil {
		a.addLog(-1, "system", 0, fmt.Sprintf("台帳を書けませんでした: %v", err))
	}
	a.finish(err)
	return err
}

func (a *App) ListenAndServe(ctx context.Context, addr string) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	var runErr error
	err := a.w.ListenAndServe(ctx, addr, func(ctx context.Context) {
		if runErr = a.Run(ctx); runErr != nil {
			cancel()
		}
	})
	if runErr != nil {
		return runErr
	}
	return err
}

func (a *App) finish(err error) {
	a.mu.Lock()
	a.meta.Finished = true
	a.err = err
	m := a.meta
	a.mu.Unlock()
	a.w.Publish(webEvent{Type: "meta", Meta: &m})
	if err != nil {
		a.addLog(-1, "system", 0, "受信を終えました: "+err.Error())
	} else {
		a.addLog(-1, "system", 0, "入力が終端に達しました")
	}
	select {
	case <-a.done:
	default:
		close(a.done)
	}
}


func (a *App) addStation(ch *Channel) {
	a.mu.Lock()
	ms := &monStation{ch: ch, targets: map[int]string{}, recs: map[int]*Rec{},
		st: Station{Idx: ch.id, MHz: ch.mhz, Watched: true, Ext: map[string]any{}}}
	a.stations[ch.id] = ms
	a.meta.Channels = append(a.meta.Channels, ch.mhz)
	sort.Float64s(a.meta.Channels)
	m := a.meta
	a.mu.Unlock()
	a.w.Publish(webEvent{Type: "meta", Meta: &m})
	a.publishStation(ms, true)
	a.startTick()
}

func (a *App) dropStation(ch *Channel, dropped bool) {
	if !dropped {
		a.publishStation(a.station(ch.id), true)
		return
	}
	a.mu.Lock()
	ms := a.stations[ch.id]
	if ms == nil || ms.ch != ch {
		a.mu.Unlock()
		return
	}
	delete(a.stations, ch.id)
	for i, f := range a.meta.Channels {
		if f == ch.mhz {
			a.meta.Channels = append(a.meta.Channels[:i], a.meta.Channels[i+1:]...)
			break
		}
	}
	a.mu.Unlock()
	a.w.Publish(webEvent{Type: "station_gone", Station: &Station{Idx: ch.id, MHz: ch.mhz}})
	a.addLog(ch.id, "station", ch.state.currentT(), fmt.Sprintf("%.4f MHz の監視をやめた", ch.mhz))
}

func (a *App) station(id int) *monStation {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.stations[id]
}

func (a *App) startTick() {
	a.tick.Do(func() { go a.tickLoop() })
}

func (a *App) tickLoop() {
	{
		t := time.NewTicker(stationTick)
		defer t.Stop()
		for range t.C {
			a.mu.Lock()
			list := make([]*monStation, 0, len(a.stations))
			for _, ms := range a.stations {
				list = append(list, ms)
			}
			a.mu.Unlock()
			for _, ms := range list {
				a.publishStation(ms, false)
			}
			select {
			case <-a.done:
				for _, ms := range list {
					a.publishStation(ms, true)
				}
				return
			default:
			}
		}
	}
}

func stationOf(ms *monStation) Station {
	st := ms.ch.state
	st.mu.Lock()
	now := max(st.t, st.slots.last)
	out := Station{Idx: ms.st.Idx, MHz: ms.st.MHz, Watched: true,
		Synced: st.syncLocked, LastTSec: round(now, 2), Recording: st.broadcast.Active,
		Slots: st.slots.view(now, st.syncLocked, st.broadcast.Active)}
	if st.levelDBFS != nil {
		v := round(*st.levelDBFS, 1)
		out.LevelDB = &v
	}
	ext := map[string]any{"talking": st.broadcast.Active}
	if st.seed != decoder.SeedAuto {
		ext["seed"] = st.seed
	}
	if st.municipalityCode != 0 {
		ext["municipality_code"] = st.municipalityCode
	}
	st.mu.Unlock()
	out.Ext = ext
	return out
}

func (a *App) publishStation(ms *monStation, force bool) {
	if ms == nil {
		return
	}
	st := stationOf(ms)
	b, _ := json.Marshal(st)
	a.mu.Lock()
	same := string(b) == string(ms.last)
	ms.last, ms.st = b, st
	a.mu.Unlock()
	if same && !force {
		return
	}
	a.w.Publish(webEvent{Type: "station", Station: &st})
}


func (a *App) onEmit(idx int, ev any) {
	if a.tap != nil {
		a.tap(idx, ev)
	}
	a.w.Publish(t86Event{Type: "t86", Idx: idx, Ev: ev})
	ms := a.station(idx)
	if ms == nil {
		return
	}
	switch e := ev.(type) {
	case logEvent:
		a.addLog(idx, logGroup(e.Text), e.T, e.Text)
	case broadcastEvent:
		label := ""
		if e.Target != nil {
			label = e.Target.Label
		}
		a.mu.Lock()
		if label != "" {
			ms.targets[e.WindowID] = label
		} else {
			label = ms.targets[e.WindowID]
		}
		a.mu.Unlock()
		switch e.Type {
		case "broadcast_start":
			txt := fmt.Sprintf("◆ 通報開始 #%d", e.WindowID)
			if label != "" {
				txt += "（報知対象: " + label + "）"
			}
			a.addLog(idx, "call", e.T, txt)
		case "broadcast_end":
			a.addLog(idx, "call", e.T, fmt.Sprintf("◇ 通報終了 #%d", e.WindowID))
		}
		a.publishStation(ms, true)
	case iqStatusEvent:
		a.updateRec(ms, e.WindowID, func(r *Rec) {
			r.Base = strings.TrimSuffix(e.Path, "."+string(rec.IQ))
			r.Name = rec.Rel(a.meta.LogDir, r.Base)
			r.Sec, r.Open = e.Seconds, !e.Done
		})
		if e.Done {
			a.addLog(idx, "record", ms.ch.state.currentT(),
				fmt.Sprintf("◎ 通報 #%d の I/Q を確定（%.1f 秒）%s", e.WindowID, e.Seconds, e.Note))
		}
	case audioStatusEvent:
		if e.WavPath != "" {
			a.updateRec(ms, e.WindowID, func(r *Rec) {
				r.Audio, r.AudioSec = rec.Rel(a.meta.LogDir, e.WavPath), e.DecodedSeconds
			})
		}
	}
}

func (a *App) updateRec(ms *monStation, wid int, f func(r *Rec)) {
	a.mu.Lock()
	r := ms.recs[wid]
	if r == nil {
		r = &Rec{Idx: ms.st.Idx, WindowID: wid, TSec: round(ms.ch.state.currentT(), 2), Open: true}
		ms.recs[wid] = r
		a.recs = append(a.recs, r)
		if len(a.recs) > maxRecs {
			a.recs = a.recs[len(a.recs)-maxRecs:]
		}
	}
	f(r)
	r.Reason = fmt.Sprintf("通報 #%d", wid)
	if l := ms.targets[wid]; l != "" {
		r.Reason += "　" + l
	}
	c := *r
	a.mu.Unlock()
	a.w.Publish(webEvent{Type: "rec", Rec: &c})
}

func logGroup(text string) string {
	switch {
	case strings.Contains(text, "通報"):
		return "call"
	case strings.Contains(text, "スクランブル値") || strings.Contains(text, "信号喪失") ||
		strings.Contains(text, "CFO") || strings.Contains(text, "市区町村"):
		return "station"
	}
	return "system"
}

func (a *App) addLog(idx int, group string, t float64, text string) {
	l := LogLine{TSec: round(t, 2), Idx: idx, Group: group, Text: text}
	a.mu.Lock()
	a.logs = append(a.logs, l)
	if len(a.logs) > maxLogs {
		a.logs = a.logs[len(a.logs)-maxLogs:]
	}
	a.mu.Unlock()
	a.w.Publish(webEvent{Type: "log", Log: &l})
}

func (a *App) onAudio(idx, _ int, pcm []int16, rate int) {
	if a.tapAudio != nil {
		a.tapAudio(idx, pcm)
	}
	if a.cfg.AudioTap != nil {
		a.cfg.AudioTap(idx, pcm, rate)
	}
	a.mu.Lock()
	ms := a.stations[idx]
	var seq int64
	if ms != nil {
		seq = ms.audioSeq
		ms.audioSeq += int64(len(pcm))
	}
	a.mu.Unlock()
	buf := make([]byte, 12+2*len(pcm))
	binary.LittleEndian.PutUint64(buf[0:], uint64(seq))
	binary.LittleEndian.PutUint16(buf[8:], uint16(idx))
	binary.LittleEndian.PutUint16(buf[10:], uint16(rate))
	for i, v := range pcm {
		binary.LittleEndian.PutUint16(buf[12+2*i:], uint16(v))
	}
	a.audio.Broadcast(buf)
}

func (a *App) Snapshot() webSnapshot {
	a.mu.Lock()
	s := webSnapshot{Generator: credit.Generator, Meta: a.meta, Logs: append([]LogLine(nil), a.logs...),
		Stations: []Station{}, Recs: []Rec{}, T86: map[string]snapshot{}}
	s.Meta.Channels = append([]float64{}, a.meta.Channels...)
	list := make([]*monStation, 0, len(a.stations))
	for _, ms := range a.stations {
		list = append(list, ms)
	}
	for _, r := range a.recs {
		s.Recs = append(s.Recs, *r)
	}
	a.mu.Unlock()
	sort.Slice(list, func(i, j int) bool { return list[i].st.MHz < list[j].st.MHz })
	for _, ms := range list {
		s.Stations = append(s.Stations, stationOf(ms))
		s.T86[strconv.Itoa(ms.st.Idx)] = ms.ch.state.snapshot()
	}
	return s
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func boolParam(r *http.Request, name string) (bool, error) {
	v := r.URL.Query().Get(name)
	if v == "" {
		return false, errors.New(name + " が必要です（?enabled=true|false）")
	}
	return strconv.ParseBool(v)
}
