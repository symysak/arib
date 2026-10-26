package decoder

import (
	"math"

	"github.com/symysak/arib/internal/std-t61/dsp"
	"github.com/symysak/arib/internal/std-t61/fec"
	"github.com/symysak/arib/internal/std-t61/l1"
	"github.com/symysak/arib/internal/std-t61/l2"
	"github.com/symysak/arib/internal/std-t61/l3"
)

const (
	syncMaxErrAcquire = 1
	syncMaxErrConfirm = 2
	syncMaxErrTrack   = 3
	syncLossFrames    = 10
	swOffsetSymbols   = l1.SWStart / 2
	a1SWOffsetSymbols = l1.A1DataStart/2 - 10
	a1RICHFromSW = 10

	syncVerifyFrames = 3
	syncVerifyNeed   = 2
	syncFallbackSymbols = 2 * int64(dsp.SymbolRate)
)

type Config struct {
	SampleRate float64
	OffsetHz   float64
	Edition l1.Edition
	Scramble int
	NoConstellation bool
	KeepRaw bool
	ParseL2 func(channel string, raw []byte) l2.Frame
	AnnotateL3 func(channel string, l2f l2.Frame, sig *l3.Signal)
}

const (
	ConstTapStep = 4
	constTapMax = 2 * int(dsp.SymbolRate) / ConstTapStep
)

type ConstSample struct {
	TSec float64
	Diff complex64
}

type Frame struct {
	Number   int64
	Symbol   int64
	TimeSec  float64
	SW       *l1.SyncWord
	SWErrors int
	Flywheel bool
	Kind     l1.ChannelKind
	Uplink   bool
	RICH     l1.RICH
	SFPos    int
	Control *l1.ControlUnit
	SACCH   *l1.SACCHUnit
	RCH     *l1.RCHUnit
	FACCH   *l1.FACCHUnit
	NV2    *l1.FACCHUnit
	NV2Msg []byte
	SB     *l1.SyncBurst
	TCH []uint8
	TCHSoft []float32
	TCHInit int
	TCHIdle bool
	FACCHHalf int
	PhaseErrDeg float64
	Raw []uint8
	A1RICH *l1.A1RICH
	TCH1     []uint8
	TCH1Soft []float32
	Messages []Message
	UplinkScrambleAssigned int
	UplinkScrambleFound int
	UplinkControlScrambleFound int
	Frag []FragInfo
}

type FragInfo struct {
	Channel string
	Have    int
	Need    int
	Done    bool
	OK      bool
	Metric float64
	Corrected int
	Filler    bool
}

type Message struct {
	Channel string
	L2      l2.Frame
	L3      *l3.Signal
	Recovered bool
}

type Decoder struct {
	cfg   Config
	demod *dsp.Demod

	syms []dsp.Symbol
	base int64
	constTap []ConstSample

	locked   bool
	nextSW   int64
	lastSW   *l1.SyncWord
	kindCand *l1.SyncWord
	misses   int
	huntSyms int64
	frameNo  int64
	sfPos    int
	edition  l1.Edition
	edVote   int
	a1Vote    int
	scramble  int
	sacchBuf  []float32
	sacchFrom int
	rchBuf    []float32
	facchPrev []float32
	upScramble int
	upAssigned int
	upFound, upCand, upCandN int
	upSkip int
	upCtlFound, upCtlBad, upCtlSkip int
	upCtlFrames, upCtlLastGood      int
	upCtlCand, upCtlCandLeft        int
	facchWait                       bool
	facchExtra bool
	nv2Prev   []float32
	nv2PrevM3 bool
	combPCC, combSACCH, combFACCH repeatCombiner
	asm map[string]*l2.Reassembler

	Stats Stats
}

type Stats struct {
	Frames, SWDetected, Flywheel int
	RICHOK                       int
	ControlUnits, ControlOK      int
	UpControlUnits, UpControlOK int
	SACCHUnits, SACCHOK         int
	RCHUnits, RCHOK             int
	FACCHUnits, FACCHOK         int
	SACCHFiller, RCHFiller, FACCHFiller int
	NV2Units, NV2OK                     int
	A1Frames, A1Voice  int
	SBUnits, SBOK      int
	SyncLoss           int
	SWCount            map[string]int
	L2Frames, L2Errors int
	L3Messages         int
	Combined int
}

