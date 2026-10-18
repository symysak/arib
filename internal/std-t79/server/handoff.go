package server


import (
	"fmt"
	"math"
	"strings"
	"sync"
)

const handoffSec = 15.0

type endedCall struct {
	peer string
	mhz  float64
	slot int
	t    float64
	base string
}

type handoffBoard struct {
	mu    sync.Mutex
	ended []endedCall
}

func peerKey(peer string) string {
	if i := strings.Index(peer, "・話者 "); i >= 0 {
		peer = peer[:i]
	}
	return strings.TrimSpace(peer)
}

func (b *handoffBoard) put(e endedCall) {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := b.ended[:0]
	for _, x := range b.ended {
		if e.t-x.t < 2*handoffSec {
			out = append(out, x)
		}
	}
	b.ended = append(out, e)
}

func (b *handoffBoard) match(peer string, mhz float64, slot int, t float64) (endedCall, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for i := len(b.ended) - 1; i >= 0; i-- {
		x := b.ended[i]
		same := math.Abs(x.mhz-mhz) < matchMHz && x.slot == slot
		if x.peer == peer && !same && t-x.t >= -1 && t-x.t <= handoffSec {
			return x, true
		}
	}
	return endedCall{}, false
}

func (c *Channel) noteEnded(n int, now float64) {
	cs := &c.calls[n]
	if c.isUp || cs.peer == "" {
		return
	}
	c.std.hand.put(endedCall{peer: peerKey(cs.peer), mhz: c.cfg.MHz, slot: n, t: now, base: cs.base})
}

func (c *Channel) checkHandoff(n int) {
	cs := &c.calls[n]
	if c.isUp || cs.handoffDone || cs.peer == "" {
		return
	}
	cs.handoffDone = true
	prev, ok := c.std.hand.match(peerKey(cs.peer), c.cfg.MHz, n, cs.t0)
	if !ok {
		return
	}
	text := fmt.Sprintf("前の通話 %.4f MHz ST#%d（%.1f 秒前に終了）から %.4f MHz ST#%d へ。発着番号 %s が同じ（再発呼型のチャネル切替とみる。推定）",
		prev.mhz, prev.slot, cs.t0-prev.t, c.cfg.MHz, n, peerKey(cs.peer))
	c.note(n, "handoff "+text+"・前の録音 "+c.relName(prev.base))
	c.logf(n, "call", "info", "ハンドオフ（推定）: %s", text)
	c.emitCall(CallRec{TSec: c.tNow(), Slot: n, Channel: "CC", Kind: "ハンドオフ（推定）", Text: text, ToSlot: -1, Ref: -1, ID: -1, MHz: prev.mhz})
}
