package server


import (
	"fmt"
	"math"
	"sync"

	"github.com/symysak/arib/internal/rcr39qpsk/decoder"
	"github.com/symysak/arib/internal/rcr39qpsk/l1"
	"github.com/symysak/arib/internal/rcr39qpsk/l2"
	"github.com/symysak/arib/internal/voice"
)

type Row struct {
	Group string `json:"g"`
	Name  string `json:"n"`
	Value string `json:"v"`
}

type hintBoard struct {
	mu    sync.Mutex
	items []Hint
}

const matchMHz = GridHz / 2 / 1e6

func (b *hintBoard) put(h Hint) {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := b.items[:0]
	for _, x := range b.items {
		same := math.Abs(x.MHz-h.MHz) < matchMHz && x.Slot == h.Slot && x.Up == h.Up
		if same && h.Call == nil && x.Call != nil {
			h.Call, h.Src, h.Ctl = x.Call, x.Src, x.Ctl
		}
		if !same {
			out = append(out, x)
		}
	}
	b.items = append(out, h)
	if len(b.items) > 64 {
		b.items = b.items[len(b.items)-64:]
	}
}

func (b *hintBoard) take(mhz float64, up bool) []Hint {
	b.mu.Lock()
	defer b.mu.Unlock()
	var got []Hint
	out := b.items[:0]
	for _, x := range b.items {
		if math.Abs(x.MHz-mhz) < matchMHz && x.Up == up {
			got = append(got, x)
		} else {
			out = append(out, x)
		}
	}
	b.items = out
	return got
}

func (c *Channel) applyHints() {
	for _, h := range c.std.hints.take(c.cfg.MHz, c.up != nil) {
		if h.Slot >= 0 && h.Slot < 4 && !(h.Up && h.Scramble < 0) {
			init := l1.InitAllOnes
			txt := "オール 1"
			if h.Scramble >= 0 {
				init = l1.InitFromPattern(h.Scramble)
				txt = fmt.Sprintf("0x%02X", h.Scramble)
			}
			dir := "下り"
			if c.up != nil {
				dir = "上り"
			}
			if cur, known := c.scramble(h.Slot); !known || cur != init {
				c.logf(h.Slot, "call", "info", "無線チャネル指定（%s）: ST#%d の%sスクランブル %s", h.From, h.Slot, dir, txt)
			}
			c.setScramble(h.Slot, init)
		}
		if h.Slot >= 0 && h.Slot < 4 && h.Call != nil {
			c.link.HintCall(h.Slot, h)
		}
		if h.Codec != "" {
			c.codecHint(h.Codec, "呼設定の伝達能力（"+h.From+"）")
		}
		if h.Org != "" && c.orgName() == "" {
			c.setOrg(h.Org + "（指定元）")
		}
		c.link.HintExt(h)
	}
}

func (c *Channel) codecHint(name, why string) {
	if c.vs.mode != "auto" || c.vs.chosen != nil {
		return
	}
	cd, ok := voice.Lookup(c.std.spec.VoiceChannel, name)
	if !ok {
		c.logf(-1, "call", "warn", "伝達能力が符号器 %s を名乗ったが、%s の登録表に無い", name, c.std.spec.Name)
		return
	}
	cd = voice.Preferred(cd)
	c.setCodec(&cd, why)
	for m := range c.calls {
		cs := &c.calls[m]
		if !cs.on {
			continue
		}
		c.endTracker(m)
		cs.tr = c.newTracker(m)
		c.callEvent(m, "voice_note", "符号器が決まった: "+cd.Name+"（"+why+"）")
		for _, h := range cs.hist {
			c.pushTracker(m, h)
		}
		cs.hist = nil
	}
}

