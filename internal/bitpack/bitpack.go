package bitpack

func ToBytes(bits []uint8) []byte {
	out := make([]byte, (len(bits)+7)/8)
	for i, b := range bits {
		if b&1 == 1 {
			out[i>>3] |= 0x80 >> uint(i&7)
		}
	}
	return out
}
