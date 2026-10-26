package iqsrc

import "sync"


type Tuning struct {
	CenterHz, VfoHz, Rate float64
}

type Tuner interface {
	Tune() <-chan Tuning
}

type Controller interface {
	SetFreq(hz float64) error
	SetGain(db float64, agc bool) error
}

type tuneFeed struct {
	mu     sync.Mutex
	ch     chan Tuning
	cur    Tuning
	have   bool
	closed bool
}

func newTuneFeed() *tuneFeed { return &tuneFeed{ch: make(chan Tuning, 1)} }

func (f *tuneFeed) push(t Tuning) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closed {
		return
	}
	f.cur, f.have = t, true
	select {
	case <-f.ch:
	default:
	}
	f.ch <- t
}

func (f *tuneFeed) current() (Tuning, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.cur, f.have
}

func (f *tuneFeed) close() {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.closed {
		f.closed = true
		close(f.ch)
	}
}
