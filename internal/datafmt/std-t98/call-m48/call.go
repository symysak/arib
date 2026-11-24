package callm48

import (
	"fmt"

	"github.com/symysak/arib/internal/sjis"
	"github.com/symysak/arib/internal/std-t98/fsk4/l3"
)

const (
	Name  = "call-m48"
	Maker = 48
)

func init() {
	l3.RegisterMaker(l3.MakerFormat{Maker: Maker, Name: "FRC", Format: Name, New: func() l3.MakerReader { return &reader{} }})
}

var StatusText = map[int]string{
	1: "緊急", 2: "出発", 3: "到着", 4: "待機", 5: "集合", 6: "要連絡", 7: "休憩", 8: "問題発生", 9: "解決", 10: "警備中",
	11: "警戒中", 12: "作業中", 13: "巡回中", 14: "走行中", 15: "停止中", 16: "接客中", 17: "帰社", 18: "移動中", 19: "確認中", 20: "対応済",
}

type reader struct {
	name [5][]byte
	last string
}

func kindText(k byte, dst int) string {
	switch {
	case dst == 0xFFFF:
		return "全体"
	case k == 0x80 || k == 0x88:
		return "個別"
	case k == 0x20 || k == 0x30:
		return "グループ"
	}
	return ""
}

func header(o []byte, oct6Bits int) (l3.MakerInfo, bool) {
	if o[0] != 0x01 || (o[1] != 0x40 && o[1] != 0x00) {
		return l3.MakerInfo{}, false
	}
	src, dst := int(o[2])<<8|int(o[3]), int(o[4])<<8|int(o[5])
	kind := kindText(o[6], dst)
	if kind == "" {
		return l3.MakerInfo{}, false
	}
	m := l3.MakerInfo{What: "選択呼出のヘッダ", Kind: kind, Src: src, Dst: dst, Status: -1}
	cipher := "通常"
	if o[1] == 0x40 {
		cipher = "秘話"
	}
	dstText := fmt.Sprintf("%d", dst)
	if dst == 0xFFFF {
		dstText = "0xFFFF（全局）"
	}
	m.Rows = []l3.Row{
		{Name: "種類（オクテット 0）", Text: "0x01（呼のヘッダ）"},
		{Name: "秘話（オクテット 1）", Text: fmt.Sprintf("0x%02X（%s）", o[1], cipher)},
		{Name: "送信元 ID（オクテット 2〜3）", Text: fmt.Sprintf("%d", src)},
		{Name: "宛先 ID（オクテット 4〜5）", Text: dstText},
		{Name: fmt.Sprintf("呼の種類（オクテット 6。上 %d ビット）", oct6Bits), Text: fmt.Sprintf("0x%02X（%s）", o[6], kind)},
	}
	m.Summary = "選択呼出 " + m.CallText()
	return m, true
}

func (r *reader) namePart(o []byte) (l3.MakerInfo, bool) {
	if o[0] != 0x3F || o[1] != 0x82 || o[6]&0x80 == 0 {
		return l3.MakerInfo{}, false
	}
	n := int(o[6]&0x1F) >> 2
	if n < 1 || n > 4 {
		return l3.MakerInfo{}, false
	}
	r.name[n] = append([]byte(nil), o[2:6]...)
	m := l3.MakerInfo{What: "自局名の部品", Src: -1, Dst: -1, Status: -1,
		Rows:    []l3.Row{{Name: "番号", Text: fmt.Sprintf("%d / 4", n)}, {Name: "中身", Text: fmt.Sprintf("% X", o[2:6])}},
		Summary: fmt.Sprintf("自局名の部品 %d/4", n)}
	if r.name[1] == nil || r.name[2] == nil || r.name[3] == nil {
		return m, true
	}
	raw := append(append(append([]byte(nil), r.name[1]...), r.name[2]...), r.name[3]...)
	sum := 0
	for _, x := range raw {
		sum += int(x)
	}
	sum &= 0xFFFF
	check := "和の部品はまだ"
	if p := r.name[4]; p != nil {
		if int(p[0])<<8|int(p[1]) == sum || int(p[2])<<8|int(p[3]) == sum {
			check = "和が合う"
		} else {
			r.name = [5][]byte{}
			m.Rows = append(m.Rows, l3.Row{Name: "検査", Text: fmt.Sprintf("和が合わない（計算 0x%04X）。部品を捨てた", sum)})
			return m, true
		}
	}
	name, _ := sjis.Decode(raw)
	m.What, m.Name = "自局名", name
	m.Summary = "自局名 " + name
	m.Rows = append(m.Rows, l3.Row{Name: "自局名（Shift_JIS 12 バイト）", Text: name}, l3.Row{Name: "検査", Text: check})
	if check == "和が合う" && name != r.last {
		m.Event, r.last = true, name
	}
	return m, true
}

