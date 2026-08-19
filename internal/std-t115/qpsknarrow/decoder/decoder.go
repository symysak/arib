package decoder

import (
	"math"
	"sort"

	"github.com/symysak/arib/internal/std-t115/qpsknarrow/control"
	"github.com/symysak/arib/internal/std-t115/qpsknarrow/dsp"
	"github.com/symysak/arib/internal/std-t115/qpsknarrow/fec"
)

type FrameKind int

const (
	KindSB0 FrameKind = iota
	KindSC
)

func (k FrameKind) String() string {
	if k == KindSB0 {
		return "SB0"
	}
	return "SC"
}

type Frame struct {
	TimeSec float64
	Kind    FrameKind

	CorrPeak float64
	PhaseDeg float64

	SW string

	CRCOK bool
	Message *control.Message


	TCHCRCOK bool
	TCHMessage *control.Message
	Voice []byte
	AMRSN int
}

type Config struct {
	SampleRate float64
	OffsetHz float64
	ScrambleInit int
	MinCorr float64
	TurboIters int
	BlockSec float64
	OverlapSec float64
	OnScramble func(ScrambleInfo)
	OnBlock func(BlockStats)
	OnConstellation func(Constellation)
	ConstellationSec float64
	ConstellationPoints int
}

type Constellation struct {
	TSec float64
	Points []float32
}

type BlockStats struct {
	TSec float64
	CFOHz   float64
	CFOProm float64
	TimingTau float64
	EVM float64
	LevelDB float64
}

func (c *Config) fill() {
	if c.MinCorr == 0 {
		c.MinCorr = 0.75
	}
	if c.TurboIters == 0 {
		c.TurboIters = 8
	}
	if c.BlockSec <= 0 {
		c.BlockSec = 0.5
	}
	if c.OverlapSec < 0 {
		c.OverlapSec = 0
	}
	if c.OverlapSec == 0 {
		c.OverlapSec = 0.25
	}
	if c.ConstellationSec <= 0 {
		c.ConstellationSec = 0.08
	}
	if c.ConstellationPoints <= 0 {
		c.ConstellationPoints = 250
	}
}

const (
	symPa    = 0
	symCCH1  = dsp.SB0PaBits / 2
	symSW    = symCCH1 + dsp.SB0CCH1Bits/2
	symCCH2  = symSW + dsp.SB0SWBits/2
	symPb    = symCCH2 + dsp.SB0CCH2Bits/2
	symSWLen = dsp.SB0SWBits / 2
	symSCFirst = 0
	symSCLast  = symSW + symSWLen
)

type Decoder struct {
	cfg Config

	paTmpl  []complex128
	sw1Tmpl []complex128
	sw3Tmpl []complex128

	rmSrc  []int
	tchMap []int
	pn900  []uint8

	samplePos int64
	seen      map[int64]bool

	pend      []complex64
	pendStart int64
	blockLen  int
	overlap   int

	processed int64
	lastConstT float64

	finder    *fec.ScrambleFinder
	scramble  int
	pinned    bool
	scrConf   float64
	scrFrames int
}

func New(cfg Config) *Decoder {
	cfg.fill()
	d := &Decoder{
		cfg:     cfg,
		paTmpl:  dsp.BitsToSymbols(dsp.HexToBits(dsp.Pa1Hex)),
		sw1Tmpl: dsp.BitsToSymbols(dsp.HexToBits(dsp.SW1Hex)),
		sw3Tmpl: dsp.BitsToSymbols(dsp.HexToBits(dsp.SW3Hex)),
		rmSrc:   fec.CCHPattern(),
		tchMap:  fec.TCHTxMap(),
		seen:    make(map[int64]bool),
	}
	if cfg.ScrambleInit != 0 {
		d.setScramble(cfg.ScrambleInit, true, 0, 0)
	} else {
		d.finder = fec.NewScrambleFinder()
	}
	return d
}

const scrambleAcquireSec = 0.25

func (d *Decoder) setBlockLen() {
	sec := d.cfg.BlockSec
	if d.finder != nil && scrambleAcquireSec < sec {
		sec = scrambleAcquireSec
	}
	d.blockLen = int(sec * d.cfg.SampleRate)
	d.overlap = int(d.cfg.OverlapSec * d.cfg.SampleRate)
	if d.overlap >= d.blockLen {
		d.overlap = d.blockLen / 2
	}
}

func (d *Decoder) setScramble(init int, pinned bool, conf float64, frames int) {
	d.scramble = init
	d.pinned = pinned
	d.scrConf = conf
	d.scrFrames = frames
	d.pn900 = fec.Scramble(init, dsp.FrameBits)
	if pinned {
		d.finder = nil
	}
	if d.blockLen != 0 {
		d.setBlockLen()
	}
}

