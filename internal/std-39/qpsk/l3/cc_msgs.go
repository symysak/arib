package l3


type ccDir byte

const (
	ccBoth ccDir = 'B'
	ccDown ccDir = 'D'
	ccUp   ccDir = 'U'
)

func (d ccDir) text() string {
	switch d {
	case ccDown:
		return "下り"
	case ccUp:
		return "上り"
	}
	return "両方向"
}

type ccRow struct {
	ref       string
	dir       ccDir
	mandatory bool
	min, max  int
	shift     int
	rep       int
}

type ccMsg struct {
	name  string
	en    string
	sec   string
	table string
	area  string
	dir   ccDir
	chans string
	group string
	rows  []ccRow
}

var (
	rowChanID = func(d ccDir) ccRow { return ccRow{ref: "5.9", dir: d, min: 2, max: 3} }
	rowFac    = func(d ccDir) ccRow { return ccRow{ref: "5.20", dir: d, min: 2} }
	rowProg   = func(d ccDir) ccRow { return ccRow{ref: "5.15", dir: d, min: 3, max: 3, rep: 2} }
	rowDisp   = func(d ccDir) ccRow { return ccRow{ref: "5.10", dir: d, min: 2, max: 34} }
	rowSignal = func(d ccDir) ccRow { return ccRow{ref: "5.16", dir: d, min: 2, max: 2} }
	rowFA     = func(d ccDir) ccRow { return ccRow{ref: "5.11", dir: d, min: 2, max: 4} }
	rowFI     = func(d ccDir) ccRow { return ccRow{ref: "5.12", dir: d, min: 2, max: 5} }
	rowUU     = func(d ccDir) ccRow { return ccRow{ref: "5.21", dir: d, min: 2, max: 131} }
	rowCause  = func(d ccDir) ccRow { return ccRow{ref: "5.8", dir: d, min: 2, max: 5} }
	rowKeypad = func(d ccDir) ccRow { return ccRow{ref: "5.14", dir: d, min: 2, max: 35} }
	rowBearer = func(d ccDir) ccRow { return ccRow{ref: "5.3", dir: d, mandatory: true, min: 5, max: 8, rep: -1} }
	rowShift  = func(d ccDir, cs int, m bool) ccRow {
		return ccRow{ref: "5.2", dir: d, mandatory: m, min: 1, max: 1, shift: cs}
	}
	rowSubaddr = func(ref string, d ccDir) ccRow { return ccRow{ref: ref, dir: d, min: 2, max: 23} }
)

func ccSetupRows() []ccRow {
	return []ccRow{
		rowBearer(ccUp), rowChanID(ccUp), rowFac(ccUp), rowProg(ccUp), rowKeypad(ccUp), rowFA(ccUp),
		{ref: "5.6", dir: ccUp, min: 2, max: 16},
		rowSubaddr("5.7", ccUp),
		{ref: "5.4", dir: ccUp, mandatory: true, min: 5, max: 15},
		rowSubaddr("5.5", ccUp),
		{ref: "5.13", dir: ccUp, min: 3, max: 3},
		rowUU(ccUp),
		rowShift(ccUp, 6, false),
		rowSubaddr("5.25", ccUp), rowSubaddr("5.24", ccUp),
	}
}

func ccReSetupRows() []ccRow {
	return []ccRow{
		rowShift(ccUp, 6, true),
		{ref: "5.27", dir: ccUp, mandatory: true, min: 4, max: 11},
		{ref: "5.23", dir: ccUp, mandatory: true, min: 3, max: 3},
	}
}

