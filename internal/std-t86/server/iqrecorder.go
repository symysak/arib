package server

import (
	"fmt"
	"math"
	"path/filepath"
	"sync/atomic"
	"time"

	"github.com/symysak/arib/internal/std-t86/dsp"
	"github.com/symysak/arib/internal/std-t86/wavio"
)


const (
	iqTargetFS = 40000.0
	iqPrerollS = 5.0
	iqTailS    = 2.0
	iqMaxRecS  = 600.0
	iqFlushS   = 10.0
)

type iqMsg struct {
	kind   string
	chunk  []complex64
	wid    int
	target *target
}

type iqRecording struct {
	wid        int
	path       string
	parts      []complex64
	sinceWrite int
	tailLeft   int
	hasTail    bool
	note       string
}

type iqRecorder struct {
	fsOut  float64
	logDir string
	emit   func(Event)
	state  *liveState
	sidecarRefresh func(int)

	nco   *dsp.NCO
	decim *dsp.Decimator

	preroll    int
	tail       int
	maxSamples int

	ring []complex64
	rec  *iqRecording
	dropped atomic.Int64

	ch   chan iqMsg
	done chan struct{}
}

func newIQRecorder(fs, f0 float64, logDir string, emit func(Event),
	state *liveState, sidecarRefresh func(int)) *iqRecorder {
	decim := int(math.Floor(fs / iqTargetFS))
	if decim < 1 {
		decim = 1
	}
	fsOut := fs / float64(decim)
	dir := logDir
	if abs, err := filepath.Abs(dir); err == nil {
		dir = abs
	}
	return &iqRecorder{
		fsOut: fsOut, logDir: dir, emit: emit, state: state,
		sidecarRefresh: sidecarRefresh,
		nco:            dsp.NewNCO(f0, fs),
		decim:          dsp.NewDecimator(fs, decim),
		preroll:        int(iqPrerollS * fsOut),
		tail:           int(iqTailS * fsOut),
		maxSamples:     int(iqMaxRecS * fsOut),
		ch:   make(chan iqMsg, 64),
		done: make(chan struct{}),
	}
}

func (r *iqRecorder) start() { go r.run() }

func (r *iqRecorder) stop() {
	close(r.ch)
	<-r.done
}


func (r *iqRecorder) push(chunk []complex64) {
	defer func() { _ = recover() }()
	select {
	case r.ch <- iqMsg{kind: "iq", chunk: chunk}:
	default:
		r.dropped.Add(1)
	}
}

func (r *iqRecorder) windowOpened(wid int, t *target) {
	defer func() { _ = recover() }()
	r.ch <- iqMsg{kind: "open", wid: wid, target: t}
}

func (r *iqRecorder) windowClosed(wid int) {
	defer func() { _ = recover() }()
	r.ch <- iqMsg{kind: "close", wid: wid}
}


func (r *iqRecorder) run() {
	defer close(r.done)
	for m := range r.ch {
		switch m.kind {
		case "iq":
			r.onSamples(m.chunk)
		case "open":
			r.onOpen(m.wid, m.target)
		case "close":
			r.onClose(m.wid)
		}
	}
	if r.rec != nil {
		r.finalize()
	}
}

func (r *iqRecorder) onSamples(chunk []complex64) {
	x := r.decim.Process(r.nco.Mix(chunk))
	if len(x) == 0 {
		return
	}
	if r.rec == nil {
		r.ring = append(r.ring, x...)
		if len(r.ring) > r.preroll {
			r.ring = append(r.ring[:0], r.ring[len(r.ring)-r.preroll:]...)
		}
		return
	}
	rec := r.rec
	rec.parts = append(rec.parts, x...)
	rec.sinceWrite += len(x)
	if len(rec.parts) >= r.maxSamples {
		rec.note = fmt.Sprintf("録音上限 %.0fs 到達", float64(r.maxSamples)/r.fsOut)
		r.finalize()
		return
	}
	if rec.hasTail {
		rec.tailLeft -= len(x)
		if rec.tailLeft <= 0 {
			r.finalize()
			return
		}
	}
	if float64(rec.sinceWrite) >= iqFlushS*r.fsOut {
		r.write(false)
	}
}

func (r *iqRecorder) onOpen(wid int, t *target) {
	if r.rec != nil {
		r.finalize()
	}
	stamp := time.Now().Format("20060102-150405")
	path := filepath.Join(r.logDir,
		fmt.Sprintf("%s_broadcast%d%s_iq.wav", stamp, wid, targetFilenamePart(t)))
	r.rec = &iqRecording{wid: wid, path: path, parts: append([]complex64(nil), r.ring...)}
	r.ring = nil
	r.emit(newLogEvent(r.state.currentT(),
		fmt.Sprintf("通報 #%d の IQ を %s へ保存します（%.0fHz 複素, プリロール %.0fs）",
			wid, path, r.fsOut, float64(r.preroll)/r.fsOut)))
}

func (r *iqRecorder) onClose(wid int) {
	if r.rec == nil || r.rec.wid != wid {
		return
	}
	r.rec.tailLeft = r.tail
	r.rec.hasTail = true
}

func (r *iqRecorder) write(final bool) {
	rec := r.rec
	if rec == nil || len(rec.parts) == 0 {
		return
	}
	rec.sinceWrite = 0
	peak := 0.0
	for _, v := range rec.parts {
		peak = math.Max(peak, math.Max(math.Abs(float64(real(v))), math.Abs(float64(imag(v)))))
	}
	scale := 1.0
	if peak > 0 {
		scale = 32000.0 / peak
	}
	data := make([]int16, 0, len(rec.parts)*2)
	for _, v := range rec.parts {
		data = append(data, int16(float64(real(v))*scale), int16(float64(imag(v))*scale))
	}
	if err := wavio.Write(rec.path, data, 2, int(math.Round(r.fsOut))); err != nil {
		rec.note = fmt.Sprintf("IQ 保存失敗: %v", err)
	}
	note := rec.note
	if n := r.dropped.Load(); n > 0 {
		if note != "" {
			note += " / "
		}
		note += fmt.Sprintf("入力取りこぼし %d チャンク（録音に隙間あり）", n)
	}
	info := iqStatus{
		Path:    rec.path,
		Seconds: round(float64(len(rec.parts))/r.fsOut, 1),
		FS:      round(r.fsOut, 1),
		Done:    final,
		Note:    note,
	}
	r.state.setWindowIQ(rec.wid, info)
	r.emit(iqStatusEvent{
		Type: "iq_status", WindowID: rec.wid, Path: info.Path,
		Seconds: info.Seconds, FS: info.FS, Done: info.Done, Note: info.Note,
	})
}

func (r *iqRecorder) finalize() {
	rec := r.rec
	if rec == nil {
		return
	}
	r.write(true)
	r.rec = nil
	if r.sidecarRefresh != nil {
		r.sidecarRefresh(rec.wid)
	}
}
