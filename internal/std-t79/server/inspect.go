package server


import (
	"fmt"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/symysak/arib/internal/std-t79/decoder"
	"github.com/symysak/arib/internal/std-t79/l1"
	"github.com/symysak/arib/internal/std-t79/l2"
	"github.com/symysak/arib/internal/std-t79/l3"
	"github.com/symysak/arib/internal/std-t79/numbering"
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
	No     int64   `json:"no"`
	TSec   float64 `json:"t_sec"`
	Idx    int     `json:"idx"`
	Slot   int     `json:"slot"`
	Frame  int64   `json:"frame"`
	SW     string  `json:"sw"`
	Kind   string  `json:"kind"`
	Why    string  `json:"why"`
	Info   string  `json:"info"`
	Repeat int     `json:"repeat"`
	Tree   []Node  `json:"tree"`
	RawHex string  `json:"raw_hex"`
	L2No   int64   `json:"l2_no,omitempty"`
}

type L2Rec struct {
	No      int64   `json:"no"`
	TSec    float64 `json:"t_sec"`
	Idx     int     `json:"idx"`
	Slot    int     `json:"slot"`
	Channel string  `json:"channel"`
	DLCI    string  `json:"dlci"`
	Org     string  `json:"org,omitempty"`
	Control string  `json:"control"`
	State   string  `json:"state"`
	Summary string  `json:"summary"`
	Err     string  `json:"err,omitempty"`
	Repeat  int     `json:"repeat"`
	Tree    []Node  `json:"tree"`
	RawHex  string  `json:"raw_hex"`
	L3Nos   []int64 `json:"l3_nos,omitempty"`
}

type L3Rec struct {
	No      int64   `json:"no"`
	TSec    float64 `json:"t_sec"`
	Idx     int     `json:"idx"`
	Slot    int     `json:"slot"`
	Channel string  `json:"channel"`
	Entity  string  `json:"entity"`
	Name    string  `json:"name"`
	Summary string  `json:"summary"`
	Err     string  `json:"err,omitempty"`
	Repeat  int     `json:"repeat"`
	Tree    []Node  `json:"tree"`
	RawHex  string  `json:"raw_hex"`
	L2No    int64   `json:"l2_no"`
}

type CallRec struct {
	No       int64   `json:"no"`
	TSec     float64 `json:"t_sec"`
	Idx      int     `json:"idx"`
	Slot     int     `json:"slot"`
	Channel  string  `json:"channel"`
	Kind     string  `json:"kind"`
	Text     string  `json:"text"`
	Org      string  `json:"org,omitempty"`
	MSOrg    string  `json:"ms_org,omitempty"`
	MS       string  `json:"ms,omitempty"`
	MHz      float64 `json:"mhz,omitempty"`
	ToSlot   int     `json:"to_slot"`
	Scramble string  `json:"scramble,omitempty"`
	Ref      int     `json:"ref"`
	Calling  string  `json:"calling,omitempty"`
	Called   string  `json:"called,omitempty"`
	Display  string  `json:"display,omitempty"`
	Cause    string  `json:"cause,omitempty"`
	Codec    string  `json:"codec,omitempty"`
	Mode string `json:"mode,omitempty"`
	ID   int    `json:"id"`
}

type NetInfo struct {
	Idx       int       `json:"idx"`
	TSec      float64   `json:"t_sec"`
	MHz       float64   `json:"mhz"`
	CC        int       `json:"cc"`
	Org       string    `json:"org"`
	Area      int       `json:"area"`
	System    string    `json:"system"`
	Maker     int       `json:"maker"`
	Zone      int       `json:"zone"`
	Base      int       `json:"base"`
	Control   []float64 `json:"control"`
	Neighbors []float64 `json:"neighbors"`
	Rows      []Row     `json:"rows"`
	Count     int       `json:"count"`
}

type ConstInfo struct {
	TSec   float64   `json:"t_sec"`
	Points []float32 `json:"points"`
}


type seq struct{ n atomic.Int64 }

func (s *seq) next() int64 { return s.n.Add(1) }


type l1State struct {
	key   string
	rec   *L1Rec
	sentT float64
}

const (
	l1UpdateSec = 1.0
	l1NoiseSec  = 5.0
	constStep   = 4
	constGroup  = 160
)

