package server

import (
	"errors"
	"fmt"
	"io"
	"math"
	"sync"
	"sync/atomic"
	"time"

	"github.com/symysak/stdt86/internal/std-t115/qpsknarrow/amrwbp"
	"github.com/symysak/stdt86/internal/std-t115/qpsknarrow/control"
	dec "github.com/symysak/stdt86/internal/std-t115/qpsknarrow/decoder"
	"github.com/symysak/stdt86/internal/std-t115/qpsknarrow/dsp"
	"github.com/symysak/stdt86/internal/std-t115/qpsknarrow/iqsrc"
)

type Config struct {
	Source string
	OffsetHz float64
	SampleRate float64
	Format string
	FreqHz float64
	GainDB float64
	AGC    bool
	Realtime bool
	Speed float64
	ScrambleInit int
	Addr string
	LogDir string
	NoIQ bool
}

const (
	maxControls   = 400
	maxFrames     = 600
	maxLogs       = 200
	maxBroadcasts = 50
)

const (
	batchSFs  = 13
	warmUpSFs = 4
)

const sampleQueue = 8

const (
	silenceCloseSec = 3.0
	midJoinFrames = 12
	broadcastMaxS = 15 * 60
)

type Pipeline struct {
	cfg Config

	mu         sync.RWMutex
	snap       Snapshot
	controls   []ControlInfo
	frames     []FrameInfo
	broadcasts []BroadcastInfo
	logs       []LogInfo
	quality    QualityInfo
	signal     SignalInfo
	constell   ConstellationInfo
	current    *BroadcastInfo

	audio map[int][]int16

	hub      *hub
	audioHub *hub

	asm      amrwbp.Assembler
	pendSFs  [][]amrwbp.Frame
	warmSFs  [][]amrwbp.Frame
	audioSeq int64

	lastActivity float64
	voiceRun     int
	nextBCID     int

	started time.Time
	stopped chan struct{}
	once    sync.Once

	lw  *logWriter
	iqr *iqRecorder

	pinReq atomic.Int64
}

func NewPipeline(cfg Config) *Pipeline {
	t0, est := TimeOrigin(cfg.Source)
	p := &Pipeline{
		cfg:      cfg,
		audio:    map[int][]int16{},
		hub:      newHub(),
		audioHub: newHub(),
		stopped:  make(chan struct{}),
		nextBCID: 1,
	}
	p.pinReq.Store(-1)
	lw, err := newLogWriter(cfg.LogDir, time.Now())
	if err != nil {
		lw = &logWriter{wavPath: map[int]string{}}
	}
	p.lw = lw
	p.snap = Snapshot{
		T0WallMS:       t0.UnixMilli(),
		T0Estimated:    est,
		Source:         cfg.Source,
		OffsetHz:       cfg.OffsetHz,
		Realtime:       cfg.Realtime,
		System:         "ARIB STD-T115 QPSK ナロー方式（Volume 2, 7.5kHz 間隔）",
		SymbolRate:     dsp.SymbolRate,
		FrameBits:      dsp.FrameBits,
		RollOff:        dsp.RollOff,
		SW1Hex:         dsp.SW1Hex,
		SW3Hex:         dsp.SW3Hex,
		AudioAvailable: amrwbp.Available(),
		AudioRate:      amrwbp.OutputRate,
		LogDir:         cfg.LogDir,
		LogPath:        lw.LogPath(),
	}
	return p
}

func (p *Pipeline) Snapshot() Snapshot {
	p.mu.RLock()
	defer p.mu.RUnlock()
	s := p.snap
	s.Quality = p.quality
	s.Signal = p.signal
	s.Constellation = p.constell
	s.Controls = append(make([]ControlInfo, 0, len(p.controls)), p.controls...)
	s.Frames = append(make([]FrameInfo, 0, len(p.frames)), p.frames...)
	s.Broadcasts = append(make([]BroadcastInfo, 0, len(p.broadcasts)), p.broadcasts...)
	s.Logs = append(make([]LogInfo, 0, len(p.logs)), p.logs...)
	if p.current != nil {
		c := *p.current
		s.Current = &c
	}
	return s
}