func New(cfg Config) *Decoder {
	d := &Decoder{cfg: cfg, sfPos: -1, edition: cfg.Edition, upScramble: -1, upAssigned: -1, upFound: -1, upCand: -1, upCtlFound: -1, upCtlCand: -1}
	d.demod = dsp.New(dsp.Config{SampleRate: cfg.SampleRate, OffsetHz: cfg.OffsetHz, CoarseCheck: coarseCheck})
	d.scramble = cfg.Scramble
	if d.scramble == 0 {
		d.scramble = fec.ScrambleAllOnes
	}
	d.Stats.SWCount = map[string]int{}
	d.asm = map[string]*l2.Reassembler{"PCC": {}, "PCC↑": {}, "SACCH": {}, "FACCH": {}, "NV2": {}}
	return d
}

func (d *Decoder) pushUnit(f *Frame, ch string, w l1.W, data []uint8) {
	msg := d.asm[ch].Push(l2.Unit{First: w.First, Last: w.Last, WO: w.WO, Data: l2.BitsToBytes(data)})
	if msg == nil {
		return
	}
	m := d.emitL2(f, ch, msg, false)
	if m.L2.Err == "" && m.L2.Control.Name == "I'" {
		first, unit := unitOctets(ch)
		if orig, ok := d.asm[ch].Recover(msg, first, unit); ok {
			d.emitL2(f, ch, orig, true)
		}
	}
}

func (d *Decoder) emitL2(f *Frame, ch string, msg []byte, recovered bool) Message {
	parse := d.cfg.ParseL2
	if parse == nil {
		parse = func(_ string, b []byte) l2.Frame { return l2.Parse(b) }
	}
	dir := senderDir(f, d.edition)
	m := Message{Channel: ch, L2: parse(ch, msg), Recovered: recovered}
	m.L2.Channel = ch
	m.L2.Dir = l2.DirDown
	if dir == l3.DirUp {
		m.L2.Dir = l2.DirUp
	}
	d.Stats.L2Frames++
	if m.L2.Err != "" {
		d.Stats.L2Errors++
	} else if len(m.L2.Info) > 0 && m.L2.CarriesInfo() {
		sig := l3.ParseDir(m.L2.Info, dir)
		if d.cfg.AnnotateL3 != nil {
			d.cfg.AnnotateL3(ch, m.L2, &sig)
		}
		if init, _, ok := l3.AssignedScramble(sig); ok && dir == l3.DirDown {
			d.upAssigned = init
			f.UplinkScrambleAssigned = init + 1
		}
		m.L3 = &sig
		d.Stats.L3Messages += len(sig.Messages)
	}
	f.Messages = append(f.Messages, m)
	return m
}

func unitOctets(ch string) (first, unit int) {
	switch ch {
	case "PCC":
		return 11, 11
	case "PCC↑":
		return 8, 12
	case "SACCH":
		return 6, 6
	case "FACCH":
		return 12, 12
	}
	return 0, 0
}

func senderDir(f *Frame, ed l1.Edition) l3.Dir {
	if f.Uplink {
		return l3.DirUp
	}
	if ed != l1.EditionSCPC && directChannel(f.RICH) {
		return l3.DirUp
	}
	return l3.DirDown
}

func directChannel(r l1.RICH) bool { return r.HasMode() && r.S == 1 && r.D == 3 }

func (d *Decoder) resetAssemblers() {
	for _, r := range d.asm {
		*r = l2.Reassembler{}
	}
}

func (d *Decoder) ResetSync() {
	d.locked = false
	d.syms, d.base = nil, 0
	d.misses, d.sfPos = 0, -1
	d.lastSW, d.kindCand = nil, nil
	d.huntSyms = 0
	d.ResetAssembly()
}

func (d *Decoder) ResetAssembly() {
	d.resetAssemblers()
	d.sacchBuf, d.sacchFrom = nil, -1
	d.rchBuf = nil
	d.facchPrev, d.facchWait, d.facchExtra = nil, false, false
	d.nv2Prev, d.nv2PrevM3 = nil, false
	d.combPCC.reset()
	d.combSACCH.reset()
	d.combFACCH.reset()
}

func (d *Decoder) Edition() l1.Edition { return d.edition }

func (d *Decoder) CFOHz() float64 { return d.demod.CFOHz() }

func (d *Decoder) CoarseHz() float64 { return d.demod.CoarseHz() }

