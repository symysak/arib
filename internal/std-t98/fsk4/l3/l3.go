package l3

import (
	"fmt"
	"strings"
)

const (
	TypeIdle  uint8 = 0x00
	TypeVoice uint8 = 0x01
	TypeMakerDefFirst uint8 = 0x02
	TypeMakerDefLast  uint8 = 0x13
	TypeEnd           uint8 = 0x1E
)

const (
	CallNormal = 0
	CallCipher = 1
)

const CommonMaker = 0

func TypeName(t uint8) string {
	switch {
	case t == TypeIdle:
		return "アイドル"
	case t == TypeVoice:
		return "音声通信"
	case t == TypeEnd:
		return "終話"
	case t >= TypeMakerDefFirst && t <= TypeMakerDefLast:
		return fmt.Sprintf("製造者定義形式 %d", t-TypeMakerDefFirst+1)
	}
	return fmt.Sprintf("予約（0x%02X）", t)
}

func IsMakerDef(t uint8) bool { return t >= TypeMakerDefFirst && t <= TypeMakerDefLast }

func CallName(c int) string {
	switch c {
	case CallNormal:
		return "通常通信"
	case CallCipher:
		return "秘話通信"
	}
	return fmt.Sprintf("予約（%02b）", c)
}

type Row struct {
	Name string `json:"name"`
	Bits string `json:"bits,omitempty"`
	Text string `json:"text"`
}

type Message struct {
	Type uint8  `json:"type"`
	Name string `json:"name"`
	Call  int `json:"call"`
	UC    int `json:"uc"`
	Maker int `json:"maker"`
	Units    int     `json:"units"`
	MakerDef []uint8 `json:"maker_def,omitempty"`
	Raw []byte `json:"raw"`
}

func (m Message) Cipher() bool { return m.Call == CallCipher }

func (m Message) MakerDefHex() string {
	if len(m.MakerDef) == 0 {
		return ""
	}
	return strings.ToUpper(fmt.Sprintf("%x", pack(m.MakerDef)))
}

func (m Message) Summary() string {
	s := fmt.Sprintf("%s・UC %d・製造者 %d", m.Name, m.UC, m.Maker)
	if m.Call != CallNormal {
		s += "・" + CallName(m.Call)
	}
	if h := m.MakerDefHex(); h != "" {
		s += " " + h
	}
	return s
}

func (m Message) Rows() []Row {
	form := ""
	if IsMakerDef(m.Type) {
		form = "（製造者ごとに自由。規格は書式を決めていない）"
	}
	rows := []Row{
		{Name: "メッセージ種別", Bits: bin(uint(m.Type), 5), Text: m.Name + form},
		{Name: "通話識別", Bits: bin(uint(m.Call), 2), Text: CallName(m.Call)},
		{Name: "ユーザコード UC", Bits: bin(uint(m.UC), 9), Text: ucText(m.UC)},
		{Name: "製造者番号", Bits: bin(uint(m.Maker), 7), Text: makerText(m.Maker)},
		{Name: "ユニット数", Text: fmt.Sprintf("%d", m.Units)},
	}
	if len(m.MakerDef) > 0 {
		txt := m.MakerDefHex()
		if allZero(m.MakerDef) {
			txt += "（全部 0）"
		}
		rows = append(rows, Row{Name: fmt.Sprintf("製造者定義 %d ビット", len(m.MakerDef)), Bits: bitStr(m.MakerDef), Text: txt})
	}
	return rows
}

func ucText(uc int) string {
	if uc == 0 {
		return "0（ユーザコードで分けない。呼出用チャネルはいつも 0）"
	}
	return fmt.Sprintf("%d", uc)
}

func makerText(n int) string {
	if n == CommonMaker {
		return "0（共通製造者番号 = 製造者無指定）"
	}
	return fmt.Sprintf("%d", n)
}

type Asm struct {
	typ   uint8
	bits  []uint8
	left  int
	units int
}

func NewAsm() *Asm { return &Asm{left: -1} }

func (a *Asm) Reset() { a.left, a.bits, a.units = -1, nil, 0 }

func (a *Asm) Push(head uint8, data []uint8) (Message, bool) {
	first, remain, typ := head>>7 == 1, int(head>>5&3), head&0x1F
	switch {
	case first:
		a.typ, a.bits, a.units = typ, append([]uint8(nil), data[:18]...), 1
	case a.left < 0 || remain != a.left || typ != a.typ:
		a.Reset()
		return Message{}, false
	default:
		a.bits = append(a.bits, data[:18]...)
		a.units++
	}
	if remain > 0 {
		a.left = remain - 1
		return Message{}, false
	}
	m := parse(a.typ, a.bits, a.units)
	a.Reset()
	return m, true
}