func (c *Channel) layers(s *decoder.Slot) {
	n := s.Num
	if s.Message != nil {
		c.l2msg(s, "CAC", s.Message)
	}
	if s.Recovered != nil {
		c.recovered(s, "CAC", s.Recovered)
	}
	if u := s.SACCH; u != nil {
		if b := c.sacchR[n].Push(l2.Unit{First: u.W.First, Last: u.W.Last, WO: u.W.WO, Data: u.Bytes()}); b != nil {
			c.l2msg(s, "SACCH", b)
			if r, ok := c.sacchR[n].Recover(b, 6, 6); ok {
				c.recovered(s, "SACCH", r)
			}
		}
	}
	for _, l := range s.FACCHLate {
		u := l.Unit
		if b := c.facchR[n].Push(l2.Unit{First: u.W.First, Last: u.W.Last, WO: u.W.WO, Data: u.Bytes()}); b != nil {
			c.l2msg(s, "FACCH", b)
		}
	}
	if u := s.FACCH; u != nil {
		fch := "FACCH"
		if o := c.directOpt(); o != nil {
			fch = "DACCH"
			if o.Control {
				fch = "DCCH"
			}
		}
		if b := c.facchR[n].Push(l2.Unit{First: u.W.First, Last: u.W.Last, WO: u.W.WO, Data: u.Bytes()}); b != nil {
			c.l2msg(s, fch, b)
			if r, ok := c.facchR[n].Recover(b, 12, 12); ok {
				c.recovered(s, fch, r)
			}
		}
	}
}

func (c *Channel) recovered(s *decoder.Slot, ch string, b []byte) {
	c.logf(s.Num, "l2", "info", "%s: 部分再送（I'）で元の I フレーム %d オクテットを組み直した", c.chLabel(s, ch), len(b))
	c.l2msg(s, ch, b)
}

func (c *Channel) l2msg(s *decoder.Slot, ch string, b []byte) {
	dir := l2.DirDown
	switch {
	case c.directOpt() != nil:
		dir = l2.DirUnknown
	case c.up != nil:
		dir = l2.DirUp
	}
	f := l2.ParseDir(b, dir)
	var r *L3Result
	if f.CarriesInfo() && len(f.Info) > 0 {
		r = c.link.L3(s, ch, f)
	}
	c.emitLayers(s, ch, b, f, r)
	if r != nil {
		c.link.AfterLayers(s, ch, f, b, r)
	}
	ms := ""
	if text, org, ok := c.link.MSI(f.Addr); ok {
		ms = text
		c.seenOrg(org)
	}
	addr := f.Addr.Key()
	if ms != "" {
		addr = "MS:" + ms
	}
	var who string
	ms, who = c.smsiTrack(s, ch, f, ms)
	if who != "" {
		addr = "MS:" + who
	}
	if mg, ok := f.Management(); ok && c.fresh(fmt.Sprintf("mg/%s/%s", f.Addr.Key(), mg.Name), s.TSec) {
		r := CallRec{TSec: s.TSec, Slot: s.Num, Channel: c.chLabel(s, ch), Kind: "SMSI チェック", Text: mg.Name, ToSlot: -1, Ref: -1, ID: -1, MS: ms}
		c.emitCall(r)
	}
	if r != nil {
		c.link.Act(s, ch, f, r, ms, addr)
	}
}

func (c *Channel) smsiTrack(s *decoder.Slot, ch string, f l2.Frame, ms string) (string, string) {
	a := f.Addr
	key := c.cfg.MHz
	if c.up != nil {
		key -= c.std.spec.UpOffsetMHz
	}
	switch {
	case a.AC == 1 && a.HasMSI() && a.HasSMSI() && ms != "":
		if c.std.echoes.setSMSI(key, a.SMSI, ms) {
			c.logf(s.Num, "call", "info", "SMSI 0x%02X を割当: %s（%s）", a.SMSI, ms, c.chLabel(s, ch))
		}
	case a.AC == 2 && a.HasSMSI():
		if old, ok := c.std.echoes.clearSMSI(key, a.SMSI); ok {
			c.logf(s.Num, "call", "info", "SMSI 0x%02X を解除: %s", a.SMSI, old)
		}
	case a.HasSMSI() && !a.HasMSI():
		if m, ok := c.std.echoes.lookupSMSI(key, a.SMSI); ok {
			return fmt.Sprintf("SMSI 0x%02X = %s", a.SMSI, m), m
		}
		return fmt.Sprintf("SMSI 0x%02X（割当を見ていない）", a.SMSI), ""
	}
	return ms, ""
}

const freshSec = 30

func (c *Channel) fresh(key string, t float64) bool {
	if c.seen == nil {
		c.seen = map[string]float64{}
	}
	if last, ok := c.seen[key]; ok && t-last < freshSec {
		c.seen[key] = t
		return false
	}
	c.seen[key] = t
	if len(c.seen) > 512 {
		for k, v := range c.seen {
			if t-v > freshSec {
				delete(c.seen, k)
			}
		}
	}
	return true
}
