package l1

import (
	"fmt"

	nx "github.com/symysak/arib/internal/nxdn/l1"
	"github.com/symysak/arib/internal/voice"
)

const VoiceChannel voice.Channel = "T98-TCH"

const TCHBits = nx.TCHBits

const WhiteningInit = 0x0E4

type RICH struct {
	F, Rsv, M, D, P uint8
	Raw uint8
}

func ReadRICH(raw uint8) RICH {
	return RICH{F: raw >> 7, Rsv: raw >> 5 & 3, M: raw >> 2 & 7, D: raw >> 1 & 1, P: raw & 1, Raw: raw}
}

func (r RICH) ParityOK() bool {
	x := r.Raw
	x ^= x >> 4
	x ^= x >> 2
	x ^= x >> 1
	return x&1 == 0
}

type Mode int

const (
	ModeReserved Mode = iota
	ModeSync
	ModeNonVoice1
	ModeNV2Voice
	ModeVoiceNV2
	ModeVoice
	ModeNonVoice2
	ModeIdle
)

func (m Mode) String() string {
	switch m {
	case ModeSync:
		return "同期バースト"
	case ModeNonVoice1:
		return "非音声 1"
	case ModeNV2Voice:
		return "非音声 2 + 音声"
	case ModeVoiceNV2:
		return "音声 + 非音声 2"
	case ModeVoice:
		return "音声"
	case ModeNonVoice2:
		return "非音声 2"
	case ModeIdle:
		return "終話"
	}
	return "不明"
}

func (r RICH) Mode() Mode {
	if !r.ParityOK() || r.Rsv != 0 || r.D != 0 {
		return ModeReserved
	}
	if r.F == 0 {
		if r.M == 4 {
			return ModeSync
		}
		return ModeReserved
	}
	switch r.M {
	case 0:
		return ModeNonVoice1
	case 1:
		return ModeNV2Voice
	case 2:
		return ModeVoiceNV2
	case 3:
		return ModeVoice
	case 4:
		return ModeNonVoice2
	case 5:
		return ModeIdle
	}
	return ModeReserved
}

func (r RICH) Valid() bool { return r.Mode() != ModeReserved }

func (m Mode) HalfName(i int) string {
	if m == ModeSync {
		return [2]string{"PICH", "未定義"}[i]
	}
	return [2]string{"TCH1", "TCH2"}[i]
}

func (m Mode) Halves() [2]nx.Half {
	switch m {
	case ModeSync:
		return [2]nx.Half{nx.HalfCoded, nx.HalfRaw}
	case ModeNV2Voice:
		return [2]nx.Half{nx.HalfCoded, nx.HalfTCH}
	case ModeVoiceNV2:
		return [2]nx.Half{nx.HalfTCH, nx.HalfCoded}
	case ModeVoice:
		return [2]nx.Half{nx.HalfTCH, nx.HalfTCH}
	case ModeNonVoice2:
		return [2]nx.Half{nx.HalfCoded, nx.HalfCoded}
	}
	return [2]nx.Half{nx.HalfRaw, nx.HalfRaw}
}

func Dialect(raw uint8) nx.Layout {
	m := ReadRICH(raw).Mode()
	if m == ModeReserved {
		return nx.Layout{}
	}
	return nx.Layout{Valid: true, Format: nx.FormatVoice, Half: m.Halves()}
}

func (r RICH) String() string {
	s := fmt.Sprintf("F%d M%03b D%d", r.F, r.M, r.D)
	switch {
	case !r.ParityOK():
		return s + " パリティ誤り"
	case r.Rsv != 0:
		return s + fmt.Sprintf("（予約ビット %02b）", r.Rsv)
	case r.D != 0:
		return s + "（D は予約）"
	}
	m := r.Mode()
	if m == ModeReserved {
		return s + "（M は予約）"
	}
	if m == ModeSync {
		return s + " 同期バースト（SACCH + PICH）"
	}
	return s + " 通信用チャネル " + m.String()
}

func RICHBits(f, m, d uint8) []uint8 {
	raw := f<<7 | m<<2 | d<<1
	x := raw
	x ^= x >> 4
	x ^= x >> 2
	x ^= x >> 1
	return nx.RICHBitsRaw(raw | x&1)
}

type Unit struct {
	First  bool
	Remain int
	Type   uint8
}

func ReadUnit(head uint8) Unit {
	return Unit{First: head>>7 == 1, Remain: int(head >> 5 & 3), Type: head & 0x1F}
}

func (u Unit) Head() uint8 {
	h := uint8(u.Remain&3)<<5 | u.Type&0x1F
	if u.First {
		h |= 0x80
	}
	return h
}