func (d *Decoder) Locked() bool { return d.locked }

func (d *Decoder) Feed(iq []complex64) []Frame {
	syms := d.demod.Process(iq)
	if len(syms) == 0 {
		return nil
	}
	d.tapConst(syms)
	if len(d.syms) == 0 {
		d.base = syms[0].Index
	}
	d.syms = append(d.syms, syms...)
	return d.run()
}

func (d *Decoder) tapConst(syms []dsp.Symbol) {
	if d.cfg.NoConstellation {
		return
	}
	for _, s := range syms {
		if s.Index%ConstTapStep != 0 {
			continue
		}
		d.constTap = append(d.constTap, ConstSample{
			TSec: float64(s.Index) / dsp.SymbolRate, Diff: s.Diff})
	}
	if n := len(d.constTap) - constTapMax; n > 0 {
		d.constTap = append(d.constTap[:0], d.constTap[n:]...)
	}
}

func (d *Decoder) TakeConst() []ConstSample {
	if len(d.constTap) == 0 {
		return nil
	}
	out := d.constTap
	d.constTap = nil
	return out
}

func (d *Decoder) Flush() []Frame { return nil }

func (d *Decoder) a1Possible() bool {
	return d.cfg.Edition == l1.EditionAuto && (d.edition == l1.EditionAuto || d.edition == l1.EditionA1) ||
		d.cfg.Edition == l1.EditionA1
}

func (d *Decoder) a1RICHAt(sw int64) l1.A1RICH {
	i := int(sw + a1RICHFromSW - d.base)
	if i < 0 || i+l1.A1RICHBits/2 > len(d.syms) {
		return l1.A1RICH{}
	}
	soft := make([]float32, 0, l1.A1RICHBits)
	for _, s := range d.syms[i : i+l1.A1RICHBits/2] {
		x, y := s.Soft()
		soft = append(soft, x, y)
	}
	return l1.DecodeA1RICH(soft)
}

func (d *Decoder) richOKAt(sw int64) bool {
	if d.edition == l1.EditionA1 {
		return d.a1RICHAt(sw).OK
	}
	i := int(sw - swOffsetSymbols - d.base)
	soft := make([]float32, 0, l1.RIEnd-l1.RIStart)
	for _, s := range d.syms[i+l1.RIStart/2 : i+l1.RIEnd/2] {
		x, y := s.Soft()
		soft = append(soft, x, y)
	}
	return l1.DecodeRICH(soft).OK
}

func (d *Decoder) richVerified(sw int64) (ok, a1 bool) {
	if d.edition != l1.EditionA1 {
		n := 0
		soft := make([]float32, 0, l1.RIEnd-l1.RIStart)
		for k := 0; k < syncVerifyFrames; k++ {
			i := int(sw-swOffsetSymbols-d.base) + k*l1.FrameSymbols
			soft = soft[:0]
			for _, s := range d.syms[i+l1.RIStart/2 : i+l1.RIEnd/2] {
				x, y := s.Soft()
				soft = append(soft, x, y)
			}
			if l1.DecodeRICH(soft).OK {
				n++
				if n >= syncVerifyNeed {
					return true, false
				}
			}
		}
	}
	if !d.a1Possible() {
		return false, false
	}
	n := 0
	for k := 0; k < syncVerifyFrames; k++ {
		if d.a1RICHAt(sw + int64(k*l1.FrameSymbols)).OK {
			n++
			if n >= syncVerifyNeed {
				return true, true
			}
		}
	}
	return false, false
}

func (d *Decoder) swOffset() int64 {
	if d.edition == l1.EditionA1 {
		return a1SWOffsetSymbols
	}
	return swOffsetSymbols
}

func (d *Decoder) hardBits(from int64, nbits int) []uint8 {
	i := int(from - d.base)
	nsym := (nbits + 1) / 2
	if i < 0 || i+nsym > len(d.syms) {
		return nil
	}
	out := make([]uint8, 0, nbits)
	for _, s := range d.syms[i : i+nsym] {
		x, y := s.Bits()
		out = append(out, x, y)
	}
	return out[:nbits]
}

