package decoder

import "github.com/symysak/arib/internal/std-t61/l1"

var coarseWords = func() []uint32 {
	out := make([]uint32, 0, len(l1.SyncWords))
	for _, sw := range l1.SyncWords {
		var v uint32
		for _, b := range sw.Bits[:20] {
			v = v<<1 | uint32(b&1)
		}
		out = append(out, v)
	}
	return out
}()

const coarseMinSW = 3

func coarseCheck(b []uint8) int {
	var cnt [l1.FrameBits]int
	var w uint32
	bestP, bestN := 0, 0
	for i, v := range b {
		w = (w<<1 | uint32(v&1)) & 0xFFFFF
		if i < 19 {
			continue
		}
		for _, sw := range coarseWords {
			if w == sw {
				p := (i - 19) % l1.FrameBits
				cnt[p]++
				if cnt[p] > bestN {
					bestP, bestN = p, cnt[p]
				}
				break
			}
		}
	}
	if bestN < coarseMinSW {
		return 0
	}
	rich := 0
	soft := make([]float32, l1.RIEnd-l1.RIStart)
	for sw := bestP; sw+20 <= len(b); sw += l1.FrameBits {
		start := sw - l1.SWStart
		if start+l1.RIStart < 0 {
			continue
		}
		for k := range soft {
			soft[k] = 1 - 2*float32(b[start+l1.RIStart+k])
		}
		if l1.DecodeRICH(soft).OK {
			rich++
		}
	}
	return bestN*16 + rich
}
