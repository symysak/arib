package data

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
	Route string
	Slot int
	Idle bool
}

type Kind string

const (
	KindPosition Kind = "position"
	KindMessage Kind = "message"
)

type Row struct {
	Name string `json:"name"`
	Bits string `json:"bits,omitempty"`
	Text string `json:"text,omitempty"`
}

type Position struct {
	Lat float64 `json:"lat"`
	Lon float64 `json:"lon"`
	Heading float64 `json:"heading"`
}

type Event struct {
	Kind    Kind    `json:"kind"`
	No      int64   `json:"no"`
	TimeSec float64 `json:"t_sec"`
	Format string `json:"format"`
	From string `json:"from,omitempty"`
	Label string    `json:"label"`
	Pos   *Position `json:"pos,omitempty"`
	Rows  []Row     `json:"rows,omitempty"`
	Raw []byte `json:"raw,omitempty"`
}

type Reader interface {
	Read(f Frame) []Event
}

type Options map[string]string

type Format struct {
	Name string
	Desc string
	Ch Channel
	ManualOnly bool
	New func(o Options) (Reader, error)
	Probe func(frames []Frame) (ok, n int)
}

var (
	regMu sync.RWMutex
	reg   = map[key]Format{}
)

type key struct {
	ch   Channel
	name string
}

func Register(f Format) {
	if f.Name == "" || f.New == nil || f.Ch == "" {
		panic("data: 名前・チャネル・New の無い登録")
	}
	regMu.Lock()
	defer regMu.Unlock()
	k := key{f.Ch, f.Name}
	if _, dup := reg[k]; dup {
		panic("data: 同じ名前の登録 " + string(f.Ch) + "/" + f.Name)
	}
	reg[k] = f
}

func Lookup(ch Channel, name string) (Format, bool) {
	regMu.RLock()
	defer regMu.RUnlock()
	if f, ok := reg[key{ch, name}]; ok {
		return f, true
	}
	for k, f := range reg {
		if k.ch == ch && strings.EqualFold(k.name, name) {
			return f, true
		}
	}
	return Format{}, false
}

func Formats(chs ...Channel) []Format {
	regMu.RLock()
	defer regMu.RUnlock()
	out := make([]Format, 0, len(reg))
	for _, f := range reg {
		if len(chs) == 0 || slices.Contains(chs, f.Ch) {
			out = append(out, f)
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
	for _, f := range Formats(chs...) {
		if !slices.Contains(ns, f.Name) {
			ns = append(ns, f.Name)
		}
	}
	if len(ns) == 0 {
		return "無し"
	}
	return strings.Join(ns, " / ")
}

const (
	Auto = "auto"
	Off  = "off"
)

func Valid(ch Channel, name string) error {
	if name == "" || strings.EqualFold(name, Auto) || strings.EqualFold(name, Off) {
		return nil
	}
	if _, ok := Lookup(ch, name); !ok {
		return fmt.Errorf("データの書式 %q は無い（%s・auto・off）", name, Names(ch))
	}
	return nil
}
