package server

import (
	"context"
	"fmt"
	"github.com/symysak/arib/internal/bitpack"
	"github.com/symysak/arib/internal/credit"
	"math"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/symysak/arib/internal/rec"
	"github.com/symysak/arib/internal/rx"
	"github.com/symysak/arib/internal/std-t61/callseq"
	dec "github.com/symysak/arib/internal/std-t61/decoder"
	"github.com/symysak/arib/internal/std-t61/l1"
	"github.com/symysak/arib/internal/std-t61/ledger"
	"github.com/symysak/arib/internal/std-t61/watch"
	"github.com/symysak/arib/internal/voice"
)

const (
	stationTickMS = 200
	maxLogs  = 400
	maxCalls = 300
	maxL1    = 2000
	maxL2    = 500
	maxL3    = 200
	maxRecs  = 120
	defaultWatchMax = 128
	ledgerSaveSec = 30
	constGroup = 48
)

type Config struct {
	Source string
	Watch  watch.Config
	Addr   string
	Ext Extension
	LedgerDir string
	OnFrame func(mhz float64, f dec.Frame)
	Codec string
	CodecOpt voice.Options
	StationWC map[float64]int
	TCHDump bool
	Headless bool
	AudioTap func(idx int, pcm []int16, rate int)
	NoFollowAssign bool
}

type Pipeline struct {
	cfg Config

	mu       sync.Mutex
	stations []*stationState
	byMHz    map[float64]*stationState
	nextIdx int
	calls   []Call
	logs    []LogLine
	l1recs  []*L1Rec
	l2recs  []*L2Rec
	l3recs  []*L3Rec
	noL1    int64
	noL2    int64
	noL3    int64
	noV     int64
	asm    map[asmKey]*L2Rec
	lastL2 map[asmKey]*L2Rec
	l1All    bool
	recs     []Rec
	meta     Meta
	selected int

	led      *ledger.Ledger
	lastSave time.Time

	eng *rx.Wide
	audio *Hub
	voiceCodec string
	seq *callseq.Tracker
	followed map[float64]bool
	followAdd func(mhz float64) error

	out     func(Event)
	started time.Time
	done    chan struct{}
	err     error
}

type stationState struct {
	st  Station
	ext ChannelExt
	lastNV2 string
	sbKey string
	constPend []dec.ConstSample
	dirty     bool
	pcmSeq    int64
	forget bool
	voice *voiceRun
	callID int
}

func New(cfg Config) *Pipeline {
	p := &Pipeline{cfg: cfg, byMHz: map[float64]*stationState{}, selected: 0,
		asm: map[asmKey]*L2Rec{}, lastL2: map[asmKey]*L2Rec{}, done: make(chan struct{}), seq: callseq.New()}
	if cfg.Ext != nil {
		cfg.Ext.Attach(p)
	}
	p.out = func(Event) {}
	return p
}

func (p *Pipeline) Meta() Meta { return p.meta }

func (p *Pipeline) Snapshot() Snapshot {
	p.mu.Lock()
	defer p.mu.Unlock()
	sn := Snapshot{Generator: credit.Generator, Meta: p.meta}
	for _, s := range p.stations {
		sn.Stations = append(sn.Stations, s.st)
	}
	sn.Calls = append(sn.Calls, p.calls...)
	sn.Logs = append(sn.Logs, p.logs...)
	sn.L1 = append(sn.L1, p.l1recs...)
	sn.L2 = append(sn.L2, p.l2recs...)
	sn.L3 = append(sn.L3, p.l3recs...)
	sn.CallSeq = p.seq.Calls()
	if p.cfg.Ext != nil {
		sn.Ext = p.cfg.Ext.Snapshot()
	}
	sn.Recs = append(sn.Recs, p.recs...)
	return sn
}

func (p *Pipeline) Select(idx int) {
	p.mu.Lock()
	if p.stationByIdx(idx) != nil {
		p.selected = idx
	}
	p.mu.Unlock()
}

func (p *Pipeline) stationByIdx(idx int) *stationState {
	for _, s := range p.stations {
		if s.st.Idx == idx {
			return s
		}
	}
	return nil
}

func (p *Pipeline) Emit(e Event) { p.emit(e) }

func (p *Pipeline) Now() float64 { return p.now() }

func (p *Pipeline) AddLog(idx int, group, format string, a ...any) {
	p.addLogFor(idx, group, format, a...)
}

func (p *Pipeline) EmitCall(c Call) {
	p.mu.Lock()
	p.calls = append(p.calls, c)
	if len(p.calls) > maxCalls {
		p.calls = p.calls[len(p.calls)-maxCalls:]
	}
	p.mu.Unlock()
	p.emit(Event{Type: "call", Call: &c})
}

func (p *Pipeline) StationMHz(idx int) float64 {
	p.mu.Lock()
	defer p.mu.Unlock()
	if s := p.stationByIdx(idx); s != nil {
		return s.st.MHz
	}
	return 0
}

func (p *Pipeline) MarkDirty(idx int) {
	p.mu.Lock()
	if s := p.stationByIdx(idx); s != nil {
		s.dirty = true
	}
	p.mu.Unlock()
}

func (p *Pipeline) SetL1All(on bool) {
	p.mu.Lock()
	p.l1All = on
	p.meta.L1All = on
	m := p.meta
	p.mu.Unlock()
	p.emit(Event{Type: "meta", Meta: &m})
}

