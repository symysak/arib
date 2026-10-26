package l2

import (
	"fmt"

	"github.com/symysak/arib/internal/std-t61/l1"
)

var (
	acNames = []string{"制御なし", "SMSI 割当", "SMSI 解除", "MSI+SMSI 要求"}
	aiNames = []string{"MSI/SMSI 無し", "MSI あり", "SMSI あり", "SMSI + MSI"}
)

func row(name string, bits string, v int, text string) l1.Row {
	return l1.Row{Name: name, Bits: bits, Value: v, Text: text}
}

func (f Frame) Expand() []l1.Row {
	rows := []l1.Row{
		row("生オクテット", "", len(f.Raw), fmt.Sprintf("% X", f.Raw)),
		row("AC ID制御", fmt.Sprintf("%02b", f.Address.AC), int(f.Address.AC), pick(acNames, f.Address.AC)),
		row("AI ID表示", fmt.Sprintf("%02b", f.Address.AI), int(f.Address.AI), pick(aiNames, f.Address.AI)),
		row("SAPI", fmt.Sprintf("%03b", f.Address.SAPI), int(f.Address.SAPI), sapiName(f.Address.SAPI)),
		row("C/R", boolBit(f.Address.CR), boolInt(f.Address.CR), f.crText()),
	}
	if f.Address.HasSMSI() {
		rows = append(rows, row("SMSI 補助移動局識別", fmt.Sprintf("%08b", f.Address.SMSI), int(f.Address.SMSI), ""))
	}
	if f.Address.HasMSI() {
		text := fmt.Sprintf("%d (0x%X, %d ビット)", f.Address.MSI, f.Address.MSI, f.Address.MSIBits)
		if f.Address.IsGlobal() {
			text += " = 放送用グローバル"
		}
		rows = append(rows, row("MSI 移動局識別", "", int(f.Address.MSI), text))
	}
	rows = append(rows,
		row("アドレス長", "", f.Address.Octets, fmt.Sprintf("%d オクテット", f.Address.Octets)),
		row("制御形式", "", int(f.Control.Format), f.Control.Name),
	)
	switch f.Control.Format {
	case FormatI:
		rows = append(rows,
			row("N(S) 送信順序番号", "", int(f.Control.NS), ""),
			row("N(R) 受信順序番号", "", int(f.Control.NR), ""))
	case FormatS:
		rows = append(rows, row("N(R) 受信順序番号", "", int(f.Control.NR), ""))
	}
	if len(f.Control.Partial) > 0 {
		name, text := "部分再送 n(R) 再送要求ユニット", fmt.Sprintf("%v", f.Control.Partial)
		if f.Control.Format == FormatI {
			name = "部分再送 n(S) 送るユニット"
		}
		if p := f.Control.Partial; len(p) == 2 && p[1] == 0 && p[0] > 0 {
			text += fmt.Sprintf("（%d 以降全部）", p[0])
		}
		if f.Control.PartialUndefined() {
			text += "（S' で規格が定義するのは REJ' だけ）"
		}
		rows = append(rows, row(name, "", len(f.Control.Partial), text))
	}
	rows = append(rows,
		row("P/F ポール/ファイナル", boolBit(f.Control.PF), boolInt(f.Control.PF), ""),
		row("情報フィールド長", "", len(f.Info), fmt.Sprintf("% X", f.Info)),
	)
	if r, ok := f.FRMRInfo(); ok {
		rows = append(rows, row("FRMR リジェクト理由", "", len(f.Info), r.Text()))
	}
	if f.Control.Name == "XID" && len(f.Info) > 0 {
		rows = append(rows, row("XID 識別情報", "", len(f.Info), fmt.Sprintf("% X（中身と交換の手順は規定しない。§4.2.2.3.8.12）", f.Info)))
	}
	if m, ok := f.Management(); ok {
		rows = append(rows,
			row("マネジメントエンティティ識別子", "", int(f.Info[0]), m.Entity),
			row("メッセージ種別（SMSI チェック手順）", "", len(f.Info), m.Name))
		if len(m.Rest) > 0 {
			rows = append(rows, row("後続", "", len(m.Rest), fmt.Sprintf("% X", m.Rest)))
		}
	}
	if f.Control.Name == "I'" {
		rows = append(rows, row("I' の情報", "", len(f.Info), "元の I のユニットの並び（先頭ユニットの残りは Don't care）。レイヤ 3 ではない"))
	}
	if f.Err != "" {
		rows = append(rows, row("形式異常", "", 0, f.Err))
	}
	for _, n := range f.Notes() {
		rows = append(rows, row("規格の注記", "", 0, n))
	}
	return rows
}

func pick(t []string, i uint8) string {
	if int(i) < len(t) {
		return t[i]
	}
	return "?"
}

func sapiName(v uint8) string {
	switch v {
	case 0:
		return "呼制御"
	case 4:
		return "予約(実波で使用)"
	case 7:
		return "レイヤ2マネジメント"
	}
	return "予約"
}

func (f Frame) crText() string {
	cmd, ok := f.Command()
	if !ok {
		if f.Address.CR {
			return "基地局→移動局ならコマンド / 移動局→基地局ならレスポンス"
		}
		return "基地局→移動局ならレスポンス / 移動局→基地局ならコマンド"
	}
	s := "レスポンス"
	if cmd {
		s = "コマンド"
	}
	if f.Dir == DirUp {
		return s + "（移動局→基地局）"
	}
	return s + "（基地局→移動局）"
}

func boolBit(b bool) string {
	if b {
		return "1"
	}
	return "0"
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
