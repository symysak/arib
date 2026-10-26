package rx

import (
	"math"
	"time"
)


type ScanConfig struct {
	Enabled bool
	Max int
	VerifySec float64
	SNRdB float64
	IntervalSec float64
	QuietMin float64
	EvictIdleMin float64
	EvictUnsyncedMin float64
	HeadS float64
}

const defaultScanMax = 128

func (o *ScanConfig) applyDefaults() {
	if o.Max <= 0 {
		o.Max = defaultScanMax
	}
	if o.VerifySec <= 0 {
		o.VerifySec = 30
	}
	if o.IntervalSec <= 0 {
		o.IntervalSec = 2
	}
	if o.EvictIdleMin == 0 {
		o.EvictIdleMin = 30
	}
	if o.EvictUnsyncedMin == 0 {
		o.EvictUnsyncedMin = 5
	}
	if o.HeadS == 0 {
		o.HeadS = 3
	}
}

const (
	scanPinned    = iota
	scanVerifying
	scanLocked
	scanLedger
)

const (
	scanBlockWeak = 2 * time.Minute
	scanBlockDead = 30 * time.Minute
)

const backlogHighWater = 24

const shedLowWater = 8

func anyBacklog(ss []*station) bool {
	for _, s := range ss {
		if s.backlog() >= backlogHighWater {
			return true
		}
	}
	return false
}

type blocklist struct {
	grid  float64
	until map[int]time.Time
}

func newBlocklist(grid float64) *blocklist {
	return &blocklist{grid: grid, until: map[int]time.Time{}}
}

func (b *blocklist) key(offHz float64) int { return int(math.Round(offHz / b.grid)) }

func (b *blocklist) block(offHz float64, d time.Duration) {
	b.until[b.key(offHz)] = time.Now().Add(d)
}

func (b *blocklist) blockedNow(offHz float64) bool {
	k := b.key(offHz)
	until, ok := b.until[k]
	if !ok {
		return false
	}
	if time.Now().After(until) {
		delete(b.until, k)
		return false
	}
	return true
}

func evictIdle(ss []*station, o *ScanConfig, wall time.Time) *station {
	if o.EvictIdleMin <= 0 {
		return nil
	}
	var pick *station
	var oldest time.Time
	for _, s := range ss {
		if s.kind != scanLedger && s.kind != scanLocked {
			continue
		}
		at := s.activeAt()
		if s.recording.Load() || wall.Sub(at).Minutes() < o.EvictIdleMin {
			continue
		}
		if pick == nil || at.Before(oldest) {
			pick, oldest = s, at
		}
	}
	return pick
}

func evictUnsynced(ss []*station, o *ScanConfig, wall time.Time) *station {
	if o.EvictUnsyncedMin <= 0 {
		return nil
	}
	var pick *station
	var oldest time.Time
	for _, s := range ss {
		if s.kind != scanLedger && s.kind != scanLocked {
			continue
		}
		if s.ch.Counters().Good != 0 || s.recording.Load() {
			continue
		}
		at := s.activeAt()
		if wall.Sub(at).Minutes() < o.EvictUnsyncedMin {
			continue
		}
		if pick == nil || at.Before(oldest) {
			pick, oldest = s, at
		}
	}
	return pick
}

