package server


import (
	"fmt"

	"github.com/symysak/arib/internal/nxdn/decoder"
	"github.com/symysak/arib/internal/nxdn/l1"
)

const l1RepeatEvery = 5

func (c *Channel) l1Row(f decoder.Frame, kd l1.Keyed, rd Reading, hasMsg bool) {
	key, info := c.link.FrameKey(kd.Parsed)
	if prev := c.lastL1; prev != nil && prev.key == key && !hasMsg {
		prev.Repeat++
		prev.TSec = f.TimeSec
		if prev.Repeat%l1RepeatEvery == 0 {
			r := *prev
			c.sink.Emit(Event{Type: EvFrame, Frame: &r})
		}
		return
	}
	why := "変化"
	if hasMsg {
		why = "メッセージ"
	}
	if f.First {
		why = "捕捉"
	}
	c.l1No++
	r := &FrameRec{No: int64(c.cfg.ID)<<40 | c.l1No, Idx: c.cfg.ID, TSec: f.TimeSec, Frame: f.No, Kind: rd.Kind,
		Info: info, Why: why, Repeat: 1, Tree: c.link.Tree(f, kd), RawHex: RawHex(kd), key: key}
	c.lastL1 = r
	cp := *r
	c.sink.Emit(Event{Type: EvFrame, Frame: &cp})
}

func RawHex(kd l1.Keyed) string {
	bits := make([]uint8, 0, l1.FrameBits)
	for i := 19; i >= 0; i-- {
		bits = append(bits, uint8(uint32(0xCDF59)>>i&1))
	}
	bits = append(bits, l1.Hard(kd.Parsed.Soft)...)
	return fmt.Sprintf("%x", PackBits(bits))
}

func PackBits(b []uint8) []byte {
	out := make([]byte, (len(b)+7)/8)
	for i, x := range b {
		out[i/8] |= (x & 1) << (7 - i%8)
	}
	return out
}


func SyncNode(f decoder.Frame) Node {
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
	return sw
}

func OKText(ok bool, fixed int) string {
	if ok {
		return fmt.Sprintf("一致（直した %d ビット）", fixed)
	}
	return fmt.Sprintf("不一致（直した %d ビット）", fixed)
}

func BitStr(b []uint8) string {
	out := make([]byte, len(b))
	for i, x := range b {
		out[i] = '0' + x&1
	}
	return string(out)
}