func (d *Decoder) run() []Frame {
	var frames []Frame
	for {
		if !d.locked {
			found := false
			first := d.base + swOffsetSymbols
			last := d.base + int64(len(d.syms)) - 16 - syncVerifyFrames*l1.FrameSymbols
			verify := d.huntSyms < syncFallbackSymbols
			for p := first; p <= last; p++ {
				bits := d.hardBits(p, 32)
				if bits == nil {
					break
				}
				sw, e := l1.MatchSyncWord(bits, syncMaxErrAcquire)
				if sw == nil {
					continue
				}
				sw2, _ := l1.MatchSyncWord(d.hardBits(p+l1.FrameSymbols, 32), syncMaxErrConfirm)
				if sw2 == nil || sw2.Kind != sw.Kind || sw2.Uplink != sw.Uplink {
					continue
				}
				if verify {
					ok, a1 := d.richVerified(p)
					if !ok {
						continue
					}
					if a1 && d.edition == l1.EditionAuto {
						d.edition = l1.EditionA1
					}
				}
				d.locked, d.nextSW, d.lastSW, d.misses = true, p, sw, 0
				d.huntSyms = 0
				d.sfPos = -1
				d.sacchBuf, d.rchBuf, d.facchPrev, d.facchWait, d.facchExtra = nil, nil, nil, false, false
				d.nv2Prev, d.nv2PrevM3 = nil, false
				d.resetAssemblers()
				_ = e
				found = true
				break
			}
			if !found {
				if last >= first {
					d.huntSyms += last - first + 1
				}
				keep := len(d.syms) - (swOffsetSymbols + 16 + 1 + syncVerifyFrames*l1.FrameSymbols)
				if keep > 0 {
					d.syms = d.syms[keep:]
					d.base += int64(keep)
				}
				return frames
			}
		}
		start := d.nextSW - d.swOffset()
		end := start + l1.FrameSymbols
		if end+16 > d.base+int64(len(d.syms)) {
			return frames
		}
		bits := d.hardBits(d.nextSW, 32)
		sw, e := l1.MatchSyncWord(bits, syncMaxErrTrack)
		fly := false
		if sw != nil && d.lastSW != nil && d.lastSW.Kind == l1.KindSyncBurst &&
			sw.Kind == l1.KindService && sw.Uplink == d.lastSW.Uplink && (e == 0 || d.richOKAt(d.nextSW)) {
			d.kindCand = nil
		} else if sw != nil && d.lastSW != nil && (sw.Kind != d.lastSW.Kind || sw.Uplink != d.lastSW.Uplink) {
			if e > 0 || d.kindCand != sw {
				d.kindCand = sw
				if e > 0 {
					d.kindCand = nil
				}
				sw = nil
			}
		} else {
			d.kindCand = nil
		}
		if sw == nil {
			d.misses++
			if d.misses > syncLossFrames {
				d.locked = false
				d.huntSyms = 0
				d.Stats.SyncLoss++
				d.sfPos = -1
				keep := int(start - d.base)
				if keep > 0 && keep <= len(d.syms) {
					d.syms = d.syms[keep:]
					d.base = start
				}
				continue
			}
			sw, e, fly = d.lastSW, -1, true
		} else {
			d.misses = 0
			d.lastSW = sw
		}
		frames = append(frames, d.decodeFrame(start, sw, e, fly))
		d.nextSW += l1.FrameSymbols
		keep := int(start - d.base)
		if keep > 0 && keep <= len(d.syms) {
			d.syms = d.syms[keep:]
			d.base = start
		}
	}
}

