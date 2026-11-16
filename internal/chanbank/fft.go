package chanbank

import "math"

type plan struct {
	n       int
	factors []int
	tw      []complex128
}

func smooth5(n int) bool {
	if n <= 0 {
		return false
	}
	for _, p := range []int{2, 3, 5} {
		for n%p == 0 {
			n /= p
		}
	}
	return n == 1
}

func factorize(n int) []int {
	var f []int
	for n%4 == 0 {
		f = append(f, 4)
		n /= 4
	}
	for _, p := range []int{2, 3, 5} {
		for n%p == 0 {
			f = append(f, p)
			n /= p
		}
	}
	return f
}

func newPlan(n int) *plan {
	if !smooth5(n) {
		panic("chanbank: FFT の長さは 2・3・5 だけで割り切れること")
	}
	p := &plan{n: n, factors: factorize(n), tw: make([]complex128, n)}
	for i := range p.tw {
		s, c := math.Sincos(-2 * math.Pi * float64(i) / float64(n))
		p.tw[i] = complex(c, s)
	}
	return p
}

func (p *plan) forward(out, in []complex128) {
	if p.n == 1 {
		out[0] = in[0]
		return
	}
	p.rec(out, in, p.n, 1, 0)
}

func (p *plan) inverse(out, in, work []complex128) {
	for i, v := range in {
		work[i] = complex(real(v), -imag(v))
	}
	p.forward(out, work)
	for i, v := range out {
		out[i] = complex(real(v), -imag(v))
	}
}

func (p *plan) rec(out, in []complex128, n, stride, fi int) {
	if n == 1 {
		out[0] = in[0]
		return
	}
	r := p.factors[fi]
	m := n / r
	for q := 0; q < r; q++ {
		p.rec(out[q*m:(q+1)*m], in[q*stride:], m, stride*r, fi+1)
	}
	step := p.n / n
	tw := p.tw
	switch r {
	case 2:
		for k := 0; k < m; k++ {
			a := out[k]
			b := out[k+m] * tw[k*step]
			out[k], out[k+m] = a+b, a-b
		}
	case 4:
		for k := 0; k < m; k++ {
			t0 := out[k]
			t1 := out[k+m] * tw[k*step]
			t2 := out[k+2*m] * tw[2*k*step]
			t3 := out[k+3*m] * tw[3*k*step]
			a, b := t0+t2, t0-t2
			c, d := t1+t3, t1-t3
			jd := complex(imag(d), -real(d))
			out[k] = a + c
			out[k+m] = b + jd
			out[k+2*m] = a - c
			out[k+3*m] = b - jd
		}
	default:
		var t [5]complex128
		wr := p.n / r
		for k := 0; k < m; k++ {
			for q := 0; q < r; q++ {
				t[q] = out[q*m+k] * tw[q*k*step]
			}
			for s := 0; s < r; s++ {
				var acc complex128
				for q := 0; q < r; q++ {
					acc += t[q] * tw[(q*s%r)*wr]
				}
				out[s*m+k] = acc
			}
		}
	}
}
