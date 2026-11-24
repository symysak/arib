package server

import (
	"fmt"
	"io/fs"

	"github.com/symysak/arib/internal/citycodes"
	fampane "github.com/symysak/arib/internal/rcr39qpsk/pane"
	fam "github.com/symysak/arib/internal/rcr39qpsk/server"
	"github.com/symysak/arib/internal/std-t79/numbering"
	"github.com/symysak/arib/internal/std-t79/pane"
	"github.com/symysak/arib/internal/std-t79/tch"
	"github.com/symysak/arib/internal/voice"
)

const System = "std-t79"

const SystemUp = System + "-up"

const UpOffsetMHz = -9.0

const RasterHz = 12500.0

func IsUplinkMHz(mhz float64) bool { return mhz >= 259.5 && mhz < 267 }

type (
	Config          = fam.Config
	Monitor         = fam.Monitor
	Standard        = fam.Standard
	StdConfig       = fam.StdConfig
	DirectOpt       = fam.DirectOpt
	DirectStation   = fam.DirectStation
	Station         = fam.Station
	Snapshot        = fam.Snapshot
	CallRec         = fam.CallRec
	NetInfo         = fam.NetInfo
	Node            = fam.Node
	Row             = fam.Row
	L1Rec           = fam.L1Rec
	L2Rec           = fam.L2Rec
	L3Rec           = fam.L3Rec
	Rec             = fam.Rec
	LogLine         = fam.LogLine
	CarrierInfo     = fam.CarrierInfo
	SlotInfo        = fam.SlotInfo
	ChannelSnapshot = fam.ChannelSnapshot
	Meta            = fam.Meta
	Event           = fam.Event
)

func Spec() fam.Spec {
	sh := &shared{nets: &netBoard{}, vendor: &vendorFile{}}
	return fam.Spec{
		Name:         "STD-T79",
		System:       System,
		Title:        "ARIB STD-T79（都道府県・市町村デジタル移動通信システム TYPE1。π/4 QPSK 16 ksym/s・4 スロット TDMA）",
		VoiceChannel: tch.VoiceChannel,
		RasterHz:     RasterHz,
		UpOffsetMHz:  UpOffsetMHz,
		IsUplink:     IsUplinkMHz,
		CarrierKind:  carrierKind,
		CIText:       ciText,
		CallSign:     func(id int) string { return numbering.CallSign(id).Text() },
		AreaText:     numbering.AreaText,
		SCDOrg:       scdOrg,
		Panes:        []fs.FS{pane.FS(), fampane.FS()},
		NewLink:      func(h fam.Host) fam.Link { return newLink(h, sh) },
	}
}

func NewStandard(cfg StdConfig) *Standard { return fam.NewStandard(Spec(), cfg) }
func NewMonitor(cfg Config) *Monitor      { return fam.NewMonitor(Spec(), cfg) }

func Codecs() []voice.Codec { return fam.Codecs(tch.VoiceChannel) }
func CodecNames() string    { return fam.CodecNames(tch.VoiceChannel) }
func CheckCodec(name string, opt voice.Options) ([]string, error) {
	return fam.CheckCodec(tch.VoiceChannel, "T79", name, opt)
}

func carrierKind(ci [4]int, _ int) string {
	switch {
	case ci[0] > ci[3] && ci[0] > 0:
		return "制御キャリア"
	case ci[3] > 0:
		return "通信キャリア"
	}
	return ""
}

func ciText(ci int) string {
	switch ci {
	case 0:
		return "00 制御キャリア"
	case 3:
		return "11 通信キャリア"
	}
	return fmt.Sprintf("%02b（未定義）", ci)
}

func scdOrg(scd int) string {
	switch {
	case scd < 1000 || scd >= 48000:
		return ""
	case scd%1000 == 0:
		return numbering.PrefName(scd / 1000)
	}
	if name, ok := citycodes.Name(scd); ok {
		return name
	}
	return ""
}

type shared struct {
	nets   *netBoard
	vendor *vendorFile
}
