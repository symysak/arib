package server

import (
	"strings"

	"github.com/symysak/arib/internal/std-39/qpsk/l3"
)

type seqTracker struct{ t *l3.Tracker }

func (s *seqTracker) Observe(sig l3.Signal, dir int, ch string, t float64) {
	if s.t == nil {
		s.t = l3.NewTracker()
	}
	s.t.AddSignal(sig, dir, ch, t)
	for _, m := range sig.Messages {
		if m.Call == nil {
			continue
		}
		for _, b := range m.Call.Bearers {
			switch b.Mode {
			case "単信個別":
				s.t.SetHint(l3.KindSimplex)
			case "同報":
				s.t.SetHint(l3.KindBroadcast)
			}
		}
	}
}

func (s *seqTracker) Kind() (string, string) {
	if s.t == nil {
		return "", ""
	}
	g := s.t.Guess()
	if g.Kind == "" || !g.Kind.IsCall() {
		return "", ""
	}
	name := string(g.Kind)
	if len(g.Candidates) > 1 {
		var c []string
		for _, k := range g.Candidates {
			c = append(c, string(k))
		}
		name = strings.Join(c, " / ")
	}
	return name, g.Text + "・確度 " + g.Confidence + "（" + g.Why + "）"
}

func piggybackOf(sig l3.Signal) (string, []string) {
	text := ""
	if c, ok := l3.PiggybackSignal(sig); ok {
		text = c.Text()
	}
	return text, l3.PiggybackCheck(sig)
}
