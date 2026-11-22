package server

import (
	"encoding/binary"
	"fmt"
	"maps"
	"math"
	"os"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/symysak/arib/internal/chanbank"
	"github.com/symysak/arib/internal/data"
	"github.com/symysak/arib/internal/nxdn/decoder"
	"github.com/symysak/arib/internal/nxdn/dsp"
	"github.com/symysak/arib/internal/nxdn/l1"
	"github.com/symysak/arib/internal/rec"
	"github.com/symysak/arib/internal/rx"
	"github.com/symysak/arib/internal/voice"
	"github.com/symysak/arib/internal/voice/agc"
)

const (
	stationTick = 200 * time.Millisecond
	signalTick = 500 * time.Millisecond
	syncedSec = 1.0
	lockMinRICH = 4
	lockMinCRC = 2
	recTailSec = 0.5
	histBins = 36
	eyeEvery = 2
	maxPend = 25
)

type Channel struct {
	std  *Standard
	cfg  rx.ChannelConfig
	sink rx.Sink
	opt  ChannelOpt

	d     *decoder.Decoder
	keyer *l1.Keyer
	pend  []decoder.Frame
	link   Link
	dataSt *data.Station
	sub   int
	tx    *txState
	txSeq int
	vs    voiceState
	gain                          *agc.AGC
	lastPub                       time.Time
	lastSig                       time.Time
	hist                          [histBins]int
	sigN                          int
	sigDev, sigDC, sigEVM, sigFSW float64
	eyeN                          int
	lastL1                        *FrameRec
	l1No                          int64
	msgNo                         int64
	lastMsg                       map[string]*MsgRec
	tsv                           *os.File
	tsvDay                        string
	keyLog                        int

	nFrames, nGood atomic.Int64
	nCRC           atomic.Int64
	trusted map[int]bool
	lastInit int
	procSec  atomic.Uint64
	procIn   atomic.Uint64

	mu        sync.Mutex
	st        Station
	codecName string
	ledCodec  string
	watched   bool
	forget    bool
	lastT     float64
	level     *float64
}

type sigAcc struct {
	n, nLevel              int
	dev, dc, evm, fsw, lev float64
}

func (a *sigAcc) add(f decoder.Frame, level *float64) {
	a.n++
	a.dev += f.DevHz
	a.dc += f.DCHz
	a.evm += f.EVM
	a.fsw += float64(f.FSWErr)
	if level != nil {
		a.nLevel++
		a.lev += *level
	}
}

func (a sigAcc) line() string {
	if a.n == 0 {
		return ""
	}
	n := float64(a.n)
	lv := "-"
	if a.nLevel > 0 {
		lv = fmt.Sprintf("%.1f dBFS", a.lev/float64(a.nLevel))
	}
	return fmt.Sprintf("signal レベル %s・偏移 %.0f Hz・DC %.0f Hz・EVM %.3f・同期ワードの誤り %.2f ビット（%d フレームの平均）",
		lv, a.dev/n, a.dc/n, a.evm/n, a.fsw/n, a.n)
}

type txState struct {
	rec      TxRec
	base     string
	tch, idx *os.File
	tr       *voice.Tracker
	audio    *audioBuf
	endSeen  bool
	opened   bool
	callLog    []string
	voiceEnded bool
	sig sigAcc
	crcSeen bool
	strong int
	pending []string
	lastT   float64
}

func newChannel(s *Standard, c rx.ChannelConfig, sink rx.Sink, opt ChannelOpt) *Channel {
	ch := &Channel{std: s, cfg: c, sink: sink, opt: opt, watched: true, lastMsg: map[string]*MsgRec{}, trusted: map[int]bool{}, lastInit: -1}
	bank, _ := c.Front.(*chanbank.Bank)
	ch.d = decoder.New(decoder.Config{SampleRate: c.FsIn, OffsetHz: c.OffHz, StartSec: c.StartSec, Bank: bank})
	ch.keyer = s.keyer(opt.Init)
	if s.spec.DataChannel != "" {
		ch.dataSt = data.NewStation(s.spec.DataChannel, s.cfg.Data)
	}
	ch.setProc(sink.WallSec(c.StartSec))
	ch.procIn.Store(math.Float64bits(c.StartSec))
	ch.st = Station{Idx: c.ID, MHz: c.MHz, OffsetHz: c.OffHz, Init: -1, U: -1, Maker: -1, Ext: map[string]any{}}
	if init, ok := ch.keyer.Init(); ok {
		ch.st.Init, ch.st.InitWhy = init, ch.keyer.Why()
	}
	ch.initVoice()
	ch.link = s.spec.NewLink(ch)
	return ch
}

