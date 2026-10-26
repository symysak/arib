package callseq

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"sync"

	"github.com/symysak/arib/internal/std-t61/l3"
)

type Msg struct {
	TSec    float64
	Station int
	MHz     float64
	Channel string
	Uplink  bool
	Addr    string
	L3      []l3.Message
	Notes []string
}

type Obs struct {
	TSec    float64
	Station int
	MHz     float64
	Service bool
	StdRICH bool
	M, D    uint8
	SB, SBIndividual, SBInitial bool
	SBID                        uint32
}

type Event struct {
	TSec    float64 `json:"t_sec"`
	Station int     `json:"idx"`
	MHz     float64 `json:"mhz"`
	Kind    string  `json:"kind"`
	Text    string  `json:"text"`
}

type Call struct {
	ID         int     `json:"id"`
	Station    int     `json:"idx"`
	MHz        float64 `json:"mhz"`
	TrafficMHz float64 `json:"traffic_mhz,omitempty"`
	Traffic    int     `json:"traffic_idx"`
	Addr       string  `json:"addr,omitempty"`
	Ref        int     `json:"ref"`
	Start      float64 `json:"start"`
	Last       float64 `json:"last"`
	Open       bool    `json:"open"`
	Type       string  `json:"type,omitempty"`
	Section    string  `json:"section,omitempty"`
	Downlink   string  `json:"downlink,omitempty"`
	Basis      string  `json:"basis,omitempty"`
	Proc      string  `json:"proc"`
	Anomalies int     `json:"anomalies,omitempty"`
	Phase     string  `json:"phase"`
	Outcome   string  `json:"outcome,omitempty"`
	Detail    string  `json:"detail,omitempty"`
	Events    []Event `json:"events"`

	ev       l3.CallEvidence
	switched bool
	cause    int
	failed   string
	idleRun  int
	ctlKey   string
	trafKey  string
	mm       bool
	mmWho    string
	mmRetry  int
}

type Update struct {
	Call  Call
	Event *Event
}

const (
	linkSec     = 10.0
	ctlIdleSec  = 60.0
	trafIdleSec = 5.0
	idleFrames  = 75
	keepDone    = 200
	freqTolMHz  = 0.003
)

type pending struct {
	call       *Call
	band, fc   int
	t          float64
	fromSwitch bool
}

type Tracker struct {
	mu     sync.Mutex
	nextID int
	open   map[string]*Call
	done   []*Call
	pend   []pending
	fo     map[int]float64
	recent map[string]*Call
}

func New() *Tracker {
	return &Tracker{open: map[string]*Call{}, fo: map[int]float64{}, recent: map[string]*Call{}}
}

const recentSec = 10.0

func onlyRelease(ms []l3.Message) bool {
	for _, x := range ms {
		switch {
		case x.Entity == "RT" && (x.Type == 0x81 || x.Type == 0x82 || x.Type == 0x21):
		case x.Entity == "CC" && (x.Type == 0x45 || x.Type == 0x4D || x.Type == 0x5A):
		case x.Entity == "RT" && !callRT[x.Type]:
		default:
			return false
		}
	}
	return true
}

func ctlKey(mhz float64, addr string) string { return fmt.Sprintf("%.4f/%s", mhz, addr) }
func trafKey(mhz float64) string             { return fmt.Sprintf("%.4f/通信", mhz) }

func (t *Tracker) newCall(tsec float64, st int, mhz float64, addr string) *Call {
	t.nextID++
	return &Call{ID: t.nextID, Proc: "呼", Station: st, MHz: mhz, Traffic: -1, Addr: addr, Ref: -1, Start: tsec, Last: tsec, Open: true,
		Phase: "確立", ev: l3.NewCallEvidence(), cause: -1}
}

var callRT = map[uint8]bool{0x01: true, 0x02: true, 0x03: true, 0x04: true, 0x21: true, 0x43: true, 0x61: true,
	0x71: true, 0x72: true, 0x73: true, 0x81: true, 0x82: true, 0xC1: true, 0xC2: true}

func (t *Tracker) LearnFo(band, fc int, mhz float64) {
	t.mu.Lock()
	t.fo[band] = mhz - 0.00625*float64(fc)
	t.mu.Unlock()
}

func (t *Tracker) Fo(band int) (float64, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	v, ok := t.fo[band]
	return v, ok
}

