package qpsk


import (
	"math/rand"

	"github.com/symysak/arib/internal/rcr39qpsk/l1"
)

type SynthCarrier struct {
	Bits    []uint8
	Voice   map[string]bool
	SACCH   []uint8
	FACCH   []uint8
	Message []byte
	FACCHFrame int
}

const (
	SynthCallAt       = 30
	SynthCC           = 45
	SynthPattern      = 0x5A
	SynthOtherPattern = 0x3C
)

func Synth(frames int, r *rand.Rand) SynthCarrier { return SynthWith(frames, r, -1) }

func SynthWith(frames int, r *rand.Rand, cacInit int) SynthCarrier {
	c := SynthCarrier{Voice: map[string]bool{}, FACCHFrame: SynthCallAt + 2}
	c.Message = []byte("STD39 SYNTH BCCH 0123456789ABCD")
	var units [][]uint8
	for i := 0; i < 3; i++ {
		u := make([]byte, 11)
		copy(u, c.Message[i*11:])
		bits := make([]uint8, 88)
		for k := range bits {
			bits[k] = u[k/8] >> (7 - k%8) & 1
		}
		units = append(units, bits)
	}
	ws := []l1.W{{First: true, WO: 2}, {WO: 1}, {Last: true, WO: 31 - 22}}
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
	rchA, rchB := l1.EncodeRCH([]uint8{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0})
	ccInit := l1.InitFromCC(SynthCC)
	ctlInit := ccInit
	if cacInit >= 0 {
		ctlInit = cacInit
	}
	callInit := l1.InitFromPattern(SynthPattern)
	otherInit := l1.InitFromPattern(SynthOtherPattern)
	const ci = 3
	sw := func(n, sf int) l1.SW {
		if sf == 0 {
			return l1.SW{Kind: l1.SWSuper, N: 5 + n}
		}
		return l1.SW{Kind: l1.SWNormal, N: 1 + n}
	}
	sacchOf := func(sf int) []uint8 {
		switch u, k := l1.SACCHPart(sf); {
		case u >= 0:
			return sacchTx[20*k : 20*k+20]
		case sf == 0:
			return rchA
		default:
			return rchB
		}
	}
	for f := 0; f < frames; f++ {
		sf := f % l1.FramesPerSuper
		for n := 0; n < 4; n++ {
			var slot []uint8
			switch n {
			case 0:
				tch := make([]uint8, 256)
				for i := range tch {
					tch[i] = uint8(r.Intn(2))
				}
				junk := make([]uint8, 20)
				for i := range junk {
					junk[i] = uint8(r.Intn(2))
				}
				slot = l1.BuildTraffic(sw(0, sf), ci, SynthCC, tch, junk, 1, otherInit)
			case 1:
				k := sf % 6
				if k < 3 {
					slot = l1.BuildCACAt(sw(1, sf), ci, SynthCC, ws[k], units[k], sf+1, ctlInit)
				} else {
					slot = l1.BuildCACAt(sw(1, sf), ci, SynthCC, l1.W{First: true, Last: true, WO: 0}, make([]uint8, 88), sf+1, ctlInit)
				}
			case 3:
				slot = l1.BuildTraffic(l1.SW{Kind: l1.SWIdle, N: 12}, ci, SynthCC, make([]uint8, 256), make([]uint8, 20), 0, ccInit)
			case 2:
				switch {
				case f < SynthCallAt:
					slot = l1.BuildTraffic(l1.SW{Kind: l1.SWIdle, N: 11}, ci, SynthCC, make([]uint8, 256), make([]uint8, 20), 0, ccInit)
				case f < SynthCallAt+2:
					slot = l1.BuildSyncBurst(3, l1.SyncBurst{CI: ci, CC: SynthCC, B: f - SynthCallAt, ID: 1<<19 | 3939})
				default:
					tch := make([]uint8, 256)
					for i := range tch {
						tch[i] = uint8(r.Intn(2))
					}
					switch f {
					case c.FACCHFrame, c.FACCHFrame + 1:
						l1.PlaceFACCH(tch, facchY, f == c.FACCHFrame+1)
					default:
						c.Voice[string(tch)] = true
					}
					slot = l1.BuildTraffic(sw(2, sf), ci, SynthCC, tch, sacchOf(sf), 1, callInit)
				}
			}
			c.Bits = append(c.Bits, slot...)
		}
	}
	return c
}
