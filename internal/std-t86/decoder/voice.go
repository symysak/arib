package decoder

import "github.com/symysak/arib/internal/std-t86/dsp"

type voiceSmoother struct {
	win     int
	horizon int

	pending []pendingBurst
	hist    map[int][]bool
	hasLast bool
	lastPos int
	lastRng int
}

type pendingBurst struct {
	burst *dsp.TchBurst
	ring  int
	voice bool
}

type DecidedBurst struct {
	Burst   *dsp.TchBurst
	IsVoice bool
}

func newVoiceSmoother(win int) *voiceSmoother {
	if win <= 0 {
		win = 2
	}
	return &voiceSmoother{
		win: win,
		horizon: (6*win + 2) * slotSamples,
		hist:    map[int][]bool{},
	}
}

func (s *voiceSmoother) oldestPendingPos() (int, bool) {
	if len(s.pending) == 0 {
		return 0, false
	}
	return s.pending[0].burst.Pos, true
}

func (s *voiceSmoother) ringOf(pos int) int {
	ring := 0
	if s.hasLast {
		ring = ((s.lastRng+roundHalfEven(float64(pos-s.lastPos)/slotSamples))%6 + 6) % 6
	}
	s.hasLast = true
	s.lastPos, s.lastRng = pos, ring
	return ring
}

func (s *voiceSmoother) push(b *dsp.TchBurst) []DecidedBurst {
	ring := s.ringOf(b.Pos)
	s.pending = append(s.pending, pendingBurst{b, ring, dsp.IsVoiceType(b.CType)})
	return s.release(b.Pos)
}

func (s *voiceSmoother) mates(pending []pendingBurst, start, pos, ring int) []bool {
	limit := pos + s.horizon
	out := make([]bool, 0, s.win)
	for i := start; i < len(pending) && len(out) < s.win; i++ {
		if pending[i].ring == ring && pending[i].burst.Pos <= limit {
			out = append(out, pending[i].voice)
		}
	}
	return out
}

func (s *voiceSmoother) release(highWater int) []DecidedBurst {
	var out []DecidedBurst
	for len(s.pending) > 0 {
		p := s.pending[0]
		mates := s.mates(s.pending, 1, p.burst.Pos, p.ring)
		if len(mates) < s.win && highWater <= p.burst.Pos+s.horizon {
			break
		}
		out = append(out, DecidedBurst{p.burst, s.vote(p, mates)})
		s.pending = s.pending[1:]
	}
	return out
}

func (s *voiceSmoother) flush() []DecidedBurst {
	pend := s.pending
	s.pending = nil
	out := make([]DecidedBurst, 0, len(pend))
	for i, p := range pend {
		out = append(out, DecidedBurst{p.burst, s.vote(p, s.mates(pend, i+1, p.burst.Pos, p.ring))})
	}
	return out
}

func (s *voiceSmoother) vote(p pendingBurst, mates []bool) bool {
	yes, n := 0, 0
	for _, v := range s.hist[p.ring] {
		n++
		if v {
			yes++
		}
	}
	n++
	if p.voice {
		yes++
	}
	for _, v := range mates {
		n++
		if v {
			yes++
		}
	}
	h := append(s.hist[p.ring], p.voice)
	if len(h) > s.win {
		h = h[len(h)-s.win:]
	}
	s.hist[p.ring] = h
	return yes*2 > n
}

func roundHalfEven(x float64) int {
	f := float64(int(x))
	if x < 0 && x != f {
		f--
	}
	d := x - f
	i := int(f)
	switch {
	case d > 0.5:
		return i + 1
	case d < 0.5:
		return i
	default:
		if i%2 == 0 {
			return i
		}
		return i + 1
	}
}