func (c *Channel) setProc(sec float64) { c.procSec.Store(math.Float64bits(sec)) }

func (c *Channel) tNow() float64 { return math.Float64frombits(c.procSec.Load()) }

func (c *Channel) ID() int { return c.cfg.ID }

func (c *Channel) MHz() float64 { return c.cfg.MHz }


func (c *Channel) Feed(wide []complex64) {
	c.d.WantEye = int(c.std.selected.Load()) == c.cfg.ID
	c.afterFeed(c.d.Feed(wide))
}

func (c *Channel) FeedFront(_ []complex64, prep any) {
	c.d.WantEye = int(c.std.selected.Load()) == c.cfg.ID
	frames, ok := c.d.FeedFront(prep)
	if !ok {
		return
	}
	c.afterFeed(frames)
}

func (c *Channel) afterFeed(frames []decoder.Frame) {
	c.sink.IQ(c.d.IQ, dsp.Fs)
	c.setProc(c.sink.WallSec(c.d.ProcessedSec()))
	c.procIn.Store(math.Float64bits(c.d.ProcessedSec()))
	for i := range frames {
		frames[i].TimeSec = c.sink.WallSec(frames[i].TimeSec)
	}
	if lv := c.d.LevelDB(); !math.IsInf(lv, 0) {
		c.mu.Lock()
		c.level = &lv
		c.mu.Unlock()
	}
	for _, f := range frames {
		c.onFrame(f)
	}
	if c.tx != nil && !c.d.Locked() {
		c.endTx(c.tNow(), "同期が外れた")
	}
	c.publish(false)
}

func (c *Channel) Resync() {
	now := c.tNow()
	c.endTx(now, "取りこぼし")
	start := c.d.ProcessedSec()
	if p, ok := c.sink.(rx.Positioner); ok {
		start = math.Max(start, p.InputSec())
	}
	c.d.Reset(start)
	c.setProc(c.sink.WallSec(start))
	c.pend = nil
	c.link.Reset()
	c.mu.Lock()
	c.st.Overflow++
	n := c.st.Overflow
	c.mu.Unlock()
	if n == 1 || n%100 == 0 {
		c.logf("system", "warn", "復号が追いつかず入力を捨てたので仕切り直した（累計 %d 回）", n)
	}
}

func (c *Channel) Lock() (bool, float64, string) {
	n, g, k := c.nFrames.Load(), c.nGood.Load(), c.nCRC.Load()
	score := 0.0
	if n > 0 {
		score = float64(g) / float64(n)
	}
	c.mu.Lock()
	init := c.st.Init
	c.mu.Unlock()
	label := fmt.Sprintf("RICH OK %d/%d フレーム・CRC OK %d", g, n, k)
	if init >= 0 {
		label += fmt.Sprintf("・初期値 0x%03X", init)
	}
	return g >= lockMinRICH && k >= lockMinCRC, score, label
}

func (c *Channel) Counters() rx.Counters {
	return rx.Counters{Frames: int(c.nFrames.Load()), Good: int(c.nGood.Load()), ProcSec: math.Float64frombits(c.procIn.Load()) - c.cfg.StartSec}
}

func (c *Channel) Summary() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	s := c.st
	init := "未定"
	if s.Init >= 0 {
		init = fmt.Sprintf("0x%03X", s.Init)
	}
	return fmt.Sprintf("%.4f MHz: フレーム %d（同期ワード %d・RICH %d・CRC %d/%d）初期値 %s 送信 %d 本",
		c.cfg.MHz, s.Frames, s.FSWOK, s.RICHOK, s.CRCOK, s.CRCAll, init, s.Txs)
}

