package cliflag

import (
	"fmt"
	"strconv"
	"strings"
)

type Channel struct {
	MHz float64
	Opts map[string]string
}

func ParseChannels(s string) ([]Channel, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, nil
	}
	var out []Channel
	for _, item := range strings.Split(s, ",") {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		parts := strings.Split(item, ":")
		mhz, err := strconv.ParseFloat(strings.TrimSpace(parts[0]), 64)
		if err != nil || mhz <= 0 {
			return nil, fmt.Errorf("-channels の周波数が読めない: %q（MHz で書く。例 58.505）", parts[0])
		}
		ch := Channel{MHz: mhz}
		for _, kv := range parts[1:] {
			k, v, ok := strings.Cut(kv, "=")
			k, v = strings.TrimSpace(k), strings.TrimSpace(v)
			if !ok || k == "" {
				return nil, fmt.Errorf("-channels の局の設定が読めない: %q（<鍵>=<値> で書く。例 %g:seed=289）", kv, mhz)
			}
			if ch.Opts == nil {
				ch.Opts = map[string]string{}
			}
			if _, dup := ch.Opts[k]; dup {
				return nil, fmt.Errorf("-channels の %g MHz に %s が 2 回ある", mhz, k)
			}
			ch.Opts[k] = v
		}
		out = append(out, ch)
	}
	return out, nil
}

func CheckKeys(chans []Channel, known ...string) error {
	for _, c := range chans {
		for k := range c.Opts {
			ok := false
			for _, n := range known {
				if k == n {
					ok = true
					break
				}
			}
			if !ok {
				return fmt.Errorf("-channels の %g MHz: 知らない設定 %q（使えるのは %s）", c.MHz, k, strings.Join(known, "・"))
			}
		}
	}
	return nil
}

func MHzList(chans []Channel) []float64 {
	out := make([]float64, len(chans))
	for i, c := range chans {
		out[i] = c.MHz
	}
	return out
}
