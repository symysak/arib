package server

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"math"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/symysak/arib/internal/rec"
	"github.com/symysak/arib/internal/rx"
	"github.com/symysak/arib/internal/std-t79/decoder"
	"github.com/symysak/arib/internal/std-t79/dsp"
	"github.com/symysak/arib/internal/std-t79/l1"
	"github.com/symysak/arib/internal/std-t79/l2"
	"github.com/symysak/arib/internal/std-t79/l3"
	"github.com/symysak/arib/internal/voice"
)

const (
	ddcRate = 4 * dsp.SymbolRate
	callStartFrames = 3
	callIdleFrames = 25
	callLostSec = 3.0
	callMaxSec = 15 * 60
	recTailSec = 0.5
	recLagSec   = 0.5
	historyMax  = 400
	emitSec     = 0.25
	lockMinSW   = 20
	syncHoldSec = 1.0
	upHoldSec = 1.0
)

type callState struct {
	talker, selcall int
	lastPICH        string
	dupWith int
	handoffDone bool
	lastTalker int
	talkerT    float64
	on         bool
	base       string
	t0         float64
	seq      int64
	lastBusy float64
	busyRun  int
	idleRun  int
	early    []decoder.TCHFrame
	tchW     *bufio.Writer
	tchF     *os.File
	idxW     *bufio.Writer
	idxF     *os.File
	calls    *os.File
	voice    int
	facch    int
	init     int
	known    bool
	tr       *voice.Tracker
	hist     []decoder.TCHFrame
	audio    *audioBuf
	pendCall *l3.Call
	pendCtl  float64
	pendSrc  string
	upFrame  int64
	upRun    int
	lastRCH  string
	mode     l3.CallType
	modeRank int
	modeSrc  string
	stats    modeStats
	peer     string
	lastCC   string
	data bool
	vclock string
}

type Channel struct {
	std  *Standard
	cfg  rx.ChannelConfig
	sink rx.Sink
	ddc  *dsp.DDC
	dec  *decoder.Decoder
	isUp bool
	up *decoder.UpDecoder
	unitOK atomic.Int64
	rate   float64

	proc     int64
	lastEmit float64
	calls    [4]callState
	callSeq int64
	idle   [4]*idleTCH
	vs     voiceState
	mix    audioMixer
	seen   map[string]float64
	sacchR [4]l2.Reassembler
	facchR [4]l2.Reassembler
	cacMiss int
	cacOff  bool
	toDirect, directLeak bool
	dirScr     int
	lastSW     [4]float64
	l1s        [4]l1State
	folds      map[string]*l2Fold
	net        netState
	constBuf   []float32
	constPhase int

	mu       sync.Mutex
	car      CarrierInfo
	phaseAvg float64
	kindCount [4]map[string]int
	slotView  [4]string
	orgs      []string
	codecName string
	org       string
	maker int
	vend   vendorState
	nowT   float64
	lastT  float64
	recOn  int
	forget bool
	closed bool
	ciN    [4]int
	upCtl  bool
	ccN    map[int]int
	ids    [4]int
}

func newChannel(s *Standard, c rx.ChannelConfig, sink rx.Sink) *Channel {
	ddc := dsp.NewDDC(c.FsIn, c.OffHz, ddcRate)
	ch := &Channel{std: s, cfg: c, sink: sink, ddc: ddc, rate: ddc.SampleRate(), ccN: map[int]int{}}
	_, direct := c.Opt.(*DirectOpt)
	ch.isUp = IsUplinkMHz(c.MHz) || direct
	ch.newDecoder(c.StartSec)
	ch.car.LevelDB = math.Inf(-1)
	ch.lastEmit = math.Inf(-1)
	ch.nowT = c.StartSec
	if s.ledgerOrgs != nil {
		if orgs := s.ledgerOrgs(c.MHz); len(orgs) > 0 {
			ch.org = strings.Join(orgs, " / ") + "（前回まで）"
		}
	}
	ch.initVoice()
	for i := range ch.kindCount {
		ch.kindCount[i] = map[string]int{}
		ch.slotView[i] = "未同期"
	}
	for i := range ch.calls {
		ch.calls[i].talker, ch.calls[i].selcall, ch.calls[i].dupWith, ch.calls[i].lastTalker = -1, -1, -1, -1
		ch.dirScr = -1
	}
	for i := range ch.car.Slots {
		ch.car.Slots[i] = SlotInfo{Num: i, SW: "-", Kind: "未同期", SFPos: -1, ID: -1, Scramble: "-"}
		ch.ids[i] = -1
	}
	return ch
}