func (d *Decoder) ScrambleInit() int { return d.scramble }

func (d *Decoder) ScrambleLocked() bool { return d.scramble != 0 }

func (d *Decoder) ScramblePinned() bool { return d.pinned }

func (d *Decoder) PinScramble(init int) {
	if init == 0 {
		d.scramble, d.pinned, d.scrConf, d.scrFrames = 0, false, 0, 0
		d.pn900 = nil
		d.finder = fec.NewScrambleFinder()
		if d.blockLen != 0 {
			d.setBlockLen()
		}
		return
	}
	d.setScramble(init, true, 0, 0)
}

func (d *Decoder) DecodeBlock(iq []complex64) []Frame {
	s := d.samplePos
	d.samplePos += int64(len(iq))
	return d.decodeAt(iq, s)
}

func (d *Decoder) Feed(iq []complex64) []Frame {
	if d.blockLen == 0 {
		d.setBlockLen()
	}
	d.pend = append(d.pend, iq...)
	var out []Frame
	for len(d.pend) >= d.blockLen {
		blk, ov := d.blockLen, d.overlap
		out = append(out, d.decodeAt(d.pend[:blk], d.pendStart)...)
		adv := blk - ov
		d.pend = append(d.pend[:0], d.pend[adv:]...)
		d.pendStart += int64(adv)
	}
	return out
}

func (d *Decoder) Flush() []Frame {
	if len(d.pend) == 0 {
		return nil
	}
	out := d.decodeAt(d.pend, d.pendStart)
	d.pendStart += int64(len(d.pend))
	d.pend = d.pend[:0]
	return out
}

func (d *Decoder) decodeAt(iq []complex64, startSample int64) []Frame {
	blockStart := startSample
	if end := startSample + int64(len(iq)); end > d.processed {
		d.processed = end
	}

	fe := dsp.NewFrontEnd(dsp.Config{SampleRate: d.cfg.SampleRate, OffsetHz: d.cfg.OffsetHz})
	y := fe.Process(iq)
	if len(y) < 4*dsp.FrameSymbols {
		return nil
	}
	tau := dsp.TimingOffset(y)
	sym := dsp.SampleSymbols(y, tau)
	if len(sym) < dsp.FrameSymbols*2 {
		return nil
	}
	var p float64
	for _, s := range sym {
		p += real(s)*real(s) + imag(s)*imag(s)
	}
	p = math.Sqrt(p / float64(len(sym)) / 2)
	if p <= 0 {
		return nil
	}
	for i := range sym {
		sym[i] /= complex(p, 0)
	}
	cfo, prom := dsp.EstimateCFO(sym)
	if prom > 10 {
		sym = dsp.Derotate(sym, cfo)
	}
	sym = dsp.TrackPhase(sym, 0.01)

	if d.cfg.OnBlock != nil {
		d.cfg.OnBlock(blockStats(sym, blockStart, d.cfg.SampleRate, tau, cfo, prom, p))
	}
	if d.cfg.OnConstellation != nil {
		d.emitConstellation(sym, float64(blockStart)/d.cfg.SampleRate)
	}

	symToSec := func(i int) float64 {
		return float64(blockStart)/d.cfg.SampleRate + float64(i)/dsp.SymbolRate
	}

	thr := d.cfg.MinCorr * math.Sqrt(float64(len(d.paTmpl)))
	type cand struct {
		start int
		kind  FrameKind
		c     complex128
	}
	var cands []cand
	corrPa := dsp.Correlate(sym, d.paTmpl)
	for i, c := range corrPa {
		if cabs(c) >= thr && isLocalMax(corrPa, i) {
			cands = append(cands, cand{i, KindSB0, c})
		}
	}
	corrSW := dsp.Correlate(sym, d.sw3Tmpl)
	for i, c := range corrSW {
		if cabs(c) >= thr && isLocalMax(corrSW, i) {
			cands = append(cands, cand{i - symSW, KindSC, c})
		}
	}
	sort.Slice(cands, func(i, j int) bool { return cabs(cands[i].c) > cabs(cands[j].c) })

	if d.finder != nil {
		for _, cd := range cands {
			if cd.kind != KindSB0 || cd.start < 0 || cd.start+dsp.FrameSymbols > len(sym) {
				continue
			}
			d.feedScramble(sym[cd.start:cd.start+dsp.FrameSymbols],
				math.Atan2(imag(cd.c), real(cd.c)))
		}
		d.tryLockScramble(symToSec(0))
	}

	var out []Frame
	for _, cd := range cands {
		if cd.start < 0 || cd.start+dsp.FrameSymbols > len(sym) {
			continue
		}
		sec := symToSec(cd.start)
		if d.claimed(sec) {
			continue
		}
		ph := math.Atan2(imag(cd.c), real(cd.c))
		var f Frame
		if cd.kind == KindSB0 {
			f = d.decodeSB0(sym[cd.start:cd.start+dsp.FrameSymbols], ph)
		} else {
			f = d.decodeSC(sym[cd.start:cd.start+dsp.FrameSymbols], ph)
		}
		want := dsp.SW1Hex
		if cd.kind == KindSC {
			want = dsp.SW3Hex
		}
		if hexBitErrors(f.SW, want) > maxSWErrors {
			continue
		}
		f.TimeSec = sec
		f.CorrPeak = cabs(cd.c)
		d.markNew(sec)
		out = append(out, f)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].TimeSec < out[j].TimeSec })
	return out
}

