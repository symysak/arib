package ledger

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

const (
	channelsFile = "channels.tsv"

	minOK  = 200
	minPct = 8

	channelStepHz = 6250.0
)

type Channel struct {
	MHz float64
	Orgs  []string
	First time.Time
	Last  time.Time
	Note string
	Codec string
	WC    int
}

func (c Channel) OrgText() string { return strings.Join(c.Orgs, "; ") }

type Ledger struct {
	dir string

	mu    sync.Mutex
	chans map[int64]*Channel
	base map[int64]counters
	ok map[int64]counters
	orgRuns map[orgKey]orgRun
	codecs map[int64]codecPick
}

type codecPick struct {
	codec string
	wc    int
}

type counters struct{ frames, richOK int }

func key(mhz float64) int64 { return int64(math.Round(mhz * 1e6 / channelStepHz)) }

func snap(mhz float64) float64 { return float64(key(mhz)) * channelStepHz / 1e6 }

func Open(dir string) (*Ledger, error) {
	l := &Ledger{dir: dir, chans: map[int64]*Channel{},
		base: map[int64]counters{}, ok: map[int64]counters{}, orgRuns: map[orgKey]orgRun{}, codecs: map[int64]codecPick{}}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	if err := l.load(); err != nil {
		return nil, err
	}
	return l, nil
}

func (l *Ledger) Dir() string { return l.dir }

func parseTime(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}
	}
	return t
}

func fmtTime(t time.Time) string {
	if t.IsZero() {
		return "-"
	}
	return t.Format(time.RFC3339)
}

func splitOrgs(s string) []string {
	var out []string
	for _, v := range strings.Split(s, ";") {
		if v = strings.TrimSpace(v); v != "" {
			out = append(out, v)
		}
	}
	return out
}

func (l *Ledger) load() error {
	return rec.ReadTSV(filepath.Join(l.dir, channelsFile), func(r []string) error {
		if len(r) < 4 {
			return fmt.Errorf("列が足りない")
		}
		mhz, err := strconv.ParseFloat(r[0], 64)
		if err != nil {
			return err
		}
		c := &Channel{MHz: snap(mhz), Orgs: splitOrgs(r[1]), First: parseTime(r[2]), Last: parseTime(r[3]), WC: -1}
		if len(r) > 4 {
			c.Note = r[4]
		}
		if len(r) > 5 {
			c.Codec = strings.TrimSpace(r[5])
		}
		if len(r) > 6 {
			if n, err := strconv.Atoi(strings.TrimSpace(r[6])); err == nil && n >= 0 && n < 512 {
				c.WC = n
			}
		}
		l.chans[key(mhz)] = c
		return nil
	})
}

func (l *Ledger) Channels() []Channel {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make([]Channel, 0, len(l.chans))
	for _, c := range l.chans {
		cp := *c
		cp.Orgs = append([]string(nil), c.Orgs...)
		out = append(out, cp)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].MHz < out[j].MHz })
	return out
}

func (l *Ledger) Note(mhz float64) string {
	l.mu.Lock()
	defer l.mu.Unlock()
	if c := l.chans[key(mhz)]; c != nil {
		return c.Note
	}
	return ""
}

func (l *Ledger) Frequencies() []float64 {
	cs := l.Channels()
	out := make([]float64, 0, len(cs))
	for _, c := range cs {
		out = append(out, c.MHz)
	}
	return out
}

func (l *Ledger) Observe(mhz float64, frames, richOK int, at time.Time) {
	l.mu.Lock()
	defer l.mu.Unlock()
	k := key(mhz)
	b := l.base[k]
	if frames < b.frames || richOK < b.richOK {
		b = counters{}
	}
	df, dok := frames-b.frames, richOK-b.richOK
	l.base[k] = counters{frames, richOK}
	if df <= 0 {
		return
	}
	if c := l.chans[k]; c != nil {
		c.Last = at
		return
	}
	n := l.ok[k]
	n.frames += df
	n.richOK += dok
	l.ok[k] = n
	if n.richOK < minOK || n.richOK*100 < n.frames*minPct {
		return
	}
	c := &Channel{MHz: snap(mhz), First: at, Last: at, WC: -1}
	if p, ok := l.codecs[k]; ok {
		c.Codec, c.WC = p.codec, p.wc
		delete(l.codecs, k)
	}
	l.chans[k] = c
}

