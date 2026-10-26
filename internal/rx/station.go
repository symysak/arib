package rx

import (
	"fmt"
	"os"
	"sync/atomic"
	"time"

	"github.com/symysak/arib/internal/rec"
)

const queueChunks = 32

type station struct {
	e   *Engine
	id  int
	mhz float64
	offHz float64
	ch    Channel
	in    chan []complex64
	done chan struct{}
	dropped atomic.Int64
	replayLeft atomic.Int64

	kind int
	since float64
	mark  int
	nominalDone bool
	dropping bool

	activeNS  atomic.Int64
	recording atomic.Bool

	rate     float64
	ring     []complex64
	ringHead int
	ringFull bool
	ringInit bool
	peak     float32
	recs     map[int]*openRec
	fed      int64
}

type openRec struct {
	w       *WavWriter
	sidecar *os.File
	tailLeft int
}

func newStation(e *Engine, id int, mhz float64) *station {
	s := &station{e: e, id: id, mhz: mhz, in: make(chan []complex64, queueChunks), done: make(chan struct{}), recs: map[int]*openRec{}}
	s.activeNS.Store(time.Now().UnixNano())
	return s
}

func (s *station) activeAt() time.Time { return time.Unix(0, s.activeNS.Load()) }

func (s *station) run() {
	lastDropped := s.dropped.Load()
	for iq := range s.in {
		if iq == nil {
			s.ch.Retune()
			continue
		}
		if s.replayLeft.Load() > 0 {
			s.replayLeft.Add(-1)
		}
		if n := s.dropped.Load(); n != lastDropped {
			lastDropped = n
			s.ch.Resync()
		}
		s.fed += int64(len(iq))
		s.ch.Feed(iq)
	}
	s.ch.Close(s.dropping)
	for id := range s.recs {
		s.closeRec(id)
	}
}

func (s *station) backlog() int {
	b := len(s.in) - int(s.replayLeft.Load())
	if b < 0 {
		return 0
	}
	return b
}

func (s *station) summary() string {
	return s.ch.Summary() + fmt.Sprintf(" 取りこぼし %d", s.dropped.Load())
}


func (s *station) Log(format string, a ...any) { s.e.lg.Printf(format, a...) }

func (s *station) Emit(ev any) {
	if s.e.cfg.OnEmit != nil {
		s.e.cfg.OnEmit(s.id, ev)
	}
}

func (s *station) StreamTime() time.Time {
	return s.e.origin.Add(time.Duration(float64(s.fed) / s.e.rate * float64(time.Second)))
}

func (s *station) Audio(id int, pcm []int16, rate int) {
	if s.e.cfg.OnAudio != nil {
		s.e.cfg.OnAudio(s.id, id, pcm, rate)
	}
}

func (s *station) MarkActive(hold time.Duration) { s.e.band.note(s.mhz, hold) }

func (s *station) BandActive() (bool, float64) { return s.e.band.active() }

func (s *station) IQ(y []complex64, rate float64) {
	if !s.ringInit {
		s.rate, s.ringInit, s.peak = rate, true, 1
		s.ring = make([]complex64, int((s.e.cfg.PreS+s.e.cfg.MaxLagS)*rate))
	}
	for _, v := range y {
		if a := absC(v); a > s.peak {
			s.peak = a
		}
	}
	if len(s.recs) == 0 {
		s.pushRing(y)
		return
	}
	for id, r := range s.recs {
		r.w.Write(y, s.peak)
		if r.tailLeft >= 0 {
			if r.tailLeft -= len(y); r.tailLeft <= 0 {
				s.closeRec(id)
			}
		}
	}
}

func (s *station) pushRing(y []complex64) {
	if len(s.ring) == 0 {
		return
	}
	for _, v := range y {
		s.ring[s.ringHead] = v
		s.ringHead++
		if s.ringHead == len(s.ring) {
			s.ringHead, s.ringFull = 0, true
		}
	}
}

func (s *station) RecOpen(id int, label string, lagSec float64) string {
	if r := s.recs[id]; r != nil {
		s.closeRec(id)
	}
	s.activeNS.Store(time.Now().UnixNano())
	s.recording.Store(true)
	dir := s.e.cfg.Dir()
	if sn, ok := s.e.std.(SystemNamer); ok {
		if name := sn.SystemFor(s.mhz); name != "" {
			dir = rec.SystemDir(s.e.cfg.LogDir, name)
		}
	}
	base, err := rec.NewBase(dir, time.Now(), s.mhz, label)
	if err != nil {
		s.Log("%.4f MHz: 録音を開けない: %v", s.mhz, err)
		return ""
	}
	wr, err := NewWavWriter(PathOrDiscard(rec.Path(base, rec.IQ), s.e.cfg.BandIQ || s.e.cfg.NoIQ), int(s.rate))
	if err != nil {
		s.Log("%.4f MHz: 録音を開けない: %v", s.mhz, err)
		return ""
	}
	avail := s.ringHead
	if s.ringFull {
		avail = len(s.ring)
	}
	n := min(avail, int((s.e.cfg.PreS+lagSec)*s.rate))
	if n > 0 {
		start := s.ringHead - n
		if start < 0 {
			start += len(s.ring)
		}
		if start < s.ringHead {
			wr.Write(s.ring[start:s.ringHead], s.peak)
		} else {
			wr.Write(s.ring[start:], s.peak)
			wr.Write(s.ring[:s.ringHead], s.peak)
		}
	}
	s.ringHead, s.ringFull = 0, false
	r := &openRec{w: wr, tailLeft: -1}
	r.sidecar, _ = os.Create(rec.Path(base, rec.Text))
	s.recs[id] = r
	return base
}

func (s *station) RecNote(id int, line string) {
	if r := s.recs[id]; r != nil && r.sidecar != nil {
		fmt.Fprintln(r.sidecar, line)
	}
}

func (s *station) RecUpdate(id int, label string) {
	s.RecNote(id, "ラベル "+label)
}

func (s *station) RecClose(id int, tailSec float64) {
	r := s.recs[id]
	if r == nil {
		return
	}
	if tailSec > 0 && s.rate > 0 {
		r.tailLeft = int(tailSec * s.rate)
		return
	}
	s.closeRec(id)
}

func (s *station) closeRec(id int) {
	r := s.recs[id]
	if r == nil {
		return
	}
	delete(s.recs, id)
	if len(s.recs) == 0 {
		s.recording.Store(false)
	}
	s.activeNS.Store(time.Now().UnixNano())
	dur := r.w.Close()
	s.Log("%.4f MHz: 録音終了 %.1f 秒", s.mhz, dur)
	if r.sidecar != nil {
		fmt.Fprintf(r.sidecar, "\n終了 %s 長さ %.1f 秒\n", time.Now().Format(time.RFC3339), dur)
		r.sidecar.Close()
	}
}
