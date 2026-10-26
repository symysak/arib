package main

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"math"
	"net"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/symysak/arib/internal/cliflag"
	"github.com/symysak/arib/internal/credit"
	"github.com/symysak/arib/internal/iqsrc"
	"github.com/symysak/arib/internal/rec"
	"github.com/symysak/arib/internal/rx"
	"github.com/symysak/arib/internal/webui"
)


//go:embed static
var probeStatic embed.FS

const probeDir = "probe"

func cmdProbe(argv []string) int {
	fs := flag.NewFlagSet("arib probe", flag.ContinueOnError)
	var l cliflag.Live
	l.Register(fs, cliflag.LiveDefaults{System: probeDir, Addr: "127.0.0.1:8200"})
	hold := fs.Float64("hold", 3, "当たりを別の規格へ移すまでの余裕 [s]（その規格が続けて上回ったら移す。ばたつかせない）")
	only := fs.String("std", "", "試す規格を絞る（カンマ区切り。既定は全部: "+strings.ReplaceAll(names(), "|", ",")+"）")
	verbose := fs.Bool("v", false, "規格ごとの受信のログも端末へ出す（既定は <log-dir>/probe/<系統>/watch.log にだけ書く）")
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, `局 1 本を全規格で同時に試す: arib probe [オプション] -input <入力>[@<中心 MHz>]…

  SDR# のプラグインを VFO 追従・間引きありにして 1 局ぶんを流す。周波数は接続の中で分かる:
    arib probe -input sdrsharp://127.0.0.1:5555
  録音ファイル（1 局ぶん。周波数は名前の …_<Hz>Hz / …_<MHz>MHz から読めれば省略可）:
    arib probe -input <録音.wav>@<局>

  画面 http://<addr>/ に規格ごとの当たりの具合と、当たった規格の Web モニタ（音声つき）が出る。
  結果は台帳 <ledger>/probe.tsv（周波数 → 規格・系統・符号器・点数・最後に見た時刻）に貯める。
  局は 1 本だけ（多局・走査は arib live --std <規格> で）。

`)
		fs.PrintDefaults()
	}
	if err := fs.Parse(cliflag.ReorderArgs(fs, argv)); err != nil {
		return 2
	}
	if err := cliflag.CheckNoArgs(fs); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	if len(l.Inputs) > 1 {
		fmt.Fprintln(os.Stderr, "probe は入力 1 本だけを見る（-input は 1 つ）")
		return 2
	}
	if l.Channels != "" || l.Scan.Enabled {
		fmt.Fprintln(os.Stderr, "probe は局 1 本だけを見る（多局・走査は arib live --std <規格> -channels / -scan で）")
		return 2
	}
	plan, err := l.Plan()
	src := plan.Source
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	if plan.Warn != "" {
		fmt.Fprintln(os.Stderr, plan.Warn)
	}
	syss, err := pickSystems(*only)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	var logOut io.Writer
	if *verbose {
		logOut = os.Stderr
	}
	p, err := newProbe(probeConfig{Source: src, Live: l, Plan: plan, Hold: time.Duration(*hold * float64(time.Second)),
		Systems: syss, LedgerDir: l.LedgerDir(), LogOut: logOut, Out: os.Stderr})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	fmt.Fprintln(os.Stderr, credit.Line())
	ctx, stop := interruptContext()
	defer stop()
	if err := p.serve(ctx, l.Addr); err != nil {
		fmt.Fprintln(os.Stderr, "終了:", err)
		return 1
	}
	return 0
}

func pickSystems(only string) ([]*System, error) {
	if strings.TrimSpace(only) == "" {
		return allSystems(), nil
	}
	var out []*System
	for _, n := range strings.Split(only, ",") {
		s := lookup(strings.TrimSpace(n))
		if s == nil || s.New == nil {
			return nil, fmt.Errorf("知らない規格 %q（%s）", n, names())
		}
		out = append(out, s)
	}
	return out, nil
}

type probeConfig struct {
	Source    string
	Live      cliflag.Live
	Plan      cliflag.Plan
	Hold      time.Duration
	Systems   []*System
	LedgerDir string
	LogOut, Out io.Writer
}

type cand struct {
	sys *System
	mon Monitor
	idx int
	audio atomic.Int64
	done  atomic.Bool
	err   error
	win   lockWin
}

