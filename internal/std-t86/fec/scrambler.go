package fec

import "fmt"


func MunicipalCodeToSeed(code int) (int, error) {
	if code <= 0 {
		return 0, fmt.Errorf("市区町村コードが不正です: %d", code)
	}
	return code & 0x1FF, nil
}

func LFSRPN(seed, length int) []uint8 {
	reg := uint32(seed) & 0x1FF
	out := make([]uint8, length)
	for k := range out {
		out[k] = uint8(reg & 1)
		fb := (reg ^ (reg >> 4)) & 1
		reg = (reg >> 1) | (fb << 8)
	}
	return out
}

func Descramble(input []uint8, seed int) []uint8 {
	pn := LFSRPN(seed, len(input))
	out := make([]uint8, len(input))
	for i, b := range input {
		out[i] = (b & 1) ^ pn[i]
	}
	return out
}

func Scramble(input []uint8, seed int) []uint8 { return Descramble(input, seed) }
