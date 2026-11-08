package server

import (
	"fmt"
	"strings"

	dec "github.com/symysak/arib/internal/std-t61/decoder"
	t61voice "github.com/symysak/arib/internal/std-t61/voice"
	"github.com/symysak/arib/internal/voice"
)


type KV struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

type Profile struct {
	Edition string `json:"edition"`
	Impl string `json:"impl"`
	Codec string `json:"codec"`
	Keys []KV `json:"keys"`
}

type profileState struct {
	fdma, scpc, a1 bool
	down           bool
	cc             int
	upTraffic      int
	upControl      int
	upAssigned     int
}

func newProfileState() profileState {
	return profileState{cc: -1, upTraffic: -1, upControl: -1, upAssigned: -1}
}

func (ps *profileState) observe(f dec.Frame) {
	switch {
	case f.A1RICH != nil && f.A1RICH.OK:
		ps.a1 = true
	case f.SACCH != nil || f.Control != nil || (f.SW != nil && f.SW.FDMAOnly):
		ps.fdma = true
	case f.RICH.OK && f.A1RICH == nil:
		ps.scpc = true
	}
	if !f.Uplink {
		ps.down = true
	}
	if ps.fdma && f.RICH.OK && f.RICH.S == 1 {
		ps.cc = int(f.RICH.CC)
	}
	if f.Uplink && f.TCH != nil && f.TCHInit > 0 {
		ps.upTraffic = f.TCHInit
	}
	if v := f.UplinkScrambleFound; v > 0 {
		ps.upTraffic = v - 1
	}
	if v := f.UplinkScrambleAssigned; v > 0 {
		ps.upAssigned = v - 1
	}
	if v := f.UplinkControlScrambleFound; v > 0 {
		ps.upControl = v - 1
	}
}

func codecLabel(name string) (codec, impl string) {
	if name == "" {
		return "", ""
	}
	for _, ch := range []voice.Channel{t61voice.TCH1, t61voice.TCH} {
		if c, ok := voice.Lookup(ch, name); ok && c.Vocoder != "" {
			return c.Vocoder, c.Transport
		}
	}
	return name, ""
}

func (p *Pipeline) profile(s *stationState) Profile {
	ps := &s.prof
	var out Profile
	switch {
	case ps.a1:
		out.Edition = "SCPC 付録 1"
	case ps.fdma:
		out.Edition = "FDMA"
	case ps.scpc:
		out.Edition = "SCPC"
	}
	var impl []string
	if v := s.voice; v != nil {
		name := v.lastName
		if name == "" && v.chosen != nil {
			name = v.chosen.Name
		}
		c, i := codecLabel(name)
		out.Codec = c
		if i != "" {
			impl = append(impl, i)
		}
		if v.lastName != "" && v.lastWC >= 0 {
			out.Keys = append(out.Keys, KV{"WC（音声 TCH1）", fmt.Sprintf("%d（0x%03X）", v.lastWC, v.lastWC)})
		}
	}
	if s.a1 != nil {
		if wc, ok := s.a1.key.WC(); ok {
			impl = append(impl, "付録 1 U u292")
			out.Keys = append(out.Keys, KV{"WC（データ U）", fmt.Sprintf("%d（0x%03X）", wc, wc)})
		}
	}
	if s.ctl != nil {
		if wc, ok := s.ctl.key.WC(); ok {
			impl = append(impl, "付録 1 FN1 の制御語")
			out.Keys = append(out.Keys, KV{"WC（FN1 の制御語）", fmt.Sprintf("%d（0x%03X）", wc, wc)})
		}
	}
	fdma := ps.fdma && !ps.a1
	if fdma && ps.down {
		out.Keys = append(out.Keys, KV{"下りのスクランブル", "全 1（0x1FF。基地局は固定）"})
	}
	if fdma && ps.cc >= 0 {
		out.Keys = append(out.Keys, KV{"カラーコード", fmt.Sprintf("%d", ps.cc)})
	}
	if ps.upControl >= 0 {
		out.Keys = append(out.Keys, KV{"上り制御のスクランブル", fmt.Sprintf("0x%03X（当てた値）", ps.upControl)})
	}
	if ps.upAssigned >= 0 {
		out.Keys = append(out.Keys, KV{"上り通信のスクランブル（指定）", fmt.Sprintf("0x%03X", ps.upAssigned)})
	}
	if ps.upTraffic >= 0 {
		out.Keys = append(out.Keys, KV{"上り通信のスクランブル", fmt.Sprintf("0x%03X", ps.upTraffic)})
	}
	out.Impl = strings.Join(impl, "・")
	return out
}
