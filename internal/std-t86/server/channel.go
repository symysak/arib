package server

import (
	"fmt"
	"math"
	"sort"
	"sync/atomic"
	"time"

	"github.com/symysak/arib/internal/citycodes"
	"github.com/symysak/arib/internal/rx"
	"github.com/symysak/arib/internal/std-t86/control"
	"github.com/symysak/arib/internal/std-t86/decoder"
	"github.com/symysak/arib/internal/std-t86/fec"
)

const (
	controlResetS = 1.0
	broadcastAbandonS = 5.0
	broadcastMaxS = 15 * 60.0
	lockMinCRC = 3
	chunkSeconds = 0.16
)

type ChannelOpts struct {
	SeedFixed     *int
	MunicipalCode int
	SyncThresh    float64
}

func SeedPtr(v int) *int { return &v }

type channelEnv struct {
	origin     timeOrigin
	sourceDesc string
}

type Channel struct {
	id   int
	mhz  float64
	sink rx.Sink
	cfg  ChannelOpts
	fsIn float64
	t0      float64
	decoder *decoder.StreamingDecoder
	state   *liveState
	audio   *audioWorker
	iqrec   *iqRec
	onClose func(dropped bool)

	cfoResetReq atomic.Bool
	cfoEnableReq atomic.Int32

	seedPinReq atomic.Int32
	seedPin atomic.Int32

	ctrlTotal, ctrlOK atomic.Int64
	procSamples       atomic.Int64
	broadcasting      atomic.Bool

	inCount      int64
	lastQualityT float64
	lastLevelIn  int64
	tchBucket    map[string]int
	tchBucketT   float64
	msgTimes     []float64
	constBest    []decoder.SlotSample
	constT       float64
	lastControlIn int64
	lastValidIn int64
	pend     []complex64
	chunkN   int
	armed    bool
	hadValid bool
	closed   bool
}

const seedPinNone = -2

func (p *Channel) fixedSeed() int {
	switch v := p.seedPin.Load(); {
	case v == seedPinNone:
		if p.cfg.SeedFixed != nil {
			return *p.cfg.SeedFixed
		}
		return decoder.SeedAuto
	case v == decoder.SeedAuto:
		return decoder.SeedAuto
	default:
		return int(v)
	}
}

func newChannel(c rx.ChannelConfig, sink rx.Sink, cfg ChannelOpts, env channelEnv) *Channel {
	if cfg.SyncThresh <= 0 {
		cfg.SyncThresh = 0.6
	}
	cfgSeed := decoder.SeedAuto
	if cfg.SeedFixed != nil {
		cfgSeed = *cfg.SeedFixed
	}
	st := newLiveState(cfgSeed, cfg.MunicipalCode)
	st.f0Hz, st.hasF0 = c.OffHz, true
	if c.MHz > 0 {
		st.centerHz, st.hasCenter = c.MHz*1e6-c.OffHz, true
	}
	st.sourceDesc = env.sourceDesc
	st.origin = env.origin
	st.t = c.StartSec

	p := &Channel{
		id: c.ID, mhz: c.MHz, sink: sink, cfg: cfg, fsIn: c.FsIn, t0: c.StartSec,
		decoder:      decoder.NewStreamingDecoder(c.FsIn, c.OffHz, cfgSeed, cfg.SyncThresh),
		state:        st,
		tchBucket:    map[string]int{},
		lastQualityT: -1.0,
		lastLevelIn:  math.MinInt32,
		chunkN:       max(1, int(c.FsIn*chunkSeconds)),
	}
	p.seedPin.Store(seedPinNone)
	p.seedPinReq.Store(seedPinNone)
	p.audio = newAudioWorker(cfgSeed, st, p.emit,
		func(wid int, pcm []int16) { sink.Audio(wid, pcm, audioSampleRate) })
	p.iqrec = newIQRec(c.FsIn, c.OffHz, c.MHz, sink, p.emit, st, p.audio)
	p.audio.start()
	return p
}

func (p *Channel) emit(ev Event) {
	switch e := ev.(type) {
	case controlMsgEvent, broadcastEvent, audioStatusEvent, iqStatusEvent:
		p.state.addLog(ev)
	case logEvent:
		p.state.addLog(ev)
		p.sink.Log("%.4f MHz: %s", p.mhz, e.Text)
	}
	p.send(ev)
}

