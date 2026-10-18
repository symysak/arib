package l1

import (
	"fmt"
	"math/bits"
)


type DirectMode int

const (
	ModeDirect    DirectMode = iota
	ModeRelayDown
	ModeRelayUp
)

func (m DirectMode) String() string {
	return [...]string{"直接通信", "中継（中継局→移動局）", "中継（移動局→中継局）"}[m]
}

var directWordsN = [3][5]int{
	{9, 10, 1, 11, 1},
	{6, 7, 2, 8, 2},
	{4, 5, 12, 3, 3},
}

type DSW struct {
	OK    bool
	Mode  DirectMode
	SB    bool
	Ctl   bool
	Voice bool
	First bool
	N     int
	Errs  int
}

func (s DSW) Name() string {
	if !s.OK {
		return "-"
	}
	if s.SB {
		return fmt.Sprintf("SS%d", s.N)
	}
	if s.Ctl {
		return fmt.Sprintf("¬S%d", s.N)
	}
	return fmt.Sprintf("S%d", s.N)
}

func (s DSW) Kind(control bool) string {
	switch {
	case !s.OK:
		return "-"
	case s.SB:
		return "同期バースト"
	}
	k := "TCH"
	if s.Ctl {
		k = "DACCH"
		if control {
			k = "DCCH"
		}
	}
	if s.Voice {
		return k + "（音声）"
	}
	return k + "（非音声）"
}

func DirectWord(m DirectMode, n int, ctl, sb bool) ([]uint8, int) {
	if sb {
		v := SW32[directWordsN[m][4]-1]
		out := make([]uint8, 32)
		for i := range out {
			out[i] = uint8(v >> (31 - i) & 1)
		}
		return out, UpSSPos
	}
	v := SW20[directWordsN[m][n]-1]
	if ctl {
		v ^= 0xFFFFF
	}
	out := make([]uint8, 20)
	for i := range out {
		out[i] = uint8(v >> (19 - i) & 1)
	}
	return out, UpSWPos
}

func DirectWords(m DirectMode) []DSW {
	var out []DSW
	for n := 0; n < 4; n++ {
		for _, ctl := range []bool{false, true} {
			out = append(out, DSW{OK: true, Mode: m, Ctl: ctl, Voice: n < 2, First: n%2 == 0, N: directWordsN[m][n]})
		}
	}
	return append(out, DSW{OK: true, Mode: m, SB: true, N: directWordsN[m][4]})
}

func (s DSW) Bits() ([]uint8, int) {
	if s.SB {
		return DirectWord(s.Mode, 0, false, true)
	}
	n := 0
	switch {
	case s.Voice && !s.First:
		n = 1
	case !s.Voice && s.First:
		n = 2
	case !s.Voice:
		n = 3
	}
	return DirectWord(s.Mode, n, s.Ctl, false)
}

func FindSWDirect(slot []uint8, m DirectMode, maxErr20, maxErr32 int) DSW {
	best := DSW{Errs: 99}
	w := word(slot, UpSWPos, 20)
	w32 := word(slot, UpSSPos, 32)
	for _, c := range DirectWords(m) {
		var e int
		if c.SB {
			e = bits.OnesCount32(w32 ^ SW32[c.N-1])
			if e > maxErr32 {
				continue
			}
		} else {
			v := SW20[c.N-1]
			if c.Ctl {
				v ^= 0xFFFFF
			}
			e = bits.OnesCount32(w ^ v)
			if e > maxErr20 {
				continue
			}
		}
		if e < best.Errs {
			best = c
			best.Errs = e
		}
	}
	if best.Errs == 99 {
		return DSW{}
	}
	return best
}

var directDataRange = [][2]int{{16, 156}, {188, 304}}

const (
	DPICHPos  = 176
	DPICHBits = 12
	PICHUnits = 5
)

func DirectDataRaw(slot []float32) []float32 { return gather(slot, directDataRange) }

func DirectDataAt(raw []float32, init int) []float32 {
	v := append([]float32(nil), raw...)
	descramble(v, init)
	return v
}

func DirectPICH(slot []float32) []float32 {
	return append([]float32(nil), slot[DPICHPos:DPICHPos+DPICHBits]...)
}

type PICH struct {
	OK        bool
	BI        int
	IC        int
	ID        int
	Corrected int
	CRCInit   uint8
}

func (p PICH) Number() int      { return p.ID & 0x7FFFF }
func (p PICH) Individual() bool { return p.ID>>19&1 == 1 }

