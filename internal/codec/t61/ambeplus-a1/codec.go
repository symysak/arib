package ambeplusa1

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/symysak/arib/internal/std-t61/l1"
	"github.com/symysak/arib/internal/std-t61/scpc-a1f40"
	t61voice "github.com/symysak/arib/internal/std-t61/voice"
	"github.com/symysak/arib/internal/vocoder/ambe"
	"github.com/symysak/arib/internal/vocoder/ambe2-c"
	"github.com/symysak/arib/internal/voice"
)

const Name = "ambeplus-a1"

const Variant = "k7f40"

func init() {
	voice.Register(voice.Codec{
		Name:      Name,
		Desc:      "AMBE+ 2000（第 1 編 付録 1 の TCH1 に 40 ビットのフレーム。スクランブルの初期値は無音から当てる。-codec-opt wc=0..511 で固定）",
		Vocoder:   "AMBE+ 2000",
		Transport: "付録 1 TCH1 f40（K=7 の 3 区画・40 ビット × 8 / クラスタ）",
		Ch:        t61voice.TCH1,
		Check:     "FEC",
		Available: ambe2c.Available,
		New:       newFromOptions,
		Probe:     Probe,
	})
}

func newFromOptions(o voice.Options) (voice.Voice, error) {
	wc, hint := -1, -1
	for k, s := range o {
		switch k {
		case "wc":
			n, err := strconv.ParseInt(s, 0, 32)
			if err != nil || n < 0 || n >= l1.A1Patterns {
				return nil, fmt.Errorf("wc は 0..511: %q", s)
			}
			wc = int(n)
		case "hint":
			name, w, ok := strings.Cut(s, ":")
			if n, err := strconv.ParseInt(w, 0, 32); ok && name == Name && err == nil && n >= 0 && n < l1.A1Patterns {
				hint = int(n)
			}
		default:
			return nil, fmt.Errorf("%s に設定 %q はありません（wc）", Name, k)
		}
	}
	return New(wc, hint), nil
}

const (
	keyVotes = 2
	maxPending = 500
	maxFill = 50
	frameSec = 0.04
)

type slot struct {
	sec  int
	f    [scpca1f40.FrameBits]uint8
	lost bool
}

type Voice struct {
	wc     int
	hinted bool
	fixed  bool
	votes  map[int]int
	pend   []slot
	lastNo  int64
	lastFN  int
	lastT   float64
	stream  *ambe2c.Stream
	picked  int
	noteWhy string
}

func New(wc, hint int) *Voice {
	v := &Voice{wc: -1, votes: map[int]int{}, lastNo: -1, picked: -1}
	switch {
	case wc >= 0:
		v.wc, v.fixed, v.noteWhy = wc, true, "（固定）"
	case hint >= 0:
		v.wc, v.hinted, v.noteWhy = hint, true, "（台帳か前の通話の組）"
	}
	return v
}

func (v *Voice) Name() string { return "AMBE+ 2000（付録 1 TCH1）" }

func (v *Voice) Key() (wc int, ok bool) { return v.wc, v.wc >= 0 }

func softOf(f voice.Frame) []float32 {
	if len(f.Soft) == len(f.Bits) && f.Soft != nil {
		return f.Soft
	}
	s := make([]float32, len(f.Bits))
	for i, b := range f.Bits {
		s[i] = 1 - 2*float32(b&1)
	}
	return s
}

