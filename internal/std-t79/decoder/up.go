package decoder


import (
	"math"
	"math/cmplx"

	"github.com/symysak/arib/internal/std-t79/dsp"
	"github.com/symysak/arib/internal/std-t79/l1"
	"github.com/symysak/arib/internal/std-t79/l2"
)

type UpConfig struct {
	SampleRate float64
	OffsetHz   float64
	StartSec   float64
	Direct bool
	Mode   l1.DirectMode
	WatchDirect bool
}

type UpStats struct {
	Bursts, Control, CACOK, SBTotal, SBOK, SACCHTotal, SACCHOK, FACCHOK, Traffic int
	Misaligned int
	PICHTotal, PICHOK int
	BadI int
	DirectHits, IdleHits int
	DirectFirstT, DirectLastT float64
	DirectSBOK int
}

const (
	phaseTol    = 48.0
	phaseAnchor = 4
)

const upFrameSamples = 4 * 160 * 4

type upSlot struct {
	slotState
	lastPos float64
	have    bool
	ccInit  int
	pich   [l1.PICHUnits][]float32
	pichAt int
}

type UpDecoder struct {
	cfg   UpConfig
	demod *dsp.BurstDemod
	words []l1.SW
	slots [4]upSlot
	Stats UpStats
	lastT float64
	phase float64
	lastOff float64
	phaseN  int
	alt  float64
	altN int
	dwords []l1.DSW
	dcl    [4]directCluster
	Const []complex64
	dHint int
}

func NewUp(cfg UpConfig) *UpDecoder {
	if cfg.Direct {
		d := &UpDecoder{cfg: cfg, lastT: -1, dHint: -1}
		d.demod, d.dwords = newDirectDemod(cfg)
		for i := range d.slots {
			d.slots[i].sf, d.slots[i].init, d.slots[i].ccInit, d.slots[i].pichAt = -1, l1.InitAllOnes, -1, -1
		}
		return d
	}
	d := &UpDecoder{cfg: cfg, words: l1.UpWords(), lastT: -1}
	var ws []dsp.BurstWord
	for _, sw := range d.words {
		b, pos := l1.UpWord(sw)
		ws = append(ws, dsp.BurstWord{Bits: b, Pos: pos})
	}
	if cfg.WatchDirect {
		for _, w := range l1.DirectWords(l1.ModeDirect) {
			if !w.SB && w.N == 1 {
				continue
			}
			b, pos := w.Bits()
			ws = append(ws, dsp.BurstWord{Bits: b, Pos: pos})
			d.dwords = append(d.dwords, w)
		}
		s12 := l1.DSW{OK: true, Mode: l1.ModeDirect, N: 12}
		b := make([]uint8, 20)
		for i := range b {
			b[i] = uint8(l1.SW20[11] >> (19 - i) & 1)
		}
		ws = append(ws, dsp.BurstWord{Bits: b, Pos: l1.UpSWPos})
		d.dwords = append(d.dwords, s12)
	}
	d.demod = dsp.NewBurst(dsp.BurstConfig{SampleRate: cfg.SampleRate, OffsetHz: cfg.OffsetHz, Words: ws})
	for i := range d.slots {
		d.slots[i].sf, d.slots[i].init, d.slots[i].ccInit = -1, l1.InitAllOnes, -1
	}
	return d
}

func (d *UpDecoder) LastBurstSec() float64 { return d.lastT }

func (d *UpDecoder) FloorDB() float64 { return 10 * math.Log10(d.demod.Floor()+1e-20) }

func (d *UpDecoder) SetScramble(slot, init int) {
	if d.cfg.Direct {
		d.dHint = init
		for i := range d.slots {
			st := &d.slots[i]
			st.init, st.initKnown, st.hinted = init, true, true
		}
		return
	}
	if slot < 0 || slot >= len(d.slots) {
		return
	}
	st := &d.slots[slot]
	st.init, st.initKnown, st.initGave, st.hinted = init, true, false, true
}

func (d *UpDecoder) Scramble(slot int) (int, bool) {
	if slot < 0 || slot >= len(d.slots) {
		return 0, false
	}
	return d.slots[slot].init, d.slots[slot].initKnown
}

func (d *UpDecoder) Feed(x []complex64) []Slot {
	var out []Slot
	for _, b := range d.demod.Process(x) {
		if s, ok := d.burst(b); ok {
			out = append(out, s)
		}
	}
	return out
}

