package server


import (
	"fmt"
	"math"
	"strings"
	"sync"

	"github.com/symysak/arib/internal/data"
	"github.com/symysak/arib/internal/rcr39qpsk/decoder"
	"github.com/symysak/arib/internal/rcr39qpsk/l1"
	"github.com/symysak/arib/internal/rcr39qpsk/l2"
	fam "github.com/symysak/arib/internal/rcr39qpsk/server"
	"github.com/symysak/arib/internal/std-39/qpsk/l3"
	"github.com/symysak/arib/internal/std-39/qpsk/nonvoice"
	"github.com/symysak/arib/internal/voice"
)

type shared struct {
	mu sync.Mutex
	fo      float64
	foFixed bool
	ctl     map[int64]float64
	net     map[string]float64
	from    map[int64]int64
	byMHz   map[int64]float64
	procs map[string]*proc
	nvMethod string
}

func (s *shared) setNVMethod(m string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.nvMethod = m
}

func (s *shared) nvMethodNow() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.nvMethod
}

type proc struct {
	tr *l3.Tracker
	tc *l3.TimerChecker
	obs      []nonvoice.ProcObs
	nvMethod string
	nvProc   string
	last     float64
	kind     string
	up       bool
	down     bool
}

var nvTimerRules = nonvoice.TimerRules()

const (
	procGap  = 60.0
	procKeep = 300.0
)

func (s *shared) observe(key string, sig l3.Signal, dir int, ch string, t float64) (finds []l3.TimerFinding, kind, why string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.procs == nil {
		s.procs = map[string]*proc{}
	}
	for k, p := range s.procs {
		if t-p.last > procKeep {
			delete(s.procs, k)
		}
	}
	p := s.procs[key]
	if p == nil || t-p.last > procGap {
		p = &proc{tr: l3.NewTracker(), tc: l3.NewTimerChecker(nvTimerRules)}
		s.procs[key] = p
	}
	p.last = t
	if dir == l3.SeqUp {
		p.up = true
	} else {
		p.down = true
	}
	p.tr.AddSignal(sig, dir, ch, t)
	for _, m := range sig.Messages {
		finds = append(finds, p.tc.Add(l3.TimerEvent{Entity: m.Entity, Name: m.Name, Dir: dir, T: t})...)
		if len(p.obs) < 64 {
			p.obs = append(p.obs, nonvoice.ProcObs{Name: m.Name, Dir: dir})
		}
	}
	if m := nonvoice.MethodFromSignal(sig); m != "" {
		p.nvMethod = m
	}
	if p.nvMethod != "" {
		if pr, hits, ok := nonvoice.GuessProcedure(p.obs, p.nvMethod); ok && pr.Ref+pr.Name != p.nvProc {
			p.nvProc = pr.Ref + pr.Name
			return finds, pr.Name + "（" + pr.Ref + "）", fmt.Sprintf("付録の手順の図に %d 個当たる", hits)
		}
		return finds, "", ""
	}
	if !p.up || !p.down {
		return finds, "", ""
	}
	g := p.tr.Guess()
	if g.Kind == "" || !g.Kind.IsCall() || g.Text == p.kind {
		return finds, "", ""
	}
	p.kind = g.Text
	return finds, g.Text, "確度 " + g.Confidence + "（" + g.Why + "）"
}

func mhzKey(mhz float64) int64 { return int64(math.Round(mhz * 1e4)) }

func (s *shared) foFor(mhz float64, net string) float64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.foFixed {
		return s.fo
	}
	k := mhzKey(mhz)
	if fo, ok := s.ctl[k]; ok {
		return fo
	}
	if fo, ok := s.net[net]; ok && net != "" {
		return fo
	}
	if c, ok := s.from[k]; ok {
		if fo, ok := s.ctl[c]; ok {
			return fo
		}
	}
	return s.byMHz[k]
}

func codeMHz(fo float64, fc int) float64 {
	if fo == 0 || fc < 0 {
		return 0
	}
	return math.Round((fo+0.025*float64(fc))*1e6) / 1e6
}

func (s *shared) learn(mhz float64, fc int, net string) (fo float64, changed bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.foFixed || fc < 0 {
		return s.fo, false
	}
	fo = math.Round((mhz-0.025*float64(fc))*1e4) / 1e4
	s.init()
	k := mhzKey(mhz)
	old, had := s.ctl[k]
	s.ctl[k] = fo
	if net != "" {
		s.net[net] = fo
	}
	return fo, !had || math.Abs(old-fo) >= 0.0005
}

func (s *shared) init() {
	if s.ctl == nil {
		s.ctl, s.net, s.from, s.byMHz = map[int64]float64{}, map[string]float64{}, map[int64]int64{}, map[int64]float64{}
	}
}