func (c *Channel) emitL1(s *decoder.Slot) {
	st := &c.l1s[s.Num]
	all := c.std.l1All.Load() && c.isSelected()
	unit := s.SACCH != nil || s.FACCH != nil || s.SB != nil || s.RCH != nil || (s.Up && s.CAC != nil)
	key := l1Key(s)
	noise := !all && c.unconfirmed(s)
	if noise {
		key = "未確認"
	}
	why := ""
	switch {
	case all:
		why = "全スロット"
	case st.rec == nil || key != st.key:
		why = "変化"
	case unit:
		why = "ユニット"
	}
	if why == "" {
		st.rec.Repeat++
		st.rec.TSec = s.TSec
		every := l1UpdateSec
		if noise {
			every = l1NoiseSec
		}
		if s.TSec-st.sentT >= every {
			st.sentT = s.TSec
			r := *st.rec
			c.sink.Emit(Event{Type: EvL1, L1: &r})
		}
		return
	}
	r := &L1Rec{No: c.std.no.next(), TSec: s.TSec, Idx: c.cfg.ID, Slot: s.Num, Frame: s.Frame, SW: s.SW.Name(),
		Kind: slotKind(s), Why: why, Info: l1Info(s, c.makerNum()), Repeat: 1, Tree: l1Tree(s, c.makerNum()), RawHex: hexNoSpace(l1.ToBytes(s.Hard))}
	st.key, st.rec, st.sentT = key, r, s.TSec
	cp := *r
	c.sink.Emit(Event{Type: EvL1, L1: &cp})
}

func (c *Channel) unconfirmed(s *decoder.Slot) bool {
	if s.Direct != nil || c.calls[s.Num].on {
		return false
	}
	return !unitOK(s)
}

func unitOK(s *decoder.Slot) bool {
	ok := func(u *l1.Unit) bool { return u != nil && u.OK }
	return ok(s.CAC) || ok(s.SACCH) || ok(s.FACCH) || (s.RCH != nil && s.RCH.OK) || (s.SB != nil && s.SB.OK) ||
		(s.DSB != nil && s.DSB.OK) || (s.PICH != nil && s.PICH.OK)
}

func slotKind(s *decoder.Slot) string {
	switch {
	case s.SW.Kind == l1.SWNone:
		return "慣性"
	case s.Control:
		return "制御"
	case s.SW.Kind == l1.SWBurst:
		return "同期バースト"
	case s.SW.Kind == l1.SWIdle:
		return "空線"
	case s.SW.Inverted:
		return "FACCH"
	}
	return "音声"
}

func okText(ok bool) string {
	if ok {
		return "OK"
	}
	return "NG"
}

func l1Key(s *decoder.Slot) string {
	k := fmt.Sprintf("%s/%s/%d/%d/%x/%v", s.SW.Name(), slotKind(s), s.CI, s.CC, s.Init, s.InitKnown)
	if s.CAC != nil {
		k += "/cac" + okText(s.CAC.OK)
	}
	return k
}

func scrText(init int, known bool) string {
	switch {
	case !known:
		return "未確定"
	case init == l1.InitAllOnes:
		return "オール 1"
	}
	return fmt.Sprintf("基地局指定 0x%02X", l1.PatternOf(init))
}

func logicalCh(s *decoder.Slot, ch string, up bool) string {
	if ch != "CAC" {
		return ch
	}
	if up {
		return "SCCH"
	}
	return s.CACCh.String()
}

func amble(h []uint8, a, b int) string {
	if b > len(h) || a >= b {
		return ""
	}
	pat := [4]uint8{1, 0, 0, 1}
	e := 0
	for i := a; i < b; i++ {
		if h[i] != pat[(i-a)%4] {
			e++
		}
	}
	return fmt.Sprintf("パターンとの違い %d / %d ビット", e, b-a)
}

func ccNote(s *decoder.Slot) string {
	if s.CCDetected < 0 {
		return "カラーコード（検出 CC はまだ。§4.1.12）"
	}
	return fmt.Sprintf("カラーコード（検出 CC %d。§4.1.12: 連続 7 スロットで 4 回以上か 4 回連続）", s.CCDetected)
}

func cacName(s *decoder.Slot) string {
	if s.CACCh != l1.CACUnknown {
		return "CAC/" + s.CACCh.String()
	}
	return "CAC"
}