func (d *UpDecoder) burst(b dsp.Burst) (Slot, bool) {
	if d.cfg.Direct {
		return d.directBurst(b)
	}
	if b.Word >= len(d.words) {
		w := d.dwords[b.Word-len(d.words)]
		if w.N == 12 {
			if l1.FindSWDirect(b.Bits(), l1.ModeRelayUp, 2, 3).N == 12 {
				d.Stats.IdleHits++
			}
			return Slot{}, false
		}
		if got := l1.FindSWDirect(b.Bits(), l1.ModeDirect, 2, 3); got.OK && got.N == w.N && got.Ctl == w.Ctl && got.SB == w.SB {
			t := d.cfg.StartSec + b.TSec
			if d.Stats.DirectHits == 0 {
				d.Stats.DirectFirstT = t
			}
			d.Stats.DirectHits++
			d.Stats.DirectLastT = t
			if w.SB {
				if sb := l1.DecodeDirectSB(b.Soft()); sb.OK && sb.I == 0 {
					d.Stats.DirectSBOK++
				}
			}
		}
		return Slot{}, false
	}
	sw := d.words[b.Word]
	hard := b.Bits()
	soft := b.Soft()
	if got := l1.FindSWUp(hard, 2, 3); got.Kind != sw.Kind || got.N != sw.N || got.Inverted != sw.Inverted {
		return Slot{}, false
	}
	sw.Errs = l1.FindSWUp(hard, 2, 3).Errs
	if i, _ := l1.UpICC(hard); sw.Kind != l1.SWBurst && i != 0 {
		d.Stats.BadI++
		return Slot{}, false
	}
	n := sw.Slot()
	if !d.aligned(b.Pos, n) {
		d.Stats.Misaligned++
		return Slot{}, false
	}
	st := &d.slots[n]
	d.Stats.Bursts++
	d.keepConst(b.Diff)
	t := d.cfg.StartSec + b.TSec
	d.lastT = t
	adv := int64(1)
	if st.have {
		adv = int64(math.Round((b.Pos - st.lastPos) / upFrameSamples))
		if adv < 1 {
			adv = 1
		}
	}
	st.lastPos, st.have = b.Pos, true
	st.frame += adv
	switch {
	case sw.Kind == l1.SWSuper:
		st.sf = 0
	case st.sf >= 0:
		st.sf = int((int64(st.sf) + adv) % l1.FramesPerSuper)
	}
	_, cc := l1.UpICC(hard)
	s := Slot{TSec: t, Num: n, Frame: st.frame, SW: sw, CC: cc, Hard: hard, Up: true, CFOHz: b.CFOHz, LevelDB: b.LevelDB, BI: -1}
	s.TimingSym = d.lastOff / 4
	s.PhaseErrDeg = burstPhaseErr(b.Diff)
	if sw.Kind != l1.SWBurst {
		st.noteCC(cc)
	}
	s.CCDetected = detectCC(st.ccHist)
	switch {
	case sw.Kind == l1.SWBurst:
		sb := l1.DecodeSyncBurstUp(soft)
		d.Stats.SBTotal++
		if sb.OK {
			d.Stats.SBOK++
		}
		s.SB = &sb
	case n == 3 && !sw.Inverted && d.control(st, &s, soft, cc):
		d.Stats.Control++
	default:
		d.Stats.Traffic++
		st.ccInit = l1.InitFromCC(cc)
		raw := d.trafficUp(st, &s, soft)
		it := pendTCH{frame: st.frame, t: t, raw: raw, facch: sw.Inverted, voice: !sw.Inverted}
		d.queueUp(st, &s, it)
	}
	s.SFPos = st.sf
	s.Init, s.InitKnown = st.init, st.initKnown
	return s, true
}

func wrapFrame(x float64) float64 {
	x = math.Mod(x, upFrameSamples)
	if x > upFrameSamples/2 {
		x -= upFrameSamples
	} else if x <= -upFrameSamples/2 {
		x += upFrameSamples
	}
	return x
}

func (d *UpDecoder) aligned(pos float64, n int) bool {
	p := math.Mod(pos-float64(n)*upFrameSamples/4, upFrameSamples)
	if p < 0 {
		p += upFrameSamples
	}
	d.lastOff = 0
	if d.phaseN == 0 {
		d.phase, d.phaseN = p, 1
		return true
	}
	if e := wrapFrame(p - d.phase); math.Abs(e) <= phaseTol {
		d.lastOff = e
		d.phase = math.Mod(d.phase+0.1*e+upFrameSamples, upFrameSamples)
		d.phaseN++
		d.altN = 0
		return true
	}
	if d.altN > 0 && math.Abs(wrapFrame(p-d.alt)) <= phaseTol {
		d.altN++
	} else {
		d.alt, d.altN = p, 1
	}
	if d.altN >= phaseAnchor || d.phaseN == 1 && d.altN >= 2 {
		d.phase, d.phaseN, d.altN = d.alt, d.altN, 0
		return true
	}
	return false
}

func (d *UpDecoder) control(st *upSlot, s *Slot, soft []float32, cc int) bool {
	init := l1.InitFromCC(cc)
	for _, first := range []bool{true, false} {
		u := l1.DecodeUpCAC(soft, init, first)
		if !u.OK {
			continue
		}
		d.Stats.CACOK++
		s.Control, s.UpFirst = true, first
		s.CAC = &u
		s.CACCh = l1.CACSCCH
		s.Message = st.cacR[l1.CACSCCH].Push(l2.Unit{First: u.W.First, Last: u.W.Last, WO: u.W.WO, Data: u.Bytes()})
		if s.Message != nil {
			s.Recovered, _ = st.cacR[l1.CACSCCH].Recover(s.Message, 7, 10)
		}
		return true
	}
	return false
}

