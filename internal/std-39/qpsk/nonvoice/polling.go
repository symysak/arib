package nonvoice

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/symysak/arib/internal/std-39/qpsk/l3"
)


type Control struct {
	CR   int
	Bits int
	Name string
	S    int
}

var controlDefs = []struct {
	name    string
	bits    int
	withS   bool
	command bool
	full    string
}{
	{"I", 0b00000, true, true, "Information（情報転送）"},
	{"RR", 0b10000, true, false, "Receive Ready（監視）"},
	{"SABMI", 0b11000, false, true, "Set ABM + info.（非番号制）"},
	{"DISC", 0b11001, false, true, "DISConnect（非番号制）"},
	{"UA", 0b11011, false, false, "Unnumbered Ack.（非番号制）"},
	{"UI", 0b11101, false, false, "Unnumbered + Info.（非番号制）"},
	{"INV", 0b11111, false, true, "INValid（非番号制）"},
}

func parseControl(b []uint8) Control {
	c := Control{CR: int(b[0]), Bits: bitsVal(b[1:6]), S: -1}
	for _, d := range controlDefs {
		v := c.Bits
		if d.withS {
			v &^= 0b00100
		}
		if v == d.bits {
			c.Name = d.name
			if d.withS {
				c.S = c.Bits >> 2 & 1
			}
			return c
		}
	}
	return c
}

func controlDef(name string) (full string, command bool) {
	for _, d := range controlDefs {
		if d.name == name {
			return d.full, d.command
		}
	}
	return "", false
}

func (c Control) String() string {
	if c.Name == "" {
		return fmt.Sprintf("未定義(%05b)", c.Bits)
	}
	if c.S >= 0 {
		return fmt.Sprintf("%s S=%d", c.Name, c.S)
	}
	return c.Name
}

func (c Control) isCommand(dir int) bool {
	if dir == 1 {
		return c.CR == 1
	}
	return c.CR == 0
}

func controlBits(name string, s int, dir int) []uint8 {
	for _, d := range controlDefs {
		if d.name != name {
			continue
		}
		v := d.bits
		if d.withS && s == 1 {
			v |= 0b00100
		}
		cr := 0
		if d.command == (dir == 1) {
			cr = 1
		}
		return putBits([]uint8{uint8(cr)}, v, 5)
	}
	panic("nonvoice: 制御フィールドの名前が表に無い: " + name)
}

type PollW struct {
	First, Last bool
	Count       int
	W0          int
	Raw         int
}

func parsePollW(b []uint8) PollW {
	return PollW{First: b[0] == 1, Last: b[1] == 1, Count: bitsVal(b[2:4]), W0: bitsVal(b[4:8]), Raw: bitsVal(b)}
}

func (w PollW) String() string {
	n := fmt.Sprintf("%d 回目", w.Count+1)
	switch {
	case w.Raw == 0:
		return "メッセージ無し"
	case w.First && w.Last:
		return fmt.Sprintf("1 ユニット L=%d（%s）", w.W0, n)
	case w.First:
		return fmt.Sprintf("先頭 Wn=%d（%s）", w.W0, n)
	case w.Last:
		return fmt.Sprintf("最終 L=%d（%s）", w.W0, n)
	}
	return fmt.Sprintf("途中 Wn=%d（%s）", w.W0, n)
}

func pollWBits(w PollW) []uint8 {
	b := []uint8{b01(w.First), b01(w.Last)}
	b = putBits(b, w.Count, 2)
	return putBits(b, w.W0, 4)
}

func b01(v bool) uint8 {
	if v {
		return 1
	}
	return 0
}

type PollDown struct {
	P    int
	Rsv  int
	Ar   int
	Ctl  Control
	Dest int
	W    PollW
	Info []byte
	N    int
}

type PollUp struct {
	Src  int
	Ak   int
	MT   int
	Ctl  Control
	Info []byte
	N    int
}

func parsePollDown(b []uint8) PollDown {
	return PollDown{
		P: bitsVal(b[0:8]), Rsv: int(b[8]), Ar: int(b[9]), Ctl: parseControl(b[10:16]), Dest: bitsVal(b[16:24]),
		W: parsePollW(b[24:32]), Info: bytesOf(b[32:112]), N: bitsVal(b[112:119]),
	}
}

func parsePollUp(b []uint8) PollUp {
	return PollUp{Src: bitsVal(b[0:8]), Ak: int(b[8]), MT: int(b[9]), Ctl: parseControl(b[10:16]), Info: bytesOf(b[16:80]), N: bitsVal(b[80:84])}
}

func PollDownBits(p int, ar int, ctl string, s int, dest int, w PollW, info []byte) []uint8 {
	b := putBits(nil, p, 8)
	b = append(b, 0, uint8(ar&1))
	b = append(b, controlBits(ctl, s, 1)...)
	b = putBits(b, dest, 8)
	b = append(b, pollWBits(w)...)
	b = append(b, padBits(info, 80)...)
	return append(b, make([]uint8, 7)...)
}

func PollUpBits(src, ak, mt int, ctl string, s int, info []byte) []uint8 {
	b := putBits(nil, src, 8)
	b = append(b, uint8(ak&1), uint8(mt&1))
	b = append(b, controlBits(ctl, s, 2)...)
	b = append(b, padBits(info, 64)...)
	return append(b, make([]uint8, 4)...)
}