func (p *Pipeline) iqRec() *iqRecorder {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if p.iqr == nil {
		return &iqRecorder{}
	}
	return p.iqr
}

func (p *Pipeline) PinScramble(init int) { p.pinReq.Store(int64(init)) }

func (p *Pipeline) IQPath(id int) string { return p.iqRec().Path(id) }

func (p *Pipeline) lastSeen() float64 {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.lastActivity
}

func (p *Pipeline) AudioWav(id int) ([]byte, bool) {
	p.mu.RLock()
	pcm := p.audio[id]
	p.mu.RUnlock()
	if len(pcm) == 0 {
		return nil, false
	}
	return WavBytes(pcm, amrwbp.OutputRate), true
}

func (p *Pipeline) logf(tsec float64, level, format string, args ...any) {
	l := LogInfo{TSec: tsec, Level: level, Text: fmt.Sprintf(format, args...)}
	p.mu.Lock()
	p.logs = append(p.logs, l)
	if len(p.logs) > maxLogs {
		p.logs = p.logs[len(p.logs)-maxLogs:]
	}
	p.mu.Unlock()
	p.lw.WriteLog(l, p.wallOf(tsec))
	p.hub.broadcastJSON(ev(EvLog, l))
}

func (p *Pipeline) wallOf(tsec float64) time.Time {
	p.mu.RLock()
	base := p.snap.T0WallMS
	p.mu.RUnlock()
	return time.UnixMilli(base).Add(time.Duration(tsec * float64(time.Second)))
}