func (p *Channel) send(ev Event) { p.sink.Emit(ev) }

func nowMS() int64 { return time.Now().UnixMilli() }

func (p *Channel) nowT() float64 { return p.t0 + float64(p.inCount)/p.fsIn }

func (p *Channel) bbT(pos int) float64 { return p.t0 + float64(pos)/p.decoder.FSBB() }


func (p *Channel) SetSquelch(enabled bool) bool {
	p.decoder.SetSquelchEnabled(enabled)
	p.state.mu.Lock()
	p.state.squelchEnabled = enabled
	p.state.mu.Unlock()
	msg := "電力スケルチを無効化しました（悪条件でも全スロットの復号を試行します）"
	if enabled {
		msg = "電力スケルチを有効化しました"
	}
	p.emit(newLogEvent(p.state.currentT(), msg))
	return enabled
}

func (p *Channel) SetBroadcastStrict(strict bool) bool {
	p.decoder.Broadcast().Strict = strict
	p.state.mu.Lock()
	p.state.broadcastStrict = strict
	p.state.mu.Unlock()
	msg := "通報検出: 反復許容（CRC 不一致でも反復で確定）にしました"
	if strict {
		msg = "通報検出: 厳格（CRC 一致必須）にしました"
	}
	p.emit(newLogEvent(p.state.currentT(), msg))
	return strict
}

func (p *Channel) RequestCFOReset() { p.cfoResetReq.Store(true) }

func (p *Channel) RequestSeedPin(seed int) {
	if seed < 0 || seed >= fec.NSeeds {
		seed = decoder.SeedAuto
	}
	p.seedPinReq.Store(int32(seed))
}

func (p *Channel) SetCFOEnabled(enabled bool) bool {
	if enabled {
		p.cfoEnableReq.Store(1)
	} else {
		p.cfoEnableReq.Store(2)
	}
	p.state.mu.Lock()
	p.state.cfoEnabled = enabled
	p.state.mu.Unlock()
	msg := "CFO 補正を無効化しました（スロット単位の残留補正のみ。外部で同調済みの前提）"
	if enabled {
		msg = "CFO 補正を有効化しました（粗捕捉からやり直します）"
	}
	p.emit(newLogEvent(p.state.currentT(), msg))
	return enabled
}


func (p *Channel) Lock() (bool, float64, string) {
	ok, n := p.ctrlOK.Load(), p.ctrlTotal.Load()
	score := 0.0
	if n > 0 {
		score = float64(ok) / float64(n)
	}
	return ok >= lockMinCRC, score, fmt.Sprintf("制御 CRC16 一致 %d/%d 通", ok, n)
}

func (p *Channel) Counters() rx.Counters {
	return rx.Counters{Frames: int(p.ctrlTotal.Load()), Good: int(p.ctrlOK.Load()),
		ProcSec: float64(p.procSamples.Load()) / p.fsIn}
}

func (p *Channel) Summary() string {
	p.state.mu.Lock()
	seed := p.state.seed
	p.state.mu.Unlock()
	sd := "自動判定中"
	if seed != decoder.SeedAuto {
		sd = fmt.Sprint(seed)
	}
	return fmt.Sprintf("%.4f MHz: seed %s 制御 %d 通（CRC16 一致 %d）", p.mhz, sd, p.ctrlTotal.Load(), p.ctrlOK.Load())
}

func (p *Channel) State() rx.State {
	if p.broadcasting.Load() {
		return rx.StateActive
	}
	return rx.StateIdle
}

func (p *Channel) Snapshot(ext map[string]any) { ext["t86"] = p.state.snapshot() }

func (p *Channel) Retune() { p.cfoResetReq.Store(true) }

func (p *Channel) Resync() {
	p.state.mu.Lock()
	p.state.overflows++
	p.state.mu.Unlock()
}

