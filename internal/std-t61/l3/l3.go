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
	CC *CCFacts
	RT *RTFacts
	Notes map[int]string
}

func (m Message) Note(i int) string { return m.Notes[i] }

func (m *Message) SetNote(i int, text string) {
	if text == "" {
		delete(m.Notes, i)
		return
	}
	if m.Notes == nil {
		m.Notes = map[int]string{}
	}
	m.Notes[i] = text
}

func (m *Message) AddField(name, value, note string) {
	m.Fields = append(m.Fields, Field{name, value})
	m.SetNote(len(m.Fields)-1, note)
}

type Signal struct {
	HasRT, HasMM, HasCC bool
	Messages            []Message
	Err                 string
	Notes []string
}

func (s Signal) Summary() string {
	var parts []string
	for _, m := range s.Messages {
		parts = append(parts, m.Entity+":"+m.Name)
	}
	if s.Err != "" {
		parts = append(parts, "("+s.Err+")")
	}
	return strings.Join(parts, " ")
}

type Dir int

const (
	DirUnknown Dir = iota
	DirDown
	DirUp
)

func Parse(info []byte) Signal { return ParseDir(info, DirUnknown) }

func ParseDir(info []byte, dir Dir) Signal {
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
	var lenRTorMM, lenMM int = -1, -1
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
	if (lenRTorMM > len(body)) || (lenMM > len(body)) {
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
		var part []byte
		if n >= 2 {
			part = cut(lenRTorMM)
		} else {
			part = cut(-1)
		}
		s.Messages = append(s.Messages, parseRT(part))
	}
	if s.HasMM {
		var part []byte
		switch {
		case n == 3:
			part = cut(lenMM)
		case n == 2 && !s.HasRT:
			part = cut(lenRTorMM)
		default:
			part = cut(-1)
		}
		s.Messages = append(s.Messages, parseMM(part))
	}
	if s.HasCC {
		s.Messages = append(s.Messages, parseCC(cut(-1), dir))
	}
	s.Notes = pairingNotes(s)
	return s
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
	if r.i+n > len(r.b) {
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

func bcdDigits(b []byte, odd bool) string {
	var sb strings.Builder
	for i, o := range b {
		for _, nib := range []byte{o & 0xF, o >> 4} {
			if i == len(b)-1 && odd && nib == o>>4 {
				break
			}
			sb.WriteByte(digitChar(nib))
		}
	}
	return sb.String()
}

func digitChar(n byte) byte {
	switch {
	case n == 0xA:
		return '0'
	case n >= 1 && n <= 9:
		return '0' + n
	case n == 0xB:
		return '*'
	case n == 0xC:
		return '#'
	case n == 0:
		return '_'
	}
	return '?'
}

func numberIE(r *reader, name string) (Field, bool) {
	n, ok := r.byte1(name + "の内容長")
	if !ok {
		return Field{}, false
	}
	body := r.take(int(n), name)
	if body == nil || len(body) < 1 {
		r.err = name + " が短い"
		return Field{}, false
	}
	odd := body[0]&0x80 != 0
	return Field{name, bcdDigits(body[1:], odd)}, true
}

func ia5(b []byte) string {
	var sb strings.Builder
	kana := false
	for _, c := range b {
		c &= 0x7F
		switch {
		case c == 0x0E:
			kana = true
		case c == 0x0F:
			kana = false
		case kana && c >= 0x21 && c <= 0x5F:
			sb.WriteRune(halfKana[c-0x21])
		case c >= 0x20 && c < 0x7F:
			sb.WriteByte(c)
		default:
			fmt.Fprintf(&sb, "<%02X>", c)
		}
	}
	return sb.String()
}

var halfKana = []rune("。「」、・ヲァィゥェォャュョッーアイウエオカキクケコサシスセソタチツテトナニヌネノハヒフヘホマミムメモヤユヨラリルレロワン゛゜")

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
