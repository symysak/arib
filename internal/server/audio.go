package server

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/symysak/stdt86/internal/citycodes"
	"github.com/symysak/stdt86/internal/g7221"
	"github.com/symysak/stdt86/internal/scodec"
	"github.com/symysak/stdt86/internal/wavio"
)


const (
	batchFrames = 250
	audioSampleRate = 16000
	maxGapFillFrames = 250
)

func targetFilenamePart(t *target) string {
	if t == nil {
		return ""
	}
	switch t.Kind {
	case "all":
		return "_一斉"
	case "selective":
		ids := t.EffectiveIDs
		if len(ids) == 0 {
			ids = t.IDs
		}
		parts := make([]string, len(ids))
		for i, v := range ids {
			parts[i] = fmt.Sprint(v)
		}
		part := "子局" + strings.Join(parts, "-")
		r := []rune(part)
		if len(r) > 24 {
			part = string(r[:24]) + "他"
		}
		return "_" + sanitizeFilename(part)
	}
	return ""
}

func sanitizeFilename(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r == '-' || r == '_' ||
			(r >= '0' && r <= '9') || (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z'),
			r >= 0x4E00 && r <= 0x9FA5,
			r >= 0x3041 && r <= 0x3093,
			r >= 0x30A1 && r <= 0x30F6:
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}
	return b.String()
}

type audioMsg struct {
	kind  string
	wid   int
	bits  []uint8
	pos   int
	cdist int
}

type windowAudio struct {
	pending  [][]uint8
	gaps     *scodec.SlotGapTracker
	conceal  *scodec.Concealer
	pcm      []int16
	wavPath  string
	stats    audioStatus
	staleCnt int

	batchQuality string
	batchFrom    int
}

func qualityRun(entries [][]uint8, fers []uint8, acts []scodec.PLCAction) string {
	b := make([]byte, len(fers))
	for i := range fers {
		lost := i >= len(entries) || entries[i] == nil
		switch {
		case !lost && fers[i] == 0:
			b[i] = 'o'
		case !lost && acts[i] == scodec.PLCMute:
			b[i] = 'X'
		case !lost:
			b[i] = 'x'
		case acts[i] == scodec.PLCMute:
			b[i] = '_'
		default:
			b[i] = '-'
		}
	}
	return string(b)
}

type audioWorker struct {
	seed   int
	state  *liveState
	emit   func(Event)
	pcmSnk func(int, []int16)
	logDir string

	ch   chan audioMsg
	done chan struct{}

	mu      sync.Mutex
	windows map[int]*windowAudio
	targets map[int]*target
	seedMu  sync.Mutex
}

func newAudioWorker(seed int, state *liveState, emit func(Event),
	pcmSink func(int, []int16), logDir string) *audioWorker {
	dir := logDir
	if dir != "" {
		if abs, err := filepath.Abs(dir); err == nil {
			dir = abs
		}
	}
	return &audioWorker{
		seed: seed, state: state, emit: emit, pcmSnk: pcmSink, logDir: dir,
		ch: make(chan audioMsg, 1024), done: make(chan struct{}),
		windows: map[int]*windowAudio{}, targets: map[int]*target{},
	}
}

func (w *audioWorker) LogDir() string { return w.logDir }

func (w *audioWorker) setSeed(seed int) {
	w.seedMu.Lock()
	w.seed = seed
	w.seedMu.Unlock()
}

func (w *audioWorker) currentSeed() int {
	w.seedMu.Lock()
	defer w.seedMu.Unlock()
	return w.seed
}

func (w *audioWorker) start() { go w.run() }

func (w *audioWorker) stop() {
	close(w.ch)
	<-w.done
}


func (w *audioWorker) pushBurst(wid int, bits []uint8, pos, cdist int) {
	w.send(audioMsg{kind: "burst", wid: wid, bits: bits, pos: pos, cdist: cdist})
}

func (w *audioWorker) windowClosed(wid int) { w.send(audioMsg{kind: "close", wid: wid}) }

func (w *audioWorker) refreshSidecar(wid int) { w.send(audioMsg{kind: "sidecar", wid: wid}) }

func (w *audioWorker) send(m audioMsg) {
	defer func() { _ = recover() }()
	w.ch <- m
}