func (c *Channel) State() rx.State {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.st.Recording {
		return rx.StateActive
	}
	return rx.StateIdle
}

func (c *Channel) Snapshot(ext map[string]any) {
	c.mu.Lock()
	defer c.mu.Unlock()
	ext[c.std.spec.Key] = c.st
}

func (c *Channel) Close(dropped bool) {
	if !dropped {
		for _, f := range c.d.Flush() {
			f.TimeSec = c.sink.WallSec(f.TimeSec)
			c.onFrame(f)
		}
	}
	c.endTx(c.tNow(), map[bool]string{true: "監視終了", false: "入力の終端"}[dropped])
	c.closeVoice()
	if c.tsv != nil {
		c.tsv.Close()
		c.tsv = nil
	}
	c.mu.Lock()
	if dropped {
		c.watched = false
	}
	c.mu.Unlock()
	c.publish(true)
	if h := c.std.hooks; h != nil && h.closed != nil {
		h.closed(c, dropped)
	}
}


func (c *Channel) onFrame(f decoder.Frame) {
	c.nFrames.Add(1)
	c.mu.Lock()
	c.st.Frames++
	if f.FSWErr <= 2 {
		c.st.FSWOK++
	}
	c.lastT = f.TimeSec
	c.mu.Unlock()
	if !f.Inertia {
		c.sigDev += f.DevHz
		c.sigDC += f.DCHz
		c.sigEVM += f.EVM
		c.sigFSW += float64(f.FSWErr)
		c.sigN++
		for _, v := range f.Body {
			if b := int((float64(v) + 4.5) / 9 * histBins); b >= 0 && b < histBins {
				c.hist[b]++
			}
		}
	}
	if f.First && c.tx == nil {
		c.startTx(f)
	} else if c.tx == nil && !f.Inertia {
		c.startTx(f)
	}
	if c.tx != nil {
		c.tx.lastT = f.TimeSec
		c.tx.rec.Frames++
		if !f.Inertia {
			c.tx.sig.add(f, c.level)
		}
	}
	if f.Inertia {
		c.pushVoiceGap(f)
		return
	}
	c.eye(f)
	before, _ := c.keyer.Init()
	kds := c.keyer.Push(f.Body[:])
	c.pend = append(c.pend, f)
	if len(c.pend) > maxPend {
		c.pend = c.pend[len(c.pend)-maxPend:]
	}
	if len(kds) == 0 {
		return
	}
	if after, _ := c.keyer.Init(); after != before {
		c.onKey(after)
	}
	fs := c.pend[len(c.pend)-len(kds):]
	c.pend = nil
	if c.tx != nil && !c.tx.crcSeen {
		n := c.tx.strong
		for _, kd := range kds {
			p := kd.Parsed
			if !p.Layout.Valid {
				continue
			}
			n += l1.Strong(p)
			if n >= proofStrong || c.crcProof(p) {
				c.tx.crcSeen = true
				break
			}
		}
	}
	for i, kd := range kds {
		c.onKeyed(fs[i], kd)
	}
}

func (c *Channel) onKey(init int) {
	if init < 0 {
		c.mu.Lock()
		c.st.Init, c.st.InitWhy = -1, ""
		c.mu.Unlock()
		c.logf("station", "warn", "ホワイトニングの初期値で RICH が読めなくなったので当て直す")
		return
	}
	why := c.keyer.Why()
	c.mu.Lock()
	c.st.Init, c.st.InitWhy = init, why
	c.mu.Unlock()
	if c.lastInit >= 0 && c.lastInit != init {
		c.nGood.Store(0)
		c.nCRC.Store(0)
		if c.tx != nil {
			c.tx.strong = 0
		}
	}
	c.lastInit = init
	c.shareKey()
	c.logf("station", "info", "ホワイトニングの初期値 0x%03X（%s）", init, why)
}

