package server


import (
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/symysak/arib/internal/citycodes"
	"github.com/symysak/arib/internal/std-t79/decoder"
	"github.com/symysak/arib/internal/std-t79/l1"
	"github.com/symysak/arib/internal/std-t79/l3"
	"github.com/symysak/arib/internal/std-t79/numbering"
)

type DirectOpt struct {
	Mode l1.DirectMode
	Control bool
	Scramble int
	Auto bool
}

const tr149Sec = 30.0

func (c *Channel) callLost() float64 {
	if o := c.directOpt(); o != nil && o.Auto {
		return tr149Sec
	}
	return callLostSec
}

type DirectStation struct {
	MHz float64
	Opt DirectOpt
}

func (c *Channel) directOpt() *DirectOpt {
	if o, ok := c.cfg.Opt.(*DirectOpt); ok {
		return o
	}
	return nil
}

func (c *Channel) directKind() string {
	o := c.directOpt()
	if o == nil {
		return ""
	}
	if o.Mode != l1.ModeDirect {
		return o.Mode.String()
	}
	if o.Control {
		return "直接通信（制御用）"
	}
	return "直接通信"
}

func scdOrg(scd int) string {
	switch {
	case scd < 1000 || scd >= 48000:
		return ""
	case scd%1000 == 0:
		return numbering.PrefName(scd / 1000)
	}
	if name, ok := citycodes.Name(scd); ok {
		return name
	}
	return ""
}

func (c *Channel) noteDirect(s *decoder.Slot) (release bool) {
	n := s.Num
	cs := &c.calls[n]
	if sb := s.DSB; sb != nil && sb.OK {
		kind := "グループ番号"
		if sb.Individual() {
			kind = "個別番号"
		}
		who := "セレコール（宛先）"
		if sb.IC == 1 {
			who = "送信局 ID（話者）"
		}
		bi := "送信権獲得要求（BI = 1）"
		if sb.BI == 0 {
			bi = "アクセス許可・送信権解放（BI = 0）"
		}
		org := scdOrg(sb.SCD)
		text := fmt.Sprintf("%s %s %d / %s / 短縮システムコード %s", who, kind, sb.Number(), bi, numbering.AreaText(sb.SCD))
		if rt, ok := relayIDText(s.Direct.Mode, sb.IC, sb.ID); ok {
			text = fmt.Sprintf("%s / %s / 短縮システムコード %s", rt, bi, numbering.AreaText(sb.SCD))
		}
		if sb.I != 0 {
			text += fmt.Sprintf(" ※I = %03b（規定は 000）", sb.I)
		}
		if c.fresh(fmt.Sprintf("dsb/%d/%d/%d/%d", n, sb.ID, sb.BI, sb.IC), s.TSec) {
			c.emitCall(CallRec{TSec: s.TSec, Slot: n, Channel: "SB", Kind: "同期バースト（" + c.directKind() + "）", Text: text, ToSlot: -1, Ref: -1, ID: sb.ID})
		}
		c.note(n, fmt.Sprintf("dsb %d %s", s.Frame, text))
		if org != "" {
			c.seenOrg(org)
			c.mu.Lock()
			if c.org == "" || strings.HasSuffix(c.org, "（前回まで）") {
				c.org = org + "（短縮システムコード）"
			}
			c.mu.Unlock()
		}
		c.mu.Lock()
		if sb.IC == 1 {
			c.ids[n] = sb.ID
		}
		c.mu.Unlock()
		release = sb.BI == 0 && !(c.directOpt() != nil && c.directOpt().Auto)
	}
	if p := s.PICH; p != nil && p.OK {
		text := p.String()
		if rt, ok := relayIDText(s.Direct.Mode, p.IC, p.ID); ok {
			text = rt + map[int]string{1: " / BI 使用中", 0: " / BI 空き"}[p.BI]
		}
		if p.IC == 1 {
			cs.talker = p.ID
		} else {
			cs.selcall = p.ID
		}
		if text != cs.lastPICH {
			cs.lastPICH = text
			c.note(n, fmt.Sprintf("pich %d %s", s.Frame, text))
			if c.fresh(fmt.Sprintf("pich/%d/%d/%d", n, p.IC, p.ID), s.TSec) {
				c.emitCall(CallRec{TSec: s.TSec, Slot: n, Channel: "PICH", Kind: "PICH（" + c.directKind() + "）", Text: text, ToSlot: -1, Ref: -1, ID: p.ID})
			}
		}
		if p.IC == 1 {
			c.mu.Lock()
			c.ids[n] = p.ID
			c.mu.Unlock()
		}
		c.pairDuplex(n, s.TSec)
	}
	return release
}

