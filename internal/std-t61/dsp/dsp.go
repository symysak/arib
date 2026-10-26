package dsp

import (
	"math"
	"math/cmplx"
)

const (
	SymbolRate = 4800.0
	SamplesPerSymbol = 4
	FsSym = SymbolRate * SamplesPerSymbol
	RollOff = 0.2
	rrcSpan = 6
	timingWindow = 128
	timingGain = 0.3
	coarseSamples = 4800
	coarseRangeHz = 4000.0
	coarseStepHz = 250.0
	coarseFFT = 8192
	coarseMinScore = 0.15
	coarseRetryWindows = 4
)

type Config struct {
	SampleRate float64
	OffsetHz float64
	CoarseCheck func(bits []uint8) int
}

type Symbol struct {
	Index int64
	Diff complex64
}

func (s Symbol) Bits() (x, y uint8) {
	if imag(s.Diff) < 0 {
		x = 1
	}
	if real(s.Diff) < 0 {
		y = 1
	}
	return
}

func (s Symbol) Soft() (x, y float32) { return imag(s.Diff), real(s.Diff) }

func (s Symbol) PhaseError() float64 {
	p := cmplx.Phase(complex128(s.Diff))
	return math.Mod(p+4*math.Pi, math.Pi/2) - math.Pi/4
}

type Demod struct {
	cfg  Config
	nco  *nco
	dec  *decimator
	res  *resampler
	rrc  []float64
	rrcH []float64
	hist []complex64

	buf     []complex64
	bufBase int64
	nextPos float64
	symIdx  int64
	prev    complex128
	havePrv bool

	q4        complex128
	q4n       int
	cfoRot    complex128
	cfoCycles float64
	magAcc    float64
	magN      int
	magScale  float64
	tAcc complex128
	tN   int
	phErrAcc float64
	phErrN   int
	coarseBuf  []complex64
	coarseDone bool
	coarseHz   float64
	coarseWin  int
}

func New(cfg Config) *Demod {
	d := &Demod{cfg: cfg, cfoRot: 1, magScale: 1}
	d.nco = newNCO(cfg.OffsetHz, cfg.SampleRate)
	factor := int(cfg.SampleRate / (2 * FsSym))
	if factor < 1 {
		factor = 1
	}
	d.dec = newDecimator(cfg.SampleRate, factor)
	fs1 := cfg.SampleRate / float64(factor)
	d.res = newResampler(fs1, FsSym)
	d.rrc = RootRaisedCosine(SamplesPerSymbol, RollOff, rrcSpan)
	d.rrcH = rrcResponse(coarseFFT, FsSym)
	d.hist = make([]complex64, len(d.rrc)-1)
	d.nextPos = float64(len(d.rrc)/2) + 1
	return d
}

func (d *Demod) CFOHz() float64 { return d.cfoCycles * SymbolRate }

func (d *Demod) CoarseHz() float64 { return d.coarseHz }

func (d *Demod) MeanPhaseErrorDeg() float64 {
	if d.phErrN == 0 {
		return 0
	}
	v := d.phErrAcc / float64(d.phErrN) * 180 / math.Pi
	d.phErrAcc, d.phErrN = 0, 0
	return v
}

func (d *Demod) Process(x []complex64) []Symbol {
	x = d.nco.mix(x)
	y := d.dec.process(x)
	y = d.res.process(y)
	if len(y) == 0 {
		return nil
	}
	if !d.coarseDone {
		d.coarseBuf = append(d.coarseBuf, y...)
		for !d.coarseDone && len(d.coarseBuf) >= coarseSamples {
			if d.coarseWin%coarseRetryWindows == 0 {
				d.coarseAcquire(d.coarseBuf[:coarseSamples])
			}
			d.coarseWin++
			d.coarseBuf = append(d.coarseBuf[:0], d.coarseBuf[coarseSamples:]...)
		}
		if d.coarseDone {
			d.coarseBuf = nil
		}
	}
	in := make([]complex64, 0, len(d.hist)+len(y))
	in = append(in, d.hist...)
	in = append(in, y...)
	m := len(d.rrc)
	if len(in) < m {
		d.hist = in
		return nil
	}
	out := make([]complex64, len(in)-m+1)
	for i := range out {
		var re, im float64
		seg := in[i : i+m]
		for k, h := range d.rrc {
			re += float64(real(seg[k])) * h
			im += float64(imag(seg[k])) * h
		}
		out[i] = complex(float32(re), float32(im))
	}
	d.hist = append(d.hist[:0], in[len(in)-m+1:]...)
	d.buf = append(d.buf, out...)
	return d.drain()
}

