package watch

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"math"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/symysak/arib/internal/bitpack"
	"github.com/symysak/arib/internal/iqsrc"
	"github.com/symysak/arib/internal/rec"
	"github.com/symysak/arib/internal/rx"
	dec "github.com/symysak/arib/internal/std-t61/decoder"
	"github.com/symysak/arib/internal/std-t61/dsp"
	"github.com/symysak/arib/internal/std-t61/l1"
	"github.com/symysak/arib/internal/std-t61/l2"
	"github.com/symysak/arib/internal/std-t61/l3"
	"github.com/symysak/arib/internal/std-t61/pane"
)

type Config struct {
	SampleRate  float64
	Format      string
	CenterHz    float64
	ChannelsMHz []float64
	LogDir string
	System      string
	PreS, PostS float64
	MaxS        float64
	ControlS    float64
	ControlGap  float64
	VoiceOnly bool
	FollowControl bool
	IdleTCH bool
	BandIQ bool
	Scan bool
	ScanMax int
	ScanVerifySec float64
	ScanSNRdB float64
	ScanIntervalSec float64
	ScanQuietMin float64
	LedgerMHz []float64
	ScanEvictIdleMin float64
	ScanEvictUnsyncedMin float64
	ScanHeadS float64
	GainDB    float64
	AGC       bool
	RecordAll bool
	Detail bool
	LabelID func(id uint32) string
	ChannelNote func(mhz float64) string
	UnitText func(channel string, raw []byte) string
	RICHNote func(r l1.RICH) string
	SBNote   func(sb l1.SyncBurst) string
	ParseL2 func(channel string, raw []byte) l2.Frame
	AnnotateL3 func(channel string, l2f l2.Frame, sig *l3.Signal)
	AllowEmpty bool
	LogOut io.Writer
	Realtime bool
	Speed    float64
	FollowVFO bool
	Preset []float64
	Open func(o iqsrc.Options) (iqsrc.Source, error)
	Inputs []rx.InputConfig
}

type ChannelHook struct {
	Frame func(f dec.Frame)
	RecordStart func(base string, reason string)
	RecordStop func()
	Level func(dbfs float64)
	Stats func(st dec.Stats, coarseHz, cfoHz float64, locked bool)
	Const func(cs []dec.ConstSample)
	Reset func(quietSec float64)
	Closed func(dropped bool)
	Closing func()
	SetUplinkScramble func(init int)
}

type Hooks struct {
	NewChannel func(mhz float64) *ChannelHook
	Start func(describe string, sampleRate float64)
}

func Run(ctx context.Context, spec string, o Config, hooks Hooks) error {
	return NewEngine(spec, o, hooks).Run(ctx)
}

func NewEngine(spec string, o Config, hooks Hooks) *rx.Wide {
	return rx.NewWide(NewStandard(o, hooks), EngineConfig(spec, o, hooks))
}

func EngineConfig(spec string, o Config, hooks Hooks) rx.Config {
	return rx.Config{
		Source: spec, SampleRate: o.SampleRate, Format: o.Format, CenterHz: o.CenterHz,
		GainDB: o.GainDB, AGC: o.AGC, LogDir: o.LogDir, System: o.SystemName(), LogOut: o.LogOut,
		PreS: o.PreS, PostS: o.PostS, MaxS: o.MaxS, BandIQ: o.BandIQ, BandHint: bandHint,
		Channels: append([]float64(nil), o.ChannelsMHz...), Ledger: append([]float64(nil), o.LedgerMHz...),
		Scan: rx.ScanConfig{Enabled: o.Scan, Max: o.ScanMax, VerifySec: o.ScanVerifySec, SNRdB: o.ScanSNRdB,
			IntervalSec: o.ScanIntervalSec, QuietMin: o.ScanQuietMin, EvictIdleMin: o.ScanEvictIdleMin,
			EvictUnsyncedMin: o.ScanEvictUnsyncedMin, HeadS: o.ScanHeadS},
		OnStart: hooks.Start, AllowEmpty: o.AllowEmpty,
		Realtime: o.Realtime, Speed: o.Speed, FollowVFO: o.FollowVFO, Open: o.Open,
		Preset: append([]float64(nil), o.Preset...), Inputs: o.Inputs,
	}
}

