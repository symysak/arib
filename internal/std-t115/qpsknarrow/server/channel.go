package server

import (
	"fmt"
	"math"
	"sync"
	"sync/atomic"
	"time"

	"github.com/symysak/arib/internal/rec"
	"github.com/symysak/arib/internal/rx"
	"github.com/symysak/arib/internal/std-t115/qpsknarrow/amrwbp"
	"github.com/symysak/arib/internal/std-t115/qpsknarrow/control"
	dec "github.com/symysak/arib/internal/std-t115/qpsknarrow/decoder"
	"github.com/symysak/arib/internal/std-t115/qpsknarrow/dsp"
)

const (
	maxControls   = 400
	maxFrames     = 600
	maxBroadcasts = 50
)

const (
	batchSFs  = 13
	warmUpSFs = 4
)

const (
	silenceCloseSec = 3.0
	midJoinFrames = 12
	broadcastMaxS = 15 * 60
	stationTick = 200 * time.Millisecond
	lockMinGood = 3
)

type Channel struct {
	std  *Standard
	cfg  rx.ChannelConfig
	sink rx.Sink
	opt  ChannelOpt

	d        *dec.Decoder
	ddc      *recDDC
	asm      amrwbp.Assembler
	warmSFs  [][]amrwbp.Frame
	started  time.Time
	lastPub  time.Time
	startPos int64

	pinReq atomic.Int64
	nFrames, nGood, absN atomic.Int64

	mu         sync.Mutex
	controls   []ControlInfo
	frames     []FrameInfo
	broadcasts []BroadcastInfo
	quality    QualityInfo
	signal     SignalInfo
	constell   ConstellationInfo
	scramble   ScrambleInfo
	current    *BroadcastInfo
	level      *float64
	pendSFs    [][]amrwbp.Frame
	pcm        []int16

	lastActivity float64
	voiceRun     int
	nextBCID     int
	watched      bool
	forget       bool
}

func newChannel(s *Standard, c rx.ChannelConfig, sink rx.Sink, opt ChannelOpt) *Channel {
	ch := &Channel{std: s, cfg: c, sink: sink, opt: opt, nextBCID: 1, watched: true, started: time.Now()}
	ch.pinReq.Store(-1)
	ch.startPos = int64(math.Round(c.StartSec * c.FsIn))
	ch.absN.Store(ch.startPos)
	ch.newDecoder(opt.Scramble)
	ch.ddc = newRecDDC(c.FsIn, c.OffHz)
	sc := ScrambleInfo{Init: opt.Scramble, Locked: opt.Scramble != 0, Pinned: opt.Scramble != 0}
	sc.SetMunicipality()
	ch.scramble = sc
	return ch
}

func (c *Channel) newDecoder(scramble int) {
	c.d = dec.New(dec.Config{
		SampleRate:   c.cfg.FsIn,
		OffsetHz:     c.cfg.OffHz,
		ScrambleInit: scramble,
		OnScramble: c.onScramble,
		OnBlock: func(b dec.BlockStats) {
			lv := b.LevelDB
			c.mu.Lock()
			s := SignalInfo{TSec: b.TSec, CFOHz: b.CFOHz, CFOProm: b.CFOProm,
				TimingTau: b.TimingTau, EVM: b.EVM, LevelDB: b.LevelDB, CorrPeak: c.signal.CorrPeak}
			c.signal = s
			if !math.IsInf(lv, 0) && !math.IsNaN(lv) {
				c.level = &lv
			}
			c.mu.Unlock()
			c.emit(EvSignal, s)
		},
		OnConstellation: func(k dec.Constellation) {
			ci := ConstellationInfo{TSec: k.TSec, Points: k.Points}
			c.mu.Lock()
			c.constell = ci
			c.mu.Unlock()
			if h := c.std.hooks; h == nil || h.selected == nil || h.selected(c.cfg.ID) {
				c.emit(EvConstellation, ci)
			}
		},
	})
	c.d.SetStartSample(c.startPos)
}