type probe struct {
	cfg   probeConfig
	tee   *tee
	src   iqsrc.Source
	cands []*cand
	led   *probeLedger
	mux   *http.ServeMux

	mu         sync.Mutex
	verdict    Verdict
	challenger string
	challSince time.Time
	finished   bool
}

type Verdict struct {
	Name    string  `json:"name"`
	Family  string  `json:"family"`
	System  string  `json:"system"`
	Codec   string  `json:"codec"`
	Score   float64 `json:"score"`
	Lock    string  `json:"lock"`
	Proof   string  `json:"evidence"`
	FreqMHz float64 `json:"freq_mhz"`
	SinceMS int64   `json:"since_ms"`
}

type CandView struct {
	Name     string  `json:"name"`
	Label    string  `json:"label"`
	Family   string  `json:"family"`
	Plain    bool    `json:"plain"`
	OK       bool    `json:"ok"`
	Score    float64 `json:"score"`
	Lock     string  `json:"lock"`
	MHz      float64 `json:"mhz"`
	EvOK     bool    `json:"evidence_ok"`
	EvScore  float64 `json:"evidence_score"`
	Evidence string  `json:"evidence"`
	Codec    string  `json:"codec"`
	AudioSec float64 `json:"audio_sec"`
	Done     bool    `json:"done"`
	Err      string  `json:"err,omitempty"`
	heard string
}

func newProbe(cfg probeConfig) (*probe, error) {
	if len(cfg.Systems) == 0 {
		return nil, errors.New("試す規格が無い")
	}
	if cfg.Out == nil {
		cfg.Out = io.Discard
	}
	l := cfg.Live
	if strings.HasPrefix(cfg.Source, "rtltcp://") && l.SampleRate == 0 {
		l.SampleRate = 2400000
	}
	open := func() (iqsrc.Source, error) {
		return iqsrc.Open(cfg.Source, iqsrc.Options{SampleRate: l.SampleRate, Format: l.Format, FreqHz: cfg.Plan.CenterHz,
			GainDB: l.GainDB, AGC: l.AGC, Realtime: !l.FullSpeed, Speed: l.Speed})
	}
	t, src, err := newTee(open, len(cfg.Systems))
	if err != nil {
		return nil, err
	}
	p := &probe{cfg: cfg, tee: t, src: src, mux: http.NewServeMux()}
	led, err := openProbeLedger(cfg.LedgerDir)
	if err != nil {
		fmt.Fprintf(cfg.Out, "台帳を開けませんでした（台帳なしで続けます）: %v\n", err)
	}
	p.led = led
	for i, s := range cfg.Systems {
		c := &cand{sys: s, idx: i}
		env := Env{Source: cfg.Source, Open: t.branches[i].opener(), Live: l, Plan: cfg.Plan,
			LogDir: rec.SystemDir(l.LogDir, probeDir), System: s.Dir, LogOut: cfg.LogOut,
			Audio: func(pcm []int16, rate int) {
				if rate > 0 {
					c.audio.Add(int64(len(pcm)) * 1000 / int64(rate))
				}
			}}
		m, err := s.New(env)
		if err != nil {
			src.Close()
			return nil, fmt.Errorf("%s: %v", s.Name, err)
		}
		c.mon = m
		p.cands = append(p.cands, c)
		webui.Mount(p.mux, "/std/"+s.Name+"/", m.Handler())
	}
	sub, _ := fs.Sub(probeStatic, "static")
	p.mux.Handle("/", http.FileServer(http.FS(sub)))
	p.mux.HandleFunc("/api/probe", p.handleAPI)
	return p, nil
}

