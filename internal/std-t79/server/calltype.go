package server


import (
	"fmt"
	"math"
	"strings"
	"sync"

	"github.com/symysak/arib/internal/std-t79/decoder"
	"github.com/symysak/arib/internal/std-t79/l3"
)

const partnerSec = 6.0

const pendModeSec = 5.0

type netEntry struct {
	mhz       float64
	org       string
	restrict  *l3.Restriction
	turnup    *l3.Turnup
	support   int
	dedicated []l3.Dedicated
	maker     int
}

type pendMode struct {
	ctlMHz float64
	addr   string
	call   *l3.Call
	t      float64
}

type netBoard struct {
	mu   sync.Mutex
	nets []netEntry
	pend []pendMode
}

func (b *netBoard) entry(mhz float64) *netEntry {
	for i := range b.nets {
		if math.Abs(b.nets[i].mhz-mhz) < matchMHz {
			return &b.nets[i]
		}
	}
	b.nets = append(b.nets, netEntry{mhz: mhz, support: -1})
	return &b.nets[len(b.nets)-1]
}

func (b *netBoard) setBroadcast(mhz float64, org string, bc *l3.Broadcast) {
	b.mu.Lock()
	defer b.mu.Unlock()
	e := b.entry(mhz)
	if org != "" {
		e.org = org
	}
	if bc.Restrict != nil {
		e.restrict = bc.Restrict
	}
	e.turnup, e.support = bc.Turnup, bc.Support
	if bc.System != nil && bc.System.Maker > 0 {
		e.maker = bc.System.Maker
	}
}

func (b *netBoard) makerAt(mhz float64) int {
	b.mu.Lock()
	defer b.mu.Unlock()
	for i := range b.nets {
		if math.Abs(b.nets[i].mhz-mhz) < matchMHz {
			return b.nets[i].maker
		}
	}
	return 0
}

func (b *netBoard) setDedicated(mhz float64, d []l3.Dedicated) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.entry(mhz).dedicated = append([]l3.Dedicated(nil), d...)
}

func (b *netBoard) groupHalf(ctl float64, org string) *bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if ctl > 0 {
		for _, e := range b.nets {
			if math.Abs(e.mhz-ctl) < matchMHz && e.restrict != nil {
				v := e.restrict.GroupHalfDuplex
				return &v
			}
		}
	}
	var got *bool
	for _, e := range b.nets {
		if e.restrict == nil || (org != "" && e.org != org) {
			continue
		}
		v := e.restrict.GroupHalfDuplex
		if got != nil && *got != v {
			return nil
		}
		got = &v
	}
	return got
}

func (b *netBoard) special(mhz float64, slot int) (l3.CallType, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, e := range b.nets {
		half := e.restrict != nil && e.restrict.GroupHalfDuplex
		for _, d := range e.dedicated {
			if math.Abs(d.MHz-mhz) < matchMHz && d.Slot == slot {
				ct := l3.CallType{Name: "専用チャネル［単信］", Duplex: "単信", Section: "4.7.1.15",
					Downlink: "話者の声を基地局が下りへ中継する"}
				if half {
					ct.Name, ct.Duplex, ct.Downlink = "専用チャネル［半複信］", "半複信", "統制局側の声（移動局の声は上り）"
				}
				ct.Name += fmt.Sprintf(" 識別番号 %d", d.ID)
				return ct, true
			}
		}
		if t := e.turnup; t != nil && t.Slot == slot && (math.Abs(t.MHz-mhz) < matchMHz || (t.MHz == 0 && math.Abs(e.mhz-mhz) < matchMHz)) {
			return l3.CallType{Name: "基地局折返［単信］", Duplex: "単信", Section: "4.4.6",
				Downlink: "統制局と切れた基地局が同じゾーンの移動局どうしの声を折り返す"}, true
		}
	}
	return l3.CallType{}, false
}

func (b *netBoard) putPend(p pendMode) {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := b.pend[:0]
	for _, x := range b.pend {
		if p.t-x.t < pendModeSec && !(x.ctlMHz == p.ctlMHz && x.addr == p.addr) {
			out = append(out, x)
		}
	}
	b.pend = append(out, p)
}

func (b *netBoard) takePend(ctl float64, addr string, t float64) (*l3.Call, bool, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	var cands []int
	for i, x := range b.pend {
		if math.Abs(x.ctlMHz-ctl) >= matchMHz || t-x.t > pendModeSec || x.t > t {
			continue
		}
		if addr != "" && x.addr == addr {
			c := x.call
			b.pend = append(b.pend[:i], b.pend[i+1:]...)
			return c, true, true
		}
		cands = append(cands, i)
	}
	if len(cands) == 1 {
		c := b.pend[cands[0]].call
		b.pend = append(b.pend[:cands[0]], b.pend[cands[0]+1:]...)
		return c, false, true
	}
	return nil, false, false
}

