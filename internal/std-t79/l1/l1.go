package l1

import (
	"fmt"
	"math"
	"math/bits"

	"github.com/symysak/arib/internal/voice"
)

const (
	SlotBits       = 320
	SlotsPerFrame  = 4
	FramesPerSuper = 18
	SWPos = 120
	SSPos = 108
	SWEnd = 140
	TCHBits = 256
)

var (
	SW20 = [12]uint32{0x87A4B, 0x9D236, 0x81D75, 0xA94EA, 0x31BAF, 0x1E56F, 0xE712C, 0xFBC1F,
		0x5164C, 0x4D9DE, 0x8279E, 0x98908}
	SW32 = [4]uint32{0x2F94D06B, 0x1D4EE2B1, 0x70168FE9, 0x83527CAD}
)

type SWKind int

const (
	SWNone   SWKind = iota
	SWNormal
	SWSuper
	SWIdle
	SWBurst
)

func (k SWKind) String() string {
	return [...]string{"-", "通常", "SF 先頭", "空線", "同期バースト"}[k]
}

type SW struct {
	Kind SWKind
	N int
	Inverted bool
	Errs int
}

func (s SW) Slot() int {
	if s.Kind == SWNone {
		return -1
	}
	return (s.N - 1) % 4
}

func (s SW) Name() string {
	switch s.Kind {
	case SWNone:
		return "-"
	case SWBurst:
		return fmt.Sprintf("SS%d", s.N)
	}
	if s.Inverted {
		return fmt.Sprintf("¬S%d", s.N)
	}
	return fmt.Sprintf("S%d", s.N)
}

func (s SW) Busy() bool { return s.Kind == SWNormal || s.Kind == SWSuper || s.Kind == SWBurst }

func word(hard []uint8, pos, n int) uint32 {
	var v uint32
	for i := 0; i < n; i++ {
		v = v<<1 | uint32(hard[pos+i]&1)
	}
	return v
}

func FindSW(slot []uint8, maxErr20, maxErr32 int) SW {
	best := SW{Errs: 99}
	w := word(slot, SWPos, 20)
	for i, p := range SW20 {
		if e := bits.OnesCount32(w ^ p); e <= maxErr20 && e < best.Errs {
			best = SW{Kind: kindOf(i + 1), N: i + 1, Errs: e}
		}
		if i < 8 {
			if e := bits.OnesCount32(w ^ p ^ 0xFFFFF); e <= maxErr20 && e < best.Errs {
				best = SW{Kind: kindOf(i + 1), N: i + 1, Inverted: true, Errs: e}
			}
		}
	}
	w32 := word(slot, SSPos, 32)
	for i, p := range SW32 {
		if e := bits.OnesCount32(w32 ^ p); e <= maxErr32 && e < best.Errs {
			best = SW{Kind: SWBurst, N: i + 1, Errs: e}
		}
	}
	if best.Kind == SWNone {
		return SW{}
	}
	return best
}

func kindOf(n int) SWKind {
	switch {
	case n <= 4:
		return SWNormal
	case n <= 8:
		return SWSuper
	}
	return SWIdle
}

func CICC(slot []uint8) (ci, cc int) {
	return int(word(slot, SWEnd, 2)), int(word(slot, SWEnd+2, 6))
}


func PN(init, n int) []uint8 {
	r := uint32(init) & 0x1FF
	out := make([]uint8, n)
	for i := range out {
		b := r & 1
		out[i] = uint8(b)
		nb := b ^ (r >> 4 & 1)
		r = r>>1 | nb<<8
	}
	return out
}

const InitAllOnes = 0x1FF

func InitFromPattern(p int) int {
	d1 := p & 1
	return d1 | (p&0xFF)<<1
}

func PatternOf(init int) int {
	if init&1 != init>>1&1 {
		return -1
	}
	return init >> 1 & 0xFF
}

var (
	cacRange = [][2]int{{8, 120}, {148, 316}}
	tchRange = [][2]int{{8, 120}, {148, 320}}
)

func gather(slot []float32, rg [][2]int) []float32 {
	var out []float32
	for _, r := range rg {
		out = append(out, slot[r[0]:r[1]]...)
	}
	return out
}

func descramble(v []float32, init int) {
	for i, b := range PN(init, len(v)) {
		if b == 1 {
			v[i] = -v[i]
		}
	}
}


const nState = 32

