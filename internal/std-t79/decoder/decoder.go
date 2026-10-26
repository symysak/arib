package decoder

import (
	"math"

	"github.com/symysak/arib/internal/std-t79/dsp"
	"github.com/symysak/arib/internal/std-t79/l1"
	"github.com/symysak/arib/internal/std-t79/l2"
)

type Config struct {
	SampleRate float64
	OffsetHz   float64
	StartSec float64
	Constellation bool
}

const (
	acqErr       = 0
	acqVerifyErr = 2
	trackErr20 = 3
	trackErr32 = 4
	detectErr = 2
	lostSlots = 40
	sfLostN4 = 10
	pendMax = 2 * l1.FramesPerSuper
)

type TCHFrame struct {
	Frame int64
	TSec  float64
	Voice bool
	Bits  []uint8
	Soft  []float32
	FACCH bool
	Init  int
	Known bool
	Idle bool
}

type Slot struct {
	TSec  float64
	Num   int
	Frame int64
	SW    l1.SW
	Flywheel bool
	CI, CC   int
	SFLost bool
	CCDetected int
	Control     bool
	SFPos       int
	PhaseErrDeg float64
	Hard []uint8

	CAC     *l1.Unit
	Message []byte
	CACCh l1.CACChannel
	Recovered []byte
	SB        *l1.SyncBurst
	BI    int
	SACCH *l1.Unit
	RCH   *l1.RCH
	FACCH *l1.Unit
	TCH []TCHFrame
	Init      int
	InitKnown bool

	Up      bool
	UpFirst bool
	CFOHz   float64
	LevelDB float64
	Direct *l1.DSW
	DSB    *l1.DirectSB
	PICH   *l1.PICH
	TimingSym float64
}

type slotState struct {
	frame     int64
	sf        int
	init      int
	initKnown bool
	initGave  bool
	hinted    bool
	failRun   int
	sacch     [2][l1.SACCHSlots][]float32
	sacchHave [2]uint8
	facchPrev []float32
	facchFrm  int64
	rch0      []float32
	rch0Frm   int64
	pend      []pendTCH
	sfMiss int
	ccHist []int
	cacR [4]l2.Reassembler
}

type pendTCH struct {
	frame int64
	t     float64
	raw   []float32
	facch bool
	voice bool
	idle  bool
}

type Decoder struct {
	cfg   Config
	demod *dsp.Demod

	hard []uint8
	soft []float32
	perr []float32
	base int64

	locked  bool
	next    int64
	miss    int
	slotIdx int64
	slotOff int
	ciScore int
	slots   [4]slotState
	cacStruct l1.CACStructure
	Stats Stats

	Const []complex64
}

type Stats struct {
	Slots, SWFound, Locks, Losses int
	CACOK, CACTotal               int
	SACCHOK, SACCHTotal           int
	FACCHOK                       int
	SBOK, SBTotal                 int
	SFLosses int
}

func New(cfg Config) *Decoder {
	d := &Decoder{cfg: cfg}
	d.demod = dsp.New(dsp.Config{SampleRate: cfg.SampleRate, OffsetHz: cfg.OffsetHz, CoarseCheck: coarseCheck})
	for i := range d.slots {
		d.slots[i] = slotState{sf: -1, init: l1.InitAllOnes}
	}
	return d
}

func (d *Decoder) CoarseHz() float64 { return d.demod.CoarseHz() }
func (d *Decoder) CFOHz() float64    { return d.demod.CFOHz() }

func (d *Decoder) Locked() bool { return d.locked }

func coarseCheck(b []uint8) int {
	cnt := map[int]int{}
	for p := 0; p+20 <= len(b); p++ {
		w := word(b, p, 20)
		for i, s := range l1.SW20 {
			if w == s || (i < 8 && w == s^0xFFFFF) {
				cnt[p%l1.SlotBits]++
				break
			}
		}
	}
	best := 0
	for _, n := range cnt {
		best = max(best, n)
	}
	if best < 2 {
		return 0
	}
	return best
}

func word(b []uint8, p, n int) uint32 {
	var v uint32
	for i := 0; i < n; i++ {
		v = v<<1 | uint32(b[p+i]&1)
	}
	return v
}

func (d *Decoder) Feed(x []complex64) []Slot {
	for _, s := range d.demod.Process(x) {
		xb, yb := s.Bits()
		sx, sy := s.Soft()
		d.hard = append(d.hard, xb, yb)
		d.soft = append(d.soft, sx, sy)
		d.perr = append(d.perr, float32(math.Abs(s.PhaseError())))
		if d.cfg.Constellation {
			d.Const = append(d.Const, s.Diff)
		}
	}
	var out []Slot
	for {
		if !d.locked {
			if !d.acquire() {
				break
			}
		}
		if d.next+l1.SlotBits+2 > d.base+int64(len(d.hard)) {
			break
		}
		if s, ok := d.slot(); ok {
			out = append(out, s)
		}
	}
	d.trim()
	return out
}

