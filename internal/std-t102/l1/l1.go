package l1

import (
	"github.com/symysak/arib/internal/data"
	nx "github.com/symysak/arib/internal/nxdn/l1"
	"github.com/symysak/arib/internal/voice"
)

const (
	VoiceChannel voice.Channel = "T102-TCH"
	DataChannel data.Channel = "T102-UDCH1"
)

const TCHBits = nx.TCHBits

type RICH = nx.RICH

func ReadRICH(raw uint8) RICH { return nx.ReadRICH(raw) }

var Dialect nx.Dialect = nx.FMOD

func SU(head uint8) (s, u uint8) { return head >> 6, head & 0x3F }

func SUHead(s, u uint8) uint8 { return s<<6 | u&0x3F }

func SACCHInfo(s, u uint8, data []uint8) []uint8 { return nx.HeadInfo(SUHead(s, u), data) }
