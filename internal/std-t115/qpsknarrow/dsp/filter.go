package dsp

import "math"


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
		s, c := math.Sincos(-2 * math.Pi * (o.phase + step*float64(i)))
		out[i] = v * complex(float32(c), float32(s))
	}
	o.phase += step * float64(len(x))
	o.phase -= math.Floor(o.phase)
	return out
}

func Firwin(numtaps int, cutoff float64) []float64 {
	if numtaps%2 == 0 {
		numtaps++
	}
	cutoff = math.Min(math.Max(cutoff, 1e-6), 1-1e-6)
	h := make([]float64, numtaps)
	m := (numtaps - 1) / 2
	var sum float64
	for i := range h {
		n := i - m
		v := cutoff
		if n != 0 {
			v = math.Sin(math.Pi*cutoff*float64(n)) / (math.Pi * float64(n))
		}
		w := 0.54 - 0.46*math.Cos(2*math.Pi*float64(i)/float64(numtaps-1))
		h[i] = v * w
		sum += h[i]
	}
	for i := range h {
		h[i] /= sum
	}
	return h
}

type Decimator struct {
	factor int
	taps   []float64
	hist   []complex64
	base   int64
	next   int64
}

func NewDecimator(fs float64, factor int) *Decimator {
	d := &Decimator{factor: factor}
	if factor > 1 {
		nyqOut := fs / float64(factor) / 2
		cutoff := math.Min(0.8*nyqOut, nyqOut-1000)
		if cutoff <= 0 {
			cutoff = 0.8 * nyqOut
		}
		numtaps := int(math.Max(31, 6*fs/math.Max(nyqOut, 1))) | 1
		d.taps = Firwin(numtaps, cutoff/(fs/2))
	}
	return d
}

func (d *Decimator) Process(x []complex64) []complex64 {
	if d.factor == 1 {
		return x
	}
	d.hist = append(d.hist, x...)
	m := int64(len(d.taps))
	var out []complex64
	for d.next-d.base+m <= int64(len(d.hist)) {
		off := d.next - d.base
		var re, im float64
		for j := int64(0); j < m; j++ {
			c := d.hist[off+j]
			re += d.taps[j] * float64(real(c))
			im += d.taps[j] * float64(imag(c))
		}
		out = append(out, complex(float32(re), float32(im)))
		d.next += int64(d.factor)
	}
	if drop := d.next - d.base; drop > 0 {
		if drop > int64(len(d.hist)) {
			drop = int64(len(d.hist))
		}
		d.hist = append(d.hist[:0], d.hist[drop:]...)
		d.base += drop
	}
	return out
}

type CubicResampler struct {
	ratio float64
	hist  []complex64
	base  int64
	k     int64
}

func NewCubicResampler(fsIn, fsOut float64) *CubicResampler {
	return &CubicResampler{ratio: fsIn / fsOut}
}

func (r *CubicResampler) Process(x []complex64) []complex64 {
	r.hist = append(r.hist, x...)
	var out []complex64
	for {
		posAbs := float64(r.k) * r.ratio
		i0 := int64(math.Floor(posAbs))
		if i0-1 < r.base {
			r.k++
			continue
		}
		if i0+2-r.base >= int64(len(r.hist)) {
			break
		}
		f := posAbs - float64(i0)
		o := i0 - r.base
		out = append(out, cubicC64(
			r.hist[o-1], r.hist[o], r.hist[o+1], r.hist[o+2], f))
		r.k++
	}
	i0 := int64(math.Floor(float64(r.k) * r.ratio))
	if drop := (i0 - 1) - r.base; drop > 0 {
		if drop > int64(len(r.hist)) {
			drop = int64(len(r.hist))
		}
		r.hist = append(r.hist[:0], r.hist[drop:]...)
		r.base += drop
	}
	return out
}

func cubicC64(p0, p1, p2, p3 complex64, f float64) complex64 {
	re := cubic1(float64(real(p0)), float64(real(p1)), float64(real(p2)), float64(real(p3)), f)
	im := cubic1(float64(imag(p0)), float64(imag(p1)), float64(imag(p2)), float64(imag(p3)), f)
	return complex(float32(re), float32(im))
}

func cubic1(p0, p1, p2, p3, f float64) float64 {
	a := -0.5*p0 + 1.5*p1 - 1.5*p2 + 0.5*p3
	b := p0 - 2.5*p1 + 2*p2 - 0.5*p3
	c := -0.5*p0 + 0.5*p2
	return ((a*f+b)*f+c)*f + p1
}

func NextPow2(v int) int {
	n := 1
	for n < v {
		n <<= 1
	}
	return n
}

func fft(x []complex128) {
	n := len(x)
	if n <= 1 {
		return
	}
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
	for length := 2; length <= n; length <<= 1 {
		s, c := math.Sincos(-2 * math.Pi / float64(length))
		w := complex(c, s)
		for i := 0; i < n; i += length {
			cur := complex(1, 0)
			for j := 0; j < length/2; j++ {
				u := x[i+j]
				v := x[i+j+length/2] * cur
				x[i+j] = u + v
				x[i+j+length/2] = u - v
				cur *= w
			}
		}
	}
}

func FFTShiftedMagnitude(x []complex128, nfft int) []float64 {
	buf := make([]complex128, nfft)
	copy(buf, x)
	fft(buf)
	out := make([]float64, nfft)
	half := nfft / 2
	for i := 0; i < nfft; i++ {
		j := (i + half) % nfft
		out[i] = math.Hypot(real(buf[j]), imag(buf[j]))
	}
	return out
}

func FFTShiftedFreq(nfft int, fs float64) []float64 {
	out := make([]float64, nfft)
	for i := range out {
		out[i] = float64(i-nfft/2) * fs / float64(nfft)
	}
	return out
}
