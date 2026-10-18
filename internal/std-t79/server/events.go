package server

const (
	EvSnapshot    = "snapshot"
	EvStation     = "station"
	EvStationGone = "station_gone"
	EvLog         = "log"
	EvMeta        = "meta"
	EvRec         = "rec"
	EvT79         = "t79"
	EvL1          = "l1"
	EvL2          = "l2"
	EvL3          = "l3"
	EvCall        = "t79call"
	EvNet         = "t79net"
	EvConst       = "t79const"
)

type Event struct {
	Type     string     `json:"type"`
	Idx      int        `json:"idx"`
	Snapshot *Snapshot  `json:"snapshot,omitempty"`
	Meta     *Meta      `json:"meta,omitempty"`
	Station  *Station   `json:"station,omitempty"`
	Log      *LogLine   `json:"log,omitempty"`
	Rec      *Rec       `json:"rec,omitempty"`
	Data     *Detail    `json:"data,omitempty"`
	L1       *L1Rec     `json:"l1,omitempty"`
	L2       *L2Rec     `json:"l2,omitempty"`
	L3       *L3Rec     `json:"l3,omitempty"`
	Call     *CallRec   `json:"call,omitempty"`
	Net      *NetInfo   `json:"net,omitempty"`
	Const    *ConstInfo `json:"const,omitempty"`
}

type SlotInfo struct {
	Num  int    `json:"num"`
	SW   string `json:"sw"`
	Kind string `json:"kind"`
	SFPos int `json:"sf"`
	Scramble string `json:"scramble"`
	Call     bool   `json:"call"`
	ID    int     `json:"id"`
	Voice int     `json:"voice"`
	LastT float64 `json:"last_t_sec"`
	Peer string `json:"peer,omitempty"`
	Mode     string  `json:"mode,omitempty"`
	ModeNote string  `json:"mode_note,omitempty"`
	CC       int     `json:"cc"`
	PhaseE   float64 `json:"phase_err_deg"`
}

type CarrierInfo struct {
	TSec   float64 `json:"t_sec"`
	Locked bool    `json:"locked"`
	Kind  string      `json:"kind"`
	CC    int         `json:"cc"`
	Slots [4]SlotInfo `json:"slots"`
	Vendor   string  `json:"vendor,omitempty"`
	CoarseHz float64 `json:"coarse_hz"`
	CFOHz    float64 `json:"cfo_hz"`
	LevelDB  float64 `json:"level_db"`
	Codec string `json:"codec,omitempty"`

	SlotsN    int `json:"slots_n"`
	SWFound   int `json:"sw_found"`
	Locks     int `json:"locks"`
	Losses    int `json:"losses"`
	CACOK     int `json:"cac_ok"`
	CACTotal  int `json:"cac_total"`
	SACCHOK   int `json:"sacch_ok"`
	SACCHN    int `json:"sacch_total"`
	FACCHOK   int `json:"facch_ok"`
	SBOK      int `json:"sb_ok"`
	SBTotal   int `json:"sb_total"`
	Calls     int `json:"calls"`
	Overflows int `json:"overflows"`
	EUnits    int `json:"e_units,omitempty"`
	EAllowed  int `json:"e_allowed,omitempty"`
	EReceived int `json:"e_received,omitempty"`
	PEMatched int `json:"pe_matched,omitempty"`
	UpCAC     int `json:"up_cac,omitempty"`
}

type Detail struct {
	Carrier CarrierInfo `json:"carrier"`
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
	Audio bool `json:"audio,omitempty"`
}

type Meta struct {
	Input      string    `json:"input"`
	CenterMHz  float64   `json:"center_mhz"`
	SampleRate float64   `json:"sample_rate"`
	GainDB     float64   `json:"gain_db"`
	Channels   []float64 `json:"channels"`
	LogDir     string    `json:"log_dir"`
	T0WallMS   int64     `json:"t0_wall_ms"`
	Estimated  bool      `json:"t0_estimated"`
	Finished   bool      `json:"finished"`
	Network    bool      `json:"network"`
	Scan       bool      `json:"scan"`

	System     string  `json:"system"`
	SymbolRate float64 `json:"symbol_rate"`
	SlotBits   int     `json:"slot_bits"`
	L1All bool `json:"l1_all"`
	Codecs    string `json:"codecs"`
	CodecMode string `json:"codec_mode"`
	UpInput     string  `json:"up_input,omitempty"`
	UpCenterMHz float64 `json:"up_center_mhz,omitempty"`
	Inputs []InputMeta `json:"inputs"`
}

type Station struct {
	Idx       int            `json:"idx"`
	MHz       float64        `json:"mhz"`
	OffsetHz  float64        `json:"offset_hz"`
	Watched   bool           `json:"watched"`
	Synced    bool           `json:"synced"`
	Recording bool           `json:"recording"`
	LevelDB   *float64       `json:"level_db,omitempty"`
	LastTSec  float64        `json:"last_t_sec"`
	Note      string         `json:"note,omitempty"`
	Ext       map[string]any `json:"ext"`

	Dir      string    `json:"dir"`
	PairMHz  float64   `json:"pair_mhz"`
	Kind     string    `json:"kind"`
	CC       int       `json:"cc"`
	Org      string    `json:"org"`
	Slots    [4]string `json:"slots"`
	Peers    [4]string `json:"peers"`
	Modes    [4]string `json:"modes"`
	SWPct    float64   `json:"sw_pct"`
	PhaseErr float64   `json:"phase_err_deg"`
	CFOHz    float64   `json:"cfo_hz"`
	CACPct   float64   `json:"cac_pct"`
	SACCHPct float64   `json:"sacch_pct"`
	Calls    int       `json:"calls"`
	Codec    string    `json:"codec,omitempty"`
}

type ChannelSnapshot struct {
	Carrier CarrierInfo `json:"carrier"`
}

type Snapshot struct {
	Generator string                     `json:"generator"`
	Meta      Meta                       `json:"meta"`
	Stations  []Station                  `json:"stations"`
	Logs      []LogLine                  `json:"logs"`
	Recs      []Rec                      `json:"recs"`
	T79       map[string]ChannelSnapshot `json:"t79"`
	L1        []L1Rec                    `json:"l1"`
	L2        []L2Rec                    `json:"l2"`
	L3        []L3Rec                    `json:"l3"`
	Calls     []CallRec                  `json:"calls"`
	Nets      []NetInfo                  `json:"nets"`
}
