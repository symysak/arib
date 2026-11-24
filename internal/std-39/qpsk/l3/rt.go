package l3

import (
	"fmt"
	"strings"
)


type rtMsg struct {
	name  string
	dir   int
	ch    string
	table string
}

var rtMsgs = map[uint8]rtMsg{
	0x01: {"発信無線状態報告", 2, "SCCH", "表 4.3.5.2-2"},
	0x02: {"ページング", 1, "PCH", "表 4.3.5.2-3"},
	0x03: {"着信無線状態報告", 2, "SCCH", "表 4.3.5.2-4"},
	0x21: {"移動局解放", 1, "SCCH", "表 4.3.5.2-9"},
	0x43: {"無線チャネル指定", 1, "SCCH", "表 4.3.5.2-5"},
	0x61: {"切替先無線チャネル指定", 1, "SACCH/FACCH", "表 4.3.5.2-6"},
	0x62: {"システム情報", 1, "SACCH/FACCH", "表 4.3.5.2-7"},
	0x63: {"システム情報確認", 2, "SACCH/FACCH", "表 4.3.5.2-8"},
	0x71: {"切り戻し要求", 1, "SCCH", "表 4.3.5.2-17"},
	0x72: {"送信中再発呼", 2, "SCCH", "表 4.3.5.2-18"},
	0x73: {"受信中再発呼", 2, "SCCH", "表 4.3.5.2-19"},
	0x81: {"無線チャネル切断", 1, "FACCH", "表 4.3.5.2-10"},
	0x82: {"無線チャネル切断確認", 2, "FACCH", "表 4.3.5.2-11"},
	0xA1: {"報知情報", 1, "BCCH", "表 4.3.5.2-12"},
	0xA2: {"BCCH 受信", 1, "PCH", "表 4.3.5.2-15"},
	0xC1: {"送信権解放", 2, "SACCH/FACCH", "表 4.3.5.2-14"},
	0xC2: {"送信停止要求", 1, "SACCH/FACCH", "表 4.3.5.2-16"},
	0xFF: {"免許人固有情報", 0, "", "表 4.3.5.2-13"},
}

var rtClasses = []string{
	"通信の起動に関するメッセージ", "通信の解放に関するメッセージ", "チャネルの設定に関するメッセージ",
	"チャネルの設定中に関するメッセージ", "チャネル解放に関するメッセージ", "報知情報に関するメッセージ",
	"送信権に関するメッセージ",
}

var rtDirName = []string{"両方向", "下り（基地局→移動局）", "上り（移動局→基地局）"}

type rtParser struct {
	r    *reader
	m    *Message
	c    Ctx
	stop bool
}

func (p *rtParser) add(name, value string) { p.m.Fields = append(p.m.Fields, Field{name, value}) }

func (p *rtParser) note(format string, a ...any) {
	p.m.Notes = append(p.m.Notes, fmt.Sprintf(format, a...))
}

func (p *rtParser) ie(name, ref string, n int, mandatory bool) []byte {
	if p.stop {
		return nil
	}
	left := p.r.left()
	if left == 0 {
		p.stop = true
		if mandatory {
			p.note("必須の情報要素「%s」（§%s）が無い", name, ref)
			if p.m.Err == "" {
				p.m.Err = "必須の情報要素が無い: " + name
			}
		}
		return nil
	}
	if left < n {
		p.stop = true
		p.add(name+"（途中まで）", hexOf(p.r.rest()))
		p.note("情報要素「%s」（§%s）の途中で終わる（%d オクテットのうち %d）", name, ref, n, left)
		if p.m.Err == "" {
			p.m.Err = name + " が足りない"
		}
		return nil
	}
	return p.r.take(n, name)
}

func (p *rtParser) spare(name, bits string, v byte, width int) {
	p.add(fmt.Sprintf("%s 予備（ビット %s）", name, bits), fmt.Sprintf("%0*b", width, v))
}

func (p *rtParser) rangeNote(name, ref string, v, lo, hi int) {
	if v < lo || v > hi {
		p.note("%s（§%s）= %d が範囲（%d〜%d）の外", name, ref, v, lo, hi)
	}
}