func (t *Tracker) Message(m Msg) []Update {
	if !m.Uplink {
		for _, x := range m.L3 {
			if x.RT != nil && x.RT.OwnFc >= 0 && x.Type == 0xA1 {
				t.LearnFo(x.RT.OwnBand, x.RT.OwnFc, m.MHz)
			}
		}
	}
	who := "基地局"
	if m.Uplink {
		who = "移動局"
	}
	relevant := false
	var rtcc []l3.Message
	var mms []l3.Message
	for _, x := range m.L3 {
		switch {
		case (x.Entity == "RT" && callRT[x.Type]) || x.Entity == "CC":
			relevant = true
			rtcc = append(rtcc, x)
		case x.Entity == "RT":
			rtcc = append(rtcc, x)
		case x.Entity == "MM":
			mms = append(mms, x)
		}
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	var mmUps []Update
	for _, x := range mms {
		mmUps = append(mmUps, t.mm(m, x, who)...)
	}
	if !relevant {
		if len(mmUps) > 0 && len(m.Notes) > 0 {
			if c := t.open[ctlKey(m.MHz, m.Addr)+"/MM"]; c != nil {
				mmUps = append(mmUps, t.anomalies(c, m)...)
			}
		}
		return mmUps
	}
	m.L3 = rtcc
	traffic := m.Channel == "SACCH" || m.Channel == "FACCH"
	var c *Call
	var key string
	if traffic {
		key = trafKey(m.MHz)
	} else {
		key = ctlKey(m.MHz, m.Addr)
	}
	if t.open[key] == nil {
		if r := t.recent[key]; r != nil && m.TSec-r.Last < recentSec && onlyRelease(m.L3) {
			r.Last = m.TSec
			return []Update{{Call: snapshot(r)}}
		}
	}
	if traffic {
		key = trafKey(m.MHz)
		c = t.open[key]
		if c == nil {
			c = t.linkOrNew(m.TSec, m.Station, m.MHz, "通信チャネルの L3")
		}
	} else {
		key = ctlKey(m.MHz, m.Addr)
		c = t.open[key]
		if c == nil {
			c = t.newCall(m.TSec, m.Station, m.MHz, m.Addr)
			c.ctlKey = key
			t.open[key] = c
		}
	}
	ups := append(mmUps, t.anomalies(c, m)...)
	before := c.Type
	for _, x := range m.L3 {
		if text, kind := t.apply(c, x, m, who); text != "" {
			ups = append(ups, t.event(c, m.TSec, m.Station, m.MHz, kind, text))
		}
	}
	c.Last = math.Max(c.Last, m.TSec)
	ups = append(ups, t.retype(c, m.TSec, m.Station, m.MHz, before)...)
	if c.Phase == "解放" && c.Open {
		ups = append(ups, t.close(c, m.TSec, "", ""))
	}
	if len(ups) == 0 {
		ups = append(ups, Update{Call: snapshot(c)})
	}
	return ups
}

func (t *Tracker) apply(c *Call, x l3.Message, m Msg, who string) (string, string) {
	if x.Entity == "CC" && x.CC != nil {
		if c.Ref < 0 && x.CC.Ref != 7 {
			c.Ref = x.CC.Ref
		}
		if x.CC.Mode != nil && c.ev.Mode == nil {
			c.ev.Mode = x.CC.Mode
		}
		switch x.Type {
		case 0x05, 0x15, 0x16, 0x17, 0x18, 0x19, 0x12:
			if c.ev.CCType < 0 || x.Type != 0x12 {
				c.ev.CCType = int(x.Type)
			}
		}
		switch x.Type {
		case 0x05, 0x15, 0x17, 0x19:
			return fmt.Sprintf("%s: CC %s", who, x.Name), "setup"
		case 0x16, 0x18:
			c.switched, c.Phase = true, "切替"
			return fmt.Sprintf("%s: CC %s（チャネル切替の再呼）", who, x.Name), "switch"
		case 0x11:
			return fmt.Sprintf("%s: CC %s（切替先で再呼を受け付けた）", who, x.Name), "switch"
		case 0x14:
			return fmt.Sprintf("%s: CC %s（切り戻し）", who, x.Name), "switchback"
		case 0x07:
			c.Phase = "通信"
			return fmt.Sprintf("%s: CC %s（相手が応答。通信へ）", who, x.Name), "setup"
		case 0x45, 0x4D, 0x5A:
			if x.CC.Cause >= 0 && c.failed == "" && x.Type == 0x5A && c.Phase == "確立" {
				c.failed = "解放完了（確立の途中）"
			}
			if x.Type == 0x5A || x.Type == 0x4D {
				c.Phase = "解放"
			}
			return fmt.Sprintf("%s: CC %s", who, x.Name), "release"
		}
		return fmt.Sprintf("%s: CC %s", who, x.Name), "cc"
	}
	if x.Entity != "RT" || x.RT == nil {
		return "", ""
	}
	r := x.RT
	fc := func() string {
		if r.Fc < 0 {
			return ""
		}
		s := fmt.Sprintf(" 帯=%d Fc=%d", r.Band, r.Fc)
		if fo, ok := t.fo[r.Band]; ok {
			s += fmt.Sprintf("（%.4f MHz）", fo+0.00625*float64(r.Fc))
		}
		return s
	}
	switch x.Type {
	case 0x01:
		return fmt.Sprintf("%s: 発信無線状態報告（発呼）", who), "establish"
	case 0x02:
		return fmt.Sprintf("%s: ページング（着信呼出）", who), "establish"
	case 0x03:
		return fmt.Sprintf("%s: 着信無線状態報告（着信応答）", who), "establish"
	case 0x04:
		c.ev.GroupType = r.GroupType
		t.addPending(c, r, m.TSec, false)
		if r.GroupType == 2 || r.GroupType == 3 {
			return fmt.Sprintf("%s: グループ通信呼出（後追い参入）%s", who, fc()), "join"
		}
		return fmt.Sprintf("%s: グループ通信呼出%s", who, fc()), "establish"
	case 0x43:
		t.addPending(c, r, m.TSec, false)
		return fmt.Sprintf("%s: 無線チャネル指定%s", who, fc()), "assign"
	case 0x61:
		c.switched, c.Phase = true, "切替"
		t.addPending(c, r, m.TSec, true)
		return fmt.Sprintf("%s: 切替先無線チャネル指定%s", who, fc()), "switch"
	case 0x72, 0x73:
		c.switched, c.Phase = true, "切替"
		return fmt.Sprintf("%s: %s（チャネル切替・再発呼型）", who, x.Name), "switch"
	case 0x71:
		c.cause = r.Cause
		return fmt.Sprintf("%s: 切り戻し要求（%s）", who, l3.RTCauseName(r.Cause)), "switchback"
	case 0xC1:
		return fmt.Sprintf("%s: 送信権解放", who), "ptt"
	case 0xC2:
		return fmt.Sprintf("%s: 送信停止要求（送信権の衝突）", who), "ptt"
	case 0x21:
		c.cause, c.failed, c.Phase = r.Cause, "移動局解放（準正常。確立できず）", "解放"
		return fmt.Sprintf("%s: 移動局解放（%s）", who, l3.RTCauseName(r.Cause)), "release"
	case 0x81:
		c.cause = r.Cause
		c.Phase = "解放"
		return fmt.Sprintf("%s: 無線チャネル切断（%s）", who, l3.RTCauseName(r.Cause)), "release"
	case 0x82:
		c.Phase = "解放"
		return fmt.Sprintf("%s: 無線チャネル切断確認", who), "release"
	}
	return "", ""
}

func (t *Tracker) addPending(c *Call, r *l3.RTFacts, tsec float64, sw bool) {
	if r.Fc < 0 {
		return
	}
	out := t.pend[:0]
	for _, p := range t.pend {
		if p.call != c && tsec-p.t <= linkSec {
			out = append(out, p)
		}
	}
	t.pend = append(out, pending{call: c, band: r.Band, fc: r.Fc, t: tsec, fromSwitch: sw})
}

func (t *Tracker) linkOrNew(tsec float64, st int, mhz float64, why string) *Call {
	key := trafKey(mhz)
	var cands []int
	for i, p := range t.pend {
		if tsec-p.t > linkSec || tsec < p.t-0.5 || p.call.MHz == mhz {
			continue
		}
		if fo, ok := t.fo[p.band]; ok {
			if math.Abs(fo+0.00625*float64(p.fc)-mhz) < freqTolMHz {
				cands = []int{i}
				break
			}
			continue
		}
		cands = append(cands, i)
	}
	if len(cands) == 1 {
		p := t.pend[cands[0]]
		t.pend = append(t.pend[:cands[0]], t.pend[cands[0]+1:]...)
		c := p.call
		if c.trafKey != "" && c.trafKey != key {
			delete(t.open, c.trafKey)
		}
		c.TrafficMHz, c.Traffic, c.trafKey = mhz, st, key
		t.open[key] = c
		if !c.Open {
			c.Open, c.Outcome, c.Detail = true, "", ""
		}
		basis := "推定: 指定から " + fmt.Sprintf("%.1f", tsec-p.t) + " 秒で通話が始まった局"
		if _, ok := t.fo[p.band]; ok {
			basis = "周波数コードで突き合わせ"
		} else {
			t.fo[p.band] = mhz - 0.00625*float64(p.fc)
		}
		c.Events = append(c.Events, Event{TSec: tsec, Station: st, MHz: mhz, Kind: "link",
			Text: fmt.Sprintf("通信チャネル %.4f MHz（Fc=%d）へ移った（%s。%s）", mhz, p.fc, basis, why)})
		if c.Phase == "確立" {
			c.Phase = "通信"
		}
		return c
	}
	c := t.newCall(tsec, st, mhz, "")
	c.Traffic, c.TrafficMHz, c.trafKey, c.Phase = st, mhz, key, "通信"
	c.Events = append(c.Events, Event{TSec: tsec, Station: st, MHz: mhz, Kind: "note",
		Text: "通信チャネルで通話を見つけた（" + why + "。制御チャネルでの確立は見ていない）"})
	t.open[key] = c
	return c
}

func (t *Tracker) Frame(o Obs) []Update {
	if !o.Service {
		return nil
	}
	active := o.SB || (o.StdRICH && (o.M == 1 || o.M == 2 || o.M == 3 || o.M == 4))
	t.mu.Lock()
	defer t.mu.Unlock()
	key := trafKey(o.MHz)
	c := t.open[key]
	if c == nil {
		if !active {
			return nil
		}
		why := "音声"
		if o.SB {
			why = "同期バースト"
		}
		c = t.linkOrNew(o.TSec, o.Station, o.MHz, why)
		ups := []Update{{Call: snapshot(c), Event: lastEvent(c)}}
		return append(ups, t.observe(c, o)...)
	}
	return t.observe(c, o)
}

func (t *Tracker) observe(c *Call, o Obs) []Update {
	var ups []Update
	before := c.Type
	c.Last = math.Max(c.Last, o.TSec)
	if o.SB {
		v := o.SBIndividual
		if c.ev.SBIndividual == nil || *c.ev.SBIndividual != v {
			c.ev.SBIndividual = &v
		}
		if o.SBInitial {
			if n := len(c.Events); n == 0 || c.Events[n-1].Kind != "ptt" || o.TSec-c.Events[n-1].TSec > 2 {
				kind := "グループ番号"
				if v {
					kind = "移動局個別番号"
				}
				ups = append(ups, t.event(c, o.TSec, o.Station, o.MHz, "ptt", fmt.Sprintf("同期バースト（初期 SB1/SB2 = 送信の起動）呼出符号 #%06X（%s）", o.SBID, kind)))
			}
		}
	}
	if o.StdRICH {
		if o.D >= 1 && o.D <= 3 && c.ev.Route != int(o.D) {
			c.ev.Route = int(o.D)
		}
		if o.M == 5 {
			c.idleRun++
		} else {
			c.idleRun = 0
		}
	}
	ups = append(ups, t.retype(c, o.TSec, o.Station, o.MHz, before)...)
	if c.idleRun >= idleFrames && c.Open {
		ups = append(ups, t.close(c, o.TSec, "", "通信チャネルが空線へ戻った（切断の信号は見ていない）"))
	}
	return ups
}

func (t *Tracker) Tick(now float64) []Update {
	t.mu.Lock()
	defer t.mu.Unlock()
	var ups []Update
	seen := map[*Call]bool{}
	for _, c := range t.open {
		if seen[c] || !c.Open {
			continue
		}
		seen[c] = true
		lim := ctlIdleSec
		switch {
		case c.mm:
			lim = mmIdleSec
		case c.trafKey != "":
			lim = trafIdleSec
		}
		if now-c.Last > lim {
			ups = append(ups, t.close(c, now, "", "見失った（"+fmt.Sprintf("%.0f", lim)+" 秒何も来ない）"))
		}
	}
	return ups
}

func (t *Tracker) anomalies(c *Call, m Msg) []Update {
	var ups []Update
	for _, n := range m.Notes {
		c.Anomalies++
		ups = append(ups, t.event(c, m.TSec, m.Station, m.MHz, "anomaly", "規格と合わない: "+n))
	}
	return ups
}

func (t *Tracker) event(c *Call, tsec float64, st int, mhz float64, kind, text string) Update {
	e := Event{TSec: tsec, Station: st, MHz: mhz, Kind: kind, Text: text}
	c.Events = append(c.Events, e)
	if len(c.Events) > 200 {
		c.Events = c.Events[len(c.Events)-200:]
	}
	return Update{Call: snapshot(c), Event: &e}
}

func (t *Tracker) retype(c *Call, tsec float64, st int, mhz float64, before string) []Update {
	ct, ok := l3.ClassifyCall(c.ev)
	if !ok || ct.Name == before {
		return nil
	}
	c.Type, c.Section, c.Downlink, c.Basis = ct.Name, ct.Section, ct.Downlink, ct.Basis
	return []Update{t.event(c, tsec, st, mhz, "type", fmt.Sprintf("通信種別: %s（%s。§%s）", ct.Name, ct.Basis, ct.Section))}
}

func (t *Tracker) close(c *Call, tsec float64, outcome, detail string) Update {
	c.Open = false
	if c.Last < tsec {
		c.Last = tsec
	}
	if outcome == "" {
		outcome, detail = t.judge(c, detail)
	}
	if c.Anomalies > 0 {
		detail += fmt.Sprintf("（規格と合わない点 %d 件）", c.Anomalies)
	}
	c.Outcome, c.Detail = outcome, detail
	for k, v := range t.open {
		if v == c {
			delete(t.open, k)
		}
	}
	out := t.pend[:0]
	for _, p := range t.pend {
		if p.call != c {
			out = append(out, p)
		}
	}
	t.pend = out
	for k, v := range t.recent {
		if tsec-v.Last > recentSec {
			delete(t.recent, k)
		}
	}
	if c.ctlKey != "" {
		t.recent[c.ctlKey] = c
	}
	if c.trafKey != "" {
		t.recent[c.trafKey] = c
	}
	t.done = append(t.done, c)
	if len(t.done) > keepDone {
		t.done = t.done[len(t.done)-keepDone:]
	}
	return t.event(c, tsec, c.Station, c.MHz, "end", "結果: "+outcome+"（"+detail+"）")
}

func (t *Tracker) judge(c *Call, why string) (string, string) {
	switch {
	case c.failed != "":
		return "異常", c.failed + causeSuffix(c.cause)
	case c.cause >= 0 && c.cause&0x7F == 0x1D:
		return "強制切断", "緊急グループ通信のため（§4.3.8.15、表 2.1-2）"
	case c.cause >= 0 && !l3.RTCauseNormal(c.cause):
		return "異常", "理由 " + l3.RTCauseName(c.cause)
	case c.cause >= 0:
		o := "正常終了"
		if c.switched {
			o = "途中切替"
		}
		return o, "理由 " + l3.RTCauseName(c.cause)
	case c.Phase == "解放":
		o := "正常終了"
		if c.switched {
			o = "途中切替"
		}
		return o, "解放の信号（理由表示なし）"
	}
	if c.switched {
		return "途中切替", why
	}
	return "不明", why
}

func causeSuffix(v int) string {
	if v < 0 {
		return ""
	}
	return "・理由 " + l3.RTCauseName(v)
}

func lastEvent(c *Call) *Event {
	if len(c.Events) == 0 {
		return nil
	}
	e := c.Events[len(c.Events)-1]
	return &e
}

func snapshot(c *Call) Call {
	s := *c
	s.Events = append([]Event(nil), c.Events...)
	return s
}

func (t *Tracker) Calls() []Call {
	t.mu.Lock()
	defer t.mu.Unlock()
	var out []Call
	seen := map[*Call]bool{}
	for _, c := range t.done {
		out = append(out, snapshot(c))
		seen[c] = true
	}
	for _, c := range t.open {
		if !seen[c] {
			seen[c] = true
			out = append(out, snapshot(c))
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func (c Call) Text() string {
	var b strings.Builder
	fmt.Fprintf(&b, "呼#%d", c.ID)
	if c.Addr != "" {
		b.WriteString(" " + c.Addr)
	}
	if c.Ref >= 0 {
		fmt.Fprintf(&b, " 呼番号%d", c.Ref)
	}
	if c.Type != "" {
		b.WriteString(" " + c.Type)
	}
	if c.Outcome != "" {
		b.WriteString(" → " + c.Outcome)
	}
	return b.String()
}
