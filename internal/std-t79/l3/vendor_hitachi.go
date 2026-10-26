package l3

import (
	"fmt"
	"strconv"
)

func annotateHitachi(m *Message, c Ctx) {
	if m.Entity != "RT" || (m.Type != 0x62 && m.Type != 0xA1) {
		return
	}
	it := m.vendorItem(extensionWhat)
	if it == nil || len(it.Raw) < 9 {
		return
	}
	k, ok := hitachiTime(it.Raw[:9])
	if !ok {
		return
	}
	if m.Type == 0x62 {
		k.What, k.PerCall = "通話の時刻", true
	} else {
		k.What, k.Step = "基地局時刻", 60
	}
	it.Known = len(it.Raw) == 9
	m.Clock = k
	m.addVendorField(MakerHitachi, k.What, k.Text)
	m.addVendorField(MakerHitachi, "日時の前の 2 欄", fmt.Sprintf("種別 0x%02X / 0x%02X（意味は不明）", it.Raw[0], it.Raw[5]))
	if len(it.Raw) > 9 {
		m.addVendorField(MakerHitachi, "日時の後ろ（不明）", hexOf(it.Raw[9:]))
	}
}

func hitachiTime(b []byte) (*Clock, bool) {
	d, ok := tbcd(b[1:9])
	if !ok {
		return nil, false
	}
	num := func(i, n int) int { v, _ := strconv.Atoi(d[i : i+n]); return v }
	y, mo, dd, h, mi, s := num(0, 4), num(4, 2), num(6, 2), num(10, 2), num(12, 2), num(14, 2)
	sec := clockOf(h, mi, s)
	if sec < 0 || mo < 1 || mo > 12 || dd < 1 || dd > 31 {
		return nil, false
	}
	date := fmt.Sprintf("%04d-%02d-%02d", y, mo, dd)
	return &Clock{Date: date, Sec: sec, Text: fmt.Sprintf("%s %02d:%02d:%02d", date, h, mi, s), ByMaker: MakerHitachi}, true
}

func tbcd(b []byte) (string, bool) {
	out := make([]byte, 0, 2*len(b))
	for _, o := range b {
		for _, n := range []byte{o & 0xF, o >> 4} {
			if n > 9 {
				return "", false
			}
			out = append(out, '0'+n)
		}
	}
	return string(out), true
}
