package voice

import (
	"math"
	"sync"
)

type Stats struct {
	Frames    int
	CheckOK   int
	CheckNG   int
	Gaps      int
	Filled    int
	Dropped   int
	Corrected int
	Restarts  int
}

type Call struct {
	TimeSec float64
	Frames  int
	OK      int
	First   []byte
}

const EntryRun = 2

const FrameSec = 0.04

const DecodeQueue = 50

type Tracker struct {
	MaxGap int
	MaxGapFill int
	NewCodec func() (Voice, error)
	Sink func(pcm []int16)
	Filter func(pcm []int16) []int16
	Lossless bool
	OnOnset func()
	OnStart func(f Frame)
	OnEnd func(c Call)
	OnUnit func(u Unit, prevNo, curNo int64)
	Err error

	mu    sync.Mutex
	stats Stats

	inCall     bool
	gap        int
	voiceRun   int
	callFrames int
	callOK     int
	first      []byte

	havePrev bool
	prev     Frame
	cur      Voice

	dec     chan decCmd
	decDone chan struct{}
}

func New() *Tracker { return &Tracker{MaxGap: 25} }

type decCmd struct {
	v     Voice
	u     Unit
	reset bool
}

func (t *Tracker) InCall() bool { return t.inCall }

func (t *Tracker) Snapshot() Stats { t.mu.Lock(); defer t.mu.Unlock(); return t.stats }

func (t *Tracker) Error() error { t.mu.Lock(); defer t.mu.Unlock(); return t.Err }

func (t *Tracker) bump(f func(*Stats)) { t.mu.Lock(); f(&t.stats); t.mu.Unlock() }

func (t *Tracker) setErr(e error) {
	t.mu.Lock()
	if t.Err == nil {
		t.Err = e
	}
	t.mu.Unlock()
}

func (t *Tracker) Push(f Frame, isVoice bool) {
	if !isVoice {
		t.voiceRun = 0
		if t.inCall {
			t.gap++
			if t.gap >= t.MaxGap {
				t.end(f.TimeSec)
			}
		}
		return
	}
	t.gap = 0
	if !t.inCall {
		t.voiceRun++
		if t.voiceRun < EntryRun {
			t.keepPrev(f)
			return
		}
		t.inCall = true
		t.callFrames, t.callOK, t.first = 0, 0, nil
		if t.OnStart != nil {
			t.OnStart(f)
		}
		t.ensureCodec()
		if t.havePrev {
			t.transport(t.prev, t.prev.No)
		}
	}
	if t.havePrev && t.prev.No != 0 {
		if miss := t.missing(f); miss > 1 {
			t.bump(func(s *Stats) { s.Gaps++ })
			limit := t.MaxGapFill
			if limit == 0 {
				limit = 50
			}
			fill := miss
			if fill > limit {
				d := fill - limit
				t.bump(func(s *Stats) { s.Dropped += d })
				fill = limit
			}
			t.bump(func(s *Stats) { s.Filled += fill })
			for i := 0; i < fill; i++ {
				if t.cur == nil {
					break
				}
				if u, ok := t.cur.Erase(); ok {
					t.enqueue(u)
				}
			}
			t.havePrev = false
		}
	}
	t.transport(f, t.prev.No)
	t.keepPrev(f)
}

func (t *Tracker) keepPrev(f Frame) {
	t.prev.Ch, t.prev.No, t.prev.TimeSec = f.Ch, f.No, f.TimeSec
	t.prev.Bits = append(t.prev.Bits[:0], f.Bits...)
	if len(f.Soft) == len(f.Bits) && f.Soft != nil {
		t.prev.Soft = append(t.prev.Soft[:0], f.Soft...)
	} else {
		t.prev.Soft = nil
	}
	t.havePrev = true
}

func (t *Tracker) missing(f Frame) int {
	miss := int(f.No - t.prev.No)
	if t.prev.TimeSec > 0 && f.TimeSec > t.prev.TimeSec {
		if n := int(math.Round((f.TimeSec - t.prev.TimeSec) / FrameSec)); n > miss {
			miss = n
		}
	}
	return miss
}

