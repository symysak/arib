package scramble

import (
	"math"
	"math/bits"
	"sync"

	"github.com/symysak/arib/internal/vocoder/ambe"
)

func Keystream(key uint16, n int) []uint8 {
	l := uint32(key) & 0x7FFF
	out := make([]uint8, n)
	for i := range out {
		out[i] = uint8(l & 1)
		fb := (l ^ l>>1) & 1
		l = l>>1 | fb<<14
	}
	return out
}

func Descramble(a [ambe.Bits3600]uint8, mask []uint8) [ambe.Bits3600]uint8 {
	d := ambe.Params49(a)
	for i := 0; i < 49 && i < len(mask); i++ {
		d[i] ^= mask[i] & 1
	}
	return ambe.FromParams49(d)
}

const (
	KeyMinUnits = 10
	KeyMaxUnits = 250
	keyZ = 6
	keyGain = 0.9
)

type Pair struct {
	X      uint64
	PA, PB int
}

func FindKey(ps []Pair) (uint16, bool, bool) {
	if len(ps) == 0 {
		return 0, false, false
	}
	base := 0
	sfPos := false
	for _, p := range ps {
		base += bits.OnesCount64(p.X)
		sfPos = sfPos || p.PA >= 4 || p.PB >= 4
	}
	type cand struct {
		key            uint16
		best           int
		mean, sd       float64
		pick, plainWin bool
	}
	scan := func(mask func(key uint16, pos int) uint64) cand {
		best, bestKey := math.MaxInt, uint16(0)
		var sum, sq float64
		for key := uint16(1); key <= 0x7FFF; key++ {
			c := 0
			for _, p := range ps {
				c += bits.OnesCount64(p.X ^ mask(key, p.PA) ^ mask(key, p.PB))
			}
			sum += float64(c)
			sq += float64(c) * float64(c)
			if c < best {
				best, bestKey = c, key
			}
		}
		mean := sum / 0x7FFF
		sd := math.Sqrt(max(sq/0x7FFF-mean*mean, 1e-9))
		return cand{key: bestKey, best: best, mean: mean, sd: sd,
			pick:     float64(best) <= mean-keyZ*sd && float64(best) <= keyGain*float64(base),
			plainWin: float64(base) <= mean-keyZ*sd && base <= best}
	}
	frame := maskTable()
	nx := scan(func(key uint16, pos int) uint64 { return frame[key][pos&3] })
	sf := cand{best: math.MaxInt, plainWin: true}
	if sfPos {
		tab := maskTableSF()
		sf = scan(func(key uint16, pos int) uint64 { return tab[key][pos] })
	}
	switch {
	case nx.pick && (!sf.pick || nx.best <= sf.best):
		return nx.key, false, true
	case sf.pick:
		return sf.key, true, true
	case nx.plainWin && sf.plainWin:
		return 0, false, true
	}
	return 0, false, false
}

var maskTable = sync.OnceValue(func() *[0x8000][4]uint64 {
	t := new([0x8000][4]uint64)
	for key := 1; key < 0x8000; key++ {
		ks := Keystream(uint16(key), 4*49)
		for p := 0; p < 4; p++ {
			for i := 0; i < 49; i++ {
				t[key][p] |= uint64(ks[49*p+i]&1) << i
			}
		}
	}
	return t
})

var maskTableSF = sync.OnceValue(func() *[0x8000][16]uint64 {
	t := new([0x8000][16]uint64)
	for key := 1; key < 0x8000; key++ {
		ks := Keystream(uint16(key), 16*49)
		for p := 0; p < 16; p++ {
			for i := 0; i < 49; i++ {
				t[key][p] |= uint64(ks[49*p+i]&1) << i
			}
		}
	}
	return t
})

func Pack49(d [49]uint8) (x uint64) {
	for i := 0; i < 49; i++ {
		x |= uint64(d[i]&1) << i
	}
	return
}
