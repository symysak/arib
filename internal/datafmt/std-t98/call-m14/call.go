package callm14

import (
	"fmt"

	"github.com/symysak/arib/internal/std-t98/fsk4/l3"
)

const (
	Name  = "call-m14"
	Maker = 14
)

func init() {
	l3.RegisterMaker(l3.MakerFormat{Maker: Maker, Name: "アイコム", Format: Name, New: func() l3.MakerReader { return reader{} }})
}

type reader struct{}

func typeOK(t uint8) bool { return l3.IsMakerDef(t) && t != 0x06 }

func read(b []uint8) l3.MakerInfo {
	ct := l3.BitsOf(b, 20, 3)
	src, dst := l3.BitsOf(b, 24, 16), l3.BitsOf(b, 40, 16)
	kind := "グループ"
	switch {
	case dst == 0xFFFF:
		kind = "全体"
	case ct == 4:
		kind = "個別"
	}
	m := l3.MakerInfo{What: "選択呼出のヘッダ", Kind: kind, Src: src, Dst: dst, Status: -1}
	dstText := fmt.Sprintf("%d", dst)
	if dst == 0xFFFF {
		dstText = "0xFFFF（全体）"
	}
	m.Rows = []l3.Row{
		{Name: "通話種別（ビット 20–22）", Bits: bits(b, 20, 3), Text: fmt.Sprintf("%d（4 = 個別。宛先が全体なら全体呼出）", ct)},
		{Name: "ID 1 = 送信元（24–39）", Bits: bits(b, 24, 16), Text: fmt.Sprintf("%d", src)},
		{Name: "ID 2 = 宛先（40–55）", Bits: bits(b, 40, 16), Text: dstText},
	}
	if rest := l3.BitsOf(b, 56, 16); rest != 0 {
		m.Rows = append(m.Rows, l3.Row{Name: "残り 16 ビット（56–71）", Bits: bits(b, 56, 16), Text: fmt.Sprintf("0x%04X（いつもは 0）", rest)})
	}
	m.Summary = "選択呼出 " + m.CallText()
	return m
}

func (reader) SACCH(msg l3.Message) (l3.MakerInfo, bool) {
	if msg.Maker != Maker || !typeOK(msg.Type) || len(msg.MakerDef) < 38 {
		return l3.MakerInfo{}, false
	}
	return read(msg.MakerDefBits()), true
}

func nv2(b [10]byte) (head uint8, msg []uint8, ok bool) {
	head = b[0]
	msg = make([]uint8, 72)
	for i := range msg {
		msg[i] = uint8(l3.Bits(b[1:], i, 1))
	}
	ok = head>>7 == 1 && typeOK(head&0x1F) && l3.BitsOf(msg, 11, 7) == Maker
	return
}

func (reader) NV2(b [10]byte, ctx l3.MakerCtx) (l3.MakerInfo, bool) {
	_, msg, ok := nv2(b)
	if !ok || l3.BitsOf(msg, 0, 2) > l3.CallCipher {
		return l3.MakerInfo{}, false
	}
	m := read(msg)
	m.What = "選択呼出のヘッダ（通話の頭の非音声 2）"
	m.Rows = append([]l3.Row{
		{Name: "M オクテット", Text: fmt.Sprintf("0x%02X（先頭・%s）", b[0], l3.TypeName(b[0]&0x1F))},
		{Name: "通話識別・UC・製造者番号", Text: fmt.Sprintf("%s・UC %d・製造者 %d", l3.CallName(l3.BitsOf(msg, 0, 2)), l3.BitsOf(msg, 2, 9), Maker)},
	}, m.Rows...)
	return m, true
}

func (reader) TxStart() {}

func (reader) Plain(b [10]byte, ctx l3.MakerCtx) bool {
	_, msg, ok := nv2(b)
	if !ok || l3.BitsOf(msg, 18, 2) != 0 || msg[23] != 0 || l3.BitsOf(msg, 56, 16) != 0 {
		return false
	}
	if ctx.Call >= 0 && l3.BitsOf(msg, 0, 2) != ctx.Call {
		return false
	}
	if ctx.UC >= 0 && l3.BitsOf(msg, 2, 9) != ctx.UC {
		return false
	}
	return true
}

func bits(b []uint8, at, n int) string {
	out := make([]byte, n)
	for i := range out {
		out[i] = '0' + b[at+i]&1
	}
	return string(out)
}
