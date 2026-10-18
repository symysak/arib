package server


import (
	"github.com/symysak/arib/internal/std-t79/decoder"
	"github.com/symysak/arib/internal/std-t79/l1"
)

const (
	idleWindow    = 250
	idleMinFrames = 50
	idleMaxBits = 12
	idleQuietRun = 25
)

type tchKey [l1.TCHBits / 8]byte

type idleTCH struct {
	ring   []tchKey
	pos    int
	counts map[tchKey]int
	dom    tchKey
	idle   tchKey
	have   bool
	run    int
	speech int
}

func newIdleTCH() *idleTCH { return &idleTCH{counts: map[tchKey]int{}} }

func (d *idleTCH) mark(f decoder.TCHFrame) decoder.TCHFrame {
	if f.FACCH {
		if d.quiet() {
			f.Idle, f.Voice = true, false
		}
		return f
	}
	if !f.Voice || len(f.Bits) != l1.TCHBits {
		return f
	}
	var k tchKey
	copy(k[:], l1.ToBytes(f.Bits))
	d.push(k)
	idle := d.have && hamming(k, d.idle) <= idleMaxBits
	if idle {
		d.run++
		d.speech = 0
	} else if d.speech++; d.speech >= 2 || !d.quiet() {
		d.run = 0
	} else {
		idle = true
	}
	if idle {
		f.Idle, f.Voice = true, false
	}
	return f
}

func (d *idleTCH) quiet() bool { return d.run >= idleQuietRun }

func (d *idleTCH) push(k tchKey) {
	if len(d.ring) < idleWindow {
		d.ring = append(d.ring, k)
	} else {
		old := d.ring[d.pos]
		if d.counts[old]--; d.counts[old] <= 0 {
			delete(d.counts, old)
		}
		d.ring[d.pos] = k
		d.pos = (d.pos + 1) % idleWindow
	}
	d.counts[k]++
	if d.counts[k] > d.counts[d.dom] {
		d.dom = k
	}
	n := len(d.ring)
	if n < idleMinFrames || (d.have && d.dom == d.idle) {
		return
	}
	near := 0
	for _, x := range d.ring {
		if hamming(x, d.dom) <= idleMaxBits {
			near++
		}
	}
	if near*2 >= n {
		d.idle, d.have = d.dom, true
	}
}

func hamming(a, b tchKey) int {
	n := 0
	for i := range a {
		x := a[i] ^ b[i]
		for ; x != 0; x &= x - 1 {
			n++
		}
	}
	return n
}