func relayIDText(mode l1.DirectMode, ic, id int) (string, bool) {
	if mode == l1.ModeDirect {
		return "", false
	}
	num, ind := id&0x7FFFF, id>>19&1 == 1
	switch {
	case ic == 0 && mode == l1.ModeRelayDown:
		return fmt.Sprintf("専用チャネル通信識別番号 %d", num), true
	case ic == 1 && !ind && mode == l1.ModeRelayDown:
		return "短縮システムコード " + numbering.AreaText(num&0xFFFF), true
	case ic == 1 && ind:
		return fmt.Sprintf("専用チャネル送信局 ID %d（話者）", num), true
	}
	return fmt.Sprintf("IC %d・識別子 %d・%d（表 4.6.1.7-4・-6 に無い組）", ic, id>>19, num), true
}

func (c *Channel) pairDuplex(n int, t float64) {
	cs := &c.calls[n]
	if !cs.on || cs.dupWith >= 0 || cs.talker < 0 || cs.selcall < 0 {
		return
	}
	for m := range c.calls {
		o := &c.calls[m]
		if m == n || !o.on || o.talker != cs.selcall || o.selcall != cs.talker {
			continue
		}
		cs.dupWith, o.dupWith = m, n
		text := fmt.Sprintf("複信の対: CH%d（話者 %d）⇄ CH%d（話者 %d）", n+1, cs.talker&0x7FFFF, m+1, o.talker&0x7FFFF)
		c.note(n, "duplex_pair "+text)
		c.note(m, "duplex_pair "+text)
		c.emitCall(CallRec{TSec: t, Slot: n, Channel: "PICH", Kind: "直接通信 複信", Text: text, ToSlot: m, Ref: -1, ID: cs.talker})
		c.logf(n, "call", "info", "%s", text)
		return
	}
}

func (cs *callState) directPeer() string {
	num := func(id int) string {
		if id < 0 {
			return "-"
		}
		k := "G"
		if id>>19&1 == 1 {
			k = ""
		}
		return fmt.Sprintf("%s%d", k, id&0x7FFFF)
	}
	return fmt.Sprintf("話者 %s → 宛先 %s", num(cs.talker), num(cs.selcall))
}

func (m *Monitor) onDirect(mhz float64, scramble int, mode l1.DirectMode) {
	if !m.hasUp || mhz <= 0 {
		return
	}
	m.mu.Lock()
	for _, c := range m.chans {
		if c.isUp && math.Abs(c.MHz()-mhz) < matchMHz {
			m.mu.Unlock()
			return
		}
	}
	if m.added == nil {
		m.added = map[int64]bool{}
	}
	k := -int64(math.Round(mhz*1e4)) - 1e9
	if m.added[k] {
		m.mu.Unlock()
		return
	}
	m.added[k] = true
	m.mu.Unlock()
	opt := &DirectOpt{Mode: mode, Scramble: scramble, Auto: true}
	if err := m.eng.Add(mhz, opt); err != nil {
		m.addLog("info", "system", "直接通信の %.4f MHz は監視しない: %v", mhz, err)
		return
	}
	txt := "全 1"
	if scramble >= 0 {
		txt = fmt.Sprintf("0x%02X", scramble)
	}
	m.addLog("info", "call", "直接通信の通信用 %.4f MHz を監視に加えた（スクランブル %s）", mhz, txt)
}

func (c *Channel) directAct(s *decoder.Slot, m l3.Message) {
	d := m.Direct
	o := c.directOpt()
	if o != nil && !o.Control {
		o.Control = true
	}
	mode := l1.ModeDirect
	if o != nil {
		mode = o.Mode
	}
	var text string
	switch m.Type {
	case 0x44:
		c.dirScr = d.Scramble
		text = fmt.Sprintf("候補 %v・スクランブル %s", mhzList(d.MHz), scrPattern(d.Scramble))
	case 0x45:
		if d.OK != nil && *d.OK {
			text = "OK " + mhzList(d.MHz)
		} else {
			text = "NG（空きチャネル無し）"
		}
	case 0x47:
		text = fmt.Sprintf("%s・スクランブル %s", mhzList(d.MHz), scrPattern(d.Scramble))
	}
	if c.fresh(fmt.Sprintf("dir/%d/%s", m.Type, text), s.TSec) {
		c.logf(s.Num, "call", "info", "RT %s: %s", m.Name, text)
		c.emitCall(CallRec{TSec: s.TSec, Slot: s.Num, Channel: c.chLabel(s, "DCCH"), Kind: "RT " + m.Name, Text: text, ToSlot: -1, Ref: -1, ID: -1})
	}
	scr := d.Scramble
	if m.Type == 0x45 {
		scr = c.dirScr
	}
	if m.Type == 0x44 || len(d.MHz) == 0 || d.MHz[0] <= 0 || c.std.hooks == nil || c.std.hooks.direct == nil {
		return
	}
	c.std.hooks.direct(d.MHz[0], scr, mode)
}

