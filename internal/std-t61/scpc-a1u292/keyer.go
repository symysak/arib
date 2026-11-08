package scpca1u292

const (
	IdleMaxErr = 3
	Hold = 16
	LoseAfter = 32
	SlowAfter = 32
	SlowEvery = 16
)

type Item struct {
	TCH1 []uint8
	Soft []float32
	Tag  any
}

type Decoded struct {
	Item
	Result
}

type Keyer struct {
	wc      int
	decided bool
	fixed   bool
	hint    int
	votes   map[int]int
	held    []Item
	misses  int
	tries   int
}

func NewKeyer(wc, hint int) *Keyer {
	k := &Keyer{hint: -1, votes: map[int]int{}}
	if wc >= 0 && wc < Patterns {
		k.wc, k.decided, k.fixed = wc, true, true
	} else if hint >= 0 && hint < Patterns {
		k.hint = hint
	}
	return k
}

func (k *Keyer) WC() (int, bool) { return k.wc, k.decided }

func (k *Keyer) Feed(it Item) []Decoded {
	if len(it.TCH1) != Bits {
		return nil
	}
	if k.decided {
		r := Decode(it.TCH1, it.Soft, k.wc)
		if r.OK {
			k.misses = 0
		} else if k.misses++; !k.fixed && k.misses >= LoseAfter {
			k.decided, k.misses, k.votes = false, 0, map[int]int{}
		}
		return []Decoded{{it, r}}
	}
	k.held = append(k.held, it)
	if len(k.held) > Hold {
		k.held = k.held[len(k.held)-Hold:]
	}
	if wc, ok := KeyFromIdle(it.TCH1, IdleMaxErr); ok {
		return k.decide(wc)
	}
	if k.hint >= 0 && Decode(it.TCH1, it.Soft, k.hint).OK {
		return k.decide(k.hint)
	}
	k.tries++
	if k.tries > SlowAfter && len(k.votes) == 0 && k.tries%SlowEvery != 0 {
		return nil
	}
	for _, wc := range KeyByCRC(it.TCH1, it.Soft) {
		if k.votes[wc]++; k.votes[wc] >= 2 {
			return k.decide(wc)
		}
	}
	return nil
}

func (k *Keyer) decide(wc int) []Decoded {
	k.wc, k.decided, k.misses = wc, true, 0
	out := make([]Decoded, 0, len(k.held))
	for _, h := range k.held {
		out = append(out, Decoded{h, Decode(h.TCH1, h.Soft, wc)})
	}
	k.held = nil
	return out
}