const DefaultSystem = "std-t61"

func (o Config) SystemName() string {
	if o.System == "" {
		return DefaultSystem
	}
	return o.System
}

func bandHint(center float64) string {
	return fmt.Sprintf("再デコード: std-t61 live -no-ui -full-speed -input <このファイル>@%.4f -channels <局1>,<局2>,…\n", center)
}

type Standard struct {
	o     Config
	hooks Hooks
	upBoard atomic.Int32
}

func NewStandard(o Config, hooks Hooks) *Standard { return &Standard{o: o, hooks: hooks} }

func (*Standard) Name() string { return "T61" }

func (*Standard) Grid() float64 { return scanSlotHz }

func (t *Standard) NewChannel(c rx.ChannelConfig, sink rx.Sink) rx.Channel {
	w := newWatcher(c.MHz, c.OffHz, c.FsIn, t.o, sink)
	w.upBoard = &t.upBoard
	if t.hooks.NewChannel != nil {
		w.hook = t.hooks.NewChannel(c.MHz)
		if w.hook != nil {
			w.hook.SetUplinkScramble = func(init int) {
				if w.d != nil {
					w.d.SetUplinkScrambleFound(init)
				}
			}
		}
	}
	return w
}

func (*Standard) NewDetector(c rx.DetectorConfig) rx.Detector { return NewDetector(c) }

func NewDetector(c rx.DetectorConfig) rx.Detector {
	return newScanner(c.FsIn, c.CenterHz, c.SNRdB)
}

func (*Standard) Routes(*http.ServeMux, func(int) rx.Channel) {}

func (*Standard) Pane() fs.FS { return pane.FS() }


const ngHold = 50

const signalLostSec = 3.0

const richHold = 3

type richHoldState struct {
	cand, cur      uint32
	candRun, ngRun int
	ok             bool
}

func (h *richHoldState) push(r l1.RICH) {
	if r.OK {
		h.ngRun = 0
		if r.Raw == h.cand {
			h.candRun++
		} else {
			h.cand, h.candRun = r.Raw, 1
		}
		if h.candRun >= richHold && (!h.ok || h.cur != h.cand) {
			h.cur, h.ok = h.cand, true
		}
		return
	}
	h.ngRun++
	h.candRun = 0
	if h.ngRun >= ngHold {
		h.ok = false
	}
}

func (h *richHoldState) current(own l1.RICH) l1.RICH {
	if h.ok {
		return l1.RICHFromRaw(h.cur)
	}
	return own
}

func callLike(r l1.RICH) bool { return r.HasMode() && !r.IsIdle() && !r.IsIdleLine() }

func countsAsCall(held, own l1.RICH, flywheel bool) bool {
	if flywheel || !callLike(held) {
		return false
	}
	return !own.OK || callLike(own)
}

type watcher struct {
	mhz  float64
	ddc  *dsp.DDC
	d    *dec.Decoder
	o    Config
	sink rx.Sink

	peak float32

	richOK atomicInt
	frames atomicInt
	procMS atomicInt
	state atomic.Int32

	recOn      bool
	recStart   time.Time
	recLast    time.Time
	recReason  string
	lastCtlRec time.Time

	lastState string
	lastNV2 string
	lastGlobal map[string]loggedLine
	hold       richHoldState
	sbRun      int
	ctlRun     int
	callRun    int
	events     int
	lastFrame  time.Time
	lostDone   bool
	hook       *ChannelHook
	idle *idleTCH
	upBoard *atomic.Int32
	upSeen  int32
}

func newWatcher(mhz, offHz, fsIn float64, o Config, sink rx.Sink) *watcher {
	ddc := dsp.NewDDC(fsIn, offHz, 48000)
	w := &watcher{mhz: mhz, ddc: ddc, o: o, sink: sink}
	if o.IdleTCH {
		w.idle = newIdleTCH()
	}
	w.d = dec.New(dec.Config{SampleRate: ddc.SampleRate(), NoConstellation: !o.Detail, KeepRaw: o.Detail,
		ParseL2: o.ParseL2, AnnotateL3: o.AnnotateL3})
	w.peak = 1
	return w
}

