package l3

import (
	"fmt"
	"strings"
)

type pairRule struct {
	nos  string
	rt   uint8
	cc   []uint8
	soft string
}

var pairRules = []pairRule{
	{"(1)(2)", 0x01, []uint8{0x15, 0x17}, ""},
	{"(3)", 0x02, []uint8{0x12}, ""},
	{"(4)", 0x03, []uint8{0x13}, ""},
	{"(5)", 0x21, []uint8{0x5A}, ""},
	{"(6)", 0x43, []uint8{0x05}, "発移動局と着移動局が別のゾーンなら必須ではない（※1）"},
	{"(7)", 0x81, []uint8{0x45}, ""},
	{"(8)(10)", 0x72, []uint8{0x16, 0x18}, ""},
	{"(9)", 0x73, []uint8{0x16}, ""},
	{"(11)", 0x71, []uint8{0x14}, ""},
	{"(12)", 0x04, []uint8{0x19}, ""},
}

var ccNeedsRT = map[uint8][]uint8{
	0x15: {0x01}, 0x17: {0x01}, 0x12: {0x02}, 0x13: {0x03}, 0x16: {0x72, 0x73}, 0x18: {0x72}, 0x14: {0x71}, 0x19: {0x04},
}

func pairingNotes(s Signal) []string {
	var rt, cc *Message
	for i := range s.Messages {
		switch s.Messages[i].Entity {
		case "RT":
			rt = &s.Messages[i]
		case "CC":
			cc = &s.Messages[i]
		}
	}
	var out []string
	has := func(list []uint8, v uint8) bool {
		for _, x := range list {
			if x == v {
				return true
			}
		}
		return false
	}
	if rt != nil && rt.Err == "" {
		for _, r := range pairRules {
			if r.rt != rt.Type {
				continue
			}
			if cc != nil && has(r.cc, cc.Type) {
				break
			}
			var want []string
			for _, t := range r.cc {
				want = append(want, ccNames[t])
			}
			got := "CC が無い"
			if cc != nil {
				got = "CC が " + cc.Name
			}
			n := fmt.Sprintf("相乗りの組 %s: RT %s は CC %s と相乗りで送る（§4.3.3.2）が、%s", r.nos, rt.Name, strings.Join(want, " か "), got)
			if r.soft != "" {
				n += "（" + r.soft + "）"
			}
			out = append(out, n)
			break
		}
	}
	if cc != nil && cc.Err == "" {
		if need, ok := ccNeedsRT[cc.Type]; ok && (rt == nil || !has(need, rt.Type)) {
			var want []string
			for _, t := range need {
				want = append(want, rtNames[t])
			}
			got := "RT が無い"
			if rt != nil {
				got = "RT が " + rt.Name
			}
			out = append(out, fmt.Sprintf("相乗りの組: CC %s は RT %s と相乗りで送る（§4.3.3.2）が、%s", cc.Name, strings.Join(want, " か "), got))
		}
	}
	return out
}
