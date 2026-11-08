package server

import (
	"encoding/binary"
	"fmt"
	"math"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/symysak/arib/internal/data"
	"github.com/symysak/arib/internal/rec"
	"github.com/symysak/arib/internal/rx"
	"github.com/symysak/arib/internal/std-t102/decoder"
	"github.com/symysak/arib/internal/std-t102/dsp"
	"github.com/symysak/arib/internal/std-t102/l1"
	"github.com/symysak/arib/internal/std-t102/l3"
	"github.com/symysak/arib/internal/voice"
	"github.com/symysak/arib/internal/voice/agc"
)

const (
	stationTick = 200 * time.Millisecond
	signalTick = 500 * time.Millisecond
	syncedSec = 1.0
	lockMinRICH = 4
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

	d      *decoder.Decoder
	keyer  *l1.Keyer
	pend   []decoder.Frame
	sacch  l3.SACCH
	dataSt *data.Station
	b54 *l3.B54Asm
	sfPos int
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
	procSec        atomic.Uint64

	mu        sync.Mutex
	st        Station
	codecName string
	ledCodec  string
	watched   bool
	forget    bool
	lastT     float64
	level     *float64
}

type txState struct {
	rec      TxRec
	base     string
	calls    *os.File
	tch, idx *os.File
	tr       *voice.Tracker
	audio    *audioBuf
	b54Call *l3.B54
	endSeen bool
	opened  bool
	lastT   float64
}

func newChannel(s *Standard, c rx.ChannelConfig, sink rx.Sink, opt ChannelOpt) *Channel {
	ch := &Channel{std: s, cfg: c, sink: sink, opt: opt, watched: true, lastMsg: map[string]*MsgRec{}}
	ch.d = decoder.New(decoder.Config{SampleRate: c.FsIn, OffsetHz: c.OffHz, StartSec: c.StartSec})
	hints := append(s.keys.list(), DefaultInits...)
	ch.keyer = l1.NewKeyer(opt.Init, hints...)
	ch.dataSt = data.NewStation(l1.DataChannel, s.cfg.Data)
	ch.b54 = l3.NewB54Asm(s.cfg.B54Keys)
	ch.setProc(c.StartSec)
	ch.st = Station{Idx: c.ID, MHz: c.MHz, OffsetHz: c.OffHz, Init: -1, U: -1, Maker: -1, Ext: map[string]any{}}
	if opt.Init >= 0 {
		ch.st.Init, ch.st.InitWhy = opt.Init, "指定"
	}
	ch.initVoice()
	return ch
}

func (c *Channel) setProc(sec float64) { c.procSec.Store(math.Float64bits(sec)) }

func (c *Channel) tNow() float64 { return math.Float64frombits(c.procSec.Load()) }

func (c *Channel) ID() int { return c.cfg.ID }

func (c *Channel) MHz() float64 { return c.cfg.MHz }


func (c *Channel) Feed(wide []complex64) {
	c.d.WantEye = int(c.std.selected.Load()) == c.cfg.ID
	frames := c.d.Feed(wide)
	c.sink.IQ(c.d.IQ, dsp.Fs)
	c.setProc(c.d.ProcessedSec())
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
	c.d.Reset(now)
	c.pend = nil
	c.sacch.Reset()
	c.mu.Lock()
	c.st.Overflow++
	n := c.st.Overflow
	c.mu.Unlock()
	if n == 1 || n%100 == 0 {
		c.logf("system", "warn", "復号が追いつかず入力を捨てたので仕切り直した（累計 %d 回）", n)
	}
}

func (c *Channel) Retune() {
	now := c.tNow()
	c.endTx(now, "同調し直し")
	c.d.Reset(now)
	c.pend = nil
	c.sacch.Reset()
	if c.opt.Init < 0 {
		c.keyer = l1.NewKeyer(-1, append(c.std.keys.list(), DefaultInits...)...)
		c.mu.Lock()
		c.st.Init, c.st.InitWhy = -1, ""
		c.mu.Unlock()
	}
}

func (c *Channel) Lock() (bool, float64, string) {
	n, g := c.nFrames.Load(), c.nGood.Load()
	score := 0.0
	if n > 0 {
		score = float64(g) / float64(n)
	}
	c.mu.Lock()
	init := c.st.Init
	c.mu.Unlock()
	label := fmt.Sprintf("RICH OK %d/%d フレーム", g, n)
	if init >= 0 {
		label += fmt.Sprintf("・初期値 0x%03X", init)
	}
	return g >= lockMinRICH, score, label
}