func (w *watcher) Resync() {
	w.d.ResetSync()
	w.hold = richHoldState{}
	w.sbRun, w.ctlRun, w.callRun = 0, 0, 0
	w.sink.Log("%.4f MHz: 取りこぼしがあったので同期を取り直す", w.mhz)
}

func (w *watcher) Feed(iq []complex64) {
	y := w.ddc.Process(iq)
	if len(y) == 0 {
		return
	}
	var sum float64
	for _, v := range y {
		a := absC(v)
		if a > w.peak {
			w.peak = a
		}
		sum += float64(a) * float64(a)
	}
	if w.hook != nil && w.hook.Level != nil {
		rms := math.Sqrt(sum / float64(len(y)))
		w.hook.Level(20 * math.Log10(math.Max(rms, 1e-9)/math.Max(float64(w.peak), 1e-9)))
	}
	w.sink.IQ(y, w.ddc.SampleRate())
	w.applyUplinkScramble()
	for _, f := range w.d.Feed(y) {
		if w.idle != nil {
			f = w.idle.mark(f)
		}
		w.onFrame(f)
		if w.hook != nil && w.hook.Frame != nil {
			w.hook.Frame(f)
		}
	}
	if w.hook != nil && w.hook.Const != nil {
		if cs := w.d.TakeConst(); len(cs) > 0 {
			w.hook.Const(cs)
		}
	}
	w.richOK.Store(w.d.Stats.RICHOK)
	w.frames.Store(w.d.Stats.Frames)
	w.procMS.Add(int(float64(len(y)) / w.ddc.SampleRate() * 1000))
	if w.hook != nil && w.hook.Stats != nil {
		w.hook.Stats(w.d.Stats, w.d.CoarseHz(), w.d.CFOHz(), w.d.Locked())
	}
	if w.recOn {
		now := time.Now()
		if now.Sub(w.recLast).Seconds() > w.o.PostS || now.Sub(w.recStart).Seconds() > w.o.MaxS {
			w.stopRec()
		}
	}
	if !w.lastFrame.IsZero() && !w.lostDone {
		if q := time.Since(w.lastFrame).Seconds(); q > signalLostSec {
			w.d.ResetAssembly()
			w.hold = richHoldState{}
			w.lostDone = true
			w.sink.Log("%.4f MHz: 信号が %.0f 秒途切れたので組立を仕切り直し", w.mhz, q)
			if w.hook != nil && w.hook.Reset != nil {
				w.hook.Reset(q)
			}
		}
	}
}

func (w *watcher) Close(dropped bool) {
	if w.hook != nil && w.hook.Closing != nil {
		w.hook.Closing()
	}
	if w.recOn {
		w.stopRec()
	}
	if w.hook != nil && w.hook.Closed != nil {
		w.hook.Closed(dropped)
	}
}

func (w *watcher) Lock() (bool, float64, string) {
	n, fr := w.richOK.Load(), w.frames.Load()
	score := 0.0
	if fr > 0 {
		score = float64(n) / float64(fr)
	}
	return scanLockedNow(fr, n), score, fmt.Sprintf("RICH OK %d/%d フレーム", n, fr)
}

func (w *watcher) Counters() rx.Counters {
	return rx.Counters{Frames: w.frames.Load(), Good: w.richOK.Load(), ProcSec: float64(w.procMS.Load()) / 1000}
}

func (w *watcher) State() rx.State { return rx.State(w.state.Load()) }

func (w *watcher) Snapshot(map[string]any) {}

func (w *watcher) Retune() {}