func (c *Channel) Feed(wide []complex64) {
	if v := c.pinReq.Swap(-1); v >= 0 {
		c.applyPin(int(v))
	}
	c.sink.IQ(c.ddc.Process(wide), c.ddc.fsOut)
	frames := c.d.Feed(wide)
	c.absN.Store(c.d.ProcessedSamples())
	c.handleFrames(frames, c.procSec())
	c.publishStation(false)
}

func (c *Channel) Resync() {
	c.mu.Lock()
	c.quality.Overflows++
	n := c.quality.Overflows
	pinned := 0
	if c.scramble.Pinned {
		pinned = c.scramble.Init
	}
	c.mu.Unlock()
	c.startPos = c.d.ProcessedSamples()
	c.newDecoder(pinned)
	if n == 1 || n%100 == 0 {
		c.logf(c.procSec(), "warn", "system", "復号が追いつかず入力を捨てたので仕切り直した（累計 %d 回）", n)
	}
}

func (c *Channel) Retune() {
	c.startPos = c.d.ProcessedSamples()
	c.newDecoder(c.pinnedValue())
}

func (c *Channel) pinnedValue() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.scramble.Pinned {
		return c.scramble.Init
	}
	return 0
}

func (c *Channel) Lock() (bool, float64, string) {
	n, g := c.nFrames.Load(), c.nGood.Load()
	score := 0.0
	if n > 0 {
		score = float64(g) / float64(n)
	}
	c.mu.Lock()
	sc := c.scramble
	c.mu.Unlock()
	label := fmt.Sprintf("CRC16 OK %d/%d フレーム", g, n)
	if sc.Locked {
		label += fmt.Sprintf("・スクランブル 0x%04X", sc.Init)
	}
	return g >= lockMinGood, score, label
}

func (c *Channel) Counters() rx.Counters {
	return rx.Counters{Frames: int(c.nFrames.Load()), Good: int(c.nGood.Load()),
		ProcSec: c.nowSec() - c.cfg.StartSec}
}

func (c *Channel) Summary() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	q := c.quality
	return fmt.Sprintf("%.4f MHz: フレーム %d（CCH %d/%d TCH %d/%d）スクランブル %s 通報 %d 本",
		c.cfg.MHz, q.Frames, q.CCHOK, q.CCHTotal, q.TCHOK, q.TCHTotal, scrambleText(c.scramble), len(c.broadcasts))
}

func (c *Channel) State() rx.State {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.current != nil {
		return rx.StateActive
	}
	return rx.StateIdle
}

func (c *Channel) Snapshot(ext map[string]any) { ext["t115"] = c.snapshot() }

func (c *Channel) Close(dropped bool) {
	now := c.procSec()
	if !dropped {
		c.handleFrames(c.d.Flush(), c.procSec())
		now = c.procSec()
		c.flushAudio(true)
		c.closeBroadcast(now, "終端")
		c.emitQuality(now)
		el := time.Since(c.started).Seconds()
		c.logf(now, "info", "system", "入力終端。%.1f 秒を %.1f 秒で処理（%.1f×）",
			now-c.cfg.StartSec, el, (now-c.cfg.StartSec)/math.Max(el, 1e-9))
	} else {
		c.flushAudio(true)
		c.mu.Lock()
		last := c.lastActivity
		c.mu.Unlock()
		c.closeBroadcast(math.Max(last, 0), "監視終了")
	}
	c.mu.Lock()
	if dropped {
		c.watched = false
	}
	c.mu.Unlock()
	c.publishStation(true)
	if h := c.std.hooks; h != nil && h.closed != nil {
		h.closed(c, dropped)
	}
}


func (c *Channel) ID() int { return c.cfg.ID }

func (c *Channel) MHz() float64 { return c.cfg.MHz }