func (c *Channel) Counters() rx.Counters {
	return rx.Counters{Frames: int(c.nFrames.Load()), Good: int(c.nGood.Load()), ProcSec: c.tNow() - c.cfg.StartSec}
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
	ext["t102"] = c.st
}

func (c *Channel) Close(dropped bool) {
	if !dropped {
		for _, f := range c.d.Flush() {
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
	c.std.keys.add(init)
	c.logf("station", "info", "ホワイトニングの初期値 0x%03X（%s）", init, why)
	c.note(fmt.Sprintf("whitening 0x%03X %s", init, why))
}

func (c *Channel) onKeyed(f decoder.Frame, kd l1.Keyed) {
	p := kd.Parsed
	r := p.RICH
	valid := r.Valid()
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
		if h.FACCH1 != nil {
			count(h.FACCH1.CRCOK)
		}
	}
	if p.Data != nil {
		count(p.Data.CRCOK)
	}
	c.mu.Lock()
	if valid {
		c.st.RICHOK++
		c.st.Mode = r.String()
	}
	c.st.CRCOK += crcOK
	c.st.CRCAll += crcAll
	c.mu.Unlock()
	if c.tx != nil && valid {
		c.openTx()
		t := &c.tx.rec
		if t.Kind == "" || t.Kind == "不明" {
			t.Kind = r.Format().String()
		}
		t.D = int(r.D)
	}

	var msgs []MsgRec
	switch {
	case !valid || !r.Superframe():
		c.sfPos = 0
	case p.SACCH != nil && p.SACCH.CRCOK:
		c.sfPos = 3 - int(p.SACCH.S&3)
	default:
		c.sfPos = (c.sfPos + 1) & 3
	}
	if s := p.SACCH; s != nil && valid {
		c.setU(int(s.U))
		if s.CRCOK {
			if b, ok := c.sacch.Push(r.Superframe(), s.S, s.U, s.Data[:]); ok {
				if !r.Superframe() {
					b = b[:2]
				}
				msgs = append(msgs, c.msg(f.TimeSec, l3.Parse("SACCH", b), int(s.U)))
			}
		} else {
			c.sacch.Reset()
		}
	}
	for i, h := range p.Half {
		if h.FACCH1 != nil && h.FACCH1.CRCOK {
			b := l1.FACCH1Bytes(h.FACCH1)
			m := c.msg(f.TimeSec, l3.Parse("FACCH1", b[:]), -1)
			if i == 0 || len(msgs) == 0 || msgs[len(msgs)-1].key != m.key {
				msgs = append(msgs, m)
			}
		}
	}
	if d := p.Data; d != nil {
		c.setU(int(d.U))
		if d.CRCOK {
			if c.tx != nil {
				c.tx.rec.Data++
			}
			switch m := l3.Parse(d.Kind.String(), d.Body[:]); {
			case d.Kind == l1.DataFACCH2:
				msgs = append(msgs, c.msg(f.TimeSec, m, int(d.U)))
			case m.B54 != nil:
				msgs = append(msgs, c.msg(f.TimeSec, m, int(d.U)))
			}
			c.readData(f, d)
		}
	}
	for i := range msgs {
		c.emitMsg(&msgs[i])
	}
	c.b54Msgs(f.TimeSec, msgs)
	pr := c.profile()
	c.mu.Lock()
	c.st.Profile = pr
	c.mu.Unlock()
	c.l1Row(f, kd, msgs)
	c.voiceFrame(f, p)

	if c.tx != nil {
		for _, m := range msgs {
			if m.Msg.Type == l3.TypeEnd {
				c.tx.endSeen = true
			}
			if m.Msg.Maker >= 0 {
				c.tx.rec.Maker = m.Msg.Maker
				c.mu.Lock()
				c.st.Maker = m.Msg.Maker
				c.mu.Unlock()
			}
			if len(c.tx.rec.Msgs) < 16 {
				c.tx.rec.Msgs = append(c.tx.rec.Msgs, m.Msg.Ch+" "+m.Msg.Summary())
			}
		}
		if valid && r.M == 0 && c.tx.rec.Voice > 0 {
			c.tx.endSeen = true
		}
	}
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
		if fh, err := os.Create(rec.Path(t.base, rec.Calls)); err == nil {
			t.calls = fh
		}
		c.note(fmt.Sprintf("tx_start %.3f", t.rec.TSec))
		if init, ok := c.keyer.Init(); ok {
			c.note(fmt.Sprintf("whitening 0x%03X %s", init, c.keyer.Why()))
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
		c.sacch.Reset()
		return
	}
	if t.endSeen && why == "同期が外れた" {
		why = "終わりのフレーム"
	}
	c.b54Events(t.lastT, c.b54.Reset())
	audioSec := c.finishVoice()
	t.rec.Open = false
	t.rec.EndSec = math.Max(t.lastT+l1.FrameSec, t.rec.TSec)
	t.rec.End = why
	t.rec.Audio = audioSec > 0
	sec := t.rec.EndSec - t.rec.TSec
	c.note(fmt.Sprintf("tx_end %.3f %s（%d フレーム・%.2f 秒）", t.rec.EndSec, why, t.rec.Frames, sec))
	if t.calls != nil {
		t.calls.Close()
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
	c.sacch.Reset()
	c.mu.Lock()
	c.st.Recording, c.st.Talking = false, false
	c.mu.Unlock()
	c.publish(true)
}

func (c *Channel) note(line string) {
	t := c.tx
	if t == nil || t.base == "" {
		return
	}
	c.sink.RecNote(0, line)
	if t.calls != nil {
		fmt.Fprintln(t.calls, line)
	}
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


func (c *Channel) msg(t float64, m l3.Message, u int) MsgRec {
	key := fmt.Sprintf("%s/%X/%d", m.Ch, m.Raw, u)
	tree := []Node{{Label: m.Ch, Value: m.Name}}
	for _, r := range m.Rows {
		tree[0].Children = append(tree[0].Children, Node{Label: r.Name, Value: r.Text, Bits: r.Bits})
	}
	if u >= 0 {
		tree[0].Children = append([]Node{{Label: "ユーザコード U", Value: fmt.Sprintf("%d", u)}}, tree[0].Children...)
	}
	return MsgRec{Idx: c.cfg.ID, TSec: t, Msg: m, U: u, Tree: tree, Repeat: 1, key: key}
}

func (c *Channel) emitMsg(m *MsgRec) {
	if prev := c.lastMsg[m.Msg.Ch]; prev != nil && prev.key == m.key && m.TSec-prev.TSec < 60 {
		prev.Repeat++
		prev.TSec = m.TSec
		r := *prev
		c.sink.Emit(Event{Type: EvMsg, Msg: &r})
		return
	}
	c.msgNo++
	m.No = int64(c.cfg.ID)<<40 | c.msgNo
	cp := *m
	c.lastMsg[m.Msg.Ch] = &cp
	c.sink.Emit(Event{Type: EvMsg, Msg: m})
	c.note(fmt.Sprintf("msg %s %s", m.Msg.Ch, m.Msg.Summary()))
}


func (c *Channel) readData(f decoder.Frame, d *l1.DataUnit) {
	bits := l1.SACCHInfo(d.S, d.U, make([]uint8, 18))[:8]
	bits = append(bits, l1.BytesToBits(d.Body[:])...)
	evs := c.dataSt.Read(data.Frame{Ch: l1.DataChannel, No: f.No, TimeSec: f.TimeSec, Bits: bits, Route: d.Kind.String()})
	for _, ev := range evs {
		c.emitData(&DataEvent{Idx: c.cfg.ID, MHz: c.cfg.MHz, Event: ev})
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
		path := rec.DataLog(rec.SystemDir(c.std.cfg.LogDir, System), now, c.cfg.MHz)
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
	parts := []string{}
	if s.Init >= 0 {
		parts = append(parts, fmt.Sprintf("初期値 0x%03X", s.Init))
	}
	if s.U >= 0 {
		parts = append(parts, fmt.Sprintf("U=%d", s.U))
	}
	if s.Maker >= 0 {
		parts = append(parts, fmt.Sprintf("製造者 %d", s.Maker))
	}
	if s.Caller != "" {
		parts = append(parts, "発信者名「"+s.Caller+"」")
	}
	s.Note = strings.Join(parts, "・")
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
