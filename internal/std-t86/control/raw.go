package control

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

func hexToBits(s string) ([]uint8, error) {
	out := make([]uint8, 0, len(s)*4)
	for i := 0; i < len(s); i++ {
		var v int
		switch c := s[i]; {
		case c >= '0' && c <= '9':
			v = int(c - '0')
		case c >= 'a' && c <= 'f':
			v = int(c-'a') + 10
		case c >= 'A' && c <= 'F':
			v = int(c-'A') + 10
		default:
			return nil, fmt.Errorf("hex ではない文字 %q", string(c))
		}
		out = append(out, uint8((v>>3)&1), uint8((v>>2)&1), uint8((v>>1)&1), uint8(v&1))
	}
	return out, nil
}

func scanTokens(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var toks []string
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	for sc.Scan() {
		toks = append(toks, strings.Fields(sc.Text())...)
	}
	return toks, sc.Err()
}

func LoadRawSlots(path string, slotsPerFrame int) ([][]uint8, error) {
	if slotsPerFrame <= 0 {
		slotsPerFrame = 6
	}
	toks, err := scanTokens(path)
	if err != nil {
		return nil, err
	}
	want := BitsPerSlot / 4
	var out [][]uint8
	for i := 0; i < len(toks); i += slotsPerFrame {
		t := toks[i]
		if len(t) != want {
			continue
		}
		bits, err := hexToBits(t)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		out = append(out, bits)
	}
	return out, nil
}

func LoadHexLines(path string, bitLen int) ([][]uint8, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var out [][]uint8
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	for sc.Scan() {
		t := strings.TrimSpace(sc.Text())
		if len(t) != bitLen/4 {
			continue
		}
		bits, err := hexToBits(t)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		out = append(out, bits)
	}
	return out, sc.Err()
}
