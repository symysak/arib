package server


import (
	"fmt"
	"strings"

	"github.com/symysak/arib/internal/rcr39qpsk/decoder"
	"github.com/symysak/arib/internal/rcr39qpsk/l1"
	"github.com/symysak/arib/internal/rcr39qpsk/l2"
	fam "github.com/symysak/arib/internal/rcr39qpsk/server"
	"github.com/symysak/arib/internal/std-t79/l3"
	"github.com/symysak/arib/internal/std-t79/numbering"
)

type slotT79 struct {
	pendCall *l3.Call
	pendCtl  float64
	pendSrc  string
	mode     l3.CallType
	modeRank int
	modeSrc  string
	stats    modeStats
	lastCC   string
	data bool
	vclock string
}

type link struct {
	h  fam.Host
	sh *shared
	st [4]slotT79
	maker int
	vend vendorState
	cacMiss int
	cacOff  bool
	dirScr int
}

func newLink(h fam.Host, sh *shared) *link { return &link{h: h, sh: sh, dirScr: -1} }


func (l *link) L3(s *decoder.Slot, ch string, f l2.Frame) *fam.L3Result {
	d3 := 1
	switch {
	case l.h.Direct() != nil:
		d3 = 0
	case l.h.Up():
		d3 = 2
	}
	sig := l.parseL3(f.Info, d3)
	r := &fam.L3Result{Summary: sig.Summary(), Piggy: fam.Node{Label: "相乗り表示部", Value: piggy(sig), Note: sig.Err}, Sig: sig}
	for _, m := range sig.Messages {
		r.Messages = append(r.Messages, fam.L3Msg{Entity: m.Entity, Name: m.Name, Summary: l3Summary(m), Err: m.Err, Tree: msgNode(m), Raw: m.Raw})
	}
	return r
}

func (l *link) AfterLayers(s *decoder.Slot, ch string, f l2.Frame, raw []byte, r *fam.L3Result) {
	l.actOnVendorData(s, ch, f, raw, r.Sig.(l3.Signal))
}

func (l *link) Act(s *decoder.Slot, ch string, f l2.Frame, r *fam.L3Result, ms, addr string) {
	l.act(s, ch, r.Sig.(l3.Signal), ms, addr)
}

func (l *link) MSI(a l2.Address) (string, string, bool) {
	m, ok := numbering.AddressMSI(a)
	if !ok {
		return "", "", false
	}
	return m.Text(), m.System.Org, true
}

func (l *link) L2Fields() l2.MSIFields { return numbering.L2Fields }
func (l *link) FoldKey() int           { return l.makerNum() }

func (l *link) RCHText(info []uint8, up bool) string { return l1.RCHVendorText(l.makerNum(), info, up) }

func (l *link) HintCall(slot int, h fam.Hint) {
	call, _ := h.Call.(*l3.Call)
	if call == nil {
		return
	}
	st := &l.st[slot]
	st.pendCall, st.pendCtl, st.pendSrc = call, h.Ctl, h.Src
	if l.h.CallOn(slot) {
		l.setCallInfoFrom(slot, call, h.Ctl, h.Src)
	}
}

func (l *link) HintExt(h fam.Hint) {
	if maker, _ := h.Ext.(int); maker > 0 && l.makerNum() == 0 {
		l.setMaker(maker, "無線チャネル指定の指定元 "+h.From)
	}
}

func (l *link) CallOpen(n int) {
	st := &l.st[n]
	*st = slotT79{pendCall: st.pendCall, pendCtl: st.pendCtl, pendSrc: st.pendSrc, stats: modeStats{sbPrev: -1}}
}

func (l *link) CallStart(n int) {
	l.modeAtStart(n)
	st := &l.st[n]
	if st.pendCall != nil {
		src := st.pendSrc
		if src == "" {
			src = "無線チャネル指定に相乗りした呼設定"
		}
		l.setCallInfoFrom(n, st.pendCall, st.pendCtl, src)
		st.pendCall = nil
	}
}

func (l *link) Observe(n int, s *decoder.Slot, voice bool) {
	st := &l.st[n]
	st.stats.observe(s, voice)
	if st.stats.voice > 0 && st.stats.voice%50 == 0 {
		l.modeInfer(n)
	}
}

