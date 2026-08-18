package scodec

import (
	"fmt"

	"github.com/symysak/stdt86/internal/std-t86/fec"
)

const (
	G7221FrameBits   = 320
	TCHBits          = 512
	NProtected       = 190
	NUnprotected     = 130
	ConstraintLength = 8

	CVinLen  = NProtected + 7
	CodedLen = 408
	PuncLen  = 382
)

var ConvPolys = []int{0o247, 0o371}

var CRC7Gen = [8]uint8{1, 0, 0, 1, 0, 1, 0, 1}

const CRC7Shift = 9

var puncturePos = func() [CodedLen]bool {
	var m [CodedLen]bool
	for k := 0; k < 26; k++ {
		m[5+16*k] = true
	}
	return m
}()

var ilSrc = func() [TCHBits]int {
	var t [TCHBits]int
	for i := 0; i < TCHBits; i++ {
		t[i] = 8*i - (i/64)*510 - (i/256)*7
	}
	assertPermutation(t[:], "ilSrc")
	return t
}()

func assertPermutation(p []int, name string) {
	seen := make([]bool, len(p))
	for _, v := range p {
		if v < 0 || v >= len(p) || seen[v] {
			panic(fmt.Sprintf("%s は置換ではありません（値 %d）", name, v))
		}
		seen[v] = true
	}
}

func CRC7(payload []uint8, shift int) []uint8 {
	d := make([]uint8, shift+len(payload))
	for i, b := range payload {
		d[shift+i] = b & 1
	}
	for i := len(d) - 1; i >= 7; i-- {
		if d[i] == 0 {
			continue
		}
		for j, g := range CRC7Gen {
			if g != 0 {
				d[i-7+j] ^= 1
			}
		}
	}
	out := make([]uint8, 7)
	copy(out, d[:7])
	return out
}

func cvinFromPayload(payload, crc []uint8) []uint8 {
	cv := make([]uint8, CVinLen)
	copy(cv[0:4], crc[0:4])
	for x := 4; x < 99; x++ {
		cv[x] = payload[2*x-8]
	}
	for x := 99; x < 194; x++ {
		cv[x] = payload[2*x-197]
	}
	copy(cv[194:197], crc[4:7])
	return cv
}

func cvinToPayload(cv []uint8) (payload, crc []uint8) {
	payload = make([]uint8, NProtected)
	crc = make([]uint8, 7)
	copy(crc[0:4], cv[0:4])
	for x := 4; x < 99; x++ {
		payload[2*x-8] = cv[x]
	}
	for x := 99; x < 194; x++ {
		payload[2*x-197] = cv[x]
	}
	copy(crc[4:7], cv[194:197])
	return payload, crc
}

func Puncture(coded []uint8) []uint8 {
	u := make([]uint8, PuncLen)
	for i := range u {
		u[i] = coded[i+(i+10)/15]
	}
	return u
}

func Depuncture(u []uint8) []uint8 {
	v := make([]uint8, CodedLen)
	ui := 0
	for j := 0; j < CodedLen; j++ {
		if puncturePos[j] {
			v[j] = fec.Erasure
			continue
		}
		v[j] = u[ui]
		ui++
	}
	return v
}

func TransmissionEncode(mi []uint8) ([]uint8, error) {
	if len(mi) != G7221FrameBits {
		return nil, fmt.Errorf("mi は %d bit 必要（%d 受領）", G7221FrameBits, len(mi))
	}
	payload, unprotected := mi[:NProtected], mi[NProtected:]
	cv := cvinFromPayload(payload, CRC7(payload, CRC7Shift))
	coded := fec.ConvEncode(cv, ConvPolys, ConstraintLength)
	ilin := append(append([]uint8{}, unprotected...), Puncture(coded)...)
	tx := make([]uint8, TCHBits)
	for i := 0; i < TCHBits; i++ {
		tx[i] = ilin[ilSrc[i]]
	}
	return tx, nil
}

func TransmissionDecode(tch []uint8) ([]uint8, uint8, error) {
	if len(tch) != TCHBits {
		return nil, 1, fmt.Errorf("TCH は %d bit 必要（%d 受領）", TCHBits, len(tch))
	}
	ilin := make([]uint8, TCHBits)
	for i := 0; i < TCHBits; i++ {
		ilin[ilSrc[i]] = tch[i]
	}
	unprotected, u := ilin[:NUnprotected], ilin[NUnprotected:]
	info := fec.ViterbiDecode(Depuncture(u), ConvPolys, ConstraintLength, true)
	payload, crcRx := cvinToPayload(info)
	fer := uint8(1)
	if equalBits(CRC7(payload, CRC7Shift), crcRx) {
		fer = 0
	}
	return append(payload, unprotected...), fer, nil
}

func equalBits(a, b []uint8) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}


func rotl9(x, r int) int {
	return ((x << uint(r)) | (x >> uint(9-r))) & 0x1FF
}

