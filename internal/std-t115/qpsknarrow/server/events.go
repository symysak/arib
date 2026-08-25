package server

import (
	"encoding/json"

	"github.com/symysak/arib/internal/std-t115/qpsknarrow/control"
	"github.com/symysak/arib/internal/std-t115/qpsknarrow/municipality"
)

const (
	EvSnapshot       = "snapshot"
	EvFrame          = "frame"
	EvControl        = "control"
	EvBroadcastStart = "broadcast_start"
	EvBroadcastEnd   = "broadcast_end"
	EvQuality        = "quality"
	EvSignal         = "signal"
	EvConstellation  = "constellation"
	EvScramble       = "scramble"
	EvLog            = "log"
	EvFinished       = "finished"
)

type Event struct {
	Type string          `json:"type"`
	Data json.RawMessage `json:"data,omitempty"`
}

type FrameInfo struct {
	TSec     float64 `json:"t_sec"`
	Kind     string  `json:"kind"`
	CorrPeak float64 `json:"corr"`
	PhaseDeg float64 `json:"phase_deg"`
	SWOK     bool    `json:"sw_ok"`
	CRCOK    bool    `json:"crc_ok"`
	Channel string `json:"channel,omitempty"`
	AMRSN int `json:"amr_sn"`
}

type ControlInfo struct {
	TSec     float64 `json:"t_sec"`
	Kind     string  `json:"kind"`
	Channel  string  `json:"channel"`
	Type     int     `json:"msg_type"`
	TypeName string  `json:"msg_type_name"`
	RawHex   string  `json:"raw_hex"`
	Summary  string  `json:"summary"`

	Busy             bool   `json:"busy"`
	ManufacturerCode int    `json:"mfr_code"`
	ManufacturerName string `json:"mfr_name"`
	LicenseeInfo     int    `json:"licensee_info"`
	ChSwitchToSC     bool   `json:"ch_switch_to_sc"`
	ChSwitchTiming   int    `json:"ch_switch_timing"`
	AMRSN            int    `json:"amr_sn"`

	Notify *NotifyInfo `json:"notify,omitempty"`

	Bits control.Expanded `json:"bits"`
}

type NotifyInfo struct {
	MediaCode     int    `json:"media_code"`
	Media         string `json:"media"`
	TransProt     int    `json:"trans_prot"`
	CallNo        int    `json:"call_no"`
	SubStationID  int    `json:"substation_id"`
	Simultaneous  bool   `json:"simultaneous"`
	Emergency     bool   `json:"emergency"`
	ForcedVolume  bool   `json:"forced_volume"`
	RecordRelease bool   `json:"record_release"`
	NumberNotify  bool   `json:"number_notify"`
	VolumeSetting int    `json:"volume_setting"`
	TimeSplitOK   bool   `json:"time_split_ok"`
	SplitNo       int    `json:"split_no"`
	SpareOct5     int    `json:"spare_oct5"`
	SpareOct6     int    `json:"spare_oct6"`
	SpareOct9to12 uint32 `json:"spare_oct9_12"`
	Target        string `json:"target"`
}

type BroadcastInfo struct {
	ID        int     `json:"id"`
	StartSec  float64 `json:"start_sec"`
	EndSec    float64 `json:"end_sec"`
	Target    string  `json:"target"`
	CallNo    int     `json:"call_no"`
	Emergency bool    `json:"emergency"`
	MidJoin bool `json:"mid_join"`
	VoiceFrames  int     `json:"voice_frames"`
	VoiceSeconds float64 `json:"voice_seconds"`
	EndReason string `json:"end_reason,omitempty"`
	AudioURL string `json:"audio_url,omitempty"`
	IQURL string `json:"iq_url,omitempty"`
}

