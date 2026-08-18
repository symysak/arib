package dsp

import "math"


const SamplesPerSymbol = 4

type Config struct {
	SampleRate float64
	OffsetHz float64
}

type FrontEnd struct {
	cfg   Config
	nco   *NCO
	dec   *Decimator
	res   *CubicResampler
	rrc   []float64
	hist  []complex64
	fs1   float64
	fsSym float64
}

func NewFrontEnd(cfg Config) *FrontEnd {
	f := &FrontEnd{cfg: cfg}
	f.fsSym = SamplesPerSymbol * SymbolRate
	if cfg.OffsetHz != 0 {
		f.nco = NewNCO(cfg.OffsetHz, cfg.SampleRate)
	}
	factor := int(cfg.SampleRate / (2 * f.fsSym))
	if factor < 1 {
		factor = 1
	}
	f.dec = NewDecimator(cfg.SampleRate, factor)
	f.fs1 = cfg.SampleRate / float64(factor)
	f.res = NewCubicResampler(f.fs1, f.fsSym)
	f.rrc = RootRaisedCosine(f.fsSym, 1.0/SymbolRate, RollOff, 8)
	f.hist = make([]complex64, len(f.rrc)-1)
	return f
}

func (f *FrontEnd) Process(x []complex64) []complex64 {
	if f.nco != nil {
		x = f.nco.Mix(x)
	}
	y := f.dec.Process(x)
	y = f.res.Process(y)
	if len(y) == 0 {
		return nil
	}
	buf := make([]complex64, 0, len(f.hist)+len(y))
	buf = append(buf, f.hist...)
	buf = append(buf, y...)
	m := len(f.rrc)
	if len(buf) < m {
		f.hist = append(f.hist[:0], buf...)
		return nil
	}
	out := make([]complex64, len(buf)-m+1)
	for i := range out {
		var re, im float64
		for j := 0; j < m; j++ {
			c := buf[i+j]
			re += f.rrc[j] * float64(real(c))
			im += f.rrc[j] * float64(imag(c))
		}
		out[i] = complex(float32(re), float32(im))
	}
	f.hist = append(f.hist[:0], buf[len(buf)-(m-1):]...)
	return out
}

func (f *FrontEnd) SampleRateSym() float64 { return f.fsSym }

func RootRaisedCosine(fs, T, alpha float64, span int) []float64 {
	n := int(float64(span) * T * fs)
	h := make([]float64, 2*n+1)
	for i := range h {
		t := float64(i-n) / fs
		switch {
		case math.Abs(t) < 1e-15:
			h[i] = 1 - alpha + 4*alpha/math.Pi
		case math.Abs(math.Abs(t)-T/(4*alpha)) < 1e-12:
			h[i] = alpha / math.Sqrt2 *
				((1+2/math.Pi)*math.Sin(math.Pi/(4*alpha)) -
					(1-2/math.Pi)*math.Cos(math.Pi/(4*alpha)))
		default:
			x := t / T
			num := math.Sin(math.Pi*x*(1-alpha)) +
				4*alpha*x*math.Cos(math.Pi*x*(1+alpha))
			den := math.Pi * x * (1 - math.Pow(4*alpha*x, 2))
			h[i] = num / den
		}
	}
	var e float64
	for _, v := range h {
		e += v * v
	}
	e = math.Sqrt(e)
	for i := range h {
		h[i] /= e
	}
	return h
}

func TimingOffset(y []complex64) float64 {
	var acc complex128
	for n, v := range y {
		p := float64(real(v))*float64(real(v)) + float64(imag(v))*float64(imag(v))
		th := -2 * math.Pi * float64(n) / SamplesPerSymbol
		acc += complex(p*math.Cos(th), p*math.Sin(th))
	}
	tau := -math.Atan2(imag(acc), real(acc)) / (2 * math.Pi)
	if tau < 0 {
		tau += 1
	}
	return tau
}

