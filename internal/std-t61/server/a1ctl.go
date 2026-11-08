package server


import (
	"fmt"
	"strings"

	"github.com/symysak/arib/internal/data"
	dec "github.com/symysak/arib/internal/std-t61/decoder"
	"github.com/symysak/arib/internal/std-t61/l1"
	scpca1f40 "github.com/symysak/arib/internal/std-t61/scpc-a1f40"
	scpca1u292 "github.com/symysak/arib/internal/std-t61/scpc-a1u292"
)

type a1Res struct {
	U   *scpca1u292.Result
	Ctl *ctlRes
}

type ctlRes struct {
	OK     bool
	WC     int
	Errors int
	Data   [scpca1f40.ControlBytes]byte
}

type a1Ctl struct {
	key  *scpca1f40.ControlKeyer
	rd   *data.Station
	hint int
}

func (p *Pipeline) newA1Ctl(mhz float64) *a1Ctl {
	spec := p.cfg.DataFormat
	if data.Valid(scpca1f40.ControlChannel, spec) != nil {
		spec = data.Auto
	}
	c := &a1Ctl{rd: data.NewStation(scpca1f40.ControlChannel, spec), hint: -1}
	if w, ok := p.stationWC(mhz); ok {
		c.key = scpca1f40.NewControlKeyer(w)
		return c
	}
	c.key = scpca1f40.NewControlKeyer(-1)
	c.key.Judge = judgeControl
	if p.led != nil {
		if _, w := p.led.Codec(mhz); w >= 0 {
			c.key.Hint(w)
			c.hint = w
		}
	}
	return c
}

func (p *Pipeline) onA1Ctl(s *stationState, f dec.Frame) *ctlRes {
	r := f.A1RICH
	if r == nil || !r.OK || !r.IsVoice() || r.FN != 1 || len(f.TCH1Soft) != l1.A1TCH1Bits || strings.EqualFold(p.cfg.DataFormat, data.Off) {
		return nil
	}
	w, ok := scpca1f40.Control(f.TCH1Soft)
	if !ok || !w.Usable() {
		return nil
	}
	if s.ctl == nil {
		s.ctl = p.newA1Ctl(s.st.MHz)
	}
	if s.a1 == nil {
		s.a1 = p.newA1Data(s.st.MHz)
	}
	c := s.ctl
	if v := s.voice; v != nil && v.lastName != "" && v.lastWC >= 0 && v.lastWC != c.hint {
		c.key.Hint(v.lastWC)
		c.hint = v.lastWC
	}
	route := "経由"
	if r.REP == 0 {
		route = "直接"
	}
	cur := &ctlRes{WC: -1, Errors: w.Errors}
	defer func() {
		if wc, ok := c.key.WC(); ok && s.voice != nil {
			s.voice.setDataWC(wc)
		}
	}()
	for _, d := range c.key.Feed(scpca1f40.ControlItem{Word: w, Tag: [2]int64{f.Number, int64(r.CFN)}}) {
		tag := d.Tag.([2]int64)
		if tag[0] == f.Number {
			cur.OK, cur.WC, cur.Data = true, d.WC, d.Data
		}
		evs := c.rd.Read(data.Frame{Ch: scpca1f40.ControlChannel, No: tag[0], TimeSec: f.TimeSec, Bits: bitsOf(d.Data[:]),
			Route: route, Slot: int(tag[1])})
		for _, ev := range evs {
			p.onDataEvent(s, ev)
		}
	}
	if cur.OK {
		p.mu.Lock()
		s.st.Ext["ctl_key"] = fmt.Sprintf("%s wc=%d", scpca1f40.ControlName, cur.WC)
		if n := c.rd.Format(); n != "" {
			s.st.Ext["ctl_format"] = n
		}
		s.dirty = true
		p.mu.Unlock()
	}
	return cur
}

func judgeControl(cands map[int][]scpca1f40.ControlDecoded) (int, bool) {
	best, bestOK, second := -1, 0, 0
	for wc, ds := range cands {
		frames := make([]data.Frame, len(ds))
		for i, d := range ds {
			tag := d.Tag.([2]int64)
			frames[i] = data.Frame{Ch: scpca1f40.ControlChannel, No: tag[0], Bits: bitsOf(d.Data[:]), Slot: int(tag[1])}
		}
		top := 0
		for _, f := range data.Formats(scpca1f40.ControlChannel) {
			if f.Probe == nil {
				continue
			}
			if ok, n := f.Probe(frames); ok >= 3 && 2*ok >= n && ok > top {
				top = ok
			}
		}
		switch {
		case top > bestOK:
			best, bestOK, second = wc, top, bestOK
		case top > second:
			second = top
		}
	}
	return best, best >= 0 && bestOK > second
}

func bitsOf(b []byte) []uint8 {
	out := make([]uint8, 8*len(b))
	for i := range out {
		out[i] = b[i/8] >> (7 - i%8) & 1
	}
	return out
}