func (p *Pipeline) Selected() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.selected
}

func (p *Pipeline) now() float64 { return time.Since(p.started).Seconds() }

func (p *Pipeline) emit(e Event) {
	if e.TSec == 0 {
		e.TSec = p.now()
	}
	p.out(e)
}

func (p *Pipeline) addLog(group, format string, a ...any) {
	p.addLogFor(-1, group, format, a...)
}

func (p *Pipeline) addLogFor(idx int, group, format string, a ...any) {
	l := LogLine{TSec: p.now(), Idx: idx, Group: group, Text: fmt.Sprintf(format, a...)}
	p.mu.Lock()
	p.logs = append(p.logs, l)
	if len(p.logs) > maxLogs {
		p.logs = p.logs[len(p.logs)-maxLogs:]
	}
	p.mu.Unlock()
	p.emit(Event{Type: "log", Log: &l})
}

func (p *Pipeline) Run(ctx context.Context, out func(Event)) error {
	p.out = out
	p.started = time.Now()
	chans := append([]float64(nil), p.cfg.Watch.ChannelsMHz...)
	sort.Float64s(chans)
	p.meta = Meta{
		Input: p.cfg.Source, CenterMHz: p.cfg.Watch.CenterHz / 1e6,
		SampleRate: p.cfg.Watch.SampleRate, GainDB: p.cfg.Watch.GainDB,
		LogDir:   p.cfg.Watch.LogDir,
		Channels: []float64{},
		T0WallMS: p.started.UnixMilli(),
	}
	if p.cfg.Ext != nil {
		p.meta.Method = p.cfg.Ext.Name()
	}
	user := map[int64]bool{}
	for _, f := range chans {
		user[int64(math.Round(f*1e4))] = true
	}
	chans = p.openLedger(chans)
	for _, mhz := range chans {
		p.addStation(mhz)
	}
	p.restoreFromLedger()
	p.addLog("system", "受信開始 %s 中心 %.4f MHz 利得 %.1f dB", p.cfg.Source, p.meta.CenterMHz, p.meta.GainDB)
	go p.tick()
	wc := p.cfg.Watch
	wc.ChannelsMHz, wc.LedgerMHz = nil, nil
	for _, mhz := range chans {
		if user[int64(math.Round(mhz*1e4))] {
			wc.ChannelsMHz = append(wc.ChannelsMHz, mhz)
		} else {
			wc.LedgerMHz = append(wc.LedgerMHz, mhz)
		}
	}
	if !p.cfg.Headless {
		wc.Detail = true
		wc.AllowEmpty = true
	}
	if p.led != nil && wc.ChannelNote == nil {
		wc.ChannelNote = p.led.Note
	}
	eng := watch.NewEngine(p.cfg.Source, wc, watch.Hooks{
		NewChannel: p.newChannel,
		Start: func(describe string, rate float64) {
			p.mu.Lock()
			p.meta.Input, p.meta.SampleRate = describe, rate
			p.mu.Unlock()
		},
	})
	p.mu.Lock()
	p.eng = eng
	p.mu.Unlock()
	err := eng.Run(ctx)
	p.err = err
	p.saveLedger()
	p.mu.Lock()
	p.meta.Finished = true
	p.mu.Unlock()
	close(p.done)
	if err != nil {
		p.addLog("system", "入力が終わりました: %v", err)
	} else {
		p.addLog("system", "入力が終端に達しました")
	}
	return err
}

func (p *Pipeline) Engine() *rx.Wide {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.eng
}

func (p *Pipeline) forget(mhz float64) {
	p.mu.Lock()
	for _, s := range p.stations {
		if math.Abs(s.st.MHz-mhz) < 1e-6 {
			s.forget = true
		}
	}
	p.mu.Unlock()
}

func (p *Pipeline) setMeta(f func(m *Meta)) {
	p.mu.Lock()
	f(&p.meta)
	m := p.meta
	p.mu.Unlock()
	p.emit(Event{Type: "meta", Meta: &m})
}

func (p *Pipeline) Done() <-chan struct{} { return p.done }

func (p *Pipeline) tick() {
	t := time.NewTicker(stationTickMS * time.Millisecond)
	defer t.Stop()
	for {
		select {
		case <-p.done:
			return
		case <-t.C:
			p.mu.Lock()
			var send []Station
			for _, s := range p.stations {
				if s.ext != nil {
					before := len(s.st.Ext)
					s.ext.Station(s.st.Ext)
					if before != len(s.st.Ext) {
						s.dirty = true
					}
				}
				if s.dirty {
					s.dirty = false
					send = append(send, s.st)
				}
			}
			p.mu.Unlock()
			for i := range send {
				st := send[i]
				p.emit(Event{Type: "station", Station: &st})
			}
			p.seqTick()
			if p.led != nil && time.Since(p.lastSave).Seconds() >= ledgerSaveSec {
				p.lastSave = time.Now()
				p.saveLedger()
			}
		}
	}
}