func (w *audioWorker) setWindowTarget(wid int, t *target) {
	w.mu.Lock()
	w.targets[wid] = t
	w.mu.Unlock()
}


func (w *audioWorker) run() {
	defer close(w.done)
	for m := range w.ch {
		switch m.kind {
		case "burst":
			wa := w.window(m.wid)
			missing, ok := wa.gaps.Step(m.pos)
			if !ok {
				wa.staleCnt++
				continue
			}
			if missing >= maxGapFillFrames {
				w.emit(newLogEvent(w.state.currentT(), fmt.Sprintf(
					"通報 #%d: 音声が長時間途絶えたため補間を %.1f 秒で打ち切りました"+
						"（以降の時間軸は詰まります）",
					m.wid, float64(maxGapFillFrames)*0.02)))
			}
			for i := 0; i < missing; i++ {
				wa.pending = append(wa.pending, nil)
			}
			wa.pending = append(wa.pending, m.bits)
			wa.stats.cdistSum += m.cdist
			if m.cdist > wa.stats.CDistMax {
				wa.stats.CDistMax = m.cdist
			}
			if m.cdist > 0 {
				wa.stats.CDistBad++
			}
			if len(wa.pending) >= batchFrames {
				w.decodeBatch(m.wid, wa)
			}
		case "close":
			w.decodeBatch(m.wid, w.window(m.wid))
		case "sidecar":
			wa := w.window(m.wid)
			if wa.wavPath != "" {
				_ = w.writeSidecar(m.wid, wa)
			}
		}
	}
	w.mu.Lock()
	ids := make([]int, 0, len(w.windows))
	for id := range w.windows {
		ids = append(ids, id)
	}
	w.mu.Unlock()
	for _, id := range ids {
		w.decodeBatch(id, w.window(id))
	}
}

func (w *audioWorker) window(wid int) *windowAudio {
	w.mu.Lock()
	defer w.mu.Unlock()
	wa, ok := w.windows[wid]
	if !ok {
		wa = &windowAudio{
			gaps:    scodec.NewSlotGapTracker(0, maxGapFillFrames),
			conceal: &scodec.Concealer{},
		}
		w.windows[wid] = wa
	}
	return wa
}

func (w *audioWorker) decodeBatch(wid int, wa *windowAudio) {
	entries := wa.pending
	wa.pending = nil
	if len(entries) == 0 {
		return
	}
	nReal := 0
	for _, e := range entries {
		if e != nil {
			nReal++
		}
	}
	wa.stats.Frames += nReal
	wa.stats.Filled += len(entries) - nReal
	wa.stats.Stale = wa.staleCnt
	wa.batchQuality, wa.batchFrom = "", len(wa.stats.Quality)

	note := ""
	seed := w.currentSeed()
	if seed == 0 {
		note = "スクランブル値が未確定のため音声デコードを保留しました"
	} else if err := w.decodeAndAppend(wid, wa, entries, seed); err != nil {
		note = fmt.Sprintf("デコード失敗: %v", err)
	} else if wa.stats.CRC7OK < wa.stats.Frames/2 {
		note = "CRC7 不一致多数 — 受信品質が低い可能性があります"
	}

	if err := w.writeWAV(wid, wa); err != nil {
		if note != "" {
			note += " / "
		}
		note += fmt.Sprintf("WAV 保存失敗: %v", err)
	}
	wa.stats.Note = note
	wa.stats.WavPath = wa.wavPath
	if wa.stats.Frames > 0 {
		wa.stats.CRC7Rate = round(float64(wa.stats.CRC7OK)/float64(wa.stats.Frames), 4)
		wa.stats.CDistMean = round(float64(wa.stats.cdistSum)/float64(wa.stats.Frames), 2)
	}

	w.state.setAudioStatus(wid, wa.stats)
	w.emit(audioStatusEvent{
		Type: "audio_status", WindowID: wid,
		Frames: wa.stats.Frames, CRC7OK: wa.stats.CRC7OK, CRC7Fail: wa.stats.CRC7Fail,
		CRC7Rate: wa.stats.CRC7Rate, Filled: wa.stats.Filled, Stale: wa.stats.Stale,
		PLCRepeat: wa.stats.PLCRepeat, PLCMute: wa.stats.PLCMute,
		CDistMax: wa.stats.CDistMax, CDistMean: wa.stats.CDistMean,
		CDistBad:        wa.stats.CDistBad,
		DecodedSeconds:  round(wa.stats.DecodedSeconds, 1),
		DecodeAttempted: wa.stats.DecodeAttempted,
		Note:            note, WavPath: wa.wavPath,
		Quality: wa.batchQuality, QualityFrom: wa.batchFrom,
	})
}

