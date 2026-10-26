package decoder


import (
	"math/rand"

	"github.com/symysak/arib/internal/std-t79/l1"
)

type SynthCarrier struct {
	Bits    []uint8
	Voice   map[string]bool
	SACCH   []uint8
	FACCH   []uint8
	Message []byte
}

const (
	SynthCallAt  = 30
	SynthCC      = 21
	SynthPattern = 0x5A
)

var (
	SynthRCH   = []uint8{0, 0, 1, 0, 0, 0, 1, 1, 0, 0, 0, 0, 0, 0}
	SynthRCHUp = []uint8{0, 1, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1, 0}
)

func Synth(frames int, r *rand.Rand) SynthCarrier { return SynthWith(frames, r, nil) }

func SynthWith(frames int, r *rand.Rand, voice func(k int) []uint8) SynthCarrier {
	nVoice := 0
	c := SynthCarrier{Voice: map[string]bool{}}
	c.Message = []byte("T79 SYNTH BCCH 0123456789ABCDEF")
	var units [][]uint8
	var ws []l1.W
	for i := 0; i < 3; i++ {
		u := make([]byte, 11)
		copy(u, c.Message[i*11:])
		bits := make([]uint8, 88)
		for k := range bits {
			bits[k] = u[k/8] >> (7 - k%8) & 1
		}
		units = append(units, bits)
	}
	ws = []l1.W{{First: true, WO: 2}, {WO: 1}, {Last: true, WO: 31 - 22}}
	c.SACCH = make([]uint8, 48)
	for i := range c.SACCH {
		c.SACCH[i] = uint8(r.Intn(2))
	}
	sacchTx := l1.EncodeSACCH(l1.W{First: true, Last: true, WO: 6}, c.SACCH)
	c.FACCH = make([]uint8, 96)
	for i := range c.FACCH {
		c.FACCH[i] = uint8(r.Intn(2))
	}
	facchY := l1.EncodeFACCH(l1.W{First: true, Last: true, WO: 12}, c.FACCH)
	rchA, rchB := l1.EncodeRCH(SynthRCH)
	init := l1.InitFromPattern(SynthPattern)
	for f := 0; f < frames; f++ {
		sf := f % l1.FramesPerSuper
		for n := 0; n < 4; n++ {
			var slot []uint8
			switch n {
			case 0, 2:
				slot = l1.BuildTraffic(l1.SW{Kind: l1.SWIdle, N: 9 + n}, 0, SynthCC, make([]uint8, 256), make([]uint8, 20), 0, l1.InitAllOnes)
			case 3:
				sw := l1.SW{Kind: l1.SWNormal, N: 4}
				if sf == 0 {
					sw = l1.SW{Kind: l1.SWSuper, N: 8}
				}
				k := sf % 6
				if k < 3 {
					slot = l1.BuildCAC(sw, SynthCC, ws[k], units[k], sf+1)
				} else {
					slot = l1.BuildCAC(sw, SynthCC, l1.W{First: true, Last: true, WO: 0}, make([]uint8, 88), sf+1)
				}
			case 1:
				switch {
				case f < SynthCallAt:
					sw := l1.SW{Kind: l1.SWIdle, N: 10}
					slot = l1.BuildTraffic(sw, 0, SynthCC, make([]uint8, 256), make([]uint8, 20), 0, l1.InitAllOnes)
				case f < SynthCallAt+2:
					slot = l1.BuildSyncBurst(2, l1.SyncBurst{CI: 0, CC: SynthCC, B: f - SynthCallAt, ID: 1<<19 | 4242})
				case f == SynthCallAt+2 || f == SynthCallAt+3:
					tch := make([]uint8, 256)
					for i := range tch {
						tch[i] = uint8(r.Intn(2))
					}
					l1.PlaceFACCH(tch, facchY, f == SynthCallAt+3)
					slot = l1.BuildTraffic(l1.SW{Kind: l1.SWNormal, N: 2, Inverted: true}, 0, SynthCC, tch, make([]uint8, 20), 1, init)
				default:
					tch := make([]uint8, 256)
					if voice != nil {
						copy(tch, voice(nVoice))
					} else {
						for i := range tch {
							tch[i] = uint8(r.Intn(2))
						}
					}
					nVoice++
					c.Voice[string(tch)] = true
					sw := l1.SW{Kind: l1.SWNormal, N: 2}
					if sf == 0 {
						sw = l1.SW{Kind: l1.SWSuper, N: 6}
					}
					var sa []uint8
					switch u, k := l1.SACCHPart(sf); {
					case u >= 0:
						sa = sacchTx[20*k : 20*k+20]
					case sf == 0:
						sa = rchA
					default:
						sa = rchB
					}
					slot = l1.BuildTraffic(sw, 0, SynthCC, tch, sa, 1, init)
				}
			}
			c.Bits = append(c.Bits, slot...)
		}
	}
	return c
}
