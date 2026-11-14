package chplan

import (
	"fmt"
	"math"
	"sort"
)

type Class string

const (
	Registered    Class = "登録局"
	RegisteredSky Class = "登録局（上空）"
	Licensed      Class = "免許局"
	RelayUp       Class = "免許局（中継・上り）"
	RelayDown     Class = "免許局（中継・下り）"
	Licensed150   Class = "免許局 150 MHz 帯"
)

type Channel struct {
	Class Class `json:"class"`
	No  string  `json:"no"`
	MHz float64 `json:"mhz"`
	Note string `json:"note,omitempty"`
}

func (c Channel) Label() string { return fmt.Sprintf("%s %s ch", c.Class, c.No) }

const SpacingMHz = 0.00625

type block struct {
	class  Class
	prefix string
	first  int
	n      int
	base   float64
}

var blocks = []block{
	{Registered, "", 1, 70, 351.20000},
	{Registered, "", 71, 12, 351.03125},
	{RegisteredSky, "S", 1, 5, 351.16875},
	{RegisteredSky, "S", 6, 10, 351.10625},
	{Licensed, "", 1, 65, 467.00000},
	{Licensed, "", 66, 10, 465.096875},
	{RelayUp, "D", 1, 10, 465.034375},
	{RelayDown, "D", 1, 10, 468.796875},
	{Licensed150, "", 1, 28, 154.44375},
}

var all = func() []Channel {
	var out []Channel
	for _, b := range blocks {
		for i := 0; i < b.n; i++ {
			c := Channel{Class: b.class, No: fmt.Sprintf("%s%d", b.prefix, b.first+i), MHz: round(b.base + float64(i)*SpacingMHz)}
			switch {
			case b.class == Registered && b.first+i == 15:
				c.Note = "呼出用チャネル（秘話・選択呼出は使わない。ユーザコードは 0）"
			case b.class == Licensed150 && b.first+i >= 20:
				c.Note = "データ専用チャネル（音声通信の機能を設けない）"
			}
			out = append(out, c)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].MHz < out[j].MHz })
	return out
}()

func round(mhz float64) float64 { return math.Round(mhz*1e6) / 1e6 }

func All() []Channel { return append([]Channel(nil), all...) }

func Frequencies() []float64 {
	out := make([]float64, len(all))
	for i, c := range all {
		out[i] = c.MHz
	}
	return out
}

func Lookup(mhz float64) (Channel, bool) {
	i := sort.Search(len(all), func(i int) bool { return all[i].MHz >= mhz })
	best, bd := -1, math.Inf(1)
	for _, k := range []int{i - 1, i} {
		if k >= 0 && k < len(all) {
			if d := math.Abs(all[k].MHz - mhz); d < bd {
				best, bd = k, d
			}
		}
	}
	if best < 0 || bd > SpacingMHz/2 {
		return Channel{}, false
	}
	return all[best], true
}

func Within(centerMHz, spanMHz float64) []Channel {
	var out []Channel
	for _, c := range all {
		if math.Abs(c.MHz-centerMHz) <= spanMHz/2 {
			out = append(out, c)
		}
	}
	return out
}