func (c *Channel) onKeyed(f decoder.Frame, kd l1.Keyed) {
	p := kd.Parsed
	valid := p.Layout.Valid
	if valid {
		c.nGood.Add(1)
	}
	crcOK, crcAll := 0, 0
	count := func(ok bool) {
		crcAll++
		if ok {
			crcOK++
		}
	}
	if p.SACCH != nil {
		count(p.SACCH.CRCOK)
	}
	for _, h := range p.Half {
		if h.Coded != nil {
			count(h.Coded.CRCOK)
		}
	}
	if p.Data != nil {
		count(p.Data.CRCOK)
	}
	rd := c.link.Frame(f, kd)
	c.mu.Lock()
	if valid {
		c.st.RICHOK++
		c.st.Mode = rd.Mode
	}
	c.st.CRCOK += crcOK
	c.st.CRCAll += crcAll
	c.mu.Unlock()
	if n := l1.Strong(p); valid && c.tx != nil && n > 0 {
		c.tx.strong += n
		if c.tx.strong >= proofStrong {
			c.tx.crcSeen = true
			if init, ok := c.keyer.Init(); ok && !c.trustedInit(init) {
				c.trusted[init] = true
				c.logf("station", "info", "ホワイトニングの初期値 0x%03X を信じる（1 回の送信で長い CRC か AMBE+2 の検査が通った欄が %d）", init, c.tx.strong)
			}
		}
	}
	if valid && c.crcProof(p) {
		c.nCRC.Add(1)
		if c.tx != nil {
			c.tx.crcSeen = true
		}
		c.shareKey()
	}
	if c.tx != nil && valid && c.tx.crcSeen {
		wasOpen := c.tx.opened
		c.openTx()
		t := &c.tx.rec
		switch {
		case rd.TxKind != "":
			if t.Kind != rd.TxKind {
				t.Kind = rd.TxKind
				if wasOpen {
					c.emitTx()
				}
			}
		case t.Kind == "" || t.Kind == "不明":
			t.Kind = rd.Kind
		}
		t.D = rd.D
	}

	c.sub = rd.Sub
	if rd.User >= 0 {
		c.setU(rd.User)
	}
	if len(rd.Data) > 0 {
		if c.tx != nil {
			c.tx.rec.Data += len(rd.Data)
		}
		c.readData(rd.Data)
	}
	msgs := rd.Msgs
	newMsg := false
	for i := range msgs {
		msgs[i].Idx = c.cfg.ID
		if c.emitMsg(&msgs[i]) {
			newMsg = true
		}
	}
	if rd.After != nil {
		rd.After()
	}
	pr := c.link.Profile()
	c.mu.Lock()
	c.st.Profile = pr
	c.mu.Unlock()
	c.l1Row(f, kd, rd, newMsg)
	c.voiceFrame(f, p)

	if c.tx != nil {
		for _, m := range msgs {
			if m.Msg.End {
				c.tx.endSeen = true
			}
			if m.Msg.Maker >= 0 {
				c.tx.rec.Maker = m.Msg.Maker
				c.mu.Lock()
				c.st.Maker = m.Msg.Maker
				c.mu.Unlock()
			}
			if line := m.Msg.Ch + " " + m.Msg.Summary; len(c.tx.rec.Msgs) < 16 && !slices.Contains(c.tx.rec.Msgs, line) {
				c.tx.rec.Msgs = append(c.tx.rec.Msgs, line)
			}
		}
		if rd.EndAfterVoice && c.tx.rec.Voice > 0 {
			c.tx.endSeen = true
		}
	}
}

func (c *Channel) shareKey() {
	if init, ok := c.keyer.Init(); ok {
		if lock, _, _ := c.Lock(); lock {
			c.std.keys.add(init)
		}
	}
}

func (c *Channel) crcProof(p l1.Parsed) bool {
	init, ok := c.keyer.Init()
	if !ok || !c.trustedInit(init) {
		return false
	}
	if longCRC(p) {
		return true
	}
	if p.SACCH == nil || !p.SACCH.CRCOK {
		return false
	}
	for _, h := range p.Half {
		if h.Coded != nil {
			return false
		}
	}
	if f := c.std.spec.SACCHProof; f != nil && !f(p) {
		return false
	}
	return true
}