func (c *Channel) dirName() string {
	if k := c.directKind(); k != "" {
		return k
	}
	if c.up != nil {
		return "上り"
	}
	return "下り"
}

func (c *Channel) dirPrefix() string {
	if c.up != nil {
		return "上り "
	}
	return ""
}

func (c *Channel) chLabel(s *decoder.Slot, ch string) string {
	if ch == "CAC" && s != nil && s.CACCh != l1.CACUnknown {
		ch += "/" + s.CACCh.String()
	}
	if c.up != nil {
		return ch + "↑"
	}
	return ch
}

func (c *Channel) newDecoder(start float64) {
	if o := c.directOpt(); o != nil {
		c.up = decoder.NewUp(decoder.UpConfig{SampleRate: c.rate, StartSec: start, Direct: true, Mode: o.Mode})
		if o.Scramble >= 0 {
			c.up.SetScramble(-1, l1.InitFromPattern(o.Scramble))
		}
		return
	}
	if c.isUp {
		c.up = decoder.NewUp(decoder.UpConfig{SampleRate: c.rate, StartSec: start, WatchDirect: c.std.cfg.DirectScan})
		return
	}
	c.dec = decoder.New(decoder.Config{SampleRate: c.rate, StartSec: start, Constellation: true})
}

func (c *Channel) setScramble(slot, init int) {
	if c.up != nil {
		c.up.SetScramble(slot, init)
		return
	}
	c.dec.SetScramble(slot, init)
}

func (c *Channel) scramble(slot int) (int, bool) {
	if c.up != nil {
		return c.up.Scramble(slot)
	}
	return c.dec.Scramble(slot)
}

func (c *Channel) ID() int       { return c.cfg.ID }
func (c *Channel) MHz() float64  { return c.cfg.MHz }
func (c *Channel) tNow() float64 { return c.cfg.StartSec + float64(c.proc)/c.rate }

func (c *Channel) Feed(wide []complex64) {
	y := c.ddc.Process(wide)
	if len(y) == 0 {
		return
	}
	c.applyHints()
	c.sink.IQ(y, c.rate)
	level := levelDB(y)
	var slots []decoder.Slot
	if c.up != nil {
		slots = c.up.Feed(y)
	} else {
		slots = c.dec.Feed(y)
	}
	c.proc += int64(len(y))
	now := c.tNow()
	for i := range slots {
		c.handle(&slots[i], now)
	}
	c.watchDirect()
	for n := range c.calls {
		cs := &c.calls[n]
		if cs.on && now-cs.lastBusy > c.callLost() {
			c.endCall(n, now, "同期ワードが途絶えた")
		} else if cs.on && now-cs.t0 > callMaxSec {
			c.endCall(n, now, "上限で切り替え")
		}
	}
	c.mu.Lock()
	if c.up != nil {
		st := c.up.Stats
		c.car.TSec, c.car.Locked = now, c.up.LastBurstSec() >= 0 && now-c.up.LastBurstSec() < upHoldSec
		c.car.CoarseHz, c.car.LevelDB = 0, level
		c.car.SlotsN, c.car.SWFound = st.Bursts, st.Bursts
		c.car.CACOK, c.car.CACTotal, c.car.SACCHOK, c.car.SACCHN = st.CACOK, st.Control, st.SACCHOK, st.SACCHTotal
		c.car.FACCHOK, c.car.SBOK, c.car.SBTotal = st.FACCHOK, st.SBOK, st.SBTotal
	} else {
		st := c.dec.Stats
		c.car.TSec, c.car.Locked = now, c.dec.Locked()
		c.car.CoarseHz, c.car.CFOHz, c.car.LevelDB = round2(c.dec.CoarseHz()), round2(c.dec.CFOHz()), level
		c.car.SlotsN, c.car.SWFound, c.car.Locks, c.car.Losses = st.Slots, st.SWFound, st.Locks, st.Losses
		c.car.CACOK, c.car.CACTotal, c.car.SACCHOK, c.car.SACCHN = st.CACOK, st.CACTotal, st.SACCHOK, st.SACCHTotal
		c.car.FACCHOK, c.car.SBOK, c.car.SBTotal = st.FACCHOK, st.SBOK, st.SBTotal
	}
	c.car.Kind, c.car.CC = c.kindLocked()
	for n := range c.car.Slots {
		c.car.Slots[n].Call = c.calls[n].on
		c.car.Slots[n].Voice = c.calls[n].voice
		c.car.Slots[n].ID = c.ids[n]
		if c.up != nil {
			if now-c.lastSW[n] > upHoldSec {
				c.car.Slots[n].Kind = "送信なし"
			}
		} else if c.car.Locked && now-c.lastSW[n] > syncHoldSec {
			c.car.Slots[n].Kind = "未同期"
		}
	}
	if !c.car.Locked && c.up == nil {
		for n := range c.car.Slots {
			c.car.Slots[n].Kind = "未同期"
		}
	}
	c.nowT = now
	car := c.car
	car.Audio = c.mix.state()
	emit := now-c.lastEmit >= emitSec
	if emit {
		for n := range c.slotView {
			c.slotView[n] = summarizeKinds(c.kindCount[n], c.car.Slots[n].Kind)
			clear(c.kindCount[n])
		}
	}
	c.mu.Unlock()
	c.emitConst()
	if emit {
		c.lastEmit = now
		c.sink.Emit(Event{Type: EvT79, Data: &Detail{Carrier: car}})
		stn := c.station()
		c.sink.Emit(Event{Type: EvStation, Station: &stn})
	}
}