func (w *watcher) shareUplinkScramble(f dec.Frame) {
	if f.UplinkScrambleFound > 0 {
		w.sink.Log("%.4f MHz: 上りの同期バーストからスクランブルを当てた（PN 初期値 0x%03X）。この局の上りに使う", w.mhz, f.UplinkScrambleFound-1)
	}
	if f.UplinkControlScrambleFound > 0 {
		w.sink.Log("%.4f MHz: 上り制御のスクランブルを当てた（PN 初期値 0x%03X。カラーコード由来と違う）。この局の上りに使う", w.mhz, f.UplinkControlScrambleFound-1)
	}
	v := int32(f.UplinkScrambleAssigned)
	if v <= 0 || w.upBoard == nil {
		return
	}
	if old := w.upBoard.Swap(v); old != v {
		w.sink.Log("%.4f MHz: 基地局が上りの通信チャネルのスクランブルを指定した（PN 初期値 0x%03X）。帯域の局の上りに使う", w.mhz, v-1)
	}
	w.upSeen = v
}

func (w *watcher) applyUplinkScramble() {
	if w.upBoard == nil {
		return
	}
	if v := w.upBoard.Load(); v != w.upSeen {
		w.upSeen = v
		w.d.SetUplinkScramble(int(v) - 1)
	}
}

func (w *watcher) onFrame(f dec.Frame) {
	w.lastFrame, w.lostDone = time.Now(), false
	w.shareUplinkScramble(f)
	var reason string
	var cur l1.RICH
	if f.A1RICH != nil {
		reason = w.a1Trigger(f)
	} else {
		reason, cur = w.richTrigger(f)
	}
	if w.idle != nil && (strings.HasPrefix(reason, "通話") || strings.HasPrefix(reason, "RICH")) &&
		(f.TCHIdle || (w.idle.quiet() && !f.RICH.IsVoice())) {
		reason = ""
	}
	if reason != "" && !strings.HasPrefix(reason, "制御チャネル") &&
		!strings.Contains(reason, "追随") {
		w.sink.MarkActive(time.Duration(w.o.PostS+2) * time.Second)
	}
	if reason == "" && w.o.FollowControl && cur.IsControlChannel() {
		if on, by := w.sink.BandActive(); on {
			reason = fmt.Sprintf("%.4f MHz の通話に追随（制御チャネル）", by)
		}
	}
	if reason != "" {
		w.events++
		if !w.recOn {
			w.startRec(reason, f)
		}
		w.recLast = time.Now()
		if reason == "制御チャネル" && time.Since(w.recStart).Seconds() > w.o.ControlS {
			w.stopRec()
			w.lastCtlRec = time.Now()
		}
	}
	if u := f.Control; u != nil && u.OK && !u.Uplink && u.RN {
		line := fmt.Sprintf("%.4f MHz: 衝突制御 E %s（W %s %X）", w.mhz, u.EText(), u.W, bitpack.ToBytes(u.Data))
		w.sink.Log("%s", line)
		if w.recOn {
			w.sink.RecNote(0, line)
		}
	}
	if f.NV2Msg != nil {
		if h := fmt.Sprintf("%X", f.NV2Msg); h != w.lastNV2 {
			w.lastNV2 = h
			line := fmt.Sprintf("%.4f MHz: 非音声2 %s", w.mhz, h)
			if w.o.UnitText != nil {
				if t := w.o.UnitText("NV2", f.NV2Msg); t != "" {
					line += " | " + t
				}
			}
			if w.recOn {
				w.sink.Log("%s", line)
			}
			if w.recOn {
				w.sink.RecNote(0, line)
			}
		}
	}
	for _, m := range f.Messages {
		line := fmt.Sprintf("%.4f MHz: L2[%s] %s | %s", w.mhz, m.Channel, m.L2.Address, m.L2.Control)
		if m.L3 != nil {
			line += " | " + m.L3.Summary()
			for _, msg := range m.L3.Messages {
				for i, fld := range msg.Fields {
					line += fmt.Sprintf("\n    %s.%s: %s", msg.Entity, fld.Name, fld.Value)
					if n := msg.Note(i); n != "" {
						line += "  〔" + n + "〕"
					}
				}
			}
			for _, n := range m.L3.Notes {
				line += "\n    注記: " + n
			}
		} else if len(m.L2.Info) > 0 {
			line += fmt.Sprintf(" 余り=%X", m.L2.Info)
		}
		if m.L2.Note != "" {
			line += fmt.Sprintf("\n    読み替え: %s\n    生: %X", m.L2.Note, m.L2.Raw)
		}
		if w.o.UnitText != nil {
			if t := w.o.UnitText(m.Channel, m.L2.Raw); t != "" {
				line += " | " + t
			}
		}
		if (w.recOn || m.L3 != nil) && w.logGlobal(m.Channel, m.L2.Address, m.L3, line) {
			w.sink.Log("%s", line)
		}
		if w.recOn {
			w.sink.RecNote(0, line)
		}
	}
}