func (p *Pipeline) Run() error {
	src, err := iqsrc.Open(p.cfg.Source, iqsrc.Options{
		SampleRate: p.cfg.SampleRate,
		Format:     p.cfg.Format,
		FreqHz:     p.cfg.FreqHz,
		GainDB:     p.cfg.GainDB,
		AGC:        p.cfg.AGC,
		Realtime:   p.cfg.Realtime,
		Speed:      p.cfg.Speed,
	})
	if err != nil {
		return err
	}
	defer src.Close()
	rate := src.SampleRate()
	network := iqsrc.IsNetwork(p.cfg.Source)

	p.mu.Lock()
	p.snap.SampleRate = rate
	if n := src.Len(); n > 0 {
		p.snap.DurationS = float64(n) / rate
	}
	p.snap.Network = network
	p.snap.SourceDesc = src.Describe()
	initScr := ScrambleInfo{
		Init: p.cfg.ScrambleInit, Locked: p.cfg.ScrambleInit != 0,
		Pinned: p.cfg.ScrambleInit != 0,
	}
	initScr.SetMunicipality()
	p.snap.Scramble = initScr
	p.snap.ScrambleInit = p.cfg.ScrambleInit
	p.mu.Unlock()

	d := dec.New(dec.Config{
		SampleRate:   rate,
		OffsetHz:     p.cfg.OffsetHz,
		ScrambleInit: p.cfg.ScrambleInit,
		OnScramble: func(si dec.ScrambleInfo) {
			info := ScrambleInfo{
				Init: si.Init, Locked: true, Prominence: si.Prominence,
				Confidence: si.Confidence, Frames: si.Frames, TSec: si.TimeSec,
			}
			info.SetMunicipality()
			p.mu.Lock()
			p.snap.Scramble = info
			p.snap.ScrambleInit = si.Init
			p.mu.Unlock()
			p.logf(si.TimeSec, "info",
				"スクランブル値を自動判定しました: 0x%04X（信頼度 %.3f, SB0 %d 枚）→ 市区町村 %s",
				si.Init, si.Confidence, si.Frames, info.MunicipalityLabel)
			p.hub.broadcastJSON(ev(EvScramble, info))
		},
		OnBlock: func(b dec.BlockStats) {
			p.EmitSignal(SignalInfo{
				TSec: b.TSec, CFOHz: b.CFOHz, CFOProm: b.CFOProm,
				TimingTau: b.TimingTau, EVM: b.EVM, LevelDB: b.LevelDB,
			})
		},
		OnConstellation: func(c dec.Constellation) {
			p.EmitConstellation(ConstellationInfo{TSec: c.TSec, Points: c.Points})
		},
	})

	iqr := &iqRecorder{}
	if p.lw.Enabled() && !p.cfg.NoIQ {
		iqr = newIQRecorder(p.cfg.LogDir, p.lw.Stamp(), rate, p.cfg.OffsetHz, d.LagSeconds())
		iqr.OnDone = func(id int, path string, sec float64, err error) {
			if err != nil {
				p.logf(p.lastSeen(), "error", "通報 #%d の I/Q 保存に失敗: %v", id, err)
				return
			}
			p.logf(p.lastSeen(), "info", "通報 #%d の I/Q を保存しました: %s（%.1f 秒 / %.0f Hz）",
				id, path, sec, iqr.SampleRate())
		}
	}
	p.mu.Lock()
	p.iqr = iqr
	p.snap.IQRate = iqr.SampleRate()
	p.mu.Unlock()

	p.started = time.Now()
	p.logf(0, "info", "受信開始: %s（オフセット %+.1f kHz）",
		src.Describe(), p.cfg.OffsetHz/1000)
	if !p.snap.AudioAvailable {
		p.logf(0, "warn", "AMR-WB+ デコーダが未ビルドのため音声は出ません（bash scripts/std-t115/build_amrwbplus.sh）")
	}

	chunk := int(0.25 * rate)
	samples := make(chan []complex64, sampleQueue)
	var readPos atomic.Int64
	readErr := make(chan error, 1)
	go p.readLoop(src, chunk, rate, samples, &readPos, readErr)

	var pos int64
	for x := range samples {
		select {
		case <-p.stopped:
			return nil
		default:
		}
		if v := p.pinReq.Swap(-1); v >= 0 {
			d.PinScramble(int(v))
			info := ScrambleInfo{Init: d.ScrambleInit(), Locked: d.ScrambleLocked(),
				Pinned: d.ScramblePinned()}
			info.SetMunicipality()
			p.mu.Lock()
			p.snap.Scramble = info
			p.snap.ScrambleInit = info.Init
			p.mu.Unlock()
			if v == 0 {
				p.logf(p.lastSeen(), "info", "スクランブル値の固定を解除しました（自動判定へ戻す）")
			} else {
				p.logf(p.lastSeen(), "info", "スクランブル値を 0x%04X に固定しました → 市区町村 %s",
					v, info.MunicipalityLabel)
			}
			p.hub.broadcastJSON(ev(EvScramble, info))
		}
		pos += int64(len(x))
		iqr.Feed(x)
		frames := d.Feed(x)
		p.handleFrames(frames, float64(d.ProcessedSamples())/rate)
	}
	select {
	case err := <-readErr:
		if err != nil {
			p.logf(float64(pos)/rate, "error", "入力エラー: %v", err)
		}
	default:
	}
	p.handleFrames(d.Flush(), float64(d.ProcessedSamples())/rate)
	p.flushAudio(true)
	p.closeBroadcast(float64(pos)/rate, "終端")
	iqr.Flush()
	p.emitQuality(float64(pos) / rate)
	p.logf(float64(pos)/rate, "info", "ソース終端。%.1f 秒を %.1f 秒で処理（%.1f×）",
		float64(pos)/rate, time.Since(p.started).Seconds(),
		(float64(pos)/rate)/math.Max(time.Since(p.started).Seconds(), 1e-9))
	p.hub.broadcastJSON(ev(EvFinished, map[string]any{"t_sec": float64(pos) / rate}))
	_ = p.lw.Close()
	return nil
}

