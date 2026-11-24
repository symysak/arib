package nonvoice


import (
	"regexp"
	"strings"

	"github.com/symysak/arib/internal/std-39/qpsk/l3"
)

func TimerRules() []l3.TimerRule {
	var out []l3.TimerRule
	for _, t := range Timers {
		if r, ok := l3.NewTimerRule(t.No, "移動局", "RT", t.Start, t.Stop, t.Default); ok {
			out = append(out, r)
		}
	}
	return out
}

func MethodOnChannel(s l3.Signal) string {
	for _, m := range s.Messages {
		if m.Assign != nil {
			continue
		}
		if x := MethodFromSignal(l3.Signal{Messages: []l3.Message{m}}); x != "" {
			return x
		}
	}
	return ""
}

const IdleSec = 10.0

type ProcObs struct {
	Name string
	Dir  int
}

var procParen = regexp.MustCompile(`（[^）]*）`)

func stepNames(msg string) []string {
	var out []string
	for _, p := range strings.Split(procParen.ReplaceAllString(msg, ""), ",") {
		p = strings.TrimSpace(p)
		if p != "" && p != "−" && p != "-" {
			out = append(out, p)
		}
	}
	return out
}

func GuessProcedure(obs []ProcObs, method string) (p Procedure, hits int, ok bool) {
	best := -1
	for i, pr := range Procedures {
		if method != "" && pr.Method != method {
			continue
		}
		have := map[ProcObs]bool{}
		for _, st := range pr.Steps {
			for _, n := range stepNames(st.Msg) {
				have[ProcObs{n, st.Dir}] = true
			}
		}
		seen := map[ProcObs]bool{}
		h := 0
		for _, o := range obs {
			if have[o] && !seen[o] {
				seen[o] = true
				h++
			}
		}
		if h > hits {
			best, hits = i, h
		}
	}
	if best < 0 || hits < 2 {
		return Procedure{}, hits, false
	}
	return Procedures[best], hits, true
}
