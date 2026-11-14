package server

import (
	"fmt"

	"github.com/symysak/arib/internal/data"
	"github.com/symysak/arib/internal/nxdn/decoder"
	nxl1 "github.com/symysak/arib/internal/nxdn/l1"
	nx "github.com/symysak/arib/internal/nxdn/server"
	"github.com/symysak/arib/internal/std-t102/l1"
	"github.com/symysak/arib/internal/std-t102/l3"
)

type link struct {
	h     nx.Host
	sacch l3.SACCH
	b54 *l3.B54Asm
	b54Call *l3.B54
	sfPos int
}

func newLink(h nx.Host, keys map[int]uint16) *link {
	return &link{h: h, b54: l3.NewB54Asm(keys)}
}

func (k *link) TxStart() { k.b54Call = nil }

func (k *link) TxEnd(t float64, opened bool) {
	if opened {
		k.b54Events(t, k.b54.Reset())
	}
	k.b54Call = nil
	k.sacch.Reset()
}

func (k *link) Reset() { k.sacch.Reset() }

func (k *link) Frame(f decoder.Frame, kd nxl1.Keyed) nx.Reading {
	p := kd.Parsed
	r := l1.ReadRICH(p.RICH)
	valid := p.Layout.Valid
	rd := nx.Reading{Mode: r.String(), Kind: r.Format().String(), D: int(r.D), User: -1}

	switch {
	case !valid || !r.Superframe():
		k.sfPos = 0
	case p.SACCH != nil && p.SACCH.CRCOK:
		s, _ := l1.SU(p.SACCH.Head)
		k.sfPos = 3 - int(s&3)
	default:
		k.sfPos = (k.sfPos + 1) & 3
	}
	rd.Sub = k.sfPos

	var msgs []nx.MsgRec
	if s := p.SACCH; s != nil && valid {
		ss, u := l1.SU(s.Head)
		rd.User = int(u)
		if s.CRCOK {
			if b, ok := k.sacch.Push(r.Superframe(), ss, u, s.Data[:]); ok {
				if !r.Superframe() {
					b = b[:2]
				}
				msgs = append(msgs, msgRec(f.TimeSec, l3.Parse("SACCH", b), int(u)))
			}
		} else {
			k.sacch.Reset()
		}
	}
	for i, h := range p.Half {
		if h.Coded != nil && h.Coded.CRCOK {
			b := nxl1.CodedBytes(h.Coded)
			m := msgRec(f.TimeSec, l3.Parse("FACCH1", b[:]), -1)
			if i == 0 || len(msgs) == 0 || msgs[len(msgs)-1].Key != m.Key {
				msgs = append(msgs, m)
			}
		}
	}
	if d := p.Data; d != nil {
		_, u := l1.SU(d.Head)
		rd.User = int(u)
		if d.CRCOK {
			kind := r.DataKind()
			switch m := l3.Parse(kind.String(), d.Body[:]); {
			case kind == nxl1.DataFACCH2:
				msgs = append(msgs, msgRec(f.TimeSec, m, int(u)))
			case m.B54 != nil:
				msgs = append(msgs, msgRec(f.TimeSec, m, int(u)))
			}
			bits := append(nxl1.HeadInfo(d.Head, make([]uint8, 18))[:8], nxl1.BytesToBits(d.Body[:])...)
			rd.Data = append(rd.Data, data.Frame{Ch: l1.DataChannel, No: f.No, TimeSec: f.TimeSec, Bits: bits, Route: kind.String()})
		}
	}
	rd.Msgs = msgs
	rd.After = func() { k.b54Msgs(f.TimeSec, msgs) }
	rd.EndAfterVoice = valid && r.M == 0
	return rd
}

func msgRec(t float64, m l3.Message, u int) nx.MsgRec {
	tree := []Node{{Label: m.Ch, Value: m.Name}}
	for _, r := range m.Rows {
		tree[0].Children = append(tree[0].Children, Node{Label: r.Name, Value: r.Text, Bits: r.Bits})
	}
	if u >= 0 {
		tree[0].Children = append([]Node{{Label: "ユーザコード U", Value: fmt.Sprintf("%d", u)}}, tree[0].Children...)
	}
	return nx.MsgRec{TSec: t, U: u, Tree: tree, Repeat: 1, Key: fmt.Sprintf("%s/%X/%d", m.Ch, m.Raw, u), Ext: m,
		Msg: nx.Message{Ch: m.Ch, Type: int(m.Type), Name: m.Name, Summary: m.Summary(), Maker: m.Maker, Raw: m.Raw, End: m.Type == l3.TypeEnd}}
}