func (w *audioWorker) decodeAndAppend(wid int, wa *windowAudio,
	entries [][]uint8, seed int) error {
	frames, fers, err := scodec.DecodeTCHFramesGapped(entries, seed)
	if err != nil {
		return err
	}
	for i, f := range fers {
		if entries[i] == nil {
			continue
		}
		if f == 0 {
			wa.stats.CRC7OK++
		} else {
			wa.stats.CRC7Fail++
		}
	}
	concealed, acts := wa.conceal.ApplyTraced(frames, fers)
	wa.batchQuality = qualityRun(entries, fers, acts)
	wa.batchFrom = len(wa.stats.Quality)
	if wid >= 0 {
		wa.stats.Quality += wa.batchQuality
	}
	for _, a := range acts {
		switch a {
		case scodec.PLCRepeat:
			wa.stats.PLCRepeat++
		case scodec.PLCMute:
			wa.stats.PLCMute++
		}
	}
	pcm, err := g7221.Decode(concealed, true)
	if err != nil {
		return err
	}
	wa.stats.DecodeAttempted = true
	wa.stats.DecodedSeconds += float64(len(pcm)) / audioSampleRate

	pcm16 := make([]int16, len(pcm))
	for i, v := range pcm {
		if v > 1 {
			v = 1
		} else if v < -1 {
			v = -1
		}
		pcm16[i] = int16(v * 32767)
	}
	if wid >= 0 {
		w.mu.Lock()
		wa.pcm = append(wa.pcm, pcm16...)
		w.mu.Unlock()
	}
	sinkID := wid
	if sinkID < 0 {
		sinkID = 0
	}
	w.pcmSnk(sinkID, pcm16)
	return nil
}

func (w *audioWorker) writeWAV(wid int, wa *windowAudio) error {
	if w.logDir == "" {
		return nil
	}
	w.mu.Lock()
	pcm := append([]int16(nil), wa.pcm...)
	w.mu.Unlock()
	if len(pcm) == 0 {
		return nil
	}
	if wa.wavPath == "" {
		w.mu.Lock()
		part := targetFilenamePart(w.targets[wid])
		w.mu.Unlock()
		stamp := time.Now().Format("20060102-150405")
		wa.wavPath = filepath.Join(w.logDir,
			fmt.Sprintf("%s_broadcast%d%s.wav", stamp, wid, part))
		w.emit(newLogEvent(w.state.currentT(),
			fmt.Sprintf("通報 #%d の音声を %s へ保存します", wid, wa.wavPath)))
	}
	if err := wavio.Write(wa.wavPath, pcm, 1, audioSampleRate); err != nil {
		return err
	}
	return w.writeSidecar(wid, wa)
}

