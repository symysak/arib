package l3

import (
	"fmt"
	"strings"
)


var ccDirectTables = map[uint8][]ccMsgIE{
	0x1A: {
		{ref: "5.3", name: "伝達能力", dir: "発→着", mandatory: true, maxLen: 8},
		{ref: "5.9", name: "チャネル識別子", dir: "発→着", maxLen: 3},
		{ref: "5.15", name: "経過識別子", dir: "発→着", maxLen: 3},
		{ref: "5.14", name: "キーパッドファシリティ", dir: "発→着", maxLen: 35},
		{ref: "5.11", name: "フィーチャアクティベーション", dir: "発→着", maxLen: 4},
		{ref: "5.6", name: "発番号", dir: "発→着", maxLen: 16},
		{ref: "5.7", name: "発サブアドレス（第 1）", dir: "発→着", maxLen: 23},
		{ref: "5.4", name: "着番号", dir: "発→着", mandatory: true, maxLen: 15},
		{ref: "5.5", name: "着サブアドレス（第 1）", dir: "発→着", maxLen: 23},
		{ref: "5.13", name: "高位レイヤ整合性", dir: "発→着", maxLen: 3},
		{ref: "5.21", name: "ユーザ・ユーザ", dir: "発→着", maxLen: 131},
		{ref: "5.2", name: "固定シフト（コード群 6）", dir: "発→着", maxLen: 1, shift: 6},
		{ref: "5.28", name: "製造者番号", dir: "発→着", maxLen: 3},
		{ref: "5.25", name: "発サブアドレス（第 2）", dir: "発→着", maxLen: 23},
		{ref: "5.24", name: "着サブアドレス（第 2）", dir: "発→着", maxLen: 23},
		{ref: "5.29", name: "非音声通信用伝達能力", dir: "発→着"},
		{ref: "5.30", name: "免許人固有ユーザ・ユーザ", dir: "発→着", maxLen: 130},
	},
	0x1B: {
		{ref: "5.3", name: "伝達能力", dir: "発→着", mandatory: true, maxLen: 8},
		{ref: "5.6", name: "発番号", dir: "発→着", mandatory: true, maxLen: 16},
		{ref: "5.2", name: "固定シフト（コード群 6）", dir: "発→着", maxLen: 1, shift: 6},
		{ref: "5.28", name: "製造者番号", dir: "発→着", maxLen: 3},
		{ref: "5.29", name: "非音声通信用伝達能力", dir: "発→着"},
	},
}

func init() {
	for t, rows := range ccDirectTables {
		ccMsgTables[t] = rows
	}
}

type ccMsgIE struct {
	ref       string
	name      string
	dir       string
	mandatory bool
	maxLen    int
	shift     int
}

type ccIdent struct {
	cs     int
	id     byte
	single bool
	mask   byte
}

var ccRefIdent = map[string]ccIdent{
	"5.3": {0, 0x04, false, 0}, "5.4": {0, 0x70, false, 0}, "5.5": {0, 0x71, false, 0}, "5.6": {0, 0x6C, false, 0},
	"5.7": {0, 0x6D, false, 0}, "5.8": {0, 0x08, false, 0}, "5.9": {0, 0x18, false, 0}, "5.10": {0, 0x28, false, 0},
	"5.11": {0, 0x38, false, 0}, "5.12": {0, 0x39, false, 0}, "5.13": {0, 0x7D, false, 0}, "5.14": {0, 0x2C, false, 0},
	"5.15": {0, 0x1E, false, 0}, "5.16": {0, 0x34, false, 0}, "5.17": {0, 0xA0, true, 0xFF}, "5.18": {0, 0xB0, true, 0xF0},
	"5.19": {0, 0x14, false, 0}, "5.20": {0, 0x1C, false, 0}, "5.21": {0, 0x7E, false, 0}, "5.22": {5, 0x82, true, 0xFF},
	"5.23": {6, 0x1F, false, 0}, "5.24": {6, 0x72, false, 0}, "5.25": {6, 0x6E, false, 0}, "5.26": {6, 0xF0, true, 0xF0},
	"5.27": {6, 0x0F, false, 0}, "5.28": {6, 0x01, false, 0}, "5.29": {6, 0x04, false, 0}, "5.30": {6, 0x7E, false, 0},
}

type ccSeen struct {
	cs     int
	id     byte
	single bool
	shift  int
	octets int
	name   string
}

func (r ccMsgIE) matches(s ccSeen) bool {
	if r.ref == "5.2" {
		return s.shift == r.shift
	}
	id, ok := ccRefIdent[r.ref]
	if !ok || s.shift >= 0 || id.cs != s.cs || id.single != s.single {
		return false
	}
	if id.single {
		return s.id&id.mask == id.id
	}
	return s.id == id.id
}

func ccValidate(m *Message, t uint8, seen []ccSeen, serviceID int) {
	if serviceID > 1 {
		m.Notes = append(m.Notes, fmt.Sprintf("付加サービス識別子 %d は予約（§4.3.7.3.3: 受信側はこのメッセージを無視する）", serviceID))
	}
	rows, ok := ccMsgTables[t]
	if !ok {
		return
	}
	used := make([]bool, len(seen))
	for _, r := range rows {
		found := false
		for i, s := range seen {
			if r.matches(s) {
				found, used[i] = true, true
				max := r.maxLen
				if max < 0 && r.ref == "5.10" {
					max = 34
				}
				if max > 0 && s.octets > max {
					m.Notes = append(m.Notes, fmt.Sprintf("%s が %d オクテット（最大 %d。表 4.3.7.1）", r.name, s.octets, max))
				}
			}
		}
		if r.mandatory && !found {
			msg := fmt.Sprintf("必須の情報要素「%s」（§4.3.7.2.%s）が無い", r.name, r.ref)
			m.Notes = append(m.Notes, msg)
			if m.Err == "" {
				m.Err = msg
			}
		}
	}
	var extra []string
	for i, s := range seen {
		if !used[i] {
			extra = append(extra, s.name)
		}
	}
	if len(extra) > 0 {
		m.Notes = append(m.Notes, "このメッセージの表に無い情報要素: "+strings.Join(extra, "・"))
	}
	last := map[int]byte{}
	for _, s := range seen {
		if s.single || s.shift >= 0 {
			continue
		}
		if p, ok := last[s.cs]; ok && (s.id < p || (s.id == p && s.id != 0x04)) {
			m.Notes = append(m.Notes, fmt.Sprintf("「%s」の識別子 0x%02X が前の 0x%02X より小さいか重複（コード群 %d。§4.3.7.2.5.1 は昇順）", s.name, s.id, p, s.cs))
		}
		last[s.cs] = s.id
	}
}
