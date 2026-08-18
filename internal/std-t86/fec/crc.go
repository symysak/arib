package fec

const (
	CRC16Poly = 0x1021
	CRC16Init = 0xFFFF
)

func CRC16CCITT(input []uint8) uint16 {
	reg := uint16(CRC16Init)
	for _, b := range input {
		reg ^= uint16(b&1) << 15
		if reg&0x8000 != 0 {
			reg = (reg << 1) ^ CRC16Poly
		} else {
			reg <<= 1
		}
	}
	return reg
}

func CRC16Bits(input []uint8) []uint8 {
	v := CRC16CCITT(input)
	out := make([]uint8, 16)
	for i := range out {
		out[i] = uint8((v >> uint(15-i)) & 1)
	}
	return out
}
