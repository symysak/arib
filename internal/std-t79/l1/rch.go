package l1

import "fmt"


const RCHBits = 14

var bchP = [4][4]uint8{{1, 0, 1, 1}, {1, 1, 1, 0}, {1, 1, 0, 1}, {0, 1, 1, 1}}

func bch84(d []uint8) []uint8 {
	out := append([]uint8(nil), d[:4]...)
	for j := 0; j < 4; j++ {
		var p uint8
		for i := 0; i < 4; i++ {
			p ^= d[i] & bchP[i][j]
		}
		out = append(out, p)
	}
	return out
}

type RCH struct {
	OK        bool
	Info      []uint8
	Corrected int
	CRCInit   uint8
}

func DecodeRCH(first, second []float32) RCH {
	tx := append(append([]float32(nil), first...), second...)
	y := make([]float32, 40)
	for i, s := range tx {
		y[(i%5)*8+i/5] = s
	}
	var r RCH
	data := make([]uint8, 0, 20)
	for k := 0; k < 5; k++ {
		cw := y[8*k : 8*k+8]
		best, bm := 0, float32(-1e30)
		for v := 0; v < 16; v++ {
			c := bch84([]uint8{uint8(v >> 3 & 1), uint8(v >> 2 & 1), uint8(v >> 1 & 1), uint8(v & 1)})
			var m float32
			for i, b := range c {
				if b == 0 {
					m += cw[i]
				} else {
					m -= cw[i]
				}
			}
			if m > bm {
				best, bm = v, m
			}
		}
		c := bch84([]uint8{uint8(best >> 3 & 1), uint8(best >> 2 & 1), uint8(best >> 1 & 1), uint8(best & 1)})
		for i, b := range c {
			if hardOf(cw[i]) != b {
				r.Corrected++
			}
		}
		data = append(data, c[:4]...)
	}
	r.Info = data[:RCHBits]
	got := uint8(bitsVal(data[RCHBits:20]))
	for _, init := range []uint8{0x3F, 0} {
		if CRC6(r.Info, init) == got {
			r.OK, r.CRCInit = true, init
			break
		}
	}
	return r
}

func EncodeRCH(info []uint8) (first, second []uint8) {
	d := append(append([]uint8(nil), info[:RCHBits]...), putBits(nil, int(CRC6(info[:RCHBits], 0x3F)), 6)...)
	y := make([]uint8, 0, 40)
	for k := 0; k < 5; k++ {
		y = append(y, bch84(d[4*k:4*k+4])...)
	}
	tx := make([]uint8, 40)
	for i := range tx {
		tx[i] = y[(i%5)*8+i/5]
	}
	return tx[:20], tx[20:]
}

func PowText(v int) string {
	switch v {
	case 0b0000:
		return "0dB"
	case 0b0010:
		return "−10dB"
	case 0b0100:
		return "−20dB"
	case 0b1010:
		return "+10dB"
	case 0b1100:
		return "+20dB"
	}
	return fmt.Sprintf("予備(%04b)", v)
}

func TAText(v int) string {
	switch {
	case v == 0b1111:
		return "現タイミング送信する"
	case v == 0:
		return "標準タイミングで送信する"
	case v <= 7:
		return fmt.Sprintf("%d シンボル早める", v)
	}
	return fmt.Sprintf("予約(%04b)", v)
}

func RCHText(info []uint8, up bool) string {
	if len(info) < RCHBits {
		return ""
	}
	if up {
		switch bitsVal(info[:2]) {
		case 0:
			return "アイドル"
		case 1:
			return "状態報告 POW-U " + PowText(bitsVal(info[10:14]))
		case 2:
			return fmt.Sprintf("免許人固有信号 %012b", bitsVal(info[2:]))
		}
		return fmt.Sprintf("予備 %014b", bitsVal(info))
	}
	if info[0] == 1 {
		return fmt.Sprintf("予備 %014b", bitsVal(info))
	}
	switch bitsVal(info[1:4]) {
	case 0:
		return "アイドル"
	case 1:
		return "電力制御 POW " + PowText(bitsVal(info[4:8]))
	case 2:
		return "タイムアライメント指定 TA " + TAText(bitsVal(info[4:8]))
	}
	return fmt.Sprintf("免許人固有信号 %013b", bitsVal(info[1:]))
}

func RCHVendorText(maker int, info []uint8, up bool) string {
	t := RCHText(info, up)
	if maker == 163 && !up && len(info) >= RCHBits && info[0] == 0 && bitsVal(info[1:6]) == 0b01100 {
		t += fmt.Sprintf("（独自・日本無線・推定: 通番 %d）", bitsVal(info[6:14]))
	}
	return t
}

type CollisionE struct {
	Allowed  bool
	Received bool
	PE       uint16
}

func (u Unit) E() (CollisionE, bool) {
	if len(u.Extra) != 23 {
		return CollisionE{}, false
	}
	e := u.Extra[5:]
	return CollisionE{Allowed: e[0] == 1, Received: e[1] == 1, PE: uint16(bitsVal(e[2:18]))}, true
}

func (e CollisionE) String() string {
	ib, rn := "送信禁止(B)", "非受信(N)"
	if e.Allowed {
		ib = "送信許可(I)"
	}
	if e.Received {
		rn = "受信(R)"
	}
	return fmt.Sprintf("%s・%s・PE 0x%04X", ib, rn, e.PE)
}
