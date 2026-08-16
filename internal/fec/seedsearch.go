package fec

import (
	"math/bits"
	"sort"
	"sync"
)


const (
	NSeeds = 512
	CACLen = 256

	synLen   = CACLen/2 + ControlK - 1
	synWords = (synLen + 63) / 64
)

type syndromeBits [synWords]uint64

func polyTaps(poly, K int) []uint8 {
	t := make([]uint8, K)
	for k := 0; k < K; k++ {
		t[k] = uint8((poly >> uint(k)) & 1)
	}
	return t
}

var (
	g1Taps = polyTaps(ControlPoly1, ControlK)
	g2Taps = polyTaps(ControlPoly2, ControlK)
)

func syndromeOf(cac []uint8) syndromeBits {
	var s syndromeBits
	for k := 0; k < len(cac)/2; k++ {
		v1 := cac[2*k] & 1
		v2 := cac[2*k+1] & 1
		for j := 0; j < ControlK; j++ {
			var bit uint8
			if v1 != 0 && g2Taps[j] != 0 {
				bit ^= 1
			}
			if v2 != 0 && g1Taps[j] != 0 {
				bit ^= 1
			}
			if bit != 0 {
				i := k + j
				s[i>>6] ^= 1 << uint(i&63)
			}
		}
	}
	return s
}

func Syndrome(cac []uint8) []uint8 {
	s := syndromeOf(cac)
	out := make([]uint8, synLen)
	for i := range out {
		out[i] = uint8((s[i>>6] >> uint(i&63)) & 1)
	}
	return out
}

var (
	pnSynOnce  sync.Once
	pnSynTable [NSeeds]syndromeBits
)

func pnSyndromes() *[NSeeds]syndromeBits {
	pnSynOnce.Do(func() {
		for s := 0; s < NSeeds; s++ {
			pnSynTable[s] = syndromeOf(LFSRPN(s, CACLen))
		}
	})
	return &pnSynTable
}

type SeedSearcher struct {
	Top     int
	weights [NSeeds]int64
	nSlots  int
}

func NewSeedSearcher(top int) *SeedSearcher {
	if top <= 0 {
		top = 8
	}
	return &SeedSearcher{Top: top}
}

func (ss *SeedSearcher) Push(cac []uint8) {
	syn := syndromeOf(cac)
	tab := pnSyndromes()
	for s := 0; s < NSeeds; s++ {
		w := 0
		for w2 := 0; w2 < synWords; w2++ {
			w += bits.OnesCount64(tab[s][w2] ^ syn[w2])
		}
		ss.weights[s] += int64(w)
	}
	ss.nSlots++
}

func (ss *SeedSearcher) NSlots() int { return ss.nSlots }

func (ss *SeedSearcher) order() []int {
	idx := make([]int, NSeeds)
	for i := range idx {
		idx[i] = i
	}
	sort.SliceStable(idx, func(a, b int) bool {
		return ss.weights[idx[a]] < ss.weights[idx[b]]
	})
	return idx
}

func (ss *SeedSearcher) Candidates() []int {
	idx := ss.order()
	if len(idx) > ss.Top {
		idx = idx[:ss.Top]
	}
	return idx
}

type SeedWeight struct {
	Seed   int
	Weight int64
}

func (ss *SeedSearcher) Ranking(top int) []SeedWeight {
	idx := ss.order()
	if len(idx) > top {
		idx = idx[:top]
	}
	out := make([]SeedWeight, len(idx))
	for i, s := range idx {
		out[i] = SeedWeight{Seed: s, Weight: ss.weights[s]}
	}
	return out
}

func (ss *SeedSearcher) Weights() []int64 {
	out := make([]int64, NSeeds)
	copy(out, ss.weights[:])
	return out
}

func SearchSeed(cacs [][]uint8) (int, []int64) {
	ss := NewSeedSearcher(1)
	for _, c := range cacs {
		ss.Push(c)
	}
	best := 0
	w := ss.Weights()
	for s := 1; s < NSeeds; s++ {
		if w[s] < w[best] {
			best = s
		}
	}
	return best, w
}