func (p *Pipeline) addStation(mhz float64) *stationState {
	p.mu.Lock()
	if s := p.byMHz[mhz]; s != nil {
		s.st.Watched, s.dirty = true, true
		p.mu.Unlock()
		return s
	}
	idx := p.nextIdx
	p.nextIdx++
	s := &stationState{st: Station{Idx: idx, MHz: mhz, SFPos: -1, Watched: true, Ext: map[string]any{}}}
	if p.led != nil {
		s.st.Note = p.led.Note(mhz)
	}
	p.stations = append(p.stations, s)
	p.byMHz[mhz] = s
	p.meta.Channels = append(p.meta.Channels, mhz)
	sort.Float64s(p.meta.Channels)
	ext := p.cfg.Ext
	p.mu.Unlock()
	if ext != nil {
		s.ext = ext.NewChannel(idx, mhz)
	}
	s.dirty = true
	return s
}

func (p *Pipeline) dropStation(s *stationState) {
	p.mu.Lock()
	if p.byMHz[s.st.MHz] != s {
		p.mu.Unlock()
		return
	}
	delete(p.byMHz, s.st.MHz)
	for i, x := range p.stations {
		if x == s {
			p.stations = append(p.stations[:i], p.stations[i+1:]...)
			break
		}
	}
	for i, f := range p.meta.Channels {
		if f == s.st.MHz {
			p.meta.Channels = append(p.meta.Channels[:i], p.meta.Channels[i+1:]...)
			break
		}
	}
	if p.selected == s.st.Idx && len(p.stations) > 0 {
		p.selected = p.stations[0].st.Idx
	}
	idx, mhz := s.st.Idx, s.st.MHz
	p.mu.Unlock()
	p.emit(Event{Type: "station_gone", TSec: p.now(), Station: &Station{Idx: idx, MHz: mhz}})
	p.addLogFor(idx, "system", "%.4f MHz の監視をやめた", mhz)
}

func (p *Pipeline) newChannel(mhz float64) *watch.ChannelHook {
	s := p.addStation(mhz)
	if s == nil {
		return nil
	}
	onFrame := p.cfg.OnFrame
	var vr *voiceRun
	if p.cfg.Codec != "" || p.cfg.TCHDump {
		vr = newVoiceRun(p, s)
	}
	s.voice = vr
	hk := &watch.ChannelHook{
		Frame: func(f dec.Frame) {
			p.onFrame(s, f)
			if vr != nil {
				vr.frame(f)
			}
			if onFrame != nil {
				onFrame(mhz, f)
			}
		},
		Level: func(db float64) { p.mu.Lock(); s.st.LevelDB, s.dirty = db, true; p.mu.Unlock() },
		Stats: func(ds dec.Stats, coarse, cfo float64, locked bool) {
			p.mu.Lock()
			st := &s.st
			st.CoarseHz, st.CFOHz, st.Synced = coarse, cfo, locked
			st.SyncLoss = ds.SyncLoss
			st.SWDetected, st.Flywheel, st.RICHOKNum = ds.SWDetected, ds.Flywheel, ds.RICHOK
			st.SACCHNum, st.SACCHOK, st.SACCHFiller = ds.SACCHUnits-ds.SACCHFiller, ds.SACCHOK, ds.SACCHFiller
			st.FACCHNum, st.FACCHOK = ds.FACCHUnits, ds.FACCHOK
			st.SBNum, st.SBOK = ds.SBUnits, ds.SBOK
			st.NV2OK = ds.NV2OK
			st.CtlNum, st.CtlOK = ds.ControlUnits, ds.ControlOK
			st.UpCtlNum, st.UpCtlOK = ds.UpControlUnits, ds.UpControlOK
			st.Frames = ds.Frames
			s.dirty = true
			p.mu.Unlock()
		},
		Const:       func(cs []dec.ConstSample) { p.onConst(s, cs) },
		Reset:       func(q float64) { p.onSignalLost(s, q) },
		RecordStart: func(base, reason string) { p.onRecordStart(s, base, reason) },
		RecordStop:  func() { p.onRecordStop(s) },
		Closing: func() {
			if vr != nil {
				vr.finish()
			}
		},
		Closed: func(dropped bool) {
			if vr != nil {
				vr.finish()
			}
			if !dropped {
				return
			}
			p.mu.Lock()
			seen := s.st.Frames > 0 && !s.forget
			s.forget = false
			if seen {
				s.st.Watched, s.st.Synced, s.dirty = false, false, true
			}
			p.mu.Unlock()
			if seen {
				p.addLogFor(s.st.Idx, "system", "%.4f MHz の監視をやめた（行は残す）", s.st.MHz)
				return
			}
			p.dropStation(s)
		},
	}
	if vr != nil {
		vr.setUpScramble = func(init int) {
			if hk.SetUplinkScramble != nil {
				hk.SetUplinkScramble(init)
			}
		}
	}
	return hk
}