func (p PICH) String() string {
	if !p.OK {
		return "CRC NG"
	}
	who := "セレコール（宛先）"
	if p.IC == 1 {
		who = "送信局 ID（話者）"
	}
	kind := "グループ番号"
	if p.Individual() {
		kind = "個別番号"
	}
	bi := "空き"
	if p.BI == 1 {
		bi = "使用中"
	}
	return fmt.Sprintf("%s %s %d / BI %s", who, kind, p.Number(), bi)
}

func DecodePICH(units [][]float32) PICH {
	var tx []float32
	for _, u := range units {
		tx = append(tx, u...)
	}
	if len(tx) < 56 {
		return PICH{}
	}
	y := make([]float32, 56)
	for i := 0; i < 56; i++ {
		y[(i%7)*8+i/7] = tx[i]
	}
	var p PICH
	data := make([]uint8, 0, 28)
	for k := 0; k < 7; k++ {
		d, corr := bchML(y[8*k : 8*k+8])
		data = append(data, d...)
		p.Corrected += corr
	}
	got := uint8(bitsVal(data[22:28]))
	for _, init := range []uint8{0x3F, 0} {
		if CRC6(data[:22], init) == got {
			p.OK, p.CRCInit = true, init
			break
		}
	}
	p.BI, p.IC, p.ID = int(data[0]), int(data[1]), bitsVal(data[2:22])
	return p
}

func bchML(cw []float32) ([]uint8, int) {
	best, bm := 0, float32(-1e30)
	for v := 0; v < 16; v++ {
		c := bch84([]uint8{uint8(v >> 3 & 1), uint8(v >> 2 & 1), uint8(v >> 1 & 1), uint8(v & 1)})
		var m float32
		for i, b := range c {
			if b == 0 {
				m += cw[i]
			} else {
				m -= cw[i]
			}
		}
		if m > bm {
			best, bm = v, m
		}
	}
	c := bch84([]uint8{uint8(best >> 3 & 1), uint8(best >> 2 & 1), uint8(best >> 1 & 1), uint8(best & 1)})
	corr := 0
	for i, b := range c {
		if hardOf(cw[i]) != b {
			corr++
		}
	}
	return c[:4], corr
}

func EncodePICH(bi, ic, id int) [][]uint8 {
	info := putBits(nil, bi, 1)
	info = putBits(info, ic, 1)
	info = putBits(info, id, 20)
	info = putBits(info, int(CRC6(info, 0x3F)), 6)
	var y []uint8
	for k := 0; k < 7; k++ {
		y = append(y, bch84(info[4*k:4*k+4])...)
	}
	tx := make([]uint8, 60)
	for i := 0; i < 56; i++ {
		tx[i] = y[(i%7)*8+i/7]
	}
	out := make([][]uint8, PICHUnits)
	for k := range out {
		out[k] = tx[12*k : 12*k+12]
	}
	return out
}

type DirectSB struct {
	OK        bool
	I         int
	BI        int
	IC        int
	SCD       int
	ID        int
	Corrected int
	CRCInit   uint8
}

func (s DirectSB) Number() int      { return s.ID & 0x7FFFF }
func (s DirectSB) Individual() bool { return s.ID>>19&1 == 1 }

func DecodeDirectSB(slot []float32) DirectSB {
	d, errs, ok, init := sbBits(slot, UpSWEnd)
	return DirectSB{OK: ok, CRCInit: init, Corrected: errs, I: bitsVal(d[0:3]), BI: int(d[3]), IC: int(d[4]),
		SCD: bitsVal(d[5:21]), ID: bitsVal(d[21:41])}
}

func headerDirect(sw DSW) []uint8 {
	slot := make([]uint8, SlotBits)
	b, pos := sw.Bits()
	copy(slot[pos:], b)
	return slot
}

func BuildDirect(sw DSW, data, pich []uint8, init int) []uint8 {
	slot := headerDirect(sw)
	scatter(slot, directDataRange, data, init)
	copy(slot[DPICHPos:], pich)
	return slot
}

func BuildDirectSB(m DirectMode, sb DirectSB) []uint8 {
	slot := headerDirect(DSW{OK: true, Mode: m, SB: true, N: directWordsN[m][4]})
	info := putBits(nil, sb.I, 3)
	info = putBits(info, sb.BI, 1)
	info = putBits(info, sb.IC, 1)
	info = putBits(info, sb.SCD, 16)
	info = putBits(info, sb.ID, 20)
	copy(slot[UpSWEnd:], sbEncode(info))
	return slot
}
