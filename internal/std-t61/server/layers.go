package server

import (
	"fmt"
	"github.com/symysak/arib/internal/bitpack"

	dec "github.com/symysak/arib/internal/std-t61/decoder"
	"github.com/symysak/arib/internal/std-t61/l1"
	"github.com/symysak/arib/internal/std-t61/l2"
)


type Node struct {
	Label    string `json:"label"`
	Value    string `json:"value,omitempty"`
	Bits     string `json:"bits,omitempty"`
	Range    string `json:"range,omitempty"`
	Note     string `json:"note,omitempty"`
	Children []Node `json:"children,omitempty"`
}

type L1Rec struct {
	Idx      int     `json:"idx"`
	No       int64   `json:"no"`
	TSec     float64 `json:"t_sec"`
	MHz      float64 `json:"mhz"`
	Kind     string  `json:"kind"`
	Uplink   bool    `json:"uplink"`
	Flywheel bool    `json:"flywheel"`
	SFPos    int     `json:"sf_pos"`
	Why      string  `json:"why"`
	Info     string  `json:"info"`
	RICHOK   bool    `json:"rich_ok"`
	RICHBits string  `json:"rich_bits"`
	Tree     []Node  `json:"tree"`
	RawHex   string  `json:"raw_hex"`
	L2No     int64   `json:"l2_no,omitempty"`
}

type L2Rec struct {
	Idx      int     `json:"idx"`
	No       int64   `json:"no"`
	TSec     float64 `json:"t_sec"`
	MHz      float64 `json:"mhz"`
	Channel  string  `json:"channel"`
	State    string  `json:"state"`
	Stage    string  `json:"stage"`
	Units    int     `json:"units"`
	Remain   int     `json:"remain"`
	Summary  string  `json:"summary"`
	RawHex   string  `json:"raw_hex"`
	Tree     []Node  `json:"tree"`
	FrameNos []int64 `json:"frame_nos"`
	L3Nos    []int64 `json:"l3_nos,omitempty"`
	Err      string  `json:"err,omitempty"`
}

type L3Rec struct {
	Idx    int     `json:"idx"`
	No     int64   `json:"no"`
	TSec   float64 `json:"t_sec"`
	MHz    float64 `json:"mhz"`
	Entity string  `json:"entity"`
	Name   string  `json:"name"`
	Tree   []Node  `json:"tree"`
	RawHex string  `json:"raw_hex"`
	L2No   int64   `json:"l2_no"`
	Err    string  `json:"err,omitempty"`
}

func rangeOf(a, b int) string { return fmt.Sprintf("bit %d-%d", a, b) }

func RowsToNodes(rows []l1.Row) []Node { return rowsToNodes(rows) }

func rowsToNodes(rows []l1.Row) []Node {
	out := make([]Node, 0, len(rows))
	for _, r := range rows {
		out = append(out, Node{Label: r.Name, Bits: r.Bits, Value: r.Text, Note: fmt.Sprintf("= %d", r.Value)})
		if r.Text == "" {
			out[len(out)-1].Value = fmt.Sprintf("%d", r.Value)
			out[len(out)-1].Note = ""
		}
	}
	return out
}

