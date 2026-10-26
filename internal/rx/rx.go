package rx

import (
	"io/fs"
	"net/http"
	"time"
)

type Standard interface {
	Name() string
	Grid() float64
	NewChannel(c ChannelConfig, sink Sink) Channel
	NewDetector(c DetectorConfig) Detector
	Routes(mux *http.ServeMux, lookup func(id int) Channel)
	Pane() fs.FS
}

type SystemNamer interface {
	SystemFor(mhz float64) string
}

type ChannelConfig struct {
	ID int
	MHz, OffHz, FsIn float64
	Opt any
	StartSec float64
}

type DetectorConfig struct {
	FsIn, CenterHz float64
	SNRdB float64
}

type Detector interface {
	Feed(wide []complex64)
	Candidates() []float64
	Edge() float64
}

type Channel interface {
	Feed(wide []complex64)
	Resync()
	Lock() (ok bool, score float64, label string)
	Counters() Counters
	Summary() string
	State() State
	Snapshot(ext map[string]any)
	Retune()
	Close(dropped bool)
}

type Nominaler interface {
	NominalMHz() (mhz float64, ok bool)
}

type Ledgered interface {
	LedgerMHz() (mhz float64, ok bool)
}

type Counters struct {
	Frames, Good int
	ProcSec float64
}

type State int

const (
	StateIdle    State = iota
	StateActive
	StateControl
)

type Sink interface {
	Log(format string, a ...any)
	Emit(ev any)
	StreamTime() time.Time
	IQ(y []complex64, rate float64)
	RecOpen(id int, label string, lagSec float64) string
	RecNote(id int, line string)
	RecUpdate(id int, label string)
	RecClose(id int, tailSec float64)
	Audio(id int, pcm []int16, rate int)
	MarkActive(hold time.Duration)
	BandActive() (on bool, byMHz float64)
}