func l1Info(s *decoder.Slot, maker int) string {
	p := []string{fmt.Sprintf("%s %s CI %02b CC %d", s.SW.Name(), slotKind(s), s.CI, s.CC)}
	if s.Up {
		p = []string{fmt.Sprintf("上り %s %s CC %d %+.0f Hz +%.0f dB 位相 %+.1f シンボル", upSWName(s.SW), slotKind(s), s.CC, s.CFOHz, s.LevelDB, s.TimingSym)}
	}
	if d := s.Direct; d != nil {
		p = []string{fmt.Sprintf("%s CH%d %s %s %+.0f Hz +%.0f dB", d.Mode, s.Num+1, d.Name(), d.Kind(false), s.CFOHz, s.LevelDB)}
		if sb := s.DSB; sb != nil {
			p = append(p, fmt.Sprintf("SB %s BI %d IC %d ID %d", okText(sb.OK), sb.BI, sb.IC, sb.Number()))
		}
		if pc := s.PICH; pc != nil {
			p = append(p, "PICH "+pc.String())
		}
	}
	if s.SFPos >= 0 {
		p = append(p, fmt.Sprintf("SF %d", s.SFPos))
	}
	if s.CCDetected >= 0 && s.CCDetected != s.CC {
		p = append(p, fmt.Sprintf("検出 CC %d", s.CCDetected))
	}
	if u := s.CAC; u != nil {
		p = append(p, fmt.Sprintf("%s %s W %s 訂正 %d", cacName(s), okText(u.OK), u.W, u.Corrected))
	}
	if s.SACCH != nil {
		p = append(p, "SACCH 完結 "+s.SACCH.W.String())
	}
	if r := s.RCH; r != nil && r.OK {
		p = append(p, "RCH "+l1.RCHVendorText(maker, r.Info, s.Up))
	}
	if s.FACCH != nil {
		p = append(p, "FACCH 完結 "+s.FACCH.W.String())
	}
	if sb := s.SB; sb != nil {
		p = append(p, fmt.Sprintf("同期バースト %s ID %d", okText(sb.OK), sb.ID))
	}
	if !s.Control && s.SW.Kind != l1.SWNone && s.SW.Kind != l1.SWBurst {
		p = append(p, "スクランブル "+scrText(s.Init, s.InitKnown))
	}
	return strings.Join(p, "　")
}

func bitsOf(h []uint8, a, b int) (bits string, hexs string) {
	if len(h) < b {
		return "", ""
	}
	if b-a <= 32 {
		var sb strings.Builder
		for _, x := range h[a:b] {
			sb.WriteByte('0' + x)
		}
		return sb.String(), ""
	}
	return "", hexNoSpace(l1.ToBytes(h[a:b]))
}

func field(h []uint8, label string, a, b int, value, note string) Node {
	bits, hx := bitsOf(h, a, b)
	n := Node{Label: label, Value: value, Bits: bits, Range: fmt.Sprintf("%d–%d", a, b-1), Note: note}
	if hx != "" && value == "" {
		n.Value = hx
	}
	return n
}

