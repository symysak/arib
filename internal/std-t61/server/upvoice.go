package server


import (
	"fmt"
	"sort"

	dec "github.com/symysak/arib/internal/std-t61/decoder"
	"github.com/symysak/arib/internal/std-t61/l1"
	t61voice "github.com/symysak/arib/internal/std-t61/voice"
	"github.com/symysak/arib/internal/voice"
)

const (
	upHoldMin = 8
	upHoldMax = 24
	upQuiet = 25
	upRetry = 16
	upMinChecks = 4
	upSecondRatio = 3
)

type upHeld struct {
	f       dec.Frame
	vf      voice.Frame
	isVoice bool
}

type upGate struct {
	held     []upHeld
	verified bool
	quiet    int
	retry    int
}

func (r *voiceRun) upProbers() []voice.Codec {
	if r.chosen != nil {
		if r.chosen.Probe != nil && r.chosen.Rank != nil && r.chosen.Ch == t61voice.TCH {
			return []voice.Codec{*r.chosen}
		}
		return nil
	}
	var out []voice.Codec
	for _, s := range r.cands {
		if s.c.Probe != nil && s.c.Rank != nil && s.c.Ch == t61voice.TCH {
			out = append(out, s.c)
		}
	}
	return out
}

func (r *voiceRun) upFrame(f dec.Frame, vf voice.Frame, isVoice bool) bool {
	g := &r.up
	if !isVoice {
		if g.quiet++; g.quiet >= upQuiet {
			g.verified = false
		}
		if len(g.held) == 0 {
			return false
		}
		g.held = append(g.held, upHeld{f, vf, false})
		if len(g.held) >= upHoldMax {
			r.resolveUp()
		}
		return true
	}
	g.quiet = 0
	if g.verified {
		return false
	}
	if g.retry > 0 {
		g.retry--
		return false
	}
	if len(r.upProbers()) == 0 {
		return false
	}
	g.held = append(g.held, upHeld{f, vf, true})
	n := 0
	for _, h := range g.held {
		if h.isVoice {
			n++
		}
	}
	if n >= upHoldMin || len(g.held) >= upHoldMax {
		r.resolveUp()
	}
	return true
}

func redescramble(vf voice.Frame, from, to int) voice.Frame {
	a, b := l1.ServiceTCHMask(from), l1.ServiceTCHMask(to)
	out := vf
	out.Bits = append([]uint8(nil), vf.Bits...)
	if vf.Soft != nil {
		out.Soft = append([]float32(nil), vf.Soft...)
	}
	for i := range out.Bits {
		if a[i] != b[i] {
			out.Bits[i] ^= 1
			if out.Soft != nil {
				out.Soft[i] = -out.Soft[i]
			}
		}
	}
	return out
}

func (r *voiceRun) resolveUp() {
	g := &r.up
	var frames []voice.Frame
	var inits []int
	for _, h := range g.held {
		if h.isVoice {
			frames = append(frames, h.vf)
			inits = append(inits, h.f.TCHInit)
		}
	}
	probers := r.upProbers()
	for _, c := range probers {
		if ok, n := c.Probe(frames); n >= upMinChecks && 2*ok >= n {
			g.verified = true
			r.flushUp()
			return
		}
	}
	best, why, ok := r.searchUp(probers, frames, inits)
	if !ok {
		g.retry = upRetry
		r.flushUp()
		return
	}
	c := probers[best.c]
	if r.setUpScramble != nil {
		r.setUpScramble(best.init)
	}
	r.p.addLogFor(r.idx, "call", "上りの音声フレームからスクランブルを当てた（PN 初期値 0x%03X・%s）", best.init, why)
	r.p.notice("%.4f MHz: 上りの音声フレームからスクランブルを当てた（PN 初期値 0x%03X・%s）。この局の上りに使う", r.mhz, best.init, why)
	r.event(r.curT, "voice_note", fmt.Sprintf("上りの音声フレームからスクランブルを当てた（PN 初期値 0x%03X・%s）", best.init, why))
	for i := range g.held {
		if h := &g.held[i]; h.isVoice && h.f.TCHInit != best.init {
			h.vf = redescramble(h.vf, h.f.TCHInit, best.init)
			h.f.TCHInit = best.init
		}
	}
	if r.chosen == nil {
		r.start(&c, c.Name, "上りの音声フレームの検査 "+why)
	}
	g.verified = true
	r.flushUp()
}

type upScore struct {
	c, init int
	ok, n   int
}

const upRankTop = 4

func (r *voiceRun) searchUp(probers []voice.Codec, frames []voice.Frame, inits []int) (best upScore, why string, ok bool) {
	last := inits[len(inits)-1]
	var use []voice.Frame
	for i, vf := range frames {
		if inits[i] == last {
			use = append(use, vf)
		}
	}
	from := l1.ServiceTCHMask(last)
	masks := make([][]uint8, 512)
	for s := 1; s < 512; s++ {
		to := l1.ServiceTCHMask(s)
		m := make([]uint8, len(from))
		for i := range m {
			m[i] = from[i] ^ to[i]
		}
		masks[s] = m
	}
	best = upScore{c: -1, init: -1}
	secondOK := 0
	try := make([]voice.Frame, len(use))
	for ci, c := range probers {
		if c.Rank == nil {
			continue
		}
		rank := c.Rank(use, masks[1:])
		idx := make([]int, 0, 511)
		for s := 1; s < 512; s++ {
			if rank[s-1] >= 0 {
				idx = append(idx, s)
			}
		}
		sort.Slice(idx, func(a, b int) bool { return rank[idx[a]-1] < rank[idx[b]-1] })
		if len(idx) > upRankTop {
			idx = idx[:upRankTop]
		}
		for _, s := range idx {
			for i, vf := range use {
				try[i] = redescramble(vf, last, s)
			}
			okN, n := c.Probe(try)
			switch {
			case okN > best.ok:
				secondOK = best.ok
				best = upScore{c: ci, init: s, ok: okN, n: n}
			case okN > secondOK:
				secondOK = okN
			}
		}
	}
	if best.c < 0 || best.n < upMinChecks || 2*best.ok < best.n || best.ok < upSecondRatio*secondOK {
		return best, "", false
	}
	c := probers[best.c]
	return best, fmt.Sprintf("%s %s %d/%d・2 番目 %d", c.Name, c.Check, best.ok, best.n, secondOK), true
}

func (r *voiceRun) flushUp() {
	held := r.up.held
	r.up.held = nil
	for _, h := range held {
		r.deliver(h.f, h.vf, h.isVoice)
	}
}
