package server

import (
	"fmt"
	"math"
	"path/filepath"

	"github.com/symysak/arib/internal/rec"
	"github.com/symysak/arib/internal/rx"
	"github.com/symysak/arib/internal/std-t86/dsp"
)


const (
	iqTargetFS = 40000.0
	IQPrerollS = 5.0
	iqTailS    = 2.0
	iqMaxRecS  = 600.0
	iqReportS  = 10.0
)

type iqRecording struct {
	wid      int
	base     string
	samples  int
	reported int
	tailLeft int
	hasTail  bool
	note     string
}

type iqRec struct {
	fsOut  float64
	mhz    float64
	sink   rx.Sink
	emit   func(Event)
	state  *liveState
	audio  *audioWorker
	nco    *dsp.NCO
	decim  *dsp.Decimator
	tail   int
	maxS   int
	fed    int
	rec    *iqRecording
	prerol float64
}

func newIQRec(fs, f0, mhz float64, sink rx.Sink, emit func(Event), state *liveState, audio *audioWorker) *iqRec {
	decim := int(math.Floor(fs / iqTargetFS))
	if decim < 1 {
		decim = 1
	}
	fsOut := fs / float64(decim)
	return &iqRec{
		fsOut: fsOut, mhz: mhz, sink: sink, emit: emit, state: state, audio: audio,
		nco:    dsp.NewNCO(f0, fs),
		decim:  dsp.NewDecimator(fs, decim),
		tail:   int(iqTailS * fsOut),
		maxS:   int(iqMaxRecS * fsOut),
		prerol: IQPrerollS,
	}
}

func (r *iqRec) push(chunk []complex64) float64 {
	x := r.decim.Process(r.nco.Mix(chunk))
	if len(x) == 0 {
		return math.NaN()
	}
	r.sink.IQ(x, r.fsOut)
	r.fed += len(x)
	var pw float64
	for _, v := range x {
		pw += float64(real(v))*float64(real(v)) + float64(imag(v))*float64(imag(v))
	}
	lvl := 10.0 * math.Log10(pw/float64(len(x))+1e-20)
	rec := r.rec
	if rec == nil {
		return lvl
	}
	rec.samples += len(x)
	switch {
	case rec.samples >= r.maxS:
		rec.note = fmt.Sprintf("録音上限 %.0fs 到達", float64(r.maxS)/r.fsOut)
		r.finalize()
	case rec.hasTail:
		if rec.tailLeft -= len(x); rec.tailLeft <= 0 {
			r.finalize()
		}
	case float64(rec.samples-rec.reported) >= iqReportS*r.fsOut:
		r.report(false)
	}
	return lvl
}

func iqLabel(wid int, t *target) string {
	return fmt.Sprintf("broadcast%d%s", wid, targetFilenamePart(t))
}

func (r *iqRec) windowOpened(wid int, t *target) {
	if r.rec != nil {
		r.finalize()
	}
	base := r.sink.RecOpen(wid, iqLabel(wid, t), 0)
	if base == "" {
		r.emit(newLogEvent(r.state.currentT(), fmt.Sprintf("通報 #%d の録音を開けませんでした", wid)))
		r.audio.setWindowBase(wid, "")
		return
	}
	pre := min(r.fed, int(r.prerol*r.fsOut))
	r.rec = &iqRecording{wid: wid, base: base, samples: pre}
	r.audio.setWindowBase(wid, base)
	r.sink.RecNote(wid, fmt.Sprintf("STD-T86 通報 #%d（%.4f MHz）", wid, r.mhz))
	if t != nil {
		r.sink.RecNote(wid, "報知対象: "+t.Label)
	}
	r.sink.RecNote(wid, fmt.Sprintf("I/Q: %.0f Hz 複素 2ch int16（I=L / Q=R）・プリロール %.0f 秒。"+
		"再デコードは std-t86 live <この .iq.wav>（オフセット 0 の 1 局）", r.fsOut, r.prerol))
	r.sink.RecNote(wid, "音声: "+filepath.Base(rec.Path(base, rec.Audio))+"（詳細は "+filepath.Base(rec.Path(base, rec.AudioText))+"）")
	r.emit(newLogEvent(r.state.currentT(),
		fmt.Sprintf("通報 #%d の IQ を %s へ保存します（%.0fHz 複素, プリロール %.0fs）",
			wid, rec.Path(base, rec.IQ), r.fsOut, r.prerol)))
	r.report(false)
}

func (r *iqRec) windowUpdated(wid int, t *target) {
	if r.rec == nil || r.rec.wid != wid || t == nil {
		return
	}
	r.sink.RecUpdate(wid, t.Label)
}

func (r *iqRec) windowClosed(wid int) {
	if r.rec == nil || r.rec.wid != wid {
		return
	}
	r.rec.tailLeft = r.tail
	r.rec.hasTail = true
}

func (r *iqRec) report(final bool) {
	cur := r.rec
	cur.reported = cur.samples
	info := iqStatus{
		Path:    rec.Path(cur.base, rec.IQ),
		Seconds: round(float64(cur.samples)/r.fsOut, 1),
		FS:      round(r.fsOut, 1),
		Done:    final,
		Note:    cur.note,
	}
	r.state.setWindowIQ(cur.wid, info)
	r.emit(iqStatusEvent{
		Type: "iq_status", WindowID: cur.wid, Path: info.Path,
		Seconds: info.Seconds, FS: info.FS, Done: info.Done, Note: info.Note,
	})
}

func (r *iqRec) finalize() {
	rec := r.rec
	if rec == nil {
		return
	}
	if rec.note != "" {
		r.sink.RecNote(rec.wid, "注記: "+rec.note)
	}
	r.sink.RecClose(rec.wid, 0)
	r.report(true)
	r.rec = nil
	r.audio.refreshSidecar(rec.wid)
}