func (p *Pipeline) onFrame(s *stationState, f dec.Frame) {
	if s.ext != nil {
		s.ext.Frame(f)
	}
	sel := false
	p.mu.Lock()
	st := &s.st
	st.LastTSec = p.now()
	st.Ledger = false
	st.Synced = !f.Flywheel || st.Synced
	if !f.Flywheel && f.SW != nil {
		st.SWName = f.SW.Name
	}
	st.Kind = kindName(f.Kind)
	st.Uplink = f.Uplink
	st.SFPos = f.SFPos
	st.PhaseErrDeg = f.PhaseErrDeg
	richChanged := false
	if r := f.A1RICH; r != nil && r.OK {
		bits := fmt.Sprintf("付録1 %01b %01b %01b", r.IT, r.LCFEMP, r.REP)
		if st.RICHBits != bits || !st.RICHOK {
			richChanged = true
			st.RICHBits, st.RICHText, st.RICHRows, st.RICHNote = bits, r.Describe(), nil, ""
			st.Mode = a1ModeName(*r)
			p.mu.Unlock()
			p.addLogFor(st.Idx, "station", "%.4f MHz %s %s %s", st.MHz, st.Kind, st.Mode, r.Describe())
			p.mu.Lock()
		}
	}
	if f.RICH.OK {
		if st.RICHBits != f.RICH.Bits17() || !st.RICHOK {
			richChanged = true
			st.RICHBits = f.RICH.Bits17()
			st.RICHText = f.RICH.Describe(l1.EditionFDMA)
			st.RICHRows = f.RICH.ExpandRICH(l1.EditionFDMA)
			st.RICHNote = ""
			if p.cfg.Ext != nil {
				st.RICHRows = append(st.RICHRows, p.cfg.Ext.ExpandRICH(f.RICH)...)
				st.RICHNote = p.cfg.Ext.NoteRICH(f.RICH)
			}
			st.Mode = modeName(f.RICH)
			note := ""
			if st.RICHNote != "" {
				note = "（" + st.RICHNote + "）"
			}
			p.mu.Unlock()
			p.addLogFor(st.Idx, "station", "%.4f MHz %s RICH %s %s%s", st.MHz, st.Kind, st.RICHBits, st.Mode, note)
			p.mu.Lock()
		}
	}
	st.RICHOK = f.RICH.OK || (f.A1RICH != nil && f.A1RICH.OK)
	if f.SACCH != nil {
		if f.SACCH.OK {
			st.SACCHHex = hexOf(bitpack.ToBytes(f.SACCH.Data))
		}
	}
	if f.RCH != nil && f.RCH.OK {
		st.RCHRaw = int(f.RCH.Raw)
	}
	if f.FACCH != nil {
		if f.FACCH.OK {
			st.FACCHHex = hexOf(bitpack.ToBytes(f.FACCH.Data))
		}
	}
	if f.Control != nil && f.Control.OK {
		st.CtlHex, st.CtlE = hexOf(bitpack.ToBytes(f.Control.Data)), f.Control.EText()
	}
	var nv2 []byte
	if f.NV2Msg != nil {
		st.NV2Hex = hexOf(f.NV2Msg)
		if st.NV2Hex != s.lastNV2 {
			s.lastNV2 = st.NV2Hex
			nv2 = f.NV2Msg
		}
	}
	var sb *Call
	if f.SB != nil {
		if f.SB.OK {
			id := f.SB.IDText()
			if p.cfg.Ext != nil {
				if lbl := p.cfg.Ext.LabelID(f.SB.ID); lbl != "" {
					id = lbl
				}
			}
			key := fmt.Sprintf("%s/%d/%d", id, f.SB.I, f.SB.TA)
			changed := s.sbKey != key
			s.sbKey = key
			st.SBID, st.TA, st.SSC, st.SBStale = id, int(f.SB.TA), int(f.SB.SSC), false
			st.SBNote = ""
			if p.cfg.Ext != nil {
				st.SBNote = p.cfg.Ext.NoteSyncBurst(*f.SB)
			}
			if p.led != nil && p.cfg.Ext != nil {
				p.led.AddOrg(st.MHz, p.cfg.Ext.OrgID(f.SB.ID), time.Now())
			}
			st.SBRows, st.SBTSec = f.SB.ExpandSyncBurst(), p.now()
			if p.cfg.Ext != nil {
				st.SBRows = append(st.SBRows, p.cfg.Ext.ExpandSyncBurst(*f.SB)...)
			}
			if changed {
				text := fmt.Sprintf("I=%d TA=%d SSC=%d", f.SB.I, f.SB.TA, f.SB.SSC)
				if st.SBNote != "" {
					text += " " + st.SBNote
				}
				sb = &Call{Idx: st.Idx, MHz: st.MHz, TSec: p.now(), Kind: "sync_burst",
					Text: text, ID: id, Rows: st.SBRows}
			}
		}
	}
	s.dirty = true
	sel = p.selected == st.Idx
	idx := st.Idx
	p.mu.Unlock()

	if sb != nil {
		p.mu.Lock()
		p.calls = append(p.calls, *sb)
		if len(p.calls) > maxCalls {
			p.calls = p.calls[len(p.calls)-maxCalls:]
		}
		p.mu.Unlock()
		p.emit(Event{Type: "call", Call: sb})
		p.addLogFor(sb.Idx, "call", "%.4f MHz 同期バースト %s %s", sb.MHz, sb.ID, sb.Text)
	}
	if why := l1Why(f, richChanged, sb != nil); why != "" || (p.l1All && sel) {
		if why == "" {
			why = "全フレーム表示"
		}
		p.storeL1(&L1Rec{Idx: idx, TSec: p.now(), MHz: st.MHz, Kind: st.Kind,
			Uplink: f.Uplink, Flywheel: f.Flywheel, SFPos: f.SFPos, Why: why,
			Info: l1Info(f), RICHOK: f.RICH.OK || (f.A1RICH != nil && f.A1RICH.OK), RICHBits: f.RICH.Bits17(),
			Tree: buildL1Tree(f, l1.EditionFDMA, p.cfg.Ext), RawHex: hexOf(bitpack.ToBytes(f.Raw))})
	}
	for _, fr := range f.Frag {
		if fr.Channel == "RCH" {
			continue
		}
		p.onFragment(s, f, fr)
	}
	if f.SACCH != nil && f.SACCH.OK {
		p.onUnit(s, f, "SACCH", f.SACCH.W, f.SACCH.Data)
	}
	if f.FACCH != nil && f.FACCH.OK {
		p.onUnit(s, f, "FACCH", f.FACCH.W, f.FACCH.Data)
	}
	if f.Control != nil && f.Control.OK {
		ch := "PCC"
		if f.Control.Uplink {
			ch = "PCC↑"
		}
		p.onUnit(s, f, ch, f.Control.W, f.Control.Data)
	}
	for _, m := range f.Messages {
		p.onMessage(s, f, m)
		p.seqMessage(s, m)
		p.followAssign(s, m)
	}
	p.seqFrame(s, f)
	if nv2 != nil {
		p.onNV2(s, f, nv2)
	}
}