func summarizeKinds(cnt map[string]int, cur string) string {
	if cur == "未同期" {
		return cur
	}
	best, bn := "", 0
	for _, k := range []string{"音声", "FACCH", "同期バースト", "制御", "空線"} {
		if n := cnt[k]; n > bn {
			best, bn = k, n
		}
	}
	switch {
	case best != "":
		return best
	case cnt["慣性"] > 0:
		return "慣性"
	}
	return cur
}

func (c *Channel) kindLocked() (string, int) {
	kind := ""
	switch {
	case c.directOpt() != nil:
		kind = c.directKind()
	case c.up != nil && c.upCtl:
		kind = "上り（制御）"
	case c.up != nil:
		kind = "上り"
	case c.ciN[0] > c.ciN[3] && c.ciN[0] > 0:
		kind = "制御キャリア"
	case c.ciN[3] > 0:
		kind = "通信キャリア"
	}
	cc, best := -1, 0
	for k, n := range c.ccN {
		if n > best {
			cc, best = k, n
		}
	}
	return kind, cc
}

func (c *Channel) handle(s *decoder.Slot, now float64) {
	n := s.Num
	cs := &c.calls[n]
	info := SlotInfo{Num: n, SW: s.SW.Name(), SFPos: s.SFPos, ID: -1, LastT: s.TSec, CC: s.CC, PhaseE: round2(s.PhaseErrDeg)}
	busy := false
	switch {
	case s.SW.Kind == l1.SWNone:
		info.Kind = "慣性"
	case s.Up && s.Control:
		info.Kind = "制御（上り）"
	case s.Control:
		info.Kind = "制御"
	case s.SW.Kind == l1.SWBurst:
		info.Kind, busy = "同期バースト", true
	case s.SW.Kind == l1.SWIdle:
		info.Kind = "空線"
	case s.SW.Inverted:
		info.Kind, busy = "FACCH", true
	default:
		info.Kind, busy = "音声", true
	}
	if s.Up && s.Direct == nil && (info.Kind == "音声" || info.Kind == "FACCH") && !cs.on && s.Frame != cs.upFrame+1 &&
		s.SACCH == nil && s.FACCH == nil {
		info.Kind = "送信なし"
	}
	if d := s.Direct; d != nil {
		info.SW = d.Name()
		info.Kind = d.Kind(c.directOpt() != nil && c.directOpt().Control)
		if s.DSB != nil && s.DSB.OK && s.DSB.BI == 0 {
			busy = false
		}
	}
	silent := false
	if c.std.cfg.IdleTCH && busy && !s.Up && s.Direct == nil {
		if c.idle[n] == nil {
			c.idle[n] = newIdleTCH()
		}
		d, speech, idle := c.idle[n], false, false
		for i := range s.TCH {
			s.TCH[i] = d.mark(s.TCH[i])
			speech = speech || s.TCH[i].Voice
			idle = idle || s.TCH[i].Idle
		}
		if silent = !speech && (idle || d.quiet()); silent {
			busy = false
		}
	}
	switch {
	case !s.InitKnown:
		info.Scramble = "未確定"
	case s.Init == l1.InitAllOnes:
		info.Scramble = "オール 1"
	default:
		info.Scramble = fmt.Sprintf("0x%02X", l1.PatternOf(s.Init))
	}
	if s.SW.Kind != l1.SWNone {
		c.lastSW[n] = s.TSec
	}

	c.mu.Lock()
	if s.SW.Kind != l1.SWNone {
		c.lastT = s.TSec
	}
	if s.Up {
		c.car.CFOHz = round2(s.CFOHz)
		if s.Control {
			c.upCtl = true
		}
		if s.SW.Kind != l1.SWBurst && s.Direct == nil {
			c.ccN[s.CC]++
		}
	} else if s.SW.Kind != l1.SWNone && s.SW.Kind != l1.SWBurst {
		c.ciN[s.CI]++
		c.ccN[s.CC]++
		if c.ciN[s.CI] > 1<<20 {
			c.ciN = [4]int{}
			c.ccN = map[int]int{}
		}
	}
	info.ID = c.ids[n]
	info.Peer = cs.peer
	if cs.on {
		info.Mode, info.ModeNote = cs.mode.Name, cs.mode.Downlink
	}
	if s.SW.Kind != l1.SWNone {
		c.phaseAvg = round2(0.9*c.phaseAvg + 0.1*s.PhaseErrDeg)
	}
	c.car.Slots[n] = info
	c.kindCount[n][info.Kind]++
	c.mu.Unlock()

	if s.FACCH != nil {
		cs.facch++
		c.note(n, fmt.Sprintf("facch %d %s %s", s.Frame, s.FACCH.W, hexOctets(s.FACCH.Bytes())))
	}
	if s.SW.Kind == l1.SWNone {
		c.sacchR[n].Reset()
		c.facchR[n].Reset()
	}
	c.layers(s)
	c.noteEcho(s)
	c.noteRCH(s)
	if s.Up && unitOK(s) {
		c.unitOK.Add(1)
	}
	c.emitL1(s)
	if s.SB != nil && s.SB.OK {
		sb := s.SB
		text := fmt.Sprintf("ID %d（識別子 %d・個別番号 %d）B=%d TA=%d SSC=%d CC=%d", sb.ID, sb.ID>>19, sb.IDNumber(), sb.B, sb.TA, sb.SSC, sb.CC)
		if c.fresh(fmt.Sprintf("sb/%d/%d/%d", n, sb.ID, sb.B), s.TSec) {
			c.emitCall(CallRec{TSec: s.TSec, Slot: n, Channel: "SB", Kind: "同期バースト", Text: text, ToSlot: -1, Ref: -1, ID: sb.ID})
		}
		c.mu.Lock()
		c.ids[n] = sb.ID
		c.mu.Unlock()
		c.note(n, fmt.Sprintf("sb %d %s", s.Frame, text))
	}
	c.notePress(s)
	c.pollTalker(n, now)
	if s.SFLost {
		c.logf(n, "l1", "warn", "ST#%d スーパーフレーム同期外れ（SF 先頭の同期ワードが N4 = 10 回続けて無い。§4.1.20.1）。SF 位置が分かるまで SACCH・RCH は組まない", n)
	}
	release := false
	if s.Direct != nil {
		release = c.noteDirect(s)
		if cs.talker >= 0 || cs.selcall >= 0 {
			cs.peer = cs.directPeer()
		}
	}

	if s.Up {
		if s.Frame == cs.upFrame+1 {
			cs.upRun++
		} else {
			cs.upRun = 1
		}
		cs.upFrame = s.Frame
	}
	sure := (s.SB != nil && s.SB.OK) || s.FACCH != nil || s.SACCH != nil || (s.DSB != nil && s.DSB.OK && s.DSB.BI == 1)
	if busy && !s.Control && (s.SW.Errs <= 1 || sure || s.Up) {
		cs.busyRun++
		if s.Up {
			cs.busyRun = cs.upRun
		}
		cs.idleRun = 0
		cs.lastBusy = now
		if !cs.on && (cs.busyRun >= callStartFrames || sure) {
			c.startCall(n, s, now)
		}
	} else if s.SW.Kind == l1.SWIdle || s.Control || silent {
		cs.busyRun = 0
		cs.idleRun++
	} else {
		cs.busyRun = 0
	}
	if cs.on {
		cs.stats.observe(s, info.Kind == "音声" || (s.Direct != nil && !s.Direct.SB && !s.Direct.Ctl && s.Direct.Voice))
		if cs.stats.voice > 0 && cs.stats.voice%50 == 0 {
			c.modeInfer(n)
		}
	}
	for _, f := range s.TCH {
		c.probe(n, f)
		switch {
		case cs.on:
			c.writeTCH(n, f)
			c.pushTracker(n, f)
		case cs.busyRun > 0:
			cs.early = append(cs.early, f)
		}
	}
	if !cs.on && cs.busyRun == 0 {
		cs.early = cs.early[:0]
	}
	if cs.on && s.InitKnown && (!cs.known || cs.init != s.Init) {
		cs.init, cs.known = s.Init, true
		c.note(n, fmt.Sprintf("scramble %s（SACCH / FACCH の CRC16 で確かめた）", info.Scramble))
		c.logf(n, "call", "info", "ST#%d の%sスクランブルは %s", n, c.dirName(), info.Scramble)
	}
	if cs.on && cs.idleRun >= callIdleFrames {
		c.endCall(n, now, "空線信号に戻った")
	}
	if cs.on && release {
		c.endCall(n, now, "送信権解放（同期バーストの BI = 0。§4.5.1.12）")
	}
}