func (d *Demod) drain() []Symbol {
	var syms []Symbol
	for {
		i := d.nextPos - float64(d.bufBase)
		ii := int(math.Floor(i))
		if ii+2 >= len(d.buf) {
			break
		}
		if ii-1 < 0 {
			d.nextPos += SamplesPerSymbol
			continue
		}
		frac := i - float64(ii)
		s := cubic(c128(d.buf[ii-1]), c128(d.buf[ii]), c128(d.buf[ii+1]), c128(d.buf[ii+2]), frac)
		for k := ii - SamplesPerSymbol + 1; k <= ii; k++ {
			if k < 0 {
				continue
			}
			v := d.buf[k]
			p := float64(real(v))*float64(real(v)) + float64(imag(v))*float64(imag(v))
			n := d.bufBase + int64(k)
			switch n & 3 {
			case 0:
				d.tAcc += complex(p, 0)
			case 1:
				d.tAcc += complex(0, -p)
			case 2:
				d.tAcc -= complex(p, 0)
			case 3:
				d.tAcc += complex(0, p)
			}
		}
		d.tN++
		d.nextPos += SamplesPerSymbol
		if d.havePrv {
			diff := s * cmplx.Conj(d.prev)
			mag := cmplx.Abs(diff)
			if mag > 0 {
				u := diff / complex(mag, 0)
				d.q4 += u * u * u * u
				d.q4n++
				d.magAcc += mag
				d.magN++
			}
			diff = diff * d.cfoRot / complex(d.magScale, 0)
			sym := Symbol{Index: d.symIdx, Diff: complex64(diff)}
			d.phErrAcc += math.Abs(sym.PhaseError())
			d.phErrN++
			syms = append(syms, sym)
			d.symIdx++
		}
		d.prev, d.havePrv = s, true
		if d.tN >= timingWindow {
			d.updateLoops()
		}
	}
	keep := int(math.Floor(d.nextPos-float64(d.bufBase))) - 2 - SamplesPerSymbol
	if keep > 0 && keep <= len(d.buf) {
		d.buf = append(d.buf[:0], d.buf[keep:]...)
		d.bufBase += int64(keep)
	}
	return syms
}

func (d *Demod) updateLoops() {
	if d.tN > 0 && cmplx.Abs(d.tAcc) > 0 {
		tau := -cmplx.Phase(d.tAcc) / (2 * math.Pi) * SamplesPerSymbol
		cur := math.Mod(d.nextPos, SamplesPerSymbol)
		e := tau - cur
		e = math.Mod(e+SamplesPerSymbol*2.5, SamplesPerSymbol) - SamplesPerSymbol/2
		d.nextPos += timingGain * e
	}
	d.tAcc, d.tN = 0, 0
	if d.q4n > 0 && cmplx.Abs(d.q4) > 0.3*float64(d.q4n) {
		ph := cmplx.Phase(d.q4) - math.Pi
		ph = math.Mod(ph+3*math.Pi, 2*math.Pi) - math.Pi
		cyc := ph / 4 / (2 * math.Pi)
		d.cfoCycles = cyc
		d.cfoRot = cmplx.Exp(complex(0, -2*math.Pi*d.cfoCycles))
	}
	d.q4, d.q4n = 0, 0
	if d.magN > 0 {
		d.magScale = d.magAcc / float64(d.magN)
		if d.magScale <= 0 {
			d.magScale = 1
		}
	}
	d.magAcc, d.magN = 0, 0
}

func (d *Demod) coarseAcquire(buf []complex64) {
	X := make([]complex128, coarseFFT)
	for i, v := range buf {
		X[i] = c128(v)
	}
	fft(X)
	n := int(math.Round(coarseRangeHz / coarseStepHz))
	best, bestM, bestV := 0.0, -1.0, 0
	for i := -n; i <= n; i++ {
		f := float64(i) * coarseStepHz
		m, bits := d.coarseEval(X, f, len(buf))
		if d.cfg.CoarseCheck == nil {
			if m > bestM {
				best, bestM = f, m
			}
			continue
		}
		v := d.cfg.CoarseCheck(bits)
		if v > bestV || (v == bestV && v > 0 && m > bestM) {
			best, bestM, bestV = f, m, v
		}
	}
	if d.cfg.CoarseCheck != nil && bestV <= 0 {
		return
	}
	if d.cfg.CoarseCheck == nil && bestM < coarseMinScore {
		return
	}
	d.coarseDone = true
	d.coarseHz = best
	d.nco.step -= 2 * math.Pi * d.coarseHz / d.cfg.SampleRate
}