func padBits(info []byte, n int) []uint8 {
	b := bitsOf(info)
	if len(b) > n {
		panic("nonvoice: 情報が長すぎる")
	}
	return append(b, make([]uint8, n-len(b))...)
}

func AddrText(v int) string {
	switch {
	case v == 0:
		return "0（無し）"
	case v <= 150:
		return fmt.Sprintf("%d（個別）", v)
	case v <= 191:
		return fmt.Sprintf("%d（予備）", v)
	case v <= 254:
		return fmt.Sprintf("%d（グループ）", v)
	}
	return "255（グローバル）"
}


type DataInfo struct {
	User bool
	Ack  bool
	No   int
}

func parseDataInfo(b byte) DataInfo {
	return DataInfo{User: b&0x80 != 0, Ack: b&0x40 != 0, No: int(b & 0x3F)}
}

func (d DataInfo) fields() ([]l3.Field, []string) {
	var notes []string
	kind := "0（制御信号）"
	if d.User {
		kind = "1（ユーザ情報）"
	}
	ack := "0（ACK 否）"
	if d.Ack {
		ack = "1（ACK 要 = 最優先データ。移動局は「最優先データ受信」でメッセージ No を折り返す）"
	}
	no := fmt.Sprint(d.No)
	if !d.User {
		if d.Ack {
			notes = append(notes, "データ情報: 制御信号なのに ACK 要（§3.1.4.4.1.2: 制御信号は常に ACK 否）")
		}
		if d.No != 0 {
			notes = append(notes, "データ情報: 制御信号なのにメッセージ No が 0 でない（§3.1.4.4.1.3）")
		}
		no += "（無効）"
	} else if d.No == 0 {
		notes = append(notes, "データ情報: ユーザ情報のメッセージ No が 0（§3.1.4.4.1.3: 1〜63）")
	}
	return []l3.Field{{Name: "情報種別識別ビット", Value: kind}, {Name: "ACK 要否ビット", Value: ack}, {Name: "メッセージ No", Value: no}}, notes
}

func DataInfoByte(user, ack bool, no int) byte {
	v := byte(no & 0x3F)
	if user {
		v |= 0x80
	}
	if ack {
		v |= 0x40
	}
	return v
}


type pollGroup struct {
	units   map[int][]byte
	finalAt int
	final   []byte
	finalL  int
	last    [4]int
	gap     [4]bool
	lastC   int
	done    bool
	tFirst  float64
	counts  map[int]bool
}

func newPollGroup(t float64) *pollGroup {
	g := &pollGroup{units: map[int][]byte{}, finalAt: -1, tFirst: t, counts: map[int]bool{}, lastC: -1}
	for i := range g.last {
		g.last[i] = -1
	}
	return g
}

func (g *pollGroup) push(w PollW, info []byte) ([]byte, bool) {
	c := w.Count & 3
	g.counts[c] = true
	g.lastC = c
	pos := -1
	switch {
	case w.First:
		pos = 0
		g.gap[c] = false
	case w.Last:
		if g.last[c] >= 0 && !g.gap[c] {
			pos = g.last[c] + 1
		}
	default:
		if prev := g.last[c]; prev >= 0 {
			pos = prev + 1
			for pos%16 != w.W0 {
				pos++
			}
		} else {
			pos = w.W0
		}
	}
	if w.Last {
		g.final = append([]byte(nil), info...)
		g.finalL = w.W0
		if pos >= 0 {
			g.finalAt = pos
		}
		if w.First {
			g.finalAt = 0
		}
	} else if pos >= 0 {
		if _, ok := g.units[pos]; !ok {
			g.units[pos] = append([]byte(nil), info...)
		}
	}
	if pos >= 0 {
		g.last[c] = pos
		if !w.Last {
			g.gap[c] = false
		}
	}
	if g.done || g.final == nil || g.finalAt < 0 {
		return nil, false
	}
	var out []byte
	for i := 0; i < g.finalAt; i++ {
		u, ok := g.units[i]
		if !ok {
			return nil, false
		}
		out = append(out, u...)
	}
	l := min(max(g.finalL, 0), len(g.final))
	out = append(out, g.final[:l]...)
	g.done = true
	return out, true
}

func (g *pollGroup) markGap() {
	if g.lastC >= 0 {
		g.gap[g.lastC] = true
	}
}

type linkBuf struct {
	data   []byte
	lastS  int
	last   []byte
	units  int
	user   bool
	tFirst float64
	tLast  float64
	notes  []string
}

func trimPad(b []byte) []byte { return bytes.TrimRight(b, "\x00") }

func userText(b []byte) string {
	h := fmt.Sprintf("%X", b)
	if len(h) > 64 {
		h = h[:64] + "…"
	}
	ok := len(b) > 0
	for _, c := range trimPad(b) {
		if c < 0x20 || c >= 0x7F {
			ok = false
			break
		}
	}
	if ok {
		return fmt.Sprintf("%s「%s」", h, strings.TrimRight(string(b), "\x00"))
	}
	return h
}