var ccMsgs = map[uint8]*ccMsg{
	0x01: {name: "呼出", en: "ALERTing", sec: "4.3.7.2.1.1", table: "表 4.3.7.2-2", area: "グローバル", dir: ccBoth, chans: "SACCH／FACCH", group: "呼設定用メッセージ",
		rows: []ccRow{rowChanID(ccBoth), rowFac(ccBoth), rowProg(ccBoth), rowDisp(ccDown), rowSignal(ccDown), rowFA(ccUp), rowFI(ccDown), rowUU(ccBoth)}},
	0x02: {name: "呼設定受付", en: "CALL PROCeeding", sec: "4.3.7.2.1.2", table: "表 4.3.7.2-3", area: "ローカル", dir: ccDown, chans: "SCCH，SACCH／FACCH", group: "呼設定用メッセージ",
		rows: []ccRow{rowChanID(ccDown), rowFac(ccDown), rowProg(ccDown), rowDisp(ccDown), rowFI(ccDown), rowShift(ccDown, 5, false), {ref: "5.22", dir: ccDown, min: 1, max: 1}}},
	0x07: {name: "応答", en: "CONNect", sec: "4.3.7.2.1.3", table: "表 4.3.7.2-4", area: "グローバル", dir: ccBoth, chans: "SACCH／FACCH", group: "呼設定用メッセージ",
		rows: []ccRow{rowChanID(ccBoth), rowFac(ccBoth), rowProg(ccBoth), rowDisp(ccDown), rowSignal(ccDown), rowFA(ccUp), rowFI(ccDown), rowUU(ccDown)}},
	0x0F: {name: "応答確認", en: "CONNect ACKnowledge", sec: "4.3.7.2.1.4", table: "表 4.3.7.2-5", area: "ローカル", dir: ccBoth, chans: "SACCH／FACCH", group: "呼設定用メッセージ",
		rows: []ccRow{rowChanID(ccDown), rowDisp(ccDown), rowSignal(ccDown)}},
	0x03: {name: "経過表示", en: "PROGress", sec: "4.3.7.2.1.7", table: "表 4.3.7.2-8", area: "グローバル", dir: ccDown, chans: "SCCH，SACCH／FACCH", group: "呼設定用メッセージ",
		rows: []ccRow{rowCause(ccDown), rowFac(ccDown), {ref: "5.15", dir: ccDown, mandatory: true, min: 3, max: 3, rep: 2}, rowDisp(ccDown), rowUU(ccDown)}},
	0x05: {name: "呼設定", en: "SETUP", sec: "4.3.7.2.1.10", table: "表 4.3.7.2-11", area: "グローバル", dir: ccDown, chans: "SCCH，SACCH／FACCH", group: "呼設定用メッセージ",
		rows: []ccRow{
			rowBearer(ccDown), rowChanID(ccDown), rowFac(ccDown), rowProg(ccDown), rowDisp(ccDown), rowKeypad(ccDown), rowSignal(ccDown), rowFI(ccDown),
			{ref: "5.6", dir: ccDown, min: 2, max: 16},
			rowSubaddr("5.7", ccDown),
			{ref: "5.4", dir: ccDown, min: 5, max: 15},
			rowSubaddr("5.5", ccDown),
			{ref: "5.13", dir: ccDown, min: 3, max: 3},
			rowUU(ccDown),
			rowShift(ccDown, 6, false),
			rowSubaddr("5.25", ccDown), rowSubaddr("5.24", ccDown),
		}},
	0x11: {name: "再呼設定受付", en: "ReCALL PROCeeding", sec: "4.3.7.2.1.15", table: "表 4.3.7.2-16", area: "ローカル", dir: ccDown, chans: "SCCH", group: "呼設定用メッセージ"},
	0x12: {name: "通信形態", en: "Communication Mode", sec: "4.3.7.2.1.16", table: "表 4.3.7.2-17", area: "ローカル", dir: ccDown, chans: "PCH", group: "呼設定用メッセージ",
		rows: []ccRow{rowBearer(ccDown)}},
	0x13: {name: "通信可否", en: "Communication Possibility", sec: "4.3.7.2.1.17", table: "表 4.3.7.2-18", area: "ローカル", dir: ccUp, chans: "SCCH", group: "呼設定用メッセージ",
		rows: []ccRow{rowShift(ccUp, 6, true), {ref: "5.26", dir: ccUp, mandatory: true, min: 1, max: 1}}},
	0x14: {name: "再呼設定拒否", en: "ReCALL PROCeeding Reject", sec: "4.3.7.2.1.18", table: "表 4.3.7.2-19", area: "ローカル", dir: ccDown, chans: "SCCH", group: "呼設定用メッセージ"},
	0x15: {name: "単信呼設定", en: "Simplex Operation SETUP", sec: "4.3.7.2.1.19", table: "表 4.3.7.2-20", area: "グローバル", dir: ccUp, chans: "SCCH", group: "呼設定用メッセージ",
		rows: ccSetupRows()},
	0x16: {name: "単信再呼設定", en: "Simplex Operation Re-SETUP", sec: "4.3.7.2.1.20", table: "表 4.3.7.2-21", area: "ローカル", dir: ccUp, chans: "SCCH", group: "呼設定用メッセージ",
		rows: ccReSetupRows()},
	0x17: {name: "複信呼設定", en: "Duplex Operation SETUP", sec: "4.3.7.2.1.21", table: "表 4.3.7.2-22", area: "グローバル", dir: ccUp, chans: "SCCH", group: "呼設定用メッセージ",
		rows: ccSetupRows()},
	0x18: {name: "複信再呼設定", en: "Duplex Operation Re-SETUP", sec: "4.3.7.2.1.22", table: "表 4.3.7.2-23", area: "ローカル", dir: ccUp, chans: "SCCH", group: "呼設定用メッセージ",
		rows: ccReSetupRows()},
	0x20: {name: "ユーザ情報", en: "USER INFOrmation", sec: "4.3.7.2.1.11", table: "表 4.3.7.2-12", area: "アクセス", dir: ccBoth, chans: "SACCH／FACCH", group: "通信中メッセージ",
		rows: []ccRow{{ref: "5.21", dir: ccBoth, mandatory: true, min: 2, max: 131}, {ref: "5.17", dir: ccBoth, min: 1, max: 1}}},
	0x45: {name: "切断", en: "DISConnect", sec: "4.3.7.2.1.5", table: "表 4.3.7.2-6", area: "グローバル", dir: ccBoth, chans: "SCCH，SACCH／FACCH", group: "呼解放用メッセージ",
		rows: []ccRow{{ref: "5.8", dir: ccBoth, mandatory: true, min: 2, max: 5}, rowFac(ccBoth), rowProg(ccBoth), rowDisp(ccDown), rowSignal(ccDown), rowFI(ccDown), rowUU(ccBoth)}},
	0x4D: {name: "解放", en: "RELease", sec: "4.3.7.2.1.8", table: "表 4.3.7.2-9", area: "ローカル（注 1）", dir: ccBoth, chans: "SCCH，SACCH／FACCH", group: "呼解放用メッセージ",
		rows: []ccRow{rowCause(ccBoth), rowDisp(ccDown), rowSignal(ccDown), rowFI(ccDown), rowUU(ccBoth)}},
	0x5A: {name: "解放完了", en: "RELease COMPlete", sec: "4.3.7.2.1.9", table: "表 4.3.7.2-10", area: "ローカル（注 1）", dir: ccBoth, chans: "SCCH，SACCH／FACCH", group: "呼解放用メッセージ",
		rows: []ccRow{rowCause(ccBoth), rowDisp(ccDown), rowSignal(ccDown), rowFI(ccDown), rowUU(ccUp)}},
	0x62: {name: "ファシリティ", en: "FACility", sec: "4.3.7.2.1.12", table: "表 4.3.7.2-13", area: "ローカル（注 1）", dir: ccBoth, chans: "SACCH／FACCH", group: "その他のメッセージ",
		rows: []ccRow{{ref: "5.20", dir: ccBoth, mandatory: true, min: 2}, rowDisp(ccBoth)}},
	0x79: {name: "輻輳制御", en: "CONGestion CONtrol", sec: "4.3.7.2.1.13", table: "表 4.3.7.2-14", area: "ローカル（注 1）", dir: ccBoth, chans: "SACCH／FACCH", group: "その他のメッセージ",
		rows: []ccRow{rowCause(ccBoth), rowDisp(ccDown), {ref: "5.18", dir: ccBoth, mandatory: true, min: 1, max: 1}}},
	0x7B: {name: "付加情報", en: "INFOrmation", sec: "4.3.7.2.1.6", table: "表 4.3.7.2-7", area: "ローカル", dir: ccBoth, chans: "SCCH，SACCH／FACCH", group: "その他のメッセージ",
		rows: []ccRow{rowCause(ccDown), rowDisp(ccDown), rowKeypad(ccBoth), rowSignal(ccDown), rowFA(ccUp), rowFI(ccDown)}},
	0x7D: {name: "状態表示", en: "STATus", sec: "4.3.7.2.1.14", table: "表 4.3.7.2-15", area: "ローカル", dir: ccBoth, chans: "SCCH，SACCH／FACCH", group: "その他のメッセージ",
		rows: []ccRow{{ref: "5.8", dir: ccBoth, mandatory: true, min: 2, max: 5}, {ref: "5.19", dir: ccBoth, mandatory: true, min: 3, max: 3}, rowDisp(ccDown)}},
}

var ccSetupTypes = map[uint8]bool{0x05: true, 0x15: true, 0x16: true, 0x17: true, 0x18: true}

var ccReleaseTypes = map[uint8]bool{0x45: true, 0x4D: true, 0x5A: true}

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
	"5.19": {0, 0x14, false, 0}, "5.20": {0, 0x1C, false, 0}, "5.21": {0, 0x7F, false, 0}, "5.22": {5, 0x82, true, 0xFF},
	"5.23": {6, 0x1F, false, 0}, "5.24": {6, 0x72, false, 0}, "5.25": {6, 0x6E, false, 0}, "5.26": {6, 0xF0, true, 0xF0},
	"5.27": {6, 0x0F, false, 0},
}

func (r ccRow) matches(ie *ccIE) bool {
	if r.ref == "5.2" {
		return ie.shiftTo == r.shift
	}
	id, ok := ccRefIdent[r.ref]
	if !ok || ie.shiftTo >= 0 || id.cs != ie.cs || id.single != ie.single {
		return false
	}
	if id.single {
		return ie.id&id.mask == id.id
	}
	return ie.id == id.id
}