const proofStrong = 3

func (c *Channel) trustedInit(init int) bool {
	return c.opt.Init >= 0 || c.std.defaultInit(init) || c.trusted[init]
}

func longCRC(p l1.Parsed) bool {
	for _, h := range p.Half {
		if h.Coded != nil && h.Coded.CRCOK {
			return true
		}
	}
	return p.Data != nil && p.Data.CRCOK
}

func (c *Channel) setU(u int) {
	if c.tx != nil {
		c.tx.rec.U = u
	}
	c.mu.Lock()
	c.st.U = u
	c.mu.Unlock()
}


func (c *Channel) startTx(f decoder.Frame) {
	c.tx = &txState{rec: TxRec{Idx: c.cfg.ID, TSec: f.TimeSec, Open: true, Kind: "不明", U: -1, Maker: -1, D: -1}, lastT: f.TimeSec}
	c.lastMsg = map[string]*MsgRec{}
	c.link.TxStart()
}

func (c *Channel) openTx() {
	t := c.tx
	if t == nil || t.opened {
		return
	}
	t.opened = true
	c.txSeq++
	t.rec.ID = c.txSeq
	lag := c.tNow() - t.rec.TSec + decoder.LagSec()
	if lag < 0 {
		lag = 0
	}
	t.base = c.sink.RecOpen(0, fmt.Sprintf("tx%d", t.rec.ID), lag)
	t.rec.Base = t.base
	if t.base != "" {
		c.note(fmt.Sprintf("tx_start %.3f %s %.5f MHz", t.rec.TSec, c.wallAt(t.rec.TSec), c.cfg.MHz))
		if init, ok := c.keyer.Init(); ok {
			c.note(fmt.Sprintf("whitening 0x%03X %s", init, c.keyer.Why()))
		}
		for _, line := range t.pending {
			c.note(line)
		}
		c.emitRec(true, "送信", 0, false)
	}
	c.mu.Lock()
	c.st.Recording = t.base != ""
	c.st.Txs++
	c.mu.Unlock()
	c.sink.MarkActive(3 * time.Second)
	c.emitTx()
}

func (c *Channel) endTx(now float64, why string) {
	t := c.tx
	if t == nil {
		return
	}
	if !t.opened {
		c.tx = nil
		c.link.TxEnd(now, false)
		c.mu.Lock()
		was := c.st.Talking
		c.st.Talking = false
		c.mu.Unlock()
		if was {
			c.publish(true)
		}
		return
	}
	if t.endSeen && why == "同期が外れた" {
		why = "終わりのフレーム"
	}
	c.link.TxEnd(t.lastT, true)
	audioSec := c.finishVoice()
	if t.rec.Voice > 0 && !t.voiceEnded {
		c.callEvent(t.lastT, "voice_end", fmt.Sprintf("音声 %d フレーム（%.1f 秒） 符号器なし", t.rec.Voice, float64(t.rec.Voice)*voice.FrameSec))
	}
	t.rec.Open = false
	t.rec.EndSec = math.Max(t.lastT+l1.FrameSec, t.rec.TSec)
	t.rec.End = why
	t.rec.Audio = audioSec > 0
	sec := t.rec.EndSec - t.rec.TSec
	if l := t.sig.line(); l != "" {
		c.note(l)
	}
	c.note(txInfo(t.rec))
	c.note(fmt.Sprintf("tx_end %.3f %s %s（%d フレーム・%.2f 秒）", t.rec.EndSec, c.wallAt(t.rec.EndSec), why, t.rec.Frames, sec))
	if t.base != "" && len(t.callLog) > 0 {
		lines := append([]string{fmt.Sprintf("%.5f MHz 録音開始（送信 tx%d）", c.cfg.MHz, t.rec.ID)}, t.callLog...)
		if err := os.WriteFile(rec.Path(t.base, rec.Calls), []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
			c.logf("record", "warn", "通話ログを書けない: %v", err)
		}
	}
	if t.tch != nil {
		t.tch.Close()
		t.idx.Close()
	}
	if t.base != "" {
		c.sink.RecClose(0, recTailSec)
		c.emitRec(false, "送信（"+t.rec.Kind+"）", sec, audioSec > 0)
	}
	c.emitTx()
	c.logf("call", "info", "送信 #%d 終わり（%s・%s・%d フレーム・%.1f 秒%s）", t.rec.ID, t.rec.Kind, why, t.rec.Frames, sec,
		map[bool]string{true: fmt.Sprintf("・音声 %.1f 秒", audioSec), false: ""}[audioSec > 0])
	c.tx = nil
	c.mu.Lock()
	c.st.Recording, c.st.Talking = false, false
	c.mu.Unlock()
	c.publish(true)
}

