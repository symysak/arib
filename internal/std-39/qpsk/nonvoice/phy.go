package nonvoice

import (
	"github.com/symysak/arib/internal/rcr39qpsk/l1"
)


type Format int

const (
	FmtNone     Format = iota
	FmtPollDown
	FmtPollUp
	FmtContDown
	FmtContUp1
	FmtContUpN
)

type geom struct {
	rg         [][2]int
	rows, cols int
	info       int
	name       string
}

var geoms = map[Format]geom{
	FmtPollDown: {[][2]int{{8, 120}, {148, 316}}, 20, 14, 119, "付録 1 下り"},
	FmtPollUp:   {[][2]int{{58, 156}, {184, 296}}, 15, 14, 84, "付録 1 上り"},
	FmtContDown: {[][2]int{{8, 120}, {148, 316}}, 20, 14, 119, "付録 2 下り"},
	FmtContUp1:  {[][2]int{{98, 156}, {184, 296}}, 17, 10, 64, "付録 2 上り第 1 ユニット"},
	FmtContUpN:  {[][2]int{{44, 156}, {184, 296}}, 16, 14, 91, "付録 2 上り第 2 ユニット以降"},
}

func (f Format) String() string {
	if g, ok := geoms[f]; ok {
		return g.name
	}
	return "-"
}

func (f Format) InfoBits() int { return geoms[f].info }

func (f Format) Up() bool { return f == FmtPollUp || f == FmtContUp1 || f == FmtContUpN }

type Field struct {
	Format    Format
	Bits      []uint8
	CRC       uint16
	OK        bool
	Corrected int
	Init      int
}

func DecodeField(f Format, slot []float32, init int) Field {
	g, ok := geoms[f]
	if !ok || len(slot) < l1.SlotBits {
		return Field{Format: f, Init: init}
	}
	var v []float32
	for _, r := range g.rg {
		v = append(v, slot[r[0]:r[1]]...)
	}
	for i, b := range l1.PN(init, len(v)) {
		if b == 1 {
			v[i] = -v[i]
		}
	}
	n := g.rows * g.cols
	y := make([]float32, n)
	for i := 0; i < n; i++ {
		y[(i%g.rows)*g.cols+i/g.rows] = v[i]
	}
	d, errs := l1.Viterbi(y)
	fd := Field{Format: f, Bits: d[:g.info], Corrected: errs, Init: init}
	fd.CRC = uint16(bitsVal(d[g.info : g.info+16]))
	fd.OK = l1.CRC16(d[:g.info]) == fd.CRC
	return fd
}

func EncodeField(f Format, slot []uint8, info []uint8, init int) {
	encodeFieldCRC(f, slot, info, init, l1.CRC16(info))
}

func encodeFieldCRC(f Format, slot []uint8, info []uint8, init int, c uint16) {
	g := geoms[f]
	if len(info) != g.info {
		panic("nonvoice: 情報ビット数が形式と合わない")
	}
	b := append(append([]uint8(nil), info...), putBits(nil, int(c), 16)...)
	b = append(b, 0, 0, 0, 0, 0)
	y := l1.Encode(b)
	n := g.rows * g.cols
	tx := make([]uint8, n)
	for i := 0; i < n; i++ {
		tx[i] = y[(i%g.rows)*g.cols+i/g.rows]
	}
	pn := l1.PN(init, n)
	k := 0
	for _, r := range g.rg {
		for i := r[0]; i < r[1]; i++ {
			slot[i] = tx[k] ^ pn[k]
			k++
		}
	}
}

func BuildSlot(f Format, slotNum, ci, cc int, info []uint8, init int) []uint8 {
	slot := make([]uint8, l1.SlotBits)
	sw := l1.SW20[slotNum&3]
	if f.Up() {
		putWord(slot, l1.UpSWPos, 20, sw^0xFFFFF)
		putWord(slot, l1.UpSWEnd+2, 6, uint32(cc))
	} else {
		putWord(slot, 6, 2, 0b10)
		putWord(slot, l1.SWPos, 20, sw)
		putWord(slot, l1.SWEnd, 2, uint32(ci))
		putWord(slot, l1.SWEnd+2, 6, uint32(cc))
	}
	EncodeField(f, slot, info, init)
	return slot
}

func SoftFromHard(hard []uint8) []float32 {
	out := make([]float32, len(hard))
	for i, b := range hard {
		if b&1 == 1 {
			out[i] = -1
		} else {
			out[i] = 1
		}
	}
	return out
}


func bitsVal(b []uint8) int {
	v := 0
	for _, x := range b {
		v = v<<1 | int(x&1)
	}
	return v
}

func putBits(dst []uint8, v, n int) []uint8 {
	for i := n - 1; i >= 0; i-- {
		dst = append(dst, uint8(v>>i&1))
	}
	return dst
}

func putWord(slot []uint8, pos, n int, v uint32) {
	for i := 0; i < n; i++ {
		slot[pos+i] = uint8(v >> (n - 1 - i) & 1)
	}
}

func allZero(b []uint8) bool {
	for _, x := range b {
		if x != 0 {
			return false
		}
	}
	return true
}

func bytesOf(b []uint8) []byte { return l1.ToBytes(b) }

func bitsOf(b []byte) []uint8 {
	out := make([]uint8, 0, 8*len(b))
	for _, o := range b {
		out = putBits(out, int(o), 8)
	}
	return out
}
