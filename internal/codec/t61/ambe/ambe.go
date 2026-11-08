package ambe

import (
	"fmt"
	"strconv"
	"strings"

	t61voice "github.com/symysak/arib/internal/std-t61/voice"
	"github.com/symysak/arib/internal/vocoder/ambe"
	"github.com/symysak/arib/internal/vocoder/ambe2-c"
	"github.com/symysak/arib/internal/voice"
)

const TCHBits = 256

const DefaultFramesPerRadioFrame = 2

const (
	NameAMBE = "ambe"
)

func init() {
	voice.Register(voice.Codec{
		Name:      NameAMBE,
		Desc:      "AMBE+2（TCH 256 ビットから。伝送路は未同定なので -codec-opt mode=d49|72 offsets=0/49/…）",
		Vocoder:   "AMBE+2（TCH 256・伝送路は未確定）",
		Transport: "TCH 256",
		Ch:        t61voice.TCH,
		Check:     "検査",
		Tentative: true,
		Available: ambe2c.Available,
		New: func(o voice.Options) (voice.Voice, error) {
			ao, err := parseAMBEOptions(o)
			if err != nil {
				return nil, err
			}
			return NewAMBE(ao)
		},
	})
}

func packBits(b []uint8) []byte {
	out := make([]byte, (len(b)+7)/8)
	for i, v := range b {
		out[i/8] |= (v & 1) << (7 - i%8)
	}
	return out
}


type Mode int

const (
	ModeD49 Mode = iota
	Mode72
)

func (m Mode) Bits() int {
	if m == Mode72 {
		return ambe.Bits3600
	}
	return 49
}

func (m Mode) String() string {
	if m == Mode72 {
		return "72"
	}
	return "d49"
}

type AMBEOptions struct {
	Mode      Mode
	Offsets   []int
	Positions [][]int
}

func parseAMBEOptions(o voice.Options) (AMBEOptions, error) {
	var ao AMBEOptions
	for k, v := range o {
		switch k {
		case "mode":
			switch v {
			case "d49":
				ao.Mode = ModeD49
			case "72":
				ao.Mode = Mode72
			default:
				return ao, fmt.Errorf("mode は d49 / 72: %q", v)
			}
		case "offsets":
			for _, p := range strings.Split(v, "/") {
				n, err := strconv.Atoi(strings.TrimSpace(p))
				if err != nil {
					return ao, fmt.Errorf("offsets は / 区切りの数: %q", v)
				}
				ao.Offsets = append(ao.Offsets, n)
			}
		default:
			return ao, fmt.Errorf("ambe に設定 %q はありません（mode / offsets）", k)
		}
	}
	return ao, nil
}

type ambeVoice struct {
	mode   Mode
	pos    [][]int
	width  int
	stream *ambe2c.Stream
}

func NewAMBE(o AMBEOptions) (voice.Voice, error) {
	w := o.Mode.Bits()
	pos := o.Positions
	if pos == nil {
		offs := o.Offsets
		if offs == nil {
			for i := 0; i < DefaultFramesPerRadioFrame; i++ {
				if s := i * w; s+w <= TCHBits {
					offs = append(offs, s)
				}
			}
		}
		for _, s := range offs {
			p := make([]int, w)
			for i := range p {
				p[i] = s + i
			}
			pos = append(pos, p)
		}
	}
	if len(pos) == 0 {
		return nil, fmt.Errorf("取り出す音声フレームの位置が空")
	}
	for _, p := range pos {
		if len(p) != w {
			return nil, fmt.Errorf("位置の数が %d ではなく %d", w, len(p))
		}
		for _, i := range p {
			if i < 0 || i >= TCHBits {
				return nil, fmt.Errorf("ビット位置 %d が TCH（0..%d）の外", i, TCHBits-1)
			}
		}
	}
	return &ambeVoice{mode: o.Mode, pos: pos, width: w}, nil
}

func (a *ambeVoice) Name() string {
	return fmt.Sprintf("AMBE+2（%s, %d フレーム/無線フレーム）", a.mode, len(a.pos))
}

func (a *ambeVoice) Transport(f voice.Frame) (voice.Unit, bool, error) {
	if len(f.Bits) != TCHBits {
		return voice.Unit{}, false, fmt.Errorf("TCH が %d ビットではなく %d", TCHBits, len(f.Bits))
	}
	frames := make([][]uint8, len(a.pos))
	for k, p := range a.pos {
		buf := make([]uint8, a.width)
		for i, bi := range p {
			buf[i] = f.Bits[bi]
		}
		frames[k] = buf
	}
	return voice.Unit{Data: frames}, true, nil
}

func (a *ambeVoice) Erase() (voice.Unit, bool) {
	return voice.Unit{Data: make([][]uint8, len(a.pos))}, true
}

func (a *ambeVoice) Decode(u voice.Unit) ([]int16, error) {
	frames, ok := u.Data.([][]uint8)
	if !ok {
		return nil, fmt.Errorf("AMBE の単位ではない: %T", u.Data)
	}
	return pushAMBE(&a.stream, a.mode, frames)
}

func (a *ambeVoice) Close() error { return closeAMBE(&a.stream) }

func pushAMBE(st **ambe2c.Stream, mode Mode, frames [][]uint8) ([]int16, error) {
	if *st == nil {
		s, err := ambe2c.NewStream(ambe.Rate3600)
		if err != nil {
			return nil, err
		}
		*st = s
	}
	var out []int16
	for _, b := range frames {
		var pcm []int16
		var err error
		switch {
		case b == nil:
			pcm, err = (*st).Lost()
		case mode == ModeD49:
			var d [49]uint8
			copy(d[:], b)
			a := ambe.FromParams49(d)
			pcm, err = (*st).Push(a[:])
		default:
			pcm, err = (*st).Push(b)
		}
		if err != nil {
			return nil, err
		}
		out = append(out, pcm...)
	}
	return out, nil
}

func closeAMBE(st **ambe2c.Stream) error {
	if *st == nil {
		return nil
	}
	err := (*st).Close()
	*st = nil
	return err
}
