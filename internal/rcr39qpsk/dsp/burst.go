package dsp


import (
	"math"
	"math/cmplx"
)

const (
	burstThr = 0.8
	burstSNR = 4.0
	burstMaxCFO = 1500.0
	burstMaxErr20 = 2
	burstMaxErr32 = 3
	floorAlpha = 2e-4
	slotSyms   = 160
)

type BurstWord struct {
	Bits []uint8
	Pos  int
}

type BurstConfig struct {
	SampleRate float64
	OffsetHz   float64
	Words      []BurstWord
}

type Burst struct {
	Word int
	Pos    float64
	TSec   float64
	Metric float64
	CFOHz  float64
	LevelDB float64
	Diff []complex64
}

func (b Burst) Bits() []uint8 {
	out := make([]uint8, 0, 2*len(b.Diff))
	for _, d := range b.Diff {
		x, y := Symbol{Diff: d}.Bits()
		out = append(out, x, y)
	}
	return out
}

func (b Burst) Soft() []float32 {
	out := make([]float32, 0, 2*len(b.Diff))
	for _, d := range b.Diff {
		x, y := Symbol{Diff: d}.Soft()
		out = append(out, x, y)
	}
	return out
}

type burstWord struct {
	conjU []complex128
	bits  []uint8
	first int
}

type BurstDemod struct {
	cfg   BurstConfig
	nco   *nco
	dec   *decimator
	res   *resampler
	rrc   []float64
	hist  []complex64
	words []burstWord

	buf     []complex64
	pw      []float64
	bufBase int64
	next    int64
	floor   float64
	nFloor  int
	maxLead int
	cands   []burstCand
	lastT   int64
}

func NewBurst(cfg BurstConfig) *BurstDemod {
	d := &BurstDemod{cfg: cfg}
	d.nco = newNCO(cfg.OffsetHz, cfg.SampleRate)
	factor := int(cfg.SampleRate / (2 * FsSym))
	if factor < 1 {
		factor = 1
	}
	d.dec = newDecimator(cfg.SampleRate, factor)
	d.res = newResampler(cfg.SampleRate/float64(factor), FsSym)
	d.rrc = RootRaisedCosine(SamplesPerSymbol, RollOff, rrcSpan)
	d.hist = make([]complex64, len(d.rrc)-1)
	for _, w := range cfg.Words {
		bw := burstWord{first: w.Pos / 2, bits: w.Bits}
		for k := 0; k+1 < len(w.Bits); k += 2 {
			bw.conjU = append(bw.conjU, cmplx.Conj(cmplx.Rect(1, DeltaPhase(w.Bits[k], w.Bits[k+1]))))
		}
		d.words = append(d.words, bw)
		d.maxLead = max(d.maxLead, bw.first)
	}
	d.next = int64(4 * (d.maxLead + 2))
	return d
}

func (d *BurstDemod) Floor() float64 { return d.floor }

func (d *BurstDemod) Process(x []complex64) []Burst {
	x = d.nco.mix(x)
	y := d.res.process(d.dec.process(x))
	if len(y) == 0 {
		return nil
	}
	in := make([]complex64, 0, len(d.hist)+len(y))
	in = append(in, d.hist...)
	in = append(in, y...)
	m := len(d.rrc)
	if len(in) < m {
		d.hist = in
		return nil
	}
	for i := 0; i+m <= len(in); i++ {
		var re, im float64
		for k, h := range d.rrc {
			re += float64(real(in[i+k])) * h
			im += float64(imag(in[i+k])) * h
		}
		v := complex(float32(re), float32(im))
		p := re*re + im*im
		d.buf = append(d.buf, v)
		d.pw = append(d.pw, p)
		switch {
		case d.nFloor < 2000:
			d.nFloor++
			d.floor += (p - d.floor) / float64(d.nFloor)
		case p < burstSNR*d.floor:
			d.floor += (p - d.floor) * floorAlpha
		}
	}
	d.hist = append(d.hist[:0], in[len(in)-m+1:]...)
	return d.scan()
}

func (d *BurstDemod) at(pos float64) complex128 {
	i := pos - float64(d.bufBase)
	ii := int(math.Floor(i))
	if ii < 1 || ii+2 >= len(d.buf) {
		return 0
	}
	f := i - float64(ii)
	return cubic(c128(d.buf[ii-1]), c128(d.buf[ii]), c128(d.buf[ii+1]), c128(d.buf[ii+2]), f)
}

func (d *BurstDemod) corr(w *burstWord, t int64) (float64, float64, complex128) {
	var acc complex128
	var mag, pow float64
	for k, cu := range w.conjU {
		i := int(t-d.bufBase) + 4*k
		a, b := c128(d.buf[i]), c128(d.buf[i-4])
		dd := a * cmplx.Conj(b)
		acc += dd * cu
		mag += cmplx.Abs(dd)
		pow += d.pw[i]
	}
	if mag == 0 {
		return 0, 0, 0
	}
	return cmplx.Abs(acc) / mag, pow / float64(len(w.conjU)), acc
}