func (p *Pipeline) Stop() {
	p.once.Do(func() { close(p.stopped) })
}

func (p *Pipeline) readLoop(src iqsrc.Source, chunk int, rate float64,
	out chan []complex64, readPos *atomic.Int64, errCh chan<- error) {
	defer close(out)
	lossy := src.Lossy()
	var idleSince time.Time
	stalled := false
	for {
		select {
		case <-p.stopped:
			return
		default:
		}
		x, err := src.Read(chunk)
		if errors.Is(err, io.EOF) {
			return
		}
		if err != nil {
			errCh <- err
			return
		}
		if len(x) == 0 {
			if idleSince.IsZero() {
				idleSince = time.Now()
			} else if !stalled && time.Since(idleSince) > 2*time.Second {
				stalled = true
				p.logf(float64(readPos.Load())/rate, "warn", "入力が途絶えました")
			}
			continue
		}
		if stalled {
			stalled = false
			p.logf(float64(readPos.Load())/rate, "info", "入力が復帰しました")
		}
		idleSince = time.Time{}
		readPos.Add(int64(len(x)))
		if !lossy {
			select {
			case out <- x:
			case <-p.stopped:
				return
			}
			continue
		}
		select {
		case out <- x:
		default:
			select {
			case <-out:
			default:
			}
			p.mu.Lock()
			p.quality.Overflows++
			n := p.quality.Overflows
			p.mu.Unlock()
			if n == 1 || n%100 == 0 {
				p.logf(float64(readPos.Load())/rate, "warn",
					"復号が追いつかず入力を捨てました（累計 %d 回）", n)
			}
			select {
			case out <- x:
			default:
			}
		}
	}
}

func (p *Pipeline) handleFrames(frames []dec.Frame, nowSec float64) {
	for _, f := range frames {
		p.onFrame(f)
	}
	p.mu.RLock()
	last, open := p.lastActivity, p.current != nil
	p.mu.RUnlock()
	if open && nowSec-last > silenceCloseSec {
		p.closeBroadcast(last, "制御途絶")
	}
	p.mu.RLock()
	tooLong := p.current != nil && nowSec-p.current.StartSec > broadcastMaxS
	p.mu.RUnlock()
	if tooLong {
		p.closeBroadcast(nowSec, "上限超過")
	}
	if len(frames) > 0 {
		p.emitQuality(nowSec)
	}
	p.flushAudio(false)
}

func (p *Pipeline) onFrame(f dec.Frame) {
	fi := FrameInfo{
		TSec:     f.TimeSec,
		Kind:     f.Kind.String(),
		CorrPeak: f.CorrPeak,
		PhaseDeg: f.PhaseDeg,
		SWOK:     true,
		AMRSN:    f.AMRSN,
	}
	msg := f.Message
	if msg == nil {
		msg = f.TCHMessage
	}
	if msg != nil {
		fi.Channel = msg.Header.ChIDName
	}
	fi.CRCOK = f.CRCOK || f.TCHCRCOK

	p.mu.Lock()
	p.lastActivity = f.TimeSec
	p.quality.Frames++
	switch f.Kind {
	case dec.KindSB0:
		p.quality.SB0++
		p.quality.CCHTotal++
		if f.CRCOK {
			p.quality.CCHOK++
		}
	case dec.KindSC:
		p.quality.SC++
		p.quality.TCHTotal++
		if f.TCHCRCOK {
			p.quality.TCHOK++
		}
	}
	p.frames = append(p.frames, fi)
	if len(p.frames) > maxFrames {
		p.frames = p.frames[len(p.frames)-maxFrames:]
	}
	p.signal.TSec = f.TimeSec
	p.signal.CorrPeak = f.CorrPeak
	p.mu.Unlock()
	p.hub.broadcastJSON(ev(EvFrame, fi))

	if msg != nil {
		p.onControl(f, msg)
	}
	if f.Kind == dec.KindSC && f.AMRSN >= 0 && len(f.Voice) > 0 {
		p.onVoice(f)
	} else if f.Kind == dec.KindSC && f.TCHCRCOK && msg != nil &&
		msg.Header.ChID == control.ChIDTCH && f.AMRSN < 0 {
		p.mu.Lock()
		p.voiceRun = 0
		p.mu.Unlock()
	}
}

