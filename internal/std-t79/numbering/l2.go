package numbering

import (
	"fmt"

	"github.com/symysak/arib/internal/rcr39qpsk/l2"
)

func AddressMSI(a l2.Address) (MSI, bool) {
	if !a.HasMSI() || a.IsGlobal() {
		return MSI{}, false
	}
	return ParseMSI(a.MSI, a.MSIBits)
}

func L2Fields(msi uint64, bits int) []l2.Field {
	m, ok := ParseMSI(msi, bits)
	if !ok {
		return nil
	}
	call := msi & 0xFFFFF
	kind := "グループ番号"
	if m.Individual {
		kind = "個別番号"
	}
	out := []l2.Field{{Name: "呼出符号（MSI 下位 20 ビット）", Value: fmt.Sprintf("識別子=%d（%s） 局番号=%d", call>>19, kind, m.Number)}}
	if msi>>20 == 0 {
		out = append(out, l2.Field{Name: "システムコード（MSI 上位）", Value: "0（グループ番号 90 番台など）"})
	} else {
		out = append(out, l2.Field{Name: "システムコード（MSI 上位）", Value: m.System.Text()},
			l2.Field{Name: "団体（MSI）", Value: m.System.Org})
	}
	return out
}