func outBits(st, b int) (int, int) {
	y1 := b ^ st&1 ^ st>>2&1 ^ st>>4&1
	y2 := b ^ st>>1&1 ^ st>>2&1 ^ st>>3&1 ^ st>>4&1
	return y1, y2
}

func Encode(in []uint8) []uint8 {
	out := make([]uint8, 0, 2*len(in))
	st := 0
	for _, b := range in {
		y1, y2 := outBits(st, int(b))
		out = append(out, uint8(y1), uint8(y2))
		st = (st<<1 | int(b)) & (nState - 1)
	}
	return out
}

func Viterbi(soft []float32) ([]uint8, int) {
	n := len(soft) / 2
	inf := float32(math.Inf(1))
	pm := make([]float32, nState)
	for i := range pm {
		pm[i] = inf
	}
	pm[0] = 0
	prev := make([][nState]uint8, n)
	npm := make([]float32, nState)
	for i := 0; i < n; i++ {
		r1, r2 := soft[2*i], soft[2*i+1]
		for j := range npm {
			npm[j] = inf
		}
		for st := 0; st < nState; st++ {
			if pm[st] == inf {
				continue
			}
			for b := 0; b < 2; b++ {
				y1, y2 := outBits(st, b)
				m := pm[st] + cost(r1, y1) + cost(r2, y2)
				ns := (st<<1 | b) & (nState - 1)
				if m < npm[ns] {
					npm[ns] = m
					prev[i][ns] = uint8(st)
				}
			}
		}
		pm, npm = npm, pm
	}
	st := 0
	if pm[0] == inf {
		for s := range pm {
			if pm[s] < pm[st] {
				st = s
			}
		}
	}
	out := make([]uint8, n)
	for i := n - 1; i >= 0; i-- {
		out[i] = uint8(st & 1)
		st = int(prev[i][st])
	}
	re := Encode(out)
	errs := 0
	for i, v := range re {
		if hardOf(soft[i]) != v {
			errs++
		}
	}
	return out, errs
}

func cost(r float32, y int) float32 {
	if y == 0 {
		if r < 0 {
			return -r
		}
		return 0
	}
	if r > 0 {
		return r
	}
	return 0
}

func hardOf(v float32) uint8 {
	if v < 0 {
		return 1
	}
	return 0
}

func Hard(soft []float32) []uint8 {
	out := make([]uint8, len(soft))
	for i, v := range soft {
		out[i] = hardOf(v)
	}
	return out
}


func CRC16(b []uint8) uint16 {
	c := uint16(0xFFFF)
	for _, x := range b {
		fb := c>>15 ^ uint16(x&1)
		c <<= 1
		if fb == 1 {
			c ^= 0x1021
		}
	}
	return c
}

func CRC6(b []uint8, init uint8) uint8 {
	c := init & 0x3F
	for _, x := range b {
		fb := c>>5&1 ^ x&1
		c = c << 1 & 0x3F
		if fb == 1 {
			c ^= 0x03
		}
	}
	return c
}

func bitsVal(b []uint8) int {
	v := 0
	for _, x := range b {
		v = v<<1 | int(x&1)
	}
	return v
}

func ToBytes(b []uint8) []byte {
	out := make([]byte, (len(b)+7)/8)
	for i, x := range b {
		out[i/8] |= (x & 1) << (7 - i%8)
	}
	return out
}


type W struct {
	First, Last bool
	WO          int
}

func wOf(b []uint8) W { return W{First: b[0] == 1, Last: b[1] == 1, WO: bitsVal(b[2:8])} }

func (w W) String() string {
	switch {
	case w.First && w.Last:
		return fmt.Sprintf("単独 %d", w.WO)
	case w.First:
		return fmt.Sprintf("先頭 残り%d", w.WO)
	case w.Last:
		return fmt.Sprintf("最終 %dオクテット", w.WO)
	case w.WO == 0:
		return "メッセージ無し（ダミーデータ）"
	}
	return fmt.Sprintf("途中 %d", w.WO)
}

type Unit struct {
	OK   bool
	W    W
	Data []uint8
	Extra []uint8
	Corrected int
	CRC uint16
}

func (u Unit) Bytes() []byte { return ToBytes(u.Data) }

func (u Unit) NZero() bool {
	if len(u.Extra) != 3 {
		return true
	}
	return u.Extra[0]|u.Extra[1]|u.Extra[2] == 0
}

func (u Unit) SFPos() int {
	if len(u.Extra) != 23 {
		return -1
	}
	return bitsVal(u.Extra[:5])
}


