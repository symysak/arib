package ambe

type Rate int

const (
	Rate2400    Rate = 0
	Rate2400FEC Rate = 22
	Rate4000    Rate = 17
	Rate4000FEC Rate = 26
	Rate2000    Rate = 31
	Rate3600    Rate = 33
	Rate2450    Rate = 34
)

var bits = map[Rate]int{Rate2400: 48, Rate2400FEC: 80, Rate4000: 80, Rate4000FEC: Bits4000FEC, Rate2000: 40, Rate3600: Bits3600, Rate2450: 49}

func (r Rate) Bits() int { return bits[r] }

const SampleRate = 8000

const SamplesPerFrame = 160
