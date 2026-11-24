package ambeplus


import (
	"fmt"

	"github.com/symysak/arib/internal/std-t79/l1"
	"github.com/symysak/arib/internal/vocoder/ambe2-c"
	"github.com/symysak/arib/internal/voice"
)

const NameAMBEPlus = "ambeplus"

const t79TCHBits = ambe2c.PairBits

func init() {
	voice.Register(voice.Codec{
		Name:      NameAMBEPlus,
		Desc:      "AMBE+ 4000/2400（TCH の偶数番・奇数番がそれぞれ 20 ms の 1 フレーム。奇数番が先の 20 ms。復号は ambe2-c の RATET 26）",
		Ch:        l1.VoiceChannel,
		Check:     "FEC",
		Available: ambe2c.Available,
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
			return &ambePlusVoice{Pair4000: &ambe2c.Pair4000{OddFirst: oddFirst}}, nil
		},
	})
}

type ambePlusVoice struct {
	*ambe2c.Pair4000
}

func (a *ambePlusVoice) Name() string {
	if a.OddFirst {
		return "AMBE+ 4000/2400（奇数番が先）"
	}
	return "AMBE+ 4000/2400（偶数番が先）"
}
