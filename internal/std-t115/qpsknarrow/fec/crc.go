package fec

const (
	crc16Poly = 0x1021
	crc16Init = 0xFFFF
)

func CRC16(bits []uint8) uint16 {
	reg := uint16(crc16Init)
	for _, b := range bits {
		reg ^= uint16(b&1) << 15
		if reg&0x8000 != 0 {
			reg = (reg << 1) ^ crc16Poly
		} else {
			reg <<= 1
		}
	}
	return reg
}

func CRC16Bits(bits []uint8) []uint8 {
	return crcToBits(CRC16(bits))
}

func CRC16Linear(bits []uint8) []uint8 {
	reg := uint16(0)
	for _, b := range bits {
		reg ^= uint16(b&1) << 15
		if reg&0x8000 != 0 {
			reg = (reg << 1) ^ crc16Poly
		} else {
			reg <<= 1
		}
	}
	return crcToBits(reg)
}

func crcToBits(v uint16) []uint8 {
	out := make([]uint8, 16)
	for i := range out {
		out[i] = uint8((v >> uint(15-i)) & 1)
	}
	return out
}