func (c *Channel) PinScramble(init int) { c.pinReq.Store(int64(init)) }

func (c *Channel) applyPin(v int) {
	c.d.PinScramble(v)
	info := ScrambleInfo{Init: c.d.ScrambleInit(), Locked: c.d.ScrambleLocked(), Pinned: c.d.ScramblePinned()}
	info.SetMunicipality()
	c.mu.Lock()
	c.scramble = info
	last := c.lastActivity
	c.mu.Unlock()
	if v == 0 {
		c.logf(last, "info", "station", "スクランブル値の固定を解除しました（自動判定へ戻す）")
	} else {
		c.logf(last, "info", "station", "スクランブル値を 0x%04X に固定しました → 市区町村 %s", v, info.MunicipalityLabel)
	}
	c.emit(EvScramble, info)
	c.publishStation(true)
}

func (c *Channel) onScramble(si dec.ScrambleInfo) {
	info := ScrambleInfo{
		Init: si.Init, Locked: true, Prominence: si.Prominence,
		Confidence: si.Confidence, Frames: si.Frames, TSec: si.TimeSec, Source: si.Source,
	}
	info.SetMunicipality()
	c.mu.Lock()
	c.scramble = info
	c.mu.Unlock()
	c.logf(si.TimeSec, "info", "station",
		"スクランブル値を自動判定しました: 0x%04X（信頼度 %.3f, %s %d 枚）→ 市区町村 %s",
		si.Init, si.Confidence, si.Source, si.Frames, info.MunicipalityLabel)
	c.emit(EvScramble, info)
	c.publishStation(true)
}

func (c *Channel) procSec() float64 { return float64(c.d.ProcessedSamples()) / c.cfg.FsIn }

func (c *Channel) nowSec() float64 { return float64(c.absN.Load()) / c.cfg.FsIn }

func (c *Channel) wall(tsec float64) time.Time {
	return c.std.cfg.Origin.Add(time.Duration(tsec * float64(time.Second)))
}

func (c *Channel) emit(typ string, data any) {
	c.sink.Emit(Event{Type: typ, Idx: c.cfg.ID, Data: data})
}

func (c *Channel) logf(tsec float64, level, group, format string, args ...any) {
	l := LogLine{TSec: tsec, Idx: c.cfg.ID, Group: group, Level: level, Text: fmt.Sprintf(format, args...)}
	c.sink.Log("%.4f MHz %s [%s] %s", c.cfg.MHz, c.wall(tsec).Format("15:04:05.000"), level, l.Text)
	c.sink.Emit(Event{Type: EvLog, Idx: c.cfg.ID, Log: &l})
}

func (c *Channel) station() Station {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := c.nowSec()
	st := Station{
		Idx: c.cfg.ID, MHz: c.cfg.MHz, OffsetHz: c.cfg.OffHz, Watched: c.watched,
		Synced:    c.quality.Frames > 0 && now-c.lastActivity <= silenceCloseSec,
		Recording: c.current != nil && c.current.Base != "",
		LevelDB:   c.level, LastTSec: c.lastActivity,
		Ext:      map[string]any{"talking": c.current != nil && c.current.VoiceFrames > 0},
		Scramble: c.scramble, Frames: c.quality.Frames,
		CCHOK: c.quality.CCHOK, CCHTotal: c.quality.CCHTotal,
		TCHOK: c.quality.TCHOK, TCHTotal: c.quality.TCHTotal,
		Broadcasts: len(c.broadcasts),
	}
	if c.current != nil {
		st.Current = c.current.ID
	}
	return st
}

