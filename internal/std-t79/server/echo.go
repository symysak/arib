package server


import (
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/symysak/arib/internal/std-t79/decoder"
)

const echoSec = 2.0

type echoItem struct {
	ctl  float64
	crc  uint16
	t    time.Time
	text string
	used bool
	up   *Channel
}

type echoBoard struct {
	mu    sync.Mutex
	items []echoItem
	smsi map[smsiKey]string
}

type smsiKey struct {
	base int64
	smsi uint8
}

func smsiKeyOf(mhz float64, v uint8) smsiKey {
	return smsiKey{base: int64(math.Round(mhz * 1e6 / RasterHz)), smsi: v}
}

func (b *echoBoard) setSMSI(mhz float64, v uint8, ms string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.smsi == nil {
		b.smsi = map[smsiKey]string{}
	}
	k := smsiKeyOf(mhz, v)
	if b.smsi[k] == ms {
		return false
	}
	b.smsi[k] = ms
	return true
}

func (b *echoBoard) clearSMSI(mhz float64, v uint8) (string, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	k := smsiKeyOf(mhz, v)
	old, ok := b.smsi[k]
	delete(b.smsi, k)
	return old, ok
}

func (b *echoBoard) lookupSMSI(mhz float64, v uint8) (string, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	m, ok := b.smsi[smsiKeyOf(mhz, v)]
	return m, ok
}

func (b *echoBoard) put(it echoItem) {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := b.items[:0]
	for _, x := range b.items {
		if it.t.Sub(x.t).Seconds() < 10 {
			out = append(out, x)
		}
	}
	b.items = append(out, it)
}

func (b *echoBoard) match(ctl float64, pe uint16, t time.Time) (echoItem, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for i := range b.items {
		x := &b.items[i]
		if !x.used && x.crc == pe && math.Abs(x.ctl-ctl) < matchMHz && math.Abs(t.Sub(x.t).Seconds()) < echoSec {
			x.used = true
			return *x, true
		}
	}
	return echoItem{}, false
}

func (c *Channel) noteEcho(s *decoder.Slot) {
	u := s.CAC
	if u == nil || !u.OK {
		return
	}
	if c.up != nil {
		text := fmt.Sprintf("%.4f MHz ST#%d CAC↑ W %s", c.cfg.MHz, s.Num, u.W)
		c.std.echoes.put(echoItem{ctl: c.cfg.MHz - UpOffsetMHz, crc: u.CRC, t: c.sink.StreamTime(), text: text, up: c})
		c.mu.Lock()
		c.car.UpCAC++
		c.mu.Unlock()
		return
	}
	e, ok := u.E()
	if !ok {
		return
	}
	c.mu.Lock()
	c.car.EUnits++
	if e.Allowed {
		c.car.EAllowed++
	}
	if e.Received {
		c.car.EReceived++
	}
	c.mu.Unlock()
	if !e.Received {
		return
	}
	if it, ok := c.std.echoes.match(c.cfg.MHz, e.PE, c.sink.StreamTime()); ok {
		c.mu.Lock()
		c.car.PEMatched++
		c.mu.Unlock()
		if it.up != nil {
			it.up.mu.Lock()
			it.up.car.PEMatched++
			it.up.mu.Unlock()
		}
		r := CallRec{TSec: s.TSec, Slot: s.Num, Channel: "CAC", Kind: "部分エコー", ToSlot: -1, Ref: -1, ID: -1,
			Text: fmt.Sprintf("基地局が上りの CAC を受信（PE 0x%04X = %s）", e.PE, it.text)}
		c.emitCall(r)
	}
}

func (c *Channel) noteRCH(s *decoder.Slot) {
	r := s.RCH
	if r == nil || !r.OK {
		return
	}
	text := c.rchText(r.Info, s.Up)
	cs := &c.calls[s.Num]
	if text == cs.lastRCH {
		return
	}
	cs.lastRCH = text
	c.note(s.Num, fmt.Sprintf("rch %d %s", s.Frame, text))
	if text != "アイドル" {
		c.logf(s.Num, "call", "info", "ST#%d RCH%s %s", s.Num, map[bool]string{true: "↑", false: ""}[s.Up], text)
	}
}
