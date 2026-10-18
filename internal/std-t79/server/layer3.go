package server


import (
	"fmt"
	"math"
	"strings"
	"sync"

	"github.com/symysak/arib/internal/std-t79/decoder"
	"github.com/symysak/arib/internal/std-t79/l1"
	"github.com/symysak/arib/internal/std-t79/l2"
	"github.com/symysak/arib/internal/std-t79/l3"
	"github.com/symysak/arib/internal/voice"
)

type Row struct {
	Group string `json:"g"`
	Name  string `json:"n"`
	Value string `json:"v"`
}

type hint struct {
	mhz      float64
	slot     int
	scramble int
	call     *l3.Call
	codec    string
	from     string
	org      string
	ctl      float64
	src      string
	maker    int
	up bool
}

type hintBoard struct {
	mu    sync.Mutex
	items []hint
}

const matchMHz = GridHz / 2 / 1e6

func (b *hintBoard) put(h hint) {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := b.items[:0]
	for _, x := range b.items {
		same := math.Abs(x.mhz-h.mhz) < matchMHz && x.slot == h.slot && x.up == h.up
		if same && h.call == nil && x.call != nil {
			h.call, h.src, h.ctl = x.call, x.src, x.ctl
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

func (b *hintBoard) take(mhz float64, up bool) []hint {
	b.mu.Lock()
	defer b.mu.Unlock()
	var got []hint
	out := b.items[:0]
	for _, x := range b.items {
		if math.Abs(x.mhz-mhz) < matchMHz && x.up == up {
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
		if h.slot >= 0 && h.slot < 4 && !(h.up && h.scramble < 0) {
			init := l1.InitAllOnes
			txt := "オール 1"
			if h.scramble >= 0 {
				init = l1.InitFromPattern(h.scramble)
				txt = fmt.Sprintf("0x%02X", h.scramble)
			}
			dir := "下り"
			if c.up != nil {
				dir = "上り"
			}
			if cur, known := c.scramble(h.slot); !known || cur != init {
				c.logf(h.slot, "call", "info", "無線チャネル指定（%s）: ST#%d の%sスクランブル %s", h.from, h.slot, dir, txt)
			}
			c.setScramble(h.slot, init)
		}
		if h.slot >= 0 && h.slot < 4 {
			if h.call != nil {
				cs := &c.calls[h.slot]
				cs.pendCall, cs.pendCtl, cs.pendSrc = h.call, h.ctl, h.src
				if cs.on {
					c.setCallInfoFrom(h.slot, h.call, h.ctl, h.src)
				}
			}
		}
		if h.codec != "" {
			c.codecHint(h.codec, "呼設定の伝達能力（"+h.from+"）")
		}
		if h.org != "" && c.orgName() == "" {
			c.setOrg(h.org + "（指定元）")
		}
		if h.maker > 0 && c.makerNum() == 0 {
			c.setMaker(h.maker, "無線チャネル指定の指定元 "+h.from)
		}
	}
}

func (c *Channel) codecHint(name, why string) {
	if c.vs.mode != "auto" || c.vs.chosen != nil {
		return
	}
	cd, ok := voice.Lookup(l1.VoiceChannel, name)
	if !ok {
		c.logf(-1, "call", "warn", "伝達能力が符号器 %s を名乗ったが、T79 の登録表に無い", name)
		return
	}
	c.setCodec(&cd, why)
	for m := range c.calls {
		cs := &c.calls[m]
		if !cs.on {
			continue
		}
		c.endTracker(m)
		cs.tr = c.newTracker(m)
		c.note(m, "voice_note 符号器が決まった: "+cd.Name+"（"+why+"）")
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
	dir, d3 := l2.DirDown, 1
	switch {
	case c.directOpt() != nil:
		dir, d3 = l2.DirUnknown, 0
	case c.up != nil:
		dir, d3 = l2.DirUp, 2
	}
	f := l2.ParseDir(b, dir)
	var sig *l3.Signal
	if f.CarriesInfo() && len(f.Info) > 0 {
		x := c.parseL3(f.Info, d3)
		sig = &x
	}
	c.emitLayers(s, ch, b, f, sig)
	if sig != nil {
		c.actOnVendorData(s, ch, f, b, *sig)
	}
	ms := ""
	if m, ok := f.Addr.MSInfo(); ok {
		ms = m.Text()
		c.seenOrg(m.System.Org)
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
	if sig != nil {
		c.act(s, ch, *sig, ms, addr)
	}
}

func (c *Channel) smsiTrack(s *decoder.Slot, ch string, f l2.Frame, ms string) (string, string) {
	a := f.Addr
	key := c.cfg.MHz
	if c.up != nil {
		key -= UpOffsetMHz
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

func piggy(s l3.Signal) string {
	var p []string
	for _, x := range []struct {
		on bool
		n  string
	}{{s.HasRT, "RT"}, {s.HasMM, "MM"}, {s.HasCC, "CC"}} {
		if x.on {
			p = append(p, x.n)
		}
	}
	return strings.Join(p, "+")
}

func (c *Channel) act(s *decoder.Slot, ch string, sig l3.Signal, ms, addr string) {
	var call *l3.Call
	codec := ""
	hasAssign := false
	for _, m := range sig.Messages {
		if m.Call != nil {
			call = m.Call
			if b := l3.BearerCodec(m); b != "" {
				codec = b
			}
		}
		hasAssign = hasAssign || m.Assign != nil
	}
	callSrc := "無線チャネル指定に相乗りした呼設定"
	ctlMHz := c.cfg.MHz
	if c.up != nil {
		ctlMHz -= UpOffsetMHz
	}
	if call == nil && hasAssign && ch == "CAC" {
		if pc, exact, ok := c.std.nets.takePend(ctlMHz, addr, s.TSec); ok {
			call, callSrc = pc, "ページングの通信形態（同じ L2 アドレス）"
			if !exact {
				callSrc = "ページングの通信形態（直前の 1 件。推定）"
			}
		}
	} else if call != nil && !hasAssign && ch == "CAC" && call.Mode != nil {
		c.std.nets.putPend(pendMode{ctlMHz: ctlMHz, addr: addr, call: call, t: s.TSec})
	}
	half := c.std.nets.groupHalf(ctlMHz, c.orgName())
	if ch != "CAC" {
		half = c.std.nets.groupHalf(0, c.orgName())
	}
	from := fmt.Sprintf("%.4f MHz ST#%d %s", c.cfg.MHz, s.Num, ch)
	cr := func(kind, text string) CallRec {
		r := CallRec{TSec: s.TSec, Slot: s.Num, Channel: c.chLabel(s, ch), Kind: kind, Text: text, ToSlot: -1, Ref: -1, ID: -1, MS: ms}
		if i := strings.LastIndex(ms, " 個別番号"); i > 0 {
			r.MSOrg = ms[:i]
		} else if i := strings.LastIndex(ms, " グループ番号"); i > 0 && !strings.HasPrefix(ms, "システムコード 0") {
			r.MSOrg = ms[:i]
		}
		if call != nil {
			r.Ref, r.Calling, r.Called, r.Display, r.Cause, r.Codec = call.Ref, call.Calling, call.Called, call.Display, call.Cause, codec
			if call.MSOrg != "" {
				r.MSOrg = call.MSOrg
				c.seenOrg(call.MSOrg)
			}
			if ct, ok := l3.ClassifyCall(call, half); ok {
				r.Mode = ct.Name
			}
		}
		return r
	}
	for _, m := range sig.Messages {
		switch {
		case m.Broadcast != nil:
			c.emitNet(s, m)
			c.std.nets.setBroadcast(c.cfg.MHz, c.orgName(), m.Broadcast)
			c.cacStructure(s, m.Broadcast)
		case m.Direct != nil:
			c.directAct(s, m)
		case m.Dedicated != nil:
			c.std.nets.setDedicated(c.cfg.MHz, m.Dedicated)
			for _, d := range m.Dedicated {
				if c.fresh(fmt.Sprintf("ded/%.4f/%d/%d", d.MHz, d.Slot, d.ID), s.TSec) {
					r := cr("RT システム拡張情報", fmt.Sprintf("専用チャネル %.4f MHz ST#%d 専用チャネル通信識別番号 %d", d.MHz, d.Slot, d.ID))
					r.MHz, r.ToSlot, r.Mode = d.MHz, d.Slot, "専用チャネル"
					c.emitCall(r)
				}
			}
		case m.Short != nil:
			sm := m.Short
			if c.fresh(fmt.Sprintf("sm/%d/%s/%s/%s/%v", s.Num, m.Name, sm.From, sm.To, sm.Number), s.TSec) {
				level := "info"
				if sm.Emergency {
					level = "warn"
				}
				c.logf(s.Num, "call", level, "RT %s: %s", m.Name, sm.Text)
				r := cr("RT "+m.Name, sm.Text)
				r.Calling, r.Called = sm.From, sm.To
				if sm.Emergency {
					r.Mode = "緊急連絡"
				}
				c.emitCall(r)
			}
		case m.Assign != nil:
			a := m.Assign
			if c.fresh("rt/"+a.Text, s.TSec) {
				c.logf(s.Num, "call", "info", "RT %s: %s", m.Name, a.Text)
				r := cr(m.Name, a.Text)
				r.MHz, r.ToSlot = a.MHz, a.Slot
				r.Scramble = "オール 1"
				if a.Scramble >= 0 {
					r.Scramble = fmt.Sprintf("0x%02X", a.Scramble)
				}
				c.emitCall(r)
			}
			if a.MHz > 0 && a.Slot >= 0 {
				c.std.hints.put(hint{mhz: a.MHz, slot: a.Slot, scramble: a.Scramble, call: call, codec: codec, from: from, org: c.orgName(),
					ctl: c.cfg.MHz, src: callSrc, maker: c.makerNum()})
				if a.UpMHz > 0 {
					c.std.hints.put(hint{mhz: a.UpMHz, slot: a.Slot, scramble: a.UpScramble, call: call, codec: codec, from: from,
						org: c.orgName(), ctl: c.cfg.MHz, src: callSrc, up: true, maker: c.makerNum()})
				}
				if c.std.hooks != nil && c.std.hooks.assigned != nil {
					c.std.hooks.assigned(a.MHz)
				}
			}
		case m.Paging != nil:
			if c.fresh("pg/"+m.Paging.Text, s.TSec) {
				c.logf(s.Num, "call", "info", "RT %s: %s", m.Name, m.Paging.Text)
				c.emitCall(cr(m.Name, m.Paging.Text))
			}
		case m.Entity == "RT" && (strings.Contains(m.Name, "解放") || strings.Contains(m.Name, "切断") || strings.Contains(m.Name, "再発呼") ||
			strings.Contains(m.Name, "送信") || strings.Contains(m.Name, "切り戻し")):
			if c.fresh(fmt.Sprintf("rt/%d/%s", s.Num, m.Name), s.TSec) {
				c.logf(s.Num, "call", "info", "RT %s", m.Name)
				c.emitCall(cr(m.Name, l3Summary(m)))
			}
			if c.directOpt() != nil && m.Type == 0x81 && c.calls[s.Num].on {
				c.endCall(s.Num, c.tNow(), "無線チャネル切断（RT 0x81）")
			}
		case m.Entity == "MM":
			if c.fresh(fmt.Sprintf("mm/%d/%s/%x", s.Num, m.Name, m.Raw), s.TSec) {
				c.emitCall(cr("MM "+m.Name, l3Summary(m)))
			}
		}
	}
	if call != nil {
		if c.fresh(fmt.Sprintf("cc/%d/%s", s.Num, call.Text), s.TSec) {
			c.logf(s.Num, "call", "info", "CC %s", call.Text)
			kind := "CC"
			for _, m := range sig.Messages {
				if m.Call != nil {
					kind = "CC " + m.Name
				}
			}
			c.emitCall(cr(kind, call.Text))
		}
		if ch != "CAC" {
			c.setCallInfoFrom(s.Num, call, 0, ch)
		}
	}
	if codec != "" && ch != "CAC" {
		c.codecHint(codec, "呼設定の伝達能力（"+from+"）")
	}
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

func (c *Channel) setCallInfoFrom(n int, call *l3.Call, ctl float64, src string) {
	c.modeFromCall(n, call, ctl, src)
	c.markData(n, call)
	cs := &c.calls[n]
	peer := callPeer(call)
	if cs.data {
		peer = strings.TrimSpace(peer + " 非音声（データ）")
	}
	if peer != "" {
		cs.peer = peer
		if cs.on {
			c.checkHandoff(n)
		}
	}
	if cs.lastCC != call.Text {
		cs.lastCC = call.Text
		c.note(n, fmt.Sprintf("cc %s（%s）", call.Text, src))
	}
	c.mu.Lock()
	if cs.peer != "" {
		c.car.Slots[n].Peer = cs.peer
	}
	c.mu.Unlock()
}

func callPeer(call *l3.Call) string {
	var p []string
	if call.Calling != "" || call.Called != "" {
		p = append(p, fmt.Sprintf("%s → %s", orDash(call.Calling), orDash(call.Called)))
	}
	if call.Display != "" {
		p = append(p, call.Display)
	}
	return strings.Join(p, " ")
}

func orDash(s string) string {
	if s == "" {
		return "?"
	}
	return s
}

func (c *Channel) cacStructure(s *decoder.Slot, bc *l3.Broadcast) {
	if c.dec == nil || c.cacOff {
		return
	}
	if s.CACCh != l1.CACUnknown && s.CACCh != l1.CACBCCH {
		c.cacMiss++
		if c.cacMiss >= 3 {
			c.cacOff = true
			c.dec.ClearCACStructure()
			c.logf(s.Num, "l2", "warn", "報知情報が %s（SF 位置 %d）で組み上がった: 制御チャネル構造情報の配置と合わないので、CAC を機能チャネルに分けない", s.CACCh, s.SFPos+1)
		}
		return
	}
	if st := bc.Structure; st != nil && st.Nc == 1 {
		c.dec.SetCACStructure(l1.CACStructure{AB: st.AB, AS1: st.AS1, AP: st.AP, AS2: st.AS2, Np: st.Np})
	}
}
