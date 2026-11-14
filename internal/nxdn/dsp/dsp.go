package dsp

import (
	"math"
)

const (
	SymbolRate = 2400.0
	SamplesPerSymbol = 10
	Fs = SymbolRate * SamplesPerSymbol
	RollOff = 0.2
	Dev3 = 945.0
	Dev1 = 315.0
	ChannelSpacing = 6250.0
	chanCutoff = 3200.0
	chanSharp = 1200.0
	rxSpan = 8
)

type FrontEnd struct {
	fsIn float64
	nco  *nco
	dec  *decimator
	res  *resampler
	rx   []float32
	hist []float32
	prev complex64
	have bool
	pAcc float64
	pN   int
}

func NewFrontEnd(fsIn, offsetHz float64) *FrontEnd {
	factor := int(fsIn / Fs)
	if factor < 1 {
		factor = 1
	}
	fe := &FrontEnd{fsIn: fsIn, nco: newNCO(offsetHz, fsIn), dec: newDecimator(fsIn, factor)}
	fs1 := fsIn / float64(factor)
	if math.Abs(fs1-Fs) > 1e-6 {
		fe.res = newResampler(fs1, Fs)
	}
	fe.rx = ReceiveFilter(SamplesPerSymbol, rxSpan)
	fe.hist = make([]float32, len(fe.rx)-1)
	return fe
}

func (fe *FrontEnd) Delay() int { return len(fe.rx) / 2 }

type Output struct {
	IQ []complex64
	Disc []float32
}

func (fe *FrontEnd) Process(wide []complex64) Output {
	x := fe.dec.process(fe.nco.mix(wide))
	if fe.res != nil {
		x = fe.res.process(x)
	} else {
		x = append([]complex64(nil), x...)
	}
	raw := make([]float32, len(x))
	const k = Fs / (2 * math.Pi)
	for i, v := range x {
		if fe.have {
			d := complex128(v) * complex(float64(real(fe.prev)), -float64(imag(fe.prev)))
			raw[i] = float32(math.Atan2(imag(d), real(d)) * k)
		}
		fe.prev, fe.have = v, true
		fe.pAcc += float64(real(v))*float64(real(v)) + float64(imag(v))*float64(imag(v))
		fe.pN++
	}
	return Output{IQ: x, Disc: fe.filter(raw)}
}

func (fe *FrontEnd) LevelDB() float64 {
	if fe.pN == 0 {
		return math.Inf(-1)
	}
	p := fe.pAcc / float64(fe.pN)
	fe.pAcc, fe.pN = 0, 0
	return 10 * math.Log10(p+1e-30)
}

func (fe *FrontEnd) filter(x []float32) []float32 {
	m := len(fe.rx)
	in := append(fe.hist, x...)
	out := make([]float32, len(x))
	for i := range out {
		seg := in[i : i+m]
		var s float32
		for k, h := range fe.rx {
			s += seg[k] * h
		}
		out[i] = s
	}
	fe.hist = append(fe.hist[:0:0], in[len(in)-(m-1):]...)
	return out
}

func ReceiveFilter(sps, span int) []float32 {
	n := 2*span*sps + 1
	T := 1.0
	fs := float64(sps) / T
	const grid = 4096
	fmax := (1 + RollOff) / (2 * T)
	h := make([]float64, n)
	for i := range h {
		t := float64(i-n/2) / fs
		var s float64
		for g := 0; g <= grid; g++ {
			f := fmax * float64(g) / grid
			w := 2.0
			if g == 0 || g == grid {
				w = 1
			}
			s += w * rxResponse(f, T) * math.Cos(2*math.Pi*f*t)
		}
		win := 0.54 + 0.46*math.Cos(2*math.Pi*float64(i-n/2)/float64(n-1))
		h[i] = s * win
	}
	var sum float64
	for _, v := range h {
		sum += v
	}
	out := make([]float32, n)
	for i, v := range h {
		out[i] = float32(v / sum)
	}
	return out
}

