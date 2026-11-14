package server

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/symysak/arib/internal/nxdn/decoder"
	nxl1 "github.com/symysak/arib/internal/nxdn/l1"
	nx "github.com/symysak/arib/internal/nxdn/server"
	"github.com/symysak/arib/internal/std-t98/fsk4/chplan"
	"github.com/symysak/arib/internal/std-t98/fsk4/l1"
	"github.com/symysak/arib/internal/std-t98/fsk4/l3"
)

const (
	maxRadios = 64
	maxGroups = 32
)

type Radio struct {
	CSM   string `json:"csm"`
	Class string `json:"class"`
	N     int     `json:"n"`
	LastT float64 `json:"last_t"`
	UC     int  `json:"uc"`
	Maker  int  `json:"maker"`
	Cipher bool `json:"cipher"`
}

type Group struct {
	UC     int  `json:"uc"`
	Maker  int  `json:"maker"`
	Cipher bool `json:"cipher"`
	Key    int    `json:"key"`
	KeyHow string `json:"key_how,omitempty"`
	Txs      int     `json:"txs"`
	Sec      float64 `json:"sec"`
	VoiceSec float64 `json:"voice_sec"`
	LastT    float64 `json:"last_t"`
	Radios []string `json:"radios"`
	Types []string `json:"types"`
}

type link struct {
	h   nx.Host
	ch  *chplan.Channel
	asm *l3.Asm
	cfg      map[int]int
	found    map[int]int
	lastHint string
	ledgerKey int

	radios map[string]*Radio
	groups map[int]*Group

	tx struct {
		start, last float64
		uc, call    int
		maker       int
		pend  [3]int
		csm   string
		rank  int
		voice int
		types []string
	}
	sb0, pichOK int
}

func newLink(h nx.Host, keys map[int]int) *link {
	k := &link{h: h, asm: l3.NewAsm(), cfg: keys, found: map[int]int{}, radios: map[string]*Radio{}, groups: map[int]*Group{}}
	if c, ok := chplan.Lookup(h.MHz()); ok {
		k.ch = &c
	}
	k.lastHint = h.VoiceHint()
	if v, ok := strings.CutPrefix(k.lastHint, "ambe:"); ok {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			k.ledgerKey = n
		}
	}
	k.resetTx()
	h.EditStation(func(st *Station) { k.putExt(st) }, false)
	return k
}

func (k *link) resetTx() {
	k.tx.start, k.tx.last = -1, -1
	k.tx.uc, k.tx.call, k.tx.maker = -1, -1, -1
	k.tx.pend = [3]int{-1, -1, -1}
	k.tx.csm, k.tx.rank, k.tx.voice, k.tx.types = "", 0, 0, nil
}

var txKinds = []string{"不明", "同期バーストだけ", "終話だけ", "データ（非音声）", "音声"}

func kindRank(m l1.Mode) int {
	switch m {
	case l1.ModeSync:
		return 1
	case l1.ModeIdle:
		return 2
	case l1.ModeNonVoice1, l1.ModeNonVoice2:
		return 3
	case l1.ModeVoice, l1.ModeNV2Voice, l1.ModeVoiceNV2:
		return 4
	}
	return 0
}

func (k *link) TxStart() {
	k.resetTx()
	k.asm.Reset()
	k.h.Tx(func(tx *TxRec) bool {
		tx.Ext = map[string]any{}
		if k.ch != nil {
			tx.Ext["class"], tx.Ext["ch"] = string(k.ch.Class), k.ch.No
		}
		return false
	})
}

func (k *link) TxEnd(t float64, opened bool) {
	k.learnKey()
	if opened && k.tx.uc >= 0 {
		g := k.group(k.tx.uc)
		g.Txs++
		if k.tx.start >= 0 {
			g.Sec += k.tx.last - k.tx.start + nxl1.FrameSec
		}
		g.VoiceSec += float64(k.tx.voice) * nxl1.FrameSec / 2
		g.LastT = t
		k.h.EditStation(func(st *Station) { k.putExt(st) }, false)
	}
	k.resetTx()
	k.asm.Reset()
}

func (k *link) Reset() { k.asm.Reset() }

