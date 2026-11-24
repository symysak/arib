package server

import (
	"fmt"
	"io/fs"
	"strings"
	"sync"

	"github.com/symysak/arib/internal/rcr39qpsk/decoder"
	"github.com/symysak/arib/internal/rcr39qpsk/l1"
	fampane "github.com/symysak/arib/internal/rcr39qpsk/pane"
	fam "github.com/symysak/arib/internal/rcr39qpsk/server"
	"github.com/symysak/arib/internal/std-39/qpsk"
	"github.com/symysak/arib/internal/std-39/qpsk/nonvoice"
	"github.com/symysak/arib/internal/std-39/qpsk/pane"
	"github.com/symysak/arib/internal/std-39/qpsk/tch"
	"github.com/symysak/arib/internal/voice"
)

const System = "std-39-qpsk"

const VoiceChannel = tch.VoiceChannel

const RasterHz = 6250.0

type (
	Config    = fam.Config
	Monitor   = fam.Monitor
	Standard  = fam.Standard
	StdConfig = fam.StdConfig
	Station   = fam.Station
	Snapshot  = fam.Snapshot
)

func Spec() fam.Spec { return SpecWith(0) }

func SpecWith(foMHz float64) fam.Spec {
	sh := &shared{fo: foMHz, foFixed: foMHz > 0}
	return fam.Spec{
		Name:         "STD-39",
		System:       System,
		Title:        "ARIB RCR STD-39 第 2 編（狭帯域デジタル通信方式 TDMA。π/4 シフト QPSK 16 ksym/s・4 スロット）",
		Dialect:      qpsk.Dialect(),
		VoiceChannel: VoiceChannel,
		RasterHz:     RasterHz,
		IsUplink:     func(float64) bool { return false },
		UpAuto:       true,
		CarrierKind:  carrierKind,
		CIText:       func(ci int) string { return fmt.Sprintf("%02b %s", ci, qpsk.CIName(ci)) },
		CallSign:     callSign,
		AreaText:     func(scd int) string { return fmt.Sprint(scd) },
		SCDOrg:       func(int) string { return "" },
		Panes:        []fs.FS{pane.FS(), fampane.FS()},
		NewLink:      func(h fam.Host) fam.Link { return newLink(h, sh) },
	}
}

func NewStandard(cfg StdConfig) *Standard { return fam.NewStandard(Spec(), cfg) }
func NewMonitor(cfg Config) *Monitor      { return fam.NewMonitor(Spec(), cfg) }

func NewMonitorWith(foMHz float64, cfg Config) *Monitor { return fam.NewMonitor(SpecWith(foMHz), cfg) }

func CodecNames() string { return fam.CodecNames(VoiceChannel) }
func CheckCodec(name string, opt voice.Options) ([]string, error) {
	return fam.CheckCodec(VoiceChannel, "STD-39", name, opt)
}

func carrierKind(ci [4]int, ctl int) string {
	switch {
	case ctl > 0:
		return "制御キャリア"
	case ci[0]+ci[3] > 0:
		return "通信キャリア"
	}
	return ""
}

func callSign(id int) string {
	if id>>19&1 == 1 {
		return fmt.Sprintf("個別番号 %d", id&0x7FFFF)
	}
	return fmt.Sprintf("識別子 0・%d（規定は識別子 1）", id&0x7FFFF)
}

func newLink(h fam.Host, sh *shared) *link {
	l := &link{h: h, sh: sh}
	for n := range l.ev.inv {
		for i := range l.ev.inv[n] {
			l.ev.inv[n][i] = -1
		}
	}
	l.nvInit()
	return l
}

type link struct {
	h  fam.Host
	sh *shared
	ev evidence
	st [4]slotState
	cacMiss int
	cacOff  bool
	net string
	nvs nonvoice.Slots
}

func (l *link) RCHText(info []uint8, up bool) string { return l1.RCHVendorText(0, info, up) }
func (l *link) Slot(s *decoder.Slot)                 { l.ev.slot(s); l.nvSlot(s) }


type evidence struct {
	mu                         sync.Mutex
	ccInit, ci11, offST3, mark int
	ones                       int
	inverted                   int
	inv [4][8]int64
}

func (e *evidence) slot(s *decoder.Slot) {
	if s.Up {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if s.Control && s.CAC != nil && s.CAC.OK {
		cc := s.CCDetected
		if cc < 0 {
			cc = s.CC
		}
		switch s.CACInit {
		case l1.InitAllOnes:
			e.ones++
		case l1.InitFromCC(cc):
			e.ccInit++
		}
		if s.CI == 3 {
			e.ci11++
		}
		if s.Num != 3 {
			e.offST3++
		}
	}
	n := s.Num & 3
	if s.SW.Inverted && (s.SW.Kind == l1.SWNormal || s.SW.Kind == l1.SWSuper) {
		copy(e.inv[n][1:], e.inv[n][:7])
		e.inv[n][0] = s.Frame
		if s.SW.Errs <= 1 {
			e.inverted++
		}
	}
	unmarked := func(second int64) bool {
		for _, f := range e.inv[n] {
			if f == second-1 {
				return false
			}
		}
		return true
	}
	if s.FACCH != nil && unmarked(s.Frame) {
		e.mark++
	}
	for _, x := range s.FACCHLate {
		if unmarked(x.Frame) {
			e.mark++
		}
	}
}

func Evidence(m *Monitor) (ok bool, score float64, text string) {
	var t evidence
	m.EachLink(func(_ int, x fam.Link) {
		l, isSTD39 := x.(*link)
		if !isSTD39 {
			return
		}
		l.ev.mu.Lock()
		t.ccInit += l.ev.ccInit
		t.ci11 += l.ev.ci11
		t.offST3 += l.ev.offST3
		t.mark += l.ev.mark
		t.ones += l.ev.ones
		t.inverted += l.ev.inverted
		l.ev.mu.Unlock()
	})
	pts := t.ccInit + t.ci11 + t.offST3 + t.mark
	var p []string
	for _, x := range []struct {
		n    int
		what string
	}{{t.ccInit, "下り制御が CC 由来で解けた"}, {t.ci11, "CI 11 で CAC"}, {t.offST3, "ST#3 以外で CAC"}, {t.mark, "印の無い FACCH"}} {
		if x.n > 0 {
			p = append(p, fmt.Sprintf("%s %d", x.what, x.n))
		}
	}
	switch {
	case t.inverted > 0:
		text = fmt.Sprintf("下りの同期ワードの反転 %d（T79 の FACCH の印）", t.inverted)
		if len(p) > 0 {
			text += "・" + strings.Join(p, "・")
		}
		return false, 0, text
	case pts == 0:
		if t.ones > 0 {
			return false, 0, fmt.Sprintf("下り制御はオール 1（T79 と区別できない）%d", t.ones)
		}
		return false, 0, ""
	}
	return pts >= 3, min(1, float64(pts)/10), strings.Join(p, "・")
}