func parseRT(b []byte, c Ctx) Message {
	m := Message{Entity: "RT", Raw: append([]byte(nil), b...)}
	r := &reader{b: m.Raw}
	t, ok := r.byte1("メッセージ種別")
	if !ok {
		m.Err = r.err
		m.Notes = append(m.Notes, "必須の情報要素「メッセージ種別」（§4.3.5.3.2）が無い")
		return m
	}
	m.Type = t
	p := &rtParser{r: r, m: &m, c: c}
	md, known := rtMsgs[t]
	cls := "予約"
	switch {
	case t == 0xFF:
		cls = "免許人固有情報"
	case int(t>>5) < len(rtClasses):
		cls = rtClasses[t>>5]
	}
	if !known {
		p.add("メッセージ種別", fmt.Sprintf("0x%02X（%s・予約）", t, cls))
		if rest := r.rest(); len(rest) > 0 {
			p.add("内容", hexOf(rest))
		}
		m.Err = fmt.Sprintf("予約のメッセージ種別 0x%02X", t)
		return m
	}
	m.Name = md.name
	ch := ""
	if md.ch != "" {
		ch = "・" + md.ch
	}
	p.add("メッセージ種別", fmt.Sprintf("0x%02X（%s / %s・%s%s）", t, cls, md.name, rtDirName[md.dir], ch))
	if (c.Dir == 1 || c.Dir == 2) && md.dir != 0 && c.Dir != md.dir {
		p.note("%s は%sのメッセージ（%s）だが%sで受けた", md.name, rtDirName[md.dir], md.table, rtDirName[c.Dir])
	}

	switch t {
	case 0x01:
		p.msType()
		p.level("受信レベル", "4.3.5.3.3.5", "自ゾーン受信レベル", false)
	case 0x02:
		pg := &Paging{Text: "着信識別番号なし"}
		if v, ok := p.pagingID(false); ok {
			pg.ID = fmt.Sprint(v)
			pg.Text = "着信識別番号 " + pg.ID
		}
		m.Paging = pg
	case 0x03:
		p.pagingID(true)
		p.msType()
		p.level("受信レベル", "4.3.5.3.3.5", "自ゾーン受信レベル", false)
	case 0x21, 0x71, 0x81:
		p.cause()
	case 0x43, 0x61:
		m.Assign = p.channelSet(t == 0x61)
	case 0x62:
		m.Broadcast = p.systemInfo()
	case 0x72, 0x73:
		p.level("受信レベル", "4.3.5.3.3.5", "自ゾーン受信レベル", false)
	case 0xA1:
		m.Broadcast = p.broadcastInfo()
	case 0xA2:
		p.bcchPeriod()
	case 0x63, 0x82, 0xC1, 0xC2:
	case 0xFF:
		p.networkID()
		if !p.stop {
			if rest := r.rest(); len(rest) > 0 {
				p.add("免許人固有情報", hexOf(rest))
			}
		}
	}
	if !p.stop {
		if rest := r.rest(); len(rest) > 0 {
			p.add("認識できない情報要素（無視）", hexOf(rest))
			p.note("%s に表に無い後続のオクテットが %d 個（§4.3.3.3 (2)・付属資料 R: 認識できない情報要素として無視）", md.table, len(rest))
		}
	}
	return m
}


func (p *rtParser) pagingID(mandatory bool) (int, bool) {
	b := p.ie("着信識別番号", "4.3.5.3.3.2", 1, mandatory)
	if b == nil {
		return 0, false
	}
	p.add("着信識別番号", fmt.Sprint(b[0]))
	return int(b[0]), true
}

func (p *rtParser) msType() {
	b := p.ie("移動局種別", "4.3.5.3.3.3", 3, true)
	if b == nil {
		return
	}
	p.add("移動局分類", pick([]string{"一般移動局", "優先移動局（VIP、災害対策用等）", "無線通信卓", "保守用移動局"}, int(b[0]>>6), "予約"))
	p.add("最大送信電力（移動局種別）", pickMap(map[int]string{1: "3.0W", 7: "2.0W", 6: "2.0W−4dB", 5: "2.0W−8dB"}, int(b[0]>>3)&7, "予約")+"（数値は暫定値）")
	p.add("移動局種類", pick([]string{1: "単信", 2: "複信", 3: "単信＋複信"}, int(b[0]&7), "予約"))
	p.add("移動局 Rev.", pick([]string{"RCR STD-39 準拠"}, int(b[1]>>5), "予約"))
	p.spare("移動局種別 オクテット 2", "5", b[1]>>4&1, 1)
	p.add("標準秘匿バージョン", pick([]string{"1"}, int(b[1]>>2)&3, "予約"))
	p.spare("移動局種別 オクテット 2", "2〜1", b[1]&3, 2)
	p.spare("移動局種別 オクテット 3", "8〜7", b[2]>>6, 2)
	p.add("免許人固有定義領域（移動局種別）", fmt.Sprintf("0x%02X", b[2]&0x3F))
}

