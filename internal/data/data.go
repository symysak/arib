package data

import (
	"fmt"
	"slices"
	"sort"
	"strconv"
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
	Tx int64
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

type Wrapped struct {
	Lat int64 `json:"lat"`
	Lon int64 `json:"lon"`
	Span int64 `json:"span"`
	Unit float64 `json:"unit"`
	OriginLat float64 `json:"origin_lat"`
	OriginLon float64 `json:"origin_lon"`
	Datum string `json:"datum"`
	Group string `json:"group,omitempty"`
}

const (
	DatumWGS84 = "wgs84"
	DatumTokyo = "tokyo"
)

type Event struct {
	Kind    Kind    `json:"kind"`
	No      int64   `json:"no"`
	TimeSec float64 `json:"t_sec"`
	Format string `json:"format"`
	From string `json:"from,omitempty"`
	Label string    `json:"label"`
	Pos   *Position `json:"pos,omitempty"`
	Wrap *Wrapped `json:"wrap,omitempty"`
	Rows []Row    `json:"rows,omitempty"`
	Raw []byte `json:"raw,omitempty"`
	Count int `json:"count,omitempty"`
	Again bool `json:"again,omitempty"`
	RefNo      int64   `json:"-"`
	RefTimeSec float64 `json:"-"`
}

func (e Event) N() int {
	if e.Count > 0 {
		return e.Count
	}
	return 1
}

func (e Event) Key() string {
	no, t := e.No, e.TimeSec
	if e.Again {
		no, t = e.RefNo, e.RefTimeSec
	}
	return e.Format + "\x00" + e.From + "\x00" + e.Label + "\x00" + strconv.FormatInt(no, 10) + "\x00" + strconv.FormatFloat(t, 'g', -1, 64)
}

func Fresh(evs []Event) []Event {
	var out []Event
	for _, e := range evs {
		if !e.Again {
			out = append(out, e)
		}
	}
	return out
}

func AgainOf(ev Event, f Frame, n int) Event {
	if !ev.Again {
		ev.RefNo, ev.RefTimeSec = ev.No, ev.TimeSec
	}
	ev.Again, ev.Count, ev.No, ev.TimeSec = true, n, f.No, f.TimeSec
	return ev
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
