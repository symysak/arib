package server


import (
	"fmt"
	"strings"

	"github.com/symysak/arib/internal/nxdn/decoder"
	nxl1 "github.com/symysak/arib/internal/nxdn/l1"
	nx "github.com/symysak/arib/internal/nxdn/server"
	"github.com/symysak/arib/internal/std-t102/l1"
	"github.com/symysak/arib/internal/vocoder/ambe"
)

func (k *link) FrameKey(p nxl1.Parsed) (key, info string) { return frameKey(p) }

func (k *link) Tree(f decoder.Frame, kd nxl1.Keyed) []Node { return frameTree(f, kd) }

func frameKey(p nxl1.Parsed) (key, info string) {
	var k strings.Builder
	r := l1.ReadRICH(p.RICH)
	fmt.Fprintf(&k, "%02X", p.RICH)
	info = r.String()
	if s := p.SACCH; s != nil {
		ss, u := l1.SU(s.Head)
		fmt.Fprintf(&k, "/S%d U%d %v %v", ss, u, s.CRCOK, s.Data)
		if s.CRCOK {
			info += fmt.Sprintf("・SACCH S=%02b U=%d", ss, u)
		} else {
			info += "・SACCH CRC 誤り"
		}
	}
	for i, h := range p.Half {
		if h.Coded != nil {
			fmt.Fprintf(&k, "/F%d %v %X", i, h.Coded.CRCOK, nxl1.CodedBytes(h.Coded))
		}
	}
	if d := p.Data; d != nil {
		fmt.Fprintf(&k, "/D %v %X", d.CRCOK, d.Body)
		_, u := l1.SU(d.Head)
		if d.CRCOK {
			info += fmt.Sprintf("・%s U=%d %X…", r.DataKind(), u, d.Body[:4])
		} else {
			info += "・" + r.DataKind().String() + " CRC 誤り"
		}
	}
	return k.String(), info
}

func frameTree(f decoder.Frame, kd nxl1.Keyed) []Node {
	p := kd.Parsed
	r := l1.ReadRICH(p.RICH)
	sw := nx.SyncNode(f)
	wh := Node{Label: "ホワイトニング", Value: fmt.Sprintf("PN(9,5) 初期値 0x%03X", kd.Init), Range: "20〜383",
		Note: "§4.1.13。RICH から後ろの記号に ±1 を掛ける"}
	raw := r.Raw
	rich := Node{Label: "RICH", Value: r.String(), Bits: fmt.Sprintf("%08b", raw), Range: "20〜35", Children: []Node{
		{Label: "F 無線チャネル構造", Bits: fmt.Sprintf("%02b", r.F), Value: map[bool]string{true: "直接通信チャネル", false: "予約"}[r.F == 2]},
		{Label: "M 通信モード", Bits: fmt.Sprintf("%02b", r.M), Value: [4]string{"音声（SACCH 非スーパーフレーム）", "データ", "音声（SACCH スーパーフレーム）", "データ（バースト動作）"}[r.M]},
		{Label: "O オプション", Bits: fmt.Sprintf("%02b", r.O), Value: optionText(r)},
		{Label: "D 方向", Bits: fmt.Sprintf("%d", r.D), Value: map[uint8]string{0: "上り信号・移動局間直接通信", 1: "下り信号（基地局経由）"}[r.D]},
		{Label: "P 偶数パリティ（F・M）", Bits: fmt.Sprintf("%d", r.P), Value: map[bool]string{true: "一致", false: "不一致"}[r.ParityOK()]},
	}}
	out := []Node{sw, wh, rich}
	if s := p.SACCH; s != nil {
		ss, u := l1.SU(s.Head)
		out = append(out, Node{Label: "SACCH", Range: "36〜95", Value: nx.OKText(s.CRCOK, s.Fixed), Children: []Node{
			{Label: "S 構造識別", Bits: fmt.Sprintf("%02b", ss), Value: sText(r, ss)},
			{Label: "U ユーザコード", Bits: fmt.Sprintf("%06b", u), Value: fmt.Sprintf("%d", u)},
			{Label: "情報 18 ビット", Bits: nx.BitStr(s.Data[:])},
			{Label: "CRC6", Value: nx.OKText(s.CRCOK, s.Fixed)},
		}})
	}
	for i, h := range p.Half {
		at := [2]string{"96〜239", "240〜383"}[i]
		name := [2]string{"前半", "後半"}[i]
		switch {
		case h.Coded != nil:
			b := nxl1.CodedBytes(h.Coded)
			out = append(out, Node{Label: name + " FACCH1", Range: at, Value: nx.OKText(h.Coded.CRCOK, h.Coded.Corrected),
				Children: []Node{{Label: "情報 80 ビット", Value: fmt.Sprintf("% X", b)}, {Label: "CRC12", Value: nx.OKText(h.Coded.CRCOK, h.Coded.Corrected)}}})
		case len(h.TCH) == l1.TCHBits:
			n := Node{Label: name + " TCH", Range: at, Value: "AMBE+2 3600x2450 × 2（第 5 章の推奨）"}
			for k := 0; k < 2; k++ {
				var a [ambe.Bits3600]uint8
				copy(a[:], h.TCH[72*k:72*k+72])
				n.Children = append(n.Children, Node{Label: fmt.Sprintf("20 ms #%d", k+1), Value: fmt.Sprintf("%X", nx.PackBits(a[:])),
					Note: map[bool]string{true: "C0・C1 が通る", false: "C0・C1 が通らない"}[ambe.Valid3600(a)]})
			}
			out = append(out, n)
		}
	}
	if d := p.Data; d != nil {
		ss, u := l1.SU(d.Head)
		out = append(out, Node{Label: r.DataKind().String(), Range: "36〜383", Value: nx.OKText(d.CRCOK, d.Fixed), Children: []Node{
			{Label: "S（予備）", Bits: fmt.Sprintf("%02b", ss)},
			{Label: "U ユーザコード", Bits: fmt.Sprintf("%06b", u), Value: fmt.Sprintf("%d", u)},
			{Label: "本体 22 オクテット", Value: fmt.Sprintf("% X", d.Body)},
			{Label: "CRC15", Value: nx.OKText(d.CRCOK, d.Fixed)},
		}})
	}
	return out
}

func optionText(r l1.RICH) string {
	switch r.Format() {
	case nxl1.FormatVoice:
		a, b := r.Halves()
		n := map[nxl1.Half]string{nxl1.HalfTCH: "TCH", nxl1.HalfCoded: "FACCH1"}
		return fmt.Sprintf("前半 %s / 後半 %s", n[a], n[b])
	case nxl1.FormatData, nxl1.FormatBurst:
		return r.DataKind().String()
	}
	return "—"
}

func sText(r l1.RICH, s uint8) string {
	if !r.Superframe() {
		return "1 フレーム構造"
	}
	return fmt.Sprintf("スーパーフレームの %d / 4", 4-int(s))
}
