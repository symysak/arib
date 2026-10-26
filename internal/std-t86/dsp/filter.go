package dsp

import "math"


func sinc(x float64) float64 {
	if x == 0 {
		return 1
	}
	px := math.Pi * x
	return math.Sin(px) / px
}

func Firwin(numtaps int, cutoff float64) []float64 {
	h := make([]float64, numtaps)
	alpha := 0.5 * float64(numtaps-1)
	sum := 0.0
	for i := 0; i < numtaps; i++ {
		m := float64(i) - alpha
		v := cutoff * sinc(cutoff*m)
		w := 0.54 - 0.46*math.Cos(2*math.Pi*float64(i)/float64(numtaps-1))
		h[i] = v * w
		sum += h[i]
	}
	for i := range h {
		h[i] /= sum
	}
	return h
}

type FIRState struct {
	taps []float64
	z    []complex128
}

func NewFIRState(taps []float64) *FIRState {
	return &FIRState{taps: taps, z: make([]complex128, len(taps)-1)}
}

func (f *FIRState) Process(x []complex64) []complex64 {
	b := f.taps
	n := len(b)
	out := make([]complex64, len(x))
	for i, xv := range x {
		xc := complex(float64(real(xv)), float64(imag(xv)))
		y := complex(b[0], 0)*xc + f.z[0]
		for k := 0; k < n-2; k++ {
			f.z[k] = complex(b[k+1], 0)*xc + f.z[k+1]
		}
		f.z[n-2] = complex(b[n-1], 0) * xc
		out[i] = complex64(y)
	}
	return out
}

type NCO struct {
	fs     float64
	freqHz float64
	phase  float64
}

func NewNCO(freqHz, fs float64) *NCO { return &NCO{fs: fs, freqHz: freqHz} }

func (o *NCO) Mix(x []complex64) []complex64 {
	out := make([]complex64, len(x))
	step := o.freqHz / o.fs
	for i, v := range x {
		ph := o.phase + step*float64(i)
		s, c := math.Sincos(-2 * math.Pi * ph)
		rot := complex(c, s)
		xc := complex(float64(real(v)), float64(imag(v)))
		out[i] = complex64(xc * rot)
	}
	o.phase = math.Mod(o.phase+step*float64(len(x)), 1.0)
	return out
}

func (o *NCO) Retune(freqHz float64) { o.freqHz = freqHz }

type Decimator struct {
	factor int
	taps   []float64
	hist   []complex64
	phase  int
	work   []complex64
}

func NewDecimator(fs float64, factor int) *Decimator {
	d := &Decimator{factor: factor}
	if factor > 1 {
		nyqOut := fs / float64(factor) / 2.0
		cutoff := math.Min(0.8*nyqOut, nyqOut-1000.0)
		numtaps := int(math.Max(31, 6.0*fs/math.Max(nyqOut, 1.0))) | 1
		d.taps = Firwin(numtaps, cutoff/(fs/2.0))
		d.hist = make([]complex64, numtaps-1)
	}
	return d
}

func (d *Decimator) Process(x []complex64) []complex64 {
	if d.factor == 1 {
		return x
	}
	m := len(d.taps)
	need := len(d.hist) + len(x)
	if cap(d.work) < need {
		d.work = make([]complex64, need)
	}
	w := d.work[:need]
	copy(w, d.hist)
	copy(w[len(d.hist):], x)

	out := make([]complex64, 0, (len(x)-d.phase+d.factor-1)/d.factor)
	for n := d.phase; n < len(x); n += d.factor {
		var re, im float64
		base := n + m - 1
		for k := 0; k < m; k++ {
			v := w[base-k]
			re += d.taps[k] * float64(real(v))
			im += d.taps[k] * float64(imag(v))
		}
		out = append(out, complex(float32(re), float32(im)))
	}
	copy(d.hist, w[need-len(d.hist):])
	consumed := len(x) - d.phase
	d.phase = ((-consumed)%d.factor + d.factor) % d.factor
	return out
}

type CubicResampler struct {
	step     float64
	buf      []complex64
	bufAbs   int
	outCount int
}

func NewCubicResampler(fsIn, fsOut float64) *CubicResampler {
	return &CubicResampler{step: fsIn / fsOut}
}

func (r *CubicResampler) Process(x []complex64) []complex64 {
	r.buf = append(r.buf, x...)
	endAbs := r.bufAbs + len(r.buf)
	k0 := r.outCount
	if float64(k0)*r.step < 1.0 {
		k0 = int(math.Ceil(1.0 / r.step))
	}
	kMax := int(math.Floor(float64(endAbs-3) / r.step))
	if kMax < k0 {
		return nil
	}
	out := make([]complex64, 0, kMax-k0+1)
	k := k0
	for ; k <= kMax; k++ {
		t := r.posOf(k)
		i := int(math.Floor(t))
		if i < 1 || i+2 >= len(r.buf) {
			break
		}
		frac := t - float64(i)
		p0 := c128(r.buf[i-1])
		p1 := c128(r.buf[i])
		p2 := c128(r.buf[i+1])
		p3 := c128(r.buf[i+2])
		f2 := frac * frac
		f3 := f2 * frac
		v := 0.5 * (2.0*p1 +
			(-p0+p2)*complex(frac, 0) +
			(2.0*p0-5.0*p1+4.0*p2-p3)*complex(f2, 0) +
			(-p0+3.0*p1-3.0*p2+p3)*complex(f3, 0))
		out = append(out, complex64(v))
	}
	r.outCount = k
	keepLocal := int(math.Floor(r.posOf(k))) - 1
	if keepLocal > len(r.buf) {
		keepLocal = len(r.buf)
	}
	if keepLocal > 0 {
		r.buf = append(r.buf[:0], r.buf[keepLocal:]...)
		r.bufAbs += keepLocal
	}
	return out
}

func (r *CubicResampler) posOf(k int) float64 {
	prod := float64(float64(k) * r.step)
	return prod - float64(r.bufAbs)
}

func c128(v complex64) complex128 {
	return complex(float64(real(v)), float64(imag(v)))
}
