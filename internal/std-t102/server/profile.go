package server

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/symysak/arib/internal/std-t102/l3"
)


type KV struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

type Profile struct {
	Std   string `json:"std"`
	Impl  string `json:"impl"`
	Codec string `json:"codec"`
	Keys  []KV   `json:"keys"`
}

func (c *Channel) profile() Profile {
	var p Profile
	p.Std = "STD-T102 第 2 編"
	if c.b54 != nil && c.b54.Seen() {
		p.Std += " + ARIB STD-B54"
	}
	var impl []string
	c.mu.Lock()
	maker, u := c.st.Maker, c.st.U
	c.mu.Unlock()
	if maker >= 0 {
		s := fmt.Sprintf("製造者 %d", maker)
		if l3.HasVendor(maker) {
			s += "（独自の読みあり）"
		}
		impl = append(impl, s)
	}
	if f := c.dataSt.Format(); f != "" && f != "auto" && f != "off" {
		impl = append(impl, "データの書式 "+f)
	}
	p.Impl = strings.Join(impl, "・")
	if cd := c.vs.chosen; cd != nil && cd.Name == "ambe" {
		p.Codec = "AMBE+2 3600x2450（EHR）"
	} else if cd != nil {
		p.Codec = cd.Name
	}
	if init, ok := c.keyer.Init(); ok {
		p.Keys = append(p.Keys, KV{"ホワイトニングの初期値", fmt.Sprintf("0x%03X（%d）", init, init)})
	}
	if u >= 0 {
		p.Keys = append(p.Keys, KV{"ユーザコード U", strconv.Itoa(u)})
	}
	if key, ok := strings.CutPrefix(c.vs.hint, "ambe:"); ok && key != "" {
		k, sf := strings.CutSuffix(key, "/sf")
		switch n, err := strconv.Atoi(k); {
		case err != nil:
		case n == 0:
			p.Keys = append(p.Keys, KV{"音声秘話", "なし（平文）"})
		case sf:
			p.Keys = append(p.Keys, KV{"音声秘話（B54 スクランブル暗号・スーパーフレーム周期）", fmt.Sprintf("%d（0x%04X）", n, n)})
		default:
			p.Keys = append(p.Keys, KV{"音声秘話（NXDN スクランブラ・80 ms 周期）", fmt.Sprintf("%d（0x%04X）", n, n)})
		}
	}
	if c.b54 != nil {
		ks := c.b54.KnownKeys()
		var nos []int
		for no := range ks {
			nos = append(nos, no)
		}
		sort.Ints(nos)
		for _, no := range nos {
			v := ks[no]
			how := "設定"
			if v.Found {
				how = "CRC で当てた"
			}
			p.Keys = append(p.Keys, KV{fmt.Sprintf("B54 暗号鍵（鍵番号 %d）", no), fmt.Sprintf("%d（0x%04X・%s）", v.Key, v.Key, how)})
		}
	}
	return p
}