func mhzList(v []float64) string {
	var s []string
	for _, f := range v {
		if f > 0 {
			s = append(s, fmt.Sprintf("%.4f MHz", f))
		} else {
			s = append(s, "（計算できない）")
		}
	}
	return strings.Join(s, " / ")
}

func scrPattern(p int) string {
	if p < 0 {
		return "無し"
	}
	return fmt.Sprintf("0x%02X", p)
}

func l1TreeDirect(s *decoder.Slot) []Node {
	h := s.Hard
	d := s.Direct
	head := Node{Label: "バースト（" + d.Mode.String() + "）", Value: fmt.Sprintf("CH%d（40 ms を法とした位相のまとまり）・フレーム #%d", s.Num+1, s.Frame), Children: []Node{
		{Label: "同期ワード", Value: fmt.Sprintf("%s（%s・誤り %d。表 4.5.1.7-2・-3 / 表 4.6.1.7-2・-3）", d.Name(), d.Kind(false), d.Errs)},
		{Label: "PICH のユニット", Value: map[bool]string{true: "先頭 P(1)", false: "非先頭 P(2)〜P(5)"}[d.First]},
		{Label: "搬送波のずれ", Value: fmt.Sprintf("%+.0f Hz", s.CFOHz)},
		{Label: "レベル", Value: fmt.Sprintf("雑音の床 +%.1f dB", s.LevelDB)},
		{Label: "差動位相誤差", Value: fmt.Sprintf("%.1f°", s.PhaseErrDeg)},
		{Label: "位相のまとまりからのずれ", Value: fmt.Sprintf("%+.2f シンボル", s.TimingSym)},
	}}
	var f []Node
	if sb := s.DSB; sb != nil {
		f = []Node{
			field(h, "G・LP・R・P", 0, l1.UpSSPos, "", "ガード・リニアライザ用プリアンブル（内容は規定しない）・R・プリアンブル"),
			field(h, "SW（32）", l1.UpSSPos, l1.UpSWEnd, d.Name(), "同期バーストの同期ワード（反転しない）"),
			field(h, "パラメータ", l1.UpSWEnd, l1.UpSWEnd+104, okText(sb.OK), "41 + CRC6 + 0×5 → 畳込み → 13 × 8（§4.1.8.3）"),
			field(h, "Post・G", l1.UpSWEnd+104, l1.SlotBits, "", "ポストアンブル・ガード"),
		}
		who := map[int]string{0: "セレコール（宛先）", 1: "送信局 ID（話者）"}[sb.IC]
		kind := "グループ番号"
		if sb.Individual() {
			kind = "個別番号"
		}
		f = append(f, Node{Label: "同期バースト（" + okText(sb.OK) + "）", Value: fmt.Sprintf("訂正 %d・CRC6 初期値 0x%02X", sb.Corrected, sb.CRCInit), Children: []Node{
			{Label: "I", Value: fmt.Sprintf("%03b（規定は 000）", sb.I)},
			{Label: "BI", Value: map[int]string{1: "1 = 使用中（送信権獲得要求）", 0: "0 = 空き（アクセス許可・送信権解放）"}[sb.BI], Note: "表 4.5.1.7-5"},
			{Label: "IC", Value: fmt.Sprintf("%d = %s", sb.IC, who), Note: "表 4.5.1.7-4"},
			{Label: "短縮システムコード", Value: numbering.AreaText(sb.SCD), Note: "図 2.5.1-3（都道府県 2 桁 + 市区町村 3 桁）"},
			{Label: "ID", Value: fmt.Sprintf("%s %d（識別子 %d）", kind, sb.Number(), sb.ID>>19)},
		}})
		return []Node{head, {Label: "フィールド", Children: f}}
	}
	f = []Node{
		field(h, "G・R・P", 0, 16, "", "ガード 8・R 6・P 2"),
		field(h, "データ（前 140）", 16, l1.UpSWPos, "", "TCH / DACCH・DCCH の 0〜139（スクランブル S0〜S139）"),
		field(h, "SW", l1.UpSWPos, l1.UpSWEnd, d.Name(), "同期ワード"),
		field(h, "PICH", l1.DPICHPos, l1.DPICHPos+l1.DPICHBits, "", "個別識別用チャネル（5 バーストで 1 ユニット。スクランブルなし）"),
		field(h, "データ（後 116）", l1.DPICHPos+l1.DPICHBits, 304, "", "TCH / DACCH・DCCH の 140〜255（スクランブル S140〜S255）"),
		field(h, "G", 304, l1.SlotBits, "", "ガード 16"),
	}
	scr := "全 1"
	if s.Init != l1.InitAllOnes {
		scr = fmt.Sprintf("指定パターン 0x%02X", l1.PatternOf(s.Init))
	}
	f = append(f, Node{Label: "スクランブル", Value: scr, Note: "PN(9,5)、データ 256 だけ。自動選択の通信用は DCCH で指定、それ以外は全 1（§4.5.1.11・§4.6.1.11）"})
	tree := []Node{head, {Label: "フィールド", Children: f}}
	if p := s.PICH; p != nil {
		kind := "グループ番号"
		if p.Individual() {
			kind = "個別番号"
		}
		tree = append(tree, Node{Label: "PICH（" + okText(p.OK) + "）", Value: p.String(), Children: []Node{
			{Label: "BI", Value: fmt.Sprint(p.BI)}, {Label: "IC", Value: fmt.Sprint(p.IC)},
			{Label: "ID", Value: fmt.Sprintf("%s %d", kind, p.Number())},
			{Label: "BCH(8,4) の訂正", Value: fmt.Sprint(p.Corrected)},
		}, Note: "22 + CRC6 → BCH(8,4) × 7 → 7 × 8 インタリーブ（§4.5.1.8.2 (3)）"})
	}
	if u := s.FACCH; u != nil {
		tree = append(tree, unitNode("DACCH / DCCH（前のバーストと 2 枚で 1 ユニット。付属資料 B）", u, nil))
	}
	return tree
}

