package server

type TCHNoter interface {
	NoteTCH() string
}

func tchNote(ext Extension) string {
	if n, ok := ext.(TCHNoter); ok {
		if s := n.NoteTCH(); s != "" {
			return s
		}
	}
	return "音声の組立は系統による（T61 は規定しない）。SACCH（8 枚）とは組立の周期が違う"
}
