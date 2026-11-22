package rx

import (
	"math"

	"github.com/symysak/arib/internal/iqsrc"
)

const vfoSettleSec = 0.5

func takeTuning(src iqsrc.Source) (iqsrc.Tuning, bool) {
	t, ok := src.(iqsrc.Tuner)
	if !ok {
		return iqsrc.Tuning{}, false
	}
	select {
	case v, open := <-t.Tune():
		return v, open
	default:
		return iqsrc.Tuning{}, false
	}
}

func vfoMHz(t iqsrc.Tuning) float64 {
	if t.VfoHz > 0 {
		return t.VfoHz / 1e6
	}
	return t.CenterHz / 1e6
}

func (e *Engine) followTuning(t iqsrc.Tuning, ss []*station) (_ []*station, moved bool) {
	prev, had := e.tuning, e.haveTuning
	e.tuning, e.haveTuning = t, true
	rateMoved := t.Rate > 0 && math.Abs(t.Rate-e.rate) > 0.5
	centerMoved := t.CenterHz > 0 && math.Abs(t.CenterHz-e.cfg.CenterHz) > 0.5
	vfoMoved := !had || math.Abs(vfoMHz(t)-vfoMHz(prev)) > 1e-7
	if !rateMoved && !centerMoved && !(e.cfg.FollowVFO && vfoMoved) {
		return ss, false
	}
	e.mu.Lock()
	if rateMoved {
		if e.rate > 0 {
			e.read = int64(float64(e.read) * t.Rate / e.rate)
		}
		e.rate = t.Rate
		if e.frontOwned {
			e.front = e.makeFront(t.Rate)
		}
	}
	if centerMoved {
		e.cfg.CenterHz = t.CenterHz
	}
	e.mu.Unlock()
	moved = rateMoved || centerMoved
	e.lg.Printf("入力の同調が変わった: 中心 %.4f MHz・VFO %.4f MHz・%.0f Hz", t.CenterHz/1e6, t.VfoHz/1e6, t.Rate)
	if !e.cfg.FollowVFO {
		if moved {
			ss = e.rebuild(ss)
		}
		return ss, moved
	}
	vfo := vfoMHz(t)
	off := vfo*1e6 - e.cfg.CenterHz
	if !rateMoved && len(ss) == 1 && math.Abs(ss[0].mhz-vfo) < 1e-7 && math.Abs(ss[0].offHz-off) < 1 {
		return ss, moved
	}
	for _, s := range ss {
		e.drop(s)
	}
	for _, s := range ss {
		<-s.done
	}
	e.mu.Lock()
	e.cfg.Channels = []float64{vfo}
	e.mu.Unlock()
	e.vfoPending, e.vfoWant, e.vfoSince = true, vfo, e.inputSec()
	return nil, moved
}

func (e *Engine) settleVFO(ss []*station) []*station {
	if !e.vfoPending || e.inputSec()-e.vfoSince < vfoSettleSec {
		return ss
	}
	e.vfoPending = false
	vfo := e.vfoWant
	if off := vfo*1e6 - e.cfg.CenterHz; math.Abs(off) > e.rate/2*0.9 {
		e.lg.Printf("VFO %.4f MHz は入力帯域外（オフセット %+.0f kHz）なので監視しない", vfo, off/1e3)
		return ss
	}
	e.lg.Printf("VFO に付いて局を %.4f MHz へ移した", vfo)
	return append(ss, e.add(vfo, scanPinned, nil))
}

func (e *Engine) inputSec() float64 {
	if e.rate <= 0 {
		return 0
	}
	return float64(e.read) / e.rate
}