func (r *reader) TxStart() { r.name = [5][]byte{} }

func (r *reader) SACCH(msg l3.Message) (l3.MakerInfo, bool) {
	if msg.Maker != Maker || len(msg.MakerDef) < 54 {
		return l3.MakerInfo{}, false
	}
	o := make([]byte, 7)
	for i := 0; i < 54; i++ {
		o[i/8] |= msg.MakerDef[i] & 1 << (7 - i%8)
	}
	if m, ok := header(o, 6); ok {
		return m, true
	}
	return r.namePart(o)
}

func (r *reader) NV2(b [10]byte, ctx l3.MakerCtx) (l3.MakerInfo, bool) {
	if ctx.Maker != Maker {
		return l3.MakerInfo{}, false
	}
	if m, ok := header(b[:7], 8); ok {
		m.What = "選択呼出のヘッダ（非音声 2）"
		return m, true
	}
	if m, ok := r.namePart(b[:]); ok {
		return m, true
	}
	return data(b)
}

func data(b [10]byte) (l3.MakerInfo, bool) {
	src, dst := int(b[3])<<8|int(b[4]), int(b[5])<<8|int(b[6])
	kind := kindText(b[2], dst)
	m := l3.MakerInfo{Kind: kind, Src: src, Dst: dst, Status: -1}
	dstText := fmt.Sprintf("%d", dst)
	if dst == 0xFFFF {
		dstText = "0xFFFF（全局）"
	}
	ids := []l3.Row{
		{Name: "呼の種類（オクテット 2）", Text: fmt.Sprintf("0x%02X（%s）", b[2], kind)},
		{Name: "送信元 ID（オクテット 3〜4）", Text: fmt.Sprintf("%d", src)},
		{Name: "宛先 ID（オクテット 5〜6）", Text: dstText},
	}
	switch b[0] {
	case 0x32:
		m.What, m.Status, m.Event = "ステータス", int(b[8]), true
		m.StatusText = StatusText[m.Status]
		txt := fmt.Sprintf("%d", m.Status)
		if m.StatusText != "" {
			txt += "（既定の文言「" + m.StatusText + "」。各機で書き換えられる）"
		}
		m.Rows = append(append([]l3.Row{{Name: "種類（オクテット 0）", Text: "0x32（ステータス）"}}, ids...), l3.Row{Name: "番号（オクテット 8）", Text: txt})
		m.Summary = fmt.Sprintf("ステータス %d", m.Status)
		if m.StatusText != "" {
			m.Summary += "「" + m.StatusText + "」"
		}
		m.Summary += " " + m.CallText()
	case 0x33:
		m.What = "応答（推定）"
		m.Rows = append([]l3.Row{{Name: "種類（オクテット 0）", Text: "0x33（応答とみる）"}}, ids...)
		m.Summary = "応答 " + m.CallText()
	case 0x0F:
		m.What = "データの前置き（推定）"
		m.Rows = append(append([]l3.Row{{Name: "種類（オクテット 0）", Text: "0x0F（前置きの繰り返しとみる）"}}, ids...),
			l3.Row{Name: "残りの枚数（オクテット 8）", Text: fmt.Sprintf("%d", b[8])})
		m.Summary = fmt.Sprintf("データの前置き（残り %d）%s", b[8], " "+m.CallText())
	default:
		return l3.MakerInfo{}, false
	}
	if kind == "" {
		return l3.MakerInfo{}, false
	}
	return m, true
}

func (r *reader) Plain(b [10]byte, ctx l3.MakerCtx) bool {
	if ctx.Maker != Maker || b[0] != 0x01 || b[1] != 0x40 || b[7] != 0 || b[8] != 0 || b[9] != 0 {
		return false
	}
	return kindText(b[6], int(b[4])<<8|int(b[5])) != ""
}