func DecodeCAC(slot []float32) Unit {
	v := gather(slot, cacRange)
	descramble(v, InitAllOnes)
	y := make([]float32, 280)
	for i, s := range v {
		y[(i%20)*14+i/20] = s
	}
	d, errs := Viterbi(y)
	u := Unit{W: wOf(d), Data: d[8:96], Extra: d[96:119], Corrected: errs}
	u.OK = CRC16(d[:119]) == uint16(bitsVal(d[119:135]))
	return u
}


type Traffic struct {
	TCH   []float32
	SACCH []float32
	BI    []float32
}

func SplitTraffic(slot []float32, init int) Traffic {
	v := gather(slot, tchRange)
	descramble(v, init)
	t := Traffic{TCH: make([]float32, 0, TCHBits)}
	t.TCH = append(t.TCH, v[:112]...)
	t.SACCH = v[112:132]
	t.TCH = append(t.TCH, v[132:276]...)
	t.BI = v[276:284]
	return t
}

func TrafficRaw(slot []float32) []float32 { return gather(slot, tchRange) }

func TrafficAt(raw []float32, init int) Traffic {
	v := append([]float32(nil), raw...)
	descramble(v, init)
	t := Traffic{TCH: make([]float32, 0, TCHBits)}
	t.TCH = append(t.TCH, v[:112]...)
	t.SACCH = v[112:132]
	t.TCH = append(t.TCH, v[132:276]...)
	t.BI = v[276:284]
	return t
}

const SACCHSlots = 8

func SACCHPart(sf int) (unit, k int) {
	if sf < 2 || sf >= FramesPerSuper {
		return -1, 0
	}
	return (sf - 2) / SACCHSlots, (sf - 2) % SACCHSlots
}

func DecodeSACCH(v []float32) Unit {
	y := make([]float32, 160)
	for i, s := range v {
		y[(i%16)*10+i/16] = s
	}
	d, errs := Viterbi(y)
	u := Unit{W: wOf(d), Data: d[8:56], Extra: d[56:59], Corrected: errs}
	u.OK = CRC16(d[:59]) == uint16(bitsVal(d[59:75]))
	return u
}

func FACCHHalf(y, tch []float32, second bool) {
	for k := 0; k < 128; k++ {
		if second {
			y[16*(k%16)+8+k/16] = tch[2*k+1]
		} else {
			y[16*(k%16)+k/16] = tch[2*k]
		}
	}
}

func TCHHalf(tch []float32, second bool) []float32 {
	out := make([]float32, 128)
	for k := range out {
		if second {
			out[k] = tch[2*k]
		} else {
			out[k] = tch[2*k+1]
		}
	}
	return out
}

func DecodeFACCH(first, second []float32) Unit {
	y := make([]float32, 256)
	FACCHHalf(y, first, false)
	FACCHHalf(y, second, true)
	d, errs := Viterbi(y)
	u := Unit{W: wOf(d), Data: d[8:104], Extra: d[104:107], Corrected: errs}
	u.OK = CRC16(d[:107]) == uint16(bitsVal(d[107:123]))
	return u
}


type SyncBurst struct {
	OK        bool
	CRCInit   uint8
	CI, CC    int
	B         int
	TA, SSC   int
	ID        int
	Corrected int
}

func (s SyncBurst) IDNumber() int { return s.ID & 0x7FFFF }

func DecodeSyncBurst(slot []float32) SyncBurst { return decodeSyncBurstAt(slot, SWEnd) }

func decodeSyncBurstAt(slot []float32, start int) SyncBurst {
	d, errs, ok, init := sbBits(slot, start)
	sb := SyncBurst{Corrected: errs, OK: ok, CRCInit: init}
	sb.CI, sb.CC = bitsVal(d[0:2]), bitsVal(d[2:8])
	sb.B, sb.TA, sb.SSC = int(d[8]), bitsVal(d[9:13]), bitsVal(d[13:21])
	sb.ID = bitsVal(d[21:41])
	return sb
}

func sbBits(slot []float32, start int) (d []uint8, errs int, ok bool, init uint8) {
	v := slot[start : start+104]
	y := make([]float32, 104)
	for i, s := range v {
		y[(i%13)*8+i/13] = s
	}
	d, errs = Viterbi(y)
	got := uint8(bitsVal(d[41:47]))
	for _, in := range []uint8{0x3F, 0} {
		if CRC6(d[:41], in) == got {
			return d, errs, true, in
		}
	}
	return d, errs, false, 0
}

const VoiceChannel voice.Channel = "T79-TCH"
