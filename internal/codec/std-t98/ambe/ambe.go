package ambe

import (
	"fmt"
	"strconv"
	"strings"
	"sync/atomic"

	"github.com/symysak/arib/internal/nxdn/scramble"
	"github.com/symysak/arib/internal/std-t98/fsk4/l1"
	"github.com/symysak/arib/internal/vocoder/ambe"
	"github.com/symysak/arib/internal/vocoder/ambe2-c"
	"github.com/symysak/arib/internal/voice"
)

const Name = "ambe"

func init() {
	voice.Register(voice.Codec{
		Name:      Name,
		Desc:      "AMBE+2 3600x2450（第 5 章の推奨。TCH 144 = 72 ビット × 2。検査は C0・C1。秘話 scramble=auto|off|<鍵>）",
		Vocoder:   "AMBE+2 3600x2450（EHR）",
		Ch:        l1.VoiceChannel,
		Check:     "C0+C1",
		Available: ambe2c.Available,
		New:       New,
		Probe:     Probe,
	})
}

const (
	cipherUnknown = iota
	cipherOff
	cipherOn
)

func New(o voice.Options) (voice.Voice, error) {
	v := &Voice{}
	hint := -1
	for k, s := range o {
		switch k {
		case "hint":
			if h, ok := strings.CutPrefix(s, Name+":"); ok {
				if key, err := strconv.ParseUint(h, 0, 16); err == nil && key <= 0x7FFF {
					hint = int(key)
				}
			}
		case "cipher":
			switch s {
			case "on":
				v.cipher = cipherOn
			case "off":
				v.cipher = cipherOff
			default:
				return nil, fmt.Errorf("%s の cipher=%q は on か off", Name, s)
			}
		case "scramble":
			switch s {
			case "", "auto":
			case "off", "0":
				v.fixed = true
			default:
				key, err := strconv.ParseUint(s, 0, 16)
				if err != nil || key == 0 || key > 0x7FFF {
					return nil, fmt.Errorf("%s の scramble=%q は auto・off・1〜32767 の鍵のどれか", Name, s)
				}
				v.fixed = true
				v.key.Store(uint32(key))
			}
		default:
			return nil, fmt.Errorf("%s に設定 %q はありません", Name, k)
		}
	}
	switch {
	case v.fixed:
	case v.cipher == cipherOff:
		v.fixed = true
	case hint > 0, hint == 0 && v.cipher == cipherUnknown:
		v.hinted = true
		v.key.Store(uint32(hint))
	}
	v.hold.Store(v.cipher == cipherOn && !v.fixed && v.key.Load() == 0)
	return v, nil
}

type unit struct {
	f    [2][ambe.Bits3600]uint8
	ok   [2]bool
	sub  int
	lost bool
}

const nearMax = 2

func near(a [ambe.Bits3600]uint8) bool {
	c0, c1 := ambe.Errors3600(a)
	return c0+c1 <= nearMax
}

const holdMax = scramble.KeyMaxUnits

type Voice struct {
	stream *ambe2c.Stream
	key atomic.Uint32
	hold   atomic.Bool
	gaveUp atomic.Bool
	cipher int
	fixed   bool
	hinted  bool
	started bool
	decided bool
	units   int
	next    int
	pairs   []scramble.Pair
	prevOK  bool
	prevNo  int64
	prevD   uint64
	prevPos int
	maskKey uint32
	masks   [4][]uint8
	held    []unit
}

func (v *Voice) Name() string { return "AMBE+2 3600x2450" }

func (v *Voice) Transport(f voice.Frame) (voice.Unit, bool, error) {
	if len(f.Bits) != l1.TCHBits {
		return voice.Unit{}, false, fmt.Errorf("TCH が %d ビットではなく %d", l1.TCHBits, len(f.Bits))
	}
	u := unit{sub: f.Sub & 1}
	ok := true
	for k := 0; k < 2; k++ {
		copy(u.f[k][:], f.Bits[72*k:72*k+72])
		u.ok[k] = near(u.f[k])
		if !u.ok[k] {
			ok = false
		}
	}
	out := voice.Unit{Data: u, Check: voice.Check{Valid: true, OK: ok}, Raw: pack(f.Bits)}
	if !v.started {
		v.started = true
		switch key := v.key.Load(); {
		case v.hinted && key != 0:
			out.Note = fmt.Sprintf("秘話（秘話鍵 %d。このユーザコードの前の通話で当てた鍵）を外して鳴らす", key)
		case v.hold.Load():
			out.Note = "秘話通信（秘話鍵を当てている。当たるまで鳴らさずに溜める）"
		}
	}
	before := v.key.Load()
	switch decided, found := v.observe(f.No, u, ok); {
	case decided && found:
		key := v.key.Load()
		out.Choice = &voice.Choice{Codec: Name, Opts: voice.Options{"scramble": strconv.Itoa(int(key))}}
		switch {
		case key == before:
		case key != 0:
			out.Note = fmt.Sprintf("秘話（秘話鍵 %d）を外して鳴らす", key)
		default:
			out.Note = "平文と決めた（前の通話の鍵では外さない）"
		}
	case decided && v.hold.Load():
		v.gaveUp.Store(true)
		out.Note = "秘話通信の秘話鍵が当たらなかった（この通話は鳴らさない）"
	}
	return out, true, nil
}

