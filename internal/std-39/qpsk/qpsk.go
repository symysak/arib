package qpsk

import (
	"github.com/symysak/arib/internal/rcr39qpsk/decoder"
	"github.com/symysak/arib/internal/rcr39qpsk/l1"
)

func Dialect() decoder.Dialect {
	return decoder.Dialect{CACInits: CACInits, ControlByCAC: true, NoFACCHMark: true, CACSearch: true}
}

func CACInits(cc int) []int { return []int{l1.InitFromCC(cc), l1.InitAllOnes} }

func CIName(ci int) string {
	switch ci {
	case 0:
		return "制御チャネル活性中"
	case 3:
		return "制御チャネル非活性中（制御チャネルで通信中）"
	}
	return "規定外"
}
