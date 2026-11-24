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
	Notes  []string

	Assign    *Assignment
	Paging    *Paging
	Broadcast *Broadcast
	Call      *Call
	MM        *Location
}

type Ctx struct {
	Dir int
	FreqMHz func(fc int) float64
}

type Assignment struct {
	FC   int
	MHz  float64
	Slot int
	Slot2 int
	Scramble int
	Handover bool
	Text     string
}

type Paging struct {
	ID   string
	Text string
}

type Broadcast struct {
	Network   string
	Structure *ChannelStructure
	Perch     []int
	Restrict  string
}

type ChannelStructure struct {
	Np, Nc, AB, AP, AS1, AS2 int
	FC                       []int
	Slots []int
}

type Call struct {
	ServiceID int
	Ref       int
	Flag      int
	MsgType   uint8
	Setup     bool
	Release   bool
	Called    string
	Calling   string
	Display   string
	MSNumber  string
	Cause     string
	Bearers   []Bearer
	Text      string
}

type Bearer struct {
	Raw  []byte
	Text string
	Codec string
	Mode string
	NonVoice bool
}

type Location struct {
	Number string
	Cause  string
	Text   string
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

func Parse(info []byte) Signal { return ParseCtx(info, Ctx{}) }

func ParseCtx(info []byte, c Ctx) Signal {
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
		s.Messages = append(s.Messages, finish(parseRT(cut(l), c)))
	}
	if s.HasMM {
		l := -1
		switch {
		case n == 3:
			l = lenMM
		case n == 2 && !s.HasRT:
			l = lenRTorMM
		}
		s.Messages = append(s.Messages, finish(parseMM(cut(l), c)))
	}
	if s.HasCC {
		s.Messages = append(s.Messages, finish(parseCC(cut(-1), c)))
	}
	return s
}

func finish(m Message) Message {
	if m.Name == "" {
		m.Name = fmt.Sprintf("未定義(0x%02X)", m.Type)
	}
	for _, n := range m.Notes {
		m.Fields = append(m.Fields, Field{Name: "規格の検査", Value: n})
	}
	return m
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
