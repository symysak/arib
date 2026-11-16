package licensee

import (
	"bufio"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

const DefaultName = "t98-licensed.tsv.gz"

type Entry struct {
	Licensee    string `json:"licensee"`
	CorpNo      string `json:"corp_no,omitempty"`
	Location    string `json:"location,omitempty"`
	Area        string `json:"area,omitempty"`
	Purpose     string `json:"purpose,omitempty"`
	LicenseDate string `json:"license_date,omitempty"`
	ValidUntil  string `json:"valid_until,omitempty"`
	Bands       string `json:"bands,omitempty"`
	Bureau      string `json:"bureau,omitempty"`
}

func (e Entry) Place() string {
	if e.Location != "" {
		return e.Location
	}
	return e.Area
}

type DB struct {
	keys    []uint32
	idx     []int32
	entries []Entry
	Path, Note string
}

func (d *DB) Len() int {
	if d == nil {
		return 0
	}
	return len(d.keys)
}

func (d *DB) Lookup(csm string) (Entry, bool) {
	if d == nil || len(csm) != 9 {
		return Entry{}, false
	}
	n, err := strconv.ParseUint(csm, 10, 32)
	if err != nil {
		return Entry{}, false
	}
	k := uint32(n)
	i := sort.Search(len(d.keys), func(i int) bool { return d.keys[i] >= k })
	if i < len(d.keys) && d.keys[i] == k {
		return d.entries[d.idx[i]], true
	}
	return Entry{}, false
}

func (d *DB) Summary() string {
	if d == nil {
		return ""
	}
	s := fmt.Sprintf("%s（%d 局", d.Path, d.Len())
	if d.Note != "" {
		s += "。" + d.Note
	}
	return s + "）"
}

func Load(path string) (*DB, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var r io.Reader = f
	if strings.HasSuffix(path, ".gz") {
		z, err := gzip.NewReader(f)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		defer z.Close()
		r = z
	}
	d, err := Read(r)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	d.Path = path
	return d, nil
}

func Read(r io.Reader) (*DB, error) {
	d := &DB{}
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	var col map[string]int
	uniq := map[Entry]int32{}
	type pair struct {
		k uint32
		i int32
	}
	var ps []pair
	line := 0
	for sc.Scan() {
		line++
		t := sc.Text()
		if t == "" {
			continue
		}
		if strings.HasPrefix(t, "#") {
			if d.Note == "" {
				d.Note = noteOf(t)
			}
			continue
		}
		f := strings.Split(t, "\t")
		if col == nil {
			col = map[string]int{}
			for i, name := range f {
				col[name] = i
			}
			if _, ok := col["csm"]; !ok {
				return nil, fmt.Errorf("%d 行目: 見出しに csm の列が無い", line)
			}
			continue
		}
		get := func(name string) string {
			if i, ok := col[name]; ok && i < len(f) {
				return f[i]
			}
			return ""
		}
		n, err := strconv.ParseUint(get("csm"), 10, 32)
		if err != nil || len(get("csm")) != 9 {
			continue
		}
		e := Entry{Licensee: get("licensee"), CorpNo: get("corp_no"), Location: get("location"), Area: get("area"),
			Purpose: get("purpose"), LicenseDate: get("license_date"), ValidUntil: get("valid_until"),
			Bands: get("bands"), Bureau: get("bureau")}
		i, ok := uniq[e]
		if !ok {
			i = int32(len(d.entries))
			uniq[e] = i
			d.entries = append(d.entries, e)
		}
		ps = append(ps, pair{uint32(n), i})
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	if col == nil {
		return nil, fmt.Errorf("見出しの行が無い")
	}
	sort.Slice(ps, func(a, b int) bool { return ps[a].k < ps[b].k })
	d.keys, d.idx = make([]uint32, 0, len(ps)), make([]int32, 0, len(ps))
	for _, p := range ps {
		if n := len(d.keys); n > 0 && d.keys[n-1] == p.k {
			d.idx[n-1] = p.i
			continue
		}
		d.keys, d.idx = append(d.keys, p.k), append(d.idx, p.i)
	}
	return d, nil
}

func noteOf(t string) string {
	for _, w := range strings.Fields(strings.TrimPrefix(t, "#")) {
		if v, ok := strings.CutPrefix(w, "last_update="); ok {
			return "データ更新日 " + v
		}
	}
	return strings.TrimSpace(strings.TrimPrefix(t, "#"))
}

func Candidates() []string {
	c := []string{filepath.Join("data", "musen", DefaultName)}
	if exe, err := os.Executable(); err == nil {
		dir := filepath.Dir(exe)
		c = append(c, filepath.Join(dir, DefaultName), filepath.Join(dir, "data", "musen", DefaultName))
	}
	return c
}

func Open(spec string) (*DB, error) {
	switch spec {
	case "", "off":
		return nil, nil
	case "auto":
		for _, p := range Candidates() {
			if _, err := os.Stat(p); err == nil {
				return Load(p)
			}
		}
		return nil, nil
	}
	return Load(spec)
}