func (p *Pipeline) onNV2(s *stationState, f dec.Frame, msg []byte) {
	p.mu.Lock()
	p.noL2++
	r := &L2Rec{Idx: s.st.Idx, No: p.noL2, TSec: p.now(), MHz: s.st.MHz, Channel: "NV2",
		State: "完結", Stage: "非音声2 メッセージ（同じ内容の連送は畳む）", RawHex: hexOf(msg),
		Summary: printableASCII(msg), FrameNos: []int64{f.Number}}
	if p.cfg.Ext != nil {
		if rows := p.cfg.Ext.ExpandUnit("NV2", l1.W{First: true, Last: true, WO: uint8(len(msg))}, msg); len(rows) > 0 {
			r.Tree = append(r.Tree, Node{Label: "非音声2（上位方式）", Value: fmt.Sprintf("%d オクテット", len(msg)),
				Note: "LAPDM ではなく上位方式の器として読んだもの", Children: rowsToNodes(rows)})
		}
	}
	p.l2recs = append(p.l2recs, r)
	if len(p.l2recs) > maxL2 {
		p.l2recs = p.l2recs[len(p.l2recs)-maxL2:]
	}
	p.mu.Unlock()
	p.emit(Event{Type: "l2", L2: r})
	p.addLogFor(s.st.Idx, "call", "%.4f MHz 非音声2 %s", s.st.MHz, r.Summary)
}

func printableASCII(b []byte) string {
	out := make([]byte, len(b))
	for i, c := range b {
		if c < 0x20 || c > 0x7e {
			c = '.'
		}
		out[i] = c
	}
	return string(out)
}

func (p *Pipeline) onConst(s *stationState, cs []dec.ConstSample) {
	p.mu.Lock()
	if p.selected != s.st.Idx {
		s.constPend = nil
		p.mu.Unlock()
		return
	}
	s.constPend = append(s.constPend, cs...)
	idx := s.st.Idx
	var out []Event
	for len(s.constPend) >= constGroup {
		g := s.constPend[:constGroup]
		pts := make([]float32, 0, 2*constGroup)
		for _, c := range g {
			pts = append(pts, real(c.Diff), imag(c.Diff))
		}
		out = append(out, Event{Type: "const", TSec: g[0].TSec,
			Const: &Const{Idx: idx, TSec: g[0].TSec, Points: pts}})
		s.constPend = s.constPend[constGroup:]
	}
	if len(s.constPend) > constGroup {
		s.constPend = s.constPend[len(s.constPend)-constGroup:]
	}
	p.mu.Unlock()
	for _, e := range out {
		p.emit(e)
	}
}


func (p *Pipeline) storeL1(r *L1Rec) {
	p.mu.Lock()
	p.noL1++
	r.No = p.noL1
	p.l1recs = append(p.l1recs, r)
	if len(p.l1recs) > maxL1 {
		p.l1recs = p.l1recs[len(p.l1recs)-maxL1:]
	}
	p.mu.Unlock()
	p.emit(Event{Type: "l1", L1: r})
}

func (p *Pipeline) storeL3(r *L3Rec) {
	p.mu.Lock()
	p.noL3++
	r.No = p.noL3
	p.l3recs = append(p.l3recs, r)
	if len(p.l3recs) > maxL3 {
		p.l3recs = p.l3recs[len(p.l3recs)-maxL3:]
	}
	p.mu.Unlock()
	p.emit(Event{Type: "l3", L3: r})
}

type asmKey struct {
	idx     int
	channel string
}

