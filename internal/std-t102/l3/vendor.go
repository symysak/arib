package l3

import (
	"encoding/binary"
	"fmt"
)


var vendors = map[int]func(m *Message){
	10: annotate10,
}

func HasVendor(n int) bool { _, ok := vendors[n]; return ok }

func (m *Message) addVendor(name, value string) {
	m.Rows = append(m.Rows, Row{Name: fmt.Sprintf("%s（独自・製造者 %d・推定）", name, m.Maker), Text: value})
}

func annotateVendor(m *Message) {
	if f, ok := vendors[m.Maker]; ok {
		f(m)
	}
}

func Radio10(m Message) (int, bool) {
	b := m.Raw
	if m.Maker != 10 {
		return 0, false
	}
	switch {
	case m.Ch == "SACCH" && len(b) == 9:
		return int(b[3]), true
	case len(b) >= 4 && b[3]&3 == 0 && (b[2] != 0 || b[3] != 0):
		return int(binary.BigEndian.Uint16(b[2:4]) >> 2), true
	}
	return 0, false
}

func annotate10(m *Message) {
	if id, ok := Radio10(*m); ok {
		m.addVendor("無線機の番号", fmt.Sprintf("%02X（%d）", id, id))
		m.Name += fmt.Sprintf("（無線機 %02X）", id)
	}
}