func (c *Channel) startCall(n int, s *decoder.Slot, now float64) {
	cs := &c.calls[n]
	label := fmt.Sprintf("s%d", n)
	base := c.sink.RecOpen(n, label, recLagSec+math.Max(0, now-s.TSec))
	c.callSeq++
	*cs = callState{on: true, base: base, t0: now, seq: c.callSeq, lastBusy: now, busyRun: cs.busyRun, early: cs.early, pendCall: cs.pendCall,
		pendCtl: cs.pendCtl, pendSrc: cs.pendSrc, stats: modeStats{sbPrev: -1}, upFrame: cs.upFrame, upRun: cs.upRun,
		talker: cs.talker, selcall: cs.selcall, lastPICH: cs.lastPICH, dupWith: -1, lastTalker: -1}
	c.mu.Lock()
	c.recOn++
	c.car.Calls++
	id := c.ids[n]
	c.mu.Unlock()
	if base == "" {
		c.logf(n, "record", "warn", "ST#%d の通話を検知したが録音を開けなかった", n)
		return
	}
	c.sink.RecNote(n, fmt.Sprintf("STD-T79 %.4f MHz（%s・オフセット %+.1f kHz）ST#%d 開始 %s 同期ワード %s CC %d DDC %.0f Hz",
		c.cfg.MHz, c.dirName(), c.cfg.OffHz/1e3, n, c.sink.StreamTime().Format(time.RFC3339), s.SW.Name(), s.CC, c.rate))
	if id >= 0 {
		c.sink.RecNote(n, fmt.Sprintf("直近の同期バーストの呼出符号 %d", id))
	}
	if f, err := os.Create(rec.Path(base, rec.Calls)); err == nil {
		cs.calls = f
		fmt.Fprintf(f, "voice_start %s ST#%d %s\n", c.sink.StreamTime().Format(time.RFC3339), n, s.SW.Name())
	}
	if !c.std.cfg.NoTCH {
		c.sink.RecNote(n, fmt.Sprintf("TCH: %s + %s（1 枚 = そのスロットの 40 ms ごとの TCH 256 ビット = TCH 112 ++ TCH 144、32 バイト MSB 先頭、"+
			"デスクランブル済み。idx はスロットの無線フレーム番号 uint32 LE。FACCH が半分を使うスロットは書かない）",
			rec.Path(base, rec.TCH), rec.Path(base, rec.TCHIdx)))
		if f, err := os.Create(rec.Path(base, rec.TCH)); err == nil {
			cs.tchF, cs.tchW = f, bufio.NewWriter(f)
		}
		if f, err := os.Create(rec.Path(base, rec.TCHIdx)); err == nil {
			cs.idxF, cs.idxW = f, bufio.NewWriter(f)
		}
	}
	cs.tr = c.newTracker(n)
	if c.vs.chosen != nil {
		c.note(n, "voice_codec "+c.vs.chosen.Name+"（"+c.vs.why+"）")
	}
	for _, f := range cs.early {
		c.writeTCH(n, f)
		c.pushTracker(n, f)
	}
	cs.early = nil
	c.modeAtStart(n)
	if cs.pendCall != nil {
		src := cs.pendSrc
		if src == "" {
			src = "無線チャネル指定に相乗りした呼設定"
		}
		c.setCallInfoFrom(n, cs.pendCall, cs.pendCtl, src)
		cs.pendCall = nil
	}
	c.emitRec(n, true, strings.TrimSpace(fmt.Sprintf("%sST#%d 通話 %s", c.dirPrefix(), n, cs.peer)), 0)
	c.logf(n, "call", "info", "ST#%d 通話開始（%s）", n, s.SW.Name())
}