func (d *Demod) coarseEval(X []complex128, f float64, n int) (float64, []uint8) {
	N := len(X)
	k := int(math.Round(f / (FsSym / float64(N))))
	Y := make([]complex128, N)
	for b, h := range d.rrcH {
		if h == 0 {
			continue
		}
		Y[b] = X[((b+k)%N+N)%N] * complex(h, 0)
	}
	for i := range Y {
		Y[i] = cmplx.Conj(Y[i])
	}
	fft(Y)
	y := Y[:n]
	for i := range y {
		y[i] = cmplx.Conj(y[i])
	}
	bestM, bestPh := -1.0, 0
	for ph := 0; ph < SamplesPerSymbol; ph++ {
		var q complex128
		cnt := 0
		for i := ph + SamplesPerSymbol; i < n; i += SamplesPerSymbol {
			z := y[i] * cmplx.Conj(y[i-SamplesPerSymbol])
			a := cmplx.Abs(z)
			if a == 0 {
				continue
			}
			u := z / complex(a, 0)
			u2 := u * u
			q += u2 * u2
			cnt++
		}
		if cnt > 0 {
			if m := cmplx.Abs(q) / float64(cnt); m > bestM {
				bestM, bestPh = m, ph
			}
		}
	}
	if d.cfg.CoarseCheck == nil {
		return bestM, nil
	}
	bits := make([]uint8, 0, 2*(n/SamplesPerSymbol))
	for i := bestPh + SamplesPerSymbol; i < n; i += SamplesPerSymbol {
		z := y[i] * cmplx.Conj(y[i-SamplesPerSymbol])
		x, yb := Symbol{Diff: complex64(z)}.Bits()
		bits = append(bits, x, yb)
	}
	return bestM, bits
}

func rrcResponse(n int, fs float64) []float64 {
	h := make([]float64, n)
	f1 := (1 - RollOff) * SymbolRate / 2
	f2 := (1 + RollOff) * SymbolRate / 2
	for b := range h {
		f := float64(b) * fs / float64(n)
		if b > n/2 {
			f = float64(b-n) * fs / float64(n)
		}
		f = math.Abs(f)
		switch {
		case f <= f1:
			h[b] = 1
		case f < f2:
			h[b] = math.Sqrt(0.5 * (1 + math.Cos(math.Pi*(f-f1)/(RollOff*SymbolRate))))
		}
	}
	return h
}

func fft(x []complex128) {
	n := len(x)
	for i, j := 1, 0; i < n; i++ {
		bit := n >> 1
		for ; j&bit != 0; bit >>= 1 {
			j ^= bit
		}
		j ^= bit
		if i < j {
			x[i], x[j] = x[j], x[i]
		}
	}
	for l := 2; l <= n; l <<= 1 {
		ang := -2 * math.Pi / float64(l)
		wl := complex(math.Cos(ang), math.Sin(ang))
		for i := 0; i < n; i += l {
			w := complex(1, 0)
			for k := 0; k < l/2; k++ {
				u, v := x[i+k], x[i+k+l/2]*w
				x[i+k], x[i+k+l/2] = u+v, u-v
				w *= wl
			}
		}
	}
}

func RootRaisedCosine(sps int, alpha float64, span int) []float64 {
	n := 2*span*sps + 1
	h := make([]float64, n)
	var e float64
	for i := 0; i < n; i++ {
		t := float64(i-span*sps) / float64(sps)
		var v float64
		switch {
		case math.Abs(t) < 1e-9:
			v = 1 - alpha + 4*alpha/math.Pi
		case math.Abs(math.Abs(t)-1/(4*alpha)) < 1e-9:
			v = alpha / math.Sqrt2 * ((1+2/math.Pi)*math.Sin(math.Pi/(4*alpha)) + (1-2/math.Pi)*math.Cos(math.Pi/(4*alpha)))
		default:
			v = (math.Sin(math.Pi*t*(1-alpha)) + 4*alpha*t*math.Cos(math.Pi*t*(1+alpha))) /
				(math.Pi * t * (1 - (4*alpha*t)*(4*alpha*t)))
		}
		h[i] = v
		e += v * v
	}
	s := 1 / math.Sqrt(e)
	for i := range h {
		h[i] *= s
	}
	return h
}

