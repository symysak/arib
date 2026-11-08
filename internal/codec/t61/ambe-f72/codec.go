package ambef72

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/symysak/arib/internal/std-t61/l1"
	"github.com/symysak/arib/internal/std-t61/scpc-a1f72"
	t61voice "github.com/symysak/arib/internal/std-t61/voice"
	"github.com/symysak/arib/internal/vocoder/ambe"
	"github.com/symysak/arib/internal/vocoder/ambe2-c"
	"github.com/symysak/arib/internal/voice"
)

const Name = "ambe-f72"

const Variant = "f72"

func init() {
	voice.Register(voice.Codec{
		Name:      Name,
		Desc:      "AMBE+2 3600x2450（第 1 編 付録 1 の TCH1 に 72 ビット × 4 枚、外側の符号なし。スクランブルの初期値は C0・C1 の検査で当てる。-codec-opt wc=0..511 で固定）",
		Vocoder:   "AMBE+2 3600x2450",
		Transport: "付録 1 TCH1 f72（72 ビット × 4・外側の符号なし）",
		Ch:        t61voice.TCH1,
		Check:     "C0+C1",
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
	hintVotes = 2
	maxPending = 125
	maxFill = 50
	frameSec = 0.04
)

type slot struct {
	f    [scpca1f72.FrameBits]uint8
	lost bool
}

type pendV struct {
	tch1 []uint8
	lost int
}

type Voice struct {
	wc      int
	hint    int
	fixed   bool
	scores  [scpca1f72.Patterns]int
	nV      int
	pend    []pendV
	lastNo  int64
	lastFN  int
	lastT   float64
	stream  *ambe2c.Stream
	picked  int
	noteWhy string
}

func New(wc, hint int) *Voice {
	v := &Voice{wc: -1, hint: hint, lastNo: -1, picked: -1}
	if wc >= 0 {
		v.wc, v.fixed, v.noteWhy = wc, true, "（固定）"
	}
	return v
}

func (v *Voice) Name() string { return "AMBE+2 3600x2450（付録 1 TCH1）" }

func (v *Voice) Key() (wc int, ok bool) { return v.wc, v.wc >= 0 }

func (v *Voice) Transport(f voice.Frame) (voice.Unit, bool, error) {
	if len(f.Bits) != l1.A1TCH1Bits {
		return voice.Unit{}, false, fmt.Errorf("TCH1 が %d ビットではなく %d", l1.A1TCH1Bits, len(f.Bits))
	}
	fn := f.Sub & 3
	lost := 0
	if v.lastNo >= 0 {
		miss := int(f.No - v.lastNo)
		if f.TimeSec > v.lastT && v.lastT > 0 {
			if n := int(math.Round((f.TimeSec - v.lastT) / frameSec)); n > miss {
				miss = n
			}
		}
		for i, k := 1, v.lastFN; i < miss && i <= maxFill; i++ {
			k = (k + 1) & 3
			if scpca1f72.VoiceFN(k) {
				lost += scpca1f72.FramesPerV
			}
		}
	}
	v.lastNo, v.lastFN, v.lastT = f.No, fn, f.TimeSec
	if !scpca1f72.VoiceFN(fn) {
		return voice.Unit{}, false, nil
	}
	tch1 := append([]uint8(nil), f.Bits...)
	if v.wc < 0 {
		v.learn(tch1)
		v.pend = append(v.pend, pendV{tch1, lost})
		if over := len(v.pend) - maxPending; over > 0 {
			v.pend = v.pend[over:]
		}
		v.decide()
		if v.wc < 0 {
			return voice.Unit{}, false, nil
		}
		pend := v.pend
		v.pend = nil
		var slots []slot
		chk := voice.Check{Valid: true, OK: true}
		for _, p := range pend {
			slots = append(slots, v.slots(p.tch1, p.lost, &chk)...)
		}
		return v.unit(slots, chk), true, nil
	}
	chk := voice.Check{Valid: true, OK: true}
	return v.unit(v.slots(tch1, lost, &chk), chk), true, nil
}

func (v *Voice) learn(tch1 []uint8) {
	sc := scpca1f72.KeyScores([][]uint8{tch1})
	for i, n := range sc {
		v.scores[i] += n
	}
	v.nV++
}

func (v *Voice) decide() {
	if v.hint >= 0 && v.scores[v.hint] >= hintVotes {
		v.wc, v.noteWhy = v.hint, "（台帳か前の通話の組）"
		return
	}
	if wc, n, second := scpca1f72.Best(v.scores); scpca1f72.Decided(n, second, v.nV) {
		v.wc, v.noteWhy = wc, "（C0・C1 の検査から）"
	}
}

func (v *Voice) slots(tch1 []uint8, lost int, chk *voice.Check) []slot {
	out := make([]slot, lost, lost+scpca1f72.FramesPerV)
	for i := range out {
		out[i].lost = true
	}
	fs, _ := scpca1f72.Split(tch1, v.wc)
	for _, f := range fs {
		if !ambe.Valid3600(f) {
			chk.OK = false
		}
		out = append(out, slot{f: f})
	}
	return out
}

func (v *Voice) unit(slots []slot, chk voice.Check) voice.Unit {
	u := voice.Unit{Check: chk, Note: fmt.Sprintf("%s・wc=%d%s", Variant, v.wc, v.noteWhy)}
	if v.picked != v.wc {
		v.picked = v.wc
		u.Choice = &voice.Choice{Codec: Name, Opts: voice.Options{"wc": strconv.Itoa(v.wc)}}
	}
	var raw []uint8
	for _, s := range slots {
		if !s.lost {
			raw = append(raw, s.f[:]...)
		}
	}
	u.Data = slots
	u.Raw = packBits(raw)
	return u
}

func (v *Voice) Erase() (voice.Unit, bool) { return voice.Unit{}, false }

func (v *Voice) Decode(u voice.Unit) ([]int16, error) {
	slots, ok := u.Data.([]slot)
	if !ok {
		return nil, fmt.Errorf("AMBE+2 3600x2450 の単位ではない: %T", u.Data)
	}
	if v.stream == nil {
		s, err := ambe2c.NewStream(ambe.Rate3600)
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
	var tch1s [][]uint8
	for _, f := range frames {
		if scpca1f72.VoiceFN(f.Sub&3) && len(f.Bits) == l1.A1TCH1Bits {
			tch1s = append(tch1s, f.Bits)
			n += scpca1f72.FramesPerV
		}
	}
	if len(tch1s) == 0 {
		return 0, 0
	}
	_, ok, _ = scpca1f72.Best(scpca1f72.KeyScores(tch1s))
	return ok, n
}

func packBits(b []uint8) []byte {
	out := make([]byte, (len(b)+7)/8)
	for i, x := range b {
		out[i/8] |= (x & 1) << (7 - i%8)
	}
	return out
}
