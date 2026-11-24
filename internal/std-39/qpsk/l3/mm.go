package l3

import "fmt"


const (
	CodeMMRegRequest uint8 = 0x21
	CodeMMRegAck     uint8 = 0x22
	CodeMMRegReject  uint8 = 0x23
)

type mmSpec struct {
	name, en string
	dir      int
	table    string
}

var mmSpecs = map[uint8]mmSpec{
	CodeMMRegRequest: {"位置登録要求", "Location Registration Request", 2, "表 4.3.6.2-2"},
	CodeMMRegAck:     {"位置登録受付", "Location Registration Acknowledgement", 1, "表 4.3.6.2-3"},
	CodeMMRegReject:  {"位置登録拒否", "Location Registration Reject", 1, "表 4.3.6.2-4"},
}

func MMName(t uint8) string {
	if s, ok := mmSpecs[t]; ok {
		return s.name
	}
	if t>>5 == 1 {
		return fmt.Sprintf("位置登録に関するメッセージ 予約(0x%02X)", t)
	}
	return fmt.Sprintf("予約(0x%02X)", t)
}

var mmDirNames = map[int]string{1: "下り（基地局→移動局）", 2: "上り（移動局→基地局）"}

func parseMM(b []byte, c Ctx) Message {
	m := Message{Entity: "MM", Raw: append([]byte(nil), b...)}
	r := &reader{b: b}
	t, ok := r.byte1("メッセージ種別")
	if !ok {
		m.Err = r.err
		return m
	}
	m.Type = t
	m.Name = MMName(t)
	spec, known := mmSpecs[t]
	if !known {
		m.Err = fmt.Sprintf("未定義のメッセージ種別 0x%02X", t)
		m.Notes = append(m.Notes, fmt.Sprintf("メッセージ種別 0x%02X は図 4.3.6.3-2 に無い（予約）", t))
		m.Fields = append(m.Fields, Field{"メッセージ種別", fmt.Sprintf("%s (0x%02X)", m.Name, t)})
		if rest := r.rest(); len(rest) > 0 {
			m.Fields = append(m.Fields, Field{"内容", hexOf(rest)})
		}
		return m
	}
	m.Fields = append(m.Fields, Field{"メッセージ種別", fmt.Sprintf("%s [%s] (0x%02X)", spec.name, spec.en, t)})
	if c.Dir != 0 && c.Dir != spec.dir {
		m.Notes = append(m.Notes, fmt.Sprintf("%sは%sのメッセージだが%sで受けた（%s）",
			spec.name, mmDirNames[spec.dir], mmDirNames[c.Dir], spec.table))
	}
	loc := &Location{}
	switch t {
	case CodeMMRegRequest:
		if r.left() == 0 {
			m.Notes = append(m.Notes, "必須の情報要素「登録番号」が無い（"+spec.table+"）")
			m.Err = "登録番号 が足りない"
			break
		}
		fs, num, notes := mmRegNumber(r)
		m.Fields = append(m.Fields, fs...)
		m.Notes = append(m.Notes, notes...)
		loc.Number = num
		loc.Text = "位置登録要求 登録番号 " + num
		if num == "" {
			loc.Text = "位置登録要求（登録番号なし）"
		}
	case CodeMMRegAck:
		loc.Text = "位置登録受付"
	case CodeMMRegReject:
		cb, ok := r.byte1("理由表示")
		if !ok {
			m.Notes = append(m.Notes, "必須の情報要素「理由表示」が無い（"+spec.table+"）")
			loc.Text = "位置登録拒否"
			break
		}
		fs, name, notes := mmCause(cb)
		m.Fields = append(m.Fields, fs...)
		m.Notes = append(m.Notes, notes...)
		loc.Cause = name
		loc.Text = "位置登録拒否: " + name
	}
	m.MM = loc
	if r.err != "" {
		m.Err = r.err
		return m
	}
	if rest := r.rest(); len(rest) > 0 {
		m.Fields = append(m.Fields, Field{"後続（拡張・§4.3.3.3）", hexOf(rest)})
	}
	return m
}