func (v *Voice) Transport(f voice.Frame) (voice.Unit, bool, error) {
	if len(f.Bits) != l1.A1TCH1Bits {
		return voice.Unit{}, false, fmt.Errorf("TCH1 が %d ビットではなく %d", l1.A1TCH1Bits, len(f.Bits))
	}
	fn := f.Sub & 3
	var slots []slot
	if v.lastNo >= 0 {
		miss := int(f.No - v.lastNo)
		if f.TimeSec > v.lastT && v.lastT > 0 {
			if n := int(math.Round((f.TimeSec - v.lastT) / frameSec)); n > miss {
				miss = n
			}
		}
		for i, k := 1, v.lastFN; i < miss && i <= maxFill; i++ {
			k = (k + 1) & 3
			for _, sec := range scpca1f40.VoiceSections(k) {
				slots = append(slots, slot{sec: sec, lost: true})
			}
		}
	}
	v.lastNo, v.lastFN, v.lastT = f.No, fn, f.TimeSec
	secs := scpca1f40.VoiceSections(fn)
	if secs == nil {
		return voice.Unit{}, false, nil
	}
	split, _ := scpca1f40.Split(softOf(f))
	chk := voice.Check{Valid: true, OK: true}
	for _, sec := range secs {
		s := split[sec]
		if !s.Voice() {
			chk.OK = false
			slots = append(slots, slot{sec: sec, lost: true})
			continue
		}
		chk.Corrected += s.Errors
		fr := s.Frame()
		slots = append(slots, slot{sec: sec, f: fr})
		if s.Errors == 0 && !v.fixed {
			if wc, ok := scpca1f40.KeyFromSilence(fr, sec); ok {
				v.vote(wc)
			}
		}
	}
	if v.wc < 0 {
		v.pend = append(v.pend, slots...)
		if over := len(v.pend) - maxPending; over > 0 {
			v.pend = v.pend[over:]
		}
		return voice.Unit{}, false, nil
	}
	if len(v.pend) > 0 {
		slots = append(v.pend, slots...)
		v.pend = nil
	}
	return v.unit(slots, chk), true, nil
}

func (v *Voice) vote(wc int) {
	v.votes[wc]++
	if v.votes[wc] < keyVotes || wc == v.wc {
		return
	}
	if v.wc < 0 || v.hinted {
		v.wc, v.hinted, v.noteWhy = wc, false, "（無音から）"
	}
}

func (v *Voice) unit(slots []slot, chk voice.Check) voice.Unit {
	u := voice.Unit{Check: chk, Note: fmt.Sprintf("%s・wc=%d%s", Variant, v.wc, v.noteWhy)}
	if v.picked != v.wc && !v.hinted {
		v.picked = v.wc
		u.Choice = &voice.Choice{Codec: Name, Opts: voice.Options{"wc": strconv.Itoa(v.wc)}}
	}
	out := make([]slot, len(slots))
	var raw []uint8
	for i, s := range slots {
		if !s.lost {
			s.f = scpca1f40.Descramble(s.f, s.sec, v.wc)
			raw = append(raw, s.f[:]...)
		}
		out[i] = s
	}
	u.Data = out
	u.Raw = packBits(raw)
	return u
}

func (v *Voice) Erase() (voice.Unit, bool) { return voice.Unit{}, false }

func (v *Voice) Decode(u voice.Unit) ([]int16, error) {
	slots, ok := u.Data.([]slot)
	if !ok {
		return nil, fmt.Errorf("AMBE+ 2000 の単位ではない: %T", u.Data)
	}
	if v.stream == nil {
		s, err := ambe2c.NewStream(ambe.Rate2000)
		if err != nil {
			return nil, err
		}
		v.stream = s
	}
	var out []int16
	for _, s := range slots {
		var pcm []int16
		var err error
		if s.lost {
			pcm, err = v.stream.Lost()
		} else {
			pcm, err = v.stream.Push(s.f[:])
		}
		if err != nil {
			return nil, err
		}
		out = append(out, pcm...)
	}
	return out, nil
}

func (v *Voice) Close() error {
	if v.stream == nil {
		return nil
	}
	err := v.stream.Close()
	v.stream = nil
	return err
}

func Probe(frames []voice.Frame) (ok, n int) {
	for _, f := range frames {
		secs := scpca1f40.VoiceSections(f.Sub & 3)
		if secs == nil || len(f.Bits) != l1.A1TCH1Bits {
			continue
		}
		split, _ := scpca1f40.Split(softOf(f))
		for _, sec := range secs {
			n++
			if split[sec].Voice() {
				ok++
			}
		}
	}
	return ok, n
}

func packBits(b []uint8) []byte {
	out := make([]byte, (len(b)+7)/8)
	for i, x := range b {
		out[i/8] |= (x & 1) << (7 - i%8)
	}
	return out
}
