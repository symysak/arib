package l3

import "fmt"


var mmNames = map[uint8]string{
	0x21: "位置登録要求",
	0x22: "位置登録受付",
	0x23: "位置登録拒否",
}

func parseMM(b []byte) Message {
	m := Message{Entity: "MM", Raw: b}
	r := &reader{b: b}
	t, ok := r.byte1("メッセージ種別")
	if !ok {
		m.Err = r.err
		return m
	}
	m.Type = t
	m.Name = mmNames[t]
	switch t {
	case 0x21:
		m.Fields = append(m.Fields, mmRegNumber(r)...)
	case 0x22:
	case 0x23:
		if c, ok := r.byte1("理由表示"); ok {
			m.Fields = append(m.Fields, Field{"理由表示", mmCause(c)})
		}
	default:
		m.Name = fmt.Sprintf("予約(0x%02X)", t)
		if t>>5 == 1 {
			m.Name = fmt.Sprintf("位置登録に関するメッセージ 予約(0x%02X)", t)
		}
		m.Err = fmt.Sprintf("未定義のメッセージ種別 0x%02X", t)
		rest := r.rest()
		if len(rest) > 0 {
			m.Fields = append(m.Fields, Field{"内容", hexOf(rest)})
		}
		m.vendorRaw(fmt.Sprintf("MM 種別 0x%02X", t), rest)
		return m
	}
	if r.err != "" {
		m.Err = r.err
		return m
	}
	if rest := r.rest(); len(rest) > 0 {
		m.Fields = append(m.Fields, Field{"後続（免許人拡張）", hexOf(rest)})
	}
	return m
}

var mmNumberTypes = []string{0: "不定", 2: "国内番号", 3: "免許人特有番号", 4: "市内番号", 6: "短縮番号", 7: "予約(1.1 版の応援通信)"}

func mmRegNumber(r *reader) []Field {
	n, ok := r.byte1("登録番号内容長")
	if !ok {
		return nil
	}
	body := r.take(int(n), "登録番号")
	if body == nil {
		body = r.b[r.i:]
		r.i = len(r.b)
	}
	if len(body) == 0 {
		return []Field{{"登録番号", "（内容長 0）"}}
	}
	o := body[0]
	odd := o&0x80 != 0
	typ := pick(mmNumberTypes, int(o>>4)&7, "予約")
	digits := bcdDigits(body[1:], odd, true)
	return []Field{
		{"登録番号", digits},
		{"番号種別", typ},
		{"桁数", fmt.Sprintf("%d（%s）", len(digits), map[bool]string{true: "奇数", false: "偶数"}[odd])},
	}
}

var mmCauseNames = map[int]string{
	0x01: "位置登録失敗",
	0x0B: "管理番号 NG",
	0x0F: "その他の正常クラス",
	0x26: "網障害",
	0x29: "一時的障害",
	0x2F: "その他のリソース利用不可クラス",
	0x60: "必須情報要素不足",
	0x64: "情報要素内容無効",
	0x6F: "その他の手順誤りクラス",
}

func mmCause(c byte) string {
	class := pickMap(map[int]string{0x0: "正常クラス", 0x2: "リソース使用不可クラス", 0x6: "手順誤りクラス"}, int(c>>4), "予約クラス")
	return fmt.Sprintf("%s（%s, 0x%02X）", pickMap(mmCauseNames, int(c), "予約"), class, c)
}
