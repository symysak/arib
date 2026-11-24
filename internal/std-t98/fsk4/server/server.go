package server

import (
	"fmt"
	"io/fs"
	"strconv"
	"strings"

	"github.com/symysak/arib/internal/cliflag"
	nxl1 "github.com/symysak/arib/internal/nxdn/l1"
	nx "github.com/symysak/arib/internal/nxdn/server"
	"github.com/symysak/arib/internal/rx"
	"github.com/symysak/arib/internal/std-t98/fsk4/chplan"
	"github.com/symysak/arib/internal/std-t98/fsk4/l1"
	"github.com/symysak/arib/internal/std-t98/fsk4/licensee"
	"github.com/symysak/arib/internal/std-t98/fsk4/pane"
	"github.com/symysak/arib/internal/voice"
)

const System = "std-t98-fsk4"

const GridHz = nx.GridHz

type (
	Monitor  = nx.Monitor
	Station  = nx.Station
	TxRec    = nx.TxRec
	MsgRec   = nx.MsgRec
	FrameRec = nx.FrameRec
	Snapshot = nx.Snapshot
	Node     = nx.Node
)

const AnyUC = -1

type Config struct {
	Engine rx.Config
	Codec    string
	CodecOpt voice.Options
	Data string
	Keys map[int]int
	NoTCH bool
	AudioAGC bool
	AudioTap func(idx int, pcm []int16, rate int)
	Licensees *licensee.DB
	LedgerDir string
	WC   string
	Opts map[int64]nx.ChannelOpt
}

func ParseWC(s string) (guess bool, init int, err error) {
	s = strings.TrimSpace(s)
	switch {
	case s == "":
		return false, -1, nil
	case strings.EqualFold(s, "auto"):
		return true, -1, nil
	}
	n, err := nx.ParseInit(s)
	if err != nil || n < 1 {
		return false, -1, fmt.Errorf("-wc %q は auto か 0x001〜0x1FF", s)
	}
	return false, n, nil
}

func Spec(keys map[int]int, db *licensee.DB, guessWC bool) nx.Spec {
	init := l1.WhiteningInit
	sp := nx.Spec{
		Name: "STD-T98", System: System, Key: "t98",
		Title:        "ARIB STD-T98 第 3 編（デジタル簡易無線・四値周波数偏位変調。2400 シンボル/s・6.25 kHz 間隔・80 ms フレーム）",
		Dialect:      l1.Dialect,
		VoiceChannel: l1.VoiceChannel, DataChannel: l1.DataChannel,
		Init: &init, InitWhy: "規格の値",
		SACCHProof: func(p nxl1.Parsed) bool { return p.SACCH != nil && l1.ReadUnit(p.SACCH.Head).First },
		Meta:       map[string]any{"channels": chplan.All()},
		Panes:      []fs.FS{pane.FS()},
		Note:       stationNote,
		NewLink:    func(h nx.Host) nx.Link { return newLink(h, keys, db) },
	}
	if guessWC {
		sp.Init, sp.InitWhy = nil, ""
		sp.Inits = func() []int { return []int{l1.WhiteningInit} }
	}
	return sp
}

func NewMonitor(cfg Config) *Monitor {
	guess, init, err := ParseWC(cfg.WC)
	if err != nil {
		guess, init = false, -1
	}
	var ip *int
	if init >= 0 {
		ip = &init
	}
	return nx.NewMonitor(Spec(cfg.Keys, cfg.Licensees, guess), nx.Config{Engine: cfg.Engine, Codec: cfg.Codec, CodecOpt: cfg.CodecOpt, Data: cfg.Data,
		Init: ip, Opts: cfg.Opts, NoTCH: cfg.NoTCH, AudioAGC: cfg.AudioAGC, AudioTap: cfg.AudioTap, LedgerDir: cfg.LedgerDir})
}

func OptsFromChannels(list []cliflag.Channel) (map[int64]nx.ChannelOpt, error) {
	return nx.OptsFromChannels(list)
}

func stationNote(s Station) string {
	parts := []string{}
	if c, ok := chplan.Lookup(s.MHz); ok {
		parts = append(parts, c.Label())
	}
	if s.U >= 0 {
		parts = append(parts, fmt.Sprintf("UC %d", s.U))
	}
	if s.Caller != "" {
		c := "呼出名称 " + s.Caller
		if n, _ := s.Ext["caller_licensee"].(string); n != "" {
			c += "（" + n + "）"
		}
		parts = append(parts, c)
	}
	return strings.Join(parts, "・")
}

func CodecNames() string { return nx.CodecNames(l1.VoiceChannel) }

func CheckCodec(name string, opt voice.Options) ([]string, error) {
	return nx.CheckCodec(l1.VoiceChannel, name, opt)
}

func ParseKeys(list []string) (map[int]int, error) {
	out := map[int]int{}
	for _, s := range list {
		uc := AnyUC
		ks := strings.TrimSpace(s)
		if a, b, ok := strings.Cut(ks, ":"); ok {
			n, err := strconv.ParseUint(strings.TrimSpace(a), 0, 16)
			if err != nil || n > 511 {
				return nil, fmt.Errorf("-key %q のユーザコードは 0〜511", s)
			}
			uc, ks = int(n), strings.TrimSpace(b)
		}
		k, err := strconv.ParseUint(ks, 0, 16)
		if err != nil || k < 1 || k > 0x7FFF {
			return nil, fmt.Errorf("-key %q は <秘話鍵 1〜32767> か <ユーザコード 0〜511>:<秘話鍵>", s)
		}
		out[uc] = int(k)
	}
	return out, nil
}
