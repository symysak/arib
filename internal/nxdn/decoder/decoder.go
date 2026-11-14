package decoder

import (
	"math"

	"github.com/symysak/arib/internal/nxdn/dsp"
	"github.com/symysak/arib/internal/nxdn/l1"
)

type Config struct {
	SampleRate float64
	OffsetHz   float64
	StartSec float64
}

type Frame struct {
	No int64
	TimeSec float64
	Body [l1.BodySymbols]float32
	FSWErr int
	Inertia bool
	First bool
	Gap int
	DevHz, DCHz float64
	EVM float64
	Eye [][]float32
}

const eyeTraces = 24

type Decoder struct {
	cfg Config
	fe  *dsp.FrontEnd

	buf  []float32
	base int64

	locked   bool
	next     float64
	lastA    float64
	lastB    float64
	inertia  int
	no       int64
	lastPos  float64
	searchAt int64
	levelDB  float64

	IQ []complex64
	WantEye bool
}

func New(cfg Config) *Decoder {
	d := &Decoder{cfg: cfg, fe: dsp.NewFrontEnd(cfg.SampleRate, cfg.OffsetHz), levelDB: math.Inf(-1)}
	return d
}

func (d *Decoder) Locked() bool { return d.locked }

func (d *Decoder) LevelDB() float64 { return d.levelDB }

func (d *Decoder) ProcessedSec() float64 {
	return d.cfg.StartSec + float64(d.base)/dsp.Fs
}

func LagSec() float64 { return 2 * l1.FrameSec }

func (d *Decoder) timeOf(abs float64) float64 {
	return d.cfg.StartSec + (abs-float64(d.fe.Delay()))/dsp.Fs
}

func (d *Decoder) Reset(startSec float64) {
	eye := d.WantEye
	*d = *New(Config{SampleRate: d.cfg.SampleRate, OffsetHz: d.cfg.OffsetHz, StartSec: startSec})
	d.WantEye = eye
}

func (d *Decoder) Feed(wide []complex64) []Frame {
	o := d.fe.Process(wide)
	d.IQ = o.IQ
	if lv := d.fe.LevelDB(); !math.IsInf(lv, -1) {
		d.levelDB = lv
	}
	d.buf = append(d.buf, o.Disc...)
	var out []Frame
	for {
		var f *Frame
		var ok bool
		if d.locked {
			f, ok = d.track()
		} else {
			f, ok = d.acquire()
		}
		if f != nil {
			out = append(out, *f)
		}
		if !ok {
			break
		}
	}
	d.trim()
	return out
}

func (d *Decoder) Flush() []Frame {
	d.buf = append(d.buf, make([]float32, 2*frameLen+searchWin+64)...)
	var out []Frame
	for i := 0; i < 8; i++ {
		var f *Frame
		var ok bool
		if d.locked {
			f, ok = d.track()
		} else {
			f, ok = d.acquire()
		}
		if f != nil && !f.Inertia {
			out = append(out, *f)
		}
		if !ok {
			break
		}
	}
	return out
}

func (d *Decoder) avail(abs float64, n int) bool {
	return int64(math.Ceil(abs))+int64(n)+2 <= d.base+int64(len(d.buf))
}

func (d *Decoder) rel(abs float64) float64 { return abs - float64(d.base) }

func (d *Decoder) trim() {
	keepFrom := d.searchAt
	if d.locked {
		keepFrom = int64(d.next) - searchWin - 4*sps
	}
	if keepFrom <= d.base {
		return
	}
	n := keepFrom - d.base
	if n > int64(len(d.buf)) {
		n = int64(len(d.buf))
	}
	d.buf = append(d.buf[:0], d.buf[n:]...)
	d.base += n
}