func (l *Ledger) SetCodec(mhz float64, codec string, wc int) {
	if codec == "" {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	k := key(mhz)
	if c := l.chans[k]; c != nil {
		c.Codec, c.WC = codec, wc
		return
	}
	l.codecs[k] = codecPick{codec, wc}
}

func (l *Ledger) Codec(mhz float64) (codec string, wc int) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if c := l.chans[key(mhz)]; c != nil {
		return c.Codec, c.WC
	}
	if p, ok := l.codecs[key(mhz)]; ok {
		return p.codec, p.wc
	}
	return "", -1
}

func (l *Ledger) AddOrg(mhz float64, org string, at time.Time) {
	if org == "" {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	k := key(mhz)
	c := l.chans[k]
	if c == nil {
		return
	}
	for _, v := range c.Orgs {
		if v == org {
			c.Last = at
			return
		}
	}
	pk := orgKey{k, org}
	r := l.orgRuns[pk]
	if r.n == 0 || at.Sub(r.last).Seconds() > orgRunGapSec {
		r.n = 0
	}
	r.n++
	r.last = at
	if r.n < orgRunMin {
		l.orgRuns[pk] = r
		return
	}
	delete(l.orgRuns, pk)
	c.Orgs = append(c.Orgs, org)
	c.Last = at
}

const (
	orgRunMin    = 3
	orgRunGapSec = 5.0
)

type orgKey struct {
	ch  int64
	org string
}

type orgRun struct {
	n    int
	last time.Time
}

func (l *Ledger) Save() error {
	l.reloadNotes()
	l.mu.Lock()
	chans := make([]*Channel, 0, len(l.chans))
	for _, c := range l.chans {
		chans = append(chans, c)
	}
	l.mu.Unlock()
	sort.Slice(chans, func(i, j int) bool { return chans[i].MHz < chans[j].MHz })

	var b strings.Builder
	b.WriteString("# 局台帳（自動生成。**メモ欄だけは手で書ける** — ツールは読むだけ）\n")
	b.WriteString("# 行を消すとその周波数は監視対象から外れる（また復号できれば戻る）\n")
	b.WriteString("# 周波数MHz\t団体（; 区切り）\t初見\t最終\tメモ\t符号器\twc（付録 1 のパターン番号）\n")
	for _, c := range chans {
		wc := ""
		if c.WC >= 0 {
			wc = strconv.Itoa(c.WC)
		}
		fmt.Fprintf(&b, "%.5f\t%s\t%s\t%s\t%s\t%s\t%s\n",
			c.MHz, c.OrgText(), fmtTime(c.First), fmtTime(c.Last), c.Note, c.Codec, wc)
	}
	return rec.WriteAtomic(filepath.Join(l.dir, channelsFile), b.String())
}

func (l *Ledger) reloadNotes() {
	notes := map[int64]string{}
	_ = rec.ReadTSV(filepath.Join(l.dir, channelsFile), func(r []string) error {
		if len(r) < 5 {
			return fmt.Errorf("メモ欄が無い")
		}
		mhz, err := strconv.ParseFloat(r[0], 64)
		if err != nil {
			return err
		}
		notes[key(mhz)] = strings.TrimSpace(r[4])
		return nil
	})
	l.mu.Lock()
	defer l.mu.Unlock()
	for k, n := range notes {
		if c := l.chans[k]; c != nil {
			c.Note = n
		}
	}
}