func (p *Channel) Close(dropped bool) {
	if p.closed {
		return
	}
	p.closed = true
	if len(p.pend) > 0 {
		p.process(p.pend)
		p.pend = nil
	}
	res := p.decoder.Flush()
	p.handleResult(res)
	if w := p.decoder.ResetBroadcast(); w != nil {
		p.finishWindow(w)
	}
	p.state.mu.Lock()
	p.state.syncLocked = false
	p.state.mu.Unlock()
	if dropped {
		p.emit(newLogEvent(p.nowT(), "この局の監視をやめました。"))
	} else {
		p.send(newLogEvent(p.nowT(), "ソース終端に達しました。"))
	}
	p.audio.stop()
	p.iqrec.finalize()
	if p.onClose != nil {
		p.onClose(dropped)
	}
}

func (p *Channel) Feed(wide []complex64) {
	if p.closed {
		return
	}
	p.pend = append(p.pend, wide...)
	off := 0
	for len(p.pend)-off >= p.chunkN {
		p.process(p.pend[off : off+p.chunkN])
		off += p.chunkN
	}
	p.pend = p.pend[:copy(p.pend, p.pend[off:])]
}

func (p *Channel) process(chunk []complex64) {
	if len(chunk) == 0 {
		return
	}
	p.inCount += int64(len(chunk))
	p.procSamples.Store(p.inCount)
	fsIn := p.fsIn
	tIn := p.nowT()
	lvl := p.iqrec.push(chunk)

	if req := p.cfoEnableReq.Swap(0); req != 0 {
		p.decoder.SetCFOEnabled(req == 1)
	}

	if p.cfoResetReq.Swap(false) {
		p.decoder.ReacquireCFO()
		p.emit(newLogEvent(tIn, "CFO を手動で再捕捉しました"))
	}

	if req := p.seedPinReq.Swap(seedPinNone); req != seedPinNone {
		p.applySeedPin(int(req), tIn)
		p.armed, p.hadValid = false, false
		p.lastControlIn = p.inCount
	}

	if p.inCount-p.lastLevelIn >= int64(fsIn*0.2) && !math.IsNaN(lvl) {
		p.lastLevelIn = p.inCount
		p.state.mu.Lock()
		p.state.levelDBFS = &lvl
		p.state.mu.Unlock()
	}

	res := p.decoder.Feed(chunk)
	p.handleResult(res)

	valid := false
	for _, pm := range res.Control {
		if pm.Msg.CRCOK {
			valid = true
			break
		}
	}
	if valid {
		p.lastControlIn = p.inCount
		p.lastValidIn = p.inCount
		p.hadValid = true
	}
	wrongSeed := p.fixedSeed() == decoder.SeedAuto &&
		p.decoder.Seed != decoder.SeedAuto && len(res.Control) > 0
	if (p.hadValid || wrongSeed) && !p.armed {
		p.armed = true
		if !valid {
			p.lastControlIn = p.inCount
		}
	}
	if p.armed && float64(p.inCount-p.lastControlIn)/fsIn >= controlResetS {
		p.resetOnSignalLoss(tIn)
		p.msgTimes = p.msgTimes[:0]
		p.lastControlIn = p.inCount
		p.armed, p.hadValid = false, false
	}

	if w := p.decoder.Broadcast().Open(); w != nil {
		stalled := float64(p.inCount-p.lastValidIn)/fsIn >= broadcastAbandonS
		tooLong := tIn-p.bbT(w.OpenPos) >= broadcastMaxS
		if stalled || tooLong {
			if done := p.decoder.ResetBroadcast(); done != nil {
				if tooLong && !stalled {
					p.emit(newLogEvent(tIn, fmt.Sprintf(
						"通報 #%d が %.0f 分を超えたので打ち切ります"+
							"（強制切断指示を取り逃したとみられます）",
						done.WindowID, broadcastMaxS/60)))
				}
				p.finishWindow(done)
			}
		}
	}

	p.constBest = append(p.constBest, res.Slots...)
	if tIn-p.constT >= 0.2 {
		sort.SliceStable(p.constBest, func(i, j int) bool {
			return p.constBest[i].EVM < p.constBest[j].EVM
		})
		best := p.constBest
		if len(best) > 4 {
			best = best[:4]
		}
		slots := make([][]complex64, len(best))
		for i, s := range best {
			slots[i] = s.Slot
		}
		p.send(constellationEvent{Type: "constellation", T: round(tIn, 1),
			Points: constellationPoints(slots, 600)})
		p.constBest = nil
		p.constT = tIn
	}

	if tIn-p.tchBucketT >= 1.0 {
		if len(p.tchBucket) > 0 {
			counts := make(map[string]int, len(p.tchBucket))
			for k, v := range p.tchBucket {
				counts[k] = v
			}
			p.send(tchSecondEvent{Type: "tch_second", T: round(tIn, 1), Counts: counts})
			p.tchBucket = map[string]int{}
		}
		p.tchBucketT = tIn
	}

	if tIn-p.lastQualityT >= 1.0 {
		p.lastQualityT = tIn
		p.emitQuality(tIn)
	}
}