func (d *Decoder) acquire() (*Frame, bool) {
	if d.searchAt < d.base {
		d.searchAt = d.base
	}
	need := frameLen + l1.FSWSymbols*sps + trackWin
	for d.avail(float64(d.searchAt), need) {
		p := int(d.searchAt - d.base)
		d.searchAt++
		r := corrAt(d.buf, p)
		if r < acqMinR || corrAt(d.buf, p+1) > r || (p > 0 && corrAt(d.buf, p-1) > r) {
			continue
		}
		_, pf, r := peak(d.buf, max(p-1, 0), p+1)
		a, b := fit(d.buf, pf)
		if a < minDev || a > maxDev {
			continue
		}
		e := fswErrors(d.buf, pf, a, b)
		if e > acqMaxErr {
			continue
		}
		if !(e == 0 && r >= soloR) {
			_, qf, _ := peak(d.buf, int(math.Round(pf))+frameLen-trackWin, int(math.Round(pf))+frameLen+trackWin)
			a2, b2 := fit(d.buf, qf)
			if a2 < minDev || a2 > maxDev || fswErrors(d.buf, qf, a2, b2) > confirmMaxErr {
				continue
			}
		}
		abs := float64(d.base) + pf
		d.locked, d.inertia = true, 0
		f := d.read(abs, a, b, e)
		f.First = true
		d.next = abs + frameLen
		d.lastPos = abs
		return f, true
	}
	return nil, false
}

func (d *Decoder) track() (*Frame, bool) {
	if !d.avail(d.next+float64(searchWin), frameLen) {
		return nil, false
	}
	c := int(math.Round(d.rel(d.next)))
	_, pf, _ := peak(d.buf, c-trackWin, c+trackWin)
	a, b := fit(d.buf, pf)
	e := 10
	if a >= minDev && a <= maxDev {
		e = fswErrors(d.buf, pf, a, b)
	}
	if e > trackMaxErr {
		_, qf, r := peak(d.buf, max(c-searchWin, 0), c+searchWin)
		if r >= acqMinR {
			a2, b2 := fit(d.buf, qf)
			if a2 >= minDev && a2 <= maxDev {
				if e2 := fswErrors(d.buf, qf, a2, b2); e2 <= acqMaxErr {
					pf, a, b, e = qf, a2, b2, e2
				}
			}
		}
	}
	abs := float64(d.base) + pf
	var f *Frame
	if e <= trackMaxErr {
		d.inertia = 0
		f = d.read(abs, a, b, e)
	} else {
		d.inertia++
		if d.inertia > maxInertia {
			d.locked = false
			d.searchAt = int64(d.next) - searchWin
			return nil, true
		}
		abs = d.next
		f = d.read(abs, d.lastA, d.lastB, 10)
		f.Inertia = true
	}
	if g := int(math.Round((abs-d.lastPos)/frameLen)) - 1; g > 0 {
		f.Gap = g
	}
	d.lastPos = abs
	d.next = abs + frameLen
	return f, true
}

func (d *Decoder) read(abs, a, b float64, fswErr int) *Frame {
	if fswErr <= trackMaxErr {
		d.lastA, d.lastB = a, b
	}
	f := &Frame{No: d.no, TimeSec: d.timeOf(abs), FSWErr: fswErr, DevHz: a, DCHz: b}
	d.no++
	p := d.rel(abs)
	var ev float64
	for i := range f.Body {
		v := float32((interp(d.buf, p+float64((l1.FSWSymbols+i)*sps)) - b) / a)
		f.Body[i] = v
		e := float64(v) - float64(Slice(float64(v)))
		ev += e * e
	}
	f.EVM = math.Sqrt(ev / float64(len(f.Body)))
	if d.WantEye {
		for k := 0; k < eyeTraces; k++ {
			at := p + float64((l1.FSWSymbols+k*l1.BodySymbols/eyeTraces)*sps)
			tr := make([]float32, 2*sps+1)
			for j := range tr {
				tr[j] = float32((interp(d.buf, at+float64(j-sps)) - b) / a)
			}
			f.Eye = append(f.Eye, tr)
		}
	}
	return f
}
