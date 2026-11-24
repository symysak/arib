package nonvoice

import "fmt"


type ContW struct {
	F11, F12 bool
	W0       int
	F21, F22 bool
	SF       bool
	W1       int
	Raw      int
}

func parseContW(b []uint8) ContW {
	return ContW{F11: b[0] == 1, F12: b[1] == 1, W0: bitsVal(b[2:8]), F21: b[8] == 1, F22: b[9] == 1, SF: b[10] == 1, W1: bitsVal(b[11:16]), Raw: bitsVal(b[:16])}
}

func contWBits(w ContW) []uint8 {
	b := []uint8{b01(w.F11), b01(w.F12)}
	b = putBits(b, w.W0, 6)
	b = append(b, b01(w.F21), b01(w.F22), b01(w.SF))
	return putBits(b, w.W1, 5)
}

func (w ContW) String() string {
	if w.Raw == 0 {
		return "メッセージ無し"
	}
	var c string
	switch {
	case w.F11 && w.F12:
		c = fmt.Sprintf("制御 1 ユニット L=%d", w.W0)
	case w.F11:
		c = fmt.Sprintf("制御 先頭 残り%d", w.W0)
	case w.F12:
		c = fmt.Sprintf("制御 最終 L=%d", w.W0)
	case w.W0 != 0:
		c = fmt.Sprintf("制御 途中 残り%d", w.W0)
	default:
		c = "制御 無し"
	}
	var u string
	switch {
	case w.F21 && w.F22:
		u = "ユーザ 1 ユニット"
	case w.F21:
		u = "ユーザ 先頭"
	case w.F22:
		u = "ユーザ 最終"
	case w.W1 != 0:
		u = "ユーザ 途中"
	default:
		u = "ユーザ 無し"
	}
	if w.W1 != 0 || w.F21 || w.F22 {
		u += fmt.Sprintf(" L=%d", w.W1)
	}
	if w.SF {
		u += "（スチール有）"
	}
	return c + " ／ " + u
}

type ContUnit struct {
	Format Format
	W      ContW
	Ctl    []byte
	User   []byte
	N      int
	IB, RN int
	PE     uint16
}

func parseContUnit(f Format, b []uint8) ContUnit {
	u := ContUnit{Format: f, W: parseContW(b[:16])}
	switch f {
	case FmtContDown:
		u.Ctl, u.User, u.N = bytesOf(b[16:48]), bytesOf(b[48:96]), bitsVal(b[96:101])
		u.IB, u.RN, u.PE = int(b[101]), int(b[102]), uint16(bitsVal(b[103:119]))
	case FmtContUp1:
		u.User = bytesOf(b[16:64])
	case FmtContUpN:
		u.Ctl, u.User, u.N = bytesOf(b[16:40]), bytesOf(b[40:88]), bitsVal(b[88:91])
	}
	return u
}

func (u ContUnit) userArea() []byte {
	if u.W.SF {
		return append(append([]byte(nil), u.Ctl...), u.User...)
	}
	return u.User
}

func ContDownBits(w ContW, ctl, user []byte, ib, rn int, pe uint16) []uint8 {
	b := contWBits(w)
	b = append(b, padBits(ctl, 32)...)
	b = append(b, padBits(user, 48)...)
	b = append(b, 0, 0, 0, 0, 0, uint8(ib&1), uint8(rn&1))
	return putBits(b, int(pe), 16)
}

func ContUpBits(first bool, w ContW, ctl, user []byte) []uint8 {
	b := contWBits(w)
	if first {
		return append(b, padBits(user, 48)...)
	}
	b = append(b, padBits(ctl, 24)...)
	b = append(b, padBits(user, 48)...)
	return append(b, 0, 0, 0)
}

type contStream struct {
	ctl     []byte
	ctlOpen bool
	ctlWr   int
	ctlT    float64
	user    []byte
	usrOpen bool
	usrT    float64
	units   int
}