func l1Tree(s *decoder.Slot, maker int) []Node {
	if s.Direct != nil {
		return l1TreeDirect(s)
	}
	if s.Up {
		return l1TreeUp(s, maker)
	}
	h := s.Hard
	head := Node{Label: "スロット", Value: fmt.Sprintf("ST#%d・無線フレーム #%d", s.Num, s.Frame), Children: []Node{
		{Label: "同期ワード", Value: fmt.Sprintf("%s（%s・誤り %d）", s.SW.Name(), s.SW.Kind, s.SW.Errs)},
		{Label: "種別", Value: slotKind(s)},
		{Label: "スーパーフレーム位置", Value: sfText(s.SFPos)},
		{Label: "差動位相誤差", Value: fmt.Sprintf("%.1f°", s.PhaseErrDeg)},
		{Label: "フレームの位相からのずれ", Value: fmt.Sprintf("%+.2f シンボル（正 = 遅い）", s.TimingSym),
			Note: "このキャリアの上りの共通の位相（ST#n の頭 = 位相 + 10 ms·n）から。受信点は基地局でないので標準タイミング（§4.1.17）からのずれではないが、TA の指定（§4.1.18）の前後で動く"},
	}}
	var f []Node
	switch {
	case s.SW.Kind == l1.SWBurst:
		f = []Node{
			field(h, "R", 0, 6, "", "バースト過渡応答用ガード"),
			field(h, "P", 6, l1.SSPos, amble(h, 6, l1.SSPos), "プリアンブル 1001…10（表 4.1.7-2）"),
			field(h, "SW（32）", l1.SSPos, l1.SWEnd, s.SW.Name(), ""),
			field(h, "パラメータ部", l1.SWEnd, l1.SWEnd+104, "", "畳込み + 13×8 インタリーブ（スクランブルしない）"),
			field(h, "Post", l1.SWEnd+104, l1.SlotBits, amble(h, l1.SWEnd+104, l1.SlotBits), "ポストアンブル（プリアンブルと同様なパターンの繰り返し。§4.1.7.3）"),
		}
	case s.Control:
		f = []Node{
			field(h, "R", 0, 6, "", ""), field(h, "P", 6, 8, amble(h, 6, 8), "プリアンブル 10（表 4.1.7-2）"),
			field(h, "CAC/E（前）", 8, l1.SWPos, "", "スクランブル S0〜S111"),
			field(h, "SW", l1.SWPos, l1.SWEnd, s.SW.Name(), ""),
			field(h, "CI", l1.SWEnd, l1.SWEnd+2, ciText(s.CI), ""),
			field(h, "CC", l1.SWEnd+2, l1.SWEnd+8, fmt.Sprint(s.CC), ccNote(s)),
			field(h, "CAC/E（後）", l1.SWEnd+8, 316, "", "スクランブル S112〜S279"),
			field(h, "I", 316, l1.SlotBits, "", "アイドル"),
		}
	default:
		f = []Node{
			field(h, "R", 0, 6, "", ""), field(h, "P", 6, 8, amble(h, 6, 8), "プリアンブル 10（表 4.1.7-2）"),
			field(h, "TCH（前 112）", 8, l1.SWPos, "", "FACCH のときは偶数番 / 奇数番が FACCH"),
			field(h, "SW", l1.SWPos, l1.SWEnd, s.SW.Name(), ""),
			field(h, "CI", l1.SWEnd, l1.SWEnd+2, ciText(s.CI), ""),
			field(h, "CC", l1.SWEnd+2, l1.SWEnd+8, fmt.Sprint(s.CC), ccNote(s)),
			field(h, "SACCH/RCH", l1.SWEnd+8, 168, sacchPos(s.SFPos), ""),
			field(h, "TCH（後 144）", 168, 312, "", ""),
			field(h, "B/I", 312, l1.SlotBits, biText(s.BI), "ビジー／アイドル（表 4.1.7-4。デスクランブル後の 8 ビットの多数決）"),
		}
	}
	tree := []Node{head, {Label: "フィールド（スクランブル前の硬判定）", Children: f}}
	if !s.Control && s.SW.Kind != l1.SWNone && s.SW.Kind != l1.SWBurst {
		tree = append(tree, Node{Label: "下りスクランブル", Value: scrText(s.Init, s.InitKnown), Note: "PN(9,5)。オール 1 か基地局指定の 8 ビット（§4.1.11）"})
	}
	if u := s.CAC; u != nil {
		ex := []Node{{Label: "S（SF 位置）", Value: fmt.Sprint(u.SFPos())}}
		if s.CACCh != l1.CACUnknown {
			ex = append(ex, Node{Label: "機能チャネル（§4.1.13.5）", Value: s.CACCh.String(), Note: "報知情報の制御チャネル構造情報（AB・AS1・AP・AS2）と SF 位置から"})
		}
		if e, ok := u.E(); ok {
			ex = append(ex, Node{Label: "E（衝突制御 §4.1.19.2）", Value: e.String(), Bits: bitsString(u.Extra[5:]),
				Note: "I/B 1 = 上りの送信許可・R/N 1 = 基地局が受信・PE = 基地局が受けた上りの CAC の CRC16"})
		}
		tree = append(tree, unitNode("CAC（20×14 デインタリーブ → ビタビ 140）", u, ex))
	}
	if r := s.RCH; r != nil {
		tree = append(tree, rchNode(r, false, maker))
	}
	if u := s.SACCH; u != nil {
		tree = append(tree, unitNode("SACCH（8 スロットで完結。16×10 → ビタビ 80）", u, nil))
	}
	if u := s.FACCH; u != nil {
		tree = append(tree, unitNode("FACCH（2 スロットで完結。付属資料 B → ビタビ 128）", u, nil))
	}
	if sb := s.SB; sb != nil {
		tree = append(tree, Node{Label: "同期バースト（13×8 → ビタビ 52 → CRC6）", Value: okText(sb.OK), Children: []Node{
			{Label: "キャリア情報", Value: ciText(sb.CI)}, {Label: "カラーコード", Value: fmt.Sprint(sb.CC)},
			{Label: "バースト識別 B", Value: map[int]string{0: "0 初期同期（SB1・SB2）", 1: "1 完了同期（SB3・SB4）"}[sb.B]},
			{Label: "タイムアライメント", Value: sbTAText(sb)}, {Label: "スーパーフレーム同期カウンタ", Value: sbSSCText(sb)},
			{Label: "呼出符号", Value: fmt.Sprintf("%d（%s。下詰め＝付属資料 D.3）", sb.ID, numbering.CallSign(sb.ID).Text())},
			{Label: "CRC6 の初期値", Value: fmt.Sprintf("0x%02X", sb.CRCInit)}, {Label: "訂正数", Value: fmt.Sprint(sb.Corrected)},
		}})
	}
	return tree
}

