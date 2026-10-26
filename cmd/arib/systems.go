package main

import (
	"context"
	"io"
	"net/http"
	"sort"
	"strings"
	"sync"

	"github.com/symysak/arib/internal/cliflag"
	"github.com/symysak/arib/internal/iqsrc"
	"github.com/symysak/arib/internal/rx"
	t115cli "github.com/symysak/arib/internal/std-t115/qpsknarrow/cli"
	t115 "github.com/symysak/arib/internal/std-t115/qpsknarrow/server"
	t61cli "github.com/symysak/arib/internal/std-t61/cli"
	dec "github.com/symysak/arib/internal/std-t61/decoder"
	t61 "github.com/symysak/arib/internal/std-t61/server"
	"github.com/symysak/arib/internal/std-t61/watch"
	t79cli "github.com/symysak/arib/internal/std-t79/cli"
	t79 "github.com/symysak/arib/internal/std-t79/server"
	t86cli "github.com/symysak/arib/internal/std-t86/cli"
	t86 "github.com/symysak/arib/internal/std-t86/server"
)

type System struct {
	Name string
	Dir string
	Label string
	Family string
	Plain bool
	Codec string
	Live func(ctx context.Context, prog string, argv []string, stderr io.Writer) int
	New func(e Env) (Monitor, error)
}

type Env struct {
	Source string
	Open func(iqsrc.Options) (iqsrc.Source, error)
	Live cliflag.Live
	Plan cliflag.Plan
	LogDir string
	System string
	LogOut io.Writer
	Audio func(pcm []int16, rate int)
}

type Monitor interface {
	Handler() http.Handler
	Run(ctx context.Context) error
	Engine() rx.Stations
	Evidence() Evidence
}

type Evidence struct {
	OK bool
	Score float64
	Text string
	Codec string
}

var (
	sysMu   sync.Mutex
	systems = map[string]*System{}
)

func register(s *System) {
	sysMu.Lock()
	defer sysMu.Unlock()
	systems[s.Name] = s
}

func lookup(name string) *System {
	sysMu.Lock()
	defer sysMu.Unlock()
	return systems[strings.ToLower(name)]
}

func allSystems() []*System {
	sysMu.Lock()
	defer sysMu.Unlock()
	out := make([]*System, 0, len(systems))
	for _, s := range systems {
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return order(out[i]) < order(out[j]) })
	return out
}

func order(s *System) string {
	pre := "9"
	switch s.Family {
	case "STD-T86":
		pre = "1"
	case "STD-T115 QPSK ナロー方式":
		pre = "2"
	case "STD-T61":
		pre = "3"
	case T79Family:
		pre = "4"
	}
	if s.Plain {
		return pre + "0" + s.Name
	}
	return pre + "1" + s.Name
}

func names() string {
	var n []string
	for _, s := range allSystems() {
		n = append(n, s.Name)
	}
	return strings.Join(n, "|")
}

func init() {
	register(&System{Name: "t86", Dir: t86.System, Label: "STD-T86", Family: "STD-T86", Plain: true, Codec: "S 方式（G.722.1）",
		Live: t86cli.Live, New: newT86})
	register(&System{Name: "t115-qpsknarrow", Dir: t115.System, Label: "STD-T115 QPSK ナロー方式", Family: "STD-T115 QPSK ナロー方式", Plain: true, Codec: "AMR-WB+",
		Live: t115cli.Live, New: newT115})
	register(&System{Name: "t61", Dir: watch.DefaultSystem, Label: "STD-T61（系統不明）", Family: "STD-T61", Plain: true,
		Live: t61cli.Live, New: func(e Env) (Monitor, error) { return NewT61(e, nil, nil), nil }})
	register(&System{Name: "t79", Dir: t79.System, Label: "STD-T79", Family: T79Family, Plain: true,
		Live: t79cli.Live, New: newT79})
}


type appMon struct {
	h   http.Handler
	run func(context.Context) error
	eng func() *rx.Wide
}

func (m *appMon) Handler() http.Handler         { return m.h }
func (m *appMon) Run(ctx context.Context) error { return m.run(ctx) }
func (m *appMon) Engine() rx.Stations           { return engineStations(m.eng()) }

func engineStations(w *rx.Wide) rx.Stations {
	if w == nil {
		return nil
	}
	return w
}
func (m *appMon) Evidence() Evidence { return Evidence{} }
func audioTap(e Env) func(int, []int16, int) {
	if e.Audio == nil {
		return nil
	}
	return func(_ int, pcm []int16, rate int) { e.Audio(pcm, rate) }
}

