package server

import (
	"github.com/symysak/arib/internal/std-t61/callseq"
	"github.com/symysak/arib/internal/std-t61/l1"
)

type Event struct {
	Type string  `json:"type"`
	TSec float64 `json:"t_sec"`

	Snapshot *Snapshot `json:"snapshot,omitempty"`
	Meta     *Meta     `json:"meta,omitempty"`
	Station  *Station  `json:"station,omitempty"`
	Call     *Call     `json:"call,omitempty"`
	Const    *Const    `json:"const,omitempty"`
	Rec      *Rec      `json:"rec,omitempty"`
	Log      *LogLine  `json:"log,omitempty"`

	L1 *L1Rec `json:"l1,omitempty"`
	L2 *L2Rec `json:"l2,omitempty"`
	L3 *L3Rec `json:"l3,omitempty"`
	Seq *callseq.Call `json:"seq,omitempty"`
	Data any `json:"data,omitempty"`
}

type Meta struct {
	Input      string    `json:"input"`
	CenterMHz  float64   `json:"center_mhz"`
	SampleRate float64   `json:"sample_rate"`
	GainDB     float64   `json:"gain_db"`
	Channels   []float64 `json:"channels"`
	LogDir     string    `json:"log_dir"`
	Method string `json:"method"`
	T0WallMS  int64 `json:"t0_wall_ms"`
	Estimated bool  `json:"t0_estimated"`
	Finished  bool  `json:"finished"`
	L1All bool `json:"l1_all"`
}

type Snapshot struct {
	Generator string    `json:"generator"`
	Meta      Meta      `json:"meta"`
	Stations  []Station `json:"stations"`
	Calls     []Call    `json:"calls"`
	Logs      []LogLine `json:"logs"`
	Recs      []Rec     `json:"recs"`

	L1 []*L1Rec `json:"l1"`
	L2 []*L2Rec `json:"l2"`
	L3 []*L3Rec `json:"l3"`
	CallSeq []callseq.Call `json:"callseq"`
	Ext map[string]any `json:"ext,omitempty"`
}

type Station struct {
	Idx int     `json:"idx"`
	MHz float64 `json:"mhz"`

	Synced   bool    `json:"synced"`
	Kind     string  `json:"kind"`
	Uplink   bool    `json:"uplink"`
	Edition  string  `json:"edition"`
	SWName   string  `json:"sw_name"`
	SFPos    int     `json:"sf_pos"`
	LastTSec float64 `json:"last_t_sec"`

	RICHOK   bool     `json:"rich_ok"`
	RICHBits string   `json:"rich_bits"`
	RICHText string   `json:"rich_text"`
	RICHRows []l1.Row `json:"rich_rows"`
	RICHNote string `json:"rich_note,omitempty"`
	Mode     string `json:"mode"`
	CallType string `json:"call_type,omitempty"`
	CallDown string `json:"call_down,omitempty"`

	Frames      int `json:"frames"`
	SWDetected  int `json:"sw_detected"`
	Flywheel    int `json:"flywheel"`
	RICHOKNum   int `json:"rich_ok_num"`
	SACCHOK     int `json:"sacch_ok"`
	SACCHNum    int `json:"sacch_num"`
	SACCHFiller int `json:"sacch_filler"`
	FACCHOK     int `json:"facch_ok"`
	FACCHNum    int `json:"facch_num"`
	SBOK        int `json:"sb_ok"`
	SBNum       int `json:"sb_num"`
	SyncLoss    int `json:"sync_loss"`

	CoarseHz    float64 `json:"coarse_hz"`
	CFOHz       float64 `json:"cfo_hz"`
	PhaseErrDeg float64 `json:"phase_err_deg"`
	LevelDB     float64 `json:"level_db"`

	Ledger bool `json:"ledger"`
	LedgerLastMS int64 `json:"ledger_last_ms"`
	Note string `json:"note,omitempty"`

	Watched bool `json:"watched"`

	Recording bool   `json:"recording"`
	RecBase   string `json:"rec_base"`

	TA   int    `json:"ta"`
	SSC  int    `json:"ssc"`
	SBID string `json:"sb_id"`
	SBNote string `json:"sb_note,omitempty"`
	SBStale bool     `json:"sb_stale"`
	SBRows  []l1.Row `json:"sb_rows"`
	SBTSec  float64  `json:"sb_t_sec"`

	SACCHHex string `json:"sacch_hex"`
	RCHRaw   int    `json:"rch_raw"`
	FACCHHex string `json:"facch_hex"`
	NV2Hex string `json:"nv2_hex,omitempty"`
	NV2OK  int    `json:"nv2_ok"`
	CtlHex   string `json:"ctl_hex,omitempty"`
	CtlE     string `json:"ctl_e,omitempty"`
	CtlOK    int    `json:"ctl_ok"`
	CtlNum   int    `json:"ctl_num"`
	UpCtlOK  int    `json:"up_ctl_ok"`
	UpCtlNum int    `json:"up_ctl_num"`

	Ext map[string]any `json:"ext,omitempty"`
}

type Call struct {
	Idx    int      `json:"idx"`
	MHz    float64  `json:"mhz"`
	TSec   float64  `json:"t_sec"`
	Kind   string   `json:"kind"`
	Text   string   `json:"text"`
	ID     string   `json:"id,omitempty"`
	Frames int      `json:"frames,omitempty"`
	CRCOK  int      `json:"crc_ok,omitempty"`
	Rows   []l1.Row `json:"rows,omitempty"`
}

type Field struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

type Const struct {
	Idx    int       `json:"idx"`
	TSec   float64   `json:"t_sec"`
	Points []float32 `json:"points"`
}

type Rec struct {
	Idx    int     `json:"idx"`
	MHz    float64 `json:"mhz"`
	TSec   float64 `json:"t_sec"`
	Base   string  `json:"base"`
	Name   string  `json:"name"`
	Reason string  `json:"reason"`
	Open   bool    `json:"open"`
	Sec    float64 `json:"sec,omitempty"`
	Audio  bool    `json:"audio,omitempty"`
	IQRemoved bool `json:"iq_removed,omitempty"`
}

type LogLine struct {
	TSec float64 `json:"t_sec"`
	Idx   int    `json:"idx"`
	Group string `json:"group"`
	Text  string `json:"text"`
}
