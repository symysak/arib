package server

import (
	"encoding/binary"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sync"

	"github.com/symysak/stdt86/internal/std-t115/qpsknarrow/dsp"
)

const (
	iqTargetRate = 8 * dsp.SymbolRate
	iqPrerollSec = 5.0
	iqTailSec = 2.0
	iqMaxSec = 600.0
)

type iqRecorder struct {
	dir   string
	stamp string
	fsOut float64

	nco *dsp.NCO
	dec *dsp.Decimator

	mu sync.Mutex
	ring    []complex64
	ringMax int

	active bool
	id     int
	target string
	buf    []complex64
	stopAt int64
	pos int64

	paths map[int]string
	done  []iqDone

	OnDone func(id int, path string, sec float64, err error)
}

func iqDecimation(fsIn float64) int {
	max := int(fsIn / iqTargetRate)
	if max < 1 {
		return 1
	}
	for f := max; f >= 1; f-- {
		if math.Mod(fsIn, float64(f)) == 0 && math.Mod(fsIn/float64(f), 1) == 0 {
			return f
		}
	}
	return max
}

func newIQRecorder(dir, stamp string, fsIn, offsetHz, lagSec float64) *iqRecorder {
	r := &iqRecorder{dir: dir, stamp: stamp, paths: map[int]string{}}
	if dir == "" || fsIn <= 0 {
		r.dir = ""
		return r
	}
	factor := iqDecimation(fsIn)
	r.fsOut = fsIn / float64(factor)
	if offsetHz != 0 {
		r.nco = dsp.NewNCO(offsetHz, fsIn)
	}
	r.dec = dsp.NewDecimator(fsIn, factor)
	r.ringMax = int((iqPrerollSec + math.Max(lagSec, 0)) * r.fsOut)
	return r
}

func (r *iqRecorder) Enabled() bool { return r != nil && r.dir != "" }

func (r *iqRecorder) SampleRate() float64 {
	if !r.Enabled() {
		return 0
	}
	return r.fsOut
}

func (r *iqRecorder) Feed(x []complex64) {
	if !r.Enabled() || len(x) == 0 {
		return
	}
	y := x
	if r.nco != nil {
		y = r.nco.Mix(y)
	}
	y = r.dec.Process(y)
	if len(y) == 0 {
		return
	}
	r.mu.Lock()
	r.pos += int64(len(y))

	if r.active {
		if limit := int(iqMaxSec * r.fsOut); len(r.buf) < limit {
			if n := limit - len(r.buf); n < len(y) {
				r.buf = append(r.buf, y[:n]...)
			} else {
				r.buf = append(r.buf, y...)
			}
		}
		if r.stopAt > 0 && r.pos >= r.stopAt {
			r.finishLocked()
		}
		r.mu.Unlock()
		r.notify()
		return
	}
	r.ring = append(r.ring, y...)
	if len(r.ring) > r.ringMax {
		r.ring = append(r.ring[:0], r.ring[len(r.ring)-r.ringMax:]...)
	}
	r.mu.Unlock()
}

func (r *iqRecorder) Start(id int, target string) {
	if !r.Enabled() {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.active {
		return
	}
	r.active = true
	r.id = id
	r.target = target
	r.stopAt = 0
	r.buf = append([]complex64(nil), r.ring...)
	r.ring = r.ring[:0]
	r.paths[id] = filepath.Join(r.dir,
		fmt.Sprintf("%s_broadcast%d%s_iq.wav", r.stamp, id, targetPart(target)))
}

func (r *iqRecorder) Stop() {
	if !r.Enabled() {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.active || r.stopAt > 0 {
		return
	}
	r.stopAt = r.pos + int64(iqTailSec*r.fsOut)
}

func (r *iqRecorder) Flush() {
	if !r.Enabled() {
		return
	}
	r.mu.Lock()
	if r.active {
		r.finishLocked()
	}
	r.mu.Unlock()
	r.notify()
}

func (r *iqRecorder) finishLocked() {
	buf, id, target := r.buf, r.id, r.target
	r.active = false
	r.buf = nil
	r.stopAt = 0
	_ = target
	path := r.paths[id]
	if len(buf) == 0 || path == "" {
		delete(r.paths, id)
		return
	}
	err := writeIQWav(path, buf, int(math.Round(r.fsOut)))
	if err != nil {
		delete(r.paths, id)
	}
	r.done = append(r.done, iqDone{id, path, float64(len(buf)) / r.fsOut, err})
}

type iqDone struct {
	id   int
	path string
	sec  float64
	err  error
}

func (r *iqRecorder) notify() {
	r.mu.Lock()
	d := r.done
	r.done = nil
	fn := r.OnDone
	r.mu.Unlock()
	if fn == nil {
		return
	}
	for _, v := range d {
		fn(v.id, v.path, v.sec, v.err)
	}
}

func (r *iqRecorder) Path(id int) string {
	if !r.Enabled() {
		return ""
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.paths[id]
}

func writeIQWav(path string, x []complex64, rate int) error {
	var peak float32
	for _, v := range x {
		if a := absf32(real(v)); a > peak {
			peak = a
		}
		if a := absf32(imag(v)); a > peak {
			peak = a
		}
	}
	scale := 1.0
	if peak > 0 {
		scale = 32000.0 / float64(peak)
	}
	n := len(x) * 4
	b := make([]byte, 0, 44+n)
	le32 := func(v uint32) { b = binary.LittleEndian.AppendUint32(b, v) }
	le16 := func(v uint16) { b = binary.LittleEndian.AppendUint16(b, v) }
	b = append(b, "RIFF"...)
	le32(uint32(36 + n))
	b = append(b, "WAVEfmt "...)
	le32(16)
	le16(1)
	le16(2)
	le32(uint32(rate))
	le32(uint32(rate * 4))
	le16(4)
	le16(16)
	b = append(b, "data"...)
	le32(uint32(n))
	for _, v := range x {
		le16(uint16(clamp16(float64(real(v)) * scale)))
		le16(uint16(clamp16(float64(imag(v)) * scale)))
	}
	return os.WriteFile(path, b, 0o644)
}

func absf32(v float32) float32 {
	if v < 0 {
		return -v
	}
	return v
}

func clamp16(v float64) int16 {
	if math.IsNaN(v) {
		return 0
	}
	if v > 32767 {
		return 32767
	}
	if v < -32768 {
		return -32768
	}
	return int16(math.Round(v))
}
