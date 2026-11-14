package synth

import (
	"math/rand"

	nx "github.com/symysak/arib/internal/nxdn/l1"
	"github.com/symysak/arib/internal/nxdn/scramble"
	"github.com/symysak/arib/internal/std-t98/fsk4/l1"
	"github.com/symysak/arib/internal/std-t98/fsk4/l3"
	"github.com/symysak/arib/internal/vocoder/ambe"
)

type ID struct{ Call, UC, Maker int }

func (id ID) sacch(typ uint8) []uint8 {
	return nx.SACCH.Encode(nx.HeadInfo(l1.Unit{First: true, Type: typ}.Head(), l3.Info(id.Call, id.UC, id.Maker)))
}

func frame(f, m uint8, sacch, a, b []uint8) []uint8 {
	out := append(l1.RICHBits(f, m, 0), sacch...)
	out = append(out, a...)
	return append(out, b...)
}

func SB0(id ID, csm string) []uint8 {
	return frame(0, 4, id.sacch(l3.TypeIdle), nx.FACCH1.Encode(l3.PICHInfo(csm)), make([]uint8, nx.TCHBits))
}

func Voice(id ID, tch1, tch2 []uint8) []uint8 {
	return frame(1, 3, id.sacch(l3.TypeVoice), tch1, tch2)
}

func End(id ID) []uint8 {
	return frame(1, 5, id.sacch(l3.TypeEnd), make([]uint8, nx.TCHBits), make([]uint8, nx.TCHBits))
}

func NonVoice2(head uint8, data []uint8, a, b [10]byte) []uint8 {
	return frame(1, 4, nx.SACCH.Encode(nx.HeadInfo(head, data)), nx.FACCH1.Encode(nx.BytesToBits(a[:])), nx.FACCH1.Encode(nx.BytesToBits(b[:])))
}

func SmoothTCH(rng *rand.Rand, key uint16, n int) [][]uint8 {
	ks := scramble.Keystream(key, 4*49)
	var d [49]uint8
	for i := range d {
		d[i] = uint8(rng.Intn(2))
	}
	var out [][]uint8
	for f := 0; f < n; f++ {
		for h := 0; h < 2; h++ {
			bits := make([]uint8, 0, nx.TCHBits)
			for k := 0; k < 2; k++ {
				for j := 0; j < 2; j++ {
					d[rng.Intn(49)] ^= 1
				}
				s := d
				if key != 0 {
					for i := range s {
						s[i] ^= ks[49*(2*h+k)+i]
					}
				}
				a := ambe.FromParams49(s)
				bits = append(bits, a[:]...)
			}
			out = append(out, bits)
		}
	}
	return out
}

func Call(rng *rand.Rand, id ID, csm string, key uint16, n int) (frames [][]uint8, tch [][]uint8) {
	tch = SmoothTCH(rng, key, n)
	frames = append(frames, SB0(id, csm))
	for i := 0; i < n; i++ {
		frames = append(frames, Voice(id, tch[2*i], tch[2*i+1]))
	}
	return append(frames, End(id)), tch
}