func (p *rtParser) restriction() string {
	b := p.ie("規制情報", "4.3.5.3.3.4", 3, true)
	if b == nil {
		return ""
	}
	o1, o2, o3 := b[0], b[1], b[2]
	yn := func(v int, no, yes string) string {
		if v != 0 {
			return yes
		}
		return no
	}
	accReg := bit(o1, 5) != 0
	corr := bit(o1, 7) != 0
	p.add("自ゾーン選択可否", yn(bit(o1, 8), "自ゾーン選択可", "自ゾーン選択否"))
	p.add("ゾーン選択補正レベル有無", yn(bit(o1, 7), "ゾーン選択補正レベル無し", "ゾーン選択補正レベル有り"))
	p.add("自ゾーンアクセス可否", yn(bit(o1, 6), "自ゾーンアクセス可", "自ゾーンアクセス否"))
	p.add("自ゾーンアクセス規制有無", yn(bit(o1, 5), "規制無し", "規制有り"))
	cl := fmt.Sprintf("%ddB（とまり木チャネル受信レベル差）", 2*int(o1&0xF))
	if !corr {
		cl += "（ゾーン選択補正レベル無しなので無意味）"
	}
	p.add("ゾーン選択補正レベル", cl)
	ignore := ""
	if !accReg {
		ignore = "（自ゾーンアクセス規制無しなので無意味）"
	}
	p.add("一般移動局発信規制有無", yn(bit(o2, 8), "発信規制無し", "発信規制有り")+ignore)
	p.add("一般移動局位置登録規制有無", yn(bit(o2, 7), "位置登録規制無し", "位置登録規制有り")+ignore)
	p.add("優先移動局発信可否", yn(bit(o2, 6), "発信可", "発信否")+ignore)
	p.add("優先移動局位置登録可否", yn(bit(o2, 5), "位置登録可", "位置登録否")+ignore)
	acc := "規制無し"
	if v := int(o2 & 0xF); v != 0 {
		acc = fmt.Sprintf("720×%dms（規制解除有効周期）", 4*v)
	}
	p.add("アクセス周期間隔", acc+ignore)
	ctl := "制限なし"
	if v := int(o3 >> 5); v != 0 {
		ctl = fmt.Sprintf("%d秒", 30*v)
	}
	p.add("制御チャネル通信通信時限", ctl)
	lim := "制限なし"
	if v := int(o3 & 0x1F); v != 0 {
		lim = fmt.Sprintf("%d分", v)
	}
	p.add("通信時限", lim)

	var s []string
	if bit(o1, 8) != 0 {
		s = append(s, "自ゾーン選択否")
	}
	if bit(o1, 6) != 0 {
		s = append(s, "自ゾーンアクセス否")
	}
	if corr {
		s = append(s, fmt.Sprintf("補正 %ddB", 2*int(o1&0xF)))
	}
	if accReg {
		var r []string
		for _, f := range []struct {
			n    int
			text string
		}{{8, "一般発信"}, {7, "一般位置登録"}, {6, "優先発信"}, {5, "優先位置登録"}} {
			if bit(o2, f.n) != 0 {
				r = append(r, f.text)
			}
		}
		t := "アクセス規制"
		if len(r) > 0 {
			t += " " + strings.Join(r, "・")
		}
		if v := int(o2 & 0xF); v != 0 {
			t += fmt.Sprintf(" 周期 720×%dms", 4*v)
		}
		s = append(s, t)
	}
	if v := int(o3 & 0x1F); v != 0 {
		s = append(s, fmt.Sprintf("通信時限 %d分", v))
	}
	if v := int(o3 >> 5); v != 0 {
		s = append(s, fmt.Sprintf("制御CH通信時限 %d秒", 30*v))
	}
	if len(s) == 0 {
		return "規制なし"
	}
	return strings.Join(s, " / ")
}