func (p *Pipeline) onControl(f dec.Frame, m *control.Message) {
	ci := ControlInfo{
		TSec:             f.TimeSec,
		Kind:             f.Kind.String(),
		Channel:          m.Header.ChIDName,
		Type:             m.Header.Type,
		TypeName:         m.Header.TypeName,
		RawHex:           fmt.Sprintf("%X", m.Raw),
		Summary:          m.String(),
		Busy:             m.Header.Busy,
		ManufacturerCode: m.Header.ManufacturerCode,
		ManufacturerName: m.Header.ManufacturerName,
		LicenseeInfo:     m.Header.LicenseeInfo,
		ChSwitchToSC:     m.Header.ChSwitchToSC,
		ChSwitchTiming:   m.Header.ChSwitchTiming,
		AMRSN:            m.Header.AMRWBPlusSN,
	}
	ci.Notify = p.notifyInfo(m)
	ci.Bits = control.Expand(m.Raw, m.Header.Type)

	if isVoiceMsgType(m.Header.Type) {
		p.handleBroadcastControl(f, m)
		return
	}

	p.mu.Lock()
	dup := len(p.controls) > 0 &&
		p.controls[len(p.controls)-1].RawHex == ci.RawHex &&
		ci.TSec-p.controls[len(p.controls)-1].TSec < 3.0
	if !dup {
		p.controls = append(p.controls, ci)
		if len(p.controls) > maxControls {
			p.controls = p.controls[len(p.controls)-maxControls:]
		}
	}
	p.mu.Unlock()
	if !dup {
		p.hub.broadcastJSON(ev(EvControl, ci))
	}

	p.handleBroadcastControl(f, m)
}

func isVoiceMsgType(t int) bool {
	switch t {
	case control.MsgVoiceGroupIndiv, control.MsgVoiceSimultaneous,
		control.MsgVoiceEmergency, control.MsgVoiceContactCall:
		return true
	}
	return false
}

func (p *Pipeline) handleBroadcastControl(f dec.Frame, m *control.Message) {
	switch m.Header.Type {
	case control.MsgNotifyStart:
		p.openBroadcast(f.TimeSec, p.notifyInfo(m), false)
	case control.MsgForcedDisconnect:
		p.closeBroadcast(f.TimeSec, "強制切断指示")
	case control.MsgDisconnect, control.MsgDisconnectFACCH:
		p.closeBroadcast(f.TimeSec, "切断指示")
	}
}

