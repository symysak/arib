package server


import (
	"github.com/symysak/arib/internal/data"
	"time"

	"github.com/symysak/arib/internal/rcr39qpsk/decoder"
	"github.com/symysak/arib/internal/rcr39qpsk/l1"
)

func (c *Channel) Up() bool           { return c.up != nil }
func (c *Channel) Direct() *DirectOpt { return c.directOpt() }
func (c *Channel) DownMHz() float64 {
	if c.up != nil {
		return c.cfg.MHz - c.std.spec.UpOffsetMHz
	}
	return c.cfg.MHz
}
func (c *Channel) Now() float64          { return c.tNow() }
func (c *Channel) StreamTime() time.Time { return c.sink.StreamTime() }
func (c *Channel) LogDir() string        { return c.std.cfg.LogDir }
func (c *Channel) Logf(n int, group, level, format string, a ...any) {
	c.logf(n, group, level, format, a...)
}
func (c *Channel) Note(n int, line string)                   { c.note(n, line) }
func (c *Channel) CallEvent(n int, kind, text string)        { c.callEvent(n, kind, text) }
func (c *Channel) ChLabel(s *decoder.Slot, ch string) string { return c.chLabel(s, ch) }
func (c *Channel) Fresh(key string, t float64) bool          { return c.fresh(key, t) }
func (c *Channel) EmitCall(r CallRec)                        { c.emitCall(r) }
func (c *Channel) EmitNet(ni NetInfo)                        { c.emitNet(ni) }

func (c *Channel) SetPacket(n int, on bool) {
	if n < 0 || n >= len(c.packet) {
		return
	}
	c.packet[n] = on
	c.applyPacket()
}

func (c *Channel) EmitData(n int, ev data.Event) {
	if ev.TimeSec == 0 {
		ev.TimeSec = c.tNow()
	}
	c.sink.Emit(Event{Type: string(ev.Kind), DataEv: &DataEvent{Idx: c.cfg.ID, MHz: c.cfg.MHz, Event: ev}})
	c.logf(n, "data", "info", "%s", ev.Label)
	c.note(n, "data "+ev.Label)
}

func (c *Channel) PutHint(h Hint) {
	c.std.hints.put(h)
	if !h.Up && c.std.hooks != nil && c.std.hooks.assigned != nil {
		c.std.hooks.assigned(h.MHz)
	}
}

func (c *Channel) OnDirect(mhz float64, scramble int, mode l1.DirectMode) {
	if c.std.hooks != nil && c.std.hooks.direct != nil {
		c.std.hooks.direct(mhz, scramble, mode)
	}
}

func (c *Channel) SetCACStructure(s l1.CACStructure) {
	if c.dec != nil {
		c.dec.SetCACStructure(s)
	}
}

func (c *Channel) ClearCACStructure() {
	if c.dec != nil {
		c.dec.ClearCACStructure()
	}
}

func (c *Channel) CodecHint(name, why string) { c.codecHint(name, why) }
func (c *Channel) CallOn(n int) bool          { return n >= 0 && n < len(c.calls) && c.calls[n].on }
func (c *Channel) CallT0(n int) float64       { return c.calls[n].t0 }
func (c *Channel) EndCall(n int, why string)  { c.endCall(n, c.tNow(), why) }

func (c *Channel) SetPeer(n int, peer string) {
	cs := &c.calls[n]
	cs.peer = peer
	if cs.on {
		c.checkHandoff(n)
	}
	c.mu.Lock()
	c.car.Slots[n].Peer = cs.peer
	c.mu.Unlock()
}

func (c *Channel) Peer(n int) string { return c.calls[n].peer }

func (c *Channel) SetMode(n int, name, note string) {
	c.mu.Lock()
	c.car.Slots[n].Mode, c.car.Slots[n].ModeNote = name, note
	c.mu.Unlock()
}

func (c *Channel) Org() string        { return c.orgName() }
func (c *Channel) SetOrg(org string)  { c.setOrg(org) }
func (c *Channel) SeenOrg(org string) { c.seenOrg(org) }

func (c *Channel) SetVendor(text string) {
	c.mu.Lock()
	c.car.Vendor = text
	c.mu.Unlock()
}