func l1TreeUp(s *decoder.Slot, maker int) []Node {
	h := s.Hard
	head := Node{Label: "スロット（上り）", Value: fmt.Sprintf("ST#%d・無線フレーム #%d", s.Num, s.Frame), Children: []Node{
		{Label: "同期ワード", Value: fmt.Sprintf("%s（%s・誤り %d。上りは下りの語のビット反転、FACCH を含めば反転を重ねて下りと同じ極性）",
			upSWName(s.SW), s.SW.Kind, s.SW.Errs)},
		{Label: "種別", Value: slotKind(s)},
		{Label: "スーパーフレーム位置", Value: sfText(s.SFPos)},
		{Label: "搬送波のずれ", Value: fmt.Sprintf("%+.0f Hz（バーストの同期ワードと差動 4 乗から）", s.CFOHz)},
		{Label: "レベル", Value: fmt.Sprintf("雑音の床 +%.1f dB", s.LevelDB)},
		{Label: "差動位相誤差", Value: fmt.Sprintf("%.1f°", s.PhaseErrDeg)},
	}}
	var f []Node
	switch {
	case s.SW.Kind == l1.SWBurst:
		f = []Node{
			field(h, "G", 0, 50, "", "ガード時間"),
			field(h, "LP", 50, 86, "", "リニアライザ用プリアンブル（中身は規定しない）"),
			field(h, "R", 86, 92, "", "バースト過渡応答用ガード"),
			field(h, "P", 92, l1.UpSSPos, amble(h, 92, l1.UpSSPos), "プリアンブル 1001…（表 4.1.7-2）"),
			field(h, "SW（32）", l1.UpSSPos, l1.UpSWEnd, upSWName(s.SW), ""),
			field(h, "パラメータ部", l1.UpSWEnd, l1.UpSWEnd+104, "", "畳込み + 13×8 インタリーブ（スクランブルしない）"),
			field(h, "G", l1.UpSWEnd+104, l1.SlotBits, "", "ガード時間"),
		}
	case s.Control && s.UpFirst:
		f = []Node{
			field(h, "LP", 0, 36, "", "リニアライザ用プリアンブル"), field(h, "R", 36, 42, "", ""),
			field(h, "P", 42, 98, amble(h, 42, 98), "プリアンブル 1001 の繰り返し（表 4.1.7-2）"),
			field(h, "CAC（前 58）", 98, l1.UpSWPos, "", "スクランブル S0〜S57（カラーコード由来）"),
			field(h, "SW", l1.UpSWPos, l1.UpSWEnd, upSWName(s.SW), ""),
			field(h, "I", l1.UpSWEnd, l1.UpSWEnd+2, "", "アイドル（常に 00）"),
			field(h, "CC", l1.UpSWEnd+2, l1.UpSWEnd+8, fmt.Sprint(s.CC), ccNote(s)),
			field(h, "CAC（後 112）", l1.UpSWEnd+8, 296, "", "スクランブル S58〜S169"),
			field(h, "G", 296, l1.SlotBits, "", "ガード時間"),
		}
	case s.Control:
		f = []Node{
			field(h, "LP", 0, 36, "", "リニアライザ用プリアンブル"), field(h, "R", 36, 42, "", ""), field(h, "P", 42, 44, amble(h, 42, 44), "プリアンブル 10（表 4.1.7-2）"),
			field(h, "CAC（前 112）", 44, l1.UpSWPos, "", "スクランブル S0〜S111（カラーコード由来）"),
			field(h, "SW", l1.UpSWPos, l1.UpSWEnd, upSWName(s.SW), ""),
			field(h, "I", l1.UpSWEnd, l1.UpSWEnd+2, "", "アイドル（常に 00）"),
			field(h, "CC", l1.UpSWEnd+2, l1.UpSWEnd+8, fmt.Sprint(s.CC), ccNote(s)),
			field(h, "CAC（後 112）", l1.UpSWEnd+8, 296, "", "スクランブル S112〜S223"),
			field(h, "G", 296, l1.SlotBits, "", "ガード時間"),
		}
	default:
		f = []Node{
			field(h, "R", 0, 6, "", ""), field(h, "P", 6, 8, amble(h, 6, 8), "プリアンブル 10（表 4.1.7-2）"),
			field(h, "TCH（前 148）", 8, l1.UpSWPos, "", "FACCH のときは偶数番 / 奇数番が FACCH"),
			field(h, "SW", l1.UpSWPos, l1.UpSWEnd, upSWName(s.SW), ""),
			field(h, "I", l1.UpSWEnd, l1.UpSWEnd+2, "", "アイドル（常に 00）"),
			field(h, "CC", l1.UpSWEnd+2, l1.UpSWEnd+8, fmt.Sprint(s.CC), ccNote(s)),
			field(h, "SACCH/RCH", l1.UpSWEnd+8, 204, sacchPos(s.SFPos), ""),
			field(h, "TCH（後 108）", 204, 312, "", ""),
			field(h, "G", 312, l1.SlotBits, "", "ガード時間"),
		}
	}
	tree := []Node{head, {Label: "フィールド（スクランブル前の硬判定）", Children: f}}
	if !s.Control && s.SW.Kind != l1.SWBurst {
		tree = append(tree, Node{Label: "上りスクランブル", Value: scrText(s.Init, s.InitKnown),
			Note: "PN(9,5)。呼接続通信は基地局指定の 8 ビット、専用チャネル通信・折返通信はカラーコード由来（§4.1.11）"})
	}
	if u := s.CAC; u != nil {
		label := "CAC 第 2 ユニット以降（16×14 デインタリーブ → ビタビ 112）"
		if s.UpFirst {
			label = "CAC 第 1 ユニット（17×10 デインタリーブ → ビタビ 85）"
		}
		tree = append(tree, unitNode(label, u, nil))
	}
	if u := s.SACCH; u != nil {
		tree = append(tree, unitNode("SACCH（8 スロットで完結。16×10 → ビタビ 80）", u, nil))
	}
	if r := s.RCH; r != nil {
		tree = append(tree, rchNode(r, true, maker))
	}
	if u := s.FACCH; u != nil {
		tree = append(tree, unitNode("FACCH（2 スロットで完結。付属資料 B → ビタビ 128）", u, nil))
	}
	if sb := s.SB; sb != nil {
		tree = append(tree, Node{Label: "同期バースト（13×8 → ビタビ 52 → CRC6）", Value: okText(sb.OK), Children: []Node{
			{Label: "カラーコード", Value: fmt.Sprint(sb.CC)},
			{Label: "バースト識別 B", Value: map[int]string{0: "0 初期同期（SB2）", 1: "1 完了同期（SB4）"}[sb.B]},
			{Label: "タイムアライメント", Value: sbTAText(sb)}, {Label: "スーパーフレーム同期カウンタ", Value: sbSSCText(sb)},
			{Label: "呼出符号（送信している移動局）", Value: fmt.Sprintf("%d（%s。下詰め＝付属資料 D.3）", sb.ID, numbering.CallSign(sb.ID).Text())},
			{Label: "CRC6 の初期値", Value: fmt.Sprintf("0x%02X", sb.CRCInit)}, {Label: "訂正数", Value: fmt.Sprint(sb.Corrected)},
		}})
	}
	return tree
}