func (d *UpDecoder) trafficUp(st *upSlot, s *Slot, soft []float32) []float32 {
	raw := l1.TrafficUpRaw(soft)
	decodeRCH(&st.slotState, s, raw[148:168], 148)
	if u, k := l1.SACCHPart(st.sf); u >= 0 {
		st.sacch[u][k] = append(st.sacch[u][k][:0], raw[148:168]...)
		st.sacchHave[u] |= 1 << k
		if k == l1.SACCHSlots-1 {
			if st.sacchHave[u] == 0xFF {
				d.Stats.SACCHTotal++
				if r, ok := d.sacchUp(st, u); ok {
					d.Stats.SACCHOK++
					s.SACCH = &r
				}
			}
			st.sacchHave[u] = 0
		}
	}
	if s.SW.Inverted {
		if st.facchPrev != nil && st.facchFrm == st.frame-1 {
			if r, ok := d.facchUp(st, st.facchPrev, raw); ok {
				d.Stats.FACCHOK++
				s.FACCH = &r
			}
		}
		st.facchPrev, st.facchFrm = append(st.facchPrev[:0], raw...), st.frame
	}
	return append([]float32(nil), raw...)
}

func (st *upSlot) candidates() []int {
	out := []int{st.init}
	if st.ccInit >= 0 && st.ccInit != st.init {
		out = append(out, st.ccInit)
	}
	return out
}

func (d *UpDecoder) sacchUp(st *upSlot, u int) (l1.Unit, bool) {
	try := func(p []uint8) l1.Unit {
		v := make([]float32, 0, 160)
		for k := 0; k < l1.SACCHSlots; k++ {
			for i, x := range st.sacch[u][k] {
				if p[148+i] == 1 {
					x = -x
				}
				v = append(v, x)
			}
		}
		return l1.DecodeSACCH(v)
	}
	for _, init := range st.candidates() {
		if r := try(l1.PN(init, 284)); r.OK {
			st.init, st.initKnown, st.failRun = init, true, 0
			return r, true
		}
	}
	if st.holdKnown() {
		return l1.Unit{}, false
	}
	st.hinted = false
	for p := range pnTab {
		if r := try(pnTab[p]); r.OK && r.NZero() {
			st.init, st.initKnown, st.failRun = l1.InitFromPattern(p), true, 0
			return r, true
		}
	}
	st.initGave = true
	return l1.Unit{}, false
}

func (d *UpDecoder) facchUp(st *upSlot, first, second []float32) (l1.Unit, bool) {
	try := func(init int) l1.Unit {
		a, b := l1.TrafficUpAt(first, init), l1.TrafficUpAt(second, init)
		return l1.DecodeFACCH(a.TCH, b.TCH)
	}
	for _, init := range st.candidates() {
		if r := try(init); r.OK {
			st.init, st.initKnown, st.failRun = init, true, 0
			return r, true
		}
	}
	if st.holdKnown() {
		return l1.Unit{}, false
	}
	st.hinted = false
	for p := range pnTab {
		if r := try(l1.InitFromPattern(p)); r.OK && r.NZero() {
			st.init, st.initKnown, st.failRun = l1.InitFromPattern(p), true, 0
			return r, true
		}
	}
	return l1.Unit{}, false
}

func (d *UpDecoder) queueUp(st *upSlot, s *Slot, it pendTCH) {
	st.pend = append(st.pend, it)
	if !st.initKnown && len(st.pend) < pendMax {
		return
	}
	if !st.initKnown && st.ccInit >= 0 && !st.hinted {
		st.init = st.ccInit
	}
	for _, p := range st.pend {
		tr := l1.TrafficUpAt(p.raw, st.init)
		f := TCHFrame{Frame: p.frame, TSec: p.t, Voice: p.voice, FACCH: p.facch, Init: st.init, Known: st.initKnown}
		f.Bits, f.Soft = l1.Hard(tr.TCH), append([]float32(nil), tr.TCH...)
		s.TCH = append(s.TCH, f)
	}
	st.pend = st.pend[:0]
}

func (d *UpDecoder) keepConst(diff []complex64) {
	if len(d.Const) > 4096 {
		return
	}
	for _, v := range diff {
		if re, im := real(v), imag(v); re*re+im*im >= 0.04 {
			d.Const = append(d.Const, v)
		}
	}
}

func burstPhaseErr(diff []complex64) float64 {
	var acc float64
	n := 0
	for _, v := range diff {
		if v == 0 {
			continue
		}
		p := cmplx.Phase(complex128(v))
		acc += math.Abs(math.Mod(p+4*math.Pi, math.Pi/2) - math.Pi/4)
		n++
	}
	if n == 0 {
		return 0
	}
	return acc / float64(n) * 180 / math.Pi
}
