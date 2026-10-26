package ambeplus


import (
	"fmt"

	"github.com/symysak/arib/internal/std-t79/l1"
	"github.com/symysak/arib/internal/vocoder/ambeplus"
	"github.com/symysak/arib/internal/voice"
)

const NameAMBEPlus = "ambeplus"

const t79TCHBits = 256

func init() {
	voice.Register(voice.Codec{
		Name:      NameAMBEPlus,
		Desc:      "AMBE+ 4000/2400（TCH の偶数番・奇数番がそれぞれ 20 ms の 1 フレーム。奇数番が先の 20 ms。表は近似 = mbelib-neo）",
		Ch:        l1.VoiceChannel,
		Check:     "FEC",
		Available: ambeplus.Available,
		New: func(o voice.Options) (voice.Voice, error) {
			oddFirst := true
			for k, v := range o {
				switch {
				case k == "order" && v == "even":
					oddFirst = false
				case k == "order" && v == "odd":
				case k == "order":
					return nil, fmt.Errorf("order は even / odd: %q", v)
				case k == "hint":
				default:
					return nil, fmt.Errorf("ambeplus に設定 %q はありません（order）", k)
				}
			}
			return &ambePlusVoice{oddFirst: oddFirst}, nil
		},
	})
}

type ambePlusVoice struct {
	oddFirst bool
	stream   *ambeplus.Stream
}

type ambePlusUnit struct {
	params [2][]uint8
	good   [2]bool
}

func (a *ambePlusVoice) Name() string {
	if a.oddFirst {
		return "AMBE+ 4000/2400（奇数番が先）"
	}
	return "AMBE+ 4000/2400（偶数番が先）"
}

func (a *ambePlusVoice) Transport(f voice.Frame) (voice.Unit, bool, error) {
	if len(f.Bits) != t79TCHBits {
		return voice.Unit{}, false, fmt.Errorf("TCH が %d ビットではなく %d", t79TCHBits, len(f.Bits))
	}
	var u ambePlusUnit
	ok, sum := true, 0
	for k := 0; k < 2; k++ {
		first := k
		if a.oddFirst {
			first = 1 - k
		}
		ch := make([]uint8, ambeplus.FrameBits)
		for i := range ch {
			ch[i] = f.Bits[2*i+first]
		}
		p, d := ambeplus.Decode(ch)
		u.params[k], u.good[k] = p, d <= ambeplus.MaxDist
		ok = ok && u.good[k]
		sum += d
	}
	raw := make([]byte, 0, 2*ambeplus.ParamBits/8)
	for _, p := range u.params {
		for i := 0; i < len(p); i += 8 {
			var b byte
			for j := 0; j < 8; j++ {
				b = b<<1 | p[i+j]
			}
			raw = append(raw, b)
		}
	}
	return voice.Unit{Data: u, Check: voice.Check{Valid: true, OK: ok, Corrected: sum}, Raw: raw}, true, nil
}

func (a *ambePlusVoice) Erase() (voice.Unit, bool) {
	return voice.Unit{Data: ambePlusUnit{}}, true
}

func (a *ambePlusVoice) Decode(u voice.Unit) ([]int16, error) {
	d, ok := u.Data.(ambePlusUnit)
	if !ok {
		return nil, fmt.Errorf("AMBE+ の単位ではない: %T", u.Data)
	}
	out := make([]int16, 0, 2*ambeplus.SamplesPerFrame)
	for k := 0; k < 2; k++ {
		if !d.good[k] || allZero(d.params[k]) {
			out = append(out, make([]int16, ambeplus.SamplesPerFrame)...)
			continue
		}
		if a.stream == nil {
			s, err := ambeplus.NewStream()
			if err != nil {
				return nil, err
			}
			a.stream = s
		}
		pcm, err := a.stream.Push(d.params[k])
		if err != nil {
			return nil, err
		}
		out = append(out, pcm...)
	}
	return out, nil
}

func (a *ambePlusVoice) Close() error {
	if a.stream == nil {
		return nil
	}
	err := a.stream.Close()
	a.stream = nil
	return err
}

func allZero(p []uint8) bool {
	for _, b := range p {
		if b != 0 {
			return false
		}
	}
	return true
}