func biText(v int) string {
	switch v {
	case 1:
		return "ビジー（上りの送信権あり）"
	case 0:
		return "アイドル（上りは空き）"
	}
	return "不明（スクランブル未確定か揃わない）"
}

func sbTAText(sb *l1.SyncBurst) string {
	if sb.B == 0 {
		return fmt.Sprintf("%04b（B = 0 なので意味なし）", sb.TA)
	}
	return fmt.Sprintf("%04b %s", sb.TA, l1.TAText(sb.TA))
}

func sbSSCText(sb *l1.SyncBurst) string {
	if sb.B == 0 {
		return fmt.Sprintf("%d（B = 0 なので意味なし）", sb.SSC)
	}
	if sb.SSC <= 251 {
		return fmt.Sprintf("%d（SF 位置 %d・%d スーパーフレーム先）", sb.SSC, sb.SSC%l1.FramesPerSuper, sb.SSC/l1.FramesPerSuper)
	}
	return fmt.Sprintf("%d（範囲外。0〜251）", sb.SSC)
}

func upSWName(sw l1.SW) string {
	switch sw.Kind {
	case l1.SWNone:
		return "-"
	case l1.SWBurst:
		return fmt.Sprintf("¬SS%d", sw.N)
	}
	if sw.Inverted {
		return fmt.Sprintf("S%d", sw.N)
	}
	return fmt.Sprintf("¬S%d", sw.N)
}

func rchNode(r *l1.RCH, up bool, maker int) Node {
	return Node{Label: "RCH（2 スロットで完結。深さ 5 → BCH(8,4) × 5 → CRC6。§4.1.8.2 (4)）", Value: okText(r.OK), Children: []Node{
		{Label: "中身（図 4.1.14-1）", Value: l1.RCHVendorText(maker, r.Info, up)},
		{Label: "情報 14 ビット", Bits: bitsString(r.Info)},
		{Label: "BCH の訂正数", Value: fmt.Sprint(r.Corrected)},
		{Label: "CRC6 の初期値", Value: fmt.Sprintf("0x%02X", r.CRCInit)},
	}}
}

func unitNode(label string, u *l1.Unit, extra []Node) Node {
	kids := []Node{
		{Label: "W（信号構成情報）", Value: u.W.String()},
		{Label: "情報", Value: hexOctets(u.Bytes())},
		{Label: "CRC16", Value: okText(u.OK)},
		{Label: "N（固定 0）", Value: map[bool]string{true: "0（規定どおり）", false: "0 でない（規定外）"}[u.NZero()], Bits: bitsString(nBits(u))},
		{Label: "ビタビの訂正数", Value: fmt.Sprint(u.Corrected), Note: "符号語なら誤りの数。乱数なら符号ビットの半分近く"},
	}
	return Node{Label: label, Value: okText(u.OK), Children: append(kids, extra...)}
}