func (c *Channel) snapshot() ChannelSnapshot {
	c.mu.Lock()
	defer c.mu.Unlock()
	s := ChannelSnapshot{
		Scramble: c.scramble, Quality: c.quality, Signal: c.signal, Constellation: c.constell,
		Controls:   append(make([]ControlInfo, 0, len(c.controls)), c.controls...),
		Frames:     append(make([]FrameInfo, 0, len(c.frames)), c.frames...),
		Broadcasts: append(make([]BroadcastInfo, 0, len(c.broadcasts)), c.broadcasts...),
	}
	if s.Constellation.Points == nil {
		s.Constellation.Points = []float32{}
	}
	if c.current != nil {
		cur := *c.current
		s.Current = &cur
	}
	return s
}

func (c *Channel) publishStation(force bool) {
	if !force && time.Since(c.lastPub) < stationTick {
		return
	}
	c.lastPub = time.Now()
	st := c.station()
	c.sink.Emit(Event{Type: EvStation, Idx: c.cfg.ID, Station: &st})
}


func (c *Channel) handleFrames(frames []dec.Frame, nowSec float64) {
	for _, f := range frames {
		c.onFrame(f)
	}
	c.mu.Lock()
	last, open := c.lastActivity, c.current != nil
	tooLong := c.current != nil && nowSec-c.current.StartSec > broadcastMaxS
	c.mu.Unlock()
	if open && nowSec-last > silenceCloseSec {
		c.closeBroadcast(last, "制御途絶")
	} else if tooLong {
		c.closeBroadcast(nowSec, "上限超過")
	}
	if len(frames) > 0 {
		c.emitQuality(nowSec)
	}
	c.flushAudio(false)
}

func (c *Channel) onFrame(f dec.Frame) {
	fi := FrameInfo{
		TSec: f.TimeSec, Kind: f.Kind.String(), CorrPeak: f.CorrPeak, PhaseDeg: f.PhaseDeg,
		SWOK: true, AMRSN: f.AMRSN,
	}
	msg := f.Message
	if msg == nil {
		msg = f.TCHMessage
	}
	if msg != nil {
		fi.Channel = msg.Header.ChIDName
	}
	fi.CRCOK = f.CRCOK || f.TCHCRCOK
	c.nFrames.Add(1)
	if fi.CRCOK {
		c.nGood.Add(1)
	}

	c.mu.Lock()
	c.lastActivity = f.TimeSec
	c.quality.Frames++
	switch f.Kind {
	case dec.KindSB0:
		c.quality.SB0++
		c.quality.CCHTotal++
		if f.CRCOK {
			c.quality.CCHOK++
		}
	case dec.KindSC:
		c.quality.SC++
		c.quality.TCHTotal++
		if f.TCHCRCOK {
			c.quality.TCHOK++
		}
	}
	c.frames = append(c.frames, fi)
	if len(c.frames) > maxFrames {
		c.frames = c.frames[len(c.frames)-maxFrames:]
	}
	c.signal.TSec = f.TimeSec
	c.signal.CorrPeak = f.CorrPeak
	c.mu.Unlock()
	c.emit(EvFrame, fi)

	if msg != nil {
		c.onControl(f, msg)
	}
	if f.Kind == dec.KindSC && f.AMRSN >= 0 && len(f.Voice) > 0 {
		c.onVoice(f)
	} else if f.Kind == dec.KindSC && f.TCHCRCOK && msg != nil &&
		msg.Header.ChID == control.ChIDTCH && f.AMRSN < 0 {
		c.mu.Lock()
		c.voiceRun = 0
		c.mu.Unlock()
	}
}