func (d *BurstDemod) loud(t int64) bool {
	i := int(t - d.bufBase)
	var pow float64
	for k := 0; k < 10; k++ {
		pow += d.pw[i+4*k]
	}
	return pow/10 > burstSNR*d.floor
}

func (d *BurstDemod) verify(w *burstWord, pos float64, acc complex128) bool {
	rot := cmplx.Phase(acc)
	if math.Abs(rot/(2*math.Pi)*SymbolRate) > burstMaxCFO {
		return false
	}
	derot := cmplx.Rect(1, -rot)
	limit := burstMaxErr20
	if len(w.bits) > 20 {
		limit = burstMaxErr32
	}
	errs := 0
	for k := range w.conjU {
		p := pos + float64(4*k)
		x, y := Symbol{Diff: complex64(d.at(p) * cmplx.Conj(d.at(p-4)) * derot)}.Bits()
		if x != w.bits[2*k] {
			errs++
		}
		if y != w.bits[2*k+1] {
			errs++
		}
		if errs > limit {
			return false
		}
	}
	return true
}

const burstNMS = 600

type burstCand struct {
	t    int64
	word int
	m    float64
}

func (d *BurstDemod) scan() []Burst {
	var out []Burst
	need := int64(4*(slotSyms+4)) + 8
	for ; d.next+need < d.bufBase+int64(len(d.buf)); d.next++ {
		if d.floor <= 0 || !d.loud(d.next) {
			out = append(out, d.finalize(d.next-burstNMS)...)
			continue
		}
		best, bm := -1, 0.0
		for wi := range d.words {
			w := &d.words[wi]
			if int(d.next-d.bufBase) < 4*(w.first+2) {
				continue
			}
			m, p, acc := d.corr(w, d.next)
			if m > burstThr && p > burstSNR*d.floor && m > bm && d.verify(w, float64(d.next), acc) {
				best, bm = wi, m
			}
		}
		if best >= 0 {
			d.cands = append(d.cands, burstCand{t: d.next, word: best, m: bm})
		}
		out = append(out, d.finalize(d.next-burstNMS)...)
	}
	oldest := d.next
	if len(d.cands) > 0 {
		oldest = d.cands[0].t
	}
	keep := int(oldest-d.bufBase) - 4*(d.maxLead+4)
	if keep > 0 && keep <= len(d.buf) {
		d.buf = append(d.buf[:0], d.buf[keep:]...)
		d.pw = append(d.pw[:0], d.pw[keep:]...)
		d.bufBase += int64(keep)
	}
	return out
}

func (d *BurstDemod) finalize(upTo int64) []Burst {
	var out []Burst
	for len(d.cands) > 0 && d.cands[0].t < upTo {
		c := d.cands[0]
		keep := c.t-d.lastT >= burstNMS || d.lastT == 0
		for _, o := range d.cands[1:] {
			if o.t-c.t >= burstNMS {
				break
			}
			if o.m > c.m {
				keep = false
				break
			}
		}
		d.cands = d.cands[1:]
		if !keep {
			continue
		}
		d.lastT = c.t
		out = append(out, d.extract(c))
	}
	return out
}

func (d *BurstDemod) extract(c burstCand) Burst {
	w := &d.words[c.word]
	bm, bpow, bacc := d.corr(w, c.t)
	m0, _, _ := d.corr(w, c.t-1)
	m2, _, _ := d.corr(w, c.t+1)
	frac := 0.0
	if den := m0 - 2*bm + m2; den < 0 {
		frac = math.Max(-0.5, math.Min(0.5, 0.5*(m0-m2)/den))
	}
	pos := float64(c.t) + frac
	rot := cmplx.Phase(bacc)
	start := pos - float64(4*w.first)
	b := Burst{Word: c.word, Pos: start, TSec: start / FsSym, Metric: bm, CFOHz: rot / (2 * math.Pi) * SymbolRate,
		LevelDB: 10 * math.Log10(bpow/d.floor)}
	derot := cmplx.Rect(1, -rot)
	var mag float64
	for k := range w.conjU {
		p := pos + float64(4*k)
		mag += cmplx.Abs(d.at(p) * cmplx.Conj(d.at(p-4)))
	}
	mag /= float64(len(w.conjU))
	if mag <= 0 {
		mag = 1
	}
	raw := make([]complex128, slotSyms)
	var q4 complex128
	for j := range raw {
		p := start + float64(4*j)
		raw[j] = d.at(p) * cmplx.Conj(d.at(p-4))
		if v := raw[j] * derot; cmplx.Abs(v) > 0 {
			u := v / complex(cmplx.Abs(v), 0)
			q4 += u * u * u * u
		}
	}
	if cmplx.Abs(q4) > 0.3*slotSyms {
		res := cmplx.Phase(-q4) / 4
		rot += res
		derot = cmplx.Rect(1, -rot)
		b.CFOHz = rot / (2 * math.Pi) * SymbolRate
	}
	b.Diff = make([]complex64, slotSyms)
	for j, v := range raw {
		b.Diff[j] = complex64(v * derot / complex(mag, 0))
	}
	return b
}