func (d *Decoder) decodeFrame(start int64, sw *l1.SyncWord, swErr int, fly bool) Frame {
	i := int(start - d.base)
	soft := make([]float32, 0, l1.FrameBits)
	var perr float64
	for _, s := range d.syms[i : i+l1.FrameSymbols] {
		x, y := s.Soft()
		soft = append(soft, x, y)
		perr += math.Abs(s.PhaseError())
	}
	f := Frame{Number: d.frameNo, Symbol: start, TimeSec: float64(start) / dsp.SymbolRate,
		SW: sw, SWErrors: swErr, Flywheel: fly, Kind: sw.Kind, Uplink: sw.Uplink, SFPos: -1,
		PhaseErrDeg: perr / float64(l1.FrameSymbols) * 180 / math.Pi}
	if d.cfg.KeepRaw {
		f.Raw = make([]uint8, l1.FrameBits)
		for i, v := range soft {
			if v < 0 {
				f.Raw[i] = 1
			}
		}
	}
	d.frameNo++
	d.Stats.Frames++
	if fly {
		d.Stats.Flywheel++
	} else {
		d.Stats.SWDetected++
		d.Stats.SWCount[sw.Name]++
	}
	if !fly && sw.SuperframeHead {
		d.sfPos = 0
	} else if d.sfPos >= 0 {
		d.sfPos = (d.sfPos + 1) % l1.SuperframeFrames
	}
	f.SFPos = d.sfPos

	if d.edition == l1.EditionA1 {
		d.decodeA1(&f, soft)
		return f
	}
	f.RICH = l1.DecodeRICH(soft[l1.RIStart:l1.RIEnd])
	if f.RICH.OK {
		d.Stats.RICHOK++
	}
	d.voteEdition(sw, f.RICH)
	if d.a1Possible() && d.edition == l1.EditionAuto && !fly {
		if !f.RICH.OK && l1.DecodeA1RICH(soft[l1.SWStart+20:l1.SWStart+20+l1.A1RICHBits]).OK {
			d.a1Vote++
		} else {
			d.a1Vote = 0
		}
		if d.a1Vote >= a1VoteNeed {
			d.edition = l1.EditionA1
		}
	}

	switch sw.Kind {
	case l1.KindControl:
		if sw.Uplink {
			d.decodeControlUp(&f, soft)
			break
		}
		ds := l1.DescrambleControl(soft, d.scramble)
		cd, nc := d.combPCC.pick(ds, func(x []float32) (bool, bool) {
			u := l1.DecodeControlDown(x)
			return u.OK, l1.IsFiller(u.OK, u.Corrected, l1.AllZero(u.Data))
		})
		u := l1.DecodeControlDown(cd)
		f.Control = &u
		d.Stats.ControlUnits++
		if u.OK {
			d.Stats.ControlOK++
			if nc > 1 {
				d.Stats.Combined++
			}
			d.pushUnit(&f, "PCC", u.W, u.Data)
		}
	case l1.KindSyncBurst:
		init := d.scramble
		if sw.Uplink {
			init = d.uplinkTrafficScramble(f.RICH)
		}
		ds := l1.DescrambleSyncBurst(soft, init)
		sb := l1.DecodeSyncBurst(ds)
		if sw.Uplink {
			sb = d.searchUplinkScramble(&f, soft, sb)
		}
		f.SB = &sb
		d.Stats.SBUnits++
		if sb.OK {
			d.Stats.SBOK++
		}
	case l1.KindService:
		init := d.scramble
		if sw.Uplink {
			init = d.uplinkTrafficScramble(f.RICH)
		}
		ds := l1.DescrambleService(soft, init)
		f.TCHInit = init
		d.decodeService(&f, ds)
	}
	return f
}

func (d *Decoder) SetUplinkScramble(init int) { d.upAssigned = init }

func (d *Decoder) UplinkScrambleAssigned() int { return d.upAssigned }

func (d *Decoder) uplinkTrafficScramble(r l1.RICH) int {
	cc := d.uplinkScramble(r)
	if d.upFound >= 0 {
		return d.upFound
	}
	if d.upAssigned < 0 {
		return cc
	}
	if r.HasMode() && r.D == 2 {
		return cc
	}
	return d.upAssigned
}

const (
	upSearchRatio = 0.95
	upSearchMargin = 0.05
	upSearchVotes = 2
	upSearchBackoff = 4
)

func SearchUplinkScramble(soft []float32) (init int, best, second float64) {
	var tot float64
	for _, x := range soft[l1.SBPIStr:l1.SBPIEnd] {
		tot += math.Abs(float64(x))
	}
	init = -1
	if tot == 0 {
		return
	}
	for s := 1; s < 512; s++ {
		sb := l1.DecodeSyncBurst(l1.DescrambleSyncBurst(soft, s))
		if !sb.OK {
			continue
		}
		switch r := sb.Metric / tot; {
		case r > best:
			init, best, second = s, r, best
		case r > second:
			second = r
		}
	}
	return
}

