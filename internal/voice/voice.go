package voice

import (
	"fmt"
	"slices"
	"sort"
	"strings"
	"sync"
)

type Channel string

type Frame struct {
	Ch      Channel
	No      int64
	TimeSec float64
	Bits    []uint8
	Soft    []float32
}

type Check struct {
	Valid     bool
	OK        bool
	Corrected int
}

type Unit struct {
	Data  any
	Check Check
	Raw   []byte
	Note  string
	Choice *Choice
}

type Choice struct {
	Codec string
	Opts  Options
}

type Voice interface {
	Name() string
	Transport(f Frame) (u Unit, ok bool, err error)
	Erase() (u Unit, ok bool)
	Decode(u Unit) ([]int16, error)
	Close() error
}

type Flusher interface {
	Flush() ([]int16, error)
}

const SampleRate = 8000

type Options map[string]string

type Codec struct {
	Name string
	Desc string
	Ch Channel
	Check string
	Tentative bool
	ManualOnly bool
	Available func() bool
	New func(o Options) (Voice, error)
}

var (
	regMu sync.RWMutex
	reg   = map[key]Codec{}
)

type key struct {
	ch   Channel
	name string
}

func Register(c Codec) {
	if c.Name == "" || c.New == nil || c.Ch == "" {
		panic("voice: 名前・チャネル・New の無い登録")
	}
	regMu.Lock()
	defer regMu.Unlock()
	k := key{c.Ch, c.Name}
	if _, dup := reg[k]; dup {
		panic("voice: 同じ名前の登録 " + string(c.Ch) + "/" + c.Name)
	}
	reg[k] = c
}

func Lookup(ch Channel, name string) (Codec, bool) {
	regMu.RLock()
	defer regMu.RUnlock()
	if c, ok := reg[key{ch, name}]; ok {
		return c, true
	}
	for k, c := range reg {
		if k.ch == ch && strings.EqualFold(k.name, name) {
			return c, true
		}
	}
	return Codec{}, false
}

func Codecs(chs ...Channel) []Codec {
	regMu.RLock()
	defer regMu.RUnlock()
	out := make([]Codec, 0, len(reg))
	for _, c := range reg {
		if len(chs) == 0 || slices.Contains(chs, c.Ch) {
			out = append(out, c)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Name != out[j].Name {
			return out[i].Name < out[j].Name
		}
		return out[i].Ch < out[j].Ch
	})
	return out
}

func Names(chs ...Channel) string {
	var ns []string
	for _, c := range Codecs(chs...) {
		if !slices.Contains(ns, c.Name) {
			ns = append(ns, c.Name)
		}
	}
	if len(ns) == 0 {
		return "無し"
	}
	return strings.Join(ns, " / ")
}

func (o Options) ParseOption(s string) error {
	k, v, ok := strings.Cut(s, "=")
	if !ok || strings.TrimSpace(k) == "" {
		return fmt.Errorf("符号器の設定は key=value: %q", s)
	}
	o[strings.TrimSpace(k)] = strings.TrimSpace(v)
	return nil
}

func Run(v Voice, f Frame) ([]int16, error) {
	u, ok, err := v.Transport(f)
	if err != nil || !ok {
		return nil, err
	}
	return v.Decode(u)
}
