package l1

import "fmt"


type RICH struct {
	F, M, O, D, P uint8
	Raw uint8
}

func ReadRICH(raw uint8) RICH {
	return RICH{F: raw >> 6, M: raw >> 4 & 3, O: raw >> 2 & 3, D: raw >> 1 & 1, P: raw & 1, Raw: raw}
}

func (r RICH) ParityOK() bool {
	x := r.Raw >> 4
	x ^= x >> 2
	x ^= x >> 1
	return x&1 == r.P
}

func (r RICH) Valid() bool {
	return r.ParityOK() && r.F == 2 && !(r.M == 0 && r.O != 0)
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

func (r RICH) Halves() (first, second Half) {
	first, second = HalfCoded, HalfCoded
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
		names := map[Half]string{HalfTCH: "TCH", HalfCoded: "FACCH1"}
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

func (r RICH) Layout() Layout {
	l := Layout{Valid: r.Valid(), Format: r.Format()}
	switch l.Format {
	case FormatVoice:
		l.Half[0], l.Half[1] = r.Halves()
	case FormatData:
		l.Coded348 = r.DataKind() == DataFACCH2 || r.DataKind() == DataUDCH1
	}
	return l
}

func FMOD(raw uint8) Layout { return ReadRICH(raw).Layout() }