func (d *Decoder) searchUplinkScramble(f *Frame, soft []float32, cur l1.SyncBurst) l1.SyncBurst {
	if cur.OK {
		var tot float64
		for _, x := range soft[l1.SBPIStr:l1.SBPIEnd] {
			tot += math.Abs(float64(x))
		}
		if tot > 0 && cur.Metric/tot >= upSearchRatio {
			d.upCand, d.upCandN = -1, 0
			return cur
		}
	}
	if d.upSkip > 0 {
		d.upSkip--
		return cur
	}
	s, best, second := SearchUplinkScramble(soft)
	if s < 0 || best < upSearchRatio {
		d.upSkip = upSearchBackoff
		return cur
	}
	if best-second < upSearchMargin {
		return cur
	}
	if s == d.upCand {
		d.upCandN++
	} else {
		d.upCand, d.upCandN = s, 1
	}
	if d.upCandN < upSearchVotes {
		return cur
	}
	if d.upFound != s {
		d.upFound = s
		f.UplinkScrambleFound = s + 1
	}
	return l1.DecodeSyncBurst(l1.DescrambleSyncBurst(soft, s))
}

func (d *Decoder) SetUplinkScrambleFound(init int) {
	d.upFound, d.upCand, d.upCandN, d.upSkip = init, -1, 0, 0
}

func (d *Decoder) UplinkScrambleFound() int { return d.upFound }

func (d *Decoder) uplinkScramble(r l1.RICH) int {
	if r.OK {
		d.upScramble = l1.ColorCodeScramble(r.CC)
	}
	if d.upCtlFound >= 0 {
		return d.upCtlFound
	}
	if d.upScramble < 0 {
		return 0
	}
	return d.upScramble
}

func (d *Decoder) decodeControlUp(f *Frame, soft []float32) {
	init := d.uplinkScramble(f.RICH)
	u := decodeControlUpAt(f.RICH, soft, init)
	u = d.searchUplinkControl(f, soft, init, u)
	f.Control = &u
	d.Stats.UpControlUnits++
	if u.OK {
		d.Stats.UpControlOK++
		d.pushUnit(f, "PCC↑", u.W, u.Data)
	}
}

func decodeControlUpAt(r l1.RICH, soft []float32, init int) l1.ControlUnit {
	if second, ok := l1.UpControlSecond(r); ok {
		return l1.DecodeControlUp(soft, init, second)
	}
	u := l1.DecodeControlUp(soft, init, false)
	if !u.OK {
		if u2 := l1.DecodeControlUp(soft, init, true); u2.OK {
			return u2
		}
	}
	return u
}

const (
	upCtlFillerCorr = 6
	upCtlBadRun = 3
	upCtlBackoff = 8
	upCtlQuietGood = 75
	upCtlConfirm = 6
)

func (d *Decoder) searchUplinkControl(f *Frame, soft []float32, init int, cur l1.ControlUnit) l1.ControlUnit {
	d.upCtlFrames++
	if upCtlGood(cur) {
		d.upCtlBad, d.upCtlLastGood, d.upCtlCand = 0, d.upCtlFrames, -1
		return cur
	}
	if d.upCtlCand >= 0 {
		if u := decodeControlUpAt(f.RICH, soft, d.upCtlCand); upCtlGood(u) {
			d.adoptUplinkControl(f, d.upCtlCand)
			return u
		}
		if d.upCtlCandLeft--; d.upCtlCandLeft <= 0 {
			d.upCtlCand = -1
			d.upCtlSkip = upCtlBackoff
		}
		return cur
	}
	if d.upCtlBad++; d.upCtlBad < upCtlBadRun {
		return cur
	}
	if d.upCtlLastGood > 0 && d.upCtlFrames-d.upCtlLastGood < upCtlQuietGood {
		return cur
	}
	if d.upCtlSkip > 0 {
		d.upCtlSkip--
		return cur
	}
	found := -1
	seconds := []bool{false, true}
	if second, ok := l1.UpControlSecond(f.RICH); ok {
		seconds = []bool{second}
	}
	for _, second := range seconds {
		init, best, next := l1.MatchUpControlFiller(soft, second)
		n := upControlBits(second)
		if init > 0 && best*8 <= n && next*3 >= n {
			found = init
			break
		}
	}
	if found < 0 {
		hits := 0
		for s := 1; s < 512 && hits < 2; s++ {
			for _, second := range seconds {
				if u := l1.DecodeControlUp(soft, s, second); u.OK && u.Corrected <= upCtlFillerCorr {
					found = s
					hits++
					break
				}
			}
		}
		if hits != 1 {
			found = -1
		}
	}
	if found < 0 || found == init {
		d.upCtlSkip = upCtlBackoff
		return cur
	}
	d.upCtlBad = 0
	d.upCtlCand, d.upCtlCandLeft = found, upCtlConfirm
	return cur
}

