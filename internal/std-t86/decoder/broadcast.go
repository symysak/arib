package decoder

import (
	"github.com/symysak/stdt86/internal/std-t86/control"
	"github.com/symysak/stdt86/internal/std-t86/dsp"
)

const slotSamples = dsp.SymbolsPerSlot * dsp.SPS

type BroadcastWindow struct {
	WindowID int
	OpenPos  int
	ClosePos int
	Closed   bool

	Target      *control.Target
	TargetCRCOK bool

	MidJoin bool
	ClosedByKH bool
	ClosedByNewCall bool
	Abandoned bool
}

func (w *BroadcastWindow) Contains(pos int) bool {
	return w.OpenPos <= pos && (!w.Closed || pos < w.ClosePos)
}

type BroadcastTracker struct {
	Strict        bool
	ConfirmWindow int
	ConfirmCount  int
	IDValidBits int

	MidJoinDelay int
	KHCloseDelay int

	TargetUpdated []*BroadcastWindow

	open   *BroadcastWindow
	recent []*BroadcastWindow
	keep   int
	nextID int
	hist   map[int][]int

	khSeen   bool
	midJoin  bool
	khOnPos  int
	khZeroAt int
}

func NewBroadcastTracker() *BroadcastTracker {
	return &BroadcastTracker{
		Strict:        true,
		ConfirmWindow: 180_000,
		ConfirmCount:  2,
		MidJoinDelay:  135_000,
		KHCloseDelay:  270_000,
		keep:          16,
		nextID:        1,
		hist:          map[int][]int{},
		khZeroAt:      -1,
	}
}

func (t *BroadcastTracker) Open() *BroadcastWindow { return t.open }

func (t *BroadcastTracker) confirmed(pos int, msg control.Message) bool {
	if msg.CRCOK {
		return true
	}
	if t.Strict {
		return false
	}
	h := append(t.hist[msg.Type], pos)
	for len(h) > 0 && pos-h[0] > t.ConfirmWindow {
		h = h[1:]
	}
	t.hist[msg.Type] = h
	return len(h) >= t.ConfirmCount
}

func (t *BroadcastTracker) OnControl(pos int, msg control.Message) []*BroadcastWindow {
	var changed []*BroadcastWindow
	if msg.CRCOK && msg.BCCH != nil && msg.BCCH.IDValidBits >= 0 && msg.BCCH.IDValidBits <= 8 {
		t.IDValidBits = 8 + msg.BCCH.IDValidBits
	}
	if msg.CRCOK && msg.Header != nil {
		changed = append(changed, t.onKH(pos, msg.Header)...)
	}

	isStart := msg.Type == control.MsgBroadcastStart || msg.Type == control.MsgDelayedStart
	if !isStart && msg.Type != control.MsgForcedRelease {
		return changed
	}
	if isStart {
		t.refreshTarget(msg)
	}
	if !t.confirmed(pos, msg) {
		return changed
	}
	if isStart && t.open != nil && msg.CRCOK && msg.Broadcast != nil &&
		t.open.Target != nil && t.open.TargetCRCOK &&
		msg.Broadcast.CallNo != t.open.Target.CallNo {
		w := t.closeOpen(pos)
		w.ClosedByNewCall = true
		changed = append(changed, w)
	}

	switch {
	case isStart && t.open == nil:
		t.midJoin = false
		t.open = &BroadcastWindow{
			WindowID:    t.nextID,
			OpenPos:     pos,
			Target:      control.BroadcastTarget(msg, t.IDValidBits),
			TargetCRCOK: msg.CRCOK,
		}
		t.nextID++
		changed = append(changed, t.open)
	case msg.Type == control.MsgForcedRelease && t.open != nil:
		changed = append(changed, t.closeOpen(pos))
	}
	return changed
}

func (t *BroadcastTracker) closeOpen(pos int) *BroadcastWindow {
	w := t.open
	w.ClosePos = pos
	w.Closed = true
	t.recent = append(t.recent, w)
	if len(t.recent) > t.keep {
		t.recent = t.recent[len(t.recent)-t.keep:]
	}
	t.open = nil
	t.khZeroAt = -1
	t.midJoin = false
	return w
}

func (t *BroadcastTracker) ArmMidJoin() {
	t.khSeen, t.midJoin, t.khZeroAt = false, false, -1
}

func (t *BroadcastTracker) onKH(pos int, h *control.CCHHeader) []*BroadcastWindow {
	var changed []*BroadcastWindow
	active := h.Broadcasting && len(h.UsedSlots) > 0

	if !t.khSeen {
		t.khSeen = true
		t.midJoin = active
		t.khOnPos = pos
	}
	if active {
		if t.midJoin && t.open == nil && pos-t.khOnPos >= t.MidJoinDelay {
			t.midJoin = false
			t.open = &BroadcastWindow{
				WindowID: t.nextID,
				OpenPos:  t.khOnPos,
				MidJoin:  true,
			}
			t.nextID++
			changed = append(changed, t.open)
		}
	} else {
		t.midJoin = false
	}

	if h.Broadcasting {
		t.khZeroAt = -1
		return changed
	}
	if t.open == nil {
		t.khZeroAt = -1
		return changed
	}
	if t.khZeroAt < 0 {
		t.khZeroAt = pos
		return changed
	}
	if pos-t.khZeroAt >= t.KHCloseDelay {
		w := t.closeOpen(pos)
		w.ClosedByKH = true
		changed = append(changed, w)
	}
	return changed
}

func (t *BroadcastTracker) Abandon(pos int) *BroadcastWindow {
	t.ArmMidJoin()
	t.hist = map[int][]int{}
	if t.open == nil {
		return nil
	}
	if pos < t.open.OpenPos {
		pos = t.open.OpenPos
	}
	w := t.closeOpen(pos)
	w.Abandoned = true
	return w
}

func (t *BroadcastTracker) refreshTarget(msg control.Message) {
	w := t.open
	if w == nil || w.TargetCRCOK || !msg.CRCOK {
		return
	}
	target := control.BroadcastTarget(msg, t.IDValidBits)
	if target == nil {
		return
	}
	if w.Target == nil || !targetsEqual(w.Target, target) {
		w.Target = target
		t.TargetUpdated = append(t.TargetUpdated, w)
	}
	w.TargetCRCOK = true
}

func (t *BroadcastTracker) WindowFor(pos int) *BroadcastWindow {
	if t.open != nil && t.open.Contains(pos) {
		return t.open
	}
	for i := len(t.recent) - 1; i >= 0; i-- {
		if t.recent[i].Contains(pos) {
			return t.recent[i]
		}
	}
	return nil
}

func targetsEqual(a, b *control.Target) bool {
	if a == nil || b == nil {
		return a == b
	}
	if a.Kind != b.Kind || a.Label != b.Label || a.Note != b.Note ||
		a.ValidBits != b.ValidBits || a.CallNo != b.CallNo ||
		len(a.IDs) != len(b.IDs) || len(a.EffectiveIDs) != len(b.EffectiveIDs) {
		return false
	}
	for i := range a.IDs {
		if a.IDs[i] != b.IDs[i] {
			return false
		}
	}
	for i := range a.EffectiveIDs {
		if a.EffectiveIDs[i] != b.EffectiveIDs[i] {
			return false
		}
	}
	return true
}
