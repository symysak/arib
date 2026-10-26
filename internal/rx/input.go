package rx

import (
	"sync/atomic"
	"time"

	"github.com/symysak/arib/internal/iqsrc"
)

const (
	StallWindow = 60 * time.Second
	StallRatio  = 0.5
)

type StallMeter struct {
	rate    float64
	from    time.Time
	samples int
}

func NewStallMeter(rate float64) *StallMeter { return &StallMeter{rate: rate} }

func (m *StallMeter) Add(n int, now time.Time) (bool, float64) {
	if m.from.IsZero() {
		m.from = now
	}
	m.samples += n
	el := now.Sub(m.from)
	if el < StallWindow {
		return false, 0
	}
	ratio := float64(m.samples) / m.rate / el.Seconds()
	m.from, m.samples = now, 0
	return ratio < StallRatio, ratio
}

func ReopenSource(src *iqsrc.Source, open func() (iqsrc.Source, error), lg *Logger, why string, stopping *atomic.Bool) bool {
	(*src).Close()
	for n := 1; ; n++ {
		wait := time.Duration(min(5*n, 60)) * time.Second
		lg.Printf("入力を開き直す（%s）: %.0f 秒後に %d 回目", why, wait.Seconds(), n)
		for end := time.Now().Add(wait); time.Now().Before(end); time.Sleep(200 * time.Millisecond) {
			if stopping.Load() {
				return false
			}
		}
		s2, err := open()
		if err == nil {
			*src = s2
			lg.Printf("入力を開き直した（%s）", s2.Describe())
			return true
		}
		lg.Printf("開き直しに失敗: %v", err)
	}
}

type HeadRing struct {
	chunks [][]complex64
	head   int
	full   bool
}

func NewHeadRing(n int) *HeadRing {
	if n <= 0 {
		return nil
	}
	return &HeadRing{chunks: make([][]complex64, n)}
}

func (r *HeadRing) Push(iq []complex64) {
	r.chunks[r.head] = iq
	r.head++
	if r.head == len(r.chunks) {
		r.head, r.full = 0, true
	}
}

func (r *HeadRing) Replay(ch chan<- []complex64) int {
	n, _ := r.replay(ch)
	return n
}

func (r *HeadRing) replay(ch chan<- []complex64) (int, int64) {
	n := len(r.chunks)
	if !r.full {
		n = r.head
	}
	if avail := cap(ch) - len(ch) - 1; avail < n {
		n = avail
	}
	if n <= 0 {
		return 0, 0
	}
	start := r.head - n
	if start < 0 {
		start += len(r.chunks)
	}
	sent := 0
	var samples int64
	for i := 0; i < n; i++ {
		c := r.chunks[(start+i)%len(r.chunks)]
		select {
		case ch <- c:
			sent++
			samples += int64(len(c))
		default:
			return sent, samples
		}
	}
	return sent, samples
}