func (s *shared) assigned(mhz, byMHz, fo float64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.init()
	k := mhzKey(mhz)
	if _, isCtl := s.ctl[k]; isCtl {
		return
	}
	root := mhzKey(byMHz)
	for i := 0; i < 8; i++ {
		if _, isCtl := s.ctl[root]; isCtl {
			break
		}
		next, ok := s.from[root]
		if !ok || next == root {
			break
		}
		root = next
	}
	if root != k {
		s.from[k] = root
	}
	if fo != 0 {
		s.byMHz[k] = fo
	}
}

func (l *link) freq(fc int) float64 { return codeMHz(l.sh.foFor(l.h.DownMHz(), l.net), fc) }

type slotState struct {
	pendCall *l3.Call
	pendSrc  string
	lastCC   string
	data     bool
	mode     string
	tr       seqTracker
	preCall *l3.Call
	preSrc  string
	at      float64
}

const preKeep = 2.0

func (l *link) dir() int {
	if l.h.Up() {
		return 2
	}
	return 1
}

func (l *link) L3(s *decoder.Slot, ch string, f l2.Frame) *fam.L3Result {
	sig := nonvoice.ParseSignal(f.Info, l3.Ctx{Dir: l.dir(), FreqMHz: l.freq})
	r := &fam.L3Result{Summary: sig.Summary(), Piggy: fam.Node{Label: "相乗り表示部", Value: piggy(sig), Note: sig.Err}, Sig: sig}
	for _, m := range sig.Messages {
		r.Messages = append(r.Messages, fam.L3Msg{Entity: m.Entity, Name: m.Name, Summary: summary(m), Err: m.Err, Tree: msgNode(m), Raw: m.Raw})
	}
	return r
}

func (l *link) AfterLayers(*decoder.Slot, string, l2.Frame, []byte, *fam.L3Result) {}

func (l *link) Act(s *decoder.Slot, ch string, f l2.Frame, r *fam.L3Result, ms, addr string) {
	l.actSig(s, ch, r.Sig.(l3.Signal), ms, addr)
}

