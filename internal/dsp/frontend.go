package dsp

import "math"

const (
	SymbolRate     = 11250.0
	SymbolsPerSlot = 150
	BitsPerSlot    = 600
	SPS            = 8
	FSBB           = SymbolRate * SPS
	ChannelBW      = 14000.0
)

const minIntermediateFS = 100000.0

const (
	trackBlockS = 0.16
	trackMinProminence = 8.0
	acqMinProminence = 8.0
	maxTrackDevHz = 100.0
	maxTrackVelHz = 5.0
	maxTrackStepHz = 25.0
)

func cfo4th(mf []complex64, fs, searchHz float64) (float64, float64) {
	nfft := 1 << maxInt(14, int(math.Ceil(math.Log2(float64(len(mf))))))
	x := make([]complex128, len(mf))
	for i, v := range mf {
		z := c128(v)
		z2 := z * z
		x[i] = z2 * z2
	}
	spec := FFTShiftedMagnitude(x, nfft)
	fax := FFTShiftedFreq(nfft, fs)
	band := make([]float64, 0, nfft)
	fband := make([]float64, 0, nfft)
	for i, f := range fax {
		if math.Abs(f) < searchHz {
			band = append(band, spec[i])
			fband = append(fband, f)
		}
	}
	if len(band) == 0 {
		return 0, 0
	}
	peak := argmax(band)
	prominence := band[peak] / (median(band) + 1e-12)
	return fband[peak] / 4.0, prominence
}

func EstimateCFO4th(mf []complex64, fs, searchHz float64) float64 {
	f, _ := cfo4th(mf, fs, searchHz)
	return f
}

type cfoCorrector struct {
	fs            float64
	acqLen        int
	block         int
	kp            float64
	ki            float64
	maxStepHz     float64
	maxVelHz      float64
	maxDevHz      float64
	trackSearchHz float64

	cfoHz    float64
	acqHz    float64
	acquired bool
	vel      float64
	nco      *NCO
	acq      []complex64
	blk      []complex64
}

func newCfoCorrector(fs float64, acqLen, block int) *cfoCorrector {
	return &cfoCorrector{
		fs: fs, acqLen: acqLen, block: block,
		kp: 0.5, ki: 0.3,
		maxStepHz: maxTrackStepHz, maxVelHz: maxTrackVelHz, maxDevHz: maxTrackDevHz,
		trackSearchHz: 1200.0,
		nco:           NewNCO(0, fs),
	}
}

func (c *cfoCorrector) reset() {
	c.cfoHz = 0
	c.acqHz = 0
	c.acquired = false
	c.vel = 0
	c.nco = NewNCO(0, c.fs)
	c.acq = nil
	c.blk = nil
}

func (c *cfoCorrector) emitTracked(x []complex64) []complex64 {
	var out []complex64
	for len(x) > 0 {
		take := minInt(len(x), c.block-len(c.blk))
		piece := c.nco.Mix(x[:take])
		out = append(out, piece...)
		c.blk = append(c.blk, piece...)
		x = x[take:]
		if len(c.blk) >= c.block {
			resid, prom := cfo4th(c.blk, c.fs, c.trackSearchHz)
			if prom >= trackMinProminence {
				c.vel = clamp(c.vel+c.ki*resid, -c.maxVelHz, c.maxVelHz)
				step := clamp(c.kp*resid+c.vel, -c.maxStepHz, c.maxStepHz)
				c.cfoHz = clamp(c.cfoHz+step, c.acqHz-c.maxDevHz, c.acqHz+c.maxDevHz)
				c.nco.Retune(c.cfoHz)
			}
			c.blk = c.blk[:0]
		}
	}
	return out
}

func (c *cfoCorrector) process(mf []complex64) []complex64 {
	if c.acquired {
		return c.emitTracked(mf)
	}
	c.acq = append(c.acq, mf...)
	var out []complex64
	for len(c.acq) >= c.acqLen {
		f, prom := cfo4th(c.acq[:c.acqLen], c.fs, 8000.0)
		if prom >= acqMinProminence {
			c.cfoHz = f
			c.acqHz = c.cfoHz
			c.acquired = true
			c.nco.Retune(c.cfoHz)
			break
		}
		out = append(out, c.acq[:c.acqLen]...)
		c.acq = c.acq[c.acqLen:]
	}
	if !c.acquired {
		return out
	}
	pending := c.acq
	c.acq = nil
	return append(out, c.emitTracked(pending)...)
}

type StreamFrontEnd struct {
	fs float64
	f0 float64

	nco        *NCO
	decim      *Decimator
	chanF      *FIRState
	resamp     *CubicResampler
	rrc        *FIRState
	cfo        *cfoCorrector
	cfoEnabled bool
}

func NewStreamFrontEnd(fs, f0, acquireSeconds float64) *StreamFrontEnd {
	decim := maxInt(1, int(math.Floor(fs/minIntermediateFS)))
	fs1 := fs / float64(decim)
	cutoff := ChannelBW / 2.0
	numtaps := int(math.Max(129, 8.0*fs1/cutoff)) | 1
	return &StreamFrontEnd{
		fs:     fs,
		f0:     f0,
		nco:    NewNCO(f0, fs),
		decim:  NewDecimator(fs, decim),
		chanF:  NewFIRState(Firwin(numtaps, cutoff/(fs1/2.0))),
		resamp: NewCubicResampler(fs1, FSBB),
		rrc:    NewFIRState(RRCTaps(0.5, SPS, 10)),
		cfo: newCfoCorrector(FSBB, int(acquireSeconds*FSBB),
			int(trackBlockS*FSBB)),
		cfoEnabled: true,
	}
}

func (f *StreamFrontEnd) CFOHz() (float64, bool) {
	if !f.cfoEnabled {
		return 0, false
	}
	return f.cfo.cfoHz, f.cfo.acquired
}

func (f *StreamFrontEnd) CFOEnabled() bool { return f.cfoEnabled }

func (f *StreamFrontEnd) SetCFOEnabled(enabled bool) {
	if enabled == f.cfoEnabled {
		return
	}
	f.cfoEnabled = enabled
	f.cfo.reset()
}

func (f *StreamFrontEnd) ReacquireCFO() { f.cfo.reset() }

func (f *StreamFrontEnd) toMF(chunk []complex64) []complex64 {
	x := f.nco.Mix(chunk)
	x = f.decim.Process(x)
	x = f.chanF.Process(x)
	x = f.resamp.Process(x)
	if len(x) == 0 {
		return nil
	}
	return f.rrc.Process(x)
}

func (f *StreamFrontEnd) Process(chunk []complex64) []complex64 {
	mf := f.toMF(chunk)
	if !f.cfoEnabled {
		return mf
	}
	return f.cfo.process(mf)
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func clamp(v, lo, hi float64) float64 {
	return math.Max(lo, math.Min(hi, v))
}