var Deinterleave = func() [TCHBits]int {
	var specDeint [TCHBits]int
	for j, v := range ilSrc {
		specDeint[v] = j
	}
	var t [TCHBits]int
	for i, v := range specDeint {
		t[i] = rotl9(v, 2) ^ 3
	}
	assertPermutation(t[:], "Deinterleave")
	return t
}()

const crc7GenBitSerial = 0xA9

func CRC7OTA(region []uint8) int {
	reg := 0
	for _, b := range region[:7] {
		reg = (reg << 1) | int(b&1)
	}
	for _, b := range region[7:] {
		reg = (reg << 1) | int(b&1)
		if reg&0x80 != 0 {
			reg ^= crc7GenBitSerial
		}
	}
	for i := 0; i < 7; i++ {
		reg <<= 1
		if reg&0x80 != 0 {
			reg ^= crc7GenBitSerial
		}
	}
	return reg & 0x7F
}

func TransmissionDecodeOTA(tch []uint8) ([]uint8, uint8, error) {
	if len(tch) != TCHBits {
		return nil, 1, fmt.Errorf("TCH は %d bit 必要（%d 受領）", TCHBits, len(tch))
	}
	ilin := make([]uint8, TCHBits)
	for i := 0; i < TCHBits; i++ {
		ilin[i] = tch[Deinterleave[i]]
	}
	unprotected, u := ilin[:NUnprotected], ilin[NUnprotected:]
	info := fec.ViterbiDecode(Depuncture(u), ConvPolys, ConstraintLength, true)
	mr := make([]uint8, G7221FrameBits)
	for k := 0; k < 95; k++ {
		mr[2*k] = info[3+k]
		mr[2*k+1] = info[98+k]
	}
	copy(mr[NProtected:], unprotected)
	rx := 0
	for _, b := range []uint8{info[196], info[195], info[194], info[193], info[2], info[1], info[0]} {
		rx = (rx << 1) | int(b&1)
	}
	fer := uint8(1)
	if CRC7OTA(info[3:193]) == rx {
		fer = 0
	}
	return mr, fer, nil
}

func TransmissionEncodeOTA(mr []uint8) ([]uint8, error) {
	if len(mr) != G7221FrameBits {
		return nil, fmt.Errorf("mr は %d bit 必要（%d 受領）", G7221FrameBits, len(mr))
	}
	payload, unprotected := mr[:NProtected], mr[NProtected:]
	info := make([]uint8, CVinLen)
	for k := 0; k < 95; k++ {
		info[3+k] = payload[2*k]
		info[98+k] = payload[2*k+1]
	}
	crc := CRC7OTA(info[3:193])
	info[196] = uint8((crc >> 6) & 1)
	info[195] = uint8((crc >> 5) & 1)
	info[194] = uint8((crc >> 4) & 1)
	info[193] = uint8((crc >> 3) & 1)
	info[2] = uint8((crc >> 2) & 1)
	info[1] = uint8((crc >> 1) & 1)
	info[0] = uint8(crc & 1)
	u := Puncture(fec.ConvEncode(info, ConvPolys, ConstraintLength))
	ilin := append(append([]uint8{}, unprotected...), u...)
	tch := make([]uint8, TCHBits)
	for i := 0; i < TCHBits; i++ {
		tch[Deinterleave[i]] = ilin[i]
	}
	return tch, nil
}

func DecodeTCHFrames(tchSlots [][]uint8, seed int, ota bool) ([][]uint8, []uint8, error) {
	pn := fec.LFSRPN(seed, TCHBits)
	frames := make([][]uint8, len(tchSlots))
	fers := make([]uint8, len(tchSlots))
	buf := make([]uint8, TCHBits)
	for i, tch := range tchSlots {
		if len(tch) != TCHBits {
			return nil, nil, fmt.Errorf("スロット %d: TCH は %d bit 必要（%d 受領）", i, TCHBits, len(tch))
		}
		for k := 0; k < TCHBits; k++ {
			buf[k] = (tch[k] & 1) ^ pn[k]
		}
		var (
			mr  []uint8
			fer uint8
			err error
		)
		if ota {
			mr, fer, err = TransmissionDecodeOTA(buf)
		} else {
			mr, fer, err = TransmissionDecode(buf)
		}
		if err != nil {
			return nil, nil, err
		}
		frames[i], fers[i] = mr, fer
	}
	return frames, fers, nil
}

func DecodeTCHFramesGapped(entries [][]uint8, seed int) ([][]uint8, []uint8, error) {
	frames := make([][]uint8, len(entries))
	fers := make([]uint8, len(entries))
	var real [][]uint8
	var idx []int
	for i, e := range entries {
		frames[i] = make([]uint8, G7221FrameBits)
		fers[i] = 1
		if e != nil {
			real = append(real, e)
			idx = append(idx, i)
		}
	}
	if len(real) == 0 {
		return frames, fers, nil
	}
	dec, decFers, err := DecodeTCHFrames(real, seed, true)
	if err != nil {
		return nil, nil, err
	}
	for k, i := range idx {
		frames[i] = dec[k]
		fers[i] = decFers[k]
	}
	return frames, fers, nil
}
