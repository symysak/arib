package main

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/symysak/arib/internal/rec"
)

type probeLedger struct {
	mu   sync.Mutex
	path string
	rows map[int64]*probeRow
	mine map[int64]bool
}

const ProbeLedgerFile = "probe.tsv"

type probeRow struct {
	mhz                          float64
	family, system, codec, proof string
	score                        float64
	first, last                  time.Time
	note                         string
}

func openProbeLedger(dir string) (*probeLedger, error) {
	if dir == "" {
		return nil, nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	l := &probeLedger{path: filepath.Join(dir, ProbeLedgerFile), rows: map[int64]*probeRow{}, mine: map[int64]bool{}}
	if err := l.read(func(r *probeRow) { l.rows[key(r.mhz)] = r }); err != nil {
		return nil, err
	}
	return l, nil
}

func key(mhz float64) int64 { return int64(math.Round(mhz * 1e4)) }

func (l *probeLedger) read(fn func(r *probeRow)) error {
	return rec.ReadTSV(l.path, func(f []string) error {
		for len(f) < 9 {
			f = append(f, "")
		}
		mhz, err := strconv.ParseFloat(strings.TrimSpace(f[0]), 64)
		if err != nil || mhz <= 0 {
			return fmt.Errorf("周波数が読めない")
		}
		r := &probeRow{mhz: mhz, family: f[1], system: f[2], codec: f[3], proof: f[5], note: strings.TrimSpace(f[8])}
		r.score, _ = strconv.ParseFloat(strings.TrimSpace(f[4]), 64)
		r.first, _ = time.Parse(time.RFC3339, strings.TrimSpace(f[6]))
		r.last, _ = time.Parse(time.RFC3339, strings.TrimSpace(f[7]))
		fn(r)
		return nil
	})
}

func (l *probeLedger) touch(v Verdict, now time.Time) {
	if l == nil || v.FreqMHz <= 0 {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	k := key(v.FreqMHz)
	r := l.rows[k]
	if r == nil {
		r = &probeRow{mhz: v.FreqMHz, first: now}
		l.rows[k] = r
	}
	l.mine[k] = true
	r.family, r.system, r.codec, r.score, r.last = v.Family, v.System, v.Codec, v.Score, now
	r.proof = v.Lock
	if v.Proof != "" {
		r.proof += "・" + v.Proof
	}
}

func (l *probeLedger) save() error {
	if l == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	onDisk := map[int64]*probeRow{}
	if err := l.read(func(r *probeRow) { onDisk[key(r.mhz)] = r }); err != nil {
		return err
	}
	for k, d := range onDisk {
		if r, ok := l.rows[k]; ok && l.mine[k] {
			r.note = d.note
		} else {
			l.rows[k] = d
		}
	}
	keys := make([]int64, 0, len(l.rows))
	for k := range l.rows {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i] < keys[j] })
	clean := func(s string) string { return strings.NewReplacer("\t", " ", "\n", " ").Replace(s) }
	var sb strings.Builder
	sb.WriteString("# 周波数MHz\t規格\t系統\t符号器\t点数\t証拠\t初見\t最終\tメモ（ツールは書かない）\n")
	for _, k := range keys {
		r := l.rows[k]
		fmt.Fprintf(&sb, "%.4f\t%s\t%s\t%s\t%.3f\t%s\t%s\t%s\t%s\n", r.mhz, clean(r.family), clean(r.system),
			clean(r.codec), r.score, clean(r.proof), stamp(r.first), stamp(r.last), r.note)
	}
	return rec.WriteAtomic(l.path, sb.String())
}

func stamp(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format(time.RFC3339)
}