func (p *probe) run(ctx context.Context) {
	p.tee.start(ctx, p.src)
	var wg sync.WaitGroup
	for _, c := range p.cands {
		wg.Add(1)
		go func(c *cand) {
			defer wg.Done()
			c.err = c.mon.Run(ctx)
			c.done.Store(true)
			p.tee.detach(c.idx)
			if c.err != nil {
				fmt.Fprintf(p.cfg.Out, "%s: 受信が止まった: %v\n", c.sys.Name, c.err)
			}
		}(c)
	}
	allDone := make(chan struct{})
	go func() { wg.Wait(); close(allDone) }()
	tk := time.NewTicker(judgeEvery)
	defer tk.Stop()
	lastSave := time.Now()
	for {
		select {
		case <-allDone:
			p.judge(time.Now())
			p.saveLedger()
			p.mu.Lock()
			p.finished = true
			v := p.verdict
			p.mu.Unlock()
			if v.Name != "" {
				fmt.Fprintf(p.cfg.Out, "入力が終わった。最後の当たり: %s\n", describe(v))
			} else {
				fmt.Fprintln(p.cfg.Out, "入力が終わった。どの規格にも当たらなかった")
			}
			return
		case now := <-tk.C:
			p.judge(now)
			if now.Sub(lastSave) >= 10*time.Second {
				lastSave = now
				p.saveLedger()
			}
		}
	}
}

func (p *probe) serve(ctx context.Context, addr string) error {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	srv := &http.Server{Handler: p.mux, ReadHeaderTimeout: 10 * time.Second}
	go func() { _ = srv.Serve(ln) }()
	fmt.Fprintf(p.cfg.Out, "probe の画面: http://%s/ （候補 %d: %s。Ctrl-C で終了）\n", ln.Addr(), len(p.cands), names())
	p.run(ctx)
	<-ctx.Done()
	sctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	return srv.Shutdown(sctx)
}

const judgeEvery = 200 * time.Millisecond

const (
	probeWindowSec = 3.0
	probeFreshSec  = 1.0
)

func lockOf(m Monitor, w *lockWin) (ok bool, score float64, label string, mhz float64) {
	if l, is := m.(interface {
		Lock() (bool, float64, string, float64)
	}); is {
		return l.Lock()
	}
	eng := m.Engine()
	if eng == nil {
		return
	}
	var best rx.Channel
	id := 0
	for _, c := range eng.Status().Channels {
		ch := eng.Channel(c.ID)
		if ch == nil {
			continue
		}
		o, s, lb := ch.Lock()
		if (o && !ok) || (o == ok && s > score) || label == "" {
			ok, score, label, mhz, best, id = o, s, lb, c.MHz, ch, c.ID
		}
	}
	if best == nil {
		return
	}
	frames, good, fresh := w.add(id, best.Counters())
	score = 0
	if frames > 0 {
		score = min(float64(good)/float64(frames), 1)
	}
	return ok && fresh, score, label, mhz
}

type lockWin struct {
	mu       sync.Mutex
	id       int
	s        []rx.Counters
	lastGood float64
}

func (w *lockWin) add(id int, c rx.Counters) (frames, good int, fresh bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if n := len(w.s); n == 0 || id != w.id || c.Frames < w.s[n-1].Frames || c.Good < w.s[n-1].Good ||
		c.ProcSec < w.s[n-1].ProcSec {
		w.id, w.s, w.lastGood = id, []rx.Counters{{}}, -1
	}
	if c.Good > w.s[len(w.s)-1].Good {
		w.lastGood = c.ProcSec
	}
	w.s = append(w.s, c)
	for len(w.s) > 2 && c.ProcSec-w.s[1].ProcSec >= probeWindowSec {
		w.s = w.s[1:]
	}
	return c.Frames - w.s[0].Frames, c.Good - w.s[0].Good, w.lastGood >= 0 && c.ProcSec-w.lastGood <= probeFreshSec
}

func (p *probe) views() []CandView {
	out := make([]CandView, 0, len(p.cands))
	for _, c := range p.cands {
		ok, score, label, mhz := lockOf(c.mon, &c.win)
		ev := c.mon.Evidence()
		codec := c.sys.Codec
		if ev.Codec != "" {
			codec = ev.Codec
		}
		v := CandView{Name: c.sys.Name, Label: c.sys.Label, Family: c.sys.Family, Plain: c.sys.Plain,
			OK: ok, Score: round3(score), Lock: label, MHz: mhz, EvOK: ev.OK, EvScore: round3(ev.Score), Evidence: ev.Text, Codec: codec, heard: ev.Codec,
			AudioSec: float64(c.audio.Load()) / 1000, Done: c.done.Load()}
		if v.Done && c.err != nil {
			v.Err = c.err.Error()
		}
		out = append(out, v)
	}
	return out
}

func round3(v float64) float64 { return math.Round(v*1000) / 1000 }

type family struct {
	name  string
	ok    bool
	score float64
	lock  string
	mhz   float64
	plain bool
}

