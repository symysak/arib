package l3

import (
	"fmt"
	"sort"
	"sync"

	"github.com/symysak/arib/internal/nxdn/scramble"
)


type MakerCtx struct {
	Maker, UC, Call int
}

type MakerInfo struct {
	What string
	Kind     string
	Src, Dst int
	Name string
	Status     int
	StatusText string
	Rows    []Row
	Summary string
	Event bool
}

func (m MakerInfo) CallText() string {
	if m.Src < 0 && m.Dst < 0 {
		return ""
	}
	id := func(n int) string {
		if n < 0 {
			return "?"
		}
		return fmt.Sprintf("%d", n)
	}
	k := m.Kind
	if k == "" {
		k = "呼"
	}
	if m.Kind == "全体" || m.Dst < 0 {
		return k + " " + id(m.Src)
	}
	return fmt.Sprintf("%s %s → %s", k, id(m.Src), id(m.Dst))
}

type MakerReader interface {
	SACCH(m Message) (MakerInfo, bool)
	NV2(b [10]byte, ctx MakerCtx) (MakerInfo, bool)
	Plain(b [10]byte, ctx MakerCtx) bool
	TxStart()
}

type MakerFormat struct {
	Maker  int
	Name   string
	Format string
	New func() MakerReader
}

var (
	makerMu  sync.RWMutex
	makerReg = map[int]MakerFormat{}
)

func RegisterMaker(f MakerFormat) {
	if f.Maker <= 0 || f.Maker > 127 || f.New == nil {
		panic("l3: 製造者番号か New の無い登録")
	}
	makerMu.Lock()
	defer makerMu.Unlock()
	if _, dup := makerReg[f.Maker]; dup {
		panic(fmt.Sprintf("l3: 製造者 %d の読みが重なった", f.Maker))
	}
	makerReg[f.Maker] = f
}

func LookupMaker(n int) (MakerFormat, bool) {
	makerMu.RLock()
	defer makerMu.RUnlock()
	f, ok := makerReg[n]
	return f, ok
}

func Makers() []MakerFormat {
	makerMu.RLock()
	defer makerMu.RUnlock()
	out := make([]MakerFormat, 0, len(makerReg))
	for _, f := range makerReg {
		out = append(out, f)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Maker < out[j].Maker })
	return out
}

func MakerName(n int) string {
	switch {
	case n < 0:
		return ""
	case n == CommonMaker:
		return "0（共通）"
	}
	if f, ok := LookupMaker(n); ok && f.Name != "" {
		return fmt.Sprintf("%d（%s）", n, f.Name)
	}
	return fmt.Sprintf("%d", n)
}

func (m Message) MakerDefBits() []uint8 {
	out := make([]uint8, 72)
	put := func(v, at, n int) {
		for i := 0; i < n; i++ {
			out[at+i] = uint8(v >> (n - 1 - i) & 1)
		}
	}
	put(m.Call, 0, 2)
	put(m.UC, 2, 9)
	put(m.Maker, 11, 7)
	copy(out[18:], m.MakerDef)
	return out
}

func DescrambleNV2(b [10]byte, key, half int) [10]byte {
	ks := scramble.Keystream(uint16(key), 80*(half+1))[80*half:]
	for i, x := range ks {
		b[i/8] ^= x << (7 - i%8)
	}
	return b
}

func FindNV2Key(b [10]byte, half int, ctx MakerCtx) (key int, f MakerFormat, ok bool) {
	var fs []MakerFormat
	if g, ok := LookupMaker(ctx.Maker); ok {
		fs = append(fs, g)
	}
	for _, g := range Makers() {
		if g.Maker != ctx.Maker {
			fs = append(fs, g)
		}
	}
	for _, g := range fs {
		r := g.New()
		hit, hits := 0, 0
		for k := 1; k <= 0x7FFF; k++ {
			if r.Plain(DescrambleNV2(b, k, half), ctx) {
				hit, hits = k, hits+1
				if hits > 1 {
					break
				}
			}
		}
		if hits == 1 {
			return hit, g, true
		}
	}
	return 0, MakerFormat{}, false
}

func Bits(b []byte, at, n int) int {
	v := 0
	for i := at; i < at+n; i++ {
		v = v<<1 | int(b[i/8]>>(7-i%8)&1)
	}
	return v
}

func BitsOf(b []uint8, at, n int) int { return int(num(b[at : at+n])) }