func rtLevelText(v int) string {
	switch {
	case v < 38 || v > 124:
		return fmt.Sprintf("範囲外 (%d)", v)
	case v == 124:
		return "80dBμV以上 (124)"
	case v == 38:
		return "−5dBμV未満 (38)"
	}
	db := func(x int) string { return strings.Replace(fmt.Sprint(x), "-", "−", 1) }
	return fmt.Sprintf("%sdBμV以上 %sdBμV未満 (%d)", db(v-44), db(v-43), v)
}

func (p *rtParser) level(name, ref, remark string, dflt bool) {
	b := p.ie(name, ref, 1, true)
	if b == nil {
		return
	}
	v := int(b[0] & 0x7F)
	var t string
	if dflt && v == 0x7F {
		t = "移動局デフォルト値 (127)"
	} else {
		t = rtLevelText(v)
		p.rangeNote(name, ref, v, 38, 124)
	}
	if remark != "" {
		t += "（" + remark + "）"
	}
	p.add(name, t)
	p.spare(name, "8", b[0]>>7, 1)
}

func rtFreq(b []byte) (band, fc int) {
	return int(b[0] >> 4), int(b[0]&0xF)<<8 | int(b[1])
}

func (p *rtParser) freq(prefix, ref string, mandatory bool) (fc int, mhz float64, ok bool) {
	b := p.ie(prefix+"周波数コード", ref, 2, mandatory)
	if b == nil {
		return -1, 0, false
	}
	band, fc := rtFreq(b)
	bt := fmt.Sprintf("予約 (%04b)", band)
	if band == 0 {
		bt = "0000（Fo 000.00 MHz・Fb 000.000 MHz・Nc 00 = 周波数は未定）"
	}
	p.add(prefix+"周波数帯識別子", bt)
	t := fmt.Sprintf("Fc %d", fc)
	if p.c.FreqMHz != nil {
		mhz = p.c.FreqMHz(fc)
		if mhz != 0 {
			t += fmt.Sprintf("（基地局送信 %.4f MHz）", mhz)
		}
	}
	p.add(prefix+"周波数コード", t)
	return fc, mhz, true
}

var rtCauses = map[int]string{
	0x10: "正常切断", 0x18: "移動局未登録", 0x19: "無通信検出", 0x1C: "通信時限満了", 0x1D: "強制一斉指令", 0x1F: "その他の正常イベント",
	0x22: "利用可能チャネルなし", 0x26: "網障害", 0x29: "一時的障害（スケルチ断時）", 0x2A: "装置輻輳", 0x2C: "要求チャネル利用不可",
	0x2F: "その他のリソース使用不可クラス",
	0x61: "メッセージ種別未定義または未提供", 0x64: "情報要素内容無効", 0x65: "呼状態とメッセージ不一致", 0x66: "タイマ満了による回復",
	0x6F: "その他の手順誤りクラス（相乗り異常・CC 異常時）",
}

var rtCauseClass = map[int]string{1: "正常クラス", 2: "リソース使用不可クラス", 6: "手順誤りクラス"}

func (p *rtParser) cause() {
	b := p.ie("理由表示", "4.3.5.3.3.7", 1, true)
	if b == nil {
		return
	}
	p.add("生成源", pick([]string{"移動局自身", "中継網"}, int(b[0]>>7), "予約"))
	v := int(b[0] & 0x7F)
	t, ok := rtCauses[v]
	if !ok {
		t = "予約"
		if cl, ok := rtCauseClass[v>>4]; ok {
			t += "（" + cl + "）"
		}
	} else {
		t = "［" + rtCauseClass[v>>4] + "］" + t
	}
	p.add("理由表示", fmt.Sprintf("%s (0x%02X)", t, v))
}

func (p *rtParser) scramble(remark string) int {
	b := p.ie("スクランブルコード", "4.3.5.3.3.8", 1, true)
	if b == nil {
		return -1
	}
	p.add("スクランブルコード", fmt.Sprintf("0x%02X（S1〜S8 = ビット 1〜8、S0 = S1。%s）", b[0], remark))
	return int(b[0])
}

