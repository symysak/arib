package server


import (
	"fmt"
	"math"
	"strings"

	"github.com/symysak/arib/internal/rx"
	"github.com/symysak/arib/internal/std-t79/l1"
	"github.com/symysak/arib/internal/webui"
)

type InputMeta struct {
	ID         string  `json:"id"`
	Label      string  `json:"label"`
	Input      string  `json:"input"`
	CenterMHz  float64 `json:"center_mhz"`
	SampleRate float64 `json:"sample_rate"`
	GainDB     float64 `json:"gain_db"`
	AGC        bool    `json:"agc,omitempty"`
	Finished   bool    `json:"finished,omitempty"`
	Up bool `json:"up,omitempty"`
}

func inputID(i int) string { return fmt.Sprintf("in%d", i) }

func (m *Monitor) setInput(id string, f func(*InputMeta)) {
	m.setMeta(func(mt *Meta) {
		for i := range mt.Inputs {
			if mt.Inputs[i].ID != id {
				continue
			}
			f(&mt.Inputs[i])
			in := mt.Inputs[i]
			if i == 0 {
				mt.Input, mt.CenterMHz, mt.SampleRate, mt.GainDB = in.Input, in.CenterMHz, in.SampleRate, in.GainDB
			}
			if in.Up {
				mt.UpInput, mt.UpCenterMHz = in.Input, in.CenterMHz
			}
		}
	})
}

type inputControl struct {
	webui.Control
	m  *Monitor
	id string
}

func (c inputControl) Remove(mhz float64) error {
	c.m.mu.Lock()
	for _, ch := range c.m.chans {
		if math.Abs(ch.MHz()-mhz) < 1e-6 {
			ch.mu.Lock()
			ch.forget = true
			ch.mu.Unlock()
		}
	}
	c.m.mu.Unlock()
	return c.Control.Remove(mhz)
}

func (c inputControl) Tune(hz float64) error {
	if err := c.Control.Tune(hz); err != nil {
		return err
	}
	c.m.setInput(c.id, func(in *InputMeta) { in.CenterMHz = hz / 1e6 })
	return nil
}

func (c inputControl) SetGain(db float64, agc bool) error {
	if err := c.Control.SetGain(db, agc); err != nil {
		return err
	}
	c.m.setInput(c.id, func(in *InputMeta) { in.GainDB, in.AGC = db, agc })
	return nil
}

var addKinds = []webui.AddKind{
	{Key: "auto", Label: "周波数で決める（下り 269〜275 MHz・上り 260〜266 MHz）"},
	{Key: "direct", Label: "直接通信（手動選択・通信用）"},
	{Key: "direct-auto", Label: "直接通信（自動選択・通信用。無通信 30 秒で閉じる）"},
	{Key: "direct-ctl", Label: "直接通信（自動選択・制御用）"},
	{Key: "relay-down", Label: "専用チャネル中継（中継局 → 移動局）"},
	{Key: "relay-up", Label: "専用チャネル中継（移動局 → 中継局）"},
}

func (c inputControl) AddKinds() []webui.AddKind { return addKinds }

func (c inputControl) AddKind(mhz float64, kind string) error {
	opt, err := directOptOf(kind)
	if err != nil {
		return err
	}
	if opt == nil {
		return c.Add(mhz)
	}
	if !IsUplinkMHz(mhz) {
		return fmt.Errorf("%.4f MHz は上りの帯（260〜266 MHz）の外。直接通信・中継の局は上りの帯の中", mhz)
	}
	i := 0
	fmt.Sscanf(c.id, "in%d", &i)
	w := c.m.eng
	if w == nil || i >= w.NumInputs() {
		return webui.ErrNoEngine
	}
	return w.Input(i).Add(mhz, opt)
}

func directOptOf(kind string) (*DirectOpt, error) {
	switch kind {
	case "", "auto", "up":
		return nil, nil
	case "direct":
		return &DirectOpt{Mode: l1.ModeDirect, Scramble: -1}, nil
	case "direct-auto":
		return &DirectOpt{Mode: l1.ModeDirect, Scramble: -1, Auto: true}, nil
	case "direct-ctl":
		return &DirectOpt{Mode: l1.ModeDirect, Scramble: -1, Control: true}, nil
	case "relay-down":
		return &DirectOpt{Mode: l1.ModeRelayDown, Scramble: -1}, nil
	case "relay-up":
		return &DirectOpt{Mode: l1.ModeRelayUp, Scramble: -1}, nil
	}
	var keys []string
	for _, k := range addKinds {
		keys = append(keys, k.Key)
	}
	return nil, fmt.Errorf("局の種類 %q は無い（%s）", kind, strings.Join(keys, " / "))
}

func (m *Monitor) inputsOf() []webui.Input {
	var labels []string
	for i := 0; i < m.eng.NumInputs(); i++ {
		labels = append(labels, m.eng.InputLabel(i))
	}
	ins := webui.WideInputs(func() *rx.Wide { return m.eng }, m.eng.NumInputs(), labels...)
	for i := range ins {
		ins[i].Control = inputControl{Control: ins[i].Control, m: m, id: ins[i].ID}
	}
	return ins
}
