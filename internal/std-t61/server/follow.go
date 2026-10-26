package server


import (
	"fmt"
	"math"

	dec "github.com/symysak/arib/internal/std-t61/decoder"
	"github.com/symysak/arib/internal/std-t61/l2"
)

var followTypes = map[byte]bool{0x04: true, 0x43: true, 0x61: true}

func (p *Pipeline) followAssign(s *stationState, m dec.Message) {
	if p.cfg.NoFollowAssign || m.L3 == nil || m.L3.Err != "" || m.L2.Dir == l2.DirUp {
		return
	}
	for _, x := range m.L3.Messages {
		if x.Err != "" || x.Entity != "RT" || x.RT == nil || x.RT.Fc < 0 || !followTypes[x.Type] {
			continue
		}
		fo, ok := p.seq.Fo(x.RT.Band)
		if !ok {
			continue
		}
		mhz := math.Round((fo+0.00625*float64(x.RT.Fc))*1e5) / 1e5
		p.addFollowed(mhz, s.st.MHz, x.Name, x.RT.Fc)
	}
}

func (p *Pipeline) addFollowed(mhz, from float64, why string, fc int) {
	p.mu.Lock()
	for f, st := range p.byMHz {
		if st.st.Watched && math.Abs(f-mhz) < 0.003 {
			p.mu.Unlock()
			return
		}
	}
	if p.followed == nil {
		p.followed = map[float64]bool{}
	}
	if p.followed[mhz] {
		p.mu.Unlock()
		return
	}
	p.followed[mhz] = true
	eng, add := p.eng, p.followAdd
	p.mu.Unlock()
	if add == nil {
		if eng == nil {
			return
		}
		add = func(f float64) error { return eng.Add(f, nil) }
	}
	go func() {
		var msg string
		if err := add(mhz); err != nil {
			msg = fmt.Sprintf("%.4f MHz の%sが指す Fc=%d（%.5f MHz）は監視しない: %v", from, why, fc, mhz, err)
		} else {
			msg = fmt.Sprintf("%.4f MHz の%sが指す Fc=%d（%.5f MHz）を監視に足した", from, why, fc, mhz)
		}
		p.AddLog(-1, "system", "%s", msg)
		if eng != nil {
			eng.Logf("%s", msg)
		}
	}()
}