func (p *Channel) resetOnSignalLoss(tIn float64) {
	p.state.resetControl(p.decoder.Broadcast().Open() == nil)
	p.decoder.ArmMidJoin()
	p.state.mu.Lock()
	p.state.municipalityCode = 0
	p.state.slots.reset()
	p.state.mu.Unlock()
	p.decoder.ReacquireCFO()
	if p.fixedSeed() == decoder.SeedAuto {
		p.decoder.ResetSeed()
		p.state.mu.Lock()
		p.state.seed = decoder.SeedAuto
		p.state.mu.Unlock()
		p.audio.setSeed(decoder.SeedAuto)
	}
	p.emit(newLogEvent(tIn,
		"信号喪失: 制御チャネル状態をリセットしました"+
			"（CFO 再捕捉・スクランブル値・自治体を再探索）"))
}

func (p *Channel) finishWindow(w *decoder.BroadcastWindow) {
	t := p.bbT(w.ClosePos)
	wall := nowMS()
	p.state.broadcastEnded(w.WindowID, t, wall)
	p.emit(broadcastEvent{Type: "broadcast_end", T: round(t, 3),
		WindowID: w.WindowID, WallMS: &wall})
	switch {
	case w.Abandoned:
		p.emit(newLogEvent(t, fmt.Sprintf(
			"通報 #%d を打ち切りました（強制切断指示を受信できないまま中断）",
			w.WindowID)))
	case w.ClosedByKH:
		p.emit(newLogEvent(t, fmt.Sprintf(
			"通報 #%d を拡声通報中フラグの解除で閉じました"+
				"（強制切断指示(0x30) を取り逃したとみられます）", w.WindowID)))
	case w.ClosedByNewCall:
		p.emit(newLogEvent(t, fmt.Sprintf(
			"通報 #%d を次の通報の開始で閉じました"+
				"（強制切断指示(0x30) を取り逃したとみられます）", w.WindowID)))
	}
	p.audio.windowClosed(w.WindowID)
	p.iqrec.windowClosed(w.WindowID)
}