func (l *link) actSig(s *decoder.Slot, ch string, sig l3.Signal, ms, addr string) {
	h := l.h
	var call *l3.Call
	codec := ""
	for _, m := range sig.Messages {
		if m.Call != nil {
			call = m.Call
			for _, b := range m.Call.Bearers {
				if b.Codec != "" && codec == "" {
					codec = codecName(b.Codec)
				}
			}
		}
	}
	if s.Num >= 0 && s.Num < 4 && ch != "CAC" {
		l.st[s.Num].tr.Observe(sig, l.dir(), ch, s.TSec)
		l.st[s.Num].at = h.Now()
		if k, _ := l.st[s.Num].tr.Kind(); k != "" {
			h.SetMode(s.Num, k, "")
		}
	}
	from := fmt.Sprintf("%.4f MHz ST#%d %s", h.MHz(), s.Num, ch)
	cr := func(kind, text string) fam.CallRec {
		r := fam.CallRec{TSec: s.TSec, Slot: s.Num, Channel: h.ChLabel(s, ch), Kind: kind, Text: text, ToSlot: -1, Ref: -1, ID: -1, MS: ms}
		if call != nil {
			r.Ref, r.Calling, r.Called, r.Display, r.Cause, r.Codec = call.Ref, call.Calling, call.Called, call.Display, call.Cause, codec
		}
		return r
	}
	l.userData(s, ch, sig, ms)
	l.nvSignal(s.Num, ch, sig)
	if addr != "" {
		finds, kind, why := l.sh.observe(addr, sig, l.dir(), ch, h.Now())
		for _, f := range finds {
			lv := "info"
			if f.Kind != "応答" {
				lv = "warn"
			}
			h.Logf(s.Num, "call", lv, "タイマ（上下の突き合わせ §4.3.8.11〜12）%s %s", ms, f.Text)
			if f.Kind != "応答" {
				h.EmitCall(cr("手順の検査", "タイマ "+f.Text))
			}
		}
		if kind != "" {
			h.Logf(s.Num, "call", "info", "制御シーケンス（上下の突き合わせ §4.3.8）%s %s %s", ms, kind, why)
			h.EmitCall(cr("制御シーケンス", kind+"（上下の突き合わせ）"))
		}
	}
	if p, bad := piggybackOf(sig); p != "" || len(bad) > 0 {
		if p != "" && h.Fresh("pg/"+p+fmt.Sprint(s.Num), s.TSec) {
			h.Logf(s.Num, "call", "info", "相乗り（§4.3.3.2）: %s", p)
		}
		for _, b := range bad {
			if h.Fresh("pgx/"+b+fmt.Sprint(s.Num), s.TSec) {
				h.Logf(s.Num, "call", "warn", "相乗り（§4.3.3.2）: %s", b)
			}
		}
	}
	for _, m := range sig.Messages {
		switch {
		case m.Broadcast != nil:
			l.broadcast(s, ch, m)
		case m.Assign != nil:
			a := m.Assign
			if h.Fresh("rt/"+a.Text, s.TSec) {
				h.Logf(s.Num, "call", "info", "RT %s: %s", m.Name, a.Text)
				r := cr(m.Name, a.Text)
				r.MHz, r.ToSlot = a.MHz, a.Slot
				if a.Scramble >= 0 {
					r.Scramble = fmt.Sprintf("0x%02X", a.Scramble)
				}
				h.EmitCall(r)
			}
			if a.MHz > 0 && a.Slot >= 0 && a.Scramble >= 0 {
				var c any
				if call != nil {
					c = call
				}
				l.sh.assigned(a.MHz, h.DownMHz(), l.sh.foFor(h.DownMHz(), l.net))
				for _, slot := range []int{a.Slot, a.Slot2} {
					if slot < 0 {
						continue
					}
					h.PutHint(fam.Hint{MHz: a.MHz, Slot: slot, Scramble: a.Scramble, Call: c, Codec: codec, From: from, Org: h.Org(),
						Ctl: h.MHz(), Src: "無線チャネル指定に相乗りした呼設定", Ext: nvHintOf(sig)})
				}
			}
		case m.Paging != nil:
			if h.Fresh("pg/"+m.Paging.Text, s.TSec) {
				h.Logf(s.Num, "call", "info", "RT %s: %s", m.Name, m.Paging.Text)
				h.EmitCall(cr(m.Name, m.Paging.Text))
			}
		case m.MM != nil:
			if h.Fresh(fmt.Sprintf("mm/%d/%x", s.Num, m.Raw), s.TSec) {
				h.EmitCall(cr("MM "+m.Name, m.MM.Text))
			}
		case m.Entity == "RT":
			if h.Fresh(fmt.Sprintf("rt/%d/%s/%x", s.Num, m.Name, m.Raw), s.TSec) {
				h.EmitCall(cr("RT "+m.Name, summary(m)))
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
			l.setCallInfo(s.Num, call, ch)
		}
	}
	if codec != "" && ch != "CAC" {
		h.CodecHint(codec, "呼設定の伝達能力（"+from+"）")
	}
}

func (l *link) userData(s *decoder.Slot, ch string, sig l3.Signal, ms string) {
	for _, m := range sig.Messages {
		if m.Entity != "CC" {
			continue
		}
		var uu string
		var rows []data.Row
		for _, f := range m.Fields {
			if f.Name == "ユーザ・ユーザ" {
				uu = f.Value
			}
			if f.Name != "規格の検査" {
				rows = append(rows, data.Row{Name: f.Name, Text: f.Value})
			}
		}
		if uu == "" || !l.h.Fresh(fmt.Sprintf("uu/%d/%x", s.Num, m.Raw), s.TSec) {
			continue
		}
		label := uu
		if i := strings.Index(uu, "ユーザ情報="); i >= 0 {
			label = uu[i+len("ユーザ情報="):]
		}
		l.h.EmitData(s.Num, data.Event{Kind: data.KindMessage, Format: "CC ユーザ・ユーザ", From: ms, Label: label, Rows: rows, Raw: m.Raw})
	}
}

var CodecAliases = map[string]string{}

func codecName(name string) string {
	if _, ok := voice.Lookup(VoiceChannel, name); ok {
		return name
	}
	if a, ok := CodecAliases[name]; ok {
		if _, ok := voice.Lookup(VoiceChannel, a); ok {
			return a
		}
	}
	return name
}

func (l *link) broadcast(s *decoder.Slot, ch string, m l3.Message) {
	b := m.Broadcast
	h := l.h
	if b.Network != "" {
		l.net = b.Network
	}
	ni := fam.NetInfo{Idx: h.ID(), TSec: s.TSec, MHz: h.MHz(), CC: s.CC, Area: -1, Org: b.Network, System: b.Network}
	if st := b.Structure; st != nil {
		for i, fc := range st.FC {
			if ch != "CAC" {
				break
			}
			slot := -1
			if i < len(st.Slots) {
				slot = st.Slots[i]
			}
			if (len(st.FC) == 1 && slot < 0) || slot == s.Num {
				if fo, changed := l.sh.learn(h.MHz(), fc, b.Network); changed {
					h.Logf(s.Num, "net", "info", "周波数コードの基準 Fo = %.4f MHz（報知の制御チャネル構造情報の F%d = %d がこの局 %.4f MHz）", fo, i+1, fc, h.MHz())
				}
				break
			}
		}
		for _, fc := range st.FC {
			ni.Control = append(ni.Control, l.freq(fc))
		}
		if ch == "CAC" {
			l.cacStructure(s, st)
		}
	}
	for _, p := range b.Perch {
		ni.Neighbors = append(ni.Neighbors, float64(p))
	}
	for _, f := range m.Fields {
		ni.Rows = append(ni.Rows, fam.Row{Group: "RT", Name: f.Name, Value: f.Value})
	}
	if b.Network != "" {
		h.SetOrg(b.Network)
	}
	h.EmitNet(ni)
}

func (l *link) cacStructure(s *decoder.Slot, st *l3.ChannelStructure) {
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
	if st.Nc == 1 {
		l.h.SetCACStructure(l1.CACStructure{AB: st.AB, AS1: st.AS1, AP: st.AP, AS2: st.AS2, Np: st.Np})
	}
}

func (l *link) setCallInfo(n int, call *l3.Call, src string) {
	st := &l.st[n]
	if !l.h.CallOn(n) {
		st.preCall, st.preSrc, st.at = call, src, l.h.Now()
	}
	for _, b := range call.Bearers {
		if b.NonVoice && !st.data {
			st.data = true
			l.h.Note(n, "data 非音声の通信（伝達能力 "+b.Text+"）: TCH はデータとして tch.bin に残し、音声の符号器は通さない")
			l.h.Logf(n, "call", "info", "ST#%d 非音声の通信（データ）: %s", n, b.Text)
		}
		if b.Mode != "" && st.mode != b.Mode {
			st.mode = b.Mode
			l.h.SetMode(n, b.Mode, "")
			if l.h.CallOn(n) {
				l.h.CallEvent(n, "mode", b.Mode+"（伝達能力の通信形態）")
			}
		}
	}
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

func (l *link) HintCall(slot int, h fam.Hint) {
	call, _ := h.Call.(*l3.Call)
	if call == nil {
		return
	}
	l.st[slot].pendCall, l.st[slot].pendSrc = call, h.Src
	if l.h.CallOn(slot) {
		l.setCallInfo(slot, call, h.Src)
	}
}

func (l *link) CallOpen(n int) {
	st := &l.st[n]
	old := *st
	*st = slotState{pendCall: old.pendCall, pendSrc: old.pendSrc}
	if old.at <= 0 || l.h.Now()-old.at > preKeep {
		return
	}
	st.tr, st.at = old.tr, old.at
	if old.preCall != nil {
		l.setCallInfo(n, old.preCall, old.preSrc)
	}
	if k, _ := st.tr.Kind(); k != "" {
		l.h.SetMode(n, k, "")
	}
}

func (l *link) CallStart(n int) {
	st := &l.st[n]
	if st.pendCall != nil {
		l.setCallInfo(n, st.pendCall, st.pendSrc)
		st.pendCall = nil
	}
}

func (l *link) Observe(int, *decoder.Slot, bool) {}

func (l *link) CallEnding(n int) {
	if k, why := l.st[n].tr.Kind(); k != "" && l.h.CallOn(n) {
		l.h.CallEvent(n, "mode", k+"（制御シーケンス §4.3.8: "+why+"）")
	}
}

func (l *link) CallClosed(n int) { l.st[n] = slotState{} }
func (l *link) Drop(n int) {
	st := &l.st[n]
	*st = slotState{pendCall: st.pendCall, pendSrc: st.pendSrc}
}
func (l *link) Data(n int) bool { return l.st[n].data || l.nvData(n) }
func (l *link) Mode(n int) (string, string) {
	if k, _ := l.st[n].tr.Kind(); k != "" {
		return k, ""
	}
	return l.st[n].mode, ""
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

func msgNode(m l3.Message) fam.Node {
	n := fam.Node{Label: m.Entity, Value: fmt.Sprintf("0x%02X %s", m.Type, m.Name), Note: m.Err}
	for _, f := range m.Fields {
		n.Children = append(n.Children, fam.Node{Label: f.Name, Value: f.Value})
	}
	return n
}

func summary(m l3.Message) string {
	switch {
	case m.Assign != nil:
		return m.Assign.Text
	case m.Call != nil:
		return m.Call.Text
	case m.Paging != nil:
		return m.Paging.Text
	case m.MM != nil:
		return m.MM.Text
	case m.Broadcast != nil && m.Broadcast.Network != "":
		return "網番号 " + m.Broadcast.Network
	}
	if len(m.Fields) > 0 {
		f := m.Fields[0]
		return f.Name + " " + f.Value
	}
	return ""
}

func (l *link) MSI(l2.Address) (string, string, bool) { return "", "", false }
func (l *link) L2Fields() l2.MSIFields                { return nil }
func (l *link) FoldKey() int                          { return 0 }