func (p *Pipeline) notifyInfo(m *control.Message) *NotifyInfo {
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

func (p *Pipeline) openBroadcast(tsec float64, n *NotifyInfo, midJoin bool) {
	p.mu.Lock()
	if p.current != nil {
		if n != nil && p.current.CallNo != n.CallNo {
			cur := p.current
			cur.EndSec = tsec
			cur.EndReason = "別の呼の開始指示"
			p.finishLocked(cur)
		} else {
			p.mu.Unlock()
			return
		}
	}
	bc := &BroadcastInfo{ID: p.nextBCID, StartSec: tsec, MidJoin: midJoin, Target: "不明"}
	p.nextBCID++
	if n != nil {
		bc.Target = n.Target
		bc.CallNo = n.CallNo
		bc.Emergency = n.Emergency
	}
	p.current = bc
	p.voiceRun = 0
	out := *bc
	p.mu.Unlock()
	p.iqRec().Start(out.ID, out.Target)
	p.logf(tsec, "info", "通報開始 #%d（%s%s）", out.ID, out.Target,
		map[bool]string{true: " ※途中参加", false: ""}[midJoin])
	p.hub.broadcastJSON(ev(EvBroadcastStart, out))
}

func (p *Pipeline) closeBroadcast(tsec float64, reason string) {
	p.flushAudio(true)
	p.mu.Lock()
	cur := p.current
	if cur == nil {
		p.mu.Unlock()
		return
	}
	cur.EndSec = tsec
	cur.EndReason = reason
	p.finishLocked(cur)
	out := *cur
	p.mu.Unlock()
	p.iqRec().Stop()
	p.logf(tsec, "info", "通報終了 #%d（%s, %.1f 秒, 音声 %.1f 秒）",
		out.ID, reason, out.EndSec-out.StartSec, out.VoiceSeconds)
	p.saveBroadcast(out)
	p.hub.broadcastJSON(ev(EvBroadcastEnd, out))
}

func (p *Pipeline) finishLocked(cur *BroadcastInfo) {
	if pcm := p.audio[cur.ID]; len(pcm) > 0 {
		cur.VoiceSeconds = float64(len(pcm)) / float64(amrwbp.OutputRate)
		cur.AudioURL = fmt.Sprintf("/api/audio/%d.wav", cur.ID)
	}
	if p.iqr.Enabled() {
		cur.IQURL = fmt.Sprintf("/api/iq/%d.wav", cur.ID)
	}
	p.broadcasts = append(p.broadcasts, *cur)
	if len(p.broadcasts) > maxBroadcasts {
		p.broadcasts = p.broadcasts[len(p.broadcasts)-maxBroadcasts:]
	}
	p.current = nil
	p.voiceRun = 0
}

func (p *Pipeline) saveBroadcast(b BroadcastInfo) {
	if !p.lw.Enabled() {
		return
	}
	p.mu.RLock()
	pcm := append([]int16(nil), p.audio[b.ID]...)
	q := p.quality
	src := p.snap.SourceDesc
	var mfr, raw string
	for i := len(p.controls) - 1; i >= 0; i-- {
		c := p.controls[i]
		if c.Notify != nil {
			mfr, raw = c.ManufacturerName, c.RawHex
			break
		}
	}
	p.mu.RUnlock()

	p.mu.RLock()
	scr := p.snap.Scramble
	p.mu.RUnlock()
	extra := map[string]string{
		"start_wall":    p.wallOf(b.StartSec).Format("2006-01-02 15:04:05.000"),
		"end_wall":      p.wallOf(b.EndSec).Format("2006-01-02 15:04:05.000"),
		"source":        src,
		"manufacturer":  mfr,
		"notify_raw":    raw,
		"cch_crc":       fmt.Sprintf("%d/%d", q.CCHOK, q.CCHTotal),
		"tch_crc":       fmt.Sprintf("%d/%d", q.TCHOK, q.TCHTotal),
		"voice_filled":  fmt.Sprintf("%d 枚（%.2f 秒）", q.VoiceFilled, float64(q.VoiceFilled)*0.08),
		"voice_dropped": fmt.Sprintf("%d", q.VoiceDrop),
		"scramble": fmt.Sprintf("0x%04X（%s）", scr.Init,
			map[bool]string{true: "運用者が固定", false: fmt.Sprintf("自動判定 信頼度 %.3f", scr.Confidence)}[scr.Pinned]),
		"municipality": scr.MunicipalityLabel,
	}
	if iq := p.iqRec().Path(b.ID); iq != "" {
		extra["iq_path"] = fmt.Sprintf("%s（%.0f Hz, 2ch int16 = I/Q。`-offset 0` で再デコード可）",
			iq, p.iqRec().SampleRate())
	}
	path, err := p.lw.SaveBroadcast(b, pcm, amrwbp.OutputRate, extra)
	if err != nil {
		p.logf(b.EndSec, "error", "通報 #%d の記録に失敗: %v", b.ID, err)
		return
	}
	if path != "" {
		p.logf(b.EndSec, "info", "通報 #%d を保存しました: %s（詳細は %s.txt）",
			b.ID, path, path)
	}
}

func (p *Pipeline) onVoice(f dec.Frame) {
	p.mu.Lock()
	p.voiceRun++
	needMidJoin := p.current == nil && p.voiceRun >= midJoinFrames
	if p.current != nil {
		p.current.VoiceFrames++
	}
	p.mu.Unlock()
	if needMidJoin {
		p.openBroadcast(f.TimeSec-float64(midJoinFrames)*dsp.FrameDurationSec, nil, true)
	}
	p.asm.Push(f.TimeSec, f.AMRSN, f.Voice)
	sfs := p.asm.Superframes()
	if len(sfs) == 0 {
		return
	}
	p.mu.Lock()
	p.pendSFs = append(p.pendSFs, sfs...)
	p.quality.VoiceSFs += len(sfs)
	p.quality.VoiceDrop = p.asm.Dropped()
	p.quality.VoiceFilled = p.asm.Filled()
	p.quality.VoiceCapped = p.asm.Capped()
	p.mu.Unlock()
}

func (p *Pipeline) flushAudio(force bool) {
	for {
		p.mu.Lock()
		if len(p.pendSFs) == 0 || (!force && len(p.pendSFs) < batchSFs) {
			p.mu.Unlock()
			return
		}
		n := len(p.pendSFs)
		if n > batchSFs {
			n = batchSFs
		}
		batch := append([][]amrwbp.Frame(nil), p.pendSFs[:n]...)
		p.pendSFs = append(p.pendSFs[:0], p.pendSFs[n:]...)
		warm := append([][]amrwbp.Frame(nil), p.warmSFs...)
		curID := 0
		if p.current != nil {
			curID = p.current.ID
		}
		avail := p.snap.AudioAvailable
		p.mu.Unlock()

		if !avail {
			continue
		}
		full := append(warm, batch...)
		pcm, err := amrwbp.Decode(full)
		if err != nil {
			p.logf(0, "error", "音声復号に失敗: %v", err)
			return
		}
		drop := len(warm) * int(0.160*float64(amrwbp.OutputRate))
		if drop > len(pcm) {
			drop = len(pcm)
		}
		out := pcm[drop:]

		p.mu.Lock()
		p.warmSFs = append([][]amrwbp.Frame(nil), batch[max(0, len(batch)-warmUpSFs):]...)
		p.quality.AudioSec += float64(len(out)) / float64(amrwbp.OutputRate)
		if curID > 0 {
			p.audio[curID] = append(p.audio[curID], out...)
		}
		seq := p.audioSeq
		p.audioSeq += int64(len(out))
		p.mu.Unlock()

		p.audioHub.broadcastBinary(pcmFrame(seq, out))
	}
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func (p *Pipeline) emitQuality(nowSec float64) {
	p.mu.Lock()
	el := time.Since(p.started).Seconds()
	p.quality.ElapsedSec = el
	if el > 0 {
		p.quality.Throughput = nowSec / el
	}
	q := p.quality
	p.mu.Unlock()
	p.hub.broadcastJSON(ev(EvQuality, q))
}

func (p *Pipeline) EmitConstellation(c ConstellationInfo) {
	p.mu.Lock()
	p.constell = c
	p.mu.Unlock()
	p.hub.broadcastJSON(ev(EvConstellation, c))
}

func (p *Pipeline) EmitSignal(s SignalInfo) {
	p.mu.Lock()
	s.CorrPeak = p.signal.CorrPeak
	p.signal = s
	p.mu.Unlock()
	p.hub.broadcastJSON(ev(EvSignal, s))
}