func (p *rtParser) ccs() *ChannelStructure {
	const ref = "4.3.5.3.3.9"
	b := p.ie("制御チャネル構造情報", ref, 3, true)
	if b == nil {
		return nil
	}
	cs := &ChannelStructure{
		Np: int(b[0] >> 4), Nc: int(b[0] & 0xF),
		AB: int(b[1] >> 4), AP: int(b[1] & 0xF),
		AS2: int(b[2] >> 4), AS1: int(b[2] & 0xF),
	}
	p.add("Np（制御チャネル当たりの移動局群分け数）", fmt.Sprint(cs.Np))
	p.add("Nc（ゾーン内制御チャネルコード数）", fmt.Sprint(cs.Nc))
	p.add("Ab（制御チャネル当たりの BCCH スロット数）", fmt.Sprint(cs.AB))
	p.add("Ap（1 群あたりの PCH スロット数）", fmt.Sprint(cs.AP))
	p.add("As2（PCH 後の SCCH スロット数）", fmt.Sprint(cs.AS2))
	p.add("As1（BCCH 後の SCCH スロット数）", fmt.Sprint(cs.AS1))
	p.rangeNote("制御チャネル構造情報の Nc", ref, cs.Nc, 1, 10)
	p.rangeNote("制御チャネル構造情報の Np", ref, cs.Np, 1, 15)
	p.rangeNote("制御チャネル構造情報の Ab", ref, cs.AB, 1, 15)
	p.rangeNote("制御チャネル構造情報の Ap", ref, cs.AP, 1, 15)
	for k := 1; k <= cs.Nc; k++ {
		fc, _, ok := p.freq(fmt.Sprintf("F%d ", k), ref, true)
		if !ok {
			return cs
		}
		s1, _, ok := p.slot(fmt.Sprintf("S%d ", k), ref)
		if !ok {
			return cs
		}
		cs.FC = append(cs.FC, fc)
		cs.Slots = append(cs.Slots, s1)
	}
	return cs
}

func (p *rtParser) perchCount() (int, bool) {
	const name, ref = "周辺ゾーン判定用とまり木チャネル数", "4.3.5.3.3.10"
	b := p.ie(name, ref, 1, true)
	if b == nil {
		return 0, false
	}
	n := int(b[0] & 0x1F)
	p.add(name, fmt.Sprint(n))
	p.spare(name, "8〜6", b[0]>>5, 3)
	p.rangeNote(name, ref, n, 0, 20)
	return n, true
}

func (p *rtParser) perchNo(name string, mandatory bool) (int, bool) {
	b := p.ie(name, "4.3.5.3.3.11", 1, mandatory)
	if b == nil {
		return -1, false
	}
	p.add(name, fmt.Sprint(b[0]))
	return int(b[0]), true
}

func (p *rtParser) perchList() []int {
	n, ok := p.perchCount()
	if !ok {
		return nil
	}
	list := []int{}
	for k := 1; k <= n; k++ {
		if !p.stop && p.r.left() == 0 {
			p.note("周辺ゾーン判定用とまり木チャネル数 %d に対してとまり木チャネル番号が %d 個しかない", n, k-1)
			if p.m.Err == "" {
				p.m.Err = "とまり木チャネル番号が足りない"
			}
		}
		v, ok := p.perchNo(fmt.Sprintf("とまり木チャネル番号（第 %d 番目）", k), false)
		if !ok {
			break
		}
		list = append(list, v)
	}
	return list
}

func rtSlotNibble(v byte) (int, string) {
	switch v {
	case 0:
		return -1, "スロット無指定"
	case 1, 2, 4, 8:
		n := map[byte]int{1: 0, 2: 1, 4: 2, 8: 3}[v]
		return n, fmt.Sprintf("スロット %d", n)
	}
	return -2, fmt.Sprintf("予約 (%04b)", v)
}

func (p *rtParser) slot(prefix, ref string) (first, second int, ok bool) {
	b := p.ie(prefix+"スロット番号", ref, 1, true)
	if b == nil {
		return -1, -1, false
	}
	first, t1 := rtSlotNibble(b[0] & 0xF)
	second, t2 := rtSlotNibble(b[0] >> 4)
	p.add(prefix+"第 1 スロット番号", t1)
	p.add(prefix+"第 2 スロット番号", t2)
	if second >= 0 {
		if first < 0 {
			p.note("%sスロット番号: 第 1 スロット番号が無指定なのに第 2 スロット番号がある（図 4.3.5.3.3-11 注 1）", prefix)
		} else if d := (second - first + 4) % 4; d != 1 && d != 3 {
			p.note("%sスロット番号: 第 1・第 2 スロットが連続していない（図 4.3.5.3.3-11 注 4）", prefix)
		}
	}
	return first, second, true
}

