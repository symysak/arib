package rx

import (
	"context"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/symysak/arib/internal/rec"
)

type Ledger struct {
	mu   sync.Mutex
	path string
	grid float64
	rows map[int64]*ledgerRow
}

type ledgerRow struct {
	mhz         float64
	first, last time.Time
	evidence    string
	note        string
	orgs        []string
}

type OrgReporter interface {
	Orgs() []string
}

const LedgerFile = "channels.tsv"

func OpenLedger(dir string, grid float64) (*Ledger, error) {
	if dir == "" {
		return nil, nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	if grid <= 0 {
		grid = 1
	}
	l := &Ledger{path: filepath.Join(dir, LedgerFile), grid: grid, rows: map[int64]*ledgerRow{}}
	if err := l.read(func(r *ledgerRow) { l.rows[l.key(r.mhz)] = r }); err != nil {
		return nil, err
	}
	return l, nil
}

func (l *Ledger) key(mhz float64) int64 { return int64(math.Round(mhz * 1e6 / l.grid)) }

func (l *Ledger) read(fn func(r *ledgerRow)) error {
	return rec.ReadTSV(l.path, func(f []string) error {
		mhz, err := strconv.ParseFloat(strings.TrimSpace(f[0]), 64)
		if err != nil || mhz <= 0 {
			return fmt.Errorf("周波数が読めない")
		}
		r := &ledgerRow{mhz: mhz}
		if len(f) > 1 {
			r.first, _ = time.Parse(time.RFC3339, strings.TrimSpace(f[1]))
		}
		if len(f) > 2 {
			r.last, _ = time.Parse(time.RFC3339, strings.TrimSpace(f[2]))
		}
		if len(f) > 3 {
			r.evidence = strings.TrimSpace(f[3])
		}
		if len(f) > 4 {
			r.note = strings.TrimSpace(f[4])
		}
		if len(f) > 5 {
			for _, o := range strings.Split(f[5], ";") {
				if o = strings.TrimSpace(o); o != "" {
					r.orgs = append(r.orgs, o)
				}
			}
		}
		fn(r)
		return nil
	})
}

func (l *Ledger) Path() string { return l.path }

func (l *Ledger) MHz() []float64 {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make([]float64, 0, len(l.rows))
	for _, r := range l.rows {
		out = append(out, r.mhz)
	}
	sort.Float64s(out)
	return out
}

func (l *Ledger) Touch(mhz float64, evidence string, now time.Time) {
	l.mu.Lock()
	defer l.mu.Unlock()
	k := l.key(mhz)
	r := l.rows[k]
	if r == nil {
		r = &ledgerRow{mhz: mhz, first: now}
		l.rows[k] = r
	}
	r.last, r.evidence = now, evidence
}

func (l *Ledger) AddOrgs(mhz float64, orgs []string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	r := l.rows[l.key(mhz)]
	if r == nil {
		return
	}
	for _, o := range orgs {
		o = strings.TrimSpace(strings.NewReplacer("\t", " ", ";", ",").Replace(o))
		if o != "" && !slices.Contains(r.orgs, o) {
			r.orgs = append(r.orgs, o)
		}
	}
}

func (l *Ledger) Orgs(mhz float64) []string {
	if l == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if r := l.rows[l.key(mhz)]; r != nil {
		return append([]string(nil), r.orgs...)
	}
	return nil
}

type Stations interface {
	Status() Status
	Channel(id int) Channel
}

func (l *Ledger) Observe(e Stations, now time.Time) {
	for _, c := range e.Status().Channels {
		ch := e.Channel(c.ID)
		if ch == nil {
			continue
		}
		mhz := c.MHz
		if lg, ok := ch.(Ledgered); ok {
			n, yes := lg.LedgerMHz()
			if !yes {
				continue
			}
			mhz = n
		} else if nm, ok := ch.(Nominaler); ok {
			n, known := nm.NominalMHz()
			if !known {
				continue
			}
			mhz = n
		}
		if ok, _, label := ch.Lock(); ok {
			l.Touch(mhz, label, now)
		}
		if or, ok := ch.(OrgReporter); ok {
			l.AddOrgs(mhz, or.Orgs())
		}
	}
}

func (l *Ledger) Save() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	onDisk := map[int64]*ledgerRow{}
	if err := l.read(func(r *ledgerRow) { onDisk[l.key(r.mhz)] = r }); err != nil {
		return err
	}
	keys := make([]int64, 0, len(l.rows))
	for k, r := range l.rows {
		if d, ok := onDisk[k]; ok {
			r.note = d.note
		}
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i] < keys[j] })
	var sb strings.Builder
	sb.WriteString("# 周波数MHz\t初見\t最終\t当たりの証拠\tメモ（ツールは書かない。行を消すとその局は起動時に張り付けない）\t団体（; 区切り）\n")
	for _, k := range keys {
		r := l.rows[k]
		fmt.Fprintf(&sb, "%.4f\t%s\t%s\t%s\t%s\t%s\n", r.mhz, stamp(r.first), stamp(r.last),
			strings.ReplaceAll(r.evidence, "\t", " "), r.note, strings.Join(r.orgs, "; "))
	}
	return rec.WriteAtomic(l.path, sb.String())
}

func stamp(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format(time.RFC3339)
}

func (l *Ledger) Keep(ctx context.Context, e Stations, lg func(format string, a ...any)) {
	tk := time.NewTicker(30 * time.Second)
	defer tk.Stop()
	lastSave := time.Now()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-tk.C:
			l.Observe(e, now)
			if now.Sub(lastSave) >= 5*time.Minute {
				lastSave = now
				if err := l.Save(); err != nil && lg != nil {
					lg("台帳を書けませんでした: %v", err)
				}
			}
		}
	}
}

func (l *Ledger) Restore(cfg *Config) int {
	if l == nil || cfg.FollowVFO {
		return 0
	}
	have := map[int64]bool{}
	for _, f := range cfg.Channels {
		have[l.key(f)] = true
	}
	limit := cfg.Scan.Max
	if limit <= 0 {
		limit = defaultScanMax
	}
	n := 0
	for _, f := range l.MHz() {
		if have[l.key(f)] || len(cfg.Channels)+len(cfg.Ledger) >= limit {
			continue
		}
		have[l.key(f)] = true
		cfg.Ledger = append(cfg.Ledger, f)
		n++
	}
	return n
}

func (l *Ledger) Finish(e Stations) error {
	if l == nil {
		return nil
	}
	l.Observe(e, time.Now())
	return l.Save()
}
