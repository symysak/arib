package l3

import (
	"fmt"
	"strings"
)

type Field struct {
	Name  string
	Value string
}

type Message struct {
	Entity string
	Type   uint8
	Name   string
	Fields []Field
	Raw    []byte
	Err    string
	Notes []string

	Assign *Assignment
	Direct *DirectChannel
	Call   *Call
	Paging *Paging
	Broadcast *Broadcast
	Dedicated []Dedicated
	Short *ShortMessage

	Vendor []VendorItem
	Clock *Clock
}

type VendorItem struct {
	What  string
	Raw   []byte
	Known bool
}

type Clock struct {
	What    string
	Date    string
	Sec     int
	Text    string
	ByMaker int
	PerCall bool
	Step int
}

type Ctx struct {
	Dir int
	Maker int
}

type Broadcast struct {
	System    *SystemID
	Control   []float64
	Neighbors []float64
	Restrict *Restriction
	Support int
	Turnup *Turnup
	Structure *ChannelStructure
}

type ChannelStructure struct {
	Np, Nc, AB, AP, AS1, AS2 int
}

type Restriction struct {
	GroupHalfDuplex bool
	GroupHandoffLimit bool
	SysConfig int
	VoiceLimitMin, DataLimitMin int
}

type Turnup struct {
	Slot int
	MHz  float64
}

type Dedicated struct {
	MHz  float64
	Slot int
	ID   int
}

type ShortMessage struct {
	Response bool
	From, To string
	Number   int
	Kind string
	Emergency bool
	Text      string
}

type SystemID struct {
	Maker, Zone, Base int
	Code              uint64
	CodeLen           int
	Industry, Seq     int
	Area              int
	AreaName          string
	Org               string
}

type DirectChannel struct {
	MHz      []float64
	Scramble int
	OK *bool
}

type Assignment struct {
	Band int
	FC   int
	MHz  float64
	Slot int
	Scramble int
	UpScramble int
	UpMHz float64
	Group       bool
	GroupFollow int
	Text        string
}

type Call struct {
	ServiceID int
	Ref       int
	MsgType uint8
	Setup, Release bool
	Mode *CommMode
	CalledPlan, CallingPlan int
	CalledType, CallingType int
	SubStar bool
	Called  string
	Calling string
	Display string
	MSOrg   string
	Cause   string
	Bearers []Bearer
	Text    string
}

type Bearer struct {
	Raw  []byte
	Text string
	Codec string
	Mode *CommMode
	NonVoice bool
}

type CommMode struct {
	Emergency bool
	Form int
}

const (
	FormSimplexIndividual = 0
	FormDuplexIndividual  = 1
	FormGroup             = 2
	FormBroadcast         = 3
)

func (m CommMode) String() string {
	f := "予約"
	if m.Form >= 0 && m.Form < len(ccModes) {
		f = ccModes[m.Form]
	}
	if m.Emergency {
		return "緊急 " + f
	}
	return "通常 " + f
}

type Paging struct {
	ID   string
	Text string
}

type Signal struct {
	HasRT, HasMM, HasCC bool
	Messages            []Message
	Err                 string
}

func (s Signal) Summary() string {
	var parts []string
	for _, m := range s.Messages {
		t := m.Entity + ":" + m.Name
		if m.Err != "" {
			t += "(!)"
		}
		parts = append(parts, t)
	}
	if s.Err != "" {
		parts = append(parts, "("+s.Err+")")
	}
	return strings.Join(parts, " ")
}

func Parse(info []byte) Signal { return ParseDir(info, 0) }

func ParseCtx(info []byte, c Ctx) Signal {
	s := ParseDir(info, c.Dir)
	if v, ok := vendors[c.Maker]; ok {
		for i := range s.Messages {
			v.annotate(&s.Messages[i], c)
		}
	}
	return s
}