func rtPowerText(v int) string {
	if v == 0 {
		return "3.0W"
	}
	att := 4 * (8 - v)
	w2 := "2.0W"
	if att > 4 {
		w2 = fmt.Sprintf("2.0W−%ddB", att-4)
	}
	return fmt.Sprintf("3.0W−%ddB / %s", att, w2)
}

func (p *rtParser) power(control bool) {
	b := p.ie("移動局送信電力指定", "4.3.5.3.3.13", 1, true)
	if b == nil {
		return
	}
	use := "（通信チャネル用）"
	ini := rtPowerText(int(b[0]>>4) & 7)
	if control {
		use = "（制御チャネル用）"
		ini += "（制御チャネル用は意味を持たない）"
	}
	p.add("最大送信電力"+use, rtPowerText(int(b[0]&7)))
	p.add("初期送信電力"+use, ini)
	p.spare("移動局送信電力指定", "8", b[0]>>7, 1)
	p.spare("移動局送信電力指定", "4", b[0]>>3&1, 1)
}

func (p *rtParser) chanInfo() {
	b := p.ie("チャネル情報", "4.3.5.3.3.14", 1, true)
	if b == nil {
		return
	}
	p.add("物理チャネル種別", pick([]string{"制御チャネル", "通信チャネル"}, bit(b[0], 1), "予約"))
	p.add("チャネル情報 ビット 2（予約）", fmt.Sprint(bit(b[0], 2)))
	p.add("秘匿種別", pick([]string{"免許人固有秘匿", "標準秘匿バージョン 1"}, int(b[0]>>2)&3, "予約"))
	p.add("免許人固有定義領域（チャネル情報）", fmt.Sprintf("0x%X", b[0]>>4))
}

func (p *rtParser) extension() {
	const name, ref = "拡張情報要素長", "4.3.5.3.3.17"
	b := p.ie(name, ref, 1, true)
	if b == nil {
		return
	}
	k := int(b[0] & 0x7F)
	p.add(name, fmt.Sprint(k))
	p.spare(name, "8", b[0]>>7, 1)
	p.rangeNote(name, ref, k, 0, 90)
	if k == 0 {
		return
	}
	if p.r.left() == 0 {
		p.note("拡張情報要素長 %d に対して拡張情報要素が無い", k)
		p.stop = true
		return
	}
	if e := p.ie("拡張情報要素", ref, k, false); e != nil {
		p.add("拡張情報要素（免許人オプション）", hexOf(e))
	}
}

func (p *rtParser) networkID() string {
	const cname, cref, ref = "網番号情報オクテット数", "4.3.5.3.3.24", "4.3.5.3.3.18"
	b := p.ie(cname, cref, 1, true)
	if b == nil {
		return ""
	}
	n := int(b[0] & 0xF)
	p.add(cname, fmt.Sprint(n))
	p.spare(cname, "8〜5", b[0]>>4, 4)
	p.rangeNote(cname, cref, n, 3, 8)
	if n < 2 {
		if n > 0 {
			if v := p.ie("網番号", ref, n, true); v != nil {
				p.add("網番号", hexOf(v))
			}
		}
		return ""
	}
	v := p.ie("網番号", ref, n, true)
	if v == nil {
		return ""
	}
	var sys uint64
	for _, o := range v[2:] {
		sys = sys<<8 | uint64(o)
	}
	p.add("制御ゾーン番号", fmt.Sprint(v[0]))
	p.add("基地局番号", fmt.Sprint(v[1]))
	if n > 2 {
		p.add("システム番号", fmt.Sprint(sys))
		if sys > 1<<44-1 {
			p.note("システム番号（§%s）= %d が 44 ビット（17592186044415）を越える", ref, sys)
		}
	}
	return fmt.Sprintf("制御ゾーン %d / 基地局 %d / システム %d", v[0], v[1], sys)
}

func (p *rtParser) bcchPeriod() {
	const name = "BCCH 受信区間長"
	b := p.ie(name, "4.3.5.3.3.19", 1, true)
	if b == nil {
		return
	}
	v := int(b[0] & 0xF)
	t := "BCCH の受信は不要"
	if v != 0 {
		t = fmt.Sprintf("%d スーパーフレームの区間受信", v)
	}
	p.add(name, t)
	p.spare(name, "8〜5", b[0]>>4, 4)
}

