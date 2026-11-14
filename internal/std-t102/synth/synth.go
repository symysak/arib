package synth

import (
	"math/rand"

	nx "github.com/symysak/arib/internal/nxdn/l1"
	"github.com/symysak/arib/internal/std-t102/l1"
	"github.com/symysak/arib/internal/std-t102/l3"
	"github.com/symysak/arib/internal/vocoder/ambe"
)

type VoiceFrame struct {
	M, O, D uint8
	S, U    uint8
	SACCH   [18]uint8
	FACCH1 [2][]uint8
	TCH    [2][]uint8
}

type DataFrame struct {
	O, D uint8
	U    uint8
	Body [22]byte
}

func (v VoiceFrame) Bits() []uint8 {
	out := nx.RICHBitsOf(2, v.M, v.O, v.D)
	out = append(out, nx.SACCH.Encode(l1.SACCHInfo(v.S, v.U, v.SACCH[:]))...)
	for h := 0; h < 2; h++ {
		if v.FACCH1[h] != nil {
			out = append(out, nx.FACCH1.Encode(v.FACCH1[h])...)
		} else {
			t := v.TCH[h]
			if t == nil {
				t = make([]uint8, nx.TCHBits)
			}
			out = append(out, t...)
		}
	}
	return out
}

func (d DataFrame) Bits() []uint8 {
	out := nx.RICHBitsOf(2, 1, d.O, d.D)
	info := append(l1.SACCHInfo(0, d.U, make([]uint8, 18))[:8], nx.BytesToBits(d.Body[:])...)
	return append(out, nx.UDCH1.Encode(info)...)
}

func Call(rng *rand.Rand, u uint8, superframes int) (frames [][]uint8, tch [][]uint8) {
	start := VoiceFrame{M: 0, O: 0, U: u, SACCH: idle18()}
	msg := make([]uint8, 80)
	copy(msg, nx.BytesToBits([]byte{l3.TypeVoice, 0x05}))
	start.FACCH1 = [2][]uint8{msg, msg}
	frames = append(frames, start.Bits())
	voiceMsg := nx.BytesToBits([]byte{l3.TypeVoice, 0x05, 1, 2, 3, 4, 5, 6, 7})
	for sf := 0; sf < superframes; sf++ {
		for k := 0; k < 4; k++ {
			v := VoiceFrame{M: 2, O: 3, S: uint8(3 - k), U: u}
			copy(v.SACCH[:], voiceMsg[18*k:18*k+18])
			for h := 0; h < 2; h++ {
				var t []uint8
				for j := 0; j < 2; j++ {
					var rest [25]uint8
					for i := range rest {
						rest[i] = uint8(rng.Intn(2))
					}
					f := ambe.MakeFrame3600(uint16(rng.Intn(4096)), uint16(rng.Intn(4096)), rest)
					t = append(t, f[:]...)
				}
				v.TCH[h] = t
				tch = append(tch, t)
			}
			frames = append(frames, v.Bits())
		}
	}
	end := VoiceFrame{M: 0, O: 0, U: u, SACCH: idle18()}
	em := make([]uint8, 80)
	copy(em, nx.BytesToBits([]byte{l3.TypeEnd, 0x05}))
	end.FACCH1 = [2][]uint8{em, em}
	frames = append(frames, end.Bits())
	return frames, tch
}

func idle18() (s [18]uint8) {
	copy(s[:], nx.BytesToBits([]byte{l3.TypeIdle, 0x05})[:16])
	return
}