func (d *Decoder) trim() {
	keep := int64(len(d.hard)) - 4*l1.SlotBits
	if d.locked {
		keep = d.next - d.base - 4
	}
	keep &^= 1
	if keep <= 0 {
		return
	}
	k := int(keep)
	d.hard = append(d.hard[:0], d.hard[k:]...)
	d.soft = append(d.soft[:0], d.soft[k:]...)
	d.perr = append(d.perr[:0], d.perr[k/2:]...)
	d.base += keep
}

func (d *Decoder) acquire() bool {
	n := len(d.hard)
	for s := 0; s+3*l1.SlotBits <= n; s += 2 {
		sw := l1.FindSW(d.hard[s:s+l1.SlotBits], acqErr, 1)
		if sw.Kind == l1.SWNone {
			continue
		}
		a := l1.FindSW(d.hard[s+l1.SlotBits:s+2*l1.SlotBits], acqVerifyErr, acqVerifyErr+1)
		b := l1.FindSW(d.hard[s+2*l1.SlotBits:s+3*l1.SlotBits], acqVerifyErr, acqVerifyErr+1)
		if (a.Kind == l1.SWNone || a.Slot() != (sw.Slot()+1)%4) && (b.Kind == l1.SWNone || b.Slot() != (sw.Slot()+2)%4) {
			continue
		}
		d.locked, d.miss = true, 0
		d.next = d.base + int64(s)
		d.slotOff = ((sw.Slot()-int(d.slotIdx%4))%4 + 4) % 4
		d.Stats.Locks++
		return true
	}
	return false
}

func (d *Decoder) slot() (Slot, bool) {
	off := int(d.next - d.base)
	num := int((d.slotIdx + int64(d.slotOff)) % 4)
	best := l1.FindSW(d.hard[off:off+l1.SlotBits], trackErr20, trackErr32)
	if best.Kind != l1.SWNone && best.Slot() != num {
		best = l1.SW{}
	}
	shift := 0
	if best.Kind == l1.SWNone || best.Errs > 0 {
		for _, sh := range []int{-2, 2} {
			o := off + sh
			if o < 0 || o+l1.SlotBits > len(d.hard) {
				continue
			}
			sw := l1.FindSW(d.hard[o:o+l1.SlotBits], 1, 2)
			if sw.Kind != l1.SWNone && sw.Slot() == num && (best.Kind == l1.SWNone || sw.Errs < best.Errs) {
				best, shift = sw, sh
			}
		}
	}
	off += shift
	d.next += int64(shift)
	hard := d.hard[off : off+l1.SlotBits]
	soft := d.soft[off : off+l1.SlotBits]
	t := d.cfg.StartSec + float64(d.next)/2/dsp.SymbolRate
	d.next += l1.SlotBits
	d.Stats.Slots++

	if best.Kind != l1.SWNone {
		d.Stats.SWFound++
	}
	if best.Kind != l1.SWNone && best.Errs <= detectErr {
		d.miss = 0
	} else {
		d.miss++
		if d.miss >= lostSlots {
			d.locked = false
			d.Stats.Losses++
			for i := range d.slots {
				d.slots[i].sf = -1
				d.slots[i].resetCAC()
				d.slots[i].facchPrev = nil
				d.slots[i].sacchHave = [2]uint8{}
			}
		}
	}
	d.slotIdx++

	st := &d.slots[num]
	st.frame++
	ci, cc := l1.CICC(hard)
	s := Slot{TSec: t, Num: num, Frame: st.frame, SW: best, Flywheel: best.Kind == l1.SWNone, CI: ci, CC: cc,
		Hard: append([]uint8(nil), hard...)}
	if best.Kind != l1.SWNone && best.Kind != l1.SWBurst {
		st.noteCC(cc)
	}
	s.CCDetected = detectCC(st.ccHist)
	s.PhaseErrDeg = d.phaseErr(off)
	switch {
	case best.Kind == l1.SWSuper:
		st.sf = 0
		if best.Errs <= detectErr {
			st.sfMiss = 0
		}
	case st.sf >= 0:
		st.sf = (st.sf + 1) % l1.FramesPerSuper
		if st.sf == 0 && best.Kind != l1.SWIdle {
			st.sfMiss++
			if st.sfMiss >= sfLostN4 {
				st.sf, st.sfMiss = -1, 0
				d.Stats.SFLosses++
				s.SFLost = true
			}
		}
	}
	if best.Kind == l1.SWNormal || best.Kind == l1.SWSuper || best.Kind == l1.SWIdle {
		switch {
		case ci == 0 && d.ciScore < 64:
			d.ciScore++
		case ci == 3 && d.ciScore > -64:
			d.ciScore--
		}
	}
	ctrlCarrier := d.ciScore > 0 || (d.ciScore == 0 && ci == 0)
	s.Control = ctrlCarrier && num == 3 && (best.Kind == l1.SWNormal || best.Kind == l1.SWSuper) && !best.Inverted
	if best.Kind == l1.SWIdle {
		st.sf = -1
	}

	s.BI = -1
	it := pendTCH{frame: st.frame, t: s.TSec}
	switch {
	case best.Kind == l1.SWNone:
	case best.Kind == l1.SWBurst:
		sb := l1.DecodeSyncBurst(soft)
		d.Stats.SBTotal++
		if sb.OK {
			d.Stats.SBOK++
		}
		s.SB = &sb
	case s.Control:
		u := l1.DecodeCAC(soft)
		d.Stats.CACTotal++
		if u.OK {
			d.Stats.CACOK++
			if p := u.SFPos(); p >= 1 && p <= 18 {
				st.sf = p - 1
			}
			s.CACCh = d.cacStruct.Channel(st.sf + 1)
			s.Message = st.cacR[s.CACCh].Push(l2.Unit{First: u.W.First, Last: u.W.Last, WO: u.W.WO, Data: u.Bytes()})
			if s.Message != nil {
				s.Recovered, _ = st.cacR[s.CACCh].Recover(s.Message, 11, 11)
			}
		} else if d.cacStruct.Valid() && st.sf >= 0 {
			s.CACCh = d.cacStruct.Channel(st.sf + 1)
		}
		s.CAC = &u
	default:
		it.raw = d.traffic(st, &s, soft)
		it.facch, it.voice = best.Inverted, !best.Inverted
		if st.initKnown {
			s.BI = busyIdle(l1.TrafficAt(l1.TrafficRaw(soft), st.init).BI)
		}
	}
	if !s.Control {
		it.idle = best.Kind == l1.SWIdle
		d.queue(st, &s, it)
	}
	s.SFPos = st.sf
	s.Init, s.InitKnown = st.init, st.initKnown
	return s, true
}

