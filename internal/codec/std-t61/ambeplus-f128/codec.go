package ambeplusf128

import (
	"fmt"
	"strconv"
	"strings"
	"sync"

	"github.com/symysak/arib/internal/std-t61/l1"
	t61voice "github.com/symysak/arib/internal/std-t61/voice"
	"github.com/symysak/arib/internal/vocoder/ambe2-c"
	"github.com/symysak/arib/internal/voice"
)

const Name = "ambeplus-f128"

const Variant = "f128"

func init() {
	voice.Register(voice.Codec{
		Name:      Name,
		Desc:      "AMBE+ 4000/2400（TCH 256 の偶数番・奇数番がそれぞれ 20 ms の 1 フレーム。奇数番が先。TCH のスクランブルの初期値はビタビの距離から当てる。-codec-opt wc=0..511 で固定）",
		Vocoder:   "AMBE+ 4000/2400",
		Transport: "TCH 256 f128（128 ビット × 2・偶数番と奇数番）",
		Ch:        t61voice.TCH,
		Check:     "FEC",
		Available: ambe2c.Available,
		New:       newFromOptions,
	})
}

func newFromOptions(o voice.Options) (voice.Voice, error) {
	wc, hint, oddFirst := -1, -1, true
	for k, s := range o {
		switch k {
		case "wc":
			n, err := strconv.ParseInt(s, 0, 32)
			if err != nil || n < 0 || n >= l1.A1Patterns {
				return nil, fmt.Errorf("wc は 0..511: %q", s)
			}
			wc = int(n)
		case "hint":
			name, w, ok := strings.Cut(s, ":")
			if n, err := strconv.ParseInt(w, 0, 32); ok && name == Name && err == nil && n >= 0 && n < l1.A1Patterns {
				hint = int(n)
			}
		case "order":
			switch s {
			case "even":
				oddFirst = false
			case "odd":
			default:
				return nil, fmt.Errorf("order は even / odd: %q", s)
			}
		default:
			return nil, fmt.Errorf("%s に設定 %q はありません（wc・order）", Name, k)
		}
	}
	return New(wc, hint, oddFirst), nil
}

const (
	decideVotes = 2
	scanEvery = 5
	maxPending = 50
)

type Voice struct {
	pair    ambe2c.Pair4000
	wc      int
	hint    int
	votes   map[int]int
	cand    []int
	sinceN  int
	pend    [][]uint8
	picked  int
	noteWhy string
}

func New(wc, hint int, oddFirst bool) *Voice {
	v := &Voice{pair: ambe2c.Pair4000{OddFirst: oddFirst}, wc: -1, hint: hint, votes: map[int]int{}, picked: -1}
	if wc >= 0 {
		v.wc, v.noteWhy = wc, "（固定）"
	}
	return v
}

func (v *Voice) Name() string { return "AMBE+ 4000/2400（TCH 256 f128）" }

func (v *Voice) Key() (wc int, ok bool) { return v.wc, v.wc >= 0 }

func mask(tch []uint8, init int) []uint8 {
	m := l1.ServiceTCHMask(init)
	out := make([]uint8, len(tch))
	for i := range tch {
		out[i] = (tch[i] ^ m[i]) & 1
	}
	return out
}

func (v *Voice) Transport(f voice.Frame) (voice.Unit, bool, error) {
	if len(f.Bits) != ambe2c.PairBits {
		return voice.Unit{}, false, fmt.Errorf("TCH が %d ビットではなく %d", ambe2c.PairBits, len(f.Bits))
	}
	raw := mask(f.Bits, f.Init)
	if v.wc < 0 {
		v.learn(raw)
		v.pend = append(v.pend, raw)
		if over := len(v.pend) - maxPending; over > 0 {
			v.pend = v.pend[over:]
		}
		v.decide()
		if v.wc < 0 {
			return voice.Unit{}, false, nil
		}
		pend := v.pend
		v.pend = nil
		var units []ambe2c.PairUnit
		var raws []byte
		chk := voice.Check{Valid: true, OK: true}
		for _, p := range pend {
			u, c, r := v.pair.Split(mask(p, v.wc))
			units = append(units, u)
			raws = append(raws, r...)
			chk.OK = chk.OK && c.OK
			chk.Corrected += c.Corrected
		}
		return v.unit(units, chk, raws), true, nil
	}
	u, chk, r := v.pair.Split(mask(raw, v.wc))
	return v.unit([]ambe2c.PairUnit{u}, chk, r), true, nil
}

func (v *Voice) learn(raw []uint8) {
	few := v.cand
	if v.hint >= 0 {
		few = append([]int{v.hint}, few...)
	}
	hits := v.try(raw, few)
	if len(hits) == 0 {
		if len(v.cand) > 0 {
			v.cand, v.sinceN = nil, 0
		}
		if v.sinceN%scanEvery == 0 {
			hits = v.try(raw, allKeys())
		}
		v.sinceN++
	}
	if len(hits) > 0 {
		v.cand, v.sinceN = hits, 0
	}
}

func (v *Voice) try(raw []uint8, keys []int) []int {
	var hits []int
	seen := map[int]bool{}
	for _, wc := range keys {
		if seen[wc] {
			continue
		}
		seen[wc] = true
		if ambe2c.PairOK(mask(raw, wc), v.pair.OddFirst) {
			hits = append(hits, wc)
			v.votes[wc]++
		}
	}
	return hits
}

var allKeys = sync.OnceValue(func() []int {
	k := make([]int, l1.A1Patterns)
	for i := range k {
		k[i] = i
	}
	return k
})

func (v *Voice) decide() {
	if v.hint >= 0 && v.votes[v.hint] >= 1 {
		v.wc, v.noteWhy = v.hint, "（台帳か前の通話の組）"
		return
	}
	best, n := -1, 0
	for wc, c := range v.votes {
		if c > n || (c == n && wc < best) {
			best, n = wc, c
		}
	}
	if n >= decideVotes {
		v.wc, v.noteWhy = best, "（ビタビの距離から）"
	}
}

func (v *Voice) unit(units []ambe2c.PairUnit, chk voice.Check, raw []byte) voice.Unit {
	u := voice.Unit{Data: units, Check: chk, Raw: raw, Note: fmt.Sprintf("%s・wc=%d%s", Variant, v.wc, v.noteWhy)}
	if v.picked != v.wc {
		v.picked = v.wc
		u.Choice = &voice.Choice{Codec: Name, Opts: voice.Options{"wc": strconv.Itoa(v.wc)}}
	}
	return u
}

func (v *Voice) Erase() (voice.Unit, bool) {
	if v.wc < 0 {
		return voice.Unit{}, false
	}
	return v.pair.Erase()
}

func (v *Voice) Decode(u voice.Unit) ([]int16, error) { return v.pair.Decode(u) }

func (v *Voice) Close() error { return v.pair.Close() }