func (w *audioWorker) writeSidecar(wid int, wa *windowAudio) error {
	if wa.wavPath == "" {
		return nil
	}
	w.mu.Lock()
	tg := w.targets[wid]
	w.mu.Unlock()
	win := w.state.windowInfoCopy(wid)

	wall := func(ms *int64) string {
		if ms == nil {
			return "—"
		}
		return time.UnixMilli(*ms).Format("2006-01-02 15:04:05")
	}

	var b strings.Builder
	fmt.Fprintf(&b, "通報 #%d\n\n", wid)
	if tg != nil {
		fmt.Fprintf(&b, "報知対象: %s\n", tg.Label)
		fmt.Fprintf(&b, "  種別: %s (all=一斉 / selective=群・個別 / unknown=不明)\n", tg.Kind)
		fmt.Fprintf(&b, "  子局識別番号(生値): %s\n", joinInts(tg.IDs))
		if len(tg.EffectiveIDs) > 0 && (tg.ValidBits != nil || !sameInts(tg.EffectiveIDs, tg.IDs)) {
			line := "  子局識別番号(マスク後): " + joinInts(tg.EffectiveIDs)
			if tg.ValidBits != nil {
				line += fmt.Sprintf("（有効ビット数 %d）", *tg.ValidBits)
			}
			b.WriteString(line + "\n")
		}
		if tg.CallNo != nil {
			fmt.Fprintf(&b, "  呼番号: %d\n", *tg.CallNo)
		}
		if tg.Note != "" {
			fmt.Fprintf(&b, "  注記: %s\n", tg.Note)
		}
		b.WriteString("  ※ 一斉の判定はマスク後 全0 のみ" +
			"（§2.5 番号計画: 全0=呼出先指定なし）。その他=群/個別呼出\n")
	} else {
		b.WriteString("報知対象: 不明（通報開始指示から取得できず）\n")
	}
	w.state.mu.Lock()
	code := w.state.municipalityCode
	if code == 0 {
		code = w.state.municipalCode
	}
	w.state.mu.Unlock()
	if code != 0 {
		name, _ := citycodes.Name(code)
		if name == "" {
			name = "?"
		}
		fmt.Fprintf(&b, "市区町村: %s（コード %d）\n", name, code)
	}
	var wallStart, wallEnd *int64
	var iq *iqStatus
	if win != nil {
		wallStart, wallEnd, iq = win.WallStart, win.WallEnd, win.IQ
	}
	fmt.Fprintf(&b, "開始（受信機実時刻）: %s\n", wall(wallStart))
	fmt.Fprintf(&b, "終了（受信機実時刻）: %s\n\n", wall(wallEnd))
	fmt.Fprintf(&b, "音声フレーム: %d / CRC7一致: %d / 欠落補間: %d\n",
		wa.stats.Frames, wa.stats.CRC7OK, wa.stats.Filled)
	fmt.Fprintf(&b, "  CRC7 不一致: %d（一致率 %.2f%% — 分母は受信フレームのみ。"+
		"欠落補間は含まない）\n", wa.stats.CRC7Fail, wa.stats.CRC7Rate*100)
	fmt.Fprintf(&b, "  PLC: 直前フレーム反復 %d / 無音置換 %d（無音置換 = 長区間の欠落）\n",
		wa.stats.PLCRepeat, wa.stats.PLCMute)
	if wa.stats.Stale > 0 {
		fmt.Fprintf(&b, "  位置逆行で破棄したバースト: %d\n", wa.stats.Stale)
	}
	fmt.Fprintf(&b, "  C 種別ハミング距離: 最大 %d / 平均 %.2f / 不一致 %d バースト"+
		"（0 = 判定コードと完全一致）\n",
		wa.stats.CDistMax, wa.stats.CDistMean, wa.stats.CDistBad)
	fmt.Fprintf(&b, "デコード秒数: %.1f\n", wa.stats.DecodedSeconds)
	if wa.stats.Quality != "" {
		fmt.Fprintf(&b, "フレーム品質 (o=CRC7一致 x=不一致→反復 X=不一致→無音 "+
			"-=欠落→反復 _=欠落→無音, 1文字=20ms):\n")
		for i := 0; i < len(wa.stats.Quality); i += 100 {
			end := i + 100
			if end > len(wa.stats.Quality) {
				end = len(wa.stats.Quality)
			}
			fmt.Fprintf(&b, "  %6d %s\n", i, wa.stats.Quality[i:end])
		}
	}
	fmt.Fprintf(&b, "WAV: %s\n", wa.wavPath)
	if iq != nil {
		fmt.Fprintf(&b, "IQ 録音: %s（%.1fs, %.0fHz 複素, オフセット0で再デコード可）\n",
			iq.Path, iq.Seconds, iq.FS)
		if iq.Note != "" {
			fmt.Fprintf(&b, "  注記: %s\n", iq.Note)
		}
	}
	return os.WriteFile(wa.wavPath+".txt", []byte(b.String()), 0o644)
}

func joinInts(v []int) string {
	if len(v) == 0 {
		return "—"
	}
	parts := make([]string, len(v))
	for i, x := range v {
		parts[i] = fmt.Sprint(x)
	}
	return strings.Join(parts, "、")
}

func sameInts(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func (w *audioWorker) windowPCM(wid int) []int16 {
	w.mu.Lock()
	wa, ok := w.windows[wid]
	w.mu.Unlock()
	if !ok || len(wa.pcm) == 0 {
		return nil
	}
	return append([]int16(nil), wa.pcm...)
}
