package server

import (
	"fmt"
	"io/fs"
	"strings"

	"github.com/symysak/arib/internal/cliflag"
	nx "github.com/symysak/arib/internal/nxdn/server"
	"github.com/symysak/arib/internal/rx"
	"github.com/symysak/arib/internal/std-t102/l1"
	"github.com/symysak/arib/internal/std-t102/pane"
	"github.com/symysak/arib/internal/voice"
)

const System = "std-t102"

const GridHz = nx.GridHz

var DefaultInits = []int{0x0E4}

func AddDefaultInits(v ...int) { DefaultInits = append(append([]int(nil), v...), DefaultInits...) }

type (
	Monitor = nx.Monitor
	ChannelOpt = nx.ChannelOpt
	Station   = nx.Station
	TxRec     = nx.TxRec
	MsgRec    = nx.MsgRec
	FrameRec  = nx.FrameRec
	Snapshot  = nx.Snapshot
	Node      = nx.Node
	DataEvent = nx.DataEvent
)

type Config struct {
	Engine rx.Config
	Codec    string
	CodecOpt voice.Options
	Data string
	Init *int
	Opts map[int64]ChannelOpt
	NoTCH bool
	AudioAGC bool
	AudioTap func(idx int, pcm []int16, rate int)
	LedgerDir string
	B54Keys map[int]uint16
}

func Spec(b54Keys map[int]uint16) nx.Spec {
	return nx.Spec{
		Name: "STD-T102", System: System, Key: "t102",
		Title:        "ARIB STD-T102 第 2 編（狭帯域デジタル通信方式 SCPC/4 値 FSK。2400 シンボル/s・6.25 kHz 間隔・80 ms フレーム）",
		Dialect:      l1.Dialect,
		VoiceChannel: l1.VoiceChannel, DataChannel: l1.DataChannel,
		Inits:   func() []int { return DefaultInits },
		Panes:   []fs.FS{pane.FS()},
		Note:    stationNote,
		NewLink: func(h nx.Host) nx.Link { return newLink(h, b54Keys) },
	}
}

func NewMonitor(cfg Config) *Monitor {
	return nx.NewMonitor(Spec(cfg.B54Keys), nx.Config{Engine: cfg.Engine, Codec: cfg.Codec, CodecOpt: cfg.CodecOpt, Data: cfg.Data,
		Init: cfg.Init, Opts: cfg.Opts, NoTCH: cfg.NoTCH, AudioAGC: cfg.AudioAGC, AudioTap: cfg.AudioTap, LedgerDir: cfg.LedgerDir})
}

func stationNote(s Station) string {
	parts := []string{}
	if s.Init >= 0 {
		parts = append(parts, fmt.Sprintf("初期値 0x%03X", s.Init))
	}
	if s.U >= 0 {
		parts = append(parts, fmt.Sprintf("U=%d", s.U))
	}
	if s.Maker >= 0 {
		parts = append(parts, fmt.Sprintf("製造者 %d", s.Maker))
	}
	if s.Caller != "" {
		parts = append(parts, "発信者名「"+s.Caller+"」")
	}
	return strings.Join(parts, "・")
}

func OptKey(mhz float64) int64 { return nx.OptKey(mhz) }

func ParseInit(s string) (int, error) { return nx.ParseInit(s) }

func OptsFromChannels(list []cliflag.Channel) (map[int64]ChannelOpt, error) {
	return nx.OptsFromChannels(list)
}

func CodecNames() string { return nx.CodecNames(l1.VoiceChannel) }

func CheckCodec(name string, opt voice.Options) ([]string, error) {
	return nx.CheckCodec(l1.VoiceChannel, name, opt)
}