func (t *Tracker) ensureCodec() {
	if t.cur != nil || t.NewCodec == nil || t.Error() != nil {
		return
	}
	v, err := t.NewCodec()
	if err != nil {
		t.setErr(err)
		return
	}
	t.cur = v
}

func (t *Tracker) transport(f Frame, prevNo int64) {
	if t.cur == nil {
		return
	}
	u, ok, err := t.cur.Transport(f)
	if err != nil {
		t.setErr(err)
		return
	}
	if !ok {
		return
	}
	if c := u.Check; c.Valid {
		t.callFrames++
		if c.OK {
			t.callOK++
		}
		t.bump(func(s *Stats) {
			s.Frames++
			s.Corrected += c.Corrected
			if c.OK {
				s.CheckOK++
			} else {
				s.CheckNG++
			}
		})
		if t.first == nil && u.Raw != nil {
			t.first = append([]byte(nil), u.Raw...)
		}
	}
	if t.OnUnit != nil {
		t.OnUnit(u, prevNo, f.No)
	}
	t.enqueue(u)
}

func (t *Tracker) enqueue(u Unit) {
	if t.Sink == nil || t.cur == nil || t.Error() != nil {
		return
	}
	t.ensureDecode()
	if t.Lossless {
		t.dec <- decCmd{v: t.cur, u: u}
		return
	}
	select {
	case t.dec <- decCmd{v: t.cur, u: u}:
	default:
		t.bump(func(s *Stats) { s.Dropped++ })
	}
}

func (t *Tracker) ensureDecode() {
	if t.dec != nil {
		return
	}
	t.dec = make(chan decCmd, DecodeQueue)
	t.decDone = make(chan struct{})
	go t.decodeLoop(t.dec, t.decDone)
}

func (t *Tracker) decodeLoop(in <-chan decCmd, done chan<- struct{}) {
	defer close(done)
	var active Voice
	defer func() {
		if active != nil {
			active.Close()
		}
	}()
	sink := func(pcm []int16) {
		if len(pcm) == 0 {
			return
		}
		if t.Filter != nil {
			pcm = t.Filter(pcm)
		}
		t.Sink(pcm)
	}
	for cmd := range in {
		if cmd.reset {
			if fl, ok := cmd.v.(Flusher); ok {
				if pcm, err := fl.Flush(); err != nil {
					t.setErr(err)
				} else {
					sink(pcm)
				}
			}
			if cmd.v != nil {
				cmd.v.Close()
			}
			if active == cmd.v {
				active = nil
			}
			if t.OnOnset != nil {
				t.OnOnset()
			}
			continue
		}
		if cmd.v != active {
			active = cmd.v
			t.bump(func(s *Stats) { s.Restarts++ })
			if t.OnOnset != nil {
				t.OnOnset()
			}
		}
		pcm, err := cmd.v.Decode(cmd.u)
		if err != nil {
			t.setErr(err)
			return
		}
		sink(pcm)
	}
}

func (t *Tracker) resetDecode(v Voice) {
	if t.dec == nil {
		if v != nil {
			v.Close()
		}
		return
	}
	if t.Lossless {
		t.dec <- decCmd{v: v, reset: true}
		return
	}
	for i := 0; i < DecodeQueue; i++ {
		select {
		case t.dec <- decCmd{v: v, reset: true}:
			return
		default:
		}
		select {
		case <-t.dec:
			t.bump(func(s *Stats) { s.Dropped++ })
		default:
		}
	}
}

func (t *Tracker) end(timeSec float64) {
	t.inCall, t.voiceRun = false, 0
	if t.OnEnd != nil {
		t.OnEnd(Call{TimeSec: timeSec, Frames: t.callFrames, OK: t.callOK, First: t.first})
	}
	t.havePrev = false
	t.resetDecode(t.cur)
	t.cur = nil
}

func (t *Tracker) Finish() {
	if t.inCall {
		t.end(0)
	}
	if t.dec == nil {
		return
	}
	close(t.dec)
	<-t.decDone
	t.dec, t.decDone = nil, nil
}
