package server

import (
	"errors"
	"io/fs"
	"math"
	"net/http"
	"strconv"
	"sync"

	"github.com/symysak/arib/internal/rx"
	"github.com/symysak/arib/internal/std-t86/decoder"
	"github.com/symysak/arib/internal/std-t86/fec"
	"github.com/symysak/arib/internal/std-t86/pane"
)

type Standard struct {
	defaults ChannelOpts
	perMHz   map[int64]ChannelOpts

	mu  sync.Mutex
	env channelEnv
	onNew   func(*Channel)
	onClose func(ch *Channel, dropped bool)
}

func NewStandard(defaults ChannelOpts, perMHz map[float64]ChannelOpts) *Standard {
	s := &Standard{defaults: defaults, perMHz: map[int64]ChannelOpts{}}
	for mhz, o := range perMHz {
		s.perMHz[mhzKey(mhz)] = o
	}
	return s
}

func mhzKey(mhz float64) int64 { return int64(math.Round(mhz * 1e4)) }

func (s *Standard) setEnv(f func(e *channelEnv)) {
	s.mu.Lock()
	f(&s.env)
	s.mu.Unlock()
}

func (*Standard) Name() string { return "T86" }

const System = "std-t86"

func (*Standard) Grid() float64 { return gridHz }

func (s *Standard) NewChannel(c rx.ChannelConfig, sink rx.Sink) rx.Channel {
	s.mu.Lock()
	opts, ok := s.perMHz[mhzKey(c.MHz)]
	if !ok {
		opts = s.defaults
	}
	if o, ok := c.Opt.(ChannelOpts); ok {
		opts = o
	}
	if opts.SyncThresh <= 0 {
		opts.SyncThresh = s.defaults.SyncThresh
	}
	env, onNew, onClose := s.env, s.onNew, s.onClose
	s.mu.Unlock()
	ch := newChannel(c, sink, opts, env)
	if onClose != nil {
		ch.onClose = func(dropped bool) { onClose(ch, dropped) }
	}
	if onNew != nil {
		onNew(ch)
	}
	return ch
}

func (*Standard) NewDetector(c rx.DetectorConfig) rx.Detector {
	return newDetector(c.FsIn, c.SNRdB)
}

func (*Standard) Pane() fs.FS { return pane.FS() }

func (*Standard) Routes(mux *http.ServeMux, lookup func(id int) rx.Channel) {
	ch := func(w http.ResponseWriter, r *http.Request) *Channel {
		id, err := strconv.Atoi(r.PathValue("id"))
		if err == nil {
			if c, ok := lookup(id).(*Channel); ok && c != nil {
				return c
			}
		}
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "その局はありません"})
		return nil
	}
	toggle := func(path, key string, set func(c *Channel, v bool) bool) {
		mux.HandleFunc("POST /ch/{id}/api/"+path, func(w http.ResponseWriter, r *http.Request) {
			c := ch(w, r)
			if c == nil {
				return
			}
			v, err := boolParam(r, "enabled")
			if err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
				return
			}
			writeJSON(w, http.StatusOK, map[string]bool{key: set(c, v)})
		})
	}
	toggle("squelch", "squelch_enabled", (*Channel).SetSquelch)
	toggle("broadcast_strict", "broadcast_strict", (*Channel).SetBroadcastStrict)
	toggle("cfo", "cfo_enabled", (*Channel).SetCFOEnabled)

	mux.HandleFunc("POST /ch/{id}/api/cfo/reset", func(w http.ResponseWriter, r *http.Request) {
		if c := ch(w, r); c != nil {
			c.RequestCFOReset()
			writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
		}
	})

	mux.HandleFunc("POST /ch/{id}/api/seed", func(w http.ResponseWriter, r *http.Request) {
		c := ch(w, r)
		if c == nil {
			return
		}
		v, err := seedParam(r.URL.Query().Get("value"))
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		c.RequestSeedPin(v)
		writeJSON(w, http.StatusOK, map[string]any{
			"seed": v, "seed_pinned": v != decoder.SeedAuto})
	})
}

func seedParam(raw string) (int, error) {
	if raw == "auto" {
		return decoder.SeedAuto, nil
	}
	if raw == "" {
		return 0, errors.New("value が必要です")
	}
	v, err := strconv.Atoi(raw)
	if err != nil {
		return 0, err
	}
	if v < decoder.SeedAuto || v >= fec.NSeeds {
		return 0, errors.New("value は -1/auto（自動判定）または 0..511 です")
	}
	return v, nil
}
