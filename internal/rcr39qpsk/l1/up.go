package l1

import "math/bits"


const (
	UpSWPos = 156
	UpSSPos = 144
	UpSWEnd = 176
	UpTrafficBits = 276
)

var (
	upTchRange  = [][2]int{{8, 156}, {184, 312}}
	upCAC1Range = [][2]int{{98, 156}, {184, 296}}
	upCACnRange = [][2]int{{44, 156}, {184, 296}}
)

func FindSWUp(slot []uint8, maxErr20, maxErr32 int) SW {
	best := SW{Errs: 99}
	w := word(slot, UpSWPos, 20)
	for i := 0; i < 8; i++ {
		p := SW20[i]
		if e := bits.OnesCount32(w ^ p ^ 0xFFFFF); e <= maxErr20 && e < best.Errs {
			best = SW{Kind: kindOf(i + 1), N: i + 1, Errs: e}
		}
		if e := bits.OnesCount32(w ^ p); e <= maxErr20 && e < best.Errs {
			best = SW{Kind: kindOf(i + 1), N: i + 1, Inverted: true, Errs: e}
		}
	}
	w32 := word(slot, UpSSPos, 32)
	for i, p := range SW32 {
		if e := bits.OnesCount32(w32 ^ p ^ 0xFFFFFFFF); e <= maxErr32 && e < best.Errs {
			best = SW{Kind: SWBurst, N: i + 1, Errs: e}
		}
	}
	if best.Errs == 99 {
		return SW{}
	}
	return best
}

func UpWord(sw SW) (bitsOut []uint8, pos int) {
	if sw.Kind == SWBurst {
		v := SW32[sw.N-1] ^ 0xFFFFFFFF
		out := make([]uint8, 32)
		for i := range out {
			out[i] = uint8(v >> (31 - i) & 1)
		}
		return out, UpSSPos
	}
	v := SW20[sw.N-1]
	if !sw.Inverted {
		v ^= 0xFFFFF
	}
	out := make([]uint8, 20)
	for i := range out {
		out[i] = uint8(v >> (19 - i) & 1)
	}
	return out, UpSWPos
}

func UpWords() []SW {
	var out []SW
	for i := 1; i <= 8; i++ {
		out = append(out, SW{Kind: kindOf(i), N: i}, SW{Kind: kindOf(i), N: i, Inverted: true})
	}
	for i := 1; i <= 4; i++ {
		out = append(out, SW{Kind: SWBurst, N: i})
	}
	return out
}

func UpICC(slot []uint8) (i, cc int) {
	return int(word(slot, UpSWEnd, 2)), int(word(slot, UpSWEnd+2, 6))
}

func InitFromCC(cc int) int { return InitFromPattern(cc & 0x3F) }

func TrafficUpRaw(slot []float32) []float32 { return gather(slot, upTchRange) }

func TrafficUpAt(raw []float32, init int) Traffic {
	v := append([]float32(nil), raw...)
	descramble(v, init)
	t := Traffic{TCH: make([]float32, 0, TCHBits)}
	t.TCH = append(t.TCH, v[:148]...)
	t.SACCH = v[148:168]
	t.TCH = append(t.TCH, v[168:276]...)
	return t
}

const (
	UpCACFirstBits = 56
	UpCACNextBits  = 80
)

func DecodeUpCAC(slot []float32, init int, first bool) Unit {
	if first {
		v := gather(slot, upCAC1Range)
		descramble(v, init)
		y := make([]float32, 170)
		for i, s := range v {
			y[(i%17)*10+i/17] = s
		}
		d, errs := Viterbi(y)
		u := Unit{W: wOf(d), Data: d[8:64], Corrected: errs, CRC: uint16(bitsVal(d[64:80]))}
		u.OK = CRC16(d[:64]) == u.CRC
		return u
	}
	v := gather(slot, upCACnRange)
	descramble(v, init)
	y := make([]float32, 224)
	for i, s := range v {
		y[(i%16)*14+i/16] = s
	}
	d, errs := Viterbi(y)
	u := Unit{W: wOf(d), Data: d[8:88], Extra: d[88:91], Corrected: errs, CRC: uint16(bitsVal(d[91:107]))}
	u.OK = CRC16(d[:91]) == u.CRC
	return u
}

func DecodeSyncBurstUp(slot []float32) SyncBurst { return decodeSyncBurstAt(slot, UpSWEnd) }


func headerUp(sw SW, cc int, controlFirst bool) []uint8 {
	slot := make([]uint8, SlotBits)
	b, pos := UpWord(sw)
	copy(slot[pos:], b)
	if sw.Kind == SWBurst {
		for i := 94; i < UpSSPos; i++ {
			slot[i] = uint8([]int{1, 0, 0, 1}[(i-94)%4])
		}
		return slot
	}
	if controlFirst {
		for i := 42; i < 98; i++ {
			slot[i] = uint8([]int{1, 0, 0, 1}[(i-42)%4])
		}
	}
	putWord(slot, UpSWEnd+2, 6, uint32(cc))
	return slot
}

func BuildTrafficUp(sw SW, cc int, tch, sacch []uint8, init int) []uint8 {
	v := make([]uint8, 0, UpTrafficBits)
	v = append(v, tch[:148]...)
	v = append(v, sacch...)
	v = append(v, tch[148:]...)
	slot := headerUp(sw, cc, false)
	scatter(slot, upTchRange, v, init)
	return slot
}

func BuildUpCAC(sw SW, cc int, first bool, w W, data []uint8) []uint8 {
	info := putBits(nil, b2(w.First), 1)
	info = putBits(info, b2(w.Last), 1)
	info = putBits(info, w.WO, 6)
	info = append(info, data...)
	if !first {
		info = append(info, 0, 0, 0)
	}
	info = putBits(info, int(CRC16(info)), 16)
	info = append(info, 0, 0, 0, 0, 0)
	y := Encode(info)
	rows, cols, rg := 17, 10, upCAC1Range
	if !first {
		rows, cols, rg = 16, 14, upCACnRange
	}
	v := make([]uint8, len(y))
	for i := range v {
		v[i] = y[(i%rows)*cols+i/rows]
	}
	slot := headerUp(sw, cc, first)
	scatter(slot, rg, v, InitFromCC(cc))
	return slot
}

func BuildSyncBurstUp(n int, sb SyncBurst) []uint8 {
	slot := headerUp(SW{Kind: SWBurst, N: n}, 0, false)
	copy(slot[UpSWEnd:], syncBurstBits(sb))
	return slot
}

func b2(v bool) int {
	if v {
		return 1
	}
	return 0
}
