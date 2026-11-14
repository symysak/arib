package server

import (
	"github.com/symysak/arib/internal/data"
)

const (
	EvSnapshot    = "snapshot"
	EvStation     = "station"
	EvStationGone = "station_gone"
	EvLog         = "log"
	EvMeta        = "meta"
	EvRec         = "rec"
	EvFrame       = "nxdnframe"
	EvMsg         = "nxdnmsg"
	EvTx          = "nxdntx"
	EvSignal      = "nxdnsignal"
	EvEye         = "nxdneye"
)

type Event struct {
	Type     string     `json:"type"`
	Idx      int        `json:"idx"`
	Snapshot *Snapshot  `json:"snapshot,omitempty"`
	Meta     *Meta      `json:"meta,omitempty"`
	Station  *Station   `json:"station,omitempty"`
	Log      *LogLine   `json:"log,omitempty"`
	Rec      *Rec       `json:"rec,omitempty"`
	Frame    *FrameRec  `json:"frame,omitempty"`
	Msg      *MsgRec    `json:"msg,omitempty"`
	Tx       *TxRec     `json:"tx,omitempty"`
	Signal   *Signal    `json:"signal,omitempty"`
	Eye      *Eye       `json:"eye,omitempty"`
	Data     *DataEvent `json:"data,omitempty"`
}

type Node struct {
	Label    string `json:"label"`
	Value    string `json:"value,omitempty"`
	Bits     string `json:"bits,omitempty"`
	Range    string `json:"range,omitempty"`
	Note     string `json:"note,omitempty"`
	Children []Node `json:"children,omitempty"`
}

type FrameRec struct {
	No     int64   `json:"no"`
	Idx    int     `json:"idx"`
	TSec   float64 `json:"t_sec"`
	Frame  int64   `json:"frame"`
	Kind   string  `json:"kind"`
	Info   string  `json:"info"`
	Why    string  `json:"why"`
	Repeat int     `json:"repeat"`
	Tree   []Node  `json:"tree"`
	RawHex string  `json:"raw_hex"`
	key    string
}

type Message struct {
	Ch   string `json:"ch"`
	Type int    `json:"type"`
	Name    string `json:"name"`
	Summary string `json:"summary"`
	Maker int    `json:"maker"`
	Raw   []byte `json:"raw"`
	End bool `json:"end,omitempty"`
}

type MsgRec struct {
	No     int64   `json:"no"`
	Idx    int     `json:"idx"`
	TSec   float64 `json:"t_sec"`
	Repeat int     `json:"repeat"`
	Msg    Message `json:"msg"`
	U      int     `json:"u"`
	Tree   []Node  `json:"tree"`
	Key string `json:"-"`
	Ext any `json:"-"`
}

type TxRec struct {
	ID     int     `json:"id"`
	Idx    int     `json:"idx"`
	TSec   float64 `json:"t_sec"`
	EndSec float64 `json:"end_sec"`
	Open   bool    `json:"open"`
	Kind string `json:"kind"`
	U       int      `json:"u"`
	Maker   int      `json:"maker"`
	D       int      `json:"d"`
	Frames  int      `json:"frames"`
	Voice   int      `json:"voice"`
	VoiceOK int      `json:"voice_ok"`
	Data    int      `json:"data"`
	Msgs    []string `json:"msgs"`
	Call string `json:"call,omitempty"`
	Name string `json:"name,omitempty"`
	Ext   map[string]any `json:"ext,omitempty"`
	End   string         `json:"end,omitempty"`
	Base  string         `json:"base,omitempty"`
	Audio bool           `json:"audio,omitempty"`
}

type Signal struct {
	TSec    float64 `json:"t_sec"`
	LevelDB float64 `json:"level_db"`
	DevHz   float64 `json:"dev_hz"`
	DCHz    float64 `json:"dc_hz"`
	EVM     float64 `json:"evm"`
	FSWErr  float64 `json:"fsw_err"`
	Hist []int `json:"hist"`
}

type Eye struct {
	TSec   float64     `json:"t_sec"`
	Traces [][]float32 `json:"traces"`
}