func (p *Channel) handleResult(res decoder.FeedResult) {
	if res.SeedDetected != nil {
		p.onSeedDetected(res.SeedDetected)
	}
	for _, pm := range res.Control {
		t := p.bbT(pm.Pos)
		p.state.addControl(pm.Msg)
		p.state.setT(t)
		p.msgTimes = append(p.msgTimes, t)
		p.ctrlTotal.Add(1)
		if pm.Msg.CRCOK {
			p.ctrlOK.Add(1)
		}
		p.emit(controlMsgEvent{
			Type: "control_msg", T: round(t, 3), Pos: pm.Pos,
			MsgType: pm.Msg.Type, Name: pm.Msg.TypeName, Channel: pm.Msg.Channel,
			CRCOK: pm.Msg.CRCOK, Busy: pm.Msg.Busy, Section: pm.Msg.Section,
			SW: pm.SW, Corr: round(pm.Corr, 3), EVM: round(pm.EVM, 1),
			PowerDB: round(pm.PowerDB, 1),
			RawHex:  pm.Msg.RawHex,
			Fields:  messageFields(pm.Msg),
		})
		if pm.Msg.CRCOK && pm.Msg.Notify != nil && pm.Msg.Notify.MunicipalCode != 0 {
			code := pm.Msg.Notify.MunicipalCode
			p.state.mu.Lock()
			changed := p.state.municipalityCode != code
			p.state.municipalityCode = code
			p.state.mu.Unlock()
			if changed {
				name := pm.Msg.Notify.CityName
				if name == "" {
					name = fmt.Sprintf("コード%d", code)
				}
				p.emit(newLogEvent(t, fmt.Sprintf(
					"★ FACCH 番号通知: 市区町村を確定 — %s（完全コード %d）", name, code)))
			}
		}
	}
	for _, b := range res.TCH {
		p.tchBucket[b.CType]++
		p.state.addTCH(b.CType)
	}
	for _, w := range res.BroadcastStarted {
		t := p.bbT(w.OpenPos)
		wall := nowMS()
		tg := toTarget(w.Target)
		p.state.broadcastStarted(w.WindowID, t, wall, tg)
		p.audio.setWindowTarget(w.WindowID, tg)
		p.iqrec.windowOpened(w.WindowID, tg)
		p.emit(broadcastEvent{Type: "broadcast_start", T: round(t, 3),
			WindowID: w.WindowID, WallMS: &wall, Target: tg})
		if w.MidJoin {
			p.emit(newLogEvent(t, fmt.Sprintf(
				"通報 #%d は途中から受信しました（開始時刻は受信開始時点・報知対象は不明）",
				w.WindowID)))
		}
		if tg != nil {
			p.emit(newLogEvent(t, fmt.Sprintf("通報 #%d 報知対象: %s",
				w.WindowID, tg.Label)))
		}
	}
	for _, w := range res.BroadcastUpdated {
		tg := toTarget(w.Target)
		p.state.setWindowTarget(w.WindowID, tg)
		p.audio.setWindowTarget(w.WindowID, tg)
		p.iqrec.windowUpdated(w.WindowID, tg)
		t := p.state.currentT()
		p.emit(broadcastEvent{Type: "broadcast_update", T: round(t, 3),
			WindowID: w.WindowID, Target: tg})
		if tg != nil {
			p.emit(newLogEvent(t, fmt.Sprintf("通報 #%d 報知対象を確定: %s",
				w.WindowID, tg.Label)))
		}
	}
	for _, v := range res.Voice {
		if !v.IsVoice {
			continue
		}
		if v.WindowID == 0 {
			continue
		}
		p.state.mu.Lock()
		seed := p.state.seed
		p.state.mu.Unlock()
		p.audio.pushBurst(v.WindowID, v.Burst.Bits, v.Burst.Pos, v.Burst.CDist, seed)
	}
	for _, w := range res.BroadcastEnded {
		p.finishWindow(w)
	}
	p.state.mu.Lock()
	if len(res.EVMs) > 0 {
		s := append([]float64(nil), res.EVMs...)
		sort.Float64s(s)
		med := s[len(s)/2]
		if len(s)%2 == 0 {
			med = 0.5 * (s[len(s)/2-1] + s[len(s)/2])
		}
		best := s[0]
		p.state.evmMedian, p.state.evmBest = &med, &best
	}
	if cfo, ok := p.decoder.CFOHz(); ok {
		v := cfo
		p.state.cfoHz = &v
	} else {
		p.state.cfoHz = nil
	}
	p.state.syncLocked = len(res.SWCounts) > 0
	for _, b := range res.Bursts {
		p.state.slots.add(b, p.bbT(b.Pos))
	}
	p.state.mu.Unlock()
	p.broadcasting.Store(p.decoder.Broadcast().Open() != nil)
}

func (p *Channel) applySeedPin(req int, tIn float64) {
	seed := req
	p.seedPin.Store(int32(req))

	p.state.mu.Lock()
	prev := p.state.seed
	p.state.mu.Unlock()
	if seed != prev && prev != decoder.SeedAuto {
		if w := p.decoder.ResetBroadcast(); w != nil {
			p.emit(newLogEvent(tIn, fmt.Sprintf(
				"スクランブル値を手動で変更したので通報 #%d を打ち切ります", w.WindowID)))
			p.finishWindow(w)
		}
	}

	p.handleResult(p.decoder.PinSeed(seed))

	p.state.mu.Lock()
	p.state.seed = seed
	p.state.seedPinned = seed != decoder.SeedAuto
	if seed != decoder.SeedAuto {
		p.state.lastSeed = seed
	}
	p.state.municipalityCode = 0
	p.state.mu.Unlock()
	p.audio.setSeed(seed)

	if seed == decoder.SeedAuto {
		p.emit(newLogEvent(tIn,
			"スクランブル値の手動固定を解除しました（自動判定に戻します）"))
		return
	}
	label := "?"
	if names := candidateNames(control.CandidatesForSeed(seed)); names != "" {
		label = names
	}
	p.emit(newLogEvent(tIn, fmt.Sprintf(
		"スクランブル値を %d に手動固定しました（候補: %s）", seed, label)))
}

