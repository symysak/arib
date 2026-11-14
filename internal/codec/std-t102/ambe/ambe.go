package ambe

import (
	"fmt"
	"strconv"
	"strings"
	"sync/atomic"

	"github.com/symysak/arib/internal/nxdn/scramble"
	"github.com/symysak/arib/internal/std-t102/l1"
	"github.com/symysak/arib/internal/vocoder/ambe"
	"github.com/symysak/arib/internal/vocoder/ambe2-c"
	"github.com/symysak/arib/internal/voice"
)

const Name = "ambe"

func init() {
	voice.Register(voice.Codec{
		Name:      Name,
		Desc:      "AMBE+2 3600x2450（第 5 章の推奨。TCH 144 = 72 ビット × 2、DMR と同じ並び。検査は C0・C1。秘話 scramble=auto|off|<鍵>）",
		Ch:        l1.VoiceChannel,
		Check:     "C0+C1",
		Available: ambe2c.Available,
		New:       New,
		Probe:     Probe,
	})
}

func New(o voice.Options) (voice.Voice, error) {
	v := &Voice{}
	hint := -1
	for k, s := range o {
		switch k {
		case "hint":
			if h, ok := strings.CutPrefix(s, Name+":"); ok {
				h, sf := strings.CutSuffix(h, "/sf")
				if key, err := strconv.ParseUint(h, 0, 16); err == nil && key <= 0x7FFF {
					hint = int(key)
					v.hintSF = sf
				}
			}
		case "scramble":
			switch s {
			case "", "auto":
			case "off", "0":
				v.fixed = true
			default:
				ks, sf := strings.CutSuffix(s, "/sf")
				key, err := strconv.ParseUint(ks, 0, 16)
				if err != nil || key == 0 || key > 0x7FFF {
					return nil, fmt.Errorf("%s の scramble=%q は auto・off・1〜32767 の鍵（B54 の周期なら後ろに /sf）のどれか", Name, s)
				}
				v.fixed = true
				v.key.Store(uint32(key) | sfBit(sf))
			}
		default:
			return nil, fmt.Errorf("%s に設定 %q はありません", Name, k)
		}
	}
	if !v.fixed && hint >= 0 {
		v.hinted = true
		v.key.Store(uint32(hint) | sfBit(v.hintSF && hint != 0))
	}
	return v, nil
}

const keySF = 1 << 16

func sfBit(sf bool) uint32 {
	if sf {
		return keySF
	}
	return 0
}

func keyText(k uint32) string {
	s := strconv.Itoa(int(k & 0x7FFF))
	if k&keySF != 0 {
		s += "/sf"
	}
	return s
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

type Voice struct {
	stream *ambe2c.Stream
	key    atomic.Uint32
	hintSF bool
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
	masks   [16][]uint8
}

func (v *Voice) Name() string { return "AMBE+2 3600x2450" }

func (v *Voice) Transport(f voice.Frame) (voice.Unit, bool, error) {
	if len(f.Bits) != l1.TCHBits {
		return voice.Unit{}, false, fmt.Errorf("TCH が %d ビットではなく %d", l1.TCHBits, len(f.Bits))
	}
	u := unit{sub: f.Sub & 7}
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
		if key := v.key.Load(); v.hinted && key&0x7FFF != 0 {
			out.Note = fmt.Sprintf("秘話（スクランブラの鍵 %s。この局の前の通話で当てた鍵）を外して鳴らす", keyText(key))
		}
	}
	before := v.key.Load()
	if v.observe(f.No, u, ok) {
		key := v.key.Load()
		out.Choice = &voice.Choice{Codec: Name, Opts: voice.Options{"scramble": keyText(key)}}
		switch {
		case key == before:
		case key&0x7FFF != 0:
			out.Note = fmt.Sprintf("秘話（スクランブラの鍵 %s）を外して鳴らす", keyText(key))
		default:
			out.Note = "平文と決めた（前の通話の鍵では外さない）"
		}
	}
	return out, true, nil
}

func (v *Voice) observe(no int64, u unit, ok bool) bool {
	if v.fixed || v.decided {
		return false
	}
	if !ok {
		v.prevOK = false
		return false
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
		return false
	}
	v.next = max(v.units+1, min(v.units+max(5, v.units/4), scramble.KeyMaxUnits))
	key, sf, ok := scramble.FindKey(v.pairs)
	if !ok && v.units < scramble.KeyMaxUnits {
		return false
	}
	v.decided = true
	v.pairs = nil
	if ok {
		v.key.Store(uint32(key) | sfBit(sf && key != 0))
	}
	return ok
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
	if v.stream == nil {
		s, err := ambe2c.NewStream(ambe.Rate3600)
		if err != nil {
			return nil, err
		}
		v.stream = s
	}
	full := v.key.Load()
	key := full & 0x7FFF
	if key != 0 && full != v.maskKey {
		ks := scramble.Keystream(uint16(key), 16*49)
		for p := 0; p < 16; p++ {
			q := p
			if full&keySF == 0 {
				q = p & 3
			}
			v.masks[p] = ks[49*q : 49*q+49]
		}
		v.maskKey = full
	}
	var out []int16
	for k := 0; k < 2; k++ {
		var pcm []int16
		var err error
		switch {
		case d.lost, key != 0 && !d.ok[k]:
			pcm, err = v.stream.Lost()
		case key != 0:
			pos := d.sub*2 + k
			if full&keySF == 0 {
				pos = (d.sub&1)*2 + k
			}
			f := scramble.Descramble(d.f[k], v.masks[pos])
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