type DataEvent struct {
	Idx int     `json:"idx"`
	MHz float64 `json:"mhz"`
	data.Event
}

type LogLine struct {
	TSec  float64 `json:"t_sec"`
	Idx   int     `json:"idx"`
	Group string  `json:"group"`
	Level string  `json:"level"`
	Text  string  `json:"text"`
}

type Rec struct {
	Base   string  `json:"base"`
	Name   string  `json:"name"`
	Idx    int     `json:"idx"`
	TSec   float64 `json:"t_sec"`
	Open   bool    `json:"open"`
	Reason string  `json:"reason"`
	Sec    float64 `json:"sec,omitempty"`
	Audio  bool    `json:"audio,omitempty"`
}

type InputMeta struct {
	ID         string  `json:"id"`
	Label      string  `json:"label"`
	Input      string  `json:"input"`
	CenterMHz  float64 `json:"center_mhz"`
	SampleRate float64 `json:"sample_rate"`
	GainDB     float64 `json:"gain_db"`
	AGC        bool    `json:"agc"`
	Finished   bool    `json:"finished"`
}

type Meta struct {
	Input      string      `json:"input"`
	CenterMHz  float64     `json:"center_mhz"`
	SampleRate float64     `json:"sample_rate"`
	GainDB     float64     `json:"gain_db"`
	Channels   []float64   `json:"channels"`
	LogDir     string      `json:"log_dir"`
	T0WallMS   int64       `json:"t0_wall_ms"`
	Estimated  bool        `json:"t0_estimated"`
	Finished   bool        `json:"finished"`
	Network    bool        `json:"network"`
	Scan       bool        `json:"scan"`
	Inputs     []InputMeta `json:"inputs"`
	System     string      `json:"system"`
	SymbolRate float64     `json:"symbol_rate"`
	Codecs     string      `json:"codecs"`
	CodecMode  string      `json:"codec_mode"`
	DataMode   string      `json:"data_mode"`
	AudioRate  int         `json:"audio_rate"`
	Ext map[string]any `json:"ext,omitempty"`
}

type Station struct {
	Idx       int            `json:"idx"`
	MHz       float64        `json:"mhz"`
	OffsetHz  float64        `json:"offset_hz"`
	Watched   bool           `json:"watched"`
	Synced    bool           `json:"synced"`
	Recording bool           `json:"recording"`
	Talking   bool           `json:"talking"`
	LevelDB   *float64       `json:"level_db,omitempty"`
	LastTSec  float64        `json:"last_t_sec"`
	Note      string         `json:"note"`
	Ext       map[string]any `json:"ext"`
	Frames   int     `json:"frames"`
	FSWOK    int     `json:"fsw_ok"`
	RICHOK   int     `json:"rich_ok"`
	CRCOK    int     `json:"crc_ok"`
	CRCAll   int     `json:"crc_all"`
	Init     int     `json:"init"`
	InitWhy  string  `json:"init_why"`
	U        int     `json:"u"`
	Maker    int     `json:"maker"`
	Mode     string  `json:"mode"`
	Codec    string  `json:"codec"`
	DevHz    float64 `json:"dev_hz"`
	DCHz     float64 `json:"dc_hz"`
	EVM      float64 `json:"evm"`
	Txs      int     `json:"txs"`
	Overflow int     `json:"overflows"`
	Voice   int `json:"voice"`
	VoiceOK int `json:"voice_ok"`
	Caller string `json:"caller,omitempty"`
	Profile Profile `json:"profile"`
}

type Snapshot struct {
	Generator string            `json:"generator"`
	Meta      Meta              `json:"meta"`
	Stations  []Station         `json:"stations"`
	Logs      []LogLine         `json:"logs"`
	Recs      []Rec             `json:"recs"`
	Frames    []FrameRec        `json:"nxdnframes"`
	Msgs      []MsgRec          `json:"nxdnmsgs"`
	Txs       []TxRec           `json:"nxdntxs"`
	Signals   map[string]Signal `json:"nxdnsignals"`
	Ext       map[string]any    `json:"ext"`
}