func (p *Pipeline) onFragment(sst *stationState, f dec.Frame, fr dec.FragInfo) {
	key := asmKey{sst.st.Idx, fr.Channel}
	p.mu.Lock()
	cur := p.asm[key]
	if cur == nil || cur.State != "組立中" {
		p.noL2++
		cur = &L2Rec{Idx: sst.st.Idx, No: p.noL2, TSec: p.now(), MHz: sst.st.MHz,
			Channel: fr.Channel, State: "組立中"}
		p.l2recs = append(p.l2recs, cur)
		if len(p.l2recs) > maxL2 {
			p.l2recs = p.l2recs[len(p.l2recs)-maxL2:]
		}
		p.asm[key] = cur
	}
	cur.FrameNos = append(cur.FrameNos, f.Number)
	switch {
	case fr.Done && fr.OK:
		cur.Stage = fmt.Sprintf("断片 %d/%d → ユニット完結（訂正 %d）", fr.Have, fr.Need, fr.Corrected)
	case fr.Done && fr.Filler:
		cur.Stage = fmt.Sprintf("断片 %d/%d → 空き枠の充填（訂正 %d）", fr.Have, fr.Need, fr.Corrected)
		cur.State = "空き枠（充填）"
		p.asm[key] = nil
	case fr.Done:
		cur.Stage = fmt.Sprintf("断片 %d/%d　訂正 %d", fr.Have, fr.Need, fr.Corrected)
		cur.State = "符号語だが CRC 不一致（別規約の疑い）"
		p.asm[key] = nil
	default:
		cur.Stage = fmt.Sprintf("断片 %d/%d", fr.Have, fr.Need)
	}
	p.lastL2[key] = cur
	r := cur
	p.mu.Unlock()
	p.emit(Event{Type: "l2", L2: r})
}

func (p *Pipeline) onUnit(s *stationState, f dec.Frame, channel string, w l1.W, data []uint8) {
	key := asmKey{s.st.Idx, channel}
	p.mu.Lock()
	cur := p.asm[key]
	now := p.now()
	newRec := func() *L2Rec {
		p.noL2++
		r := &L2Rec{Idx: s.st.Idx, No: p.noL2, TSec: now, MHz: s.st.MHz, Channel: channel}
		p.l2recs = append(p.l2recs, r)
		if len(p.l2recs) > maxL2 {
			p.l2recs = p.l2recs[len(p.l2recs)-maxL2:]
		}
		return r
	}
	var out []*L2Rec
	if cur == nil || cur.State != "組立中" {
		cur = newRec()
		cur.State = "組立中"
		cur.FrameNos = append(cur.FrameNos, f.Number)
		p.asm[key] = cur
	}
	switch {
	case w.First && w.Last:
		cur.Units, cur.Remain = 1, 0
		cur.Stage = "ユニット 1（W 単独）→ 完結待ち"
		p.asm[key] = nil
		out = append(out, cur)
	case w.First:
		cur.Units, cur.Remain = 1, int(w.WO)
		cur.Stage = fmt.Sprintf("ユニット 1 / 残り %d（W 先頭）", w.WO)
		p.asm[key] = cur
		out = append(out, cur)
	case w.Last:
		cur.Units++
		cur.Remain = 0
		cur.Stage = fmt.Sprintf("ユニット %d（W 最終）→ 完結待ち", cur.Units)
		p.asm[key] = nil
		out = append(out, cur)
	case w.WO == 0:
		cur.Stage = "メッセージ無し（任意パターン）"
		cur.State = "メッセージ無し"
		p.asm[key] = nil
		out = append(out, cur)
	default:
		cur.Units++
		cur.Remain = int(w.WO)
		cur.Stage = fmt.Sprintf("ユニット %d / 残り %d（W 途中）", cur.Units, w.WO)
		out = append(out, cur)
	}
	if p.cfg.Ext != nil {
		if rows := p.cfg.Ext.ExpandUnit(channel, w, bitpack.ToBytes(data)); len(rows) > 0 {
			cur.Tree = append(cur.Tree, Node{Label: "ユニット（上位方式）",
				Value:    fmt.Sprintf("%d オクテット", len(data)/8),
				Note:     "T61 の LAPDM ではなく上位方式の器として読んだもの",
				Children: rowsToNodes(rows)})
		}
	}
	if cur != nil {
		p.lastL2[key] = cur
	}
	p.mu.Unlock()
	for _, r := range out {
		p.emit(Event{Type: "l2", L2: r})
	}
}

func l1Why(f dec.Frame, richChanged, syncBurst bool) string {
	switch {
	case syncBurst:
		return "同期バースト"
	case richChanged:
		return "RICH が変わった"
	case f.SACCH != nil:
		return "SACCH ユニット完結"
	case f.FACCH != nil:
		return "FACCH ユニット完結"
	case f.RCH != nil:
		return "RCH ユニット完結"
	case f.NV2 != nil:
		return "非音声2 ユニット完結"
	case f.Control != nil && f.Control.Uplink:
		if f.Control.OK {
			return "上り制御ユニット完結"
		}
		return ""
	case f.Control != nil:
		return "制御ユニット完結"
	}
	return ""
}

func l1Info(f dec.Frame) string {
	s := kindName(f.Kind)
	if f.Uplink {
		s += "(上り)"
	}
	if f.Flywheel {
		s += " 慣性"
	}
	switch {
	case f.A1RICH != nil && f.A1RICH.OK:
		s += " 付録1 " + a1ModeName(*f.A1RICH)
	case f.RICH.OK:
		s += " " + modeName(f.RICH)
	default:
		s += " RICH NG"
	}
	if f.Control != nil {
		ch := "制御"
		if f.Control.Uplink {
			ch = "上り制御"
		}
		if f.Control.OK {
			s += " " + ch + "完結"
		} else {
			s += " " + ch + " CRC不一致"
		}
	}
	if f.NV2 != nil {
		s += " 非音声2完結"
	}
	for _, fr := range f.Frag {
		switch {
		case fr.Done && fr.OK:
			s += fmt.Sprintf(" %s完結", fr.Channel)
		case fr.Done && fr.Filler:
			s += fmt.Sprintf(" %s空き枠", fr.Channel)
		case fr.Done:
			s += fmt.Sprintf(" %s CRC不一致", fr.Channel)
		default:
			s += fmt.Sprintf(" %s%d/%d", fr.Channel, fr.Have, fr.Need)
		}
	}
	return s
}

