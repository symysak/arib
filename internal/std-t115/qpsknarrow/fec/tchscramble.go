package fec


type TCHScrambleSolver struct {
	sysPos []int
	parTx    []int
	parTurbo []int
	col [16]uint16
	basis  [16]tchPivot
	kernel []uint16
}

type tchPivot struct {
	value uint16
	combo uint16
	used  bool
}

func NewTCHScrambleSolver() *TCHScrambleSolver {
	s := &TCHScrambleSolver{}
	txMap := TCHTxMap()
	s.sysPos = make([]int, TCHTurboIn)
	for i := range s.sysPos {
		s.sysPos[i] = -1
	}
	for i, src := range txMap {
		if src%3 == 0 && src/3 < TCHTurboIn {
			s.sysPos[src/3] = i
			continue
		}
		s.parTx = append(s.parTx, i)
		s.parTurbo = append(s.parTurbo, src)
	}
	for j := 0; j < 16; j++ {
		pn := Scramble(1<<uint(j), TCHCodedBits)
		w := make([]uint8, TCHTurboIn)
		for k, pos := range s.sysPos {
			if pos >= 0 {
				w[k] = pn[pos]
			}
		}
		s.col[j] = bitsToU16(CRC16Linear(w))
	}
	for j := 0; j < 16; j++ {
		v, c := s.col[j], uint16(1)<<uint(j)
		pivot := false
		for v != 0 {
			b := topBit(v)
			if !s.basis[b].used {
				s.basis[b] = tchPivot{value: v, combo: c, used: true}
				pivot = true
				break
			}
			v ^= s.basis[b].value
			c ^= s.basis[b].combo
		}
		if !pivot {
			s.kernel = append(s.kernel, c)
		}
	}
	return s
}

func (s *TCHScrambleSolver) Rank() int { return 16 - len(s.kernel) }

type TCHScrambleResult struct {
	Init int
	Confidence float64
	Mismatch int
	Parity   int
}

func (s *TCHScrambleSolver) Solve(hard []uint8) (TCHScrambleResult, bool) {
	if len(hard) != TCHCodedBits {
		return TCHScrambleResult{}, false
	}
	v := make([]uint8, TCHTurboIn)
	for k, pos := range s.sysPos {
		if pos >= 0 {
			v[k] = hard[pos] & 1
		}
	}
	target := CRC16(v)

	rem, combo := target, uint16(0)
	for rem != 0 {
		b := topBit(rem)
		if !s.basis[b].used {
			return TCHScrambleResult{}, false
		}
		rem ^= s.basis[b].value
		combo ^= s.basis[b].combo
	}
	if len(s.kernel) > 4 {
		return TCHScrambleResult{}, false
	}
	best, found := TCHScrambleResult{Confidence: -1}, false
	for mask := 0; mask < 1<<uint(len(s.kernel)); mask++ {
		cand := combo
		for i, kv := range s.kernel {
			if mask&(1<<uint(i)) != 0 {
				cand ^= kv
			}
		}
		if cand == 0 {
			continue
		}
		r := s.check(hard, int(cand))
		if r.Confidence > best.Confidence {
			best, found = r, true
		}
	}
	return best, found
}

func (s *TCHScrambleSolver) check(hard []uint8, init int) TCHScrambleResult {
	pn := Scramble(init, TCHCodedBits)
	d := make([]uint8, TCHCodedBits)
	for i := range d {
		d[i] = (hard[i] ^ pn[i]) & 1
	}
	u := make([]uint8, TCHTurboIn)
	for k, pos := range s.sysPos {
		if pos >= 0 {
			u[k] = d[pos]
		}
	}
	enc := TurboEncode(u)
	mism := 0
	for i, tx := range s.parTx {
		if enc[s.parTurbo[i]] != d[tx] {
			mism++
		}
	}
	n := len(s.parTx)
	conf := 0.0
	if n > 0 {
		conf = 1 - 2*float64(mism)/float64(n)
	}
	return TCHScrambleResult{Init: init, Confidence: conf, Mismatch: mism, Parity: n}
}

func HardBits(llr []float64) []uint8 {
	out := make([]uint8, len(llr))
	for i, v := range llr {
		if v < 0 {
			out[i] = 1
		}
	}
	return out
}

func bitsToU16(b []uint8) uint16 {
	var v uint16
	for i := 0; i < 16 && i < len(b); i++ {
		v = v<<1 | uint16(b[i]&1)
	}
	return v
}

func topBit(v uint16) int {
	b := 0
	for v > 1 {
		v >>= 1
		b++
	}
	return b
}