const globalRepeatLog = 10 * time.Minute

type loggedLine struct {
	line string
	at   time.Time
}

func (w *watcher) logGlobal(ch string, a l2.Address, sig *l3.Signal, line string) bool {
	if !a.IsGlobal() {
		return true
	}
	key := ch
	if sig != nil {
		key += "|" + sig.Summary()
	}
	now := time.Now()
	if p, ok := w.lastGlobal[key]; ok && p.line == line && now.Sub(p.at) < globalRepeatLog {
		return false
	}
	if w.lastGlobal == nil {
		w.lastGlobal = map[string]loggedLine{}
	}
	w.lastGlobal[key] = loggedLine{line, now}
	return true
}

func (w *watcher) richTrigger(f dec.Frame) (reason string, cur l1.RICH) {
	w.hold.push(f.RICH)
	state := f.Kind.String()
	if f.Uplink {
		state += "(上り)"
	}
	cur = w.hold.current(f.RICH)
	if w.hold.ok {
		state += " RICH " + cur.Bits17()
	} else {
		state += " RICH NG"
	}
	if state != w.lastState {
		note := ""
		if w.o.RICHNote != nil && w.hold.ok {
			if n := w.o.RICHNote(cur); n != "" {
				note = "  〔" + n + "〕"
			}
		}
		w.sink.Log("%.4f MHz: %s  %s%s", w.mhz, state, cur.Describe(w.d.Edition()), note)
		w.lastState = state
	}
	if f.Kind == l1.KindSyncBurst && !f.Flywheel && f.SB != nil && f.SB.OK {
		w.sbRun++
	} else {
		w.sbRun = 0
	}
	if f.Kind == l1.KindControl && !f.Flywheel && f.RICH.OK {
		w.ctlRun++
	} else {
		w.ctlRun = 0
	}
	if countsAsCall(cur, f.RICH, f.Flywheel) {
		w.callRun++
	} else {
		w.callRun = 0
	}
	switch pickTrigger(w.o, cur, f.RICH, f.Flywheel, w.hold.ok, w.sbRun, w.ctlRun, w.callRun, w.recOn, time.Since(w.lastCtlRec).Seconds()) {
	case trigCall:
		reason = "通話 " + cur.Describe(w.d.Edition())
	case trigSyncBurst:
		reason = "同期バースト " + f.SB.Describe(w.d.Edition())
		if w.o.LabelID != nil {
			if lbl := w.o.LabelID(f.SB.ID); lbl != "" {
				reason = strings.Replace(reason, fmt.Sprintf("ID=#%06X", f.SB.ID&0xFFFFFF), "ID="+lbl, 1)
			}
		}
		if w.o.SBNote != nil {
			if n := w.o.SBNote(*f.SB); n != "" {
				reason += " 〔" + n + "〕"
			}
		}
	case trigRICH:
		reason = "RICH " + cur.Describe(w.d.Edition())
	case trigRecordAll:
		reason = "record-all"
	case trigControl:
		reason = "制御チャネル"
	}
	return reason, cur
}