func (v *Voice) observe(no int64, u unit, ok bool) (decided, found bool) {
	if v.fixed || v.decided {
		return false, false
	}
	if !ok {
		v.prevOK = false
		return false, false
	}
	var d [2]uint64
	for k := 0; k < 2; k++ {
		d[k] = scramble.Pack49(ambe.Params49(u.f[k]))
	}
	pos := u.sub * 2
	if v.prevOK && no == v.prevNo+1 {
		v.pairs = append(v.pairs, scramble.Pair{X: v.prevD ^ d[0], PA: v.prevPos, PB: pos})
	}
	v.pairs = append(v.pairs, scramble.Pair{X: d[0] ^ d[1], PA: pos, PB: pos + 1})
	v.prevOK, v.prevNo, v.prevD, v.prevPos = true, no, d[1], pos+1
	v.units++
	if v.next == 0 {
		v.next = scramble.KeyMinUnits
	}
	if v.units < v.next {
		return false, false
	}
	v.next = max(v.units+1, min(v.units+max(5, v.units/4), scramble.KeyMaxUnits))
	key, _, hit := scramble.FindKey(v.pairs)
	if hit && key == 0 && v.cipher == cipherOn {
		hit = false
	}
	if !hit && v.units < scramble.KeyMaxUnits {
		return false, false
	}
	v.decided = true
	v.pairs = nil
	if hit {
		v.key.Store(uint32(key))
	}
	return true, hit
}

func (v *Voice) Erase() (voice.Unit, bool) {
	v.prevOK = false
	return voice.Unit{Data: unit{lost: true}}, true
}

func (v *Voice) Decode(u voice.Unit) ([]int16, error) {
	d, ok := u.Data.(unit)
	if !ok {
		return nil, fmt.Errorf("AMBE+2 3600x2450 の単位ではない: %T", u.Data)
	}
	if v.hold.Load() && v.key.Load() == 0 {
		if v.gaveUp.Load() {
			v.held = nil
			return nil, nil
		}
		if v.held = append(v.held, d); len(v.held) > holdMax {
			v.held = v.held[1:]
		}
		return nil, nil
	}
	var out []int16
	for _, h := range v.held {
		pcm, err := v.decode(h)
		if err != nil {
			return nil, err
		}
		out = append(out, pcm...)
	}
	v.held = nil
	pcm, err := v.decode(d)
	return append(out, pcm...), err
}

func (v *Voice) Flush() ([]int16, error) {
	if v.key.Load() == 0 {
		v.held = nil
		return nil, nil
	}
	var out []int16
	for _, h := range v.held {
		pcm, err := v.decode(h)
		if err != nil {
			return nil, err
		}
		out = append(out, pcm...)
	}
	v.held = nil
	return out, nil
}

func (v *Voice) decode(d unit) ([]int16, error) {
	if v.stream == nil {
		s, err := ambe2c.NewStream(ambe.Rate3600)
		if err != nil {
			return nil, err
		}
		v.stream = s
	}
	key := v.key.Load()
	if key != 0 && key != v.maskKey {
		ks := scramble.Keystream(uint16(key), 4*49)
		for p := 0; p < 4; p++ {
			v.masks[p] = ks[49*p : 49*p+49]
		}
		v.maskKey = key
	}
	var out []int16
	for k := 0; k < 2; k++ {
		var pcm []int16
		var err error
		switch {
		case d.lost, key != 0 && !d.ok[k]:
			pcm, err = v.stream.Lost()
		case key != 0:
			f := scramble.Descramble(d.f[k], v.masks[d.sub*2+k])
			pcm, err = v.stream.Push(f[:])
		default:
			pcm, err = v.stream.Push(d.f[k][:])
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
		if len(f.Bits) != l1.TCHBits {
			continue
		}
		for k := 0; k < 2; k++ {
			var a [ambe.Bits3600]uint8
			copy(a[:], f.Bits[72*k:72*k+72])
			n++
			if near(a) {
				ok++
			}
		}
	}
	return ok, n
}

func pack(b []uint8) []byte {
	out := make([]byte, (len(b)+7)/8)
	for i, x := range b {
		out[i/8] |= (x & 1) << (7 - i%8)
	}
	return out
}
