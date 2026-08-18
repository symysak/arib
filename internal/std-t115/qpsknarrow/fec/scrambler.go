package fec

const (

	PN16Length = 65535
)

func Scramble(init, length int) []uint8 {
	reg := uint32(init) & 0xFFFF
	out := make([]uint8, length)
	for k := range out {
		s := func(j uint) uint32 { return (reg >> (15 - j)) & 1 }
		out[k] = uint8(s(0))
		fb := s(0) ^ s(2) ^ s(3) ^ s(5)
		reg = ((reg << 1) & 0xFFFF) | fb
	}
	return out
}

func Descramble(bits []uint8, init int) []uint8 {
	pn := Scramble(init, len(bits))
	out := make([]uint8, len(bits))
	for i := range bits {
		out[i] = bits[i] ^ pn[i]
	}
	return out
}
