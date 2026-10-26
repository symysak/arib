package server


import (
	"fmt"

	"github.com/symysak/arib/internal/std-t61/callseq"
	dec "github.com/symysak/arib/internal/std-t61/decoder"
	"github.com/symysak/arib/internal/std-t61/l1"
	"github.com/symysak/arib/internal/std-t61/l2"
	"github.com/symysak/arib/internal/std-t61/l3"
)

func seqAddr(a l2.Address) string {
	switch {
	case a.HasMSI() && a.MSIBits > 0 && a.IsGlobal():
		return "MSI=全局"
	case a.HasMSI() && a.MSIBits > 0:
		return fmt.Sprintf("MSI=%d", a.MSI)
	case a.HasSMSI():
		return fmt.Sprintf("SMSI=%d", a.SMSI)
	}
	return ""
}

func (p *Pipeline) seqMessage(s *stationState, m dec.Message) {
	if m.L3 == nil || m.L3.Err != "" {
		return
	}
	var ms []l3.Message
	for _, x := range m.L3.Messages {
		if x.Err == "" {
			ms = append(ms, x)
		}
	}
	if len(ms) == 0 {
		return
	}
	p.seqApply(p.seq.Message(callseq.Msg{TSec: p.now(), Station: s.st.Idx, MHz: s.st.MHz, Channel: m.Channel,
		Uplink: m.L2.Dir == l2.DirUp, Addr: seqAddr(m.L2.Address), L3: ms, Notes: m.L3.Notes}))
}

func (p *Pipeline) seqFrame(s *stationState, f dec.Frame) {
	if f.Kind != l1.KindService && f.Kind != l1.KindSyncBurst {
		return
	}
	o := callseq.Obs{TSec: p.now(), Station: s.st.Idx, MHz: s.st.MHz, Service: true,
		StdRICH: f.RICH.OK && f.RICH.S == 1 && !f.Flywheel, M: f.RICH.M, D: f.RICH.D}
	if f.SB != nil && f.SB.OK && o.StdRICH {
		o.SB, o.SBIndividual, o.SBInitial, o.SBID = true, f.SB.ID>>23&1 == 1, !f.SB.B, f.SB.ID
	}
	if !o.StdRICH {
		return
	}
	p.seqApply(p.seq.Frame(o))
}

func (p *Pipeline) seqTick() { p.seqApply(p.seq.Tick(p.now())) }

func (p *Pipeline) seqApply(ups []callseq.Update) {
	for i := range ups {
		u := ups[i]
		c := u.Call
		p.mu.Lock()
		for _, idx := range []int{c.Station, c.Traffic} {
			s := p.stationByIdx(idx)
			if s == nil || idx < 0 || c.Proc != "呼" {
				continue
			}
			switch {
			case c.Open:
				s.callID = c.ID
				if s.st.CallType != c.Type || s.st.CallDown != c.Downlink {
					s.st.CallType, s.st.CallDown, s.dirty = c.Type, c.Downlink, true
				}
			case s.callID == c.ID:
				s.callID = 0
				s.st.CallType, s.st.CallDown, s.dirty = "", "", true
			}
		}
		var vr *voiceRun
		if e := u.Event; e != nil {
			if s := p.stationByIdx(e.Station); s != nil {
				vr = s.voice
			}
		}
		p.mu.Unlock()
		p.emit(Event{Type: "callseq", Seq: &c})
		if e := u.Event; e != nil {
			p.addLogFor(e.Station, "call", "%.4f MHz 呼#%d %s", e.MHz, c.ID, e.Text)
			if vr != nil {
				vr.seqNote("seq_"+e.Kind, fmt.Sprintf("呼#%d %s", c.ID, e.Text))
			}
		}
	}
}