func (l *link) CallEnding(n int)   { l.modeInfer(n) }
func (l *link) CallClosed(n int)   { l.st[n] = slotT79{} }
func (l *link) Drop(n int)         { l.st[n].vclock = "" }
func (l *link) Slot(*decoder.Slot) {}
func (l *link) Data(n int) bool    { return l.st[n].data }
func (l *link) Mode(n int) (string, string) {
	return l.st[n].mode.Name, l.st[n].mode.Downlink
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

func (l *link) act(s *decoder.Slot, ch string, sig l3.Signal, ms, addr string) {
	h := l.h
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
	ctlMHz := h.MHz()
	if h.Up() {
		ctlMHz -= UpOffsetMHz
	}
	if call == nil && hasAssign && ch == "CAC" {
		if pc, exact, ok := l.sh.nets.takePend(ctlMHz, addr, s.TSec); ok {
			call, callSrc = pc, "ページングの通信形態（同じ L2 アドレス）"
			if !exact {
				callSrc = "ページングの通信形態（直前の 1 件。推定）"
			}
		}
	} else if call != nil && !hasAssign && ch == "CAC" && call.Mode != nil {
		l.sh.nets.putPend(pendMode{ctlMHz: ctlMHz, addr: addr, call: call, t: s.TSec})
	}
	half := l.sh.nets.groupHalf(ctlMHz, h.Org())
	if ch != "CAC" {
		half = l.sh.nets.groupHalf(0, h.Org())
	}
	from := fmt.Sprintf("%.4f MHz ST#%d %s", h.MHz(), s.Num, ch)
	cr := func(kind, text string) CallRec {
		r := CallRec{TSec: s.TSec, Slot: s.Num, Channel: h.ChLabel(s, ch), Kind: kind, Text: text, ToSlot: -1, Ref: -1, ID: -1, MS: ms}
		if i := strings.LastIndex(ms, " 個別番号"); i > 0 {
			r.MSOrg = ms[:i]
		} else if i := strings.LastIndex(ms, " グループ番号"); i > 0 && !strings.HasPrefix(ms, "システムコード 0") {
			r.MSOrg = ms[:i]
		}
		if call != nil {
			r.Ref, r.Calling, r.Called, r.Display, r.Cause, r.Codec = call.Ref, call.Calling, call.Called, call.Display, call.Cause, codec
			if call.MSOrg != "" {
				r.MSOrg = call.MSOrg
				h.SeenOrg(call.MSOrg)
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
			l.emitNet(s, m)
			l.sh.nets.setBroadcast(h.MHz(), h.Org(), m.Broadcast)
			l.cacStructure(s, m.Broadcast)
		case m.Direct != nil:
			l.directAct(s, m)
		case m.Dedicated != nil:
			l.sh.nets.setDedicated(h.MHz(), m.Dedicated)
			for _, d := range m.Dedicated {
				if h.Fresh(fmt.Sprintf("ded/%.4f/%d/%d", d.MHz, d.Slot, d.ID), s.TSec) {
					r := cr("RT システム拡張情報", fmt.Sprintf("専用チャネル %.4f MHz ST#%d 専用チャネル通信識別番号 %d", d.MHz, d.Slot, d.ID))
					r.MHz, r.ToSlot, r.Mode = d.MHz, d.Slot, "専用チャネル"
					h.EmitCall(r)
				}
			}
		case m.Short != nil:
			sm := m.Short
			if h.Fresh(fmt.Sprintf("sm/%d/%s/%s/%s/%v", s.Num, m.Name, sm.From, sm.To, sm.Number), s.TSec) {
				level := "info"
				if sm.Emergency {
					level = "warn"
				}
				h.Logf(s.Num, "call", level, "RT %s: %s", m.Name, sm.Text)
				r := cr("RT "+m.Name, sm.Text)
				r.Calling, r.Called = sm.From, sm.To
				if sm.Emergency {
					r.Mode = "緊急連絡"
				}
				h.EmitCall(r)
			}
		case m.Assign != nil:
			a := m.Assign
			if h.Fresh("rt/"+a.Text, s.TSec) {
				h.Logf(s.Num, "call", "info", "RT %s: %s", m.Name, a.Text)
				r := cr(m.Name, a.Text)
				r.MHz, r.ToSlot = a.MHz, a.Slot
				r.Scramble = "オール 1"
				if a.Scramble >= 0 {
					r.Scramble = fmt.Sprintf("0x%02X", a.Scramble)
				}
				h.EmitCall(r)
			}
			if a.MHz > 0 && a.Slot >= 0 {
				var c any
				if call != nil {
					c = call
				}
				h.PutHint(fam.Hint{MHz: a.MHz, Slot: a.Slot, Scramble: a.Scramble, Call: c, Codec: codec, From: from, Org: h.Org(),
					Ctl: h.MHz(), Src: callSrc, Ext: l.makerNum()})
				if a.UpMHz > 0 {
					h.PutHint(fam.Hint{MHz: a.UpMHz, Slot: a.Slot, Scramble: a.UpScramble, Call: c, Codec: codec, From: from,
						Org: h.Org(), Ctl: h.MHz(), Src: callSrc, Up: true, Ext: l.makerNum()})
				}
			}
		case m.Paging != nil:
			if h.Fresh("pg/"+m.Paging.Text, s.TSec) {
				h.Logf(s.Num, "call", "info", "RT %s: %s", m.Name, m.Paging.Text)
				h.EmitCall(cr(m.Name, m.Paging.Text))
			}
		case m.Entity == "RT" && (strings.Contains(m.Name, "解放") || strings.Contains(m.Name, "切断") || strings.Contains(m.Name, "再発呼") ||
			strings.Contains(m.Name, "送信") || strings.Contains(m.Name, "切り戻し")):
			if h.Fresh(fmt.Sprintf("rt/%d/%s", s.Num, m.Name), s.TSec) {
				h.Logf(s.Num, "call", "info", "RT %s", m.Name)
				h.EmitCall(cr(m.Name, l3Summary(m)))
			}
			if h.Direct() != nil && m.Type == 0x81 && h.CallOn(s.Num) {
				h.EndCall(s.Num, "無線チャネル切断（RT 0x81）")
			}
		case m.Entity == "MM":
			if h.Fresh(fmt.Sprintf("mm/%d/%s/%x", s.Num, m.Name, m.Raw), s.TSec) {
				h.EmitCall(cr("MM "+m.Name, l3Summary(m)))
			}
		}
	}
	if call != nil {
		if h.Fresh(fmt.Sprintf("cc/%d/%s", s.Num, call.Text), s.TSec) {
			h.Logf(s.Num, "call", "info", "CC %s", call.Text)
			kind := "CC"
			for _, m := range sig.Messages {
				if m.Call != nil {
					kind = "CC " + m.Name
				}
			}
			h.EmitCall(cr(kind, call.Text))
		}
		if ch != "CAC" {
			l.setCallInfoFrom(s.Num, call, 0, ch)
		}
	}
	if codec != "" && ch != "CAC" {
		h.CodecHint(codec, "呼設定の伝達能力（"+from+"）")
	}
}

func (l *link) setCallInfoFrom(n int, call *l3.Call, ctl float64, src string) {
	l.modeFromCall(n, call, ctl, src)
	l.markData(n, call)
	st := &l.st[n]
	peer := callPeer(call)
	if st.data {
		peer = strings.TrimSpace(peer + " 非音声（データ）")
	}
	if peer != "" {
		l.h.SetPeer(n, peer)
	}
	if st.lastCC != call.Text {
		st.lastCC = call.Text
		l.h.CallEvent(n, "cc", fmt.Sprintf("%s（%s）", call.Text, src))
	}
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

func (l *link) cacStructure(s *decoder.Slot, bc *l3.Broadcast) {
	if l.h.Up() || l.cacOff {
		return
	}
	if s.CACCh != l1.CACUnknown && s.CACCh != l1.CACBCCH {
		l.cacMiss++
		if l.cacMiss >= 3 {
			l.cacOff = true
			l.h.ClearCACStructure()
			l.h.Logf(s.Num, "l2", "warn", "報知情報が %s（SF 位置 %d）で組み上がった: 制御チャネル構造情報の配置と合わないので、CAC を機能チャネルに分けない", s.CACCh, s.SFPos+1)
		}
		return
	}
	if st := bc.Structure; st != nil && st.Nc == 1 {
		l.h.SetCACStructure(l1.CACStructure{AB: st.AB, AS1: st.AS1, AP: st.AP, AS2: st.AS2, Np: st.Np})
	}
}

func (l *link) emitNet(s *decoder.Slot, m l3.Message) {
	b := m.Broadcast
	ni := NetInfo{Idx: l.h.ID(), TSec: s.TSec, MHz: l.h.MHz(), CC: s.CC, Control: b.Control, Neighbors: b.Neighbors, Area: -1}
	if sy := b.System; sy != nil {
		ni.Org, ni.Area, ni.Maker, ni.Zone, ni.Base = sy.Org, sy.Area, sy.Maker, sy.Zone, sy.Base
		ni.System = fmt.Sprintf("0x%0*X", 2*sy.CodeLen, sy.Code)
		if sy.Org != "" {
			l.h.SetOrg(sy.Org)
		}
		l.setMaker(sy.Maker, "網番号")
	}
	for _, f := range m.Fields {
		ni.Rows = append(ni.Rows, Row{Group: "RT", Name: f.Name, Value: f.Value})
	}
	l.h.EmitNet(ni)
}

func msgNode(m l3.Message) Node {
	n := Node{Label: m.Entity, Value: fmt.Sprintf("0x%02X %s", m.Type, m.Name), Note: m.Err}
	for _, f := range m.Fields {
		n.Children = append(n.Children, Node{Label: f.Name, Value: f.Value})
	}
	return n
}

func l3Summary(m l3.Message) string {
	switch {
	case m.Assign != nil:
		return m.Assign.Text
	case m.Call != nil:
		return m.Call.Text
	case m.Paging != nil:
		return m.Paging.Text
	case m.Broadcast != nil && m.Broadcast.System != nil:
		return "団体 " + orDash(m.Broadcast.System.Org)
	}
	if len(m.Fields) > 0 {
		f := m.Fields[0]
		return f.Name + " " + f.Value
	}
	return ""
}


func (l *link) directAct(s *decoder.Slot, m l3.Message) {
	d := m.Direct
	o := l.h.Direct()
	if o != nil && !o.Control {
		o.Control = true
	}
	mode := l1.ModeDirect
	if o != nil {
		mode = o.Mode
	}
	var text string
	switch m.Type {
	case 0x44:
		l.dirScr = d.Scramble
		text = fmt.Sprintf("候補 %v・スクランブル %s", mhzList(d.MHz), scrPattern(d.Scramble))
	case 0x45:
		if d.OK != nil && *d.OK {
			text = "OK " + mhzList(d.MHz)
		} else {
			text = "NG（空きチャネル無し）"
		}
	case 0x47:
		text = fmt.Sprintf("%s・スクランブル %s", mhzList(d.MHz), scrPattern(d.Scramble))
	}
	if l.h.Fresh(fmt.Sprintf("dir/%d/%s", m.Type, text), s.TSec) {
		l.h.Logf(s.Num, "call", "info", "RT %s: %s", m.Name, text)
		l.h.EmitCall(CallRec{TSec: s.TSec, Slot: s.Num, Channel: l.h.ChLabel(s, "DCCH"), Kind: "RT " + m.Name, Text: text, ToSlot: -1, Ref: -1, ID: -1})
	}
	scr := d.Scramble
	if m.Type == 0x45 {
		scr = l.dirScr
	}
	if m.Type == 0x44 || len(d.MHz) == 0 || d.MHz[0] <= 0 {
		return
	}
	l.h.OnDirect(d.MHz[0], scr, mode)
}

func mhzList(v []float64) string {
	var s []string
	for _, f := range v {
		if f > 0 {
			s = append(s, fmt.Sprintf("%.4f MHz", f))
		} else {
			s = append(s, "（計算できない）")
		}
	}
	return strings.Join(s, " / ")
}

func scrPattern(p int) string {
	if p < 0 {
		return "無し"
	}
	return fmt.Sprintf("0x%02X", p)
}