func (k *link) group(uc int) *Group {
	g := k.groups[uc]
	if g == nil {
		if len(k.groups) >= maxGroups {
			old, ot := -1, 0.0
			for u, x := range k.groups {
				if old < 0 || x.LastT < ot {
					old, ot = u, x.LastT
				}
			}
			delete(k.groups, old)
		}
		g = &Group{UC: uc, Maker: -1}
		k.groups[uc] = g
	}
	return g
}

func (k *link) keyFor(uc int) (key int, how string) {
	if v, ok := k.cfg[uc]; ok {
		return v, "設定"
	}
	if v, ok := k.cfg[AnyUC]; ok {
		return v, "設定"
	}
	if v, ok := k.found[uc]; ok && v > 0 {
		return v, "当てた"
	}
	return 0, ""
}

func (k *link) learnKey() {
	hint := k.h.VoiceHint()
	if hint == k.lastHint {
		return
	}
	k.lastHint = hint
	v, ok := strings.CutPrefix(hint, "ambe:")
	n, err := strconv.Atoi(v)
	if !ok || err != nil || k.tx.uc < 0 {
		return
	}
	k.found[k.tx.uc] = n
	if n > 0 {
		k.h.Logf("call", "info", "UC %d の秘話鍵 %d を当てた", k.tx.uc, n)
	}
	k.h.EditStation(func(st *Station) { k.putExt(st) }, true)
}

func (k *link) putExt(st *Station) {
	if k.ch != nil {
		st.Ext["ch"] = *k.ch
	}
	rs := make([]Radio, 0, len(k.radios))
	for _, r := range k.radios {
		rs = append(rs, *r)
	}
	sort.Slice(rs, func(i, j int) bool { return rs[i].LastT > rs[j].LastT })
	gs := make([]Group, 0, len(k.groups))
	for _, g := range k.groups {
		c := *g
		c.Key, c.KeyHow = k.keyFor(g.UC)
		c.Radios = append([]string{}, g.Radios...)
		c.Types = append([]string{}, g.Types...)
		gs = append(gs, c)
	}
	sort.Slice(gs, func(i, j int) bool { return gs[i].LastT > gs[j].LastT })
	st.Ext["radios"], st.Ext["groups"] = rs, gs
	st.Ext["sb0"], st.Ext["pich_ok"] = k.sb0, k.pichOK
	switch k.tx.call {
	case l3.CallNormal:
		st.Ext["call"] = "通常通信"
	case l3.CallCipher:
		st.Ext["call"] = "秘話通信"
	}
}

func (k *link) Frame(f decoder.Frame, kd nxl1.Keyed) nx.Reading {
	p := kd.Parsed
	r := l1.ReadRICH(p.RICH)
	mode := r.Mode()
	rd := nx.Reading{Mode: r.String(), Kind: mode.String(), D: -1, User: -1}
	k.learnKey()
	if !p.Layout.Valid {
		return rd
	}
	if k.tx.start < 0 {
		k.tx.start = f.TimeSec
	}
	k.tx.last = f.TimeSec
	dirty := false
	var msgs []nx.MsgRec
	if s := p.SACCH; s != nil {
		if s.CRCOK {
			u := l1.ReadUnit(s.Head)
			trust := !u.First
			if u.First {
				call, uc, maker := l3.First(s.Data[:])
				if trust = k.acceptSACCH(p, mode, u, call, uc, maker); trust {
					rd.User = uc
					if call != k.tx.call || uc != k.tx.uc || maker != k.tx.maker {
						k.tx.call, k.tx.uc, k.tx.maker = call, uc, maker
						g := k.group(uc)
						g.Maker, g.Cipher, g.LastT = maker, call == l3.CallCipher, f.TimeSec
						k.h.Tx(func(tx *TxRec) bool {
							tx.Maker = maker
							tx.Ext["call"] = l3.CallName(call)
							tx.Ext["cipher"] = call == l3.CallCipher
							return true
						})
						dirty = true
					}
					if name := l3.TypeName(u.Type); !has(k.tx.types, name) {
						k.tx.types = append(k.tx.types, name)
						if g := k.group(uc); !has(g.Types, name) {
							g.Types = append(g.Types, name)
							dirty = true
						}
					}
				}
			}
			if !trust {
				k.asm.Reset()
			} else if m, ok := k.asm.Push(s.Head, s.Data[:]); ok {
				msgs = append(msgs, sacchMsg(f.TimeSec, m))
			}
		} else {
			k.asm.Reset()
		}
	}
	if mode == l1.ModeSync {
		k.sb0++
	}
	for i, h := range p.Half {
		switch {
		case h.Coded != nil && h.Coded.CRCOK && mode == l1.ModeSync:
			pi := l3.ParsePICH(nxl1.CodedBytes(h.Coded))
			msgs = append(msgs, pichMsg(f.TimeSec, pi, k.tx.uc))
			k.pichOK++
			if pi.Valid {
				k.onPICH(f.TimeSec, pi)
			}
			dirty = true
		case h.Coded != nil && h.Coded.CRCOK:
			b := nxl1.CodedBytes(h.Coded)
			msgs = append(msgs, nv2Msg(f.TimeSec, mode.HalfName(i), b, k.tx.uc))
		case h.Kind == nxl1.HalfTCH:
			k.tx.voice++
		}
	}
	if dirty {
		k.h.EditStation(func(st *Station) { k.putExt(st) }, false)
	}
	if n := kindRank(mode); n > k.tx.rank {
		k.tx.rank = n
	}
	rd.TxKind = txKinds[k.tx.rank]
	rd.Msgs = msgs
	rd.EndAfterVoice = mode == l1.ModeIdle
	return rd
}

