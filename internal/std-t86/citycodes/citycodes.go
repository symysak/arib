package citycodes

import (
	_ "embed"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

var currentTSV string

var abolishedTSV string

type Abolished struct {
	Name   string
	Date   string
	Reason string
}

var (
	current   map[int]string
	abolished map[int]Abolished
	currentKeys   []int
	abolishedKeys []int
)

func tsvLines(s string) []string {
	out := strings.Split(strings.TrimRight(s, "\r\n"), "\n")
	for i := range out {
		out[i] = strings.TrimSuffix(out[i], "\r")
	}
	return out
}

func init() {
	current = make(map[int]string)
	for _, ln := range tsvLines(currentTSV) {
		if ln == "" {
			continue
		}
		f := strings.SplitN(ln, "\t", 2)
		code, err := strconv.Atoi(f[0])
		if err != nil {
			panic(fmt.Sprintf("current.tsv: コードを解釈できません: %q", ln))
		}
		current[code] = f[1]
		currentKeys = append(currentKeys, code)
	}
	sort.Ints(currentKeys)

	abolished = make(map[int]Abolished)
	for _, ln := range tsvLines(abolishedTSV) {
		if ln == "" {
			continue
		}
		f := strings.SplitN(ln, "\t", 4)
		if len(f) < 3 {
			panic(fmt.Sprintf("abolished.tsv: 列が足りません: %q", ln))
		}
		code, err := strconv.Atoi(f[0])
		if err != nil {
			panic(fmt.Sprintf("abolished.tsv: コードを解釈できません: %q", ln))
		}
		a := Abolished{Name: f[1], Date: f[2]}
		if len(f) == 4 {
			a.Reason = f[3]
		}
		abolished[code] = a
		abolishedKeys = append(abolishedKeys, code)
	}
	sort.Ints(abolishedKeys)
}

func NumCurrent() int   { return len(current) }
func NumAbolished() int { return len(abolished) }

func AbolishedLabel(a Abolished) string {
	return "旧 " + a.Name
}

func Name(code int) (string, bool) {
	if n, ok := current[code]; ok {
		return n, true
	}
	if a, ok := abolished[code]; ok {
		return AbolishedLabel(a), true
	}
	return "", false
}

type Candidate struct {
	Code int
	Name string
}

func CandidatesForSeed(seed int, includeAbolished bool) []Candidate {
	s := seed & 0x1FF
	var out []Candidate
	for _, c := range currentKeys {
		if c&0x1FF == s {
			out = append(out, Candidate{Code: c, Name: current[c]})
		}
	}
	if includeAbolished {
		for _, c := range abolishedKeys {
			if c&0x1FF == s {
				out = append(out, Candidate{Code: c, Name: AbolishedLabel(abolished[c])})
			}
		}
	}
	return out
}

func SeedForCity(name string) []Candidate {
	var out []Candidate
	for _, c := range currentKeys {
		n := current[c]
		if strings.Contains(n, name) {
			out = append(out, Candidate{Code: c & 0x1FF, Name: n})
		}
	}
	return out
}