func nBits(u *l1.Unit) []uint8 {
	if len(u.Extra) == 3 {
		return u.Extra
	}
	return nil
}

func bitsString(b []uint8) string {
	var sb strings.Builder
	for _, x := range b {
		sb.WriteByte('0' + x)
	}
	return sb.String()
}

func sfText(p int) string {
	if p < 0 {
		return "不明（空線の間は印が無い）"
	}
	return fmt.Sprint(p)
}

func ciText(ci int) string {
	switch ci {
	case 0:
		return "00 制御キャリア"
	case 3:
		return "11 通信キャリア"
	}
	return fmt.Sprintf("%02b（未定義）", ci)
}

func sacchPos(sf int) string {
	u, k := l1.SACCHPart(sf)
	switch {
	case sf < 0:
		return "SF 位置不明"
	case u < 0:
		return fmt.Sprintf("RCH（SF %d）", sf)
	}
	return fmt.Sprintf("SACCH ユニット %d の %d/8", u+1, k+1)
}


type l2Fold struct {
	l2    *L2Rec
	l3    []*L3Rec
	sentT float64
}

const (
	foldSec   = 120.0
	resendSec = 5.0
)

func (c *Channel) emitLayers(s *decoder.Slot, ch string, b []byte, f l2.Frame, sig *l3.Signal) int64 {
	key := fmt.Sprintf("%d/%s/%d/%x", s.Num, ch, c.makerNum(), b)
	if fd, ok := c.folds[key]; ok && s.TSec-fd.l2.TSec < foldSec {
		fd.l2.Repeat++
		fd.l2.TSec = s.TSec
		for _, r := range fd.l3 {
			r.Repeat++
			r.TSec = s.TSec
		}
		if s.TSec-fd.sentT >= resendSec {
			fd.sentT = s.TSec
			c.sendFold(fd)
		}
		return fd.l2.No
	}
	r := &L2Rec{No: c.std.no.next(), TSec: s.TSec, Idx: c.cfg.ID, Slot: s.Num, Channel: c.chLabel(s, ch), DLCI: f.Addr.Key(),
		Control: f.Ctl.Name, State: "完結", Summary: f.Summary(), Err: f.Err, Repeat: 1, RawHex: hexNoSpace(b)}
	if m, ok := f.Addr.MSInfo(); ok {
		r.Org = m.System.Org
	}
	if f.Err != "" {
		r.State = "形式異常"
	}
	l2kids := make([]Node, 0, 16)
	for _, fd := range f.Fields() {
		l2kids = append(l2kids, Node{Label: fd.Name, Value: fd.Value})
	}
	ln, mode := c.linkNotes(s, ch, f)
	for _, n := range append(f.ChannelNotes(logicalCh(s, ch, c.up != nil)), ln...) {
		l2kids = append(l2kids, Node{Label: "規格の検査", Value: n})
		if r.Err == "" {
			r.Err = n
		}
	}
	if mode != "" {
		l2kids = append(l2kids, Node{Label: "データリンクの状態（推定）", Value: mode, Note: "見えた SABME・UA・DISC・DM・I から（§4.2.2.5.5）"})
	}
	r.Tree = []Node{{Label: "LAPDM", Value: f.Summary(), Children: l2kids}}
	fd := &l2Fold{l2: r, sentT: s.TSec}
	if sig != nil {
		r.Summary += "　" + sig.Summary()
		r.Tree = append(r.Tree, Node{Label: "相乗り表示部", Value: piggy(*sig), Note: sig.Err})
		for _, m := range sig.Messages {
			l3r := &L3Rec{No: c.std.no.next(), TSec: s.TSec, Idx: c.cfg.ID, Slot: s.Num, Channel: c.chLabel(s, ch), Entity: m.Entity, Name: m.Name,
				Summary: l3Summary(m), Err: m.Err, Repeat: 1, Tree: []Node{msgNode(m)}, RawHex: hexNoSpace(m.Raw), L2No: r.No}
			r.L3Nos = append(r.L3Nos, l3r.No)
			r.Tree = append(r.Tree, msgNode(m))
			fd.l3 = append(fd.l3, l3r)
		}
	}
	if c.folds == nil {
		c.folds = map[string]*l2Fold{}
	}
	c.folds[key] = fd
	if len(c.folds) > 256 {
		for k, v := range c.folds {
			if s.TSec-v.l2.TSec > foldSec {
				delete(c.folds, k)
			}
		}
	}
	c.sendFold(fd)
	return r.No
}

