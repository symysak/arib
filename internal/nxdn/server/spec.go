package server

import (
	"io/fs"

	"github.com/symysak/arib/internal/data"
	"github.com/symysak/arib/internal/nxdn/decoder"
	"github.com/symysak/arib/internal/nxdn/l1"
	"github.com/symysak/arib/internal/voice"
)


type Spec struct {
	Name, System, Title string
	Key string
	Dialect l1.Dialect
	VoiceChannel voice.Channel
	DataChannel  data.Channel
	Init    *int
	InitWhy string
	Inits func() []int
	Candidates []int
	SACCHProof func(p l1.Parsed) bool
	Meta map[string]any
	Panes []fs.FS
	Note func(Station) string
	NewLink func(h Host) Link
}

type Link interface {
	Frame(f decoder.Frame, kd l1.Keyed) Reading
	FrameKey(p l1.Parsed) (key, info string)
	Tree(f decoder.Frame, kd l1.Keyed) []Node
	TxStart()
	TxEnd(t float64, opened bool)
	Reset()
	Profile() Profile
	VoiceOpt(codec string, opt map[string]string)
}

type Reading struct {
	Mode string
	Kind string
	TxKind string
	D, User int
	Msgs []MsgRec
	Data []data.Frame
	Sub int
	EndAfterVoice bool
	After func()
}

type Host interface {
	ID() int
	MHz() float64
	Logf(group, level, format string, a ...any)
	Note(line string)
	EmitData(ev data.Event)
	Tx(edit func(*TxRec) bool)
	EditStation(edit func(*Station), publish bool)
	Station() Station
	Init() (int, bool)
	Codec() string
	VoiceHint() string
	DataFormat() string
}

type KV struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

type Profile struct {
	Std   string `json:"std"`
	Impl  string `json:"impl"`
	Codec string `json:"codec"`
	Keys  []KV   `json:"keys"`
}