func (c *Channel) onControl(f dec.Frame, m *control.Message) {
	ci := ControlInfo{
		TSec: f.TimeSec, Kind: f.Kind.String(), Channel: m.Header.ChIDName,
		Type: m.Header.Type, TypeName: m.Header.TypeName,
		RawHex: fmt.Sprintf("%X", m.Raw), Summary: m.String(), Busy: m.Header.Busy,
		ManufacturerCode: m.Header.ManufacturerCode, ManufacturerName: m.Header.ManufacturerName,
		LicenseeInfo: m.Header.LicenseeInfo, ChSwitchToSC: m.Header.ChSwitchToSC,
		ChSwitchTiming: m.Header.ChSwitchTiming, AMRSN: m.Header.AMRWBPlusSN,
	}
	ci.Notify = notifyInfo(m)
	ci.Bits = control.Expand(m.Raw, m.Header.Type)

	if isVoiceMsgType(m.Header.Type) {
		c.handleBroadcastControl(f, m)
		return
	}

	c.mu.Lock()
	dup := len(c.controls) > 0 &&
		c.controls[len(c.controls)-1].RawHex == ci.RawHex &&
		ci.TSec-c.controls[len(c.controls)-1].TSec < 3.0
	if !dup {
		c.controls = append(c.controls, ci)
		if len(c.controls) > maxControls {
			c.controls = c.controls[len(c.controls)-maxControls:]
		}
	}
	c.mu.Unlock()
	if !dup {
		c.emit(EvControl, ci)
	}
	c.handleBroadcastControl(f, m)
}

func isVoiceMsgType(t int) bool {
	switch t {
	case control.MsgVoiceGroupIndiv, control.MsgVoiceSimultaneous,
		control.MsgVoiceEmergency, control.MsgVoiceContactCall:
		return true
	}
	return false
}

func (c *Channel) handleBroadcastControl(f dec.Frame, m *control.Message) {
	switch m.Header.Type {
	case control.MsgNotifyStart:
		c.openBroadcast(f.TimeSec, notifyInfo(m), false)
	case control.MsgForcedDisconnect:
		c.closeBroadcast(f.TimeSec, "強制切断指示")
	case control.MsgDisconnect, control.MsgDisconnectFACCH:
		c.closeBroadcast(f.TimeSec, "切断指示")
	}
}

func notifyInfo(m *control.Message) *NotifyInfo {
	n := m.NotifyStart
	if n == nil {
		return nil
	}
	target := fmt.Sprintf("群/個別 #%d", n.SubStationID)
	if n.Simultaneous {
		target = "一斉"
	}
	return &NotifyInfo{
		MediaCode: n.MediaCode, Media: n.Media, TransProt: n.TransProt,
		CallNo: n.CallNo, SubStationID: n.SubStationID, Simultaneous: n.Simultaneous,
		Emergency: n.Emergency, ForcedVolume: n.ForcedVolume,
		RecordRelease: n.RecordRelease, NumberNotify: n.NumberNotify,
		VolumeSetting: n.VolumeSetting, TimeSplitOK: n.TimeSplitOK, SplitNo: n.SplitNo,
		SpareOct5: n.SpareOct5, SpareOct6: n.SpareOct6, SpareOct9to12: n.SpareOct9to12,
		Target: target,
	}
}

func (c *Channel) openBroadcast(tsec float64, n *NotifyInfo, midJoin bool) {
	c.mu.Lock()
	if c.current != nil {
		if n == nil || c.current.CallNo == n.CallNo {
			c.mu.Unlock()
			return
		}
		c.mu.Unlock()
		c.closeBroadcast(tsec, "別の呼の開始指示")
		c.mu.Lock()
	}
	bc := &BroadcastInfo{ID: c.nextBCID, StartSec: tsec, MidJoin: midJoin, Target: "不明"}
	c.nextBCID++
	if n != nil {
		bc.Target = n.Target
		bc.CallNo = n.CallNo
		bc.Emergency = n.Emergency
	}
	c.current = bc
	c.voiceRun = 0
	c.pcm = nil
	lag := c.d.LagSeconds()
	c.mu.Unlock()
	base := c.sink.RecOpen(bc.ID, recLabel(bc.ID, bc.Target), lag)
	c.mu.Lock()
	bc.Base = base
	out := *bc
	c.mu.Unlock()
	if base == "" {
		c.logf(tsec, "error", "record", "通報 #%d の録音を開けない", out.ID)
	}
	c.logf(tsec, "info", "call", "通報開始 #%d（%s%s）", out.ID, out.Target,
		map[bool]string{true: " ※途中参加", false: ""}[midJoin])
	c.emit(EvBroadcastStart, out)
	c.publishStation(true)
}

