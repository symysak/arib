package server

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	nx "github.com/symysak/arib/internal/nxdn/server"
	"github.com/symysak/arib/internal/std-t102/l3"
)


func (k *link) Profile() nx.Profile {
	var p nx.Profile
	p.Std = "STD-T102 第 2 編"
	if k.b54 != nil && k.b54.Seen() {
		p.Std += " + ARIB STD-B54"
	}
	var impl []string
	st := k.h.Station()
	maker, u := st.Maker, st.U
	if maker >= 0 {
		s := fmt.Sprintf("製造者 %d", maker)
		if l3.HasVendor(maker) {
			s += "（独自の読みあり）"
		}
		impl = append(impl, s)
	}
	if f := k.h.DataFormat(); f != "" && f != "auto" && f != "off" {
		impl = append(impl, "データの書式 "+f)
	}
	p.Impl = strings.Join(impl, "・")
	if cd := k.h.Codec(); cd == "ambe" {
		p.Codec = "AMBE+2 3600x2450（EHR）"
	} else if cd != "" {
		p.Codec = cd
	}
	if init, ok := k.h.Init(); ok {
		p.Keys = append(p.Keys, nx.KV{Name: "ホワイトニングの初期値", Value: fmt.Sprintf("0x%03X（%d）", init, init)})
	}
	if u >= 0 {
		p.Keys = append(p.Keys, nx.KV{Name: "ユーザコード U", Value: strconv.Itoa(u)})
	}
	if key, ok := strings.CutPrefix(k.h.VoiceHint(), "ambe:"); ok && key != "" {
		k, sf := strings.CutSuffix(key, "/sf")
		switch n, err := strconv.Atoi(k); {
		case err != nil:
		case n == 0:
			p.Keys = append(p.Keys, nx.KV{Name: "音声秘話", Value: "なし（平文）"})
		case sf:
			p.Keys = append(p.Keys, nx.KV{Name: "音声秘話（B54 スクランブル暗号・スーパーフレーム周期）", Value: fmt.Sprintf("%d（0x%04X）", n, n)})
		default:
			p.Keys = append(p.Keys, nx.KV{Name: "音声秘話（NXDN スクランブラ・80 ms 周期）", Value: fmt.Sprintf("%d（0x%04X）", n, n)})
		}
	}
	if k.b54 != nil {
		ks := k.b54.KnownKeys()
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
			p.Keys = append(p.Keys, nx.KV{Name: fmt.Sprintf("B54 暗号鍵（鍵番号 %d）", no), Value: fmt.Sprintf("%d（0x%04X・%s）", v.Key, v.Key, how)})
		}
	}
	return p
}
