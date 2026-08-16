package dsp

import (
	"math"
	"math/cmplx"
	"sort"
)


const SWSym = 69

var swHex = []struct {
	Name string
	Hex  string
}{
	{"S1", "000a0a00a0"},
	{"S3", "00a000aaaa"},
	{"S5", "00a0aaaaa0"},
	{"S6", "0000aa0a0a"},
}

var SWNames = func() []string {
	out := make([]string, len(swHex))
	for i, s := range swHex {
		out[i] = s.Name
	}
	return out
}()

var SWTemplates = func() [][]complex64 {
	out := make([][]complex64, len(swHex))
	for i, s := range swHex {
		bits := make([]uint8, 0, len(s.Hex)*4)
		for _, c := range s.Hex {
			var v int
			switch {
			case c >= '0' && c <= '9':
				v = int(c - '0')
			case c >= 'a' && c <= 'f':
				v = int(c-'a') + 10
			default:
				panic("同期ワードの hex が不正です")
			}
			bits = append(bits, uint8((v>>3)&1), uint8((v>>2)&1), uint8((v>>1)&1), uint8(v&1))
		}
		out[i] = BitsToSymbols(bits)
	}
	return out
}()

var (
	backSamples    = SWSym * SPS
	fwdSamples     = (SymbolsPerSlot - SWSym) * SPS
	templateSpan   = (len(SWTemplates[0])-1)*SPS + 1
	minSep         = SymbolsPerSlot*SPS - 40
	templateEnergy = swTemplateEnergies()
)

func swTemplateEnergies() []float64 {
	out := make([]float64, len(SWTemplates))
	for i, t := range SWTemplates {
		s := 0.0
		for _, v := range t {
			s += float64(real(v))*float64(real(v)) + float64(imag(v))*float64(imag(v))
		}
		out[i] = s
	}
	return out
}

func syncMetric(mf []complex64, tmpl []complex64, tEnergy float64) []float64 {
	span := (len(tmpl)-1)*SPS + 1
	n := len(mf) - span
	if n <= 0 {
		return nil
	}
	corr := make([]complex128, n)
	en := make([]float64, n)
	for k, t := range tmpl {
		ct := cmplx.Conj(c128(t))
		base := k * SPS
		for i := 0; i < n; i++ {
			v := c128(mf[base+i])
			corr[i] += v * ct
			en[i] += real(v)*real(v) + imag(v)*imag(v)
		}
	}
	out := make([]float64, n)
	for i := range out {
		out[i] = cabs(corr[i]) / (math.Sqrt(en[i]*tEnergy) + 1e-9)
	}
	return out
}

func extractSlot(mf []complex64, syncSample int, tmpl []complex64) []complex64 {
	first := syncSample - SWSym*SPS
	last := first + (SymbolsPerSlot-1)*SPS
	if first < 0 || last >= len(mf) {
		return nil
	}
	slot := make([]complex128, SymbolsPerSlot)
	var p float64
	for i := 0; i < SymbolsPerSlot; i++ {
		v := c128(mf[first+i*SPS])
		slot[i] = v
		p += real(v)*real(v) + imag(v)*imag(v)
	}
	scale := 1.0 / (math.Sqrt(p/float64(SymbolsPerSlot)) + 1e-12)
	for i := range slot {
		slot[i] *= complex(scale, 0)
	}

	const nfft = 4096
	pow4 := make([]complex128, SymbolsPerSlot)
	for i, v := range slot {
		v2 := v * v
		pow4[i] = v2 * v2
	}
	spec := FFTShiftedMagnitude(pow4, nfft)
	fb := FFTShiftedFreq(nfft, 1.0)
	bestIdx, bestVal := -1, -1.0
	for i, f := range fb {
		if math.Abs(f) < 0.05 && spec[i] > bestVal {
			bestIdx, bestVal = i, spec[i]
		}
	}
	cyc := 0.0
	if bestIdx >= 0 {
		cyc = fb[bestIdx] / 4.0
	}
	for i := range slot {
		s, c := math.Sincos(-2 * math.Pi * cyc * float64(i))
		slot[i] *= complex(c, s)
	}

	var acc complex128
	for k, t := range tmpl {
		acc += slot[SWSym+k] * cmplx.Conj(c128(t))
	}
	ang := math.Atan2(imag(acc), real(acc))
	s, c := math.Sincos(-ang)
	rot := complex(c, s)
	out := make([]complex64, SymbolsPerSlot)
	for i, v := range slot {
		out[i] = complex64(v * rot)
	}
	return out
}

type DetectedBurst struct {
	Pos     int
	SW      string
	Slot    []complex64
	Corr    float64
	EVM     float64
	PowerDB float64
}

const (
	squelchDB = 25.0
	squelchDecayDBPerS = 0.25
)

