package main

import (
	"context"
	"errors"
	"io"
	"sync"
	"time"

	"github.com/symysak/arib/internal/iqsrc"
)

type tee struct {
	open  func() (iqsrc.Source, error)
	lossy bool
	desc  string
	rate0 float64
	n     int64

	mu       sync.Mutex
	cond     *sync.Cond
	branches []*branch
	done     bool
	err      error
}

const queueMax = 32

type teeItem struct {
	iq   []complex64
	tune *iqsrc.Tuning
}

func newTee(open func() (iqsrc.Source, error), n int) (*tee, iqsrc.Source, error) {
	src, err := open()
	if err != nil {
		return nil, nil, err
	}
	t := &tee{open: open, lossy: src.Lossy(), desc: src.Describe(), rate0: src.SampleRate(), n: src.Len()}
	t.cond = sync.NewCond(&t.mu)
	var first *iqsrc.Tuning
	if tu, ok := src.(iqsrc.Tuner); ok {
		select {
		case v, open := <-tu.Tune():
			if open {
				first = &v
			}
		default:
		}
	}
	for i := 0; i < n; i++ {
		b := &branch{t: t, rate: t.rate0, feed: make(chan iqsrc.Tuning, 1)}
		if first != nil {
			b.rate, b.center = first.Rate, first.CenterHz
			b.feed <- *first
		}
		t.branches = append(t.branches, b)
	}
	return t, src, nil
}

func (t *tee) start(ctx context.Context, src iqsrc.Source) {
	go t.run(ctx, src)
}

func (t *tee) run(ctx context.Context, src iqsrc.Source) {
	defer func() {
		if src != nil {
			src.Close()
		}
	}()
	for ctx.Err() == nil {
		if tu, ok := src.(iqsrc.Tuner); ok {
			select {
			case v, open := <-tu.Tune():
				if open {
					t.push(teeItem{tune: &v})
				}
			default:
			}
		}
		chunk := int(src.SampleRate() * 0.1)
		if chunk <= 0 {
			chunk = 1024
		}
		iq, err := src.Read(chunk)
		if len(iq) > 0 {
			t.push(teeItem{iq: iq})
		}
		if err == nil {
			continue
		}
		if !t.lossy {
			t.finish(err)
			return
		}
		src.Close()
		src = nil
		for n := 1; ctx.Err() == nil; n++ {
			select {
			case <-ctx.Done():
			case <-time.After(time.Duration(min(5*n, 60)) * time.Second):
			}
			if ctx.Err() != nil {
				break
			}
			if s2, err := t.open(); err == nil {
				src = s2
				break
			}
		}
		if src == nil {
			break
		}
	}
	t.finish(io.EOF)
}

func (t *tee) push(it teeItem) {
	t.mu.Lock()
	defer t.mu.Unlock()
	for _, b := range t.branches {
		if b.detached {
			continue
		}
		if it.tune == nil && len(b.q) >= queueMax {
			if t.lossy {
				b.dropped++
				continue
			}
			for len(b.q) >= queueMax && !b.detached && !t.done {
				t.cond.Wait()
			}
			if b.detached {
				continue
			}
		}
		b.q = append(b.q, it)
	}
	t.cond.Broadcast()
}

func (t *tee) finish(err error) {
	t.mu.Lock()
	t.done, t.err = true, err
	t.cond.Broadcast()
	t.mu.Unlock()
}

func (t *tee) detach(i int) {
	t.mu.Lock()
	t.branches[i].detached = true
	t.branches[i].q = nil
	t.cond.Broadcast()
	t.mu.Unlock()
}

type branch struct {
	t        *tee
	q        []teeItem
	detached bool
	dropped  int64
	rate     float64
	center   float64
	feed     chan iqsrc.Tuning
}

func (b *branch) opener() func(iqsrc.Options) (iqsrc.Source, error) {
	return func(iqsrc.Options) (iqsrc.Source, error) {
		b.t.mu.Lock()
		defer b.t.mu.Unlock()
		if b.detached {
			return nil, errors.New("入力の分配が終わっている")
		}
		return b, nil
	}
}

func (b *branch) Read(int) ([]complex64, error) {
	t := b.t
	t.mu.Lock()
	defer t.mu.Unlock()
	deadline := time.Now().Add(time.Second)
	for len(b.q) == 0 {
		if t.done {
			return nil, t.err
		}
		if b.detached || time.Now().After(deadline) {
			return nil, nil
		}
		waitWithTimeout(t.cond, 100*time.Millisecond)
	}
	it := b.q[0]
	b.q = b.q[1:]
	t.cond.Broadcast()
	if it.tune != nil {
		b.rate, b.center = it.tune.Rate, it.tune.CenterHz
		select {
		case <-b.feed:
		default:
		}
		b.feed <- *it.tune
		return nil, nil
	}
	return it.iq, nil
}

func waitWithTimeout(c *sync.Cond, d time.Duration) {
	tm := time.AfterFunc(d, c.Broadcast)
	c.Wait()
	tm.Stop()
}

func (b *branch) SampleRate() float64 {
	b.t.mu.Lock()
	defer b.t.mu.Unlock()
	return b.rate
}
func (b *branch) Lossy() bool      { return b.t.lossy }
func (b *branch) Len() int64       { return b.t.n }
func (b *branch) Describe() string { return b.t.desc }
func (b *branch) CenterHz() (float64, bool) {
	b.t.mu.Lock()
	defer b.t.mu.Unlock()
	return b.center, b.center > 0
}
func (b *branch) Close() error              { return nil }
func (b *branch) Tune() <-chan iqsrc.Tuning { return b.feed }