func families(vs []CandView) map[string]*family {
	fams := map[string]*family{}
	for _, v := range vs {
		f := fams[v.Family]
		if f == nil {
			f = &family{name: v.Family}
			fams[v.Family] = f
		}
		if f.plain {
			continue
		}
		if v.Plain || f.lock == "" || (v.OK && !f.ok) || (v.OK == f.ok && v.Score > f.score) {
			f.ok, f.score, f.lock, f.mhz, f.plain = v.OK, v.Score, v.Lock, v.MHz, v.Plain
		}
	}
	return fams
}

func (p *probe) judge(now time.Time) {
	vs := p.views()
	fams := families(vs)
	p.mu.Lock()
	defer p.mu.Unlock()
	cur := p.verdict.Family
	var best *family
	for _, f := range fams {
		if !f.ok {
			continue
		}
		if best == nil || f.score > best.score || (f.score == best.score && f.name == cur) ||
			(f.score == best.score && best.name != cur && f.name < best.name) {
			best = f
		}
	}
	if best == nil {
		p.challenger = ""
		return
	}
	if f := fams[cur]; f != nil && f.ok && best.name != cur {
		if p.challenger != best.name {
			p.challenger, p.challSince = best.name, now
		}
		if now.Sub(p.challSince) < p.cfg.Hold {
			p.touch(pickSystem(f, vs), now)
			return
		}
	}
	p.challenger = ""
	p.touch(pickSystem(best, vs), now)
}

func pickSystem(f *family, vs []CandView) Verdict {
	var plain, sys *CandView
	for i := range vs {
		v := &vs[i]
		if v.Family != f.name {
			continue
		}
		if v.Plain {
			plain = v
			continue
		}
		if v.EvOK && (sys == nil || v.EvScore > sys.EvScore) {
			sys = v
		}
	}
	pick := sys
	if pick == nil {
		pick = plain
	}
	if pick == nil {
		for i := range vs {
			if vs[i].Family == f.name {
				pick = &vs[i]
				break
			}
		}
	}
	v := Verdict{Name: pick.Name, Family: f.name, System: pick.Label, Codec: pick.Codec,
		Score: f.score, Lock: f.lock, Proof: pick.Evidence, FreqMHz: f.mhz}
	if v.Codec == "" {
		for _, c := range vs {
			if c.Family == f.name && c.heard != "" {
				v.Codec = c.heard
				break
			}
		}
	}
	return v
}

func (p *probe) touch(v Verdict, now time.Time) {
	old := p.verdict
	if old.Name == v.Name && old.Family == v.Family {
		v.SinceMS = old.SinceMS
	} else {
		v.SinceMS = now.UnixMilli()
		fmt.Fprintf(p.cfg.Out, "当たり: %s → 画面 /std/%s/\n", describe(v), v.Name)
	}
	p.verdict = v
	p.led.touch(v, now)
}

func describe(v Verdict) string {
	s := fmt.Sprintf("%.4f MHz %s / %s（点数 %.2f・%s", v.FreqMHz, v.Family, v.System, v.Score, v.Lock)
	if v.Proof != "" {
		s += "・" + v.Proof
	}
	if v.Codec != "" {
		s += "・符号器 " + v.Codec
	}
	return s + "）"
}

func (p *probe) saveLedger() {
	if err := p.led.save(); err != nil {
		fmt.Fprintf(p.cfg.Out, "台帳を書けませんでした: %v\n", err)
	}
}

func (p *probe) snapshot() map[string]any {
	vs := p.views()
	p.mu.Lock()
	defer p.mu.Unlock()
	sort.SliceStable(vs, func(i, j int) bool { return order(lookupOr(vs[i].Name)) < order(lookupOr(vs[j].Name)) })
	in := p.cfg.Source
	led := ""
	if p.led != nil {
		led = p.led.path
	}
	return map[string]any{"generator": credit.Generator, "input": in, "verdict": p.verdict, "candidates": vs,
		"finished": p.finished, "ledger": led, "hold_sec": p.cfg.Hold.Seconds()}
}

func lookupOr(name string) *System {
	if s := lookup(name); s != nil {
		return s
	}
	return &System{Name: name}
}

func (p *probe) handleAPI(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(p.snapshot())
}

func (p *probe) current() Verdict {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.verdict
}