func (e *Engine) scanStep(ss []*station, now float64) []*station {
	o := &e.cfg.Scan
	name := e.std.Name()
	keep := ss[:0]
	for _, s := range ss {
		switch s.kind {
		case scanVerifying:
			ok, _, label := s.ch.Lock()
			c := s.ch.Counters()
			if ok {
				s.kind, s.mark = scanLocked, c.Good
				e.lg.Printf("%.4f MHz: %s として復号できた（%s）ので監視に加える", s.mhz, name, label)
				break
			}
			if c.ProcSec < o.VerifySec {
				break
			}
			{
				retry := scanBlockDead
				if c.Frames > 0 {
					retry = scanBlockWeak
				}
				e.lg.Printf("%.4f MHz: 電波はあるが %s として復号できない（%s）ので外す（%.0f 分後に再挑戦）",
					s.mhz, name, label, retry.Minutes())
				e.blocked.block(s.mhz*1e6-e.cfg.CenterHz, retry)
				e.drop(s)
				continue
			}
		case scanLocked:
			if o.QuietMin > 0 && (now-s.since)/60 >= o.QuietMin {
				n := s.ch.Counters().Good
				if n <= s.mark {
					e.lg.Printf("%.4f MHz: %.0f 分なにも復号できなかったので手放す", s.mhz, o.QuietMin)
					e.blocked.block(s.mhz*1e6-e.cfg.CenterHz, 10*time.Minute)
					e.drop(s)
					continue
				}
				s.mark, s.since = n, now
			}
		}
		keep = append(keep, s)
	}
	ss = keep
	if anyBacklog(ss) {
		if v := evictUnsynced(ss, o, time.Now()); v != nil {
			e.lg.Printf("%.4f MHz: CPU が詰まっているので、%.0f 分同期しないこの局を手放す",
				v.mhz, time.Since(v.activeAt()).Minutes())
			e.drop(v)
			ss = without(ss, v)
		}
	}
	ss = e.alignNominal(ss)
	taken := map[int]bool{}
	for _, s := range ss {
		taken[e.blocked.key(s.mhz*1e6-e.cfg.CenterHz)] = true
	}
	evicted := 0
	for _, off := range e.det.Candidates() {
		if taken[e.blocked.key(off)] || e.blocked.blockedNow(off) || nearStation(ss, (e.cfg.CenterHz+off)/1e6, e.std.Grid()/4) {
			continue
		}
		if e.claimed != nil && e.claimed((e.cfg.CenterHz+off)/1e6) {
			continue
		}
		if len(ss) >= o.Max {
			if evicted >= 1 {
				break
			}
			v := evictIdle(ss, o, time.Now())
			if v == nil {
				break
			}
			evicted++
			e.lg.Printf("%.4f MHz: 枠（%d 局）が埋まっているので、%.0f 分なにも録っていないこの局を %.4f MHz に譲る",
				v.mhz, o.Max, time.Since(v.activeAt()).Minutes(), (e.cfg.CenterHz+off)/1e6)
			e.drop(v)
			ss = without(ss, v)
		}
		taken[e.blocked.key(off)] = true
		mhz := (e.cfg.CenterHz + off) / 1e6
		e.lg.Printf("%.4f MHz（%+.1f kHz）に電波を見つけた。%.0f 秒かけて %s か確かめる",
			mhz, off/1e3, o.VerifySec, name)
		ss = append(ss, e.add(mhz, scanVerifying, nil))
	}
	return ss
}

func nearStation(ss []*station, mhz, tolHz float64) bool {
	for _, s := range ss {
		if math.Abs(s.mhz-mhz)*1e6 < tolHz {
			return true
		}
	}
	return false
}

func (e *Engine) alignNominal(ss []*station) []*station {
	var out []*station
	for i, s := range ss {
		if s.kind == scanPinned || s.nominalDone || s.recording.Load() {
			out = append(out, s)
			continue
		}
		nm, ok := s.ch.(Nominaler)
		if !ok {
			s.nominalDone = true
			out = append(out, s)
			continue
		}
		n, ok := nm.NominalMHz()
		if !ok {
			out = append(out, s)
			continue
		}
		s.nominalDone = true
		if math.Abs(s.mhz-n) < 1e-7 {
			out = append(out, s)
			continue
		}
		var twin *station
		for j, o := range ss {
			if j != i && !o.dropping && math.Abs(o.mhz-n) < 1e-7 {
				twin = o
				break
			}
		}
		for _, o := range out {
			if math.Abs(o.mhz-n) < 1e-7 {
				twin = o
			}
		}
		if twin == nil && e.claimed != nil && e.claimed(n) {
			twin = s
		}
		e.drop(s)
		e.blocked.block(s.mhz*1e6-e.cfg.CenterHz, 30*time.Minute)
		if twin != nil {
			e.lg.Printf("%.4f MHz: キャリアは %.4f MHz で、その局が既にあるので手放す", s.mhz, n)
			continue
		}
		e.lg.Printf("%.4f MHz: キャリアは %.4f MHz なので、その周波数の局に張り直す", s.mhz, n)
		ns := e.add(n, scanVerifying, nil)
		ns.nominalDone = true
		out = append(out, ns)
	}
	return out
}

func without(ss []*station, v *station) []*station {
	kept := ss[:0]
	for _, s := range ss {
		if s != v {
			kept = append(kept, s)
		}
	}
	return kept
}
