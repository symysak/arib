package sjis

import (
	_ "embed"
	"encoding/binary"
	"strings"
	"unicode/utf8"
)

//go:embed cp932.bin
var table []byte

const trails = 0xFD - 0x40

func leadIndex(b byte) int {
	switch {
	case b >= 0x81 && b <= 0x9F:
		return int(b - 0x81)
	case b >= 0xE0 && b <= 0xFC:
		return int(b-0xE0) + 31
	}
	return -1
}

func Lookup(a, b byte) (rune, bool) {
	i := leadIndex(a)
	if i < 0 || b < 0x40 || b > 0xFC {
		return 0, false
	}
	k := (i*trails + int(b-0x40)) * 2
	if k+2 > len(table) {
		return 0, false
	}
	r := rune(binary.LittleEndian.Uint16(table[k:]))
	return r, r != 0
}

func Decode(p []byte) (s string, bad int) {
	var sb strings.Builder
	sb.Grow(len(p) * 2)
	for i := 0; i < len(p); i++ {
		c := p[i]
		switch {
		case c < 0x80:
			sb.WriteByte(c)
		case c >= 0xA1 && c <= 0xDF:
			sb.WriteRune(rune(0xFF61 + int(c-0xA1)))
		case leadIndex(c) >= 0 && i+1 < len(p):
			if r, ok := Lookup(c, p[i+1]); ok {
				sb.WriteRune(r)
				i++
				continue
			}
			sb.WriteRune(utf8.RuneError)
			bad++
		default:
			sb.WriteRune(utf8.RuneError)
			bad++
		}
	}
	return sb.String(), bad
}
