package watch


import (
	"github.com/symysak/arib/internal/bitpack"
	dec "github.com/symysak/arib/internal/std-t61/decoder"
)

const (
	idleWindow    = 250
	idleMinFrames = 50
	idleMaxBits = 40
	idleQuietRun = 25
)

type tchKey [32]byte

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

func (d *idleTCH) mark(f dec.Frame) dec.Frame {
	if !f.RICH.OK || !f.RICH.IsVoice() {
		return f
	}
	if f.FACCHHalf != 0 || len(f.TCH) != 256 {
		f.TCHIdle = d.quiet()
		return f
	}
	var k tchKey
	copy(k[:], bitpack.ToBytes(f.TCH))
	d.push(k)
	f.TCHIdle = d.have && hamming(k, d.idle) <= idleMaxBits
	if f.TCHIdle {
		d.run++
		d.speech = 0
	} else if d.speech++; d.speech >= 2 || !d.quiet() {
		d.run = 0
	} else {
		f.TCHIdle = true
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