func (st *slotState) noteCC(cc int) {
	st.ccHist = append(st.ccHist, cc)
	if len(st.ccHist) > 7 {
		st.ccHist = st.ccHist[len(st.ccHist)-7:]
	}
}

func detectCC(h []int) int {
	if n := len(h); n >= 4 && h[n-1] == h[n-2] && h[n-2] == h[n-3] && h[n-3] == h[n-4] {
		return h[n-1]
	}
	if len(h) < 7 {
		return -1
	}
	cnt := map[int]int{}
	for _, v := range h {
		cnt[v]++
		if cnt[v] >= 4 {
			return v
		}
	}
	return -1
}

func busyIdle(v []float32) int {
	ones := 0
	for _, x := range v {
		if x < 0 {
			ones++
		}
	}
	switch {
	case ones >= len(v)-1:
		return 1
	case ones <= 1:
		return 0
	}
	return -1
}

func (d *Decoder) phaseErr(off int) float64 {
	a := off / 2
	b := a + l1.SlotBits/2
	if a < 0 || b > len(d.perr) {
		return 0
	}
	var sum float32
	for _, v := range d.perr[a:b] {
		sum += v
	}
	return float64(sum) / float64(b-a) * 180 / math.Pi
}

func (st *slotState) resetCAC() {
	for i := range st.cacR {
		st.cacR[i].Reset()
	}
}

func (d *Decoder) SetCACStructure(c l1.CACStructure) {
	if c == d.cacStruct || !c.Valid() {
		return
	}
	d.cacStruct = c
	for i := range d.slots {
		d.slots[i].resetCAC()
	}
}

func (d *Decoder) ClearCACStructure() {
	d.cacStruct = l1.CACStructure{}
	for i := range d.slots {
		d.slots[i].resetCAC()
	}
}

func (d *Decoder) CACStructure() l1.CACStructure { return d.cacStruct }

var pnTab [256][]uint8

func init() {
	for p := range pnTab {
		pnTab[p] = l1.PN(l1.InitFromPattern(p), 284)
	}
}

