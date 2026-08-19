package control

import (
	"sort"

	"github.com/symysak/arib/internal/std-t86/citycodes"
	"github.com/symysak/arib/internal/std-t86/fec"
)

type SeedScore struct {
	Seed    int
	Score   float64
	CRCHits int
	Known   int
}

func ScoreSeed(cacs [][]uint8, seed int) SeedScore {
	var crcHits, known, mfr int
	for _, cac := range cacs {
		payload, crcOK, err := DecodeCAC(cac, seed)
		if err != nil {
			continue
		}
		msg := ParseMessage(payload, 0)
		if crcOK {
			crcHits++
		}
		if KnownType(msg.Type) {
			known++
			if msg.BCCH != nil {
				if _, ok := Manufacturers[msg.BCCH.ManufacturerCode]; ok {
					mfr++
				}
			}
		}
	}
	return SeedScore{
		Seed:    seed,
		Score:   3.0*float64(crcHits) + float64(known) + 2.0*float64(mfr),
		CRCHits: crcHits,
		Known:   known,
	}
}

type SeedInfo struct {
	Score       float64
	SecondScore float64
	Confident  bool
	CRCHits    int
	Known      int
	NSlots     int
	Ranking    []fec.SeedWeight
	Candidates []citycodes.Candidate
}

const DetectSeedTop = 48

func DetectSeed(cacs [][]uint8, top int) (int, SeedInfo) {
	if top <= 0 {
		top = DetectSeedTop
	}
	ss := fec.NewSeedSearcher(top)
	for _, cac := range cacs {
		ss.Push(cac)
	}
	cands := ss.Candidates()
	scored := make([]SeedScore, 0, len(cands))
	for _, s := range cands {
		scored = append(scored, ScoreSeed(cacs, s))
	}
	sort.Slice(scored, func(i, j int) bool {
		a, b := scored[i], scored[j]
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
	if len(scored) == 0 {
		return 0, SeedInfo{NSlots: len(cacs), Ranking: ss.Ranking(5)}
	}
	best := scored[0]
	var second float64
	if len(scored) > 1 {
		second = scored[1].Score
	}
	minScore := 6.0
	if v := float64(len(cacs)) * 0.5; v > minScore {
		minScore = v
	}
	secondFloor := second
	if secondFloor < 1.0 {
		secondFloor = 1.0
	}
	return best.Seed, SeedInfo{
		Score:       best.Score,
		SecondScore: second,
		Confident:   best.Score >= minScore && best.Score >= 1.5*secondFloor,
		CRCHits:     best.CRCHits,
		Known:       best.Known,
		NSlots:      len(cacs),
		Ranking:     ss.Ranking(5),
		Candidates:  CandidatesForSeed(best.Seed),
	}
}