const (
	directHitsToConvert = 10
	directMinGapSec     = 0.018
)

func (c *Channel) watchDirect() {
	if c.up == nil || c.toDirect || c.directOpt() != nil || !c.std.cfg.DirectScan {
		return
	}
	st := c.up.Stats
	if st.DirectSBOK < 2 {
		return
	}
	if st.DirectHits < directHitsToConvert || st.Bursts > st.DirectHits || st.IdleHits > 0 {
		return
	}
	if gap := (st.DirectLastT - st.DirectFirstT) / float64(st.DirectHits-1); gap < directMinGapSec {
		if !c.directLeak {
			c.directLeak = true
			c.logf(-1, "system", "info", "%.4f MHz の直接通信の語は %.1f ms おきで途切れない（下りのキャリアの漏れ・像とみる）ので直接通信の局にしない", c.cfg.MHz, gap*1000)
		}
		return
	}
	c.toDirect = true
	c.logf(-1, "system", "info", "%.4f MHz で直接通信の語（S9・S10・S11・SS1）のバーストを %d 回見た。直接通信の局として張り直す", c.cfg.MHz, st.DirectHits)
	if c.std.hooks != nil && c.std.hooks.toDirect != nil {
		c.std.hooks.toDirect(c.cfg.MHz)
	}
}

func (m *Monitor) onToDirect(mhz float64) {
	m.eng.Remove(mhz)
	go func() {
		for i := 0; i < 50; i++ {
			time.Sleep(100 * time.Millisecond)
			if !m.hasStation(mhz) {
				break
			}
		}
		if err := m.eng.Add(mhz, &DirectOpt{Mode: l1.ModeDirect, Scramble: -1}); err != nil {
			m.addLog("info", "system", "直接通信の %.4f MHz を張れない: %v", mhz, err)
			return
		}
		m.addLog("info", "call", "%.4f MHz を直接通信の局として監視する（走査で見つけた）", mhz)
	}()
}

func (m *Monitor) hasStation(mhz float64) bool {
	for _, c := range m.eng.Status().Channels {
		if math.Abs(c.MHz-mhz) < 1e-6 {
			return true
		}
	}
	return false
}