func upCtlGood(u l1.ControlUnit) bool {
	return u.OK || (l1.AllZero(u.Data) && u.Corrected <= upCtlFillerCorr)
}

func (d *Decoder) adoptUplinkControl(f *Frame, s int) {
	d.upCtlCand, d.upCtlBad, d.upCtlLastGood = -1, 0, d.upCtlFrames
	if s == d.upScramble {
		d.upCtlFound = -1
		return
	}
	if d.upCtlFound != s {
		d.upCtlFound = s
		f.UplinkControlScrambleFound = s + 1
	}
}

func upControlBits(second bool) int {
	if second {
		return 264
	}
	return 200
}

func (d *Decoder) UplinkControlScrambleFound() int { return d.upCtlFound }

func (d *Decoder) decodeService(f *Frame, ds []float32) {
	mid := ds[l1.MidStart:l1.MidEnd]
	duc := directChannel(f.RICH)
	if d.edition != l1.EditionSCPC && f.SFPos >= 0 && !duc {
		switch {
		case f.SFPos == 0:
			d.rchBuf = append(d.rchBuf[:0], mid...)
			f.Frag = append(f.Frag, FragInfo{Channel: "RCH", Have: 1, Need: 2})
		case f.SFPos == 1 && len(d.rchBuf) == 20:
			d.rchBuf = append(d.rchBuf, mid...)
			u := l1.DecodeRCH(d.rchBuf)
			f.RCH = &u
			rchFill := l1.IsFiller(u.OK, u.Corrected, u.Raw == 0)
			d.Stats.RCHUnits++
			if u.OK {
				d.Stats.RCHOK++
			} else if rchFill {
				d.Stats.RCHFiller++
			}
			f.Frag = append(f.Frag, FragInfo{Channel: "RCH", Have: 2, Need: 2, Done: true, OK: u.OK, Metric: u.Metric,
				Corrected: u.Corrected, Filler: rchFill})
			d.rchBuf = nil
		case f.SFPos == 2 || f.SFPos == 10:
			d.sacchBuf = append(d.sacchBuf[:0], mid...)
			d.sacchFrom = f.SFPos
			f.Frag = append(f.Frag, FragInfo{Channel: "SACCH", Have: 1, Need: 8})
		case (f.SFPos > 2 && f.SFPos <= 9 && d.sacchFrom == 2) || (f.SFPos > 10 && d.sacchFrom == 10):
			if len(d.sacchBuf) == 20*(f.SFPos-d.sacchFrom) {
				d.sacchBuf = append(d.sacchBuf, mid...)
				if len(d.sacchBuf) == 160 {
					cd, nc := d.combSACCH.pick(d.sacchBuf, func(x []float32) (bool, bool) {
						u := l1.DecodeSACCH(x)
						return u.OK, l1.IsFiller(u.OK, u.Corrected, l1.AllZero(u.Data))
					})
					u := l1.DecodeSACCH(cd)
					f.SACCH = &u
					sacchFill := l1.IsFiller(u.OK, u.Corrected, l1.AllZero(u.Data))
					d.Stats.SACCHUnits++
					if u.OK {
						d.Stats.SACCHOK++
						if nc > 1 {
							d.Stats.Combined++
						}
						d.pushUnit(f, "SACCH", u.W, u.Data)
					} else if sacchFill {
						d.Stats.SACCHFiller++
					}
					f.Frag = append(f.Frag, FragInfo{Channel: "SACCH", Have: 8, Need: 8, Done: true, OK: u.OK, Metric: u.Metric,
						Corrected: u.Corrected, Filler: sacchFill})
					d.sacchBuf = nil
				} else {
					f.Frag = append(f.Frag, FragInfo{Channel: "SACCH", Have: len(d.sacchBuf) / 20, Need: 8})
				}
			}
		}
	}
	parity := -1
	wasWait := d.facchWait
	if d.facchWait {
		f.FACCHHalf = 2
		code := append(l1.FACCHBits(d.facchPrev, 0), l1.FACCHBits(ds, 1)...)
		if d.facchExtra {
			if u := l1.DecodeFACCH(code); u.OK {
				f.FACCH = &u
				d.Stats.FACCHUnits++
				d.Stats.FACCHOK++
				d.pushUnit(f, "FACCH", u.W, u.Data)
				f.Frag = append(f.Frag, FragInfo{Channel: "FACCH", Have: 2, Need: 2, Done: true, OK: true, Metric: u.Metric,
					Corrected: u.Corrected})
			}
		} else {
			cd, nc := d.combFACCH.pick(code, func(x []float32) (bool, bool) {
				u := l1.DecodeFACCH(x)
				return u.OK, l1.IsFiller(u.OK, u.Corrected, l1.AllZero(u.Data))
			})
			u := l1.DecodeFACCH(cd)
			f.FACCH = &u
			facchFill := l1.IsFiller(u.OK, u.Corrected, l1.AllZero(u.Data))
			d.Stats.FACCHUnits++
			if u.OK {
				d.Stats.FACCHOK++
				if nc > 1 {
					d.Stats.Combined++
				}
				d.pushUnit(f, "FACCH", u.W, u.Data)
			} else if facchFill {
				d.Stats.FACCHFiller++
			}
			f.Frag = append(f.Frag, FragInfo{Channel: "FACCH", Have: 2, Need: 2, Done: true, OK: u.OK, Metric: u.Metric,
				Corrected: u.Corrected, Filler: facchFill})
		}
		parity = 1
		d.facchWait, d.facchPrev = false, nil
	}
	if f.RICH.IsFACCH() || upFormat2FACCH(f) {
		d.facchExtra = wasWait && !d.facchExtra
		if !wasWait {
			f.FACCHHalf = 1
			parity = 0
			f.Frag = append(f.Frag, FragInfo{Channel: "FACCH", Have: 1, Need: 2})
		}
		d.facchPrev = append([]float32(nil), ds...)
		d.facchWait = true
	} else {
		d.facchExtra = false
	}
	if d.nv2PrevM3 && d.nv2Prev != nil {
		code := append(l1.FACCHBits(d.nv2Prev, 0), l1.FACCHBits(ds, 1)...)
		u := l1.DecodeFACCH(code)
		if u.OK {
			f.NV2 = &u
			d.Stats.NV2Units++
			d.Stats.NV2OK++
			if msg := d.asm["NV2"].Push(l2.Unit{First: u.W.First, Last: u.W.Last, WO: u.W.WO, Data: l2.BitsToBytes(u.Data)}); msg != nil {
				f.NV2Msg = msg
			}
		}
	}
	if f.RICH.IsNonVoice2() {
		d.nv2Prev, d.nv2PrevM3 = append([]float32(nil), ds...), true
	} else {
		d.nv2Prev, d.nv2PrevM3 = nil, false
	}
	f.TCH = l1.TCHBits(ds, parity)
	f.TCHSoft = l1.TCHSoft(ds, parity)
}