func candidateNames(cands []citycodes.Candidate) string {
	names := make([]string, 0, len(cands))
	for _, c := range cands {
		names = append(names, c.Name)
	}
	return joinStrings(names, "、")
}

func (p *Channel) onSeedDetected(info *decoder.SeedInfo) {
	p.state.mu.Lock()
	prev := p.state.lastSeed
	p.state.seed = info.Seed
	p.state.lastSeed = info.Seed
	t := p.state.t
	p.state.mu.Unlock()
	p.audio.setSeed(info.Seed)

	if prev != decoder.SeedAuto && prev != info.Seed {
		if w := p.decoder.ResetBroadcast(); w != nil {
			p.emit(newLogEvent(t, fmt.Sprintf(
				"スクランブル値が %d → %d に変わったので通報 #%d を打ち切ります"+
					"（別の局に移ったとみられます）", prev, info.Seed, w.WindowID)))
			p.finishWindow(w)
		}
	}

	cands := toCandidates(info.Candidates)
	label := "?"
	if names := candidateNames(info.Candidates); names != "" {
		label = names
	}
	p.send(seedDetectedEvent{
		Type: "seed_detected", T: round(t, 1), Seed: info.Seed,
		Score: info.Score, CRCHits: info.CRCHits, Known: info.Known,
		NSlots: info.NSlots, Candidates: cands,
	})
	p.emit(newLogEvent(t, fmt.Sprintf(
		"スクランブル値を自動判定: %d（候補: %s） score=%.0f 既知種別 %d/%d",
		info.Seed, label, info.Score, info.Known, info.NSlots)))
}

func (p *Channel) emitQuality(t float64) {
	cutoff := t - 10.0
	keep := p.msgTimes[:0]
	for _, v := range p.msgTimes {
		if v >= cutoff {
			keep = append(keep, v)
		}
	}
	p.msgTimes = keep

	summary := p.state.controlSummary()
	p.state.mu.Lock()
	p.state.msgsPerS = float64(len(keep)) / 10.0
	if summary.Total > 0 {
		r := float64(summary.CRCOK) / float64(summary.Total)
		p.state.crcOKRate = &r
	} else {
		p.state.crcOKRate = nil
	}
	q := qualityEvent{
		Type: "quality", T: round(t, 1),
		CFOHz: optFloat(p.state.cfoHz, 1), EVMMedian: optFloat(p.state.evmMedian, 1),
		EVMBest: optFloat(p.state.evmBest, 1), CRCOKRate: optFloat(p.state.crcOKRate, 3),
		MsgsPerS: round(p.state.msgsPerS, 1), LevelDBFS: optFloat(p.state.levelDBFS, 1),
		SyncLocked: p.state.syncLocked, Overflows: p.state.overflows,
	}
	p.state.mu.Unlock()

	p.send(q)
	p.send(controlSummaryEvent{Type: "control_summary", T: round(t, 1), Control: summary})
}

func optFloat(v *float64, digits int) nullableFloat {
	if v == nil {
		return noFloat
	}
	return someFloat(*v, digits)
}

func joinStrings(v []string, sep string) string {
	out := ""
	for i, s := range v {
		if i > 0 {
			out += sep
		}
		out += s
	}
	return out
}

