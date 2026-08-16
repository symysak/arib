package server

import (
	"fmt"
	"io"
	"math"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/symysak/stdt86/internal/citycodes"
	"github.com/symysak/stdt86/internal/control"
	"github.com/symysak/stdt86/internal/decoder"
	"github.com/symysak/stdt86/internal/iq"
)

const (
	chunkSeconds = 0.16
	sampleQueueChunks = 32
	controlResetS = 1.0
	broadcastAbandonS = 5.0
	broadcastMaxS = 15 * 60.0
	inputStallS = 2.0

	eventBuffer = 8192
	pcmBuffer   = 256
)

type PCMFrame struct {
	WindowID int
	PCM      []int16
}

type Config struct {
	F0Hz          float64
	Seed          int
	MunicipalCode int
	SyncThresh    float64
	LogDir        string
	SourceDesc    string
	SourcePath string
}

type Pipeline struct {
	source  iq.Source
	decoder *decoder.StreamingDecoder
	state   *liveState
	audio   *audioWorker
	iqrec   *iqRecorder
	cfg     Config

	eventCh chan Event
	pcmCh   chan PCMFrame
	sampCh  chan []complex64

	stop     chan struct{}
	stopOnce sync.Once
	wg       sync.WaitGroup
	finished chan struct{}

	cfoResetReq atomic.Bool
	cfoEnableReq atomic.Int32

	seedPinReq atomic.Int32
	seedPin atomic.Int32

	chunk int
}

const seedPinAuto = -1

func (p *Pipeline) fixedSeed() int {
	switch v := p.seedPin.Load(); {
	case v > 0:
		return int(v)
	case v == seedPinAuto:
		return 0
	default:
		return p.cfg.Seed
	}
}

func NewPipeline(source iq.Source, cfg Config) *Pipeline {
	if cfg.SyncThresh <= 0 {
		cfg.SyncThresh = 0.6
	}
	st := newLiveState(cfg.Seed, cfg.MunicipalCode)
	st.f0Hz, st.hasF0 = cfg.F0Hz, true
	if c, ok := source.CenterHz(); ok {
		st.centerHz, st.hasCenter = c, true
	}
	st.sourceDesc = cfg.SourceDesc
	if cfg.SourcePath != "" {
		st.origin = fileTimeOrigin(cfg.SourcePath)
	} else {
		st.origin = liveTimeOrigin()
	}

	p := &Pipeline{
		source:   source,
		decoder:  decoder.NewStreamingDecoder(source.SampleRate(), cfg.F0Hz, cfg.Seed, cfg.SyncThresh),
		state:    st,
		cfg:      cfg,
		eventCh:  make(chan Event, eventBuffer),
		pcmCh:    make(chan PCMFrame, pcmBuffer),
		sampCh:   make(chan []complex64, sampleQueueChunks),
		stop:     make(chan struct{}),
		finished: make(chan struct{}),
	}
	p.chunk = int(source.SampleRate() * chunkSeconds)
	if p.chunk < 1 {
		p.chunk = 1
	}
	p.audio = newAudioWorker(cfg.Seed, st, p.emit,
		func(wid int, pcm []int16) { p.sendPCM(PCMFrame{wid, pcm}) }, cfg.LogDir)
	if cfg.LogDir != "" {
		p.iqrec = newIQRecorder(source.SampleRate(), cfg.F0Hz, cfg.LogDir,
			p.emit, st, p.audio.refreshSidecar)
	}
	return p
}

func (p *Pipeline) Events() <-chan Event { return p.eventCh }

func (p *Pipeline) PCM() <-chan PCMFrame { return p.pcmCh }

func (p *Pipeline) Finished() <-chan struct{} { return p.finished }

func (p *Pipeline) AudioLogDir() string { return p.audio.LogDir() }

func (p *Pipeline) Start() {
	p.audio.start()
	if p.iqrec != nil {
		p.iqrec.start()
	}
	p.wg.Add(2)
	go p.readLoop()
	go p.dspLoop()
}

func (p *Pipeline) Stop() {
	p.stopOnce.Do(func() {
		close(p.stop)
		p.source.Close()
		p.wg.Wait()
		p.audio.stop()
		if p.iqrec != nil {
			p.iqrec.stop()
		}
	})
}

func (p *Pipeline) emit(ev Event) {
	switch ev.(type) {
	case controlMsgEvent, broadcastEvent, audioStatusEvent, iqStatusEvent, logEvent:
		p.state.addLog(ev)
	}
	p.send(ev)
}

func (p *Pipeline) send(ev Event) {
	select {
	case p.eventCh <- ev:
	default:
	}
}

func (p *Pipeline) sendPCM(f PCMFrame) {
	select {
	case p.pcmCh <- f:
	default:
	}
}

func nowMS() int64 { return time.Now().UnixMilli() }