func (c *Channel) closeBroadcast(tsec float64, reason string) {
	c.flushAudio(true)
	c.mu.Lock()
	cur := c.current
	if cur == nil {
		c.mu.Unlock()
		return
	}
	cur.EndSec = tsec
	cur.EndReason = reason
	pcm := c.pcm
	c.pcm = nil
	c.current = nil
	c.voiceRun = 0
	q, scr := c.quality, c.scramble
	var mfr, raw string
	for i := len(c.controls) - 1; i >= 0; i-- {
		if c.controls[i].Notify != nil {
			mfr, raw = c.controls[i].ManufacturerName, c.controls[i].RawHex
			break
		}
	}
	c.mu.Unlock()

	if len(pcm) > 0 {
		cur.VoiceSeconds = float64(len(pcm)) / float64(amrwbp.OutputRate)
	}
	if cur.Base != "" {
		if len(pcm) > 0 {
			p := rec.Path(cur.Base, rec.Audio)
			if err := writeAudio(p, pcm, amrwbp.OutputRate); err != nil {
				c.logf(tsec, "error", "record", "通報 #%d の音声を書けない: %v", cur.ID, err)
			} else {
				cur.AudioURL = fileURL("audio", c.std.cfg.LogDir, p)
				c.logf(tsec, "info", "record", "通報 #%d の音声を保存しました: %s（%.1f 秒）", cur.ID, p, cur.VoiceSeconds)
			}
		}
		if !c.std.cfg.NoIQ {
			cur.IQURL = fileURL("iq", c.std.cfg.LogDir, rec.Path(cur.Base, rec.IQ))
		}
		pinned := fmt.Sprintf("自動判定 信頼度 %.3f", scr.Confidence)
		if scr.Pinned {
			pinned = "運用者が固定"
		}
		extra := [][2]string{
			{"開始（実時刻）", c.wall(cur.StartSec).Format("2006-01-02 15:04:05.000")},
			{"終了（実時刻）", c.wall(cur.EndSec).Format("2006-01-02 15:04:05.000")},
			{"長さ", fmt.Sprintf("%.1f 秒", cur.EndSec-cur.StartSec)},
			{"音声", fmt.Sprintf("%.2f 秒（%d フレーム）", cur.VoiceSeconds, cur.VoiceFrames)},
			{"終了理由", cur.EndReason},
			{"scramble", fmt.Sprintf("0x%04X（%s）", scr.Init, pinned)},
			{"municipality", scr.MunicipalityLabel},
			{"manufacturer", mfr},
			{"notify_raw", raw},
			{"cch_crc", fmt.Sprintf("%d/%d", q.CCHOK, q.CCHTotal)},
			{"tch_crc", fmt.Sprintf("%d/%d", q.TCHOK, q.TCHTotal)},
			{"voice_filled", fmt.Sprintf("%d 枚（%.2f 秒）", q.VoiceFilled, float64(q.VoiceFilled)*0.08)},
			{"voice_dropped", fmt.Sprintf("%d", q.VoiceDrop)},
		}
		if cur.AudioURL != "" {
			extra = append(extra, [2]string{"audio_path", rec.Path(cur.Base, rec.Audio)})
		}
		if !c.std.cfg.NoIQ {
			extra = append(extra, [2]string{"iq_path", fmt.Sprintf("%s（%.0f Hz, 2ch int16 = I/Q。"+
				"`live -input <このファイル>@%.5f` で再デコード可）", rec.Path(cur.Base, rec.IQ), c.ddc.fsOut, c.cfg.MHz)})
		}
		for _, l := range sidecarLines(*cur, c.cfg.MHz, extra) {
			c.sink.RecNote(cur.ID, l)
		}
		c.sink.RecClose(cur.ID, iqTailSec)
	}

	c.mu.Lock()
	c.broadcasts = append(c.broadcasts, *cur)
	if len(c.broadcasts) > maxBroadcasts {
		c.broadcasts = c.broadcasts[len(c.broadcasts)-maxBroadcasts:]
	}
	out := *cur
	c.mu.Unlock()
	c.logf(tsec, "info", "call", "通報終了 #%d（%s, %.1f 秒, 音声 %.1f 秒）",
		out.ID, reason, out.EndSec-out.StartSec, out.VoiceSeconds)
	c.emit(EvBroadcastEnd, out)
	c.publishStation(true)
}