type SlotTracker struct {
	SyncThresh float64
	SquelchEnabled bool

	buf      []complex64
	bufAbs   int
	nextScan int
	lastPos  int
	pRef     float64
	pRefPos  int
}

func NewSlotTracker(syncThresh float64) *SlotTracker {
	return &SlotTracker{
		SyncThresh:     syncThresh,
		SquelchEnabled: true,
		nextScan:       backSamples,
		lastPos:        -10 * minSep,
	}
}

func (s *SlotTracker) FinalizedPos() int { return s.nextScan }

func (s *SlotTracker) squelchOK(pos int, power float64) bool {
	dt := float64(maxInt(0, pos-s.pRefPos)) / FSBB
	ref := s.pRef * math.Pow(10, -squelchDecayDBPerS*dt/10.0)
	ok := power > ref*math.Pow(10, -squelchDB/10.0)
	s.pRef = math.Max(ref, power)
	s.pRefPos = pos
	return ok || !s.SquelchEnabled
}

func (s *SlotTracker) Process(mfChunk []complex64) []DetectedBurst {
	if len(mfChunk) > 0 {
		s.buf = append(s.buf, mfChunk...)
	}
	bufEnd := s.bufAbs + len(s.buf)
	emitTo := minInt(bufEnd-templateSpan-minSep, bufEnd-fwdSamples)
	if emitTo <= s.nextScan {
		return nil
	}

	wa := maxInt(s.bufAbs, s.nextScan-minSep)
	wb := emitTo + minSep
	seg := s.buf[wa-s.bufAbs : wb-s.bufAbs+templateSpan]

	metrics := make([][]float64, len(SWTemplates))
	n := wb - wa
	for i, t := range SWTemplates {
		metrics[i] = syncMetric(seg, t, templateEnergy[i])
		n = minInt(n, len(metrics[i]))
	}
	if n <= 0 {
		return nil
	}
	mm := make([]float64, n)
	which := make([]int, n)
	for i := 0; i < n; i++ {
		best, bi := metrics[0][i], 0
		for k := 1; k < len(metrics); k++ {
			if metrics[k][i] > best {
				best, bi = metrics[k][i], k
			}
		}
		mm[i], which[i] = best, bi
	}

	maxf := maximumFilter1D(mm, minSep)
	var out []DetectedBurst
	for i := 0; i < n; i++ {
		if mm[i] < s.SyncThresh || mm[i] < maxf[i] {
			continue
		}
		pos := wa + i
		if pos < s.nextScan || pos >= emitTo {
			continue
		}
		if pos-s.lastPos <= minSep {
			continue
		}
		if len(out) > 0 && pos-out[len(out)-1].Pos <= minSep {
			continue
		}
		local := pos - s.bufAbs
		if local < backSamples {
			continue
		}
		hi := minInt(local+fwdSamples, len(s.buf))
		span := s.buf[local-backSamples : hi]
		power := 0.0
		if len(span) > 0 {
			for _, v := range span {
				power += float64(real(v))*float64(real(v)) + float64(imag(v))*float64(imag(v))
			}
			power /= float64(len(span))
		}
		if !s.squelchOK(pos, power) {
			continue
		}
		slot := extractSlot(s.buf, local, SWTemplates[which[i]])
		if slot == nil {
			continue
		}
		out = append(out, DetectedBurst{
			Pos:     pos,
			SW:      SWNames[which[i]],
			Slot:    slot,
			Corr:    mm[i],
			EVM:     EVMPercent(slot),
			PowerDB: 10.0 * math.Log10(power+1e-20),
		})
	}
	sort.SliceStable(out, func(a, b int) bool { return out[a].Pos < out[b].Pos })
	if len(out) > 0 {
		s.lastPos = out[len(out)-1].Pos
	}
	s.nextScan = emitTo
	keepFrom := maxInt(s.bufAbs, s.nextScan-minSep-backSamples)
	if cut := keepFrom - s.bufAbs; cut > 0 {
		s.buf = append(s.buf[:0], s.buf[cut:]...)
		s.bufAbs = keepFrom
	}
	return out
}

func maximumFilter1D(v []float64, r int) []float64 {
	n := len(v)
	out := make([]float64, n)
	dq := make([]int, 0, n)
	next := 0
	for i := 0; i < n; i++ {
		hi := minInt(n-1, i+r)
		for ; next <= hi; next++ {
			for len(dq) > 0 && v[dq[len(dq)-1]] <= v[next] {
				dq = dq[:len(dq)-1]
			}
			dq = append(dq, next)
		}
		lo := maxInt(0, i-r)
		for len(dq) > 0 && dq[0] < lo {
			dq = dq[1:]
		}
		out[i] = v[dq[0]]
	}
	return out
}
