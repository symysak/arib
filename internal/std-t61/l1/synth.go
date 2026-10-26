package l1

import "github.com/symysak/arib/internal/std-t61/fec"


type ServiceFrameSpec struct {
	RICH17 uint32
	SW     *SyncWord
	FieldA []uint8
	Mid    []uint8
	FieldB []uint8
	Scramble int
}

func BuildServiceFrame(s ServiceFrameSpec) []uint8 {
	f := make([]uint8, FrameBits)
	for i := 0; i < 32; i++ {
		f[i] = uint8((i + 1) & 1)
	}
	copy(f[FieldAStart:FieldAEnd], s.FieldA)
	copy(f[RIStart:RIEnd], fec.BlockRICH.Encode(fec.UintToBits(uint64(s.RICH17), 17)))
	copy(f[SWStart:SWStart+20], s.SW.Bits)
	copy(f[MidStart:MidEnd], s.Mid)
	copy(f[FieldBStart:FrameEnd], s.FieldB)
	if s.Scramble != 0 {
		pn := fec.PN95(s.Scramble, 276)
		k := 0
		for i := FieldAStart; i < FieldAEnd; i++ {
			f[i] ^= pn[k]
			k++
		}
		for i := MidStart; i < FrameEnd; i++ {
			f[i] ^= pn[k]
			k++
		}
	}
	return f
}

func BuildControlFrame(rich17 uint32, sw *SyncWord, unit []uint8, scramble int) []uint8 {
	f := make([]uint8, FrameBits)
	for i := 0; i < 32; i++ {
		f[i] = uint8((i + 1) & 1)
	}
	code := fec.BlockPCCDown.Encode(unit)
	copy(f[FieldAStart:FieldAEnd], code[:96])
	copy(f[RIStart:RIEnd], fec.BlockRICH.Encode(fec.UintToBits(uint64(rich17), 17)))
	copy(f[SWStart:SWStart+20], sw.Bits)
	copy(f[PCCDataStart:FrameEnd], code[96:])
	if scramble != 0 {
		pn := fec.PN95(scramble, 272)
		k := 0
		for i := FieldAStart; i < FieldAEnd; i++ {
			f[i] ^= pn[k]
			k++
		}
		for i := PCCDataStart; i < FrameEnd; i++ {
			f[i] ^= pn[k]
			k++
		}
	}
	return f
}

func BuildControlUpFrame(rich17 uint32, sw *SyncWord, unit []uint8, second bool, scramble int) []uint8 {
	f := make([]uint8, FrameBits)
	blk, a := fec.BlockPCCUp1, upFirstAStart
	if second {
		blk, a = fec.BlockPCCUp2, upSecondAStart
	}
	for i := 0; i < a; i++ {
		if second {
			f[i] = uint8((i + 1) & 1)
		} else {
			f[i] = []uint8{1, 0, 0, 1}[i%4]
		}
	}
	code := blk.Encode(unit)
	if scramble != 0 {
		code = fec.Descramble(code, scramble)
	}
	na := FieldAEnd - a
	copy(f[a:FieldAEnd], code[:na])
	copy(f[RIStart:RIEnd], fec.BlockRICH.Encode(fec.UintToBits(uint64(rich17), 17)))
	copy(f[SWStart:SWStart+20], sw.Bits)
	copy(f[upBStart:upBEnd], code[na:])
	return f
}

func BuildSyncBurst(rich17 uint32, param40 []uint8, scramble int) []uint8 {
	return BuildSyncBurstSW(rich17, param40, scramble, FindSyncWord("SS1"))
}

func BuildSyncBurstSW(rich17 uint32, param40 []uint8, scramble int, ss1 *SyncWord) []uint8 {
	f := make([]uint8, FrameBits)
	pat := []uint8{1, 0, 0, 1}
	for i := 0; i < RIStart; i++ {
		f[i] = pat[i%4]
	}
	copy(f[RIStart:RIEnd], fec.BlockRICH.Encode(fec.UintToBits(uint64(rich17), 17)))
	copy(f[SWStart:SBSWEnd], ss1.Bits)
	for i := SBSWEnd; i < SBPIStr; i++ {
		f[i] = pat[i%4]
	}
	info := append(append([]uint8(nil), param40...), 0)
	code := fec.BlockSyncBurst.Encode(info)
	if scramble != 0 {
		code = fec.Descramble(code, scramble)
	}
	copy(f[SBPIStr:SBPIEnd], code)
	return f
}

func FindSyncWord(name string) *SyncWord {
	for i := range SyncWords {
		if SyncWords[i].Name == name {
			return &SyncWords[i]
		}
	}
	return nil
}

func BuildA1Frame(r A1RICH, crcInit uint8, crcVariant int, tch1 []uint8, pattern int) []uint8 {
	f := make([]uint8, A1FrameBits)
	for i := range f {
		f[i] = uint8((i + 1) & 1)
	}
	copy(f[a1SWStart:a1SWEnd], fec.UintToBits(A1SW1, 20))
	copy(f[A1DataStart:], EncodeA1RICHVariant(r, crcInit, crcVariant))
	t := append([]uint8(nil), tch1...)
	if pattern >= 0 {
		t = A1DescrambleBits(t, pattern)
	}
	copy(f[A1DataStart+A1RICHBits:], t)
	return f
}
