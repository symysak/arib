package decoder


import (
	"math"

	"github.com/symysak/arib/internal/std-t79/dsp"
	"github.com/symysak/arib/internal/std-t79/l1"
)

const (
	directPhaseTol = 48.0
	directIdleSec = 2.0
)

type directCluster struct {
	phase float64
	lastT float64
	used  bool
}

func newDirectDemod(cfg UpConfig) (*dsp.BurstDemod, []l1.DSW) {
	dws := l1.DirectWords(cfg.Mode)
	var ws []dsp.BurstWord
	for _, w := range dws {
		b, pos := w.Bits()
		ws = append(ws, dsp.BurstWord{Bits: b, Pos: pos})
	}
	return dsp.NewBurst(dsp.BurstConfig{SampleRate: cfg.SampleRate, OffsetHz: cfg.OffsetHz, Words: ws}), dws
}

func (d *UpDecoder) cluster(pos, t float64) int {
	p := math.Mod(pos, upFrameSamples)
	if p < 0 {
		p += upFrameSamples
	}
	best, be := -1, directPhaseTol
	for i, c := range d.dcl {
		if !c.used || t-c.lastT > directIdleSec {
			continue
		}
		if e := math.Abs(wrapFrame(p - c.phase)); e <= be {
			best, be = i, e
		}
	}
	if best >= 0 {
		c := &d.dcl[best]
		d.lastOff = wrapFrame(p - c.phase)
		c.phase = math.Mod(c.phase+0.1*d.lastOff+upFrameSamples, upFrameSamples)
		c.lastT = t
		return best
	}
	for i := range d.dcl {
		if c := &d.dcl[i]; c.used && t-c.lastT > directIdleSec && math.Abs(wrapFrame(p-c.phase)) <= directPhaseTol {
			c.lastT = t
			d.lastOff = wrapFrame(p - c.phase)
			d.slots[i].have = false
			d.slots[i].pichAt = -1
			return i
		}
	}
	for i := range d.dcl {
		if c := &d.dcl[i]; !c.used || t-c.lastT > directIdleSec {
			*c = directCluster{phase: p, lastT: t, used: true}
			d.lastOff = 0
			st := &d.slots[i]
			*st = upSlot{}
			st.sf, st.init, st.ccInit, st.initKnown = -1, d.directInit(), -1, true
			st.pichAt = -1
			return i
		}
	}
	return -1
}

func (d *UpDecoder) directInit() int {
	if d.dHint >= 0 {
		return d.dHint
	}
	return l1.InitAllOnes
}

func (d *UpDecoder) directBurst(b dsp.Burst) (Slot, bool) {
	dw := d.dwords[b.Word]
	hard := b.Bits()
	soft := b.Soft()
	got := l1.FindSWDirect(hard, d.cfg.Mode, 2, 3)
	if !got.OK || got.N != dw.N || got.Ctl != dw.Ctl || got.SB != dw.SB {
		return Slot{}, false
	}
	t := d.cfg.StartSec + b.TSec
	n := d.cluster(b.Pos, t)
	if n < 0 {
		d.Stats.Misaligned++
		return Slot{}, false
	}
	d.lastT = t
	d.Stats.Bursts++
	d.keepConst(b.Diff)
	st := &d.slots[n]
	adv := int64(1)
	if st.have {
		adv = int64(math.Round((b.Pos - st.lastPos) / upFrameSamples))
		if adv < 1 {
			adv = 1
		}
	}
	st.lastPos, st.have = b.Pos, true
	st.frame += adv
	sw := l1.SW{Kind: l1.SWNormal, N: got.N, Inverted: got.Ctl, Errs: got.Errs}
	if got.SB {
		sw.Kind = l1.SWBurst
	}
	s := Slot{TSec: t, Num: n, Frame: st.frame, SW: sw, CC: -1, CCDetected: -1, Hard: hard, Up: true, CFOHz: b.CFOHz, LevelDB: b.LevelDB,
		BI: -1, SFPos: -1, TimingSym: d.lastOff / 4}
	dsw := got
	s.Direct = &dsw
	s.PhaseErrDeg = burstPhaseErr(b.Diff)
	if got.SB {
		sb := l1.DecodeDirectSB(soft)
		d.Stats.SBTotal++
		if sb.OK {
			d.Stats.SBOK++
		}
		s.DSB = &sb
		st.pichAt = -1
		s.Init, s.InitKnown = st.init, st.initKnown
		return s, true
	}
	d.Stats.Traffic++
	raw := l1.DirectDataRaw(soft)
	pich := l1.DirectPICH(soft)
	switch {
	case got.First:
		st.pich[0], st.pichAt = pich, 0
	case st.pichAt >= 0 && adv == 1 && st.pichAt+1 < l1.PICHUnits:
		st.pichAt++
		st.pich[st.pichAt] = pich
	default:
		st.pichAt = -1
	}
	if st.pichAt == l1.PICHUnits-1 {
		p := l1.DecodePICH(st.pich[:])
		d.Stats.PICHTotal++
		if p.OK {
			d.Stats.PICHOK++
		}
		s.PICH = &p
		st.pichAt = -1
	}
	if got.Ctl {
		if st.facchPrev != nil && st.facchFrm == st.frame-1 {
			if r, ok := d.dacch(st, st.facchPrev, raw); ok {
				d.Stats.FACCHOK++
				s.FACCH = &r
			}
		}
		st.facchPrev, st.facchFrm = append(st.facchPrev[:0], raw...), st.frame
	}
	d.queueDirect(st, &s, pendTCH{frame: st.frame, t: t, raw: raw, facch: got.Ctl, voice: !got.Ctl && got.Voice})
	s.Init, s.InitKnown = st.init, st.initKnown
	return s, true
}

var directPatterns = func() []int {
	var out []int
	for _, hi := range []int{1, 2} {
		for lo := 0; lo < 16; lo++ {
			out = append(out, hi<<4|lo)
		}
	}
	for p := 0; p < 256; p++ {
		if p>>4 != 1 && p>>4 != 2 {
			out = append(out, p)
		}
	}
	return out
}()

func (d *UpDecoder) dacch(st *upSlot, first, second []float32) (l1.Unit, bool) {
	try := func(init int) l1.Unit {
		return l1.DecodeFACCH(l1.DirectDataAt(first, init), l1.DirectDataAt(second, init))
	}
	if r := try(st.init); r.OK {
		return r, true
	}
	if d.cfg.Mode != l1.ModeDirect {
		return l1.Unit{}, false
	}
	if st.init != l1.InitAllOnes {
		if r := try(l1.InitAllOnes); r.OK {
			st.init, st.initKnown = l1.InitAllOnes, true
			return r, true
		}
	}
	for _, p := range directPatterns {
		if init := l1.InitFromPattern(p); init != st.init {
			if r := try(init); r.OK && r.NZero() {
				st.init, st.initKnown, st.hinted = init, true, false
				return r, true
			}
		}
	}
	return l1.Unit{}, false
}

func (d *UpDecoder) queueDirect(st *upSlot, s *Slot, it pendTCH) {
	st.pend = append(st.pend, it)
	if !st.initKnown && len(st.pend) < pendMax {
		return
	}
	for _, p := range st.pend {
		v := l1.DirectDataAt(p.raw, st.init)
		f := TCHFrame{Frame: p.frame, TSec: p.t, Voice: p.voice, FACCH: p.facch, Init: st.init, Known: st.initKnown}
		f.Bits, f.Soft = l1.Hard(v), v
		s.TCH = append(s.TCH, f)
	}
	st.pend = st.pend[:0]
}
