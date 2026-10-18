package rec

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)


type Kind string

const (
	IQ        Kind = "iq.wav"
	Text      Kind = "txt"
	Audio     Kind = "audio.wav"
	AudioText Kind = "audio.txt"
	Calls     Kind = "calls.txt"
	TCH       Kind = "tch.bin"
	TCHIdx    Kind = "tch.idx"
	Raw       Kind = "raw.bin"
)

var kinds = []Kind{AudioText, Calls, Audio, IQ, TCHIdx, TCH, Raw, Text}

var legacy = []struct {
	suffix string
	kind   Kind
}{
	{"_audio.wav.txt", AudioText},
	{"_audio.wav", Audio},
	{"_calls.txt", Calls},
	{"_tch.bin", TCH},
	{"_tch.idx", TCHIdx},
	{".wav", IQ},
	{".txt", Text},
}

const (
	dateLayout = "2006-01-02"
	timeLayout = "150405"
)

func SystemDir(logDir, system string) string {
	if system == "" {
		return logDir
	}
	return filepath.Join(logDir, system)
}

func Base(dir string, t time.Time, mhz float64, label string) string {
	name := fmt.Sprintf("%s_%.4fMHz", t.Format(timeLayout), mhz)
	if label = cleanLabel(label); label != "" {
		name += "_" + label
	}
	return filepath.Join(dir, t.Format(dateLayout), name)
}

func NewBase(dir string, t time.Time, mhz float64, label string) (string, error) {
	b := Base(dir, t, mhz, label)
	return b, os.MkdirAll(filepath.Dir(b), 0o755)
}

func cleanLabel(s string) string {
	return strings.Map(func(r rune) rune {
		switch r {
		case '/', '\\', '.':
			return '-'
		}
		return r
	}, s)
}

func Path(base string, k Kind) string { return base + "." + string(k) }

func Rel(root, path string) string {
	r, err := filepath.Rel(root, path)
	if err != nil || r == ".." || strings.HasPrefix(r, ".."+string(filepath.Separator)) {
		return filepath.Base(path)
	}
	return filepath.ToSlash(r)
}

type Name struct {
	Base   string
	Kind   Kind
	Time   time.Time
	MHz    float64
	Label  string
	Legacy bool
}

var (
	reNew    = regexp.MustCompile(`^(\d{6})_(\d+\.\d+)MHz(?:_(.+))?$`)
	reLegacy = regexp.MustCompile(`^(\d{8}_\d{6})_(\d+\.\d+)MHz(?:_(.+))?$`)
	reDate   = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)
)

func Parse(path string) (Name, bool) {
	dir, file := filepath.Split(path)
	for _, k := range kinds {
		stem, ok := strings.CutSuffix(file, "."+string(k))
		if !ok {
			continue
		}
		m := reNew.FindStringSubmatch(stem)
		if m == nil {
			break
		}
		n := Name{Base: strings.TrimSuffix(path, "."+string(k)), Kind: k, Label: m[3]}
		n.MHz, _ = strconv.ParseFloat(m[2], 64)
		if d := filepath.Base(filepath.Clean(dir)); reDate.MatchString(d) {
			n.Time, _ = time.ParseInLocation(dateLayout+timeLayout, d+m[1], time.Local)
		}
		return n, true
	}
	for _, l := range legacy {
		stem, ok := strings.CutSuffix(file, l.suffix)
		if !ok {
			continue
		}
		m := reLegacy.FindStringSubmatch(stem)
		if m == nil {
			continue
		}
		n := Name{Base: strings.TrimSuffix(path, l.suffix), Kind: l.kind, Label: m[3], Legacy: true}
		n.MHz, _ = strconv.ParseFloat(m[2], 64)
		n.Time, _ = time.ParseInLocation("20060102_150405", m[1], time.Local)
		return n, true
	}
	return Name{}, false
}

func Sibling(path string, k Kind) (string, bool) {
	n, ok := Parse(path)
	if !ok {
		return "", false
	}
	if !n.Legacy {
		return Path(n.Base, k), true
	}
	for _, l := range legacy {
		if l.kind == k {
			return n.Base + l.suffix, true
		}
	}
	return "", false
}

func (n Name) sortKey() string {
	s := n.Time.Format("20060102_150405") + "_" + strconv.FormatFloat(n.MHz, 'f', 4, 64) + "MHz"
	if n.Label != "" {
		s += "_" + n.Label
	}
	for _, l := range legacy {
		if l.kind == n.Kind {
			return s + l.suffix
		}
	}
	return s
}

func Find(dir string, k Kind) ([]string, error) {
	type item struct {
		path, key string
	}
	var out []item
	add := func(d string) error {
		es, err := os.ReadDir(d)
		if err != nil {
			return err
		}
		for _, e := range es {
			if e.IsDir() {
				continue
			}
			p := filepath.Join(d, e.Name())
			if n, ok := Parse(p); ok && n.Kind == k {
				out = append(out, item{p, n.sortKey()})
			}
		}
		return nil
	}
	if err := add(dir); err != nil {
		return nil, err
	}
	es, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	for _, e := range es {
		if e.IsDir() && reDate.MatchString(e.Name()) {
			if err := add(filepath.Join(dir, e.Name())); err != nil {
				return nil, err
			}
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].key != out[j].key {
			return out[i].key < out[j].key
		}
		return out[i].path < out[j].path
	})
	ps := make([]string, len(out))
	for i, it := range out {
		ps[i] = it.path
	}
	return ps, nil
}