func (k *link) acceptSACCH(p nxl1.Parsed, mode l1.Mode, u l1.Unit, call, uc, maker int) bool {
	if call > l3.CallCipher || !(u.Type <= l3.TypeVoice || l3.IsMakerDef(u.Type) || u.Type == l3.TypeEnd) {
		return false
	}
	now := [3]int{call, uc, maker}
	prev := k.tx.pend
	k.tx.pend = now
	switch {
	case now == [3]int{k.tx.call, k.tx.uc, k.tx.maker}, now == prev:
		return true
	case mode == l1.ModeIdle && u.Type == l3.TypeEnd:
		return true
	}
	for _, h := range p.Half {
		if h.Coded != nil && h.Coded.CRCOK {
			return true
		}
	}
	return false
}

func has(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

func (k *link) onPICH(t float64, pi l3.PICH) {
	if k.tx.csm != pi.CSM {
		k.tx.csm = pi.CSM
		k.h.Tx(func(tx *TxRec) bool {
			tx.Name = pi.CSM
			tx.Ext["csm_class"] = l3.ClassName(pi.Class)
			return true
		})
	}
	r := k.radios[pi.CSM]
	if r == nil {
		if len(k.radios) >= maxRadios {
			old, ot := "", 0.0
			for c, x := range k.radios {
				if old == "" || x.LastT < ot {
					old, ot = c, x.LastT
				}
			}
			delete(k.radios, old)
		}
		r = &Radio{CSM: pi.CSM, Class: l3.ClassName(pi.Class), UC: -1, Maker: -1}
		k.radios[pi.CSM] = r
		k.h.Logf("call", "info", "呼出名称 %s（%s）", pi.CSM, l3.ClassName(pi.Class))
	}
	r.N++
	r.LastT = t
	if k.tx.uc >= 0 {
		r.UC, r.Maker, r.Cipher = k.tx.uc, k.tx.maker, k.tx.call == l3.CallCipher
		if g := k.group(k.tx.uc); !has(g.Radios, pi.CSM) {
			g.Radios = append(g.Radios, pi.CSM)
		}
	}
	k.h.EditStation(func(st *Station) { st.Caller = pi.CSM }, false)
}

func (k *link) VoiceOpt(codec string, opt map[string]string) {
	if _, set := opt["scramble"]; codec != "ambe" || set {
		return
	}
	delete(opt, "hint")
	key, how := 0, ""
	if k.tx.uc >= 0 {
		key, how = k.keyFor(k.tx.uc)
	} else if v, ok := k.cfg[AnyUC]; ok {
		key, how = v, "設定"
	}
	switch k.tx.call {
	case l3.CallNormal:
		opt["cipher"] = "off"
	case l3.CallCipher:
		opt["cipher"] = "on"
		switch {
		case how == "設定":
			opt["scramble"] = strconv.Itoa(key)
		case key > 0:
			opt["hint"] = "ambe:" + strconv.Itoa(key)
		case k.ledgerKey > 0:
			opt["hint"] = "ambe:" + strconv.Itoa(k.ledgerKey)
		}
	default:
		if key > 0 {
			opt["hint"] = "ambe:" + strconv.Itoa(key)
		}
	}
}

func (k *link) Profile() nx.Profile {
	p := nx.Profile{Std: "STD-T98 第 3 編"}
	var impl []string
	if k.ch != nil {
		impl = append(impl, k.ch.Label())
	}
	st := k.h.Station()
	if st.Maker > 0 {
		impl = append(impl, fmt.Sprintf("製造者 %d", st.Maker))
	} else if st.Maker == 0 {
		impl = append(impl, "共通製造者番号")
	}
	p.Impl = strings.Join(impl, "・")
	if cd := k.h.Codec(); cd == "ambe" {
		p.Codec = "AMBE+2 3600x2450（EHR）"
	} else if cd != "" {
		p.Codec = cd
	}
	p.Keys = append(p.Keys, nx.KV{Name: "ホワイトニングの初期値", Value: fmt.Sprintf("0x%03X（規格の値）", l1.WhiteningInit)})
	ucs := make([]int, 0, len(k.groups))
	for uc := range k.groups {
		ucs = append(ucs, uc)
	}
	sort.Ints(ucs)
	for _, uc := range ucs {
		g := k.groups[uc]
		v := "通常通信"
		if g.Cipher {
			v = "秘話通信"
			switch key, how := k.keyFor(uc); {
			case key > 0:
				v += fmt.Sprintf("・秘話鍵 %d（%s）", key, how)
			default:
				v += "・秘話鍵は分からない"
			}
		}
		p.Keys = append(p.Keys, nx.KV{Name: fmt.Sprintf("UC %d", uc), Value: v})
	}
	return p
}


func rowsTree(label, value string, rows []l3.Row) []Node {
	n := Node{Label: label, Value: value}
	for _, r := range rows {
		n.Children = append(n.Children, Node{Label: r.Name, Value: r.Text, Bits: r.Bits})
	}
	return []Node{n}
}

func sacchMsg(t float64, m l3.Message) nx.MsgRec {
	return nx.MsgRec{TSec: t, U: m.UC, Repeat: 1, Tree: rowsTree("SACCH", m.Name, m.Rows()),
		Key: fmt.Sprintf("SACCH/%X", m.Raw),
		Msg: nx.Message{Ch: "SACCH", Type: int(m.Type), Name: m.Name, Summary: m.Summary(), Maker: m.Maker, Raw: m.Raw, End: m.Type == l3.TypeEnd}}
}

func pichMsg(t float64, p l3.PICH, uc int) nx.MsgRec {
	return nx.MsgRec{TSec: t, U: uc, Repeat: 1, Tree: rowsTree("PICH", "パラメータ情報チャネル（同期バースト）", p.Rows()),
		Key: fmt.Sprintf("PICH/%X", p.Raw),
		Msg: nx.Message{Ch: "PICH", Type: -1, Name: "呼出名称 " + p.CSM, Summary: p.Summary(), Maker: -1, Raw: p.Raw}}
}

func nv2Msg(t float64, ch string, b [10]byte, uc int) nx.MsgRec {
	hex := strings.ToUpper(fmt.Sprintf("% x", b[:]))
	return nx.MsgRec{TSec: t, U: uc, Repeat: 1,
		Tree: []Node{{Label: ch, Value: "TCH（非音声 2・誤り訂正あり）", Children: []Node{
			{Label: "情報 80 ビット", Value: hex, Note: "中身の書式は規格に無い（製造者・利用者が決める）"},
			{Label: "文字として", Value: printable(b[:])},
		}}},
		Key: fmt.Sprintf("%s/%X", ch, b),
		Msg: nx.Message{Ch: ch, Type: -1, Name: "非音声 2", Summary: "非音声 2 " + hex, Maker: -1, Raw: b[:]}}
}

func printable(b []byte) string {
	out := make([]byte, len(b))
	for i, x := range b {
		if x >= 0x20 && x < 0x7F {
			out[i] = x
		} else {
			out[i] = '.'
		}
	}
	return string(out)
}