type modeStats struct {
	sb1, sb3       int
	sbPrev         int
	voice          int
	biBusy, biIdle int
	runs           int
	gap            int
	partner        bool
	partnerChecked bool
}

func (st *modeStats) observe(s *decoder.Slot, voice bool) {
	if s.SB != nil && s.SB.OK {
		if st.sbPrev != s.SB.B {
			if s.SB.B == 0 {
				st.sb1++
			} else {
				st.sb3++
			}
		}
		st.sbPrev = s.SB.B
		return
	}
	st.sbPrev = -1
	if !voice {
		st.gap++
		return
	}
	if st.voice == 0 || st.gap >= callIdleFrames {
		st.runs++
	}
	st.gap = 0
	st.voice++
	switch s.BI {
	case 1:
		st.biBusy++
	case 0:
		st.biIdle++
	}
}

func (st *modeStats) infer() (l3.CallType, bool) {
	if st.voice < 25 {
		return l3.CallType{}, false
	}
	ct := l3.CallType{}
	switch {
	case st.partner && st.runs <= 1:
		ct = l3.CallType{Name: "個別［複信］移動局〜移動局", Duplex: "複信", Section: "4.7.1.5",
			Downlink: "2 スロットに互いの声（同じキャリアの別スロットと同時に始まった）"}
	case st.sb3 >= 2 || st.runs >= 2:
		ct = l3.CallType{Name: "単信（個別かグループか不明）", Duplex: "単信", Section: "4.7.1.1 / 4.7.1.2",
			Downlink: "話者の声を基地局が下りへ中継する（プレスのたびに音声が途切れる）"}
	case st.biIdle > st.biBusy && st.biIdle > 0:
		ct = l3.CallType{Name: "半複信（統制局側の発話）", Duplex: "半複信", Section: "4.7.1.3",
			Downlink: "統制局側の声。B/I がアイドルのまま（移動局は送っていない）"}
	case st.sb1 > 0 && st.sb3 == 0 && st.biBusy > 0:
		ct = l3.CallType{Name: "同報／一斉［単向］", Duplex: "単向", Section: "4.7.1.4 / 4.7.1.13",
			Downlink: "統制局の声だけ（SB3 が無く B/I がビジー）"}
	default:
		ct = l3.CallType{Name: "複信（統制局〜移動局・PBX・統制のどれか）", Duplex: "複信", Section: "4.7.1.6",
			Downlink: "相手（統制局）の声だけ。この移動局の声は上り"}
	}
	ct.Name += "（推定）"
	return ct, true
}

const (
	modeNone = iota
	modeInferred
	modeSpecial
	modeCC
)

func (c *Channel) setMode(n int, ct l3.CallType, rank int, src string) {
	cs := &c.calls[n]
	if rank < cs.modeRank || (rank == cs.modeRank && ct.Name == cs.mode.Name) {
		return
	}
	cs.mode, cs.modeRank, cs.modeSrc = ct, rank, src
	if cs.on {
		c.note(n, fmt.Sprintf("mode %s（%s）%s", ct.Name, src, ct.Downlink))
		c.logf(n, "call", "info", "ST#%d 通信種別 %s（%s）", n, ct.Name, src)
	}
	c.mu.Lock()
	c.car.Slots[n].Mode = ct.Name
	c.car.Slots[n].ModeNote = ct.Downlink
	c.mu.Unlock()
}

func (c *Channel) modeFromCall(n int, call *l3.Call, ctl float64, src string) {
	if call == nil {
		return
	}
	ct, ok := l3.ClassifyCall(call, c.std.nets.groupHalf(ctl, c.orgName()))
	if !ok {
		return
	}
	c.setMode(n, ct, modeCC, src)
}

func (c *Channel) modeAtStart(n int) {
	if ct, ok := c.std.nets.special(c.cfg.MHz, n); ok {
		c.setMode(n, ct, modeSpecial, "報知情報・システム拡張情報")
	}
	for m := range c.calls {
		if m != n && c.calls[m].on && math.Abs(c.calls[m].t0-c.calls[n].t0) < partnerSec {
			c.calls[m].stats.partner = true
			c.calls[n].stats.partner = true
		}
	}
}

func (c *Channel) modeInfer(n int) {
	cs := &c.calls[n]
	if cs.modeRank > modeInferred {
		return
	}
	if ct, ok := cs.stats.infer(); ok {
		c.setMode(n, ct, modeInferred, "同期バースト・B/I・音声の途切れ方から")
	}
}

func (cs *callState) modeLabel() string {
	return strings.TrimSpace(cs.mode.Name)
}
