package l3

import "fmt"

func annotateJRC(m *Message, c Ctx) {
	switch m.Entity {
	case "RT":
		switch m.Type {
		case 0xA1:
			jrcClock(m)
		case 0xB1, 0xB2, 0xB3, 0xB4:
			jrcShort(m)
		}
	case "MM":
		it := m.vendorItem(fmt.Sprintf("MM 種別 0x%02X", m.Type))
		if it == nil {
			return
		}
		switch m.Type {
		case 0x24:
			m.renameUnknown(MakerJRC, "位置登録の付帯 要求")
			r := &reader{b: it.Raw}
			for _, f := range mmRegNumber(r) {
				m.addVendorField(MakerJRC, f.Name, f.Value)
			}
			it.Known = r.err == "" && r.left() == 0
		case 0x25:
			m.renameUnknown(MakerJRC, "位置登録の付帯 応答")
			if len(it.Raw) == 1 {
				m.addVendorField(MakerJRC, "移動局に振る番号", fmt.Sprint(it.Raw[0]))
				it.Known = true
			}
		}
	case "CC":
		it := m.vendorItem(fmt.Sprintf("CC 種別 0x%02X", m.Type))
		if it == nil || m.Call == nil {
			return
		}
		switch m.Type {
		case 0x46:
			m.Call.Release = true
			it.Known = true
			m.renameUnknown(MakerJRC, "切断")
		case 0x1C:
			m.Call.Setup = true
			it.Known = true
			m.renameUnknown(MakerJRC, "非音声呼設定")
		}
	}
}

func jrcClock(m *Message) {
	it := m.vendorItem(extensionWhat)
	if it == nil || (len(it.Raw) != 3 && len(it.Raw) != 6) {
		return
	}
	b := it.Raw
	sec := clockOf(int(b[0]), int(b[1]), int(b[2]))
	if sec < 0 {
		return
	}
	k := &Clock{What: "基地局時刻", Sec: sec, Text: fmt.Sprintf("%02d:%02d:%02d", b[0], b[1], b[2]), ByMaker: MakerJRC}
	if len(b) == 6 {
		if b[4] < 1 || b[4] > 12 || b[5] < 1 || b[5] > 31 {
			return
		}
		k.Date = fmt.Sprintf("20%02d-%02d-%02d", b[3], b[4], b[5])
		k.Text = k.Date + " " + k.Text
	}
	it.Known = true
	m.Clock = k
	m.addVendorField(MakerJRC, "基地局時刻", k.Text)
}

var jrcShortKinds = map[int]string{0x1F: "定期の問い合わせ", 0x2F: "定期の問い合わせの応答"}

func jrcShort(m *Message) {
	sm := m.Short
	if sm == nil {
		return
	}
	name, ok := jrcShortKinds[sm.Number]
	if !ok {
		return
	}
	if it := m.vendorItem(fmt.Sprintf("メッセージ番号 0x%02X", sm.Number)); it != nil {
		it.Known = true
	}
	sm.Kind = fmt.Sprintf("%s（独自 0x%02X・推定）", name, sm.Number)
	sm.Text = fmt.Sprintf("%s → %s %s", orQ(sm.From), orQ(sm.To), sm.Kind)
	m.addVendorField(MakerJRC, "メッセージ番号の意味", name)
}