func upFormat2FACCH(f *Frame) bool {
	return f.Uplink && f.RICH.OK && f.RICH.S == 2 && f.RICH.M == 4
}

const a1VoteNeed = 3

func (d *Decoder) decodeA1(f *Frame, soft []float32) {
	rich, tch1, ok := l1.A1SplitFrame(soft)
	if !ok {
		return
	}
	r := l1.DecodeA1RICH(rich)
	f.A1RICH = &r
	d.Stats.A1Frames++
	if r.OK {
		d.Stats.RICHOK++
	}
	if r.IsVoice() {
		d.Stats.A1Voice++
	}
	f.TCH1Soft = append([]float32(nil), tch1...)
	f.TCH1 = make([]uint8, len(tch1))
	for i, v := range tch1 {
		if v < 0 {
			f.TCH1[i] = 1
		}
	}
}

func (d *Decoder) voteEdition(sw *l1.SyncWord, r l1.RICH) {
	if d.cfg.Edition != l1.EditionAuto || d.edition != l1.EditionAuto {
		return
	}
	switch {
	case sw.FDMAOnly:
		d.edVote += 3
	case r.OK && (r.SF == 3 || r.SF == 7) && !(r.F == 2 || r.F <= 1 || r.F >= 6):
		d.edVote--
	case r.OK && (r.F == 2 || r.F <= 1 || r.F >= 6):
		d.edVote++
	}
	if d.edVote >= 3 {
		d.edition = l1.EditionFDMA
	} else if d.edVote <= -3 {
		d.edition = l1.EditionSCPC
	}
}
