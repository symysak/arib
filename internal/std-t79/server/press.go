package server


import (
	"fmt"
	"math"
	"strings"
	"sync"

	"github.com/symysak/arib/internal/std-t79/decoder"
	"github.com/symysak/arib/internal/std-t79/l1"
	"github.com/symysak/arib/internal/std-t79/numbering"
)

const pressGapSec = 3.0

type pressKey struct {
	base int64
	slot int
}

type pressEv struct {
	kind string
	t    float64
	ta   int
	id   int
}

type pressSeq struct {
	evs  []pressEv
	done bool
	talker  int
	talkerT float64
}

type pressBoard struct {
	mu   sync.Mutex
	seqs map[pressKey]*pressSeq
}

func pressKeyOf(downMHz float64, slot int) pressKey {
	return pressKey{base: int64(math.Round(downMHz * 1e6 / RasterHz)), slot: slot}
}

func (b *pressBoard) note(k pressKey, ev pressEv) (summary string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.seqs == nil {
		b.seqs = map[pressKey]*pressSeq{}
	}
	sq := b.seqs[k]
	if sq == nil || (len(sq.evs) > 0 && ev.t-sq.evs[len(sq.evs)-1].t > pressGapSec) {
		talker, tt := -1, 0.0
		if sq != nil {
			talker, tt = sq.talker, sq.talkerT
		}
		sq = &pressSeq{talker: talker, talkerT: tt}
		b.seqs[k] = sq
	}
	if ev.kind == "SB2" || ev.kind == "SB4" {
		sq.talker, sq.talkerT = ev.id, ev.t
	}
	if n := len(sq.evs); n > 0 && sq.evs[n-1].kind == ev.kind {
		return ""
	}
	sq.evs = append(sq.evs, ev)
	if sq.done || ev.kind != "SB4" {
		return ""
	}
	sq.done = true
	return startupText(sq.evs)
}

func (b *pressBoard) talkerAfter(k pressKey, after float64) (id int, t float64, ok bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	sq := b.seqs[k]
	if sq == nil || sq.talker < 0 || sq.talkerT <= after {
		return 0, 0, false
	}
	return sq.talker, sq.talkerT, true
}

func startupText(evs []pressEv) string {
	var p []string
	have := map[string]bool{}
	for i, e := range evs {
		s := e.kind
		if i > 0 {
			s += fmt.Sprintf(" +%.0f ms", (e.t-evs[i-1].t)*1000)
		}
		if e.kind == "SB3" {
			s += "（TA " + l1.TAText(e.ta) + "）"
		}
		p = append(p, s)
		have[e.kind] = true
	}
	order := "SB1 → SB2 → SB3 → SB4 の順"
	for i, k := range []string{"SB1", "SB2", "SB3", "SB4"} {
		if !have[k] || (i < len(evs) && evs[i].kind != k) {
			order = "規格の順（SB1 → SB2 → SB3 → SB4）と違う・欠けあり（見えた範囲で）"
			break
		}
	}
	return strings.Join(p, " → ") + "・" + order
}

func (c *Channel) notePress(s *decoder.Slot) {
	sb := s.SB
	if sb == nil || !sb.OK || s.Direct != nil {
		return
	}
	down := c.cfg.MHz
	kind := "SB1"
	if c.isUp {
		down -= UpOffsetMHz
		kind = "SB2"
	}
	if sb.B == 1 {
		kind = map[bool]string{false: "SB3", true: "SB4"}[c.isUp]
	}
	sum := c.std.press.note(pressKeyOf(down, s.Num), pressEv{kind: kind, t: s.TSec, ta: sb.TA, id: sb.ID})
	if sum == "" {
		return
	}
	who := numbering.CallSign(sb.ID).Text()
	text := fmt.Sprintf("%.4f MHz ST#%d: %s・移動局 %s", down, s.Num, sum, who)
	c.note(s.Num, "startup "+text)
	c.logf(s.Num, "call", "info", "通信チャネルの起動（§4.1.16.2）%s", text)
	c.emitCall(CallRec{TSec: s.TSec, Slot: s.Num, Channel: "SB", Kind: "通信チャネルの起動", Text: text, ToSlot: -1, Ref: -1, ID: sb.ID, MS: who})
}

func (c *Channel) pollTalker(n int, now float64) {
	if c.isUp {
		return
	}
	cs := &c.calls[n]
	if !cs.on {
		return
	}
	id, t, ok := c.std.press.talkerAfter(pressKeyOf(c.cfg.MHz, n), cs.talkerT)
	if !ok || t < cs.t0-pressGapSec {
		return
	}
	cs.talkerT = t
	if id == cs.lastTalker {
		return
	}
	cs.lastTalker = id
	who := numbering.CallSign(id).Text()
	c.note(n, fmt.Sprintf("talker %.3f %s（上りの同期バーストの呼出符号）", t, who))
	c.emitCall(CallRec{TSec: now, Slot: n, Channel: "SB↑", Kind: "話者（プレス）", Text: "上りで送信を始めた移動局 " + who,
		ToSlot: -1, Ref: -1, ID: id, MS: who})
	c.mu.Lock()
	base := cs.peer
	if i := strings.Index(base, "・話者 "); i >= 0 {
		base = base[:i]
	}
	cs.peer = strings.TrimPrefix(base+"・話者 "+fmt.Sprint(id&0x7FFFF), "・")
	c.mu.Unlock()
}