func (c *Channel) writeTCH(n int, f decoder.TCHFrame) {
	cs := &c.calls[n]
	if !f.Voice || len(f.Bits) != l1.TCHBits {
		return
	}
	cs.voice++
	if cs.tchW == nil {
		return
	}
	_, _ = cs.tchW.Write(l1.ToBytes(f.Bits))
	var b [4]byte
	binary.LittleEndian.PutUint32(b[:], uint32(f.Frame))
	_, _ = cs.idxW.Write(b[:])
	if !f.Known && cs.voice == 1 {
		c.note(n, "scramble 未確定のままオール 1 で外した（SACCH / FACCH の CRC が 256 通りのどれでも通らなかったか、まだ来ていない）")
	}
}

func (c *Channel) endCall(n int, now float64, why string) {
	cs := &c.calls[n]
	if !cs.on {
		return
	}
	sec := now - cs.t0
	c.noteEnded(n, now)
	c.modeInfer(n)
	audioSec := c.finishVoice(n)
	if cs.tchW != nil {
		_ = cs.tchW.Flush()
		_ = cs.tchF.Close()
		_ = cs.idxW.Flush()
		_ = cs.idxF.Close()
	}
	if cs.calls != nil {
		fmt.Fprintf(cs.calls, "voice_end %s 音声 %d フレーム（%.1f 秒） FACCH %d 理由 %s\n",
			c.sink.StreamTime().Format(time.RFC3339), cs.voice, float64(cs.voice)*0.04, cs.facch, why)
		_ = cs.calls.Close()
	}
	if cs.base != "" {
		c.sink.RecNote(n, fmt.Sprintf("終了 %s 理由 %s 音声 %d フレーム FACCH %d %.1f 秒", c.sink.StreamTime().Format(time.RFC3339), why, cs.voice, cs.facch, sec))
		c.sink.RecClose(n, recTailSec)
		reason := strings.TrimSpace(fmt.Sprintf("%sST#%d %s 通話 %s 音声 %d フレーム", c.dirPrefix(), n, cs.modeLabel(), cs.peer, cs.voice))
		if audioSec > 0 {
			reason += fmt.Sprintf("（%s で %.1f 秒）", c.vs.chosen.Name, audioSec)
		}
		c.emitRecAudio(n, false, reason, sec, audioSec > 0)
	}
	c.logf(n, "call", "info", "ST#%d 通話終了（%s・音声 %d フレーム・%.1f 秒）", n, why, cs.voice, sec)
	*cs = callState{talker: -1, selcall: -1, dupWith: -1, lastTalker: -1}
	c.mu.Lock()
	c.car.Slots[n].Mode, c.car.Slots[n].ModeNote = "", ""
	c.recOn--
	c.mu.Unlock()
}

