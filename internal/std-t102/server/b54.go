package server

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/symysak/arib/internal/data"
	nx "github.com/symysak/arib/internal/nxdn/server"
	"github.com/symysak/arib/internal/std-t102/l3"
)


func B54Channels() []float64 {
	var out []float64
	for _, base := range []float64{166.528125, 168.528125} {
		for i := 0; i < 60; i++ {
			out = append(out, math.Round((base+float64(i)*0.00625)*1e6)/1e6)
		}
	}
	return out
}

func ParseB54Keys(list []string) (map[int]uint16, error) {
	out := map[int]uint16{}
	for _, s := range list {
		no, key, ok := strings.Cut(s, "=")
		n, err1 := strconv.ParseUint(strings.TrimSpace(no), 0, 8)
		k, err2 := strconv.ParseUint(strings.TrimSpace(key), 0, 16)
		if !ok || err1 != nil || err2 != nil || n < 1 || n > 63 || k < 1 || k > 0x7FFF {
			return nil, fmt.Errorf("-b54-key %q は <鍵番号 1〜63>=<暗号鍵 1〜32767>", s)
		}
		out[int(n)] = uint16(k)
	}
	return out, nil
}

func (k *link) b54Msgs(t float64, msgs []nx.MsgRec) {
	for _, m := range msgs {
		lm, _ := m.Ext.(l3.Message)
		x := lm.B54
		if x == nil {
			continue
		}
		if call := x.CallText(); call != "" {
			k.h.Tx(func(tx *TxRec) bool {
				if x.Type == l3.TypeSelVoice {
					k.b54Call = x
				}
				if x.Type == l3.TypeSelVoice || x.Type == l3.TypeGPSHeader || x.Type == l3.TypeBearerHeader || x.Type == l3.TypeMsgHeader ||
					x.Type == l3.TypeStatusReq || tx.Call == "" {
					if tx.Call != call {
						tx.Call = call
						return true
					}
				}
				return false
			})
		}
		k.b54Events(t, k.b54.Push(lm))
	}
}

func (k *link) b54Events(t float64, evs []l3.B54Event) {
	for _, e := range evs {
		k.h.Note("b54 " + e.Label())
		switch e.Kind {
		case "name":
			k.h.Tx(func(tx *TxRec) bool { tx.Name = e.Name; return true })
			k.h.EditStation(func(st *Station) { st.Caller = e.Name }, false)
			k.h.Logf("call", "info", "発信者名「%s」", e.Name)
			k.h.EditStation(func(*Station) {}, true)
			continue
		case "message":
			if e.Command != 0x83 && e.Command != 0x84 && !e.KeyUnknown {
				continue
			}
		}
		ev := data.Event{Kind: data.KindMessage, TimeSec: t, Format: "B54", Label: e.Label(), Raw: e.Raw}
		if e.Call != nil && e.Call.Caller >= 0 {
			ev.From = fmt.Sprintf("B54 %d", e.Call.Caller)
		}
		for _, r := range e.Rows() {
			ev.Rows = append(ev.Rows, data.Row{Name: r.Name, Text: r.Text})
		}
		if e.Kind == "gps" {
			ev.Kind = data.KindPosition
			if g := e.GPS; g != nil && g.Valid && g.Sane {
				ev.Pos = &data.Position{Lat: g.Lat, Lon: g.Lon, Heading: -1}
			}
		}
		k.h.EmitData(ev)
	}
}

func (k *link) VoiceOpt(codec string, opt map[string]string) {
	if _, set := opt["scramble"]; codec != "ambe" || set || k.b54Call == nil {
		return
	}
	x := k.b54Call
	switch x.Enc {
	case 0:
		opt["scramble"] = "off"
		delete(opt, "hint")
	case 1:
		if key, ok := k.b54.KeyFor(x.KeyNo); ok {
			opt["scramble"] = fmt.Sprintf("%d/sf", key)
			delete(opt, "hint")
		}
	}
}
