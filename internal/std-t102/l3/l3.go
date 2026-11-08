package l3

import (
	"fmt"
	"strings"
)

const (
	TypeVoice uint8 = 0x81
	TypeEnd   uint8 = 0x88
	TypeIdle  uint8 = 0x90
)

type Row struct {
	Name string `json:"name"`
	Bits string `json:"bits,omitempty"`
	Text string `json:"text"`
}

type Message struct {
	Ch   string `json:"ch"`
	Type uint8  `json:"type"`
	Name string `json:"name"`
	Maker int    `json:"maker"`
	Rows  []Row  `json:"rows"`
	Raw   []byte `json:"raw"`
	B54 *B54 `json:"b54,omitempty"`
}

func TypeName(t uint8) string {
	switch t {
	case TypeVoice:
		return "音声通信"
	case TypeEnd:
		return "終話"
	case TypeIdle:
		return "アイドル"
	}
	switch t >> 6 {
	case 2:
		return fmt.Sprintf("製造者識別メッセージ形式 1（未定義 0x%02X）", t)
	case 3:
		return fmt.Sprintf("製造者識別メッセージ形式 2（0x%02X・免許人・事業者が定義）", t)
	}
	return fmt.Sprintf("未定義 0x%02X", t)
}

func Parse(ch string, b []byte) Message {
	m := Message{Ch: ch, Maker: -1, Raw: append([]byte(nil), b...)}
	if len(b) == 0 {
		m.Name = "空"
		return m
	}
	m.Type = b[0]
	m.Name = TypeName(b[0])
	form := [4]string{"未定義", "未定義", "製造者識別メッセージ形式 1", "製造者識別メッセージ形式 2"}[b[0]>>6]
	var whyNot string
	if IsB54Type(b[0]) {
		if x, why := parseB54(b); x != nil {
			m.B54 = x
			m.Name = b54Names[b[0]]
			m.Rows = append(m.Rows, Row{Name: "メッセージ種別", Bits: bin(b[0], 8), Text: m.Name + "（" + form + "・ARIB STD-B54）"})
			if b54HasMaker(b[0]) {
				m.Maker = int(b[1] & 0x7F)
				m.Rows = append(m.Rows, Row{Name: "製造者識別", Bits: bin(b[1], 8),
					Text: fmt.Sprintf("製造者番号 %d（ビット 8 = 予備 %d）", m.Maker, b[1]>>7)})
			}
			m.Rows = append(m.Rows, b54Rows(x)...)
			return m
		} else {
			whyNot = why
		}
	}
	m.Rows = append(m.Rows, Row{Name: "メッセージ種別", Bits: bin(b[0], 8), Text: m.Name + "（" + form + "）"})
	if whyNot != "" {
		m.Rows = append(m.Rows, Row{Name: "ARIB STD-B54", Text: "B54 の " + b54Names[b[0]] + " の形に合わない: " + whyNot})
	}
	if len(b) < 2 {
		return m
	}
	m.Maker = int(b[1] & 0x7F)
	m.Rows = append(m.Rows, Row{Name: "製造者識別", Bits: bin(b[1], 8),
		Text: fmt.Sprintf("製造者番号 %d（ビット 8 = 予備 %d）", m.Maker, b[1]>>7)})
	if len(b) > 2 {
		rest := b[2:]
		txt := strings.ToUpper(fmt.Sprintf("% x", rest))
		if allZero(rest) {
			txt += "（全部 0）"
		}
		m.Rows = append(m.Rows, Row{Name: "製造者定義情報", Text: txt})
	}
	annotateVendor(&m)
	return m
}

func (m Message) Summary() string {
	if m.B54 != nil {
		return m.b54Summary()
	}
	if m.Maker < 0 {
		return m.Name
	}
	s := fmt.Sprintf("%s・製造者 %d", m.Name, m.Maker)
	if len(m.Raw) > 2 && !allZero(m.Raw[2:]) {
		s += " " + strings.ToUpper(fmt.Sprintf("%x", m.Raw[2:]))
	}
	return s
}

type SACCH struct {
	parts [4][]uint8
	have  [4]bool
	u     uint8
}

func (a *SACCH) Push(superframe bool, s, u uint8, data []uint8) ([]byte, bool) {
	if !superframe {
		a.reset()
		return pack(data), true
	}
	i := 3 - int(s&3)
	if i == 0 || u != a.u {
		a.reset()
		a.u = u
	}
	a.parts[i] = append([]uint8(nil), data...)
	a.have[i] = true
	if i != 3 {
		return nil, false
	}
	for _, h := range a.have {
		if !h {
			a.reset()
			return nil, false
		}
	}
	var all []uint8
	for _, p := range a.parts {
		all = append(all, p...)
	}
	a.reset()
	return pack(all), true
}

func (a *SACCH) Reset() { a.reset() }

func (a *SACCH) reset() {
	a.parts = [4][]uint8{}
	a.have = [4]bool{}
}

func pack(bits []uint8) []byte {
	out := make([]byte, (len(bits)+7)/8)
	for i, b := range bits {
		out[i/8] |= (b & 1) << (7 - i%8)
	}
	return out
}

func bin(v uint8, n int) string {
	var sb strings.Builder
	for i := n - 1; i >= 0; i-- {
		sb.WriteByte('0' + v>>i&1)
	}
	return sb.String()
}

func allZero(b []byte) bool {
	for _, x := range b {
		if x != 0 {
			return false
		}
	}
	return true
}
