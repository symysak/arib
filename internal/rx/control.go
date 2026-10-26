package rx

import (
	"errors"
	"math"
	"sort"
	"time"

	"github.com/symysak/arib/internal/iqsrc"
)


var ErrNotControllable = errors.New("この入力は同調・利得を変えられない（rtltcp:// だけが対応）")

var ErrNotRunning = errors.New("受信は終わっている")

const cmdTimeout = 10 * time.Second

type Status struct {
	Running bool `json:"running"`
	Done    bool `json:"done"`
	Input string `json:"input"`
	SampleRate float64 `json:"sample_rate"`
	CenterHz   float64 `json:"center_hz"`
	GainDB     float64 `json:"gain_db"`
	AGC        bool    `json:"agc"`
	Controllable bool `json:"controllable"`
	Scan bool `json:"scan"`
	Channels []ChannelInfo `json:"channels"`
}

type ChannelInfo struct {
	ID  int     `json:"id"`
	MHz float64 `json:"mhz"`
}

func (e *Engine) Status() Status {
	e.mu.Lock()
	defer e.mu.Unlock()
	st := Status{Running: e.running, Done: e.done, Input: e.cfg.Source, SampleRate: e.rate,
		CenterHz: e.cfg.CenterHz, GainDB: e.cfg.GainDB, AGC: e.cfg.AGC, Scan: e.cfg.Scan.Enabled,
		Controllable: e.controllable, Channels: []ChannelInfo{}}
	if e.running || e.done {
		for id, s := range e.byID {
			st.Channels = append(st.Channels, ChannelInfo{ID: id, MHz: s.mhz})
		}
	} else {
		for _, f := range append(append([]float64(nil), e.cfg.Channels...), e.cfg.Ledger...) {
			st.Channels = append(st.Channels, ChannelInfo{ID: -1, MHz: f})
		}
	}
	sort.Slice(st.Channels, func(i, j int) bool { return st.Channels[i].MHz < st.Channels[j].MHz })
	return st
}

func (e *Engine) Tune(centerHz float64) error {
	return e.control(func() error {
		ctl, ok := e.src.(iqsrc.Controller)
		if !ok {
			return ErrNotControllable
		}
		if err := ctl.SetFreq(centerHz); err != nil {
			return err
		}
		e.mu.Lock()
		e.cfg.CenterHz = centerHz
		e.mu.Unlock()
		return nil
	}, func() { e.cfg.CenterHz = centerHz }, true)
}

func (e *Engine) SetGain(db float64, agc bool) error {
	return e.control(func() error {
		ctl, ok := e.src.(iqsrc.Controller)
		if !ok {
			return ErrNotControllable
		}
		if err := ctl.SetGain(db, agc); err != nil {
			return err
		}
		e.mu.Lock()
		e.cfg.GainDB, e.cfg.AGC = db, agc
		e.mu.Unlock()
		return nil
	}, func() { e.cfg.GainDB, e.cfg.AGC = db, agc }, false)
}

func (e *Engine) control(run func() error, before func(), rebuild bool) error {
	e.mu.Lock()
	if e.done {
		e.mu.Unlock()
		return ErrNotRunning
	}
	if !e.running {
		before()
		e.mu.Unlock()
		return nil
	}
	if e.srcKnown && !e.controllable {
		e.mu.Unlock()
		return ErrNotControllable
	}
	res := make(chan error, 1)
	e.cmds = append(e.cmds, func(ss []*station) []*station {
		err := run()
		if err == nil && rebuild {
			ss = e.rebuild(ss)
		}
		res <- err
		return ss
	})
	e.mu.Unlock()
	select {
	case err := <-res:
		return err
	case <-time.After(cmdTimeout):
		return errors.New("受信エンジンの返事が無い")
	}
}

func (e *Engine) rebuild(ss []*station) []*station {
	type keep struct {
		mhz  float64
		kind int
	}
	var again []keep
	for _, s := range ss {
		if s.kind != scanVerifying {
			again = append(again, keep{s.mhz, s.kind})
		}
		e.drop(s)
	}
	for _, s := range ss {
		<-s.done
	}
	var out []*station
	for _, k := range again {
		if off := k.mhz*1e6 - e.cfg.CenterHz; math.Abs(off) > e.rate/2*0.9 {
			if e.homeless != nil && e.homeless(k.mhz, k.kind) {
				e.lg.Printf("%.4f MHz は新しい中心では帯域外なので、受けられるほかの入力へ移した", k.mhz)
				continue
			}
			e.lg.Printf("%.4f MHz は新しい中心では入力帯域外（オフセット %+.0f kHz）なので監視しない", k.mhz, off/1e3)
			continue
		}
		out = append(out, e.add(k.mhz, k.kind, nil))
	}
	if e.det != nil {
		e.det = e.std.NewDetector(DetectorConfig{FsIn: e.rate, CenterHz: e.cfg.CenterHz, SNRdB: e.cfg.Scan.SNRdB})
		e.blocked = newBlocklist(e.std.Grid())
		if e.head != nil {
			e.head = NewHeadRing(len(e.head.chunks))
		}
	}
	e.lg.Printf("中心を %.4f MHz へ移した（局 %d 本を作り直した）", e.cfg.CenterHz/1e6, len(out))
	return out
}