func (d *Decoder) emitConstellation(sym []complex128, tStart float64) {
	step := int(d.cfg.ConstellationSec * dsp.SymbolRate)
	if step < 1 {
		step = 1
	}
	maxPts := d.cfg.ConstellationPoints
	for off := 0; off < len(sym); off += step {
		end := off + step
		if end > len(sym) {
			end = len(sym)
		}
		t := tStart + float64(off)/dsp.SymbolRate
		if t <= d.lastConstT {
			continue
		}
		chunk := sym[off:end]
		dec := 1
		if len(chunk) > maxPts {
			dec = (len(chunk) + maxPts - 1) / maxPts
		}
		pts := make([]float32, 0, 2*maxPts)
		for i := 0; i < len(chunk); i += dec {
			pts = append(pts, float32(real(chunk[i])), float32(imag(chunk[i])))
		}
		d.cfg.OnConstellation(Constellation{TSec: t, Points: pts})
		d.lastConstT = t
	}
}

func blockStats(sym []complex128, blockStart int64, fs, tau, cfo, prom, level float64) BlockStats {
	st := BlockStats{
		TSec:      float64(blockStart) / fs,
		CFOHz:     cfo,
		CFOProm:   prom,
		TimingTau: tau,
		LevelDB:   20 * math.Log10(level+1e-12),
	}
	var acc float64
	for _, v := range sym {
		dr := math.Abs(real(v)) - 1
		di := math.Abs(imag(v)) - 1
		acc += dr*dr + di*di
	}
	if len(sym) > 0 {
		st.EVM = math.Sqrt(acc / float64(len(sym)) / 2)
	}
	return st
}

const maxSWErrors = 8

func hexBitErrors(a, b string) int {
	if len(a) != len(b) || len(a) == 0 {
		return 1 << 30
	}
	ab, bb := dsp.HexToBits(a), dsp.HexToBits(b)
	if ab == nil || bb == nil {
		return 1 << 30
	}
	n := 0
	for i := range ab {
		if ab[i] != bb[i] {
			n++
		}
	}
	return n
}

func (d *Decoder) binOf(sec float64) int64 {
	return int64(math.Round(sec / (dsp.FrameDurationSec / 2)))
}

func (d *Decoder) claimed(sec float64) bool { return d.seen[d.binOf(sec)] }

func (d *Decoder) markNew(sec float64) { d.seen[d.binOf(sec)] = true }

func cabs(z complex128) float64 { return math.Hypot(real(z), imag(z)) }

func isLocalMax(c []complex128, i int) bool {
	a := cabs(c[i])
	lo, hi := i-3, i+3
	if lo < 0 {
		lo = 0
	}
	if hi >= len(c) {
		hi = len(c) - 1
	}
	for j := lo; j <= hi; j++ {
		if j != i && cabs(c[j]) > a {
			return false
		}
	}
	return true
}

func (d *Decoder) decodeSB0(fr []complex128, phase float64) Frame {
	f := Frame{Kind: KindSB0, PhaseDeg: phase * 180 / math.Pi, AMRSN: -1}
	rot := complex(math.Cos(-phase), math.Sin(-phase))
	g := make([]complex128, len(fr))
	for i, s := range fr {
		g[i] = s * rot
	}
	sw, llr := cchRaw(g)
	f.SW = sw
	if llr == nil {
		return f
	}
	if d.pn900 == nil {
		return f
	}
	for i := range llr {
		if d.pn900[i] == 1 {
			llr[i] = -llr[i]
		}
	}
	c348 := fec.CollapseLLR(llr, d.rmSrc, fec.CCHTurboOut)
	info := fec.TurboDecode(c348, fec.CCHTurboIn, d.cfg.TurboIters)
	if fec.CRC16(info) == 0 {
		f.CRCOK = true
		if m, err := control.Decode(info[:fec.CCHInfoBits]); err == nil {
			f.Message = m
		}
	}
	return f
}