func buildL1Tree(f dec.Frame, ed l1.Edition, ext Extension) []Node {
	if f.A1RICH != nil {
		return buildA1Tree(f)
	}
	phys := Node{Label: "物理層", Value: "384 ビット / 40 ms"}
	sw := "慣性（同期ワード未検出）"
	if !f.Flywheel && f.SW != nil {
		sw = fmt.Sprintf("%s（誤り %d ビット）", f.SW.Name, f.SWErrors)
	}
	phys.Children = append(phys.Children,
		Node{Label: "同期ワード", Value: sw, Range: rangeOf(l1.SWStart, l1.SWStart+20)},
	)
	rich := Node{Label: "RICH", Range: rangeOf(l1.RIStart, l1.RIEnd),
		Note: "17 情報ビット + CRC6 → 畳込み K=6 → 8×7 インターリーブ"}
	if f.RICH.OK {
		rich.Value = f.RICH.Bits17()
		rich.Children = append(rowsToNodes(f.RICH.ExpandRICH(ed)),
			Node{Label: "読み下しの注意", Value: "表は T61 のもの。上位方式で割当が違うことがある（その読みは後ろの行）"})
		if ext != nil {
			rich.Children = append(rich.Children, rowsToNodes(ext.ExpandRICH(f.RICH))...)
			if n := ext.NoteRICH(f.RICH); n != "" {
				rich.Note = n
			}
		}
	} else {
		rich.Value = "CRC6 不一致"
	}
	phys.Children = append(phys.Children, rich)
	if f.SFPos >= 0 {
		phys.Children = append(phys.Children,
			Node{Label: "スーパーフレーム内位置", Value: fmt.Sprintf("%d / 18", f.SFPos)})
	}
	out := []Node{phys}

	if u := f.Control; u != nil {
		n := Node{Label: "制御ユニット", Range: rangeOf(l1.FieldAStart, l1.FieldAEnd) + " + " + rangeOf(l1.PCCDataStart, l1.FrameEnd)}
		if u.Uplink {
			n.Label = "上り制御ユニット（第 1）"
			n.Range = "bit 96-128 + bit 208-376（図 4.1.11-4 (G)）"
			if u.Second {
				n.Label = "上り制御ユニット（第 2 以降）"
				n.Range = "bit 32-128 + bit 208-376（図 4.1.11-4 (h)）"
			}
		}
		switch {
		case u.OK:
			n.Value = fmt.Sprintf("CRC 一致 W %s", u.W)
		case l1.IsFiller(u.OK, u.Corrected, l1.AllZero(u.Data)):
			n.Value = "空き（充填。全ゼロの零符号語 or 非符号語）"
		default:
			n.Value = "CRC 不一致"
		}
		n.Note = fmt.Sprintf("訂正 %d ／ N=%d", u.Corrected, u.N)
		n.Children = []Node{{Label: "情報部", Value: hexOf(bitpack.ToBytes(u.Data))}}
		if e := u.EText(); e != "" {
			n.Children = append(n.Children, Node{Label: "衝突制御 E", Value: e,
				Note: "I/B = アイドル/ビジー、R/N = 上りを受けたか、PE = 受けた上りの部分エコー（§4.1.21.3）"})
		}
		out = append(out, n)
	}
	if u := f.NV2; u != nil {
		n := Node{Label: "非音声2 ユニット", Value: fmt.Sprintf("CRC 一致 W %s", u.W),
			Range:    rangeOf(l1.FieldAStart, l1.FieldAEnd) + " + " + rangeOf(l1.FieldBStart, l1.FrameEnd),
			Note:     fmt.Sprintf("訂正 %d ／ N=%d。FACCH と同じ 2 フレーム・16×16・K=6・CRC16", u.Corrected, u.N),
			Children: []Node{{Label: "情報部", Value: hexOf(bitpack.ToBytes(u.Data))}}}
		if f.NV2Msg != nil {
			n.Children = append(n.Children, Node{Label: "メッセージ完結", Value: hexOf(f.NV2Msg), Note: printableASCII(f.NV2Msg)})
		}
		out = append(out, n)
	}

	for _, fr := range f.Frag {
		n := Node{Label: fr.Channel, Range: rangeOf(l1.MidStart, l1.MidEnd)}
		if fr.Channel == "FACCH" {
			n.Range = rangeOf(l1.FieldAStart, l1.FieldAEnd) + " + " + rangeOf(l1.FieldBStart, l1.FrameEnd)
		}
		switch {
		case fr.Done && fr.OK:
			n.Value = fmt.Sprintf("%d/%d 枚 → ユニット完結", fr.Have, fr.Need)
			n.Note = fmt.Sprintf("訂正 %d ／ ビタビ一致度 %.2f", fr.Corrected, fr.Metric)
		case fr.Done && fr.Filler:
			n.Value = fmt.Sprintf("%d/%d 枚 → 空き枠（充填）", fr.Have, fr.Need)
			n.Note = fmt.Sprintf("訂正 %d ／ 全ゼロ＝零符号語 or 計数語で埋めた空きスロット（CRC が通る"+
				"「メッセージ無し」ユニットとは別物。あちらは W=00 の正規アイドル）", fr.Corrected)
		case fr.Done:
			n.Value = fmt.Sprintf("%d/%d 枚 → 符号語だが CRC 不一致（別規約の疑い）", fr.Have, fr.Need)
			n.Note = fmt.Sprintf("訂正 %d と小さいのに CRC16 が合わない。初期値・多項式・対象範囲が違う可能性", fr.Corrected)
		default:
			n.Value = fmt.Sprintf("組立中 %d/%d 枚", fr.Have, fr.Need)
		}
		if fr.Done {
			note := "（未検証）"
			label := "ユニット"
			switch {
			case fr.Filler:
				label = "ユニット（空き枠の充填）"
				note = "送るものが無いスロットの充填（全ゼロ or 計数語）。CRC が通る「メッセージ無し」ユニットとは別物"
			case !fr.OK:
				label = "ユニット（符号語だが CRC 不一致・未検証）"
				note = "CRC16 CCITT-FALSE で検査した結果。別規約なら初期値・多項式・対象範囲が違う"
			}
			switch {
			case fr.Channel == "SACCH" && f.SACCH != nil:
				if fr.OK {
					note = fmt.Sprintf("W %s ／ N=%d", f.SACCH.W, f.SACCH.N)
				}
				n.Children = []Node{{Label: label, Value: hexOf(bitpack.ToBytes(f.SACCH.Data)), Note: note}}
			case fr.Channel == "FACCH" && f.FACCH != nil:
				if fr.OK {
					note = fmt.Sprintf("W %s ／ N=%d", f.FACCH.W, f.FACCH.N)
				}
				n.Children = []Node{{Label: label, Value: hexOf(bitpack.ToBytes(f.FACCH.Data)), Note: note}}
			case fr.Channel == "RCH" && f.RCH != nil:
				n.Children = []Node{{Label: label, Value: fmt.Sprintf("%08b", f.RCH.Raw),
					Note: fmt.Sprintf("%s ／ N=%d", f.RCH.Describe(f.Uplink), f.RCH.N)}}
			}
		}
		out = append(out, n)
	}
	if f.SB != nil {
		n := Node{Label: "同期バースト パラメータ部", Range: rangeOf(l1.SBPIStr, l1.SBPIEnd)}
		if f.SB.OK {
			n.Value = f.SB.Describe(ed)
			rows := f.SB.ExpandSyncBurst()
			if ext != nil {
				rows = append(rows, ext.ExpandSyncBurst(*f.SB)...)
				if s := ext.NoteSyncBurst(*f.SB); s != "" {
					n.Note = s
				}
			}
			n.Children = rowsToNodes(rows)
		} else {
			n.Value = "CRC6 不一致"
		}
		out = append(out, n)
	}
	if len(f.TCH) > 0 {
		out = append(out, Node{Label: "TCH", Value: fmt.Sprintf("%d ビット", len(f.TCH)),
			Range: rangeOf(l1.FieldAStart, l1.FieldAEnd) + " + " + rangeOf(l1.FieldBStart, l1.FrameEnd),
			Note:  tchNote(ext)})
	}
	return out
}