func (p *Pipeline) onMessage(s *stationState, f dec.Frame, m dec.Message) {
	key := asmKey{s.st.Idx, m.Channel}
	p.mu.Lock()
	r := p.lastL2[key]
	if r == nil {
		p.noL2++
		r = &L2Rec{Idx: s.st.Idx, No: p.noL2, TSec: p.now(), MHz: s.st.MHz, Channel: m.Channel}
		p.l2recs = append(p.l2recs, r)
		if len(p.l2recs) > maxL2 {
			p.l2recs = p.l2recs[len(p.l2recs)-maxL2:]
		}
	}
	r.State = "完結"
	r.Stage = fmt.Sprintf("完結（ユニット %d）", max(r.Units, 1))
	r.RawHex = hexOf(m.L2.Raw)
	r.Summary = fmt.Sprintf("%s | %s", m.L2.Address, m.L2.Control)
	if m.L2.Note != "" {
		r.Summary += "（読み替え）"
	}
	if m.Recovered {
		r.Summary += "（I' で組み直し）"
	}
	r.Tree = buildL2Tree(m.L2, p.cfg.Ext)
	r.Err = m.L2.Err
	no := r.No
	p.mu.Unlock()
	p.emit(Event{Type: "l2", L2: r})

	if m.L3 == nil {
		return
	}
	for _, mm := range m.L3.Messages {
		rec := &L3Rec{Idx: s.st.Idx, TSec: p.now(), MHz: s.st.MHz,
			Entity: mm.Entity, Name: mm.Name, RawHex: hexOf(mm.Raw), L2No: no, Err: mm.Err}
		for i, fl := range mm.Fields {
			rec.Tree = append(rec.Tree, Node{Label: fl.Name, Value: fl.Value, Note: mm.Note(i)})
		}
		for _, n := range m.L3.Notes {
			rec.Tree = append(rec.Tree, Node{Label: "規格の注記", Value: n})
		}
		p.storeL3(rec)
	}
}

func (p *Pipeline) onSignalLost(s *stationState, quiet float64) {
	if s.ext != nil {
		s.ext.Reset()
	}
	p.mu.Lock()
	for k, r := range p.asm {
		if k.idx != s.st.Idx || r == nil {
			continue
		}
		r.State = "破棄（信号が途切れた）"
		p.asm[k] = nil
		p.mu.Unlock()
		p.emit(Event{Type: "l2", L2: r})
		p.mu.Lock()
	}
	st := &s.st
	st.TA, st.SSC = 0, 0
	st.SBStale = st.SBID != ""
	s.sbKey = ""
	st.Synced = false
	st.RICHOK, st.RICHBits, st.RICHText, st.Mode, st.RICHNote = false, "", "", "", ""
	st.SACCHHex, st.FACCHHex, st.RCHRaw = "", "", 0
	st.NV2Hex, st.CtlHex, st.CtlE, st.SBNote = "", "", "", ""
	s.lastNV2 = ""
	s.dirty = true
	p.mu.Unlock()
	p.addLogFor(s.st.Idx, "system", "%.4f MHz 信号が %.0f 秒途切れたので仕切り直し", s.st.MHz, quiet)
}

func (p *Pipeline) onRecordStart(s *stationState, base, reason string) {
	r := Rec{Idx: s.st.Idx, MHz: s.st.MHz, TSec: p.now(), Base: base,
		Name: rec.Rel(p.cfg.Watch.LogDir, base), Reason: reason, Open: true}
	if s.voice != nil {
		s.voice.recordStart(base, reason)
	}
	p.mu.Lock()
	s.st.Recording, s.st.RecBase = true, base
	s.dirty = true
	if s.ext != nil {
		s.ext.RecordStart(base, reason)
	}
	p.recs = append(p.recs, r)
	if len(p.recs) > maxRecs {
		p.recs = p.recs[len(p.recs)-maxRecs:]
	}
	p.mu.Unlock()
	p.emit(Event{Type: "rec", Rec: &r})
	p.addLogFor(r.Idx, "record", "%.4f MHz 録音開始 %s", s.st.MHz, r.Name)
}

func (p *Pipeline) onRecordStop(s *stationState) {
	p.mu.Lock()
	s.st.Recording = false
	s.dirty = true
	base := s.st.RecBase
	r := Rec{Idx: s.st.Idx, MHz: s.st.MHz, TSec: p.now(), Base: base,
		Name: rec.Rel(p.cfg.Watch.LogDir, base), Open: false}
	p.mu.Unlock()
	res := RecStop{KeepIQ: true}
	if s.ext != nil {
		res = s.ext.RecordStop(base)
	}
	if s.voice != nil {
		res.Audio, res.Sec = s.voice.recordStop()
	}
	r.Audio, r.Sec = res.Audio, res.Sec
	p.mu.Lock()
	for i := range p.recs {
		if p.recs[i].Base == r.Base && p.recs[i].Open {
			p.recs[i].Open, p.recs[i].Audio, p.recs[i].Sec = false, r.Audio, r.Sec
		}
	}
	p.mu.Unlock()
	p.emit(Event{Type: "rec", Rec: &r})
	p.addLogFor(r.Idx, "record", "%.4f MHz 録音終了 %s", s.st.MHz, r.Name)
	if !res.KeepIQ && r.Base != "" {
		if err := os.Remove(rec.Path(r.Base, rec.IQ)); err == nil {
			p.mu.Lock()
			for i := range p.recs {
				if p.recs[i].Base == r.Base {
					p.recs[i].IQRemoved = true
				}
			}
			p.mu.Unlock()
			r.IQRemoved = true
			p.emit(Event{Type: "rec", Rec: &r})
			p.addLogFor(r.Idx, "record", "%.4f MHz 音声なしのため I/Q を削除 %s", s.st.MHz, r.Name)
		}
	}
}