func (c *Channel) onVoice(f dec.Frame) {
	c.mu.Lock()
	c.voiceRun++
	needMidJoin := c.current == nil && c.voiceRun >= midJoinFrames
	if c.current != nil {
		c.current.VoiceFrames++
	}
	c.mu.Unlock()
	if needMidJoin {
		c.openBroadcast(f.TimeSec-float64(midJoinFrames)*dsp.FrameDurationSec, nil, true)
	}
	c.asm.Push(f.TimeSec, f.AMRSN, f.Voice)
	sfs := c.asm.Superframes()
	if len(sfs) == 0 {
		return
	}
	c.mu.Lock()
	c.pendSFs = append(c.pendSFs, sfs...)
	c.quality.VoiceSFs += len(sfs)
	c.quality.VoiceDrop = c.asm.Dropped()
	c.quality.VoiceFilled = c.asm.Filled()
	c.quality.VoiceCapped = c.asm.Capped()
	c.mu.Unlock()
}

func (c *Channel) flushAudio(force bool) {
	for {
		c.mu.Lock()
		if len(c.pendSFs) == 0 || (!force && len(c.pendSFs) < batchSFs) {
			c.mu.Unlock()
			return
		}
		n := min(len(c.pendSFs), batchSFs)
		batch := append([][]amrwbp.Frame(nil), c.pendSFs[:n]...)
		c.pendSFs = append(c.pendSFs[:0], c.pendSFs[n:]...)
		curID := 0
		if c.current != nil {
			curID = c.current.ID
		}
		c.mu.Unlock()

		if !amrwbp.Available() {
			continue
		}
		warm := c.warmSFs
		full := append(append([][]amrwbp.Frame(nil), warm...), batch...)
		pcm, err := amrwbp.Decode(full)
		if err != nil {
			c.logf(c.procSec(), "error", "record", "音声復号に失敗: %v", err)
			return
		}
		drop := min(len(warm)*int(0.160*float64(amrwbp.OutputRate)), len(pcm))
		out := pcm[drop:]
		c.warmSFs = append([][]amrwbp.Frame(nil), batch[max(0, len(batch)-warmUpSFs):]...)

		c.mu.Lock()
		c.quality.AudioSec += float64(len(out)) / float64(amrwbp.OutputRate)
		if curID > 0 && c.current != nil && c.current.ID == curID {
			c.pcm = append(c.pcm, out...)
		}
		c.mu.Unlock()
		c.sink.Audio(curID, out, amrwbp.OutputRate)
	}
}

func (c *Channel) emitQuality(nowSec float64) {
	c.mu.Lock()
	el := time.Since(c.started).Seconds()
	c.quality.ElapsedSec = el
	if el > 0 {
		c.quality.Throughput = (nowSec - c.cfg.StartSec) / el
	}
	q := c.quality
	c.mu.Unlock()
	c.emit(EvQuality, q)
}

func scrambleText(s ScrambleInfo) string {
	if !s.Locked {
		return "判定中"
	}
	if s.Pinned {
		return fmt.Sprintf("0x%04X（固定）", s.Init)
	}
	return fmt.Sprintf("0x%04X", s.Init)
}