func (c *Channel) wallAt(sec float64) string {
	return c.std.cfg.Origin.Add(time.Duration(sec * float64(time.Second))).Format("2006-01-02T15:04:05.000Z07:00")
}

func txInfo(r TxRec) string {
	num := func(v int) string {
		if v < 0 {
			return "-"
		}
		return strconv.Itoa(v)
	}
	str := func(s string) string {
		if s == "" {
			return "-"
		}
		return s
	}
	line := fmt.Sprintf("tx_info 種類 %s・U %s・D %s・製造者 %s・呼 %s・名前 %s・音声 %d/%d・データ %d",
		r.Kind, num(r.U), num(r.D), num(r.Maker), str(r.Call), str(r.Name), r.VoiceOK, r.Voice, r.Data)
	keys := slices.Sorted(maps.Keys(r.Ext))
	for _, k := range keys {
		line += fmt.Sprintf("・%s %v", k, r.Ext[k])
	}
	return line
}

func (c *Channel) note(line string) {
	t := c.tx
	if t == nil {
		return
	}
	if !t.opened {
		t.pending = append(t.pending, line)
		return
	}
	if t.base == "" {
		return
	}
	c.sink.RecNote(0, line)
}

func (c *Channel) callEvent(sec float64, kind, text string) {
	if c.tx == nil {
		return
	}
	c.tx.callLog = append(c.tx.callLog, fmt.Sprintf("%9.3fs %-11s %s", sec, kind, text))
}

func (c *Channel) emitRec(open bool, reason string, sec float64, audio bool) {
	t := c.tx
	r := Rec{Base: t.base, Name: c.relName(t.base), Idx: c.cfg.ID, TSec: t.rec.TSec, Open: open, Reason: reason, Sec: sec, Audio: audio}
	c.sink.Emit(Event{Type: EvRec, Rec: &r})
}

func (c *Channel) emitTx() {
	r := c.tx.rec
	r.Msgs = append([]string{}, r.Msgs...)
	c.sink.Emit(Event{Type: EvTx, Tx: &r})
}

func (c *Channel) relName(base string) string {
	if c.std.cfg.LogDir == "" {
		return base
	}
	return rec.Rel(c.std.cfg.LogDir, base)
}

func (c *Channel) writeTCH(no int64, p l1.Parsed) {
	t := c.tx
	if c.std.cfg.NoTCH || t == nil || t.base == "" {
		return
	}
	if t.tch == nil {
		if p.Half[0].Kind != l1.HalfTCH && p.Half[1].Kind != l1.HalfTCH {
			return
		}
		var err1, err2 error
		t.tch, err1 = os.Create(rec.Path(t.base, rec.TCH))
		t.idx, err2 = os.Create(rec.Path(t.base, rec.TCHIdx))
		if err1 != nil || err2 != nil {
			if t.tch != nil {
				t.tch.Close()
			}
			if t.idx != nil {
				t.idx.Close()
			}
			t.tch, t.idx = nil, nil
			c.std.cfg.NoTCH = true
			return
		}
	}
	buf := make([]byte, 36)
	for h := 0; h < 2; h++ {
		if p.Half[h].Kind == l1.HalfTCH {
			for i, b := range p.Half[h].TCH {
				buf[18*h+i/8] |= (b & 1) << (7 - i%8)
			}
		}
	}
	t.tch.Write(buf)
	var ib [4]byte
	binary.LittleEndian.PutUint32(ib[:], uint32(no))
	t.idx.Write(ib[:])
}


