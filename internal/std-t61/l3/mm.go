package l3

import "fmt"


var mmNames = map[uint8]string{
	0x21: "位置登録要求", 0x22: "位置登録受付", 0x23: "位置登録拒否", 0x24: "位置登録要求1",
	0x25: "位置登録応答1", 0x26: "位置登録応答2", 0x27: "位置登録応答3", 0x28: "位置登録応答4",
	0x41: "グループ設定変更通知", 0x42: "グループ設定変更OK", 0x43: "グループ設定変更NG",
}

func parseMM(b []byte) Message {
	m := Message{Entity: "MM", Raw: b}
	if len(b) == 0 {
		m.Err = "空"
		return m
	}
	m.Type = b[0]
	m.Name = mmNames[m.Type]
	if m.Name == "" {
		m.Name = fmt.Sprintf("予約(0x%02X)", m.Type)
	}
	r := &reader{b: b, i: 1}
	add := func(f Field, ok bool) {
		if ok {
			m.Fields = append(m.Fields, f)
		}
	}
	switch m.Type {
	case 0x21:
		add(numberIE(r, "登録番号"))
	case 0x23:
		add(mmCause(r, "理由表示(位置登録)"))
	case 0x24:
		add(numberIE(r, "登録番号"))
		add(numberIE(r, "設定グループ番号"))
	case 0x26, 0x43:
		add(mmCause(r, "理由表示(グループ設定変更)"))
	case 0x27:
		add(mmCause(r, "理由表示(位置登録)"))
	case 0x28:
		add(mmCause(r, "理由表示(位置登録)"))
		add(mmCause(r, "理由表示(グループ設定変更)"))
	case 0x41:
		add(numberIE(r, "設定グループ番号"))
	}
	if r.err != "" {
		m.Err = r.err
	} else if r.left() > 0 {
		m.Fields = append(m.Fields, Field{"余剰", hexOf(r.b[r.i:])})
	}
	return m
}

func mmCause(r *reader, name string) (Field, bool) {
	v, ok := r.byte1(name)
	if !ok {
		return Field{}, false
	}
	names := map[byte]string{
		0x01: "位置登録失敗", 0x02: "グループ設定変更失敗", 0x0B: "管理番号NG", 0x0F: "その他の正常クラス",
		0x26: "網障害", 0x29: "一時的障害", 0x2F: "その他のリソース利用不可",
		0x60: "必須情報要素不足", 0x64: "情報要素内容無効", 0x6F: "その他の手順誤り",
	}
	n := names[v]
	if n == "" {
		n = fmt.Sprintf("予約(0x%02X)", v)
	}
	return Field{name, n}, true
}