func (p *Pipeline) SetSquelch(enabled bool) bool {
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

func (p *Pipeline) SetBroadcastStrict(strict bool) bool {
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

func (p *Pipeline) RequestCFOReset() { p.cfoResetReq.Store(true) }

func (p *Pipeline) RequestSeedPin(seed int) {
	if seed <= 0 {
		seed = seedPinAuto
	}
	p.seedPinReq.Store(int32(seed))
}

func (p *Pipeline) SetCFOEnabled(enabled bool) bool {
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


func (p *Pipeline) readLoop() {
	defer p.wg.Done()
	defer close(p.sampCh)

	stalled := false
	lastData := time.Now()
	for {
		select {
		case <-p.stop:
			return
		default:
		}
		chunk, err := p.source.Read(p.chunk)
		if err == io.EOF {
			return
		}
		if len(chunk) == 0 {
			if !stalled && time.Since(lastData) >= inputStallS*time.Second {
				stalled = true
				p.emit(newLogEvent(p.state.currentT(),
					"入力が途絶えました（接続は維持。SDR# 側の再生停止など）。"+
						"データ再開を待機します。"))
			}
			continue
		}
		if stalled {
			stalled = false
			p.emit(newLogEvent(p.state.currentT(), "入力が復帰しました。"))
		}
		lastData = time.Now()

		if !p.source.Lossy() {
			select {
			case p.sampCh <- chunk:
			case <-p.stop:
				return
			}
			continue
		}
		select {
		case p.sampCh <- chunk:
		default:
			select {
			case <-p.sampCh:
			default:
			}
			p.state.mu.Lock()
			p.state.overflows++
			p.state.mu.Unlock()
			select {
			case p.sampCh <- chunk:
			default:
			}
		}
	}
}


func (p *Pipeline) dspLoop() {
	defer p.wg.Done()
	defer close(p.finished)

	fsIn := p.source.SampleRate()
	var inCount int64
	nowT := func() float64 { return float64(inCount) / fsIn }

	lastQualityT := -1.0
	var lastLevelIn int64 = math.MinInt32
	tchBucket := map[string]int{}
	tchBucketT := 0.0
	var msgTimes []float64
	var constBest []decoder.SlotSample
	constT := 0.0
	var lastControlIn int64
	var lastValidIn int64
	armed := false
	hadValid := false

	for chunk := range p.sampCh {
		inCount += int64(len(chunk))
		tIn := nowT()
		if p.iqrec != nil {
			p.iqrec.push(chunk)
		}

		if req := p.cfoEnableReq.Swap(0); req != 0 {
			p.decoder.SetCFOEnabled(req == 1)
		}

		if p.cfoResetReq.Swap(false) {
			p.decoder.ReacquireCFO()
			p.emit(newLogEvent(tIn, "CFO を手動で再捕捉しました"))
		}

		if req := p.seedPinReq.Swap(0); req != 0 {
			p.applySeedPin(int(req), tIn, &msgTimes, tchBucket)
			armed, hadValid = false, false
			lastControlIn = inCount
		}

		if inCount-lastLevelIn >= int64(fsIn*0.2) {
			lastLevelIn = inCount
			var pw float64
			for _, v := range chunk {
				pw += float64(real(v))*float64(real(v)) + float64(imag(v))*float64(imag(v))
			}
			lvl := 10.0 * math.Log10(pw/float64(len(chunk))+1e-20)
			p.state.mu.Lock()
			p.state.levelDBFS = &lvl
			p.state.mu.Unlock()
		}

		res := p.decoder.Feed(chunk)
		p.handleResult(res, &msgTimes, tchBucket)

		valid := false
		for _, pm := range res.Control {
			if pm.Msg.CRCOK {
				valid = true
				break
			}
		}
		if valid {
			lastControlIn = inCount
			lastValidIn = inCount
			hadValid = true
		}
		wrongSeed := p.fixedSeed() == 0 && p.decoder.Seed != 0 && len(res.Control) > 0
		if (hadValid || wrongSeed) && !armed {
			armed = true
			if !valid {
				lastControlIn = inCount
			}
		}
		if armed && float64(inCount-lastControlIn)/fsIn >= controlResetS {
			p.resetOnSignalLoss(tIn)
			msgTimes = msgTimes[:0]
			lastControlIn = inCount
			armed, hadValid = false, false
		}

		if w := p.decoder.Broadcast().Open(); w != nil {
			stalled := float64(inCount-lastValidIn)/fsIn >= broadcastAbandonS
			tooLong := tIn-float64(w.OpenPos)/p.decoder.FSBB() >= broadcastMaxS
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

		constBest = append(constBest, res.Slots...)
		if tIn-constT >= 0.2 {
			sort.SliceStable(constBest, func(i, j int) bool {
				return constBest[i].EVM < constBest[j].EVM
			})
			best := constBest
			if len(best) > 4 {
				best = best[:4]
			}
			slots := make([][]complex64, len(best))
			for i, s := range best {
				slots[i] = s.Slot
			}
			p.send(constellationEvent{Type: "constellation", T: round(tIn, 1),
				Points: constellationPoints(slots, 600)})
			constBest = nil
			constT = tIn
		}

		if tIn-tchBucketT >= 1.0 {
			if len(tchBucket) > 0 {
				counts := make(map[string]int, len(tchBucket))
				for k, v := range tchBucket {
					counts[k] = v
				}
				p.send(tchSecondEvent{Type: "tch_second", T: round(tIn, 1), Counts: counts})
				tchBucket = map[string]int{}
			}
			tchBucketT = tIn
		}

		if tIn-lastQualityT >= 1.0 {
			lastQualityT = tIn
			p.emitQuality(tIn, &msgTimes)
		}
	}

	res := p.decoder.Flush()
	p.handleResult(res, &msgTimes, tchBucket)
	if w := p.decoder.ResetBroadcast(); w != nil {
		p.finishWindow(w)
	}
	p.state.mu.Lock()
	p.state.syncLocked = false
	p.state.mu.Unlock()
	p.send(newLogEvent(nowT(), "ソース終端に達しました。"))
}

func (p *Pipeline) resetOnSignalLoss(tIn float64) {
	p.state.resetControl(p.decoder.Broadcast().Open() == nil)
	p.decoder.ArmMidJoin()
	p.state.mu.Lock()
	p.state.municipalityCode = 0
	p.state.mu.Unlock()
	p.decoder.ReacquireCFO()
	if p.fixedSeed() == 0 {
		p.decoder.ResetSeed()
		p.state.mu.Lock()
		p.state.seed = 0
		p.state.mu.Unlock()
		p.audio.setSeed(0)
	}
	p.emit(newLogEvent(tIn,
		"信号喪失: 制御チャネル状態をリセットしました"+
			"（CFO 再捕捉・スクランブル値・自治体を再探索）"))
}

func (p *Pipeline) finishWindow(w *decoder.BroadcastWindow) {
	t := float64(w.ClosePos) / p.decoder.FSBB()
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
	if p.iqrec != nil {
		p.iqrec.windowClosed(w.WindowID)
	}
}

func (p *Pipeline) handleResult(res decoder.FeedResult, msgTimes *[]float64,
	tchBucket map[string]int) {
	fsBB := p.decoder.FSBB()
	if res.SeedDetected != nil {
		p.onSeedDetected(res.SeedDetected)
	}
	for _, pm := range res.Control {
		t := float64(pm.Pos) / fsBB
		p.state.addControl(pm.Msg)
		p.state.setT(t)
		*msgTimes = append(*msgTimes, t)
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
		tchBucket[b.CType]++
		p.state.addTCH(b.CType)
	}
	for _, w := range res.BroadcastStarted {
		t := float64(w.OpenPos) / fsBB
		wall := nowMS()
		tg := toTarget(w.Target)
		p.state.broadcastStarted(w.WindowID, t, wall, tg)
		p.audio.setWindowTarget(w.WindowID, tg)
		if p.iqrec != nil {
			p.iqrec.windowOpened(w.WindowID, tg)
		}
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
		p.audio.pushBurst(v.WindowID, v.Burst.Bits, v.Burst.Pos, v.Burst.CDist)
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
	p.state.mu.Unlock()
}

func (p *Pipeline) applySeedPin(req int, tIn float64, msgTimes *[]float64,
	tchBucket map[string]int) {
	seed := req
	if seed == seedPinAuto {
		seed = 0
	}
	p.seedPin.Store(int32(req))

	p.state.mu.Lock()
	prev := p.state.seed
	p.state.mu.Unlock()
	if seed != prev && prev != 0 {
		if w := p.decoder.ResetBroadcast(); w != nil {
			p.emit(newLogEvent(tIn, fmt.Sprintf(
				"スクランブル値を手動で変更したので通報 #%d を打ち切ります", w.WindowID)))
			p.finishWindow(w)
		}
	}

	p.handleResult(p.decoder.PinSeed(seed), msgTimes, tchBucket)

	p.state.mu.Lock()
	p.state.seed = seed
	p.state.seedPinned = seed != 0
	if seed != 0 {
		p.state.lastSeed = seed
	}
	p.state.municipalityCode = 0
	p.state.mu.Unlock()
	p.audio.setSeed(seed)

	if seed == 0 {
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

func (p *Pipeline) onSeedDetected(info *decoder.SeedInfo) {
	p.state.mu.Lock()
	prev := p.state.lastSeed
	p.state.seed = info.Seed
	p.state.lastSeed = info.Seed
	t := p.state.t
	p.state.mu.Unlock()
	p.audio.setSeed(info.Seed)

	if prev != 0 && prev != info.Seed {
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

func (p *Pipeline) emitQuality(t float64, msgTimes *[]float64) {
	cutoff := t - 10.0
	keep := (*msgTimes)[:0]
	for _, v := range *msgTimes {
		if v >= cutoff {
			keep = append(keep, v)
		}
	}
	*msgTimes = keep

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
	if m.Seed != 0 {
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