func (c *Channel) emitMsg(m *MsgRec) bool {
	if prev := c.lastMsg[m.Msg.Ch]; prev != nil && prev.Key == m.Key {
		prev.Repeat++
		prev.TSec = m.TSec
		r := *prev
		c.sink.Emit(Event{Type: EvMsg, Msg: &r})
		return false
	}
	c.msgNo++
	m.No = int64(c.cfg.ID)<<40 | c.msgNo
	cp := *m
	c.lastMsg[m.Msg.Ch] = &cp
	c.sink.Emit(Event{Type: EvMsg, Msg: m})
	off := 0.0
	if c.tx != nil {
		off = m.TSec - c.tx.rec.TSec
	}
	c.note(fmt.Sprintf("msg %s %s | +%.3f s | %X", m.Msg.Ch, m.Msg.Summary, off, m.Msg.Raw))
	return true
}


func (c *Channel) readData(frames []data.Frame) {
	if c.dataSt == nil {
		return
	}
	for _, fr := range frames {
		for _, ev := range c.dataSt.Read(fr) {
			c.emitData(&DataEvent{Idx: c.cfg.ID, MHz: c.cfg.MHz, Event: ev})
		}
	}
}

func (c *Channel) emitData(de *DataEvent) {
	c.sink.Emit(Event{Type: string(de.Kind), Data: de})
	c.logf("data", "info", "%s", de.Label)
	c.note("data " + de.Label)
	c.writeDataTSV(de)
}

func (c *Channel) writeDataTSV(de *DataEvent) {
	if c.std.cfg.LogDir == "" {
		return
	}
	now := time.Now()
	day := now.Format("2006-01-02")
	if c.tsv == nil || c.tsvDay != day {
		if c.tsv != nil {
			c.tsv.Close()
			c.tsv = nil
		}
		path := rec.DataLog(rec.SystemDir(c.std.cfg.LogDir, c.std.spec.System), now, c.cfg.MHz)
		if err := os.MkdirAll(dirOf(path), 0o755); err != nil {
			return
		}
		fh, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
		if err != nil {
			return
		}
		if st, err := fh.Stat(); err == nil && st.Size() == 0 {
			fmt.Fprintln(fh, "# 時刻\t種類\t書式\t送り元\t要約\t緯度\t経度\t本体")
		}
		c.tsv, c.tsvDay = fh, day
	}
	lat, lon := "", ""
	if de.Pos != nil {
		lat, lon = fmt.Sprintf("%.6f", de.Pos.Lat), fmt.Sprintf("%.6f", de.Pos.Lon)
	}
	fmt.Fprintf(c.tsv, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%X\n", now.Format("15:04:05.000"), de.Kind, de.Format, de.From,
		strings.ReplaceAll(de.Label, "\t", " "), lat, lon, de.Raw)
}