func kindName(k l1.ChannelKind) string {
	switch k {
	case l1.KindControl:
		return "制御CH"
	case l1.KindService:
		return "通信CH"
	case l1.KindSyncBurst:
		return "同期バースト"
	}
	return "?"
}

func a1ModeName(r l1.A1RICH) string {
	switch {
	case r.IsVoice():
		return "音声"
	case r.IsIdleLine():
		return "空線"
	}
	return "非音声"
}

func modeName(r l1.RICH) string {
	if !r.HasMode() {
		return "利用者情報(Format 2)"
	}
	switch r.M {
	case 0:
		return "アイドル"
	case 1:
		return "音声"
	case 2:
		return "非音声1"
	case 3:
		return "非音声2"
	case 4:
		return "FACCH"
	case 5:
		return "空線"
	}
	return "予約"
}


func hexOf(b []byte) string {
	var sb strings.Builder
	for _, v := range b {
		fmt.Fprintf(&sb, "%02X", v)
	}
	return sb.String()
}


func (p *Pipeline) openLedger(chans []float64) []float64 {
	if p.cfg.LedgerDir == "" {
		return chans
	}
	led, err := ledger.Open(p.cfg.LedgerDir)
	if err != nil {
		p.addLog("system", "局台帳を開けませんでした（台帳なしで続けます）: %v", err)
		return chans
	}
	p.led, p.lastSave = led, time.Now()
	known := led.Frequencies()
	if len(known) == 0 {
		return chans
	}
	have := map[int64]bool{}
	for _, f := range chans {
		have[int64(math.Round(f*1e4))] = true
	}
	limit := p.cfg.Watch.ScanMax
	if limit <= 0 {
		limit = defaultWatchMax
	}
	var added, dropped []float64
	for _, mhz := range known {
		k := int64(math.Round(mhz * 1e4))
		if have[k] {
			continue
		}
		if len(chans)+len(added) >= limit {
			dropped = append(dropped, mhz)
			continue
		}
		have[k] = true
		added = append(added, mhz)
	}
	chans = append(chans, added...)
	sort.Float64s(chans)
	p.cfg.Watch.ChannelsMHz = chans
	p.addLog("system", "局台帳: %d 局を読み込み、%d 局を起動時から監視します（%s）",
		len(known), len(added), p.cfg.LedgerDir)
	if len(dropped) > 0 {
		p.addLog("system", "局台帳: 上限 %d 局を超えるので %d 局は走査に任せます", limit, len(dropped))
	}
	return chans
}

func (p *Pipeline) restoreFromLedger() {
	if p.led == nil {
		return
	}
	for _, c := range p.led.Channels() {
		s := p.addStation(c.MHz)
		if s == nil {
			continue
		}
		p.mu.Lock()
		st := &s.st
		if !p.pinnedMHz(c.MHz) {
			st.Watched = false
		}
		st.Ledger, st.Note = true, c.Note
		if !c.Last.IsZero() {
			st.LedgerLastMS = c.Last.UnixMilli()
		}
		if len(c.Orgs) > 0 {
			st.SBID, st.SBStale = c.OrgText(), true
		}
		s.dirty = true
		p.mu.Unlock()
	}
}

func (p *Pipeline) pinnedMHz(mhz float64) bool {
	for _, f := range p.cfg.Watch.ChannelsMHz {
		if math.Abs(f-mhz) < 1e-6 {
			return true
		}
	}
	return false
}

func (p *Pipeline) saveLedger() {
	if p.led == nil {
		return
	}
	now := time.Now()
	p.mu.Lock()
	type obs struct {
		mhz            float64
		frames, richOK int
	}
	list := make([]obs, 0, len(p.stations))
	for _, s := range p.stations {
		list = append(list, obs{s.st.MHz, s.st.Frames, s.st.RICHOKNum})
	}
	p.mu.Unlock()
	for _, o := range list {
		p.led.Observe(o.mhz, o.frames, o.richOK, now)
	}
	if err := p.led.Save(); err != nil {
		p.addLog("system", "局台帳を書けませんでした: %v", err)
	}
	p.mu.Lock()
	for _, s := range p.stations {
		if n := p.led.Note(s.st.MHz); n != s.st.Note {
			s.st.Note, s.dirty = n, true
		}
	}
	p.mu.Unlock()
}

func (p *Pipeline) stationWC(mhz float64) (int, bool) {
	for f, wc := range p.cfg.StationWC {
		if math.Abs(f-mhz) < 1e-4 {
			return wc, true
		}
	}
	return 0, false
}