func mmRegNumber(r *reader) (fs []Field, number string, notes []string) {
	n, ok := r.byte1("登録番号内容長")
	if !ok {
		return nil, "", nil
	}
	fs = append(fs, Field{"登録番号内容長", fmt.Sprintf("%d", n)})
	if n > 13 {
		notes = append(notes, fmt.Sprintf("登録番号内容長 %d が範囲 0〜13 を超える（§4.3.6.3.3.2 (1)）", n))
	}
	body := r.take(int(n), "登録番号")
	if body == nil {
		body = r.b[r.i:]
		r.i = len(r.b)
	}
	if len(body) == 0 {
		fs = append(fs, Field{"登録番号", "（内容長 0・番号なし）"})
		return fs, "", notes
	}
	odd := body[0]&0x80 != 0
	fs = append(fs, Field{"奇数／偶数表示", map[bool]string{true: "奇数（1）", false: "偶数（0）"}[odd]})
	if sp := body[0] & 0x7F; sp != 0 {
		fs = append(fs, Field{"予備（オクテット 2 のビット 7〜1）", fmt.Sprintf("0x%02X", sp)})
	}
	digits, dn := mmDigits(body[1:], odd)
	notes = append(notes, dn...)
	fs = append(fs, Field{"登録番号", digits})
	fs = append(fs, Field{"桁数", fmt.Sprintf("%d", len(digits))})
	return fs, digits, notes
}

func mmDigits(b []byte, odd bool) (string, []string) {
	var out []byte
	var notes []string
	badReserved, badFiller := false, false
	for i, o := range b {
		for k, nib := range []byte{o & 0xF, o >> 4} {
			last := i == len(b)-1 && k == 1
			switch {
			case last && odd:
				if nib != 0 {
					notes = append(notes, fmt.Sprintf("奇数表示だが最後のフィラーが 0000 でない（0x%X。§4.3.6.3.3.2）", nib))
				}
				continue
			case nib == 0:
				if last {
					notes = append(notes, "偶数表示だが最後の数字がフィラー（0000）。奇数／偶数表示と桁数が合わない（§4.3.6.3.3.2 (2)）")
				} else {
					badFiller = true
				}
				continue
			case nib == 0xA:
				out = append(out, '0')
			case nib >= 1 && nib <= 9:
				out = append(out, '0'+nib)
			case nib == 0xB:
				out = append(out, '*')
			case nib == 0xC:
				out = append(out, '#')
			default:
				out = append(out, '?')
				badReserved = true
			}
		}
	}
	if badFiller {
		notes = append(notes, "番号ディジットの途中にフィラー（0000）がある（§4.3.6.3.3.2 (3)）")
	}
	if badReserved {
		notes = append(notes, "番号ディジットに予約の値（1101〜1111）がある（§4.3.6.3.3.2 (3)。? で表示）")
	}
	return string(out), notes
}

var mmCauseNames = map[byte][2]string{
	0x01: {"位置登録失敗", "基地局側の何らかの理由で位置登録に失敗。移動局はそのゾーンに停まり、そのゾーン内では位置登録を再起動しない"},
	0x0B: {"管理番号 NG", ""},
	0x0F: {"その他の正常クラス", "移動局登録時の何らかの理由で登録が正常に行われない（例: 着信登録移動機の登録数制限・禁止）"},
	0x26: {"網障害", "網内の障害で正常に処理できず、短時間に復旧が見込めない"},
	0x29: {"一時的障害", "基地局内の障害で正常に処理できず、短時間に復旧が見込める"},
	0x2F: {"その他のリソース利用不可クラス", "上記以外のリソース使用不可エラー"},
	0x60: {"必須情報要素不足", "情報要素の内容が不足している"},
	0x64: {"情報要素内容無効", "情報要素の内容を理解できない"},
	0x6F: {"その他の手順誤りクラス", "上記以外の手順誤りエラー"},
}

var mmCauseClasses = map[int]string{0x0: "正常クラス", 0x2: "リソース使用不可クラス", 0x6: "手順誤りクラス"}

func mmCause(c byte) (fs []Field, name string, notes []string) {
	class := pickMap(mmCauseClasses, int(c>>4), "予約クラス")
	e, ok := mmCauseNames[c]
	name = e[0]
	if !ok {
		name = fmt.Sprintf("予約(0x%02X)", c)
		notes = append(notes, fmt.Sprintf("理由表示 0x%02X は図 4.3.6.3-4 に無い（予約）", c))
	}
	fs = append(fs, Field{"理由表示", fmt.Sprintf("%s (0x%02X)", name, c)})
	fs = append(fs, Field{"理由表示のクラス", class})
	if e[1] != "" {
		fs = append(fs, Field{"理由表示の意味", e[1]})
	}
	return fs, name, notes
}