func dirOf(p string) string {
	if i := strings.LastIndexAny(p, `/\`); i >= 0 {
		return p[:i]
	}
	return "."
}

// ---- 画面 ----

func (c *Channel) logf(group, level, format string, a ...any) {
	text := fmt.Sprintf(format, a...)
	c.sink.Log("%.4f MHz: %s", c.cfg.MHz, text)
	l := LogLine{TSec: c.tNow(), Idx: c.cfg.ID, Group: group, Level: level, Text: text}
	c.sink.Emit(Event{Type: EvLog, Log: &l})
}

// station は局一覧の 1 行の写し。
func (c *Channel) station() Station {
	c.mu.Lock()
	defer c.mu.Unlock()
	s := c.st
	s.Watched = c.watched
	s.Synced = c.st.Frames > 0 && c.tNow()-c.lastT <= syncedSec
	s.LevelDB = c.level
	s.LastTSec = math.Max(c.lastT, c.tNow())
	s.Codec = c.codecName
	s.Ext = map[string]any{"talking": s.Talking}
	for k, v := range c.st.Ext {
		s.Ext[k] = v
	}
	if c.std.spec.Note != nil {
		s.Note = c.std.spec.Note(s)
	}
	return s
}

// publish は局の状態と信号の指標を画面へ流す（force でなければ間隔を置く）。
func (c *Channel) publish(force bool) {
	now := time.Now()
	if force || now.Sub(c.lastPub) >= stationTick {
		c.lastPub = now
		st := c.station()
		c.sink.Emit(Event{Type: EvStation, Station: &st})
	}
	if c.sigN > 0 && (force || now.Sub(c.lastSig) >= signalTick) {
		c.lastSig = now
		n := float64(c.sigN)
		sg := Signal{TSec: c.tNow(), DevHz: c.sigDev / n, DCHz: c.sigDC / n, EVM: c.sigEVM / n, FSWErr: c.sigFSW / n,
			Hist: append([]int(nil), c.hist[:]...)}
		c.mu.Lock()
		if c.level != nil {
			sg.LevelDB = *c.level
		}
		c.st.DevHz, c.st.DCHz, c.st.EVM = sg.DevHz, sg.DCHz, sg.EVM
		c.mu.Unlock()
		c.sigN, c.sigDev, c.sigDC, c.sigEVM, c.sigFSW = 0, 0, 0, 0, 0
		c.hist = [histBins]int{}
		c.sink.Emit(Event{Type: EvSignal, Signal: &sg})
	}
}

// eye は選択局の目のパターン（復調器が選択局のフレームにだけ付ける）を流す。
func (c *Channel) eye(f decoder.Frame) {
	if len(f.Eye) == 0 {
		return
	}
	c.eyeN++
	if c.eyeN%eyeEvery != 0 {
		return
	}
	c.sink.Emit(Event{Type: EvEye, Eye: &Eye{TSec: f.TimeSec, Traces: f.Eye}})
}

// ---- Host（規格の Link が局へ働きかける口） ----

// Logf は画面と端末のログ。
func (c *Channel) Logf(group, level, format string, a ...any) { c.logf(group, level, format, a...) }

// Note は送信の記録（サイドカー .txt）へ 1 行。
func (c *Channel) Note(line string) { c.note(line) }

// EmitData はデータの出来事を流す。
func (c *Channel) EmitData(ev data.Event) {
	c.emitData(&DataEvent{Idx: c.cfg.ID, MHz: c.cfg.MHz, Event: ev})
}

// Tx は送信中なら送信の行を書き換える（edit が true を返したら画面へ流し直す。まだ確かな送信と決まっていない = 録音を開いていない間は
// 書き換えるだけで流さない。開くときに流れる）。
func (c *Channel) Tx(edit func(*TxRec) bool) {
	if c.tx != nil && edit(&c.tx.rec) && c.tx.opened {
		c.emitTx()
	}
}

// EditStation は局の状態を書き換える。
func (c *Channel) EditStation(edit func(*Station), publish bool) {
	c.mu.Lock()
	if c.st.Ext == nil {
		c.st.Ext = map[string]any{}
	}
	edit(&c.st)
	c.mu.Unlock()
	if publish {
		c.publish(true)
	}
}

// Station は局の状態の写し（規格の Link が読む）。
func (c *Channel) Station() Station {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.st
}

// Init は今のホワイトニングの初期値。
func (c *Channel) Init() (int, bool) { return c.keyer.Init() }

// Codec は決まった音声の符号器の名前（無ければ ""）。
func (c *Channel) Codec() string {
	if c.vs.chosen == nil {
		return ""
	}
	return c.vs.chosen.Name
}

// VoiceHint は局で覚えた符号器の設定（"<符号器>:<値>"）。
func (c *Channel) VoiceHint() string { return c.vs.hint }

// DataFormat は決まったデータの書式（データの口が無ければ ""）。
func (c *Channel) DataFormat() string {
	if c.dataSt == nil {
		return ""
	}
	return c.dataSt.Format()
}
