package server

import (
	"bytes"
	"encoding/json"
	"math"
	"strconv"
)


type Event any

type orderedCounts []nameCount

type nameCount struct {
	Name  string
	Count int
}

func (c orderedCounts) MarshalJSON() ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteByte('{')
	for i, kv := range c {
		if i > 0 {
			buf.WriteByte(',')
		}
		k, err := json.Marshal(kv.Name)
		if err != nil {
			return nil, err
		}
		buf.Write(k)
		buf.WriteByte(':')
		buf.WriteString(strconv.Itoa(kv.Count))
	}
	buf.WriteByte('}')
	return buf.Bytes(), nil
}

type candidate struct {
	Code int
	Name string
}

func (c candidate) MarshalJSON() ([]byte, error) {
	return json.Marshal([]any{c.Code, c.Name})
}

func round(v float64, digits int) float64 {
	p := math.Pow(10, float64(digits))
	return math.Round(v*p) / p
}

type nullableFloat struct {
	V  float64
	OK bool
}

func (n nullableFloat) MarshalJSON() ([]byte, error) {
	if !n.OK {
		return []byte("null"), nil
	}
	return json.Marshal(n.V)
}

func someFloat(v float64, digits int) nullableFloat {
	return nullableFloat{V: round(v, digits), OK: true}
}

var noFloat = nullableFloat{}

type controlMsgEvent struct {
	Type    string  `json:"type"`
	T       float64 `json:"t"`
	Pos     int     `json:"pos"`
	MsgType int     `json:"msg_type"`
	Name    string  `json:"name"`
	Channel string  `json:"channel"`
	CRCOK   bool    `json:"crc_ok"`
	Busy bool `json:"busy"`
	Section string `json:"section"`
	SW      string  `json:"sw"`
	Corr    float64 `json:"corr"`
	EVM     float64 `json:"evm"`
	PowerDB float64 `json:"power_db"`

	RawHex string         `json:"raw_hex"`
	Fields map[string]any `json:"fields"`
}

type tchSecondEvent struct {
	Type   string         `json:"type"`
	T      float64        `json:"t"`
	Counts map[string]int `json:"counts"`
}

type broadcastEvent struct {
	Type     string  `json:"type"`
	T        float64 `json:"t"`
	WindowID int     `json:"window_id"`
	WallMS   *int64  `json:"wall_ms"`
	Target   *target `json:"target"`
}

type qualityEvent struct {
	Type       string        `json:"type"`
	T          float64       `json:"t"`
	CFOHz      nullableFloat `json:"cfo_hz"`
	EVMMedian  nullableFloat `json:"evm_median"`
	EVMBest    nullableFloat `json:"evm_best"`
	CRCOKRate  nullableFloat `json:"crc_ok_rate"`
	MsgsPerS   float64       `json:"msgs_per_s"`
	LevelDBFS  nullableFloat `json:"level_dbfs"`
	SyncLocked bool          `json:"sync_locked"`
	Overflows  int           `json:"overflows"`
}

type constellationEvent struct {
	Type   string      `json:"type"`
	T      float64     `json:"t"`
	Points [][]float64 `json:"points"`
}

func constellationPoints(slots [][]complex64, maxPoints int) [][]float64 {
	var syms []complex64
	for _, s := range slots {
		syms = append(syms, s...)
	}
	pts := make([][]float64, 0, len(syms))
	if len(syms) > maxPoints {
		for i := 0; i < maxPoints; i++ {
			idx := int(float64(i) * float64(len(syms)-1) / float64(maxPoints-1))
			s := syms[idx]
			pts = append(pts, []float64{round(float64(real(s)), 3), round(float64(imag(s)), 3)})
		}
		return pts
	}
	for _, s := range syms {
		pts = append(pts, []float64{round(float64(real(s)), 3), round(float64(imag(s)), 3)})
	}
	return pts
}

type audioStatusEvent struct {
	Type     string `json:"type"`
	WindowID int    `json:"window_id"`

	Frames    int     `json:"frames"`
	CRC7OK    int     `json:"crc7_ok"`
	CRC7Fail  int     `json:"crc7_fail"`
	CRC7Rate  float64 `json:"crc7_rate"`
	Filled    int     `json:"filled"`
	Stale     int     `json:"stale"`
	PLCRepeat int     `json:"plc_repeat"`
	PLCMute   int     `json:"plc_mute"`

	CDistMax  int     `json:"c_dist_max"`
	CDistMean float64 `json:"c_dist_mean"`
	CDistBad  int     `json:"c_dist_bad"`

	DecodedSeconds  float64 `json:"decoded_seconds"`
	DecodeAttempted bool    `json:"decode_attempted"`
	Note            string  `json:"note"`
	WavPath         string  `json:"wav_path"`

	Quality     string `json:"quality"`
	QualityFrom int    `json:"quality_from"`
}

type iqStatusEvent struct {
	Type     string  `json:"type"`
	WindowID int     `json:"window_id"`
	Path     string  `json:"path"`
	Seconds  float64 `json:"seconds"`
	FS       float64 `json:"fs"`
	Done     bool    `json:"done"`
	Note     string  `json:"note"`
}

type seedDetectedEvent struct {
	Type       string      `json:"type"`
	T          float64     `json:"t"`
	Seed       int         `json:"seed"`
	Score      float64     `json:"score"`
	CRCHits    int         `json:"crc_hits"`
	Known      int         `json:"known"`
	NSlots     int         `json:"n_slots"`
	Candidates []candidate `json:"candidates"`
}

type controlSummaryEvent struct {
	Type    string          `json:"type"`
	T       float64         `json:"t"`
	Control *controlSummary `json:"control"`
}

type logEvent struct {
	Type string  `json:"type"`
	T    float64 `json:"t"`
	Text string  `json:"text"`
}

func newLogEvent(t float64, text string) logEvent {
	return logEvent{Type: "log", T: round(t, 2), Text: text}
}
