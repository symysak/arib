package rx

import (
	"sort"
	"sync"
	"time"
)

type Clock struct {
	mu     sync.Mutex
	origin time.Time
	points []clockPoint
	gap    float64
	live   bool
}

type clockPoint struct {
	sec  float64
	wall time.Time
}

const (
	clockGapSec = 1.0
	clockEarlySec = 0.05
	clockMaxPoints = 4096
)

func NewClock(origin time.Time, live bool, jitterSec float64) *Clock {
	return &Clock{origin: origin, points: []clockPoint{{0, origin}}, gap: clockGapSec + max(jitterSec, 0), live: live}
}

func (c *Clock) Observe(from, to float64, at time.Time) {
	if c == nil || !c.live || to <= from {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	late := at.Sub(c.atLocked(to)).Seconds()
	switch {
	case late >= c.gap:
		c.add(from, at.Add(-secDur(to-from)))
	case late < -clockEarlySec:
		c.add(from, at.Add(-secDur(to-from)))
	}
}

func (c *Clock) add(sec float64, wall time.Time) {
	if n := len(c.points); n > 0 && c.points[n-1].sec >= sec {
		c.points[n-1].wall = wall.Add(-secDur(sec - c.points[n-1].sec))
		return
	}
	c.points = append(c.points, clockPoint{sec, wall})
	if len(c.points) > clockMaxPoints {
		c.points = append(c.points[:0], c.points[len(c.points)-clockMaxPoints:]...)
	}
}

func (c *Clock) At(sec float64) time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.atLocked(sec)
}

func (c *Clock) atLocked(sec float64) time.Time {
	i := sort.Search(len(c.points), func(i int) bool { return c.points[i].sec > sec }) - 1
	if i < 0 {
		i = 0
	}
	p := c.points[i]
	return p.wall.Add(secDur(sec - p.sec))
}

func (c *Clock) Sec(sec float64) float64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.atLocked(sec).Sub(c.origin).Seconds()
}

func secDur(s float64) time.Duration { return time.Duration(s * float64(time.Second)) }
