package municipality

import "github.com/symysak/stdt86/internal/std-t86/citycodes"


type Info struct {
	Scramble int
	Code int
	Name string
	Known bool
}

func CodeFromScramble(scramble int) (code int, ok bool) {
	_ = scramble
	return 0, false
}

func FromScramble(scramble int) Info {
	info := Info{Scramble: scramble}
	if scramble == 0 {
		return info
	}
	code, ok := CodeFromScramble(scramble)
	if !ok {
		return info
	}
	info.Code = code
	if name, found := citycodes.Name(code); found {
		info.Name = name
	}
	info.Known = true
	return info
}

func (i Info) Label() string {
	switch {
	case !i.Known:
		return "未判明"
	case i.Name == "":
		return "コード " + itoa(i.Code) + "（名称未登録）"
	default:
		return i.Name
	}
}

func itoa(v int) string {
	if v == 0 {
		return "0"
	}
	var b [8]byte
	n := len(b)
	for v > 0 {
		n--
		b[n] = byte('0' + v%10)
		v /= 10
	}
	return string(b[n:])
}