func c128(v complex64) complex128 { return complex(float64(real(v)), float64(imag(v))) }

func cubic(p0, p1, p2, p3 complex128, f float64) complex128 {
	return complex(cubic1(real(p0), real(p1), real(p2), real(p3), f),
		cubic1(imag(p0), imag(p1), imag(p2), imag(p3), f))
}

func cubic1(p0, p1, p2, p3, f float64) float64 {
	a := -0.5*p0 + 1.5*p1 - 1.5*p2 + 0.5*p3
	b := p0 - 2.5*p1 + 2*p2 - 0.5*p3
	c := -0.5*p0 + 0.5*p2
	return ((a*f+b)*f+c)*f + p1
}


type nco struct {
	step float64
	ph, stepPh complex128
	stepCached float64
	out        []complex64
}

func newNCO(freqHz, fs float64) *nco { return &nco{step: -2 * math.Pi * freqHz / fs, ph: 1} }

func (o *nco) mix(x []complex64) []complex64 {
	if o.step == 0 {
		return x
	}
	if o.step != o.stepCached {
		o.stepPh, o.stepCached = complex(math.Cos(o.step), math.Sin(o.step)), o.step
	}
	if o.ph == 0 {
		o.ph = 1
	}
	if cap(o.out) < len(x) {
		o.out = make([]complex64, len(x))
	}
	out := o.out[:len(x)]
	ph, st := o.ph, o.stepPh
	for i, v := range x {
		c, s := real(ph), imag(ph)
		out[i] = complex(float32(float64(real(v))*c-float64(imag(v))*s),
			float32(float64(real(v))*s+float64(imag(v))*c))
		ph *= st
	}
	if a := math.Hypot(real(ph), imag(ph)); a > 0 {
		ph /= complex(a, 0)
	}
	o.ph = ph
	return out
}

type decimator struct {
	factor int
	stages []*firStage
}

type firStage struct {
	factor int
	taps   []float64
	hist   []complex64
	phase  int
	in, out []complex64
}

const (
	winHamming = iota
	winBlackman
)

func hammingTaps(fs, trans float64) int {
	n := int(math.Ceil(3.3*fs/trans)) | 1
	if n < 9 {
		n = 9
	}
	return n
}

func blackmanTaps(fs, trans float64) int {
	n := int(math.Ceil(5.5*fs/trans)) | 1
	if n < 9 {
		n = 9
	}
	return n
}

func newFIR(fs float64, factor int, cutoff, trans float64, win int) *firStage {
	n := hammingTaps(fs, trans)
	if win == winBlackman {
		n = blackmanTaps(fs, trans)
	}
	c := cutoff / fs
	taps := make([]float64, n)
	var sum float64
	for i := range taps {
		t := float64(i - n/2)
		var v float64
		if t == 0 {
			v = 2 * c
		} else {
			v = math.Sin(2*math.Pi*c*t) / (math.Pi * t)
		}
		x := 2 * math.Pi * float64(i) / float64(n-1)
		w := 0.54 - 0.46*math.Cos(x)
		if win == winBlackman {
			w = 0.42 - 0.5*math.Cos(x) + 0.08*math.Cos(2*x)
		}
		taps[i] = v * w
		sum += taps[i]
	}
	for i := range taps {
		taps[i] /= sum
	}
	return &firStage{factor: factor, taps: taps, hist: make([]complex64, n-1)}
}