func (w *watcher) a1Trigger(f dec.Frame) string {
	r := *f.A1RICH
	state := "付録1 RICH NG"
	if r.OK {
		switch {
		case r.IsVoice():
			state = "付録1 Vフレーム(音声)"
		case r.IsIdleLine():
			state = "付録1 空線信号"
		default:
			state = "付録1 Uフレーム(非音声)"
		}
	}
	if state != w.lastState {
		w.sink.Log("%.4f MHz: %s  %s", w.mhz, state, r.Describe())
		w.lastState = state
	}
	w.sbRun, w.ctlRun = 0, 0
	if r.OK && !r.IsIdleLine() && !f.Flywheel {
		w.callRun++
	} else if r.OK || f.Flywheel {
		w.callRun = 0
	}
	if w.callRun >= callMinRun {
		return "通話 付録1 " + r.Describe()
	}
	if w.o.RecordAll && r.OK {
		return "record-all"
	}
	return ""
}

type trigger int

const (
	trigNone trigger = iota
	trigCall
	trigSyncBurst
	trigRICH
	trigRecordAll
	trigControl
)

const callMinRun = 2

func pickTrigger(o Config, cur, own l1.RICH, flywheel, curOK bool, sbRun, ctlRun, callRun int, recording bool, sinceCtlRecSec float64) trigger {
	if o.VoiceOnly {
		if callRun >= callMinRun {
			return trigCall
		}
		return trigNone
	}
	switch {
	case sbRun >= 2:
		return trigSyncBurst
	case curOK && !flywheel && !cur.IsIdleLine() && !(own.OK && own.IsIdleLine()):
		return trigRICH
	case o.RecordAll && curOK:
		return trigRecordAll
	case ctlRun >= 5:
		if !recording && sinceCtlRecSec < o.ControlGap {
			return trigNone
		}
		return trigControl
	}
	return trigNone
}

func (w *watcher) startRec(reason string, f dec.Frame) {
	base := w.sink.RecOpen(0, "", 0)
	if base == "" {
		return
	}
	w.recOn, w.recStart, w.recLast, w.recReason = true, time.Now(), time.Now(), reason
	w.state.Store(int32(rx.StateActive))
	w.sink.RecNote(0, fmt.Sprintf("周波数 %.4f MHz\n開始 %s\n理由 %s\nRICH %s %s\n入力レート %.0f Hz（DDC 後、std-t61 <file> -offset 0 で再デコード可）",
		w.mhz, time.Now().Format(time.RFC3339), reason, f.RICH.Bits17(), f.RICH.Describe(w.d.Edition()), w.ddc.SampleRate()))
	if w.o.ChannelNote != nil {
		if n := w.o.ChannelNote(w.mhz); n != "" {
			w.sink.RecNote(0, "メモ "+n)
		}
	}
	w.sink.RecNote(0, "")
	w.sink.Log("%.4f MHz: 録音開始 %s (%s)", w.mhz, filepath.Base(rec.Path(base, rec.IQ)), reason)
	if w.hook != nil && w.hook.RecordStart != nil {
		w.hook.RecordStart(base, reason)
	}
}

func (w *watcher) stopRec() {
	w.sink.RecClose(0, 0)
	w.recOn = false
	w.state.Store(int32(rx.StateIdle))
	if w.hook != nil && w.hook.RecordStop != nil {
		w.hook.RecordStop()
	}
}

func (w *watcher) Summary() string {
	s := w.d.Stats
	lock := "未同期"
	if w.d.Locked() {
		lock = "同期中"
	}
	return fmt.Sprintf("%.4f MHz: %s フレーム %d RICH OK %d SACCH %d/%d（空き枠 %d）イベント %d",
		w.mhz, lock, s.Frames, s.RICHOK, s.SACCHOK, s.SACCHUnits-s.SACCHFiller, s.SACCHFiller, w.events)
}


type atomicInt struct {
	mu sync.Mutex
	v  int
}

func (a *atomicInt) Add(n int)   { a.mu.Lock(); a.v += n; a.mu.Unlock() }
func (a *atomicInt) Store(n int) { a.mu.Lock(); a.v = n; a.mu.Unlock() }
func (a *atomicInt) Load() int   { a.mu.Lock(); defer a.mu.Unlock(); return a.v }

func absC(v complex64) float32 {
	return float32(math.Hypot(float64(real(v)), float64(imag(v))))
}
