package webui

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"github.com/symysak/arib/internal/rx"
)

type Control interface {
	Status() (rx.Status, error)
	Add(mhz float64) error
	Remove(mhz float64) error
	Tune(centerHz float64) error
	SetGain(db float64, agc bool) error
}

type Input struct {
	ID, Label string
	Control   Control
}

type AddKind struct {
	Key   string `json:"key"`
	Label string `json:"label"`
}

type KindAdder interface {
	AddKinds() []AddKind
	AddKind(mhz float64, kind string) error
}

var ErrNoEngine = errors.New("受信エンジンがまだ動いていない")

func EngineControl(get func() *rx.Engine) Control { return engineControl{get} }

type engineControl struct{ get func() *rx.Engine }

func (c engineControl) eng() (*rx.Engine, error) {
	if e := c.get(); e != nil {
		return e, nil
	}
	return nil, ErrNoEngine
}

func (c engineControl) Status() (rx.Status, error) {
	e, err := c.eng()
	if err != nil {
		return rx.Status{}, err
	}
	return e.Status(), nil
}

func (c engineControl) Add(mhz float64) error {
	e, err := c.eng()
	if err != nil {
		return err
	}
	return e.Add(mhz, nil)
}

func (c engineControl) Remove(mhz float64) error {
	e, err := c.eng()
	if err != nil {
		return err
	}
	e.Remove(mhz)
	return nil
}

func (c engineControl) Tune(hz float64) error {
	e, err := c.eng()
	if err != nil {
		return err
	}
	return e.Tune(hz)
}

func (c engineControl) SetGain(db float64, agc bool) error {
	e, err := c.eng()
	if err != nil {
		return err
	}
	return e.SetGain(db, agc)
}

func WideControl(get func() *rx.Wide) Control { return wideControl{get} }

type wideControl struct{ get func() *rx.Wide }

func (c wideControl) w() (*rx.Wide, error) {
	if w := c.get(); w != nil {
		return w, nil
	}
	return nil, ErrNoEngine
}

func (c wideControl) Status() (rx.Status, error) {
	w, err := c.w()
	if err != nil {
		return rx.Status{}, err
	}
	return w.Status(), nil
}

func (c wideControl) Add(mhz float64) error {
	w, err := c.w()
	if err != nil {
		return err
	}
	return w.Add(mhz, nil)
}

func (c wideControl) Remove(mhz float64) error {
	w, err := c.w()
	if err != nil {
		return err
	}
	w.Remove(mhz)
	return nil
}

func (c wideControl) Tune(hz float64) error {
	w, err := c.w()
	if err != nil {
		return err
	}
	return w.Tune(hz)
}

func (c wideControl) SetGain(db float64, agc bool) error {
	w, err := c.w()
	if err != nil {
		return err
	}
	return w.SetGain(db, agc)
}

func WideInputs(get func() *rx.Wide, n int, labels ...string) []Input {
	var out []Input
	for i := 0; i < n; i++ {
		i := i
		label := fmt.Sprintf("入力 %d", i+1)
		if i < len(labels) && labels[i] != "" {
			label = labels[i]
		}
		out = append(out, Input{ID: fmt.Sprintf("in%d", i), Label: label, Control: EngineControl(func() *rx.Engine {
			w := get()
			if w == nil || i >= w.NumInputs() {
				return nil
			}
			return w.Input(i)
		})})
	}
	return out
}

