package ambe

import (
	"fmt"
	"math"
	"math/bits"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/symysak/arib/internal/std-t102/l1"
	"github.com/symysak/arib/internal/std-t102/l3"
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

func descramble(a [ambe.Bits3600]uint8, mask []uint8) [ambe.Bits3600]uint8 {
	d := ambe.Params49(a)
	for i := 0; i < 49 && i < len(mask); i++ {
		d[i] ^= mask[i] & 1
	}
	return ambe.FromParams49(d)
}

const (
	keyMinUnits = 10
	keyMaxUnits = 250
	keyZ = 6
	keyGain = 0.9
)

type pair struct {
	x      uint64
	pa, pb int
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
	pairs   []pair
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
		d[k] = pack49(ambe.Params49(u.f[k]))
	}
	pos := u.sub * 2
	if v.prevOK && no == v.prevNo+1 {
		v.pairs = append(v.pairs, pair{x: v.prevD ^ d[0], pa: v.prevPos, pb: pos})
	}
	v.pairs = append(v.pairs, pair{x: d[0] ^ d[1], pa: pos, pb: pos + 1})
	v.prevOK, v.prevNo, v.prevD, v.prevPos = true, no, d[1], pos+1
	v.units++
	if v.next == 0 {
		v.next = keyMinUnits
	}
	if v.units < v.next {
		return false
	}
	v.next = max(v.units+1, min(v.units+max(5, v.units/4), keyMaxUnits))
	key, sf, ok := findKey(v.pairs)
	if !ok && v.units < keyMaxUnits {
		return false
	}
	v.decided = true
	v.pairs = nil
	if ok {
		v.key.Store(uint32(key) | sfBit(sf && key != 0))
	}
	return ok
}

func findKey(ps []pair) (uint16, bool, bool) {
	if len(ps) == 0 {
		return 0, false, false
	}
	base := 0
	sfPos := false
	for _, p := range ps {
		base += bits.OnesCount64(p.x)
		sfPos = sfPos || p.pa >= 4 || p.pb >= 4
	}
	type cand struct {
		key            uint16
		best           int
		mean, sd       float64
		pick, plainWin bool
	}
	scan := func(mask func(key uint16, pos int) uint64) cand {
		best, bestKey := math.MaxInt, uint16(0)
		var sum, sq float64
		for key := uint16(1); key <= 0x7FFF; key++ {
			c := 0
			for _, p := range ps {
				c += bits.OnesCount64(p.x ^ mask(key, p.pa) ^ mask(key, p.pb))
			}
			sum += float64(c)
			sq += float64(c) * float64(c)
			if c < best {
				best, bestKey = c, key
			}
		}
		mean := sum / 0x7FFF
		sd := math.Sqrt(max(sq/0x7FFF-mean*mean, 1e-9))
		return cand{key: bestKey, best: best, mean: mean, sd: sd,
			pick:     float64(best) <= mean-keyZ*sd && float64(best) <= keyGain*float64(base),
			plainWin: float64(base) <= mean-keyZ*sd && base <= best}
	}
	frame := maskTable()
	nx := scan(func(key uint16, pos int) uint64 { return frame[key][pos&3] })
	sf := cand{best: math.MaxInt, plainWin: true}
	if sfPos {
		tab := maskTableSF()
		sf = scan(func(key uint16, pos int) uint64 { return tab[key][pos] })
	}
	switch {
	case nx.pick && (!sf.pick || nx.best <= sf.best):
		return nx.key, false, true
	case sf.pick:
		return sf.key, true, true
	case nx.plainWin && sf.plainWin:
		return 0, false, true
	}
	return 0, false, false
}

var maskTable = sync.OnceValue(func() *[0x8000][4]uint64 {
	t := new([0x8000][4]uint64)
	for key := 1; key < 0x8000; key++ {
		ks := l3.Keystream(uint16(key), 4*49)
		for p := 0; p < 4; p++ {
			for i := 0; i < 49; i++ {
				t[key][p] |= uint64(ks[49*p+i]&1) << i
			}
		}
	}
	return t
})

var maskTableSF = sync.OnceValue(func() *[0x8000][16]uint64 {
	t := new([0x8000][16]uint64)
	for key := 1; key < 0x8000; key++ {
		ks := l3.Keystream(uint16(key), 16*49)
		for p := 0; p < 16; p++ {
			for i := 0; i < 49; i++ {
				t[key][p] |= uint64(ks[49*p+i]&1) << i
			}
		}
	}
	return t
})

func pack49(d [49]uint8) (x uint64) {
	for i := 0; i < 49; i++ {
		x |= uint64(d[i]&1) << i
	}
	return
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
		ks := l3.Keystream(uint16(key), 16*49)
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
			f := descramble(d.f[k], v.masks[pos])
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