func parse(typ uint8, bits []uint8, units int) Message {
	m := Message{Type: typ, Name: TypeName(typ), Units: units}
	m.Call = int(num(bits[0:2]))
	m.UC = int(num(bits[2:11]))
	m.Maker = int(num(bits[11:18]))
	if len(bits) > 18 {
		m.MakerDef = append([]uint8(nil), bits[18:]...)
	}
	m.Raw = append([]byte{typ}, pack(bits)...)
	return m
}

func First(data []uint8) (call, uc, maker int) {
	return int(num(data[0:2])), int(num(data[2:11])), int(num(data[11:18]))
}

func Info(call, uc, maker int) []uint8 {
	out := make([]uint8, 0, 18)
	put := func(v, n int) {
		for i := n - 1; i >= 0; i-- {
			out = append(out, uint8(v>>i&1))
		}
	}
	put(call, 2)
	put(uc, 9)
	put(maker, 7)
	return out
}


const (
	ClassUnknown  = 0
	ClassLicensed = 1
	ClassRegister = 2
)

type PICH struct {
	CSM   string `json:"csm"`
	Valid bool   `json:"valid"`
	Class int `json:"class"`
	Reserved string `json:"reserved,omitempty"`
	Raw      []byte `json:"raw"`
}

func ParsePICH(b [10]byte) PICH {
	p := PICH{Valid: true, Raw: append([]byte(nil), b[:]...)}
	var sb strings.Builder
	for i := 0; i < 9; i++ {
		d := b[i/2] >> 4
		if i%2 == 1 {
			d = b[i/2] & 0xF
		}
		if d > 9 {
			p.Valid = false
		}
		sb.WriteString(strings.ToUpper(fmt.Sprintf("%x", d)))
	}
	p.CSM = sb.String()
	if p.Valid {
		switch p.CSM[0] {
		case '1':
			p.Class = ClassLicensed
		case '2':
			p.Class = ClassRegister
		}
	}
	rest := append([]byte{b[4] & 0x0F}, b[5:]...)
	for _, x := range rest {
		if x != 0 {
			p.Reserved = strings.ToUpper(fmt.Sprintf("%x", rest))[1:]
			break
		}
	}
	return p
}

func ClassName(c int) string {
	switch c {
	case ClassLicensed:
		return "免許局"
	case ClassRegister:
		return "登録局"
	}
	return "区分不明"
}

func (p PICH) Summary() string {
	if !p.Valid {
		return "呼出名称 " + p.CSM + "（BCD でない桁がある）"
	}
	s := "呼出名称 " + p.CSM + "（" + ClassName(p.Class) + "）"
	if p.Reserved != "" {
		s += " 予約 " + p.Reserved
	}
	return s
}

func (p PICH) Rows() []Row {
	rows := []Row{{Name: "呼出名称 CSM", Bits: fmt.Sprintf("%X", p.Raw[:5])[:9], Text: p.CSM + "（9 桁の二進化十進数。§3.7.1 (5)）"}}
	if p.Valid {
		rows = append(rows, Row{Name: "区分（先頭の桁）", Text: ClassName(p.Class)})
	} else {
		rows = append(rows, Row{Name: "検査", Text: "BCD でない桁がある（CRC は通ったが呼出名称として読めない）"})
	}
	if p.Reserved == "" {
		rows = append(rows, Row{Name: "予約 44 ビット", Text: "全部 0（標準値）"})
	} else {
		rows = append(rows, Row{Name: "予約 44 ビット", Text: p.Reserved + "（標準値は全部 0。製造者が何か載せている）"})
	}
	return rows
}

func PICHInfo(csm string) []uint8 {
	out := make([]uint8, 80)
	for i := 0; i < 9 && i < len(csm); i++ {
		d := csm[i] - '0'
		for k := 0; k < 4; k++ {
			out[4*i+k] = d >> (3 - k) & 1
		}
	}
	return out
}

func num(b []uint8) uint {
	var v uint
	for _, x := range b {
		v = v<<1 | uint(x&1)
	}
	return v
}

func pack(bits []uint8) []byte {
	out := make([]byte, (len(bits)+7)/8)
	for i, b := range bits {
		out[i/8] |= (b & 1) << (7 - i%8)
	}
	return out
}

func bin(v uint, n int) string {
	out := make([]byte, n)
	for i := range out {
		out[i] = '0' + byte(v>>(n-1-i)&1)
	}
	return string(out)
}

func bitStr(b []uint8) string {
	out := make([]byte, len(b))
	for i, x := range b {
		out[i] = '0' + x&1
	}
	return string(out)
}

func allZero(b []uint8) bool {
	for _, x := range b {
		if x != 0 {
			return false
		}
	}
	return true
}