func (c *Channel) sendFold(fd *l2Fold) {
	r := *fd.l2
	c.sink.Emit(Event{Type: EvL2, L2: &r})
	for _, x := range fd.l3 {
		y := *x
		c.sink.Emit(Event{Type: EvL3, L3: &y})
	}
}

func msgNode(m l3.Message) Node {
	n := Node{Label: m.Entity, Value: fmt.Sprintf("0x%02X %s", m.Type, m.Name), Note: m.Err}
	for _, f := range m.Fields {
		n.Children = append(n.Children, Node{Label: f.Name, Value: f.Value})
	}
	return n
}

func l3Summary(m l3.Message) string {
	switch {
	case m.Assign != nil:
		return m.Assign.Text
	case m.Call != nil:
		return m.Call.Text
	case m.Paging != nil:
		return m.Paging.Text
	case m.Broadcast != nil && m.Broadcast.System != nil:
		return "団体 " + orDash(m.Broadcast.System.Org)
	}
	if len(m.Fields) > 0 {
		f := m.Fields[0]
		return f.Name + " " + f.Value
	}
	return ""
}


func (c *Channel) emitCall(r CallRec) {
	r.No = c.std.no.next()
	r.Idx = c.cfg.ID
	if r.Org == "" {
		r.Org = c.orgName()
	}
	c.sink.Emit(Event{Type: EvCall, Call: &r})
}

type netState struct {
	key   string
	sentT float64
	count int
}

func (c *Channel) emitNet(s *decoder.Slot, m l3.Message) {
	b := m.Broadcast
	ni := NetInfo{Idx: c.cfg.ID, TSec: s.TSec, MHz: c.cfg.MHz, CC: s.CC, Control: b.Control, Neighbors: b.Neighbors, Area: -1}
	if sy := b.System; sy != nil {
		ni.Org, ni.Area, ni.Maker, ni.Zone, ni.Base = sy.Org, sy.Area, sy.Maker, sy.Zone, sy.Base
		ni.System = fmt.Sprintf("0x%0*X", 2*sy.CodeLen, sy.Code)
		if sy.Org != "" {
			c.setOrg(sy.Org)
		}
		c.setMaker(sy.Maker, "網番号")
	}
	for _, f := range m.Fields {
		ni.Rows = append(ni.Rows, Row{"RT", f.Name, f.Value})
	}
	key := fmt.Sprintf("%v/%v/%v/%s", ni.Org, ni.Control, ni.Neighbors, ni.System)
	c.net.count++
	ni.Count = c.net.count
	if key == c.net.key && s.TSec-c.net.sentT < freshSec {
		return
	}
	c.net.key, c.net.sentT = key, s.TSec
	c.sink.Emit(Event{Type: EvNet, Net: &ni})
}

func (c *Channel) setOrg(org string) {
	c.mu.Lock()
	c.org = org
	c.mu.Unlock()
	c.seenOrg(org)
}

func (c *Channel) seenOrg(org string) {
	org = strings.TrimSuffix(strings.TrimSuffix(org, "（前回まで）"), "（指定元）")
	if org == "" {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, o := range c.orgs {
		if o == org {
			return
		}
	}
	c.orgs = append(c.orgs, org)
}

func (c *Channel) Orgs() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.orgs...)
}

func (c *Channel) orgName() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.org
}


func (c *Channel) emitConst() {
	var pts []complex64
	switch {
	case c.dec != nil:
		pts = c.dec.Const
		c.dec.Const = c.dec.Const[:0]
	case c.up != nil:
		pts = c.up.Const
		c.up.Const = c.up.Const[:0]
	}
	if !c.isSelected() {
		c.constBuf = c.constBuf[:0]
		return
	}
	for i := c.constPhase; i < len(pts); i += constStep {
		v := pts[i]
		c.constBuf = append(c.constBuf, real(v), imag(v))
		if len(c.constBuf) >= 2*constGroup {
			ci := ConstInfo{TSec: c.tNow(), Points: append([]float32(nil), c.constBuf...)}
			c.sink.Emit(Event{Type: EvConst, Const: &ci})
			c.constBuf = c.constBuf[:0]
		}
	}
	c.constPhase = (c.constPhase - len(pts)%constStep + constStep) % constStep
}

func (c *Channel) isSelected() bool { return int(c.std.selected.Load()) == c.cfg.ID }

func hexNoSpace(b []byte) string { return strings.ReplaceAll(hexOctets(b), " ", "") }

type stdShared struct {
	no       seq
	selected atomic.Int64
	l1All    atomic.Bool
	mu       sync.Mutex
	hand handoffBoard
	press pressBoard
	links linkBoard
	vendorOut vendorFile
}
