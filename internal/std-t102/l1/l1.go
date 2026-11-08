package l1

import (
	"fmt"

	"github.com/symysak/arib/internal/data"
	"github.com/symysak/arib/internal/voice"
)

const (
	FrameSymbols = 192
	FrameBits = 2 * FrameSymbols
	FSWSymbols = 10
	BodySymbols = FrameSymbols - FSWSymbols
	BodyBits = 2 * BodySymbols
	RICHBits = 16
	SACCHBits = 60
	TCHBits   = 144
	DataBits  = 348
	FrameSec = 0.08
)

var FSW = [FSWSymbols]int{-3, 1, -3, 3, -3, -3, 3, 3, -1, 3}

const FSWHex = "CDF59"

func DibitBits(s float32) (hi, lo float32) {
	a := s
	if a < 0 {
		a = -a
	}
	return s, 2 - a
}

func SoftBits(sym []float32) []float32 {
	out := make([]float32, 2*len(sym))
	for i, s := range sym {
		out[2*i], out[2*i+1] = DibitBits(s)
	}
	return out
}

func Hard(soft []float32) []uint8 {
	out := make([]uint8, len(soft))
	for i, v := range soft {
		if v < 0 {
			out[i] = 1
		}
	}
	return out
}

func Whitening(init, n int) []uint8 {
	reg := uint32(init) & 0x1FF
	out := make([]uint8, n)
	for k := range out {
		out[k] = uint8(reg & 1)
		fb := (reg ^ (reg >> 4)) & 1
		reg = (reg >> 1) | (fb << 8)
	}
	return out
}

const WhiteningInits = 512

func Dewhiten(body []float32, init int) []float32 {
	pn := Whitening(init, len(body))
	out := make([]float32, len(body))
	for i, s := range body {
		if pn[i] == 1 {
			s = -s
		}
		out[i] = s
	}
	return out
}

type RICH struct {
	F, M, O, D, P uint8
	Raw uint8
}

func DecodeRICH(sym []float32) RICH {
	var raw uint8
	for i := 0; i < 8; i++ {
		raw <<= 1
		if sym[i] < 0 {
			raw |= 1
		}
	}
	return RICH{F: raw >> 6, M: raw >> 4 & 3, O: raw >> 2 & 3, D: raw >> 1 & 1, P: raw & 1, Raw: raw}
}

func (r RICH) ParityOK() bool {
	x := r.Raw >> 4
	x ^= x >> 2
	x ^= x >> 1
	return x&1 == r.P
}

func (r RICH) Valid() bool { return r.ParityOK() && r.F == 2 }

type Format int

const (
	FormatUnknown Format = iota
	FormatVoice
	FormatData
	FormatBurst
)

func (f Format) String() string {
	switch f {
	case FormatVoice:
		return "音声"
	case FormatData:
		return "データ"
	case FormatBurst:
		return "データ（バースト）"
	}
	return "不明"
}

func (r RICH) Format() Format {
	if !r.Valid() {
		return FormatUnknown
	}
	switch r.M {
	case 0, 2:
		return FormatVoice
	case 1:
		return FormatData
	}
	return FormatBurst
}

func (r RICH) Superframe() bool { return r.M == 2 }

type Half int

const (
	HalfTCH Half = iota
	HalfFACCH1
)

func (r RICH) Halves() (first, second Half) {
	first, second = HalfFACCH1, HalfFACCH1
	if r.O&2 != 0 {
		first = HalfTCH
	}
	if r.O&1 != 0 {
		second = HalfTCH
	}
	return
}

type DataKind int

const (
	DataReserved DataKind = iota
	DataFACCH2
	DataUDCH1
	DataUDCH2
)

func (k DataKind) String() string {
	switch k {
	case DataFACCH2:
		return "FACCH2"
	case DataUDCH1:
		return "UDCH1"
	case DataUDCH2:
		return "UDCH2"
	}
	return "予約"
}

func (r RICH) DataKind() DataKind {
	switch {
	case r.M == 1 && r.O == 0:
		return DataFACCH2
	case r.M == 1 && r.O == 3:
		return DataUDCH1
	case r.M == 3 && r.O == 3:
		return DataUDCH2
	}
	return DataReserved
}

func (r RICH) String() string {
	s := fmt.Sprintf("F%02b M%02b O%02b D%d", r.F, r.M, r.O, r.D)
	if !r.ParityOK() {
		return s + " パリティ誤り"
	}
	if r.F != 2 {
		return s + "（F は予約）"
	}
	switch r.Format() {
	case FormatVoice:
		a, b := r.Halves()
		names := [2]string{"TCH", "FACCH1"}
		sf := "SACCH 1 フレーム"
		if r.Superframe() {
			sf = "SACCH スーパーフレーム"
		}
		s += fmt.Sprintf(" 音声 %s/%s・%s", names[a], names[b], sf)
	case FormatData, FormatBurst:
		s += " データ " + r.DataKind().String()
	}
	if r.D == 1 {
		s += "・下り（基地局経由）"
	} else {
		s += "・上り / 直接"
	}
	return s
}

const (
	VoiceChannel voice.Channel = "T102-TCH"
	DataChannel data.Channel = "T102-UDCH1"
)
