package control

import (
	"strconv"
	"strings"

	"github.com/symysak/arib/internal/std-t86/citycodes"
)

type TargetKind string

const (
	TargetAll       TargetKind = "all"
	TargetSelective TargetKind = "selective"
	TargetUnknown   TargetKind = "unknown"
)

type Target struct {
	Kind         TargetKind
	Label        string
	IDs          []int
	EffectiveIDs []int
	ValidBits    int
	CallNo       int
	Note         string
}

func BroadcastTarget(msg Message, validBits int) *Target {
	if msg.Broadcast == nil {
		return nil
	}
	f := msg.Broadcast
	var ids []int
	if f.N1 {
		ids = append(ids, f.ID1)
	}
	if f.N2 {
		ids = append(ids, f.ID2)
	}
	mask := 0xFFFF
	if validBits > 0 {
		mask = (1 << uint(validBits)) - 1
	}
	eff := make([]int, len(ids))
	allZero, allOnes := true, len(ids) > 0
	for i, v := range ids {
		eff[i] = v & mask
		if eff[i] != 0 {
			allZero = false
		}
		if eff[i] != mask {
			allOnes = false
		}
	}

	t := &Target{IDs: ids, EffectiveIDs: eff, ValidBits: validBits, CallNo: f.CallNo}
	switch {
	case len(ids) == 0:
		t.Kind, t.Label = TargetUnknown, "不明（有効な子局識別番号なし）"
	case allZero:
		t.Kind, t.Label = TargetAll, "一斉（全子局）"
	default:
		t.Kind = TargetSelective
		parts := make([]string, len(eff))
		for i, v := range eff {
			parts[i] = strconv.Itoa(v)
		}
		t.Label = "子局/群 " + strings.Join(parts, "・")
		if allOnes {
			t.Note = "全1: §4.3.7 に「一括番号（システム内の全１）」の記載があり" +
				"一斉の可能性があるが、原文が曖昧なため断定しない"
		}
	}
	return t
}

type PositionedMessage struct {
	Pos int
	Msg Message
}

type Window struct {
	Start int
	End   int
}

func BroadcastWindows(control []PositionedMessage, endPos int) []Window {
	var windows []Window
	openPos := -1
	for _, pm := range control {
		t := pm.Msg.Type
		switch {
		case (t == MsgBroadcastStart || t == MsgDelayedStart) && openPos < 0:
			openPos = pm.Pos
		case t == MsgForcedRelease && openPos >= 0:
			windows = append(windows, Window{Start: openPos, End: pm.Pos})
			openPos = -1
		}
	}
	if openPos >= 0 {
		windows = append(windows, Window{Start: openPos, End: endPos})
	}
	return windows
}

type counter struct {
	order []string
	count map[string]int
}

func newCounter() *counter { return &counter{count: map[string]int{}} }

func (c *counter) add(k string) {
	if _, ok := c.count[k]; !ok {
		c.order = append(c.order, k)
	}
	c.count[k]++
}

func (c *counter) mostCommon() []NameCount {
	out := make([]NameCount, 0, len(c.order))
	for _, k := range c.order {
		out = append(out, NameCount{Name: k, Count: c.count[k]})
	}
	stableSortDesc(out)
	return out
}

func (c *counter) top() (string, bool) {
	m := c.mostCommon()
	if len(m) == 0 {
		return "", false
	}
	return m[0].Name, true
}

type NameCount struct {
	Name  string
	Count int
}

func stableSortDesc(v []NameCount) {
	for i := 1; i < len(v); i++ {
		for j := i; j > 0 && v[j].Count > v[j-1].Count; j-- {
			v[j], v[j-1] = v[j-1], v[j]
		}
	}
}

type RecentMessage struct {
	TypeName string
	Channel  string
	RawHex   string
}

type Summary struct {
	Seed            int
	MunicipalCode   int
	Municipality    string
	Candidates      []citycodes.Candidate
	TypeCounts      []NameCount
	Manufacturers   []NameCount
	SlotUsage       []int
	BroadcastActive bool
	ParentMode      string
	SuperframeLen   int
	Total           int
	Valid           int
	CRCOK           int
	Recent          []RecentMessage
}

func Summarize(msgs []Message, seed, municipalCode int) Summary {
	var valid, pool, bcch []Message
	for _, m := range msgs {
		if m.CRCOK && KnownType(m.Type) {
			valid = append(valid, m)
		}
	}
	if len(valid) > 0 {
		pool = valid
	} else {
		for _, m := range msgs {
			if KnownType(m.Type) {
				pool = append(pool, m)
			}
		}
	}
	for _, m := range pool {
		if m.BCCH != nil {
			bcch = append(bcch, m)
		}
	}

	mfrCounts := newCounter()
	slotCounts := newCounter()
	modeCounts := newCounter()
	sfCounts := newCounter()
	broadcasting := 0
	for _, m := range bcch {
		if _, ok := Manufacturers[m.BCCH.ManufacturerCode]; ok {
			mfrCounts.add(m.BCCH.ManufacturerName)
		}
		if m.Header != nil {
			if len(m.Header.UsedSlots) > 0 {
				slotCounts.add(joinInts(m.Header.UsedSlots))
			}
			if m.Header.Broadcasting {
				broadcasting++
			}
		}
		modeCounts.add(m.BCCH.ParentMode)
		sfCounts.add(strconv.Itoa(m.BCCH.SuperframeLenS))
	}

	active := float64(broadcasting) > float64(len(bcch))/2
	if !active {
		for _, m := range pool {
			if m.Type == MsgBroadcastStart {
				active = true
				break
			}
		}
	}

	typeCounts := newCounter()
	for _, m := range pool {
		typeCounts.add(m.TypeName)
	}

	s := Summary{
		Seed:            seed,
		MunicipalCode:   municipalCode,
		Candidates:      CandidatesForSeed(seed),
		TypeCounts:      typeCounts.mostCommon(),
		Manufacturers:   mfrCounts.mostCommon(),
		BroadcastActive: active,
		Total:           len(msgs),
		Valid:           len(pool),
	}
	if municipalCode != 0 {
		if n, ok := citycodes.Name(municipalCode); ok {
			s.Municipality = n
		}
	}
	if v, ok := slotCounts.top(); ok {
		s.SlotUsage = splitInts(v)
	} else {
		s.SlotUsage = []int{}
	}
	s.ParentMode, _ = modeCounts.top()
	if v, ok := sfCounts.top(); ok {
		s.SuperframeLen, _ = strconv.Atoi(v)
	}
	for _, m := range msgs {
		if m.CRCOK {
			s.CRCOK++
		}
	}
	start := len(pool) - 12
	if start < 0 {
		start = 0
	}
	for _, m := range pool[start:] {
		s.Recent = append(s.Recent, RecentMessage{m.TypeName, m.Channel, m.RawHex})
	}
	return s
}

func joinInts(v []int) string {
	parts := make([]string, len(v))
	for i, x := range v {
		parts[i] = strconv.Itoa(x)
	}
	return strings.Join(parts, ",")
}

func splitInts(s string) []int {
	if s == "" {
		return []int{}
	}
	parts := strings.Split(s, ",")
	out := make([]int, 0, len(parts))
	for _, p := range parts {
		v, err := strconv.Atoi(p)
		if err != nil {
			continue
		}
		out = append(out, v)
	}
	return out
}