func SampleSymbols(y []complex64, tau float64) []complex128 {
	step := float64(SamplesPerSymbol)
	off := tau * step
	n := int((float64(len(y)) - 3 - off) / step)
	if n < 0 {
		n = 0
	}
	out := make([]complex128, 0, n)
	for k := 0; k < n; k++ {
		pos := float64(k)*step + off
		i0 := int(math.Floor(pos))
		fr := pos - float64(i0)
		if i0 < 1 || i0+2 >= len(y) {
			continue
		}
		out = append(out, cubic(
			c128(y[i0-1]), c128(y[i0]), c128(y[i0+1]), c128(y[i0+2]), fr))
	}
	return out
}

func c128(v complex64) complex128 {
	return complex(float64(real(v)), float64(imag(v)))
}

func cubic(p0, p1, p2, p3 complex128, f float64) complex128 {
	a := -0.5*p0 + 1.5*p1 - 1.5*p2 + 0.5*p3
	b := p0 - 2.5*p1 + 2*p2 - 0.5*p3
	c := -0.5*p0 + 0.5*p2
	ff := complex(f, 0)
	return ((a*ff+b)*ff+c)*ff + p1
}

func EstimateCFO(sym []complex128) (cfoHz, prominence float64) {
	if len(sym) < 256 {
		return 0, 0
	}
	n := NextPow2(len(sym))
	if n > 16384 {
		n = 16384
	}
	z := make([]complex128, 0, n)
	for i := 0; i < len(sym) && i < n; i++ {
		s := sym[i]
		s2 := s * s
		z = append(z, s2*s2)
	}
	mag := FFTShiftedMagnitude(z, n)
	freq := FFTShiftedFreq(n, SymbolRate)
	best, bi := -1.0, 0
	for i, m := range mag {
		if m > best {
			best, bi = m, i
		}
	}
	d := 0.0
	if bi > 0 && bi < len(mag)-1 {
		y0, y1, y2 := mag[bi-1], mag[bi], mag[bi+1]
		den := y0 - 2*y1 + y2
		if math.Abs(den) > 1e-30 {
			d = 0.5 * (y0 - y2) / den
		}
	}
	bin := SymbolRate / float64(n)
	f4 := freq[bi] + d*bin
	sorted := make([]float64, len(mag))
	copy(sorted, mag)
	med := medianOf(sorted)
	if med <= 0 {
		return f4 / 4, 0
	}
	return f4 / 4, 20 * math.Log10(best/med)
}

func medianOf(v []float64) float64 {
	for i := 1; i < len(v); i++ {
		for j := i; j > 0 && v[j] < v[j-1]; j-- {
			v[j], v[j-1] = v[j-1], v[j]
		}
	}
	if len(v) == 0 {
		return 0
	}
	return v[len(v)/2]
}

func Derotate(sym []complex128, cfoHz float64) []complex128 {
	out := make([]complex128, len(sym))
	w := -2 * math.Pi * cfoHz / SymbolRate
	for i, s := range sym {
		th := w * float64(i)
		out[i] = s * complex(math.Cos(th), math.Sin(th))
	}
	return out
}

func TrackPhase(sym []complex128, bw float64) []complex128 {
	out := make([]complex128, len(sym))
	ph, fr := 0.0, 0.0
	for i, v := range sym {
		z := v * complex(math.Cos(-ph), math.Sin(-ph))
		out[i] = z
		var d complex128 = complex(1, 1)
		if real(z) < 0 {
			d = complex(-1, imag(d))
		}
		if imag(z) < 0 {
			d = complex(real(d), -1)
		}
		e := math.Atan2(imag(z*cconj(d)), real(z*cconj(d)))
		fr += bw * bw * 0.25 * e
		ph += fr + bw*e
	}
	return out
}

func cconj(z complex128) complex128 { return complex(real(z), -imag(z)) }

func Correlate(sym []complex128, tmpl []complex128) []complex128 {
	if len(sym) < len(tmpl) || len(tmpl) == 0 {
		return nil
	}
	var e float64
	for _, t := range tmpl {
		e += real(t)*real(t) + imag(t)*imag(t)
	}
	norm := 1.0 / math.Sqrt(e)
	out := make([]complex128, len(sym)-len(tmpl)+1)
	for i := range out {
		var acc complex128
		for j, t := range tmpl {
			acc += sym[i+j] * cconj(t)
		}
		out[i] = acc * complex(norm, 0)
	}
	return out
}