func (c *Channel) note(n int, line string) {
	cs := &c.calls[n]
	if !cs.on || cs.base == "" {
		return
	}
	c.sink.RecNote(n, line)
	if cs.calls != nil {
		fmt.Fprintln(cs.calls, line)
	}
}

func (c *Channel) relName(base string) string {
	if c.std.cfg.LogDir == "" {
		return base
	}
	return rec.Rel(c.std.cfg.LogDir, base)
}

func (c *Channel) emitRec(n int, open bool, reason string, sec float64) {
	c.emitRecAudio(n, open, reason, sec, false)
}

func (c *Channel) emitRecAudio(n int, open bool, reason string, sec float64, audio bool) {
	cs := &c.calls[n]
	r := Rec{Base: cs.base, Name: c.relName(cs.base), Idx: c.cfg.ID, TSec: cs.t0, Open: open, Reason: reason, Sec: sec, Audio: audio}
	c.sink.Emit(Event{Type: EvRec, Rec: &r})
}

func (c *Channel) logf(n int, group, level, format string, a ...any) {
	text := fmt.Sprintf(format, a...)
	c.sink.Log("%.4f MHz: %s", c.cfg.MHz, text)
	l := LogLine{TSec: c.tNow(), Idx: c.cfg.ID, Group: group, Level: level, Text: text}
	c.sink.Emit(Event{Type: EvLog, Log: &l})
}

