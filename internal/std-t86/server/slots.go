package server

import (
	"math"

	"github.com/symysak/arib/internal/std-t86/decoder"
	"github.com/symysak/arib/internal/std-t86/dsp"
)


const (
	slotCount   = 6
	slotSamples = dsp.SymbolsPerSlot * dsp.SPS
	slotWindowS = 0.4
)

var slotKindOrder = []string{"音声", "FACCH", "同期バースト", "制御", "空線"}

func slotKindOf(b decoder.BurstInfo) string {
	switch b.SW {
	case "S1", "S5":
		return "制御"
	case "S6":
		return "同期バースト"
	}
	switch {
	case dsp.IsVoiceType(b.CType):
		return "音声"
	case b.CType == "FACCH":
		return "FACCH"
	}
	return "空線"
}

type slotHit struct {
	t    float64
	slot int
	kind string
}

type slotBoard struct {
	anchor int
	have   bool
	hits   []slotHit
	last   float64
}

func (b *slotBoard) reset() {
	b.have = false
	b.hits = b.hits[:0]
}

func (b *slotBoard) add(bu decoder.BurstInfo, t float64) {
	b.last = max(b.last, t)
	if bu.SW == "S5" {
		b.anchor, b.have = bu.Pos, true
	}
	if !b.have {
		return
	}
	n := int(math.Round(float64(bu.Pos-b.anchor) / slotSamples))
	n = ((n % slotCount) + slotCount) % slotCount
	b.hits = append(b.hits, slotHit{t: t, slot: n, kind: slotKindOf(bu)})
}

func (b *slotBoard) view(now float64, synced, broadcast bool) [slotCount]string {
	now = max(now, b.last)
	cut := 0
	for cut < len(b.hits) && b.hits[cut].t < now-slotWindowS {
		cut++
	}
	b.hits = append(b.hits[:0], b.hits[cut:]...)

	var out [slotCount]string
	var counts [slotCount]map[string]int
	for _, h := range b.hits {
		if counts[h.slot] == nil {
			counts[h.slot] = map[string]int{}
		}
		counts[h.slot][h.kind]++
	}
	for n := range out {
		switch {
		case !synced:
			out[n] = "未同期"
			continue
		case !b.have:
			out[n] = "番号未確定"
			continue
		}
		best, bestN := "送信なし", 0
		for _, k := range slotKindOrder {
			if c := counts[n][k]; c > bestN {
				best, bestN = k, c
			}
		}
		if broadcast && best == "音声" {
			best = "通報:" + best
		}
		out[n] = best
	}
	return out
}
