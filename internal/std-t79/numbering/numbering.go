package numbering

import (
	"fmt"
	"strings"

	"github.com/symysak/arib/internal/citycodes"
)

type System struct {
	Code     uint64
	Bits     int
	Industry int
	Seq      int
	Area     int
	AreaName string
	Org string
	ASCII string
}

func SystemCode(code uint64, bits int) System {
	s := System{Code: code, Bits: bits, Industry: -1, Seq: -1, Area: -1}
	switch bits {
	case 24:
		s.Seq, s.Industry = int(code>>20&0xF), int(code>>16&0xF)
		s.Area = int(code & 0xFFFF)
	case 16:
		s.Area = int(code & 0xFFFF)
	}
	if s.Area >= 1000 && s.Area < 48000 {
		if s.Area%1000 == 0 {
			s.AreaName = PrefName(s.Area / 1000)
		} else if name, ok := citycodes.Name(s.Area); ok {
			s.AreaName = name
		}
	}
	if s.AreaName == "" {
		s.ASCII = asciiCode(code, bits)
	}
	switch {
	case s.AreaName != "":
		s.Org = s.AreaName
	case s.ASCII != "":
		s.Org = fmt.Sprintf("%q（ASCII）", s.ASCII)
	case s.Area >= 0:
		s.Org = fmt.Sprintf("分類 %05d", s.Area)
	}
	if s.Org != "" && s.Industry == 0xD {
		s.Org += "・公務"
	}
	return s
}

func asciiCode(code uint64, bits int) string {
	if bits <= 0 || bits%8 != 0 {
		return ""
	}
	b := make([]byte, bits/8)
	for i := range b {
		b[i] = byte(code >> (8 * (len(b) - 1 - i)))
		if b[i] < 0x21 || b[i] > 0x7E {
			return ""
		}
	}
	return string(b)
}

func SystemBytes(b []byte) System {
	var v uint64
	for _, o := range b {
		v = v<<8 | uint64(o)
	}
	return SystemCode(v, 8*len(b))
}

func IndustryName(v int) string {
	if v == 0xD {
		return "公務"
	}
	return "予約"
}

func SeqName(v int) string {
	switch v {
	case 1:
		return "都道府県・市町村デジタル移動通信システム（2.0版以降）"
	case 2:
		return "市町村デジタル移動通信システム TYPE1（1.1版）"
	}
	return "予約"
}

func (s System) Text() string {
	switch s.Bits {
	case 24:
		t := fmt.Sprintf("0x%06X（業種 %X=%s・通番 %d=%s・%s）", s.Code, s.Industry, IndustryName(s.Industry), s.Seq, SeqName(s.Seq), AreaText(s.Area))
		if s.ASCII != "" {
			t += fmt.Sprintf(" ASCII で %q", s.ASCII)
		}
		return t
	case 16:
		return fmt.Sprintf("0x%04X（短縮システムコード・%s）", s.Code, AreaText(s.Area))
	}
	return fmt.Sprintf("%d (0x%X)", s.Code, s.Code)
}

func AreaText(c int) string {
	switch {
	case c >= 48000:
		return fmt.Sprintf("分類 %d（政令指定都市等の予約番号）", c)
	case c < 1000:
		return fmt.Sprintf("分類 %d（範囲外）", c)
	case c%1000 == 0:
		if name := PrefName(c / 1000); name != "" {
			return fmt.Sprintf("分類 %05d = %s", c, name)
		}
		return fmt.Sprintf("分類 %05d = 都道府県 %02d", c, c/1000)
	}
	if name, ok := citycodes.Name(c); ok {
		return fmt.Sprintf("分類 %05d = %s", c, name)
	}
	return fmt.Sprintf("分類 %05d（市区町村コード表に無い）", c)
}

func PrefName(pref int) string {
	for k := 1; k < 1000; k++ {
		if name, ok := citycodes.Name(pref*1000 + k); ok {
			name = strings.TrimPrefix(name, "旧 ")
			if i := strings.IndexByte(name, ' '); i > 0 {
				return name[:i]
			}
		}
	}
	return ""
}

type MSI struct {
	System     System
	Individual bool
	Number     int
	Raw       int
	UpAligned bool
}

func ParseMSI(v uint64, bits int) (MSI, bool) {
	if bits < 21 {
		return MSI{}, false
	}
	call := v & 0xFFFFF
	m := MSI{Individual: call>>19 == 1, Raw: int(call & 0x7FFFF)}
	if sys := v >> 20; sys != 0 {
		m.System = SystemCode(sys, 24)
	} else {
		m.System = System{Industry: -1, Seq: -1, Area: -1}
	}
	m.UpAligned = m.System.Seq != 2 && m.Raw&0x1FF == 0
	m.Number = m.Raw
	if m.UpAligned {
		m.Number = m.Raw >> 9
	}
	return m, true
}

func CallSign(id int) MSI {
	return MSI{Individual: id>>19 == 1, Raw: id & 0x7FFFF, Number: id & 0x7FFFF, System: System{Industry: -1, Seq: -1, Area: -1}}
}

func (m MSI) Digits() string {
	if m.System.Area < 0 {
		return ""
	}
	if m.Individual {
		return fmt.Sprintf("%05d%03d", m.System.Area, m.Number%1000)
	}
	return fmt.Sprintf("%05d%02d", m.System.Area, m.Number%100)
}

func (m MSI) GroupClass() string {
	switch n := m.Number; {
	case m.Individual:
		return ""
	case n <= 89:
		return "自システム"
	case n <= 98:
		return "応援通信用"
	case n == 99:
		return "全局応援通信用（全国共通）"
	}
	return "範囲外"
}

func (m MSI) Text() string {
	kind := "グループ番号"
	if m.Individual {
		kind = "個別番号"
	}
	org := m.System.Org
	if org == "" {
		org = "システムコード 0"
	}
	s := fmt.Sprintf("%s %s %d", org, kind, m.Number)
	var note []string
	if d := m.Digits(); d != "" {
		note = append(note, d)
	}
	if c := m.GroupClass(); c != "" {
		note = append(note, c)
	}
	if !m.UpAligned && m.System.Seq != 2 && m.System.Bits > 0 {
		note = append(note, "下詰め")
	}
	if len(note) > 0 {
		s += "（" + strings.Join(note, "・") + "）"
	}
	return s
}
