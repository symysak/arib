package server


import (
	"fmt"
	"regexp"
	"strings"

	"github.com/symysak/arib/internal/data"
	"github.com/symysak/arib/internal/rcr39qpsk/decoder"
	"github.com/symysak/arib/internal/rcr39qpsk/l1"
	fam "github.com/symysak/arib/internal/rcr39qpsk/server"
	"github.com/symysak/arib/internal/std-39/qpsk/l3"
	"github.com/symysak/arib/internal/std-39/qpsk/nonvoice"
)

type nvHint struct{ method string }

func (l *link) nvInit() {
	l.nvs.Mark = l.h.SetPacket
	l.nvs.Log = func(n int, text string) { l.h.Logf(n, "call", "info", "%s", text) }
	l.nvs.CtxOf = func() l3.Ctx { return l3.Ctx{Dir: l.dir(), FreqMHz: l.freq} }
}

func (l *link) nvStart(n int, method string, scramble int, why string, sure bool) {
	if method == "" {
		return
	}
	l.sh.setNVMethod(method)
	l.nvs.Start(n, method, scramble, sure, why)
}

func (l *link) nvSignal(n int, ch string, sig l3.Signal) {
	if m := nonvoice.MethodFromSignal(sig); m != "" {
		l.sh.setNVMethod(m)
	}
	if n < 0 || n > 3 || ch == "CAC" {
		return
	}
	if l.nvs.Active(n) {
		l.nvs.Observe(n, sig)
		return
	}
	l.nvStart(n, nonvoice.MethodOnChannel(sig), -1, "通信チャネルの信号が名乗った", true)
}

func (l *link) nvSlot(s *decoder.Slot) {
	n := s.Num
	if n < 0 || n > 3 || s.Control || len(s.Hard) < l1.SlotBits {
		return
	}
	idle := s.SW.Kind == l1.SWIdle
	if !idle && s.SW.Kind != l1.SWNormal && s.SW.Kind != l1.SWSuper {
		return
	}
	if !idle && !l.nvs.Active(n) && l.h.Up() {
		l.nvStart(n, l.sh.nvMethodNow(), -1, "網で分かった方式を試す（上りへは指示が届かないので、スクランブルは当てる）", false)
	}
	for _, f := range l.nvs.Feed(n, l.h.Up(), idle, s.Hard, s.TSec) {
		l.nvFrame(n, f)
	}
}

func (l *link) nvData(n int) bool { return l.nvs.Data(n) }

func (l *link) nvFrame(n int, f nonvoice.Frame) {
	if f.Kind == nonvoice.KindMessage {
		l.nvMessage(n, f)
	}
	if f.Kind == nonvoice.KindMessage && f.Signal != nil {
		addr := nvAddr(f)
		l.actSig(&decoder.Slot{Num: n, TSec: f.T, CC: -1, SFPos: -1, CACCh: l1.CACUnknown}, "UDCH", *f.Signal, addr, addr)
	}
	if f.Kind == nonvoice.KindCRCError {
		l.h.Logf(n, "data", "warn", "非音声 ST#%d %s", n, f.Text)
	}
}

func (l *link) nvMessage(n int, f nonvoice.Frame) {
	if !l.h.Fresh(fmt.Sprintf("nv/%d/%x", n, f.Payload), f.T) {
		return
	}
	rows := make([]data.Row, 0, len(f.Fields)+1)
	for _, x := range f.Fields {
		rows = append(rows, data.Row{Name: x.Name, Text: x.Value})
	}
	rows = append(rows, data.Row{Name: "本体（16 進）", Text: fmt.Sprintf("% X", f.Payload)})
	l.h.EmitData(n, data.Event{Kind: data.KindMessage, TimeSec: f.T, Format: f.Method + "（" + nonvoice.AppendixOf(f.Method) + "）",
		Label: nvLabel(f.Text), Rows: rows, Raw: f.Payload})
}

func nvAddr(f nonvoice.Frame) string {
	if f.L2 != nil && (f.L2.Addr.HasMSI() || f.L2.Addr.HasSMSI()) {
		return f.L2.Addr.Key()
	}
	for _, x := range f.Fields {
		if x.Name == "受信アドレス" || x.Name == "発信アドレス" {
			if i := strings.IndexAny(x.Value, "（ "); i > 0 {
				return "NV:" + x.Value[:i]
			}
			return "NV:" + x.Value
		}
	}
	return ""
}

var nvHexText = regexp.MustCompile(`: [0-9A-F]+「`)

func nvLabel(text string) string {
	if i := strings.Index(text, "] "); strings.HasPrefix(text, "[") && i >= 0 {
		text = text[i+2:]
	}
	return nvHexText.ReplaceAllString(text, "「")
}

func nvHintOf(sig l3.Signal) any {
	if m := nonvoice.MethodFromSignal(sig); m != "" {
		return nvHint{method: m}
	}
	return nil
}

func (l *link) HintExt(h fam.Hint) {
	if x, ok := h.Ext.(nvHint); ok {
		l.nvStart(h.Slot, x.method, h.Scramble, "無線チャネル指定（"+h.From+"）", true)
		return
	}
	l.nvStop(h.Slot)
}

func (l *link) nvStop(n int) { l.nvs.Stop(n) }