func (p *rtParser) lrTimer() {
	b := p.ie("位置登録タイマ", "4.3.5.3.3.20", 1, true)
	if b == nil {
		return
	}
	ini := int(b[0] & 7)
	it := fmt.Sprintf("%d分", ini)
	switch ini {
	case 0:
		it = "タイマ起動なし"
	case 7:
		it = "タイマ起動なし、即時位置登録"
	}
	p.add("初期タイマ値", it)
	rep := int(b[0] >> 3)
	rt := "タイマ起動なし"
	if rep != 0 {
		rt = fmt.Sprintf("%d分", 10*rep)
	}
	p.add("繰り返し周期タイマ値", rt)
}

func (p *rtParser) levelTime() {
	const name, ref = "レベル測定時間", "4.3.5.3.3.21"
	b := p.ie(name, ref, 1, true)
	if b == nil {
		return
	}
	p.add(name, fmt.Sprintf("%d フレーム（1 とまり木チャネル当たり）", b[0]))
	p.rangeNote(name, ref, int(b[0]), 1, 255)
}

func (p *rtParser) switchInterval() {
	const name = "チャネル切替間隔時間"
	b := p.ie(name, "4.3.5.3.3.23", 1, true)
	if b == nil {
		return
	}
	v := int(b[0] & 0xF)
	t := fmt.Sprintf("%d秒", 10*v)
	switch v {
	case 0:
		t = "移動局デフォルト値"
	case 15:
		t = "CH 切替禁止"
	}
	p.add(name, t)
	p.spare(name, "8〜5", b[0]>>4, 4)
}


func (p *rtParser) channelSet(handover bool) *Assignment {
	a := &Assignment{FC: -1, Slot: -1, Slot2: -1, Scramble: -1, Handover: handover}
	use := "通信チャネル用"
	if handover {
		use = "切替先通信チャネル用"
	}
	second := -1
	if fc, mhz, ok := p.freq("", "4.3.5.3.3.6", true); ok {
		a.FC, a.MHz = fc, mhz
	}
	if f, s, ok := p.slot("", "4.3.5.3.3.12"); ok {
		a.Slot, second = max(f, -1), s
		if a.Slot >= 0 {
			a.Slot2 = second
		}
	}
	a.Scramble = p.scramble(use)
	p.power(false)
	p.chanInfo()

	var t []string
	if handover {
		t = append(t, "切替先")
	}
	if a.FC >= 0 {
		f := fmt.Sprintf("Fc %d", a.FC)
		if a.MHz != 0 {
			f += fmt.Sprintf("（%.4f MHz）", a.MHz)
		}
		t = append(t, f)
	}
	if a.Slot >= 0 {
		s := fmt.Sprintf("スロット %d", a.Slot)
		if second >= 0 {
			s += fmt.Sprintf("+%d", second)
		}
		t = append(t, s)
	}
	if a.Scramble >= 0 {
		t = append(t, fmt.Sprintf("スクランブル 0x%02X", a.Scramble))
	}
	a.Text = strings.Join(t, " ")
	return a
}

func (p *rtParser) systemInfo() *Broadcast {
	bc := &Broadcast{}
	bc.Structure = p.ccs()
	p.perchNo("とまり木チャネル番号（自ゾーン）", true)
	bc.Restrict = p.restriction()
	p.power(true)
	p.levelTime()
	p.level("チャネル切替レベル", "4.3.5.3.3.22", "", true)
	p.switchInterval()
	bc.Network = p.networkID()
	bc.Perch = p.perchList()
	p.extension()
	return bc
}

func (p *rtParser) broadcastInfo() *Broadcast {
	bc := &Broadcast{}
	bc.Network = p.networkID()
	bc.Restrict = p.restriction()
	bc.Structure = p.ccs()
	p.power(true)
	p.level("待ち受け許可レベル", "4.3.5.3.3.15", "", false)
	p.level("待ち受け劣化レベル", "4.3.5.3.3.16", "周辺ゾーン判定用", false)
	bc.Perch = p.perchList()
	p.lrTimer()
	p.extension()
	return bc
}