func ParseDir(info []byte, dir int) Signal {
	var s Signal
	if len(info) == 0 {
		s.Err = "情報フィールドが空"
		return s
	}
	h := info[0]
	s.HasRT, s.HasMM, s.HasCC = h&4 != 0, h&2 != 0, h&1 != 0
	n := 0
	for _, b := range []bool{s.HasRT, s.HasMM, s.HasCC} {
		if b {
			n++
		}
	}
	if n == 0 {
		s.Err = "相乗り表示部に信号無し"
		return s
	}
	i := 1
	lenRTorMM, lenMM := -1, -1
	if n >= 2 {
		if i >= len(info) {
			s.Err = "相乗り表示部が短い"
			return s
		}
		lenRTorMM = int(info[i])
		i++
	}
	if n == 3 {
		if i >= len(info) {
			s.Err = "相乗り表示部が短い"
			return s
		}
		lenMM = int(info[i])
		i++
	}
	body := info[i:]
	if lenRTorMM > len(body) || lenMM > len(body) || lenRTorMM+max(lenMM, 0) > len(body) {
		s.Err = fmt.Sprintf("相乗り表示部の長さが不正 (%d/%d > %d)", lenRTorMM, lenMM, len(body))
		return s
	}
	cut := func(l int) []byte {
		if l < 0 || l > len(body) {
			l = len(body)
		}
		part := body[:l]
		body = body[l:]
		return part
	}
	if s.HasRT {
		l := -1
		if n >= 2 {
			l = lenRTorMM
		}
		s.Messages = append(s.Messages, finish(parseRT(cut(l))))
	}
	if s.HasMM {
		l := -1
		switch {
		case n == 3:
			l = lenMM
		case n == 2 && !s.HasRT:
			l = lenRTorMM
		}
		s.Messages = append(s.Messages, finish(parseMM(cut(l))))
	}
	if s.HasCC {
		s.Messages = append(s.Messages, finish(parseCCDir(cut(-1), dir)))
	}
	return s
}

func finish(m Message) Message {
	if m.Name == "" {
		m.Name = fmt.Sprintf("未定義(0x%02X)", m.Type)
		m.vendorRaw(fmt.Sprintf("%s 種別 0x%02X", m.Entity, m.Type), m.Raw)
	}
	return m
}

func (m *Message) vendorRaw(what string, b []byte) {
	m.Vendor = append(m.Vendor, VendorItem{What: what, Raw: append([]byte(nil), b...)})
}

func (m Message) Unknown() bool {
	for _, v := range m.Vendor {
		if !v.Known {
			return true
		}
	}
	return false
}


type reader struct {
	b   []byte
	i   int
	err string
}

func (r *reader) left() int { return len(r.b) - r.i }

func (r *reader) take(n int, what string) []byte {
	if r.err != "" {
		return nil
	}
	if n < 0 || r.i+n > len(r.b) {
		r.err = what + " が足りない"
		return nil
	}
	v := r.b[r.i : r.i+n]
	r.i += n
	return v
}

func (r *reader) byte1(what string) (byte, bool) {
	v := r.take(1, what)
	if v == nil {
		return 0, false
	}
	return v[0], true
}

func (r *reader) rest() []byte {
	if r.err != "" {
		return nil
	}
	v := r.b[r.i:]
	r.i = len(r.b)
	return v
}

func bcdDigits(b []byte, odd, lowFirst bool) string {
	var sb strings.Builder
	for i, o := range b {
		nibs := []byte{o >> 4, o & 0xF}
		if lowFirst {
			nibs = []byte{o & 0xF, o >> 4}
		}
		for k, nib := range nibs {
			if i == len(b)-1 && odd && k == 1 {
				break
			}
			if nib == 0 && lowFirst {
				continue
			}
			sb.WriteByte(digitChar(nib))
		}
	}
	return sb.String()
}

func digitChar(n byte) byte {
	switch {
	case n <= 9:
		return '0' + n
	case n == 0xA:
		return '0'
	case n == 0xB:
		return '*'
	case n == 0xC:
		return '#'
	}
	return '?'
}

func IA5Kana(b []byte) string {
	var sb strings.Builder
	for _, c := range b {
		switch {
		case c >= 0x20 && c < 0x7F:
			sb.WriteByte(c)
		case c >= 0xA1 && c <= 0xDF:
			sb.WriteRune(rune(0xFF61 + int(c) - 0xA1))
		default:
			fmt.Fprintf(&sb, "<%02X>", c)
		}
	}
	return sb.String()
}

func hexOf(b []byte) string {
	var sb strings.Builder
	for _, v := range b {
		fmt.Fprintf(&sb, "%02X", v)
	}
	return sb.String()
}

func pick(table []string, v int, fallback string) string {
	if v >= 0 && v < len(table) && table[v] != "" {
		return table[v]
	}
	return fmt.Sprintf("%s(%d)", fallback, v)
}

func pickMap(m map[int]string, v int, fallback string) string {
	if s, ok := m[v]; ok {
		return s
	}
	return fmt.Sprintf("%s(%d)", fallback, v)
}

func bit(b byte, n int) int { return int(b>>(n-1)) & 1 }