type L2Noter interface {
	NoteL2() string
}

func buildL2Tree(fr l2.Frame, ext Extension) []Node {
	note := "T61 の LAPDM（表 4.2.2.3-5）として読んだもの"
	if n, ok := ext.(L2Noter); ok {
		if s := n.NoteL2(); s != "" {
			note = s
		}
	}
	val := fmt.Sprintf("%s | %s", fr.Address, fr.Control)
	var kids []Node
	if fr.Note != "" {
		val += "（読み替え）"
		kids = append(kids, Node{Label: "読み替え", Value: fr.Note,
			Note: "規格どおりには読めないので派生実装の形で読んだ（推測）"})
	}
	kids = append(kids, rowsToNodes(fr.Expand())...)
	return []Node{{Label: "LAPDM", Value: val, Note: note, Children: kids}}
}

func buildA1Tree(f dec.Frame) []Node {
	phys := Node{Label: "物理層", Value: "第 1 編 付録 1・384 ビット / 40 ms"}
	sw := "慣性（同期ワード未検出）"
	if !f.Flywheel && f.SW != nil {
		sw = fmt.Sprintf("SW1（%s と同じ値。誤り %d ビット）", f.SW.Name, f.SWErrors)
	}
	phys.Children = append(phys.Children, Node{Label: "同期ワード", Value: sw, Range: rangeOf(8, 28)})
	rich := Node{Label: "RICH", Range: rangeOf(l1.A1DataStart, l1.A1DataStart+l1.A1RICHBits),
		Note: "9 情報ビット + CRC4 + CRC8 → 畳込み K=6 → 深さ 8 のインタリーブ"}
	if r := f.A1RICH; r.OK {
		rich.Value = fmt.Sprintf("%09b", r.Raw)
		rich.Children = []Node{{Label: "読み下し", Value: r.Describe()}, {Label: "CRC の計算法", Value: r.CRCVariant}}
	} else {
		rich.Value = "CRC 不一致"
	}
	tch := Node{Label: "TCH1", Value: fmt.Sprintf("%d ビット", len(f.TCH1)),
		Range: rangeOf(l1.A1DataStart+l1.A1RICHBits, l1.A1DataStart+l1.A1RICHBits+l1.A1TCH1Bits),
		Note:  "スクランブル（PN(9,5)、パターン番号は規格が規定しない）を掛けたまま。中身の組立は規格が規定しない"}
	phys.Children = append(phys.Children, rich, tch)
	return []Node{phys}
}
