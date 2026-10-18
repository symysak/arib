package l1


func putWord(slot []uint8, pos, n int, v uint32) {
	for i := 0; i < n; i++ {
		slot[pos+i] = uint8(v >> (n - 1 - i) & 1)
	}
}

func putBits(dst []uint8, v, n int) []uint8 {
	for i := n - 1; i >= 0; i-- {
		dst = append(dst, uint8(v>>i&1))
	}
	return dst
}

func wBits(w W) []uint8 {
	b := []uint8{0, 0}
	if w.First {
		b[0] = 1
	}
	if w.Last {
		b[1] = 1
	}
	return putBits(b, w.WO, 6)
}

func withCRC16(info []uint8) []uint8 {
	c := CRC16(info)
	b := append(append([]uint8(nil), info...), putBits(nil, int(c), 16)...)
	b = append(b, 0, 0, 0, 0, 0)
	return Encode(b)
}

func header(sw SW, ci, cc int) []uint8 {
	slot := make([]uint8, SlotBits)
	putWord(slot, 6, 2, 0b10)
	if sw.Kind == SWBurst {
		for i := 6; i < SSPos; i++ {
			slot[i] = uint8([]int{1, 0, 0, 1}[(i-6)%4])
		}
		putWord(slot, SSPos, 32, SW32[sw.N-1])
		return slot
	}
	p := SW20[sw.N-1]
	if sw.Inverted {
		p ^= 0xFFFFF
	}
	putWord(slot, SWPos, 20, p)
	putWord(slot, SWEnd, 2, uint32(ci))
	putWord(slot, SWEnd+2, 6, uint32(cc))
	return slot
}

func scatter(slot []uint8, rg [][2]int, v []uint8, init int) {
	pn := PN(init, len(v))
	k := 0
	for _, r := range rg {
		for i := r[0]; i < r[1]; i++ {
			slot[i] = v[k] ^ pn[k]
			k++
		}
	}
}

func BuildCAC(sw SW, cc int, w W, data []uint8, sf int) []uint8 {
	info := append(wBits(w), data...)
	info = putBits(info, sf, 5)
	info = append(info, make([]uint8, 18)...)
	y := withCRC16(info)
	tx := make([]uint8, 280)
	for i := range tx {
		tx[i] = y[(i%20)*14+i/20]
	}
	slot := header(sw, 0, cc)
	scatter(slot, cacRange, tx, InitAllOnes)
	return slot
}

func EncodeSACCH(w W, data []uint8) []uint8 {
	info := append(wBits(w), data...)
	info = append(info, 0, 0, 0)
	y := withCRC16(info)
	tx := make([]uint8, 160)
	for i := range tx {
		tx[i] = y[(i%16)*10+i/16]
	}
	return tx
}

func EncodeFACCH(w W, data []uint8) []uint8 {
	info := append(wBits(w), data...)
	info = append(info, 0, 0, 0)
	return withCRC16(info)
}

func PlaceFACCH(tch, y []uint8, second bool) {
	for k := 0; k < 128; k++ {
		if second {
			tch[2*k+1] = y[16*(k%16)+8+k/16]
		} else {
			tch[2*k] = y[16*(k%16)+k/16]
		}
	}
}

func BuildTraffic(sw SW, ci, cc int, tch, sacch []uint8, bi uint8, init int) []uint8 {
	v := make([]uint8, 0, 284)
	v = append(v, tch[:112]...)
	v = append(v, sacch...)
	v = append(v, tch[112:]...)
	for i := 0; i < 8; i++ {
		v = append(v, bi)
	}
	slot := header(sw, ci, cc)
	scatter(slot, tchRange, v, init)
	return slot
}

func BuildSyncBurst(n int, sb SyncBurst) []uint8 {
	slot := header(SW{Kind: SWBurst, N: n}, 0, 0)
	copy(slot[SWEnd:], syncBurstBits(sb))
	return slot
}

func syncBurstBits(sb SyncBurst) []uint8 {
	info := putBits(nil, sb.CI, 2)
	info = putBits(info, sb.CC, 6)
	info = putBits(info, sb.B, 1)
	info = putBits(info, sb.TA, 4)
	info = putBits(info, sb.SSC, 8)
	info = putBits(info, sb.ID, 20)
	return sbEncode(info)
}

func sbEncode(info []uint8) []uint8 {
	info = putBits(info, int(CRC6(info, 0x3F)), 6)
	info = append(info, 0, 0, 0, 0, 0)
	y := Encode(info)
	out := make([]uint8, 104)
	for i := range out {
		out[i] = y[(i%13)*8+i/13]
	}
	return out
}