func (s *Server) routeControl() {
	ctl := s.cfg.Control
	inputs := s.cfg.Inputs
	if len(inputs) == 0 && ctl != nil {
		inputs = []Input{{ID: "main", Label: "", Control: ctl}}
	}
	if ctl == nil && len(inputs) > 0 {
		ctl = inputs[0].Control
	}
	s.routeInputs(inputs)
	s.mux.HandleFunc("GET /api/engine", func(w http.ResponseWriter, r *http.Request) {
		if ctl == nil {
			http.Error(w, "この画面は受信エンジンを操作できない", http.StatusServiceUnavailable)
			return
		}
		st, err := ctl.Status()
		if err != nil {
			fail(w, err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(st)
	})
	s.mux.HandleFunc("POST /api/engine/channels", func(w http.ResponseWriter, r *http.Request) {
		var q struct {
			MHz float64 `json:"mhz"`
		}
		if !decode(w, r, &q) || !need(w, ctl) {
			return
		}
		if q.MHz <= 0 {
			http.Error(w, "mhz（局の周波数 [MHz]）を正の値で", http.StatusBadRequest)
			return
		}
		done(w, ctl.Add(q.MHz))
	})
	s.mux.HandleFunc("DELETE /api/engine/channels/{mhz}", func(w http.ResponseWriter, r *http.Request) {
		mhz, err := strconv.ParseFloat(r.PathValue("mhz"), 64)
		if err != nil || mhz <= 0 {
			http.Error(w, "周波数 [MHz] が不正", http.StatusBadRequest)
			return
		}
		if need(w, ctl) {
			done(w, ctl.Remove(mhz))
		}
	})
	s.mux.HandleFunc("POST /api/engine/tune", func(w http.ResponseWriter, r *http.Request) {
		var q struct {
			CenterMHz float64 `json:"center_mhz"`
		}
		if !decode(w, r, &q) || !need(w, ctl) {
			return
		}
		if q.CenterMHz <= 0 {
			http.Error(w, "center_mhz（中心周波数 [MHz]）を正の値で", http.StatusBadRequest)
			return
		}
		done(w, ctl.Tune(q.CenterMHz*1e6))
	})
	s.mux.HandleFunc("POST /api/engine/gain", func(w http.ResponseWriter, r *http.Request) {
		var q struct {
			GainDB float64 `json:"gain_db"`
			AGC    bool    `json:"agc"`
		}
		if !decode(w, r, &q) || !need(w, ctl) {
			return
		}
		done(w, ctl.SetGain(q.GainDB, q.AGC))
	})
}

type inputStatus struct {
	ID     string    `json:"id"`
	Label  string    `json:"label"`
	Status rx.Status `json:"status"`
	Err    string    `json:"error,omitempty"`
	Kinds  []AddKind `json:"kinds,omitempty"`
}

func (s *Server) routeInputs(inputs []Input) {
	find := func(w http.ResponseWriter, r *http.Request) (Control, bool) {
		id := r.PathValue("id")
		for _, in := range inputs {
			if in.ID == id && in.Control != nil {
				return in.Control, true
			}
		}
		http.Error(w, "入力 "+id+" は無い", http.StatusNotFound)
		return nil, false
	}
	s.mux.HandleFunc("GET /api/inputs", func(w http.ResponseWriter, r *http.Request) {
		out := []inputStatus{}
		for _, in := range inputs {
			is := inputStatus{ID: in.ID, Label: in.Label}
			if in.Control == nil {
				is.Err = "この入力は操作できない"
			} else if st, err := in.Control.Status(); err != nil {
				is.Err = err.Error()
			} else {
				is.Status = st
			}
			if ka, ok := in.Control.(KindAdder); ok {
				is.Kinds = ka.AddKinds()
			}
			out = append(out, is)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(out)
	})
	s.mux.HandleFunc("POST /api/inputs/{id}/channels", func(w http.ResponseWriter, r *http.Request) {
		ctl, ok := find(w, r)
		if !ok {
			return
		}
		var q struct {
			MHz  float64 `json:"mhz"`
			Kind string  `json:"kind"`
		}
		if !decode(w, r, &q) {
			return
		}
		if q.MHz <= 0 {
			http.Error(w, "mhz（局の周波数 [MHz]）を正の値で", http.StatusBadRequest)
			return
		}
		if ka, ok := ctl.(KindAdder); ok && q.Kind != "" {
			done(w, ka.AddKind(q.MHz, q.Kind))
			return
		}
		if q.Kind != "" {
			http.Error(w, "この入力は局の種類を選べない", http.StatusBadRequest)
			return
		}
		done(w, ctl.Add(q.MHz))
	})
	s.mux.HandleFunc("DELETE /api/inputs/{id}/channels/{mhz}", func(w http.ResponseWriter, r *http.Request) {
		ctl, ok := find(w, r)
		if !ok {
			return
		}
		mhz, err := strconv.ParseFloat(r.PathValue("mhz"), 64)
		if err != nil || mhz <= 0 {
			http.Error(w, "周波数 [MHz] が不正", http.StatusBadRequest)
			return
		}
		done(w, ctl.Remove(mhz))
	})
	s.mux.HandleFunc("POST /api/inputs/{id}/tune", func(w http.ResponseWriter, r *http.Request) {
		ctl, ok := find(w, r)
		if !ok {
			return
		}
		var q struct {
			CenterMHz float64 `json:"center_mhz"`
		}
		if !decode(w, r, &q) {
			return
		}
		if q.CenterMHz <= 0 {
			http.Error(w, "center_mhz（中心周波数 [MHz]）を正の値で", http.StatusBadRequest)
			return
		}
		done(w, ctl.Tune(q.CenterMHz*1e6))
	})
	s.mux.HandleFunc("POST /api/inputs/{id}/gain", func(w http.ResponseWriter, r *http.Request) {
		ctl, ok := find(w, r)
		if !ok {
			return
		}
		var q struct {
			GainDB float64 `json:"gain_db"`
			AGC    bool    `json:"agc"`
		}
		if !decode(w, r, &q) {
			return
		}
		done(w, ctl.SetGain(q.GainDB, q.AGC))
	})
}

func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(v); err != nil {
		http.Error(w, "JSON が読めない: "+err.Error(), http.StatusBadRequest)
		return false
	}
	return true
}

func need(w http.ResponseWriter, ctl Control) bool {
	if ctl == nil {
		http.Error(w, "この画面は受信エンジンを操作できない", http.StatusServiceUnavailable)
		return false
	}
	return true
}

func done(w http.ResponseWriter, err error) {
	if err != nil {
		fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func fail(w http.ResponseWriter, err error) {
	code := http.StatusInternalServerError
	switch {
	case errors.Is(err, rx.ErrNotControllable), errors.Is(err, rx.ErrNotRunning):
		code = http.StatusConflict
	case errors.Is(err, ErrNoEngine):
		code = http.StatusServiceUnavailable
	}
	http.Error(w, err.Error(), code)
}