type ScrambleInfo struct {
	Init int `json:"init"`
	Locked bool `json:"locked"`
	Pinned bool `json:"pinned"`
	Confidence float64 `json:"confidence"`
	Prominence float64 `json:"prominence"`
	Frames int `json:"frames"`
	Source string `json:"source"`
	TSec float64 `json:"t_sec"`

	MunicipalCode     int    `json:"municipal_code"`
	Municipality      string `json:"municipality"`
	MunicipalityLabel string `json:"municipality_label"`
	MunicipalityKnown bool   `json:"municipality_known"`
}

func (s *ScrambleInfo) SetMunicipality() {
	m := municipality.FromScramble(s.Init)
	s.MunicipalCode = m.Code
	s.Municipality = m.Name
	s.MunicipalityLabel = m.Label()
	s.MunicipalityKnown = m.Known
}

type QualityInfo struct {
	Frames    int `json:"frames"`
	SB0       int `json:"sb0"`
	SC        int `json:"sc"`
	CCHTotal  int `json:"cch_total"`
	CCHOK     int `json:"cch_ok"`
	TCHTotal  int `json:"tch_total"`
	TCHOK     int `json:"tch_ok"`
	SWErrors  int `json:"sw_errors"`
	VoiceSFs  int `json:"voice_superframes"`
	VoiceDrop int `json:"voice_dropped"`
	VoiceFilled int `json:"voice_filled"`
	VoiceCapped int     `json:"voice_capped"`
	AudioSec    float64 `json:"audio_sec"`
	Throughput float64 `json:"throughput"`
	ElapsedSec float64 `json:"elapsed_sec"`
	Overflows int `json:"overflows"`
}

type SignalInfo struct {
	TSec float64 `json:"t_sec"`
	CFOHz float64 `json:"cfo_hz"`
	CFOProm float64 `json:"cfo_prom_db"`
	TimingTau float64 `json:"timing_tau"`
	EVM float64 `json:"evm"`
	LevelDB float64 `json:"level_db"`
	CorrPeak float64 `json:"corr_peak"`
}

type ConstellationInfo struct {
	TSec float64 `json:"t_sec"`
	Points []float32 `json:"points"`
}

type LogInfo struct {
	TSec  float64 `json:"t_sec"`
	Level string  `json:"level"`
	Text  string  `json:"text"`
}

type Snapshot struct {
	T0WallMS int64 `json:"t0_wall_ms"`
	T0Estimated bool `json:"t0_estimated"`

	Source     string  `json:"source"`
	SourceDesc string  `json:"source_desc"`
	Network    bool    `json:"network"`
	SampleRate float64 `json:"sample_rate"`
	OffsetHz   float64 `json:"offset_hz"`
	DurationS  float64 `json:"duration_sec"`
	Realtime   bool    `json:"realtime"`

	System       string  `json:"system"`
	SymbolRate   float64 `json:"symbol_rate"`
	FrameBits    int     `json:"frame_bits"`
	RollOff      float64 `json:"roll_off"`
	SW1Hex       string  `json:"sw1_hex"`
	SW3Hex       string  `json:"sw3_hex"`
	ScrambleInit int     `json:"scramble_init"`

	AudioAvailable bool `json:"audio_available"`
	AudioRate      int  `json:"audio_rate"`

	LogDir  string `json:"log_dir"`
	LogPath string `json:"log_path"`
	IQRate float64 `json:"iq_rate"`
	Scramble ScrambleInfo `json:"scramble"`

	Quality       QualityInfo       `json:"quality"`
	Signal        SignalInfo        `json:"signal"`
	Constellation ConstellationInfo `json:"constellation"`
	Controls      []ControlInfo     `json:"controls"`
	Broadcasts    []BroadcastInfo   `json:"broadcasts"`
	Frames        []FrameInfo       `json:"frames"`
	Logs          []LogInfo         `json:"logs"`
	Current       *BroadcastInfo    `json:"current,omitempty"`
}

func mustJSON(v any) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		return json.RawMessage(`null`)
	}
	return b
}

func ev(t string, v any) Event { return Event{Type: t, Data: mustJSON(v)} }