func newDecimator(fs float64, factor int) *decimator {
	if factor <= 1 && fs <= FsSym {
		return &decimator{factor: 1}
	}
	if factor < 1 {
		factor = 1
	}
	d := &decimator{factor: factor}
	const cutoff, sharp = 6000.0, 2500.0
	fsOut := fs / float64(factor)
	single := hammingTaps(fs, sharp)
	bestD1, bestCost := 1, float64(single)*fsOut
	for d1 := 2; d1 < factor; d1++ {
		if factor%d1 != 0 {
			continue
		}
		fs1 := fs / float64(d1)
		trans1 := fs1 - 2*cutoff
		if trans1 <= sharp {
			continue
		}
		n1, n2 := blackmanTaps(fs, trans1), hammingTaps(fs1, sharp)
		if c := float64(n1)*fs1 + float64(n2)*fsOut; c < bestCost {
			bestD1, bestCost = d1, c
		}
	}
	if bestD1 > 1 {
		fs1 := fs / float64(bestD1)
		d.stages = append(d.stages, newFIR(fs, bestD1, cutoff, fs1-2*cutoff, winBlackman))
		d.stages = append(d.stages, newFIR(fs1, factor/bestD1, cutoff, sharp, winHamming))
		return d
	}
	d.stages = append(d.stages, newFIR(fs, factor, cutoff, sharp, winHamming))
	return d
}

func (d *decimator) process(x []complex64) []complex64 {
	for _, st := range d.stages {
		x = st.process(x)
		if len(x) == 0 {
			return nil
		}
	}
	return x
}

func (s *firStage) process(x []complex64) []complex64 {
	m := len(s.taps)
	if need := len(s.hist) + len(x); cap(s.in) < need {
		s.in = make([]complex64, 0, need)
	}
	in := append(s.in[:0], s.hist...)
	in = append(in, x...)
	s.in = in[:0]
	if need := len(in)/s.factor + 1; cap(s.out) < need {
		s.out = make([]complex64, 0, need)
	}
	out := s.out[:0]
	i := s.phase
	for ; i+m <= len(in); i += s.factor {
		var re, im float64
		seg := in[i : i+m]
		for k, h := range s.taps {
			re += float64(real(seg[k])) * h
			im += float64(imag(seg[k])) * h
		}
		out = append(out, complex(float32(re), float32(im)))
	}
	s.out = out[:0]
	consumed := len(in) - (m - 1)
	s.phase = i - consumed
	if s.phase < 0 {
		s.phase = 0
	}
	s.hist = append(s.hist[:0], in[consumed:]...)
	return out
}

type resampler struct {
	ratio float64
	hist  []complex64
	pos   float64
}

func newResampler(fsIn, fsOut float64) *resampler {
	return &resampler{ratio: fsIn / fsOut, pos: 1}
}

func (r *resampler) process(x []complex64) []complex64 {
	in := append(r.hist, x...)
	var out []complex64
	for {
		i := int(math.Floor(r.pos))
		if i+2 >= len(in) {
			break
		}
		if i-1 < 0 {
			r.pos += 1
			continue
		}
		f := r.pos - float64(i)
		out = append(out, complex64(cubic(c128(in[i-1]), c128(in[i]), c128(in[i+1]), c128(in[i+2]), f)))
		r.pos += r.ratio
	}
	keep := int(math.Floor(r.pos)) - 1
	if keep < 0 {
		keep = 0
	}
	if keep > len(in) {
		keep = len(in)
	}
	r.hist = append([]complex64(nil), in[keep:]...)
	r.pos -= float64(keep)
	return out
}

type DDC struct {
	nco   *nco
	dec   *decimator
	fsOut float64
}

func NewDDC(fsIn, offsetHz, fsOut float64) *DDC {
	factor := int(fsIn / fsOut)
	if factor < 1 {
		factor = 1
	}
	return &DDC{nco: newNCO(offsetHz, fsIn), dec: newDecimator(fsIn, factor), fsOut: fsIn / float64(factor)}
}

func (d *DDC) SampleRate() float64 { return d.fsOut }

func (d *DDC) Process(x []complex64) []complex64 { return d.dec.process(d.nco.mix(x)) }

func Spectrum(x []complex64, n int) []float64 {
	if n <= 0 || n&(n-1) != 0 || len(x) < n {
		return nil
	}
	win := make([]float64, n)
	for i := range win {
		win[i] = 0.5 - 0.5*math.Cos(2*math.Pi*float64(i)/float64(n-1))
	}
	acc := make([]float64, n)
	work := make([]complex128, n)
	frames := 0
	for off := 0; off+n <= len(x); off += n / 2 {
		for i := 0; i < n; i++ {
			work[i] = c128(x[off+i]) * complex(win[i], 0)
		}
		fft(work)
		for i, v := range work {
			acc[i] += real(v)*real(v) + imag(v)*imag(v)
		}
		frames++
	}
	if frames == 0 {
		return nil
	}
	for i := range acc {
		acc[i] /= float64(frames)
	}
	return acc
}