func rxResponse(f, T float64) float64 {
	a := RollOff
	var hf float64
	switch {
	case f <= (1-a)/(2*T):
		hf = 1
	case f <= (1+a)/(2*T):
		hf = math.Cos(math.Pi * T / (4 * a) * (2*f - (1-a)/T))
	default:
		return 0
	}
	x := math.Pi * f * T
	d := 1.0
	if x > 1e-9 {
		d = x / math.Sin(x)
	}
	return hf * d
}


type nco struct {
	step       float64
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
	factor  int
	taps32  []float32
	hist    []complex64
	phase   int
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
	t32 := make([]float32, n)
	for i := range taps {
		t32[i] = float32(taps[i] / sum)
	}
	return &firStage{factor: factor, taps32: t32, hist: make([]complex64, n-1)}
}

func newDecimator(fs float64, factor int) *decimator {
	if factor < 1 {
		factor = 1
	}
	d := &decimator{factor: factor}
	fsOut := fs / float64(factor)
	single := hammingTaps(fs, chanSharp)
	bestD1, bestCost := 1, float64(single)*fsOut
	for d1 := 2; d1 < factor; d1++ {
		if factor%d1 != 0 {
			continue
		}
		fs1 := fs / float64(d1)
		trans1 := fs1 - 2*chanCutoff
		if trans1 <= chanSharp {
			continue
		}
		n1, n2 := blackmanTaps(fs, trans1), hammingTaps(fs1, chanSharp)
		if c := float64(n1)*fs1 + float64(n2)*fsOut; c < bestCost {
			bestD1, bestCost = d1, c
		}
	}
	if bestD1 > 1 {
		fs1 := fs / float64(bestD1)
		d.stages = append(d.stages, newFIR(fs, bestD1, chanCutoff, fs1-2*chanCutoff, winBlackman))
		d.stages = append(d.stages, newFIR(fs1, factor/bestD1, chanCutoff, chanSharp, winHamming))
		return d
	}
	d.stages = append(d.stages, newFIR(fs, factor, chanCutoff, chanSharp, winHamming))
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
	m := len(s.taps32)
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
	taps := s.taps32
	for ; i+m <= len(in); i += s.factor {
		seg := in[i : i+m : i+m]
		var r0, r1, i0, i1 float32
		k := 0
		for ; k+2 <= m; k += 2 {
			a, b := seg[k], seg[k+1]
			r0 += real(a) * taps[k]
			i0 += imag(a) * taps[k]
			r1 += real(b) * taps[k+1]
			i1 += imag(b) * taps[k+1]
		}
		for ; k < m; k++ {
			r0 += real(seg[k]) * taps[k]
			i0 += imag(seg[k]) * taps[k]
		}
		out = append(out, complex(r0+r1, i0+i1))
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

func newResampler(fsIn, fsOut float64) *resampler { return &resampler{ratio: fsIn / fsOut, pos: 1} }

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

func c128(v complex64) complex128 { return complex(float64(real(v)), float64(imag(v))) }

func cubic(p0, p1, p2, p3 complex128, f float64) complex128 {
	a := -0.5*p0 + 1.5*p1 - 1.5*p2 + 0.5*p3
	b := p0 - 2.5*p1 + 2*p2 - 0.5*p3
	c := -0.5*p0 + 0.5*p2
	ff := complex(f, 0)
	return ((a*ff+b)*ff+c)*ff + p1
}

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
	for i := range acc {
		acc[i] /= float64(frames)
	}
	return acc
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
	for size := 2; size <= n; size <<= 1 {
		ang := -2 * math.Pi / float64(size)
		wl := complex(math.Cos(ang), math.Sin(ang))
		for st := 0; st < n; st += size {
			w := complex(1, 0)
			for k := 0; k < size/2; k++ {
				u, v := x[st+k], x[st+k+size/2]*w
				x[st+k], x[st+k+size/2] = u+v, u-v
				w *= wl
			}
		}
	}
}