const scrambleMinConfidence = 0.60

type ScrambleInfo struct {
	Init int
	Confidence float64
	Prominence float64
	Frames int
	TimeSec float64
}

func (d *Decoder) feedScramble(fr []complex128, phase float64) {
	rot := complex(math.Cos(-phase), math.Sin(-phase))
	g := make([]complex128, len(fr))
	for i, v := range fr {
		g[i] = v * rot
	}
	sw, llr := cchRaw(g)
	if llr == nil || hexBitErrors(sw, dsp.SW1Hex) > maxSWErrors {
		return
	}
	d.finder.Add(llr)
}

func (d *Decoder) tryLockScramble(tSec float64) {
	if d.finder.Frames() == 0 {
		return
	}
	r := d.finder.Best()
	if r.Confidence < scrambleMinConfidence {
		return
	}
	d.finder = nil
	d.setScramble(r.Init, false, r.Confidence, r.Frames)
	if d.cfg.OnScramble != nil {
		d.cfg.OnScramble(ScrambleInfo{
			Init: r.Init, Confidence: r.Confidence, Prominence: r.Prominence,
			Frames: r.Frames, TimeSec: tSec,
		})
	}
}

func cchRaw(g []complex128) (string, []float64) {
	sw := dsp.BitsToHex(dsp.SymbolsToBits(g[symSW : symSW+symSWLen]))
	llr := make([]float64, 0, fec.CCHCodedBits)
	llr = append(llr, dsp.SymbolsToLLR(g[symCCH1:symSW], 2)...)
	llr = append(llr, dsp.SymbolsToLLR(g[symCCH2:symPb], 2)...)
	if len(llr) != fec.CCHCodedBits {
		return sw, nil
	}
	return sw, llr
}

func (d *Decoder) decodeSC(fr []complex128, phase float64) Frame {
	f := Frame{Kind: KindSC, PhaseDeg: phase * 180 / math.Pi, AMRSN: -1}
	rot := complex(math.Cos(-phase), math.Sin(-phase))
	g := make([]complex128, len(fr))
	for i, s := range fr {
		g[i] = s * rot
	}
	f.SW = dsp.BitsToHex(dsp.SymbolsToBits(g[symSW : symSW+symSWLen]))
	if d.pn900 == nil {
		return f
	}

	llr := make([]float64, 0, fec.TCHCodedBits)
	llr = append(llr, dsp.SymbolsToLLR(g[symSCFirst:symSW], 2)...)
	llr = append(llr, dsp.SymbolsToLLR(g[symSCLast:], 2)...)
	if len(llr) != fec.TCHCodedBits {
		return f
	}
	for i := range llr {
		if d.pn900[i] == 1 {
			llr[i] = -llr[i]
		}
	}
	full := fec.TCHExpandLLR(llr, d.tchMap)
	info := fec.TurboDecode(full, fec.TCHTurboIn, d.cfg.TurboIters)
	if fec.CRC16(info) != 0 {
		return f
	}
	f.TCHCRCOK = true
	if m, err := control.Decode(info[:fec.TCHInfoBits]); err == nil {
		f.TCHMessage = m
		f.AMRSN = m.Header.AMRWBPlusSN
	}
	if f.TCHMessage != nil && isVoiceType(f.TCHMessage.Header.Type) {
		f.Voice = packBits(info[fec.TCHHeaderBits:fec.TCHInfoBits])
	} else {
		f.AMRSN = -1
	}
	return f
}

func isVoiceType(t int) bool {
	switch t {
	case control.MsgVoiceGroupIndiv, control.MsgVoiceSimultaneous,
		control.MsgVoiceEmergency, control.MsgVoiceContactCall:
		return true
	}
	return false
}

func packBits(bits []uint8) []byte {
	out := make([]byte, (len(bits)+7)/8)
	for i, b := range bits {
		if b&1 == 1 {
			out[i/8] |= 1 << uint(7-i%8)
		}
	}
	return out
}

func (d *Decoder) ProcessedSamples() int64 { return d.processed }

func (d *Decoder) LagSeconds() float64 { return d.cfg.BlockSec + d.cfg.OverlapSec }

func (d *Decoder) SetStartSample(n int64) {
	d.samplePos = n
	d.pendStart = n
	d.processed = n
	d.lastConstT = float64(n)/d.cfg.SampleRate - 1e9
}
