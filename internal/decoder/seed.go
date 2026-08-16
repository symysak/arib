package decoder

import (
	"sort"

	"github.com/symysak/stdt86/internal/citycodes"
	"github.com/symysak/stdt86/internal/control"
	"github.com/symysak/stdt86/internal/fec"
)

const (
	seedMinSlots  = 8
	seedTop       = 48
	seedScoreCACs = 12
	seedScorePerFeed = seedTop
	seedWindow       = 96
)

type SeedInfo struct {
	Seed        int
	Score       float64
	SecondScore float64
	Confident   bool
	CRCHits     int
	Known       int
	NSlots      int
	Ranking     []fec.SeedWeight
	Candidates  []citycodes.Candidate
}

type storedSlot struct {
	pos  int
	bits []uint8
	qual BurstQuality
}

type seedSearch struct {
	pre    *fec.SeedSearcher
	slots  []storedSlot
	cands  []int
	cacs   [][]uint8
	scores map[int]control.SeedScore
	cursor int
	budget int
}

func newSeedSearch() *seedSearch {
	return &seedSearch{pre: fec.NewSeedSearcher(seedTop), scores: map[int]control.SeedScore{}}
}

func (s *seedSearch) reset() {
	s.pre = fec.NewSeedSearcher(seedTop)
	s.slots = nil
	s.cands = nil
	s.cacs = nil
	s.scores = map[int]control.SeedScore{}
	s.cursor = 0
	s.budget = 0
}

func (s *seedSearch) beginFeed() { s.budget = seedScorePerFeed }

func (s *seedSearch) startRound() {
	from := maxInt(0, len(s.slots)-seedScoreCACs)
	s.cacs = s.cacs[:0]
	for _, sl := range s.slots[from:] {
		s.cacs = append(s.cacs, control.ExtractCAC(sl.bits, control.CACOffset))
	}
	s.cands = s.pre.Candidates()
	s.scores = map[int]control.SeedScore{}
	s.cursor = 0
}

func (s *seedSearch) push(pos int, bits []uint8, qual BurstQuality) *SeedInfo {
	s.slots = append(s.slots, storedSlot{pos, bits, qual})
	if len(s.slots) > seedWindow {
		s.slots = s.slots[len(s.slots)-seedWindow:]
	}
	s.pre.Push(control.ExtractCAC(bits, control.CACOffset))
	if len(s.slots) < seedMinSlots || s.budget <= 0 {
		return nil
	}
	if len(s.cands) == 0 {
		s.startRound()
	}
	for s.budget > 0 && s.cursor < len(s.cands) {
		c := s.cands[s.cursor]
		s.cursor++
		s.budget--
		s.scores[c] = control.ScoreSeed(s.cacs, c)
		if info := s.confidentInfo(); info != nil {
			return info
		}
	}
	if s.cursor >= len(s.cands) {
		s.cands = nil
	}
	return nil
}

func (s *seedSearch) confidentInfo() *SeedInfo {
	if len(s.scores) < 2 {
		return nil
	}
	ranked := make([]control.SeedScore, 0, len(s.scores))
	for _, v := range s.scores {
		ranked = append(ranked, v)
	}
	sort.Slice(ranked, func(i, j int) bool {
		a, b := ranked[i], ranked[j]
		switch {
		case a.Score != b.Score:
			return a.Score > b.Score
		case a.CRCHits != b.CRCHits:
			return a.CRCHits > b.CRCHits
		case a.Known != b.Known:
			return a.Known > b.Known
		default:
			return a.Seed > b.Seed
		}
	})
	best, second := ranked[0], ranked[1].Score
	n := float64(len(s.cacs))
	minScore := 6.0
	if n*0.5 > minScore {
		minScore = n * 0.5
	}
	secondFloor := second
	if secondFloor < 1.0 {
		secondFloor = 1.0
	}
	if best.CRCHits < 1 || best.Score < minScore || best.Score < 1.5*secondFloor {
		return nil
	}
	return &SeedInfo{
		Seed: best.Seed, Score: best.Score, SecondScore: second,
		Confident: true, CRCHits: best.CRCHits, Known: best.Known,
		NSlots: len(s.cacs), Ranking: s.pre.Ranking(5),
		Candidates: control.CandidatesForSeed(best.Seed),
	}
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
