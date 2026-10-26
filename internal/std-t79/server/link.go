package server


import (
	"fmt"
	"math"
	"sync"

	"github.com/symysak/arib/internal/std-t79/decoder"
	"github.com/symysak/arib/internal/std-t79/l2"
)

type linkKey struct {
	base int64
	slot int
	ch   string
	dlci string
}

type linkDir struct {
	seen   bool
	ns     int
	nsInfo string
	acked  bool
}

type linkState struct {
	mode string
	dir  [2]linkDir
	t    float64
}

type linkBoard struct {
	mu    sync.Mutex
	links map[linkKey]*linkState
}

const linkSec = 40.0

func (c *Channel) linkNotes(s *decoder.Slot, ch string, f l2.Frame) (notes []string, mode string) {
	if f.Addr.Octets == 0 || f.Err != "" || f.Ctl.Octets == 0 || f.Ctl.Name == "UI" || f.Ctl.Name == "XID" {
		return nil, ""
	}
	down := c.cfg.MHz
	d := 0
	if c.up != nil {
		down -= UpOffsetMHz
		d = 1
	}
	k := linkKey{base: int64(math.Round(down * 1e6 / RasterHz)), slot: s.Num, ch: logicalCh(s, ch, c.up != nil), dlci: f.Addr.Key()}
	notes, mode, event := c.std.links.step(k, d, c.tNow(), f)
	if event != "" {
		c.logf(s.Num, "l2", "info", "%s %s %s: %s", c.chLabel(s, ch), f.Addr.Key(), c.dirName(), event)
	}
	return notes, mode
}

func (b *linkBoard) step(k linkKey, d int, t float64, f l2.Frame) (notes []string, mode, event string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.links == nil {
		b.links = map[linkKey]*linkState{}
	}
	if len(b.links) > 512 {
		for kk, v := range b.links {
			if t-v.t > linkSec {
				delete(b.links, kk)
			}
		}
	}
	st := b.links[k]
	if st == nil || t-st.t > linkSec {
		st = &linkState{dir: [2]linkDir{{ns: -1}, {ns: -1}}}
		b.links[k] = st
	}
	st.t = t
	me, peer := &st.dir[d], &st.dir[1-d]
	me.seen = true
	ctl := f.Ctl
	switch ctl.Name {
	case "SABME", "SABMEI":
		st.mode = "設定中"
		me.ns, peer.ns = -1, -1
	case "DISC":
		st.mode = "解放中"
	case "UA":
		switch st.mode {
		case "設定中":
			st.mode, event = "マルチフレーム設定", "マルチフレーム設定（SABME → UA）"
		case "解放中":
			st.mode, event = "未設定", "解放（DISC → UA）"
		default:
			notes = append(notes, "UA に対応する SABME / DISC が見えていない（勧誘されない UA の可能性。§4.2.2.5.5.6）")
		}
	case "DM":
		if st.mode == "設定中" {
			event = "設定の拒否（SABME → DM）"
		} else if st.mode == "解放中" {
			event = "解放（DISC → DM）"
		}
		st.mode = "未設定"
	case "FRMR":
		event = "フレームリジェクト（FRMR）"
	}
	if ctl.Format == l2.FormatI || ctl.Format == l2.FormatS {
		nr := int(ctl.NR)
		if peer.ns >= 0 {
			switch {
			case nr == (peer.ns+1)%64:
				peer.acked = true
			case nr != peer.ns:
				notes = append(notes, fmt.Sprintf("N(R) = %d は相手の最後の I（N(S) = %d）から見て範囲外（V(A) ≦ N(R) ≦ V(S)。見えた範囲で）", nr, peer.ns))
			}
		}
	}
	if ctl.Format == l2.FormatI && !ctl.Partial {
		ns := int(ctl.NS)
		info := fmt.Sprintf("%x", f.Info)
		switch {
		case st.mode == "未設定":
			notes = append(notes, "マルチフレーム設定の前（解放の後）の I（見えた範囲で。§4.2.2.5.6）")
		}
		switch {
		case me.ns < 0:
		case ns == me.ns && info == me.nsInfo:
			notes = append(notes, fmt.Sprintf("再送（同じ N(S) = %d の I がまた来た。T200 の満了か REJ の後。§4.2.2.5.6）", ns))
		case ns == me.ns:
			notes = append(notes, fmt.Sprintf("N(S) = %d の重複（前の I と中身が違う）", ns))
		case ns != (me.ns+1)%64:
			notes = append(notes, fmt.Sprintf("N(S) が %d → %d と飛んだ（期待 %d。取りこぼしの可能性もある）", me.ns, ns, (me.ns+1)%64))
		case peer.seen && !me.acked:
			notes = append(notes, fmt.Sprintf("N(S) = %d の I の確認（N(R) = %d）が見えないまま次の I（k = 1。表 4.2.2.5-3）", me.ns, ns))
		}
		if ns != me.ns {
			me.acked = false
		}
		me.ns, me.nsInfo = ns, info
		if st.mode == "" {
			st.mode = "マルチフレーム設定"
		}
	}
	return notes, st.mode, event
}