func (c *Channel) Resync() {
	now := c.tNow()
	for n := range c.calls {
		c.endCall(n, now, "取りこぼし")
	}
	c.newDecoder(now)
	c.proc = int64(math.Round((now - c.cfg.StartSec) * c.rate))
	c.mu.Lock()
	c.car.Overflows++
	c.mu.Unlock()
}

func (c *Channel) Retune() { c.Resync() }

func (c *Channel) Close(dropped bool) {
	now := c.tNow()
	for n := range c.calls {
		c.endCall(n, now, "監視を終えた")
	}
	c.closeVoice()
	c.mu.Lock()
	c.closed, c.car.Locked = true, false
	car := c.car
	car.Audio = c.mix.state()
	c.mu.Unlock()
	c.sink.Emit(Event{Type: EvT79, Data: &Detail{Carrier: car}})
	stn := c.station()
	c.sink.Emit(Event{Type: EvStation, Station: &stn})
	if c.std.hooks != nil && c.std.hooks.closed != nil {
		c.std.hooks.closed(c, dropped)
	}
}

func (c *Channel) Lock() (bool, float64, string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	score := 0.0
	if c.car.SlotsN > 0 {
		score = float64(c.car.SWFound) / float64(c.car.SlotsN)
	}
	ok := c.car.SWFound >= lockMinSW && c.car.SWFound*2 >= c.car.SlotsN
	kind, cc := c.kindLocked()
	return ok, score, fmt.Sprintf("T79 同期ワード %d/%d スロット %s CC %d", c.car.SWFound, c.car.SlotsN, kind, cc)
}

const nominalMinSW = 200

func (c *Channel) NominalMHz() (float64, bool) {
	if c.up != nil {
		return 0, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.car.SWFound < nominalMinSW || c.car.SWFound*2 < c.car.SlotsN {
		return 0, false
	}
	f := c.cfg.MHz*1e6 + c.car.CoarseHz + c.car.CFOHz
	return math.Round(f/RasterHz) * RasterHz / 1e6, true
}

func (c *Channel) LedgerMHz() (float64, bool) {
	if c.up == nil {
		return c.NominalMHz()
	}
	if c.unitOK.Load() == 0 {
		return 0, false
	}
	if h := c.std.hooks; h != nil && h.downPaired != nil && h.downPaired(c.cfg.MHz) {
		return 0, false
	}
	return c.cfg.MHz, true
}

func (c *Channel) Counters() rx.Counters {
	c.mu.Lock()
	defer c.mu.Unlock()
	return rx.Counters{Frames: c.car.SlotsN, Good: c.car.SWFound, ProcSec: c.nowT - c.cfg.StartSec}
}

func (c *Channel) Summary() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	k, cc := c.kindLocked()
	s := c.car
	return fmt.Sprintf("%.4f MHz: %s CC %d スロット %d（同期ワード %d）CAC %d/%d SACCH %d/%d FACCH %d 同期バースト %d/%d 通話 %d 本 レベル %.1f dB",
		c.cfg.MHz, k, cc, s.SlotsN, s.SWFound, s.CACOK, s.CACTotal, s.SACCHOK, s.SACCHN, s.FACCHOK, s.SBOK, s.SBTotal, s.Calls, s.LevelDB)
}