func messageFields(m control.Message) map[string]any {
	f := map[string]any{}
	if m.Seed >= 0 {
		f["市区町村コード"] = m.Seed
	}
	if h := m.Header; h != nil {
		f["ビジーフラグ"] = h.Busy
		f["予約(oct2)"] = h.ReservedOct2
		f["SF内フレーム番号"] = h.FrameNo
		f["状況フラグ2"] = h.StatusFlags2
		f["連絡音声通信可否"] = h.VoiceLinkOK
		f["データ伝送可否"] = h.DataTxOK
		f["予備(状況フラグ2)"] = h.StatusFlags2Spare
		f["スーパーフレームのフレーム数"] = h.SuperframeFrames
		f["状況フラグ1"] = h.StatusFlags1
		f["緊急通信可否"] = h.EmergencyOK
		f["緊急通信以外可否"] = h.NonEmergencyOK
		f["通信統制中"] = h.TrafficRestricted
		f["スロット使用状況"] = h.SlotUsage
		f["使用スロット"] = h.UsedSlots
		f["拡声中放送中"] = h.Broadcasting
		f["メディア種別"] = h.Media
		f["伝送制御部プロトコル"] = h.TransProtocol
		f["予約(oct5)"] = h.ReservedOct5
	}
	switch {
	case m.BCCH != nil:
		b := m.BCCH
		f["予備(oct6)"] = b.SpareOct6
		f["報知情報更新番号"] = b.BCCHUpdateNo
		f["親局送信モード"] = b.ParentMode
		f["予約(oct7)"] = b.ReservedOct7
		f["スーパーフレーム長S"] = b.SuperframeLenS
		f["免許人固有情報"] = b.LicenseInfo
		f["PCH数"] = b.NumPCH
		f["PCH前のSCCH数"] = b.NumSCCHBeforePCH
		f["子局識別番号有効ビット数"] = b.IDValidBits
		f["上り折返識別"] = b.UplinkLoopback
		f["緊急連絡通話通信時限"] = b.EmergencyCallLimit
		f["製造者コード"] = b.ManufacturerCode
		f["製造者名"] = b.ManufacturerName
	case m.Broadcast != nil:
		b := m.Broadcast
		if b.HasSplitNo {
			f["分割番号"] = b.SplitNo
		} else {
			f["予備(oct6)"] = b.SpareOct6
		}
		f["呼番号"] = b.CallNo
		f["子局識別番号1"] = b.ID1
		f["子局識別番号2"] = b.ID2
		f["予備(oct11)"] = b.SpareOct11
		f["予約(oct11b4)"] = b.ReservedB4
		f["戸別受信機強制音量"] = b.ForceVolume
		f["音量設定値"] = b.Volume
		f["N2"] = b.N2
		f["N1"] = b.N1
		f["通報開始指示位置"] = b.StartPos
	case m.Release != nil:
		r := m.Release
		f["予備(oct6)"] = r.SpareOct6
		f["呼番号"] = r.CallNo
		f["予備(oct7)"] = r.SpareOct7
		f["切断理由"] = r.Reason
		f["予備(oct8-12)"] = r.SpareTailX
	case m.RadioControl != nil:
		c := m.RadioControl
		if s := m.SyncCtrl; s != nil {
			f["伝送制御情報部"] = s.Raw
			f["予備(伝送制御情報部b8)"] = s.Spare
			f["現スロット番号"] = s.SlotNo
			f["現スロット番号有効"] = s.SlotNoValid
			f["同期バースト残カウンタ制御有無"] = s.RemainCounterCtrl
			f["後続バースト識別"] = s.FollowingBurst
		}
		f["予備(oct1b8)"] = c.Spare0
		f["予備(oct2-4)"] = c.SpareOct2To4X
		f["予備(oct5)"] = c.SpareOct5
		f["有効番号識別子"] = c.ValidNumberID
		f["予約・予備(oct6-8)"] = c.SpareOct6To8X
		f["予備(oct9b8)"] = c.SpareOct9B8
		f["同期バースト残カウンタ"] = c.RemainCounter
		f["関連スロット識別"] = c.RelatedSlotID
		f["関連スロット"] = c.RelatedSlots
		f["予備(oct11-12)"] = c.SpareOct11To12
	case m.Notify != nil:
		n := m.Notify
		f["予備(oct1b8)"] = n.Spare0
		f["予備(oct2-5)"] = n.SpareOct2To5X
		f["予備(oct6)"] = n.SpareOct6
		f["呼番号"] = n.CallNo
		f["子局識別番号"] = n.SubStationID
		f["市区町村コード(完全)"] = n.MunicipalCode
		if n.CityName != "" {
			f["市区町村名"] = n.CityName
		}
		f["製造者コード"] = n.ManufacturerCode
		f["製造者名"] = n.ManufacturerName
		f["免許人固有情報長"] = n.LicenseInfoLen
		f["免許人固有情報"] = n.LicenseInfo
	}
	return f
}