func newT86(e Env) (Monitor, error) {
	l := e.Live
	ec := l.Engine(e.Plan)
	ec.Open, ec.LogDir, ec.LogOut, ec.PreS = e.Open, e.LogDir, e.LogOut, t86.IQPrerollS
	ec.Inputs = nil
	app := t86.New(t86.Config{Engine: ec, Defaults: t86.ChannelOpts{SyncThresh: 0.6}, Title: "seed=自動",
		AudioTap: audioTap(e)})
	return &appMon{h: app.Handler(), run: app.Run, eng: app.Engine}, nil
}


func newT115(e Env) (Monitor, error) {
	l := e.Live
	m := t115.NewMonitor(t115.Config{
		Source: e.Source, SampleRate: l.SampleRate, Format: l.Format, CenterHz: e.Plan.CenterHz,
		GainDB: l.GainDB, AGC: l.AGC, Realtime: !l.FullSpeed, Speed: l.Speed,
		Channels: e.Plan.MHz(), FollowVFO: e.Plan.FollowVFO, Open: e.Open,
		LogDir: e.LogDir, LogOut: e.LogOut, AudioTap: audioTap(e),
	})
	return &appMon{h: m.Handler(), run: m.Run, eng: m.Engine}, nil
}


type T61Mon struct {
	s  *t61.Server
	ev func() Evidence
	mu    sync.Mutex
	codec string
}

func NewT61(e Env, prep func(*watch.Config) t61.Extension, observe func(dec.Frame) Evidence) *T61Mon {
	l := e.Live
	sc := l.Scan
	cfg := watch.Config{
		SampleRate: l.SampleRate, Format: l.Format, CenterHz: e.Plan.CenterHz, ChannelsMHz: e.Plan.MHz(),
		LogDir: e.LogDir, System: e.System, PreS: 10, PostS: 15, MaxS: 900, ControlS: 180, ControlGap: 3600,
		GainDB: l.GainDB, AGC: l.AGC, Scan: sc.Enabled,
		Realtime: !l.FullSpeed, Speed: l.Speed, FollowVFO: e.Plan.FollowVFO, Open: e.Open, LogOut: e.LogOut,
	}
	var ext t61.Extension
	if prep != nil {
		ext = prep(&cfg)
	}
	m := &T61Mon{}
	var last Evidence
	var lmu sync.Mutex
	onFrame := func(_ float64, f dec.Frame) {
		if c := t61.BearerCodec(f); c != "" {
			m.mu.Lock()
			m.codec = c
			m.mu.Unlock()
		}
		if observe != nil {
			ev := observe(f)
			lmu.Lock()
			last = ev
			lmu.Unlock()
		}
	}
	m.ev = func() Evidence {
		lmu.Lock()
		defer lmu.Unlock()
		return last
	}
	tc := t61.Config{Source: e.Source, Watch: cfg, Ext: ext, OnFrame: onFrame}
	if ext == nil {
		tc.Codec, tc.AudioTap = "auto", audioTap(e)
	} else if c, ok := ext.(interface{ DefaultCodec() string }); ok && c.DefaultCodec() != "" {
		tc.Codec, tc.AudioTap = c.DefaultCodec(), audioTap(e)
	}
	p := t61.New(tc)
	m.s = t61.NewServer(p)
	return m
}

func (m *T61Mon) Handler() http.Handler         { return m.s.Handler() }
func (m *T61Mon) Run(ctx context.Context) error { return m.s.Run(ctx) }
func (m *T61Mon) Engine() rx.Stations           { return engineStations(m.s.Pipeline().Engine()) }

func (m *T61Mon) Evidence() Evidence {
	ev := m.ev()
	m.mu.Lock()
	if ev.Codec == "" && m.codec != "" {
		ev.Codec = m.codec + "（呼設定の伝達能力）"
	}
	m.mu.Unlock()
	if c := m.s.Pipeline().VoiceCodec(); c != "" {
		ev.Codec = c
	}
	return ev
}


const T79Family = "STD-T79"

type t79Mon struct {
	m *t79.Monitor
}

func (t *t79Mon) Handler() http.Handler         { return t.m.Handler() }
func (t *t79Mon) Run(ctx context.Context) error { return t.m.Run(ctx) }
func (t *t79Mon) Engine() rx.Stations           { return engineStations(t.m.Engine()) }
func (t *t79Mon) Evidence() Evidence {
	c := t.m.HeardCodec()
	if c == "" {
		return Evidence{}
	}
	return Evidence{Codec: c, Text: "音声の符号器 " + c}
}

func newT79(e Env) (Monitor, error) {
	ec := e.Live.Engine(e.Plan)
	ec.Open, ec.LogDir, ec.LogOut, ec.Inputs = e.Open, e.LogDir, e.LogOut, nil
	m := t79.NewMonitor(t79.Config{Engine: ec, Codec: "auto", AudioTap: audioTap(e)})
	return &t79Mon{m: m}, nil
}
