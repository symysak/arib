package server


import (
	"fmt"
	"strings"

	"github.com/symysak/arib/internal/std-t102/decoder"
	"github.com/symysak/arib/internal/std-t102/l1"
	"github.com/symysak/arib/internal/vocoder/ambe"
)

const l1RepeatEvery = 5

func (c *Channel) l1Row(f decoder.Frame, kd l1.Keyed, msgs []MsgRec) {
	p := kd.Parsed
	key, info := frameKey(p)
	if prev := c.lastL1; prev != nil && prev.key == key && len(msgs) == 0 {
		prev.Repeat++
		prev.TSec = f.TimeSec
		if prev.Repeat%l1RepeatEvery == 0 {
			r := *prev
			c.sink.Emit(Event{Type: EvFrame, Frame: &r})
		}
		return
	}
	why := "変化"
	if len(msgs) > 0 {
		why = "メッセージ"
	}
	if f.First {
		why = "捕捉"
	}
	c.l1No++
	r := &FrameRec{No: int64(c.cfg.ID)<<40 | c.l1No, Idx: c.cfg.ID, TSec: f.TimeSec, Frame: f.No, Kind: p.Format.String(),
		Info: info, Why: why, Repeat: 1, Tree: frameTree(f, kd), RawHex: rawHex(kd), key: key}
	c.lastL1 = r
	cp := *r
	c.sink.Emit(Event{Type: EvFrame, Frame: &cp})
}

func frameKey(p l1.Parsed) (key, info string) {
	var k strings.Builder
	fmt.Fprintf(&k, "%02X", p.RICH.Raw)
	info = p.RICH.String()
	if s := p.SACCH; s != nil {
		fmt.Fprintf(&k, "/S%d U%d %v %v", s.S, s.U, s.CRCOK, s.Data)
		if s.CRCOK {
			info += fmt.Sprintf("・SACCH S=%02b U=%d", s.S, s.U)
		} else {
			info += "・SACCH CRC 誤り"
		}
	}
	for i, h := range p.Half {
		if h.FACCH1 != nil {
			fmt.Fprintf(&k, "/F%d %v %X", i, h.FACCH1.CRCOK, l1.FACCH1Bytes(h.FACCH1))
		}
	}
	if d := p.Data; d != nil {
		fmt.Fprintf(&k, "/D %v %X", d.CRCOK, d.Body)
		if d.CRCOK {
			info += fmt.Sprintf("・%s U=%d %X…", d.Kind, d.U, d.Body[:4])
		} else {
			info += "・" + d.Kind.String() + " CRC 誤り"
		}
	}
	return k.String(), info
}

func bitStr(b []uint8) string {
	var sb strings.Builder
	for _, x := range b {
		sb.WriteByte('0' + x&1)
	}
	return sb.String()
}

func okText(ok bool, fixed int) string {
	if ok {
		return fmt.Sprintf("一致（直した %d ビット）", fixed)
	}
	return fmt.Sprintf("不一致（直した %d ビット）", fixed)
}

func frameTree(f decoder.Frame, kd l1.Keyed) []Node {
	p := kd.Parsed
	r := p.RICH
	sw := Node{Label: "同期ワード", Value: l1.FSWHex, Range: "0〜19", Bits: "−3 +1 −3 +3 −3 −3 +3 +3 −1 +3",
		Children: []Node{
			{Label: "硬判定の誤り", Value: fmt.Sprintf("%d / 10 記号", f.FSWErr)},
			{Label: "±1 の偏位", Value: fmt.Sprintf("%.0f Hz（規格 315 Hz）", f.DevHz)},
			{Label: "直流（搬送波のずれ）", Value: fmt.Sprintf("%+.0f Hz", f.DCHz)},
			{Label: "EVM", Value: fmt.Sprintf("%.3f（±1 の尺度）", f.EVM)},
			{Label: "無線フレーム", Value: fmt.Sprintf("#%d", f.No)},
		}}
	if f.Gap > 0 {
		sw.Children = append(sw.Children, Node{Label: "抜け", Value: fmt.Sprintf("前のフレームとの間に %d 枚", f.Gap)})
	}
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
		out = append(out, Node{Label: "SACCH", Range: "36〜95", Value: okText(s.CRCOK, s.Fixed), Children: []Node{
			{Label: "S 構造識別", Bits: fmt.Sprintf("%02b", s.S), Value: sText(r, s.S)},
			{Label: "U ユーザコード", Bits: fmt.Sprintf("%06b", s.U), Value: fmt.Sprintf("%d", s.U)},
			{Label: "情報 18 ビット", Bits: bitStr(s.Data[:])},
			{Label: "CRC6", Value: okText(s.CRCOK, s.Fixed)},
		}})
	}
	for i, h := range p.Half {
		at := [2]string{"96〜239", "240〜383"}[i]
		name := [2]string{"前半", "後半"}[i]
		switch {
		case h.FACCH1 != nil:
			b := l1.FACCH1Bytes(h.FACCH1)
			out = append(out, Node{Label: name + " FACCH1", Range: at, Value: okText(h.FACCH1.CRCOK, h.FACCH1.Corrected),
				Children: []Node{{Label: "情報 80 ビット", Value: fmt.Sprintf("% X", b)}, {Label: "CRC12", Value: okText(h.FACCH1.CRCOK, h.FACCH1.Corrected)}}})
		case len(h.TCH) == l1.TCHBits:
			n := Node{Label: name + " TCH", Range: at, Value: "AMBE+2 3600x2450 × 2（第 5 章の推奨）"}
			for k := 0; k < 2; k++ {
				var a [ambe.Bits3600]uint8
				copy(a[:], h.TCH[72*k:72*k+72])
				n.Children = append(n.Children, Node{Label: fmt.Sprintf("20 ms #%d", k+1), Value: fmt.Sprintf("%X", packBits(a[:])),
					Note: map[bool]string{true: "C0・C1 が通る", false: "C0・C1 が通らない"}[ambe.Valid3600(a)]})
			}
			out = append(out, n)
		}
	}
	if d := p.Data; d != nil {
		out = append(out, Node{Label: d.Kind.String(), Range: "36〜383", Value: okText(d.CRCOK, d.Fixed), Children: []Node{
			{Label: "S（予備）", Bits: fmt.Sprintf("%02b", d.S)},
			{Label: "U ユーザコード", Bits: fmt.Sprintf("%06b", d.U), Value: fmt.Sprintf("%d", d.U)},
			{Label: "本体 22 オクテット", Value: fmt.Sprintf("% X", d.Body)},
			{Label: "CRC15", Value: okText(d.CRCOK, d.Fixed)},
		}})
	}
	return out
}

func optionText(r l1.RICH) string {
	switch r.Format() {
	case l1.FormatVoice:
		a, b := r.Halves()
		n := [2]string{"TCH", "FACCH1"}
		return fmt.Sprintf("前半 %s / 後半 %s", n[a], n[b])
	case l1.FormatData, l1.FormatBurst:
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

func rawHex(kd l1.Keyed) string {
	bits := make([]uint8, 0, l1.FrameBits)
	for i := 19; i >= 0; i-- {
		bits = append(bits, uint8(uint32(0xCDF59)>>i&1))
	}
	bits = append(bits, l1.Hard(kd.Parsed.Soft)...)
	return fmt.Sprintf("%x", packBits(bits))
}

func packBits(b []uint8) []byte {
	out := make([]byte, (len(b)+7)/8)
	for i, x := range b {
		out[i/8] |= (x & 1) << (7 - i%8)
	}
	return out
}
