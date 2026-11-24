package l3


import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

type TimerEvent struct {
	Entity, Name string
	Dir          int
	T            float64
}

type TimerFinding struct {
	Timer, Side string
	Start, Stop string
	Limit       float64
	Elapsed     float64
	Kind        string
	T           float64
	Text        string
}

type TimerRule struct {
	Timer, Side, Entity string
	Start, Stop         map[string]bool
	StartDir            int
	Limit               float64
}

func NewTimerRule(timer, side, entity, start, stop string, limit float64) (TimerRule, bool) {
	if !strings.HasSuffix(strings.TrimSpace(start), "送信") || !strings.HasSuffix(strings.TrimSpace(stop), "受信") {
		return TimerRule{}, false
	}
	st, sp := tcNames(start), tcNames(stop)
	if len(st) == 0 || len(sp) == 0 {
		return TimerRule{}, false
	}
	dir := SeqUp
	if side == "基地局" {
		dir = SeqDown
	}
	return TimerRule{Timer: timer, Side: side, Entity: entity, Start: st, Stop: sp, StartDir: dir, Limit: limit}, true
}

var (
	tcQuote = regexp.MustCompile(`「([^」]+)」`)
	tcSec   = regexp.MustCompile(`([0-9]+(?:\.[0-9]+)?)\s*(ミリ秒|秒|分)`)
	tcRules = buildTimerRules()
)

func buildTimerRules() []TimerRule {
	var out []TimerRule
	seen := map[string]bool{}
	for _, t := range Timers() {
		if t.Missing {
			continue
		}
		for _, r := range t.Rows {
			limit := 0.0
			if m := tcSec.FindStringSubmatch(r.Default); m != nil {
				v, _ := strconv.ParseFloat(m[1], 64)
				switch m[2] {
				case "ミリ秒":
					v /= 1000
				case "分":
					v *= 60
				}
				limit = v
			}
			rule, ok := NewTimerRule(t.Name, t.Side, t.Entity, r.Start, r.Stop, limit)
			if !ok {
				continue
			}
			key := fmt.Sprint(t.Name, t.Side, rule.Start, rule.Stop, limit)
			if seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, rule)
		}
	}
	return out
}

func tcNames(s string) map[string]bool {
	m := map[string]bool{}
	for _, q := range tcQuote.FindAllStringSubmatch(s, -1) {
		if q[1] != "空" {
			m[q[1]] = true
		}
	}
	return m
}

func TimerRuleCount() int { return len(tcRules) }

type tcArmed struct {
	rule  int
	start string
	t     float64
}

type TimerChecker struct {
	armed []tcArmed
	rules []TimerRule
}

func NewTimerChecker(extra []TimerRule) *TimerChecker {
	return &TimerChecker{rules: append(append([]TimerRule{}, tcRules...), extra...)}
}

func (c *TimerChecker) ruleSet() []TimerRule {
	if c.rules == nil {
		return tcRules
	}
	return c.rules
}

func (c *TimerChecker) Add(e TimerEvent) []TimerFinding {
	var out []TimerFinding
	rules := c.ruleSet()
	keep := c.armed[:0]
	for _, a := range c.armed {
		r := rules[a.rule]
		stops := e.Entity == r.Entity && e.Dir != r.StartDir && r.Stop[e.Name]
		switch {
		case stops:
			f := TimerFinding{Timer: r.Timer, Side: r.Side, Start: a.start, Stop: e.Name, Limit: r.Limit, Elapsed: e.T - a.t, T: a.t, Kind: "応答"}
			if r.Limit > 0 && f.Elapsed > r.Limit {
				f.Kind = "超過"
			}
			out = append(out, f.text())
		case r.Limit > 0 && e.T-a.t > r.Limit:
			out = append(out, TimerFinding{Timer: r.Timer, Side: r.Side, Start: a.start, Limit: r.Limit, Elapsed: e.T - a.t, T: a.t, Kind: "未応答"}.text())
		default:
			keep = append(keep, a)
		}
	}
	c.armed = keep
	for i, r := range rules {
		if e.Entity != r.Entity || e.Dir != r.StartDir || !r.Start[e.Name] {
			continue
		}
		dup := false
		for j := range c.armed {
			if c.armed[j].rule == i {
				c.armed[j].t, c.armed[j].start, dup = e.T, e.Name, true
			}
		}
		if !dup {
			c.armed = append(c.armed, tcArmed{rule: i, start: e.Name, t: e.T})
		}
	}
	return out
}

func (f TimerFinding) text() TimerFinding {
	limit := "任意設定"
	if f.Limit > 0 {
		limit = fmt.Sprintf("%g 秒", f.Limit)
	}
	switch f.Kind {
	case "未応答":
		f.Text = fmt.Sprintf("%s（%s）: 「%s」から %.2f 秒、止める信号が無い（タイムアウト値 %s）", f.Timer, f.Side, f.Start, f.Elapsed, limit)
	default:
		f.Text = fmt.Sprintf("%s（%s）: 「%s」→「%s」%.2f 秒（タイムアウト値 %s%s）", f.Timer, f.Side, f.Start, f.Stop, f.Elapsed, limit,
			map[bool]string{true: "・超過", false: ""}[f.Kind == "超過"])
	}
	return f
}
