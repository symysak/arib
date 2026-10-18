package server

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/symysak/arib/internal/cliflag"
	"github.com/symysak/arib/internal/rx"
	"github.com/symysak/arib/internal/std-t115/qpsknarrow/pane"
)

type Standard struct {
	cfg StdConfig
	hooks *stdHooks
}

type StdConfig struct {
	Scramble int
	Opts map[int64]ChannelOpt
	NoIQ bool
	Origin time.Time
	LogDir string
}

type ChannelOpt struct {
	Scramble int
}

type stdHooks struct {
	opened   func(*Channel)
	closed   func(c *Channel, dropped bool)
	selected func(id int) bool
}

func NewStandard(cfg StdConfig) *Standard { return &Standard{cfg: cfg} }

func (*Standard) Name() string { return "STD-T115 QPSK ナロー" }

const System = "std-t115-qpsknarrow"

func (*Standard) Grid() float64 { return chanSpacingHz }

func (s *Standard) NewChannel(c rx.ChannelConfig, sink rx.Sink) rx.Channel {
	opt, ok := c.Opt.(ChannelOpt)
	if !ok {
		opt, ok = s.cfg.Opts[OptKey(c.MHz)]
	}
	if !ok {
		opt = ChannelOpt{Scramble: s.cfg.Scramble}
	}
	ch := newChannel(s, c, sink, opt)
	if s.hooks != nil && s.hooks.opened != nil {
		s.hooks.opened(ch)
	}
	return ch
}

func (*Standard) NewDetector(c rx.DetectorConfig) rx.Detector {
	if c.FsIn < 3*chanSpacingHz {
		return nil
	}
	return newDetector(c.FsIn, c.SNRdB)
}

func (*Standard) Routes(mux *http.ServeMux, lookup func(id int) rx.Channel) {
	mux.HandleFunc("/ch/{id}/api/scramble", func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.Atoi(r.PathValue("id"))
		if err != nil {
			http.Error(w, "局番号が不正です", http.StatusBadRequest)
			return
		}
		ch, _ := lookup(id).(*Channel)
		if ch == nil {
			http.Error(w, "その局は監視していません", http.StatusNotFound)
			return
		}
		q := r.URL.Query().Get("value")
		if q == "" {
			http.Error(w, "value= を指定してください（0 = 自動判定へ戻す）", http.StatusBadRequest)
			return
		}
		v, err := ParseScramble(q)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		ch.PinScramble(v)
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(map[string]any{"requested": v})
	})
}

func (*Standard) Pane() fs.FS { return pane.FS() }

func OptKey(mhz float64) int64 { return int64(math.Round(mhz * 1e4)) }

func ParseScramble(s string) (int, error) {
	s = strings.TrimSpace(s)
	var v int64
	var err error
	if h, ok := strings.CutPrefix(strings.ToLower(s), "0x"); ok {
		v, err = strconv.ParseInt(h, 16, 32)
	} else {
		v, err = strconv.ParseInt(s, 10, 32)
	}
	if err != nil || v < 0 || v > 0xFFFF {
		return 0, fmt.Errorf("スクランブル値 %q は 0〜65535（16 進なら 0x 付き）", s)
	}
	return int(v), nil
}

func OptsFromChannels(list []cliflag.Channel) (map[int64]ChannelOpt, error) {
	if err := cliflag.CheckKeys(list, "scramble"); err != nil {
		return nil, err
	}
	opts := map[int64]ChannelOpt{}
	for _, c := range list {
		v, ok := c.Opts["scramble"]
		if !ok {
			continue
		}
		scr, err := ParseScramble(v)
		if err != nil {
			return nil, fmt.Errorf("-channels の %g MHz: %v", c.MHz, err)
		}
		opts[OptKey(c.MHz)] = ChannelOpt{Scramble: scr}
	}
	return opts, nil
}
