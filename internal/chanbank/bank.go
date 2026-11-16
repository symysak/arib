package chanbank

import (
	"fmt"
	"math"
)

type Spec struct {
	FsIn float64
	Decim int
	Taps []float64
	Overlap int
}

type Block struct {
	Start int64
	X []complex64
}

type Bank struct {
	fs              float64
	d, n, m, l, m0  int
	fwd             *plan
	h               []complex128
	buf             []complex64
	bufStart        int64
	next            int64
	workIn, workOut []complex128
	delay float64
}

func MaxDecim5(fsIn, minOut float64) int {
	d := int(fsIn / minOut)
	for d > 1 && !smooth5(d) {
		d--
	}
	if d < 1 {
		d = 1
	}
	return d
}

func New(s Spec) (*Bank, error) {
	if s.FsIn <= 0 || s.Decim < 2 || !smooth5(s.Decim) {
		return nil, fmt.Errorf("chanbank: 間引き率 %d は 2 以上で 2・3・5 だけで割り切れること", s.Decim)
	}
	if len(s.Taps) == 0 {
		return nil, fmt.Errorf("chanbank: タップが無い")
	}
	ov := s.Overlap
	if ov <= 0 {
		ov = 4
	}
	d := s.Decim
	m0 := (len(s.Taps) - 1 + d - 1) / d
	if m0 < 1 {
		m0 = 1
	}
	m := ov * m0
	for !smooth5(m) {
		m++
	}
	n := m * d
	b := &Bank{fs: s.FsIn, d: d, n: n, m: m, m0: m0, l: n - m0*d, fwd: newPlan(n), delay: float64(len(s.Taps)-1) / 2}
	b.workIn = make([]complex128, n)
	b.workOut = make([]complex128, n)
	b.h = make([]complex128, m)
	for idx := range b.h {
		q := idx
		if q >= m/2 {
			q -= m
		}
		var acc complex128
		for i, t := range s.Taps {
			s, c := math.Sincos(-2 * math.Pi * float64(q) * float64(i) / float64(n))
			acc += complex(t*c, t*s)
		}
		b.h[idx] = acc
	}
	pad := m0 * d
	b.buf = make([]complex64, pad)
	b.bufStart = -int64(pad)
	b.next = -int64(pad)
	return b, nil
}

func (b *Bank) FsIn() float64    { return b.fs }
func (b *Bank) OutRate() float64 { return b.fs / float64(b.d) }

func (b *Bank) Size() (n, m, l int) { return b.n, b.m, b.l }

func (b *Bank) Analyze(x []complex64) []Block {
	b.buf = append(b.buf, x...)
	var out []Block
	for {
		off := int(b.next - b.bufStart)
		if off+b.n > len(b.buf) {
			break
		}
		for i, v := range b.buf[off : off+b.n] {
			b.workIn[i] = complex(float64(real(v)), float64(imag(v)))
		}
		b.fwd.forward(b.workOut, b.workIn)
		X := make([]complex64, b.n)
		for i, v := range b.workOut {
			X[i] = complex64(v)
		}
		out = append(out, Block{Start: b.next, X: X})
		b.next += int64(b.l)
	}
	if drop := int(b.next - b.bufStart); drop > 0 {
		if drop > len(b.buf) {
			drop = len(b.buf)
		}
		n := copy(b.buf, b.buf[drop:])
		b.buf = b.buf[:n]
		b.bufStart += int64(drop)
	}
	return out
}

type Prepared struct {
	Bank   *Bank
	Blocks []Block
}

func (b *Bank) Prepare(x []complex64) any { return Prepared{Bank: b, Blocks: b.Analyze(x)} }

type Channel struct {
	b     *Bank
	k0    int
	resid float64
	inv   *plan
	z, y  []complex128
	work  []complex128
	out   []complex64
}

func (b *Bank) Channel(offHz float64) *Channel {
	df := b.fs / float64(b.n)
	k0 := int(math.Round(offHz / df))
	return &Channel{b: b, k0: k0, resid: offHz - float64(k0)*df, inv: newPlan(b.m),
		z: make([]complex128, b.m), y: make([]complex128, b.m), work: make([]complex128, b.m)}
}

func (c *Channel) OutRate() float64 { return c.b.OutRate() }

func (c *Channel) Process(blocks []Block) []complex64 {
	b := c.b
	n, m, d := b.n, b.m, b.d
	keep := m - b.m0
	if need := keep * len(blocks); cap(c.out) < need {
		c.out = make([]complex64, 0, need)
	}
	out := c.out[:0]
	scale := 1 / float64(n)
	for _, blk := range blocks {
		for idx := 0; idx < m; idx++ {
			q := idx
			if q >= m/2 {
				q -= m
			}
			k := (c.k0 + q) % n
			if k < 0 {
				k += n
			}
			v := blk.X[k]
			c.z[idx] = complex(float64(real(v)), float64(imag(v))) * b.h[idx]
		}
		c.inv.inverse(c.y, c.z, c.work)
		km := (int64(c.k0) % int64(n)) * (blk.Start % int64(n)) % int64(n)
		if km < 0 {
			km += int64(n)
		}
		ph := -2 * math.Pi * float64(km) / float64(n)
		n0 := blk.Start + int64(b.m0*d)
		cyc := c.resid * (float64(n0) - b.delay) / b.fs
		ph -= 2 * math.Pi * (cyc - math.Floor(cyc))
		s, co := math.Sincos(ph)
		rot := complex(co, s) * complex(scale, 0)
		ss, cs := math.Sincos(-2 * math.Pi * c.resid * float64(d) / b.fs)
		step := complex(cs, ss)
		for i := b.m0; i < m; i++ {
			v := c.y[i] * rot
			out = append(out, complex(float32(real(v)), float32(imag(v))))
			rot *= step
		}
	}
	c.out = out[:0]
	return out
}
