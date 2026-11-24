package server


import (
	"io/fs"
	"time"

	"github.com/symysak/arib/internal/data"
	"github.com/symysak/arib/internal/rcr39qpsk/decoder"
	"github.com/symysak/arib/internal/rcr39qpsk/l1"
	"github.com/symysak/arib/internal/rcr39qpsk/l2"
	"github.com/symysak/arib/internal/voice"
)

type Spec struct {
	Name, System, Title string
	Dialect decoder.Dialect
	VoiceChannel voice.Channel
	RasterHz float64
	UpOffsetMHz float64
	IsUplink    func(mhz float64) bool
	UpAuto bool
	CarrierKind func(ci [4]int, ctl int) string
	CIText func(ci int) string
	CallSign func(id int) string
	AreaText func(scd int) string
	SCDOrg   func(scd int) string
	Panes []fs.FS
	NewLink func(h Host) Link
}

type Link interface {
	L3(s *decoder.Slot, ch string, f l2.Frame) *L3Result
	AfterLayers(s *decoder.Slot, ch string, f l2.Frame, raw []byte, r *L3Result)
	Act(s *decoder.Slot, ch string, f l2.Frame, r *L3Result, ms, addr string)
	MSI(a l2.Address) (text, org string, ok bool)
	L2Fields() l2.MSIFields
	FoldKey() int
	RCHText(info []uint8, up bool) string
	HintCall(slot int, h Hint)
	HintExt(h Hint)
	CallOpen(n int)
	CallStart(n int)
	Observe(n int, s *decoder.Slot, voice bool)
	CallEnding(n int)
	CallClosed(n int)
	Drop(n int)
	Slot(s *decoder.Slot)
	Data(n int) bool
	Mode(n int) (name, note string)
}

type L3Result struct {
	Summary string
	Piggy   Node
	Messages []L3Msg
	Sig any
}

type L3Msg struct {
	Entity, Name, Summary, Err string
	Tree                       Node
	Raw                        []byte
}

type Hint struct {
	MHz      float64
	Slot     int
	Scramble int
	Codec    string
	From     string
	Org      string
	Ctl      float64
	Src      string
	Up       bool
	Call any
	Ext  any
}

type Host interface {
	ID() int
	MHz() float64
	Up() bool
	Direct() *DirectOpt
	DownMHz() float64
	Now() float64
	StreamTime() time.Time
	LogDir() string
	Logf(n int, group, level, format string, a ...any)
	Note(n int, line string)
	CallEvent(n int, kind, text string)
	ChLabel(s *decoder.Slot, ch string) string
	Fresh(key string, t float64) bool
	EmitCall(r CallRec)
	EmitNet(ni NetInfo)
	EmitData(n int, ev data.Event)
	PutHint(h Hint)
	OnDirect(mhz float64, scramble int, mode l1.DirectMode)
	SetCACStructure(c l1.CACStructure)
	ClearCACStructure()
	SetPacket(n int, on bool)
	CodecHint(name, why string)
	CallOn(n int) bool
	CallT0(n int) float64
	EndCall(n int, why string)
	SetPeer(n int, peer string)
	Peer(n int) string
	SetMode(n int, name, note string)
	Org() string
	SetOrg(org string)
	SeenOrg(org string)
	SetVendor(text string)
}