func (d *Decoder) traffic(st *slotState, s *Slot, soft []float32) []float32 {
	raw := l1.TrafficRaw(soft)
	busy := s.SW.Kind == l1.SWNormal || s.SW.Kind == l1.SWSuper
	if s.SW.Kind != l1.SWIdle {
		decodeRCH(st, s, raw[112:132], 112)
	}
	if u, k := l1.SACCHPart(st.sf); u >= 0 && s.SW.Kind != l1.SWIdle {
		st.sacch[u][k] = append(st.sacch[u][k][:0], raw[112:132]...)
		st.sacchHave[u] |= 1 << k
		if k == l1.SACCHSlots-1 {
			if st.sacchHave[u] == 0xFF {
				d.Stats.SACCHTotal++
				if r, ok := d.sacch(st, u); ok {
					d.Stats.SACCHOK++
					s.SACCH = &r
				}
			}
			st.sacchHave[u] = 0
		}
	}
	if s.SW.Inverted {
		if st.facchPrev != nil && st.facchFrm == st.frame-1 {
			if r, ok := d.facch(st, st.facchPrev, raw); ok {
				d.Stats.FACCHOK++
				s.FACCH = &r
			}
		}
		st.facchPrev, st.facchFrm = append(st.facchPrev[:0], raw...), st.frame
	}
	if !busy {
		return nil
	}
	return append([]float32(nil), raw...)
}

func decodeRCH(st *slotState, s *Slot, field []float32, pn int) {
	switch st.sf {
	case 0:
		st.rch0, st.rch0Frm = append(st.rch0[:0], field...), st.frame
	case 1:
		if st.rch0 == nil || st.rch0Frm != st.frame-1 || !st.initKnown {
			return
		}
		p := l1.PN(st.init, pn+20)
		a := append([]float32(nil), st.rch0...)
		b := append([]float32(nil), field...)
		for i := range a {
			if p[pn+i] == 1 {
				a[i], b[i] = -a[i], -b[i]
			}
		}
		r := l1.DecodeRCH(a, b)
		s.RCH = &r
		st.rch0 = nil
	}
}

func (d *Decoder) queue(st *slotState, s *Slot, it pendTCH) {
	if it.raw == nil && len(st.pend) == 0 {
		s.TCH = append(s.TCH, TCHFrame{Frame: it.frame, TSec: it.t})
		return
	}
	st.pend = append(st.pend, it)
	if !(st.initKnown || st.initGave || len(st.pend) > pendMax || it.idle) {
		return
	}
	for _, p := range st.pend {
		f := TCHFrame{Frame: p.frame, TSec: p.t}
		if p.raw != nil {
			tr := l1.TrafficAt(p.raw, st.init)
			f.Voice, f.FACCH = p.voice, p.facch
			f.Bits, f.Soft = l1.Hard(tr.TCH), append([]float32(nil), tr.TCH...)
			f.Init, f.Known = st.init, st.initKnown
		}
		s.TCH = append(s.TCH, f)
	}
	st.pend = st.pend[:0]
}

const researchAfter = 3

func (st *slotState) holdKnown() bool {
	if !st.initKnown || st.hinted {
		return false
	}
	st.failRun++
	if st.failRun < researchAfter {
		return true
	}
	st.failRun = 0
	return false
}

func (d *Decoder) sacch(st *slotState, u int) (l1.Unit, bool) {
	try := func(p []uint8) l1.Unit {
		v := make([]float32, 0, 160)
		for k := 0; k < l1.SACCHSlots; k++ {
			for i, x := range st.sacch[u][k] {
				if p[112+i] == 1 {
					x = -x
				}
				v = append(v, x)
			}
		}
		return l1.DecodeSACCH(v)
	}
	if r := try(l1.PN(st.init, 284)); r.OK {
		st.initKnown, st.failRun = true, 0
		return r, true
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

func (d *Decoder) facch(st *slotState, first, second []float32) (l1.Unit, bool) {
	try := func(init int) l1.Unit {
		a, b := l1.TrafficAt(first, init), l1.TrafficAt(second, init)
		return l1.DecodeFACCH(a.TCH, b.TCH)
	}
	if r := try(st.init); r.OK {
		st.initKnown, st.failRun = true, 0
		return r, true
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

func (d *Decoder) SetScramble(slot, init int) {
	if slot < 0 || slot >= len(d.slots) {
		return
	}
	st := &d.slots[slot]
	st.init, st.initKnown, st.initGave, st.hinted = init, true, false, true
}

func (d *Decoder) Scramble(slot int) (init int, known bool) {
	if slot < 0 || slot >= len(d.slots) {
		return 0, false
	}
	return d.slots[slot].init, d.slots[slot].initKnown
}