func (c *Channel) State() rx.State {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.recOn > 0 {
		return rx.StateActive
	}
	if k, _ := c.kindLocked(); k == "制御キャリア" {
		return rx.StateControl
	}
	return rx.StateIdle
}

func (c *Channel) Snapshot(ext map[string]any) { ext["t79"] = c.snapshot() }

func (c *Channel) snapshot() ChannelSnapshot {
	c.mu.Lock()
	defer c.mu.Unlock()
	car := c.car
	car.Audio = c.mix.state()
	return ChannelSnapshot{Carrier: car}
}

func (c *Channel) station() Station {
	c.mu.Lock()
	defer c.mu.Unlock()
	kind, cc := c.kindLocked()
	s := Station{Idx: c.cfg.ID, MHz: c.cfg.MHz, OffsetHz: c.cfg.OffHz, Watched: !c.closed,
		Synced: !c.closed && c.car.Locked, Recording: c.recOn > 0, LastTSec: c.nowT,
		Ext: map[string]any{"talking": c.recOn > 0}}
	s.Kind, s.CC, s.Org, s.Calls, s.Codec = kind, cc, c.org, c.car.Calls, c.car.Codec
	s.Audio = c.mix.state()
	s.Dir, s.PairMHz = c.dirName(), c.cfg.MHz+UpOffsetMHz
	if c.up != nil {
		s.PairMHz = c.cfg.MHz - UpOffsetMHz
	}
	if c.directOpt() != nil {
		s.PairMHz = 0
	}
	for n, sl := range c.car.Slots {
		s.Slots[n], s.Peers[n] = c.slotView[n], sl.Peer
		if c.calls[n].on {
			s.Modes[n] = sl.Mode
		}
		if c.calls[n].on && s.Slots[n] != "未同期" {
			s.Slots[n] = "通話:" + s.Slots[n]
		}
	}
	if c.car.SlotsN > 0 {
		s.SWPct = round2(100 * float64(c.car.SWFound) / float64(c.car.SlotsN))
	}
	if c.car.CACTotal > 0 {
		s.CACPct = round2(100 * float64(c.car.CACOK) / float64(c.car.CACTotal))
	}
	if c.car.SACCHN > 0 {
		s.SACCHPct = round2(100 * float64(c.car.SACCHOK) / float64(c.car.SACCHN))
	}
	s.CFOHz = round2(c.car.CoarseHz + c.car.CFOHz)
	s.PhaseErr = c.phaseAvg
	if kind != "" {
		s.Note = fmt.Sprintf("%s CC%d", strings.TrimSuffix(kind, "キャリア"), cc)
	}
	if !math.IsInf(c.car.LevelDB, 0) && !math.IsNaN(c.car.LevelDB) {
		v := c.car.LevelDB
		s.LevelDB = &v
	}
	if c.lastT > 0 {
		s.LastTSec = c.lastT
	}
	return s
}

func hexOctets(b []byte) string {
	return strings.ToUpper(strings.TrimSpace(fmt.Sprintf("% x", b)))
}

func levelDB(y []complex64) float64 {
	var p float64
	for _, v := range y {
		p += float64(real(v))*float64(real(v)) + float64(imag(v))*float64(imag(v))
	}
	return round2(10 * math.Log10(p/float64(len(y))+1e-20))
}

func round2(v float64) float64 { return math.Round(v*100) / 100 }
