package l3

import (
	"fmt"
	"strings"

	"github.com/symysak/arib/internal/std-t79/numbering"
)


var rtNames = map[uint8]string{
	0x01: "発信無線状態報告", 0x02: "ページング", 0x03: "着信無線状態報告", 0x04: "グループ通信呼出",
	0x21: "移動局解放",
	0x43: "無線チャネル指定",
	0x44: "直接通信無線チャネル要求", 0x45: "直接通信無線チャネル応答", 0x46: "直接通信無線チャネル応答確認", 0x47: "直接通信無線チャネル指定",
	0x62: "システム情報", 0x63: "システム情報確認",
	0x71: "切り戻し要求", 0x72: "送信中再発呼", 0x73: "受信中再発呼",
	0x81: "無線チャネル切断", 0x82: "無線チャネル切断確認",
	0xA1: "報知情報", 0xA2: "BCCH受信", 0xA3: "システム拡張情報", 0xA4: "SCCH情報通知",
	0xB1: "ショートメッセージ発信", 0xB2: "ショートメッセージ発信応答",
	0xB3: "ショートメッセージ着信", 0xB4: "ショートメッセージ着信応答",
	0xC1: "送信権解放", 0xC2: "送信停止要求",
	0xFC: "免許人固有情報4", 0xFD: "免許人固有情報3", 0xFE: "免許人固有情報2", 0xFF: "免許人固有情報",
}

type rtParser struct {
	r *reader
	m *Message
}

func (p *rtParser) add(name, value string) { p.m.Fields = append(p.m.Fields, Field{name, value}) }

func (p *rtParser) byte1(name string, f func(byte) string) (byte, bool) {
	v, ok := p.r.byte1(name)
	if !ok {
		return 0, false
	}
	p.add(name, f(v))
	return v, true
}

func parseRT(b []byte) Message {
	m := Message{Entity: "RT", Raw: b}
	r := &reader{b: b}
	t, ok := r.byte1("メッセージ種別")
	if !ok {
		m.Err = r.err
		return m
	}
	m.Type = t
	m.Name = rtNames[t]
	p := &rtParser{r: r, m: &m}
	switch t {
	case 0x01:
		p.msType()
		p.byte1("受信レベル", rtLevelText)
	case 0x02:
		pg := &Paging{Text: "着信識別番号なし"}
		if r.left() > 0 {
			if v, ok := p.byte1("着信識別番号", rtDec); ok {
				pg.ID = fmt.Sprint(v)
				pg.Text = "着信識別番号 " + pg.ID
			}
		}
		m.Paging = pg
	case 0x03:
		p.byte1("着信識別番号", rtDec)
		p.msType()
		p.byte1("受信レベル", rtLevelText)
	case 0x04:
		a := p.channelSet(true)
		if a != nil {
			m.Assign = a
			m.Paging = &Paging{Text: "グループ通信 " + a.Text}
		}
	case 0x21, 0x71, 0x81:
		p.byte1("理由表示", rtCauseText)
	case 0x43:
		m.Assign = p.channelSet(false)
	case 0x44:
		dc := &DirectChannel{Scramble: -1}
		v, ok := p.byte1("周波数候補数", func(v byte) string {
			t := fmt.Sprintf("%d", v&3)
			if v&3 == 0 {
				t += "（1〜3 の外）"
			}
			if v>>2 != 0 {
				t += fmt.Sprintf(" / 予約ビット 0x%02X", v>>2)
			}
			return t
		})
		for k := 1; ok && k <= int(v&3); k++ {
			dc.MHz = append(dc.MHz, p.directFreq(fmt.Sprintf("第 %d 候補周波数コード", k)))
		}
		dc.Scramble = p.directScramble()
		m.Direct = dc
	case 0x45:
		dc := &DirectChannel{Scramble: -1}
		if v, ok := p.byte1("候補周波数 OK/NG", func(v byte) string {
			if v&1 == 0 {
				return "OK"
			}
			return "NG（空きチャネル無し）"
		}); ok {
			okv := v&1 == 0
			dc.OK = &okv
			f := p.directFreq("周波数コード")
			if okv {
				dc.MHz = []float64{f}
			}
		}
		m.Direct = dc
	case 0x46:
	case 0x47:
		dc := &DirectChannel{Scramble: -1}
		dc.MHz = []float64{p.directFreq("周波数コード")}
		dc.Scramble = p.directScramble()
		m.Direct = dc
	case 0x62, 0xA1:
		p.broadcast()
	case 0x72, 0x73:
		p.byte1("受信レベル", rtLevelText)
	case 0x63, 0x82, 0xC1, 0xC2:
	case 0xA2:
		p.byte1("BCCH受信区間長", func(v byte) string { return rtPeriodText(v, "BCCH") })
		p.byte1("BCCH受信更新番号", rtDec)
		p.extension(true)
	case 0xA3:
		p.systemExpanded()
	case 0xA4:
		p.byte1("SCCH受信区間長", func(v byte) string { return rtPeriodText(v, "SCCH") })
		p.byte1("SCCH情報更新番号", rtDec)
		p.extension(true)
	case 0xB1, 0xB3:
		p.shortMessage(false)
	case 0xB2, 0xB4:
		p.shortMessage(true)
	case 0xFC, 0xFD, 0xFE, 0xFF:
		p.networkID()
		if rest := r.rest(); len(rest) > 0 {
			p.add("免許人固有情報", hexOf(rest))
			m.vendorRaw(fmt.Sprintf("RT 種別 0x%02X", t), rest)
		}
	default:
		m.Name = fmt.Sprintf("予約(0x%02X)", t)
		m.Err = fmt.Sprintf("未定義のメッセージ種別 0x%02X", t)
		rest := r.rest()
		if len(rest) > 0 {
			p.add("内容", hexOf(rest))
		}
		m.vendorRaw(fmt.Sprintf("RT 種別 0x%02X", t), rest)
		return m
	}
	if r.err != "" {
		m.Err = r.err
		return m
	}
	if rest := r.rest(); len(rest) > 0 {
		p.add("余剰", hexOf(rest))
	}
	return m
}

func rtDec(v byte) string { return fmt.Sprint(v) }


func (p *rtParser) msType() {
	b := p.r.take(3, "移動局種別")
	if b == nil {
		return
	}
	cls := pick([]string{"一般移動局", "優先移動局(VIP・災害対策用等)", "無線通信卓", "保守用移動局"}, int(b[0]>>6), "予約")
	pw := pick([]string{1: "3.0W以上", 2: "2.0W", 3: "1.0W"}, int(b[0]>>3)&7, "予約")
	kind := pick([]string{1: "単信", 2: "複信", 3: "単信+複信"}, int(b[0]&7), "予約")
	rev := pick([]string{"STD-T79 1.1版準拠", "STD-T79 2.0版以降準拠"}, int(b[1]>>5), "予約")
	sec := pick([]string{"1"}, int(b[1]>>2)&3, "予約")
	p.add("移動局分類", cls)
	p.add("最大送信電力", pw)
	p.add("移動局種類", kind)
	p.add("移動局Rev.", rev)
	p.add("標準秘匿バージョン", sec)
	p.add("免許人固有定義領域(移動局種別)", fmt.Sprintf("0x%02X", b[2]&0x7F))
}

func rtLevelText(v byte) string {
	v &= 0x7F
	switch {
	case v >= 124:
		return fmt.Sprintf("80dBμV以上 (%d)", v)
	case v <= 38:
		return fmt.Sprintf("−5dBμV未満 (%d)", v)
	}
	return fmt.Sprintf("%ddBμV (%d)", int(v)-44, v)
}

var rtBand = map[int]struct {
	Fo, Fb float64
	Nc     int
}{
	0: {271, -9, 320},
	1: {269, -9, 160},
}

func FreqMHz(band, fc int) float64 {
	bd, ok := rtBand[band]
	if !ok {
		return 0
	}
	return bd.Fo + 0.0125*float64(fc)
}

func rtFreq(b []byte) (band, fc int, mhz float64, text string) {
	band = int(b[0] >> 4)
	fc = int(b[0]&0xF)<<8 | int(b[1])
	mhz = FreqMHz(band, fc)
	bd, ok := rtBand[band]
	if !ok {
		return band, fc, 0, fmt.Sprintf("バンド %d（予約）・FC %d", band, fc)
	}
	text = fmt.Sprintf("%.4f MHz（バンド %d・FC %d、上り %.4f MHz）", mhz, band, fc, mhz+bd.Fb)
	if fc < 1 || fc > bd.Nc-1 {
		text += fmt.Sprintf(" ※FC が範囲外（1〜%d）", bd.Nc-1)
	}
	return band, fc, mhz, text
}

func (p *rtParser) directFreq(name string) float64 {
	b := p.r.take(2, name)
	if b == nil {
		return 0
	}
	band, fc, mhz, text := rtFreq(b)
	bd, ok := rtBand[band]
	if !ok || mhz == 0 {
		p.add(name, text)
		return 0
	}
	f := mhz + bd.Fb
	p.add(name, fmt.Sprintf("%.4f MHz（直接通信 = 移動局送信周波数。バンド %d・FC %d）", f, band, fc))
	return f
}

func (p *rtParser) directScramble() int {
	v, ok := p.byte1("スクランブルコード（通信チャネル用）", func(v byte) string {
		return fmt.Sprintf("0x%02X（業種コード通番 4 ビット + 自局番号の下位 4 ビット。§4.5.1.11）", v)
	})
	if !ok {
		return -1
	}
	return ScramblePattern(v)
}

func (p *rtParser) freq(name string) (band, fc int, mhz float64, ok bool) {
	b := p.r.take(2, name)
	if b == nil {
		return -1, -1, 0, false
	}
	band, fc, mhz, text := rtFreq(b)
	p.add(name, text)
	return band, fc, mhz, true
}

func rtSlotText(v byte) (text string, first int) {
	list := func(m byte) (string, int) {
		var s []string
		f := -1
		for k := 0; k < 4; k++ {
			if m&(1<<k) != 0 {
				s = append(s, fmt.Sprintf("ST#%d", k))
				if f < 0 {
					f = k
				}
			}
		}
		if len(s) == 0 {
			return "無指定", -1
		}
		return strings.Join(s, "+"), f
	}
	voice, fv := list(v & 0xF)
	data, fd := list(v >> 4)
	first = fv
	if first < 0 {
		first = fd
	}
	return fmt.Sprintf("音声 %s / 非音声 %s", voice, data), first
}

func rtTxPower(v byte) (text string, allOnes bool) {
	pw := pick([]string{"定格出力"}, int(v&7), "予約")
	allOnes = v&0x80 == 0
	scr := "基地局指定"
	if allOnes {
		scr = "オール1"
	}
	return fmt.Sprintf("最大送信電力 %s / 呼接続通信下りスクランブル %s", pw, scr), allOnes
}

func rtChannelInfo(v byte) string {
	kind := "制御チャネル"
	if v&1 != 0 {
		kind = "通信チャネル"
	}
	sec := pick([]string{"免許人固有秘匿", "標準秘匿バージョン1"}, int(v>>2)&3, "予約")
	return fmt.Sprintf("%s / %s / 免許人固有=0x%X", kind, sec, v>>4)
}

func rtGroupCallType(v byte) string {
	t := "グループ通信"
	if v&1 != 0 {
		t = "グループ通信後追い"
	}
	return fmt.Sprintf("%s / 免許人固有=0x%02X", t, v>>1)
}

func ScramblePattern(code byte) int { return int(code) }

func (p *rtParser) channelSet(group bool) *Assignment {
	a := &Assignment{Band: -1, FC: -1, Slot: -1, Scramble: -1, UpScramble: -1, Group: group, GroupFollow: -1}
	band, fc, mhz, ok := p.freq("周波数コード")
	if !ok {
		return nil
	}
	a.Band, a.FC, a.MHz = band, fc, mhz
	if bd, ok := rtBand[band]; ok && mhz != 0 {
		a.UpMHz = mhz + bd.Fb
	}
	if v, ok := p.r.byte1("スロット番号"); ok {
		t, first := rtSlotText(v)
		a.Slot = first
		p.add("スロット番号", t)
	}
	if group {
		if v, ok := p.byte1("グループ通信種別", rtGroupCallType); ok {
			a.GroupFollow = int(v & 1)
		}
	}
	code, codeOK := p.r.byte1("スクランブルコード")
	if codeOK {
		p.add("スクランブルコード", fmt.Sprintf("0x%02X", code))
		a.UpScramble = ScramblePattern(code)
	}
	allOnes := true
	if v, ok := p.r.byte1("移動局送信電力指定"); ok {
		var t string
		t, allOnes = rtTxPower(v)
		p.add("移動局送信電力指定", t)
	}
	if codeOK && !allOnes {
		a.Scramble = ScramblePattern(code)
	}
	p.byte1("チャネル情報", rtChannelInfo)

	freq := fmt.Sprintf("バンド %d（予約）・FC %d", a.Band, a.FC)
	if a.MHz != 0 {
		freq = fmt.Sprintf("%.4f MHz（バンド %d・FC %d）", a.MHz, a.Band, a.FC)
	}
	slot := "スロット無指定"
	if a.Slot >= 0 {
		slot = fmt.Sprintf("ST#%d", a.Slot)
	}
	scr := "スクランブル オール1"
	if a.Scramble >= 0 {
		scr = fmt.Sprintf("スクランブル 0x%02X", a.Scramble)
	}
	a.Text = fmt.Sprintf("周波数 %s%s %s", freq, slot, scr)
	return a
}


var rtCauseNames = map[int]string{
	0x02: "指定中継網へのルートなし",
	0x10: "正常切断", 0x11: "着ユーザビジー", 0x18: "移動局未登録", 0x19: "無通信検出",
	0x1C: "通信時限満了", 0x1D: "緊急通信", 0x1E: "発信規制中", 0x1F: "その他の正常イベント",
	0x22: "利用可能チャネルなし", 0x23: "要求チャネル数不足", 0x26: "網障害", 0x29: "一時的障害（スケルチ断時）",
	0x2A: "装置輻輳", 0x2C: "要求チャネル利用不可", 0x2F: "その他のリソース使用不可クラス",
	0x61: "メッセージ種別未定義または未提供", 0x64: "情報要素内容無効", 0x65: "呼状態とメッセージ不一致",
	0x66: "タイマ満了による回復", 0x6F: "その他の手順誤りクラス（相乗り異常・CC 異常時）",
}

func rtCauseText(v byte) string {
	c := int(v & 0x7F)
	class := pickMap(map[int]string{0: "正常クラス", 1: "正常クラス", 2: "リソース使用不可クラス", 6: "手順誤りクラス"}, c>>4, "予約クラス")
	src := "移動局自身"
	if v&0x80 != 0 {
		src = "中継網"
	}
	return fmt.Sprintf("%s（%s, 0x%02X）/ 生成源 %s", pickMap(rtCauseNames, c, "予約"), class, c, src)
}


func (p *rtParser) broadcast() {
	r := p.r
	p.m.Broadcast = &Broadcast{Support: -1}
	p.networkID()
	p.restriction()
	p.ccStructure()
	p.byte1("移動局送信電力指定(制御チャネル用)", func(v byte) string { t, _ := rtTxPower(v); return t })
	p.byte1("待ち受け許可レベル", rtLevelText)
	p.byte1("待ち受け劣化レベル", rtLevelText)
	p.byte1("レベル測定時間", func(v byte) string { return fmt.Sprintf("%d フレーム", v) })
	p.byte1("チャネル切替レベル", func(v byte) string {
		if v&0x7F == 0x7F {
			return "移動局デフォルト値"
		}
		return rtLevelText(v)
	})
	p.byte1("チャネル切替間隔時間", func(v byte) string {
		switch n := int(v & 0xF); n {
		case 0:
			return "移動局デフォルト値"
		case 15:
			return "CH切替禁止"
		default:
			return fmt.Sprintf("%d 秒", n*10)
		}
	})
	if v, ok := p.byte1("周辺ゾーン判定用制御チャネル数", func(v byte) string { return fmt.Sprint(v & 0x1F) }); ok {
		for k := 0; k < int(v&0x1F); k++ {
			_, _, mhz, ok := p.freq(fmt.Sprintf("制御チャネル番号 #%d", k+1))
			if !ok {
				return
			}
			p.m.Broadcast.Neighbors = append(p.m.Broadcast.Neighbors, mhz)
		}
	}
	p.byte1("位置登録タイマ", rtLocRegTimer)
	if v, ok := p.byte1("システム拡張情報構造", rtSysExpStructure); ok && v&0x80 != 0 {
		p.m.Broadcast.Support = int(v>>4) & 3
	}
	tu, ok := p.byte1("折返通信提供種別とスロット番号", rtTurnup)
	if !ok {
		return
	}
	if tu&0x20 != 0 {
		t := &Turnup{Slot: int(tu >> 6)}
		p.m.Broadcast.Turnup = t
		if _, _, mhz, ok := p.freq("折返通信周波数コード"); ok {
			t.MHz = mhz
		}
	}
	if r.err == "" {
		p.extension(true)
	}
}

func (p *rtParser) networkID() {
	n, ok := p.byte1("網番号情報オクテット数", func(v byte) string { return fmt.Sprint(v & 0xF) })
	if !ok {
		return
	}
	n &= 0xF
	if n == 0 {
		return
	}
	b := p.r.take(int(n), "網番号")
	if b == nil {
		return
	}
	if len(b) < 4 {
		p.add("網番号", hexOf(b)+"（4 オクテット未満）")
		return
	}
	p.add("免許人固有定義領域(網番号)", fmt.Sprintf("0x%02X", b[0]>>2))
	p.add("製造者番号", makerText(int(b[0]&3)<<8|int(b[1])))
	p.add("制御ゾーン番号", fmt.Sprint(b[2]))
	p.add("基地局番号", fmt.Sprint(b[3]))
	if len(b) > 4 {
		p.add("システムコード", numbering.SystemBytes(b[4:]).Text())
	}
	if p.m.Broadcast != nil {
		p.m.Broadcast.System = SystemOf(b)
	}
}

func SystemOf(b []byte) *SystemID {
	if len(b) < 4 {
		return nil
	}
	s := &SystemID{Maker: int(b[0]&3)<<8 | int(b[1]), Zone: int(b[2]), Base: int(b[3]), Industry: -1, Seq: -1, Area: -1}
	if len(b) > 4 {
		sy := numbering.SystemBytes(b[4:])
		s.Code, s.CodeLen = sy.Code, len(b)-4
		s.Industry, s.Seq, s.Area, s.AreaName, s.Org = sy.Industry, sy.Seq, sy.Area, sy.AreaName, sy.Org
	}
	return s
}

func (p *rtParser) restriction() {
	b := p.r.take(4, "規制情報")
	if b == nil {
		return
	}
	o1, o2, o3, o4 := b[0], b[1], b[2], b[3]
	var s []string
	if o1&0x80 != 0 {
		s = append(s, "自ゾーン選択否")
	} else {
		s = append(s, "自ゾーン選択可")
	}
	if o1&0x40 != 0 {
		s = append(s, fmt.Sprintf("ゾーン選択補正レベル %ddB", int(o1&0xF)*2))
	}
	if o1&0x20 != 0 {
		s = append(s, "自ゾーンアクセス否")
	} else {
		s = append(s, "自ゾーンアクセス可")
	}
	if o1&0x10 != 0 {
		s = append(s, "自ゾーンアクセス規制有り")
	} else {
		s = append(s, "自ゾーンアクセス規制無し")
	}
	p.add("移動局動作情報", strings.Join(s, " / "))
	if o1&0x10 != 0 {
		var reg []string
		names := []string{"優先 位置登録否", "優先 発信否", "優先 発信否(緊急通信による)", "優先 ショートメッセージ否",
			"一般 位置登録規制", "一般 発信規制", "一般 発信規制(緊急通信による)", "一般 ショートメッセージ規制"}
		for k, n := range names {
			if o2&(1<<k) != 0 {
				reg = append(reg, n)
			}
		}
		if len(reg) == 0 {
			reg = append(reg, "なし")
		}
		p.add("規制内容", strings.Join(reg, " / "))
		per := "規制無し"
		if n := int(o4 & 0xF); n > 0 {
			per = fmt.Sprintf("720×%dms", n*4)
		}
		p.add("アクセス周期間隔", per)
	} else if o2 != 0 || o4&0xF != 0 {
		p.add("規制内容（無効）", fmt.Sprintf("0x%02X / 周期 %d", o2, o4&0xF))
	}
	lim := func(n byte) string {
		if n == 0 {
			return "制限なし"
		}
		return fmt.Sprintf("%d 分", int(n)*3)
	}
	p.add("通信時限", fmt.Sprintf("音声 %s / 非音声 %s", lim(o3&0xF), lim(o3>>4)))
	if p.m.Broadcast != nil {
		p.m.Broadcast.Restrict = &Restriction{GroupHalfDuplex: o4&0x80 != 0, GroupHandoffLimit: o4&0x40 != 0, SysConfig: int(o4>>4) & 3,
			VoiceLimitMin: int(o3&0xF) * 3, DataLimitMin: int(o3>>4) * 3}
	}
	form := "単信"
	if o4&0x80 != 0 {
		form = "半複信"
	}
	ho := "無し"
	if o4&0x40 != 0 {
		ho = "有り"
	}
	sys := pick([]string{"通常時統制局接続無し・現状統制局接続無し", "予約", "通常時統制局接続有り・現状統制局接続無し",
		"通常時統制局接続有り・現状統制局接続有り"}, int(o4>>4)&3, "予約")
	p.add("システム構成情報", sys)
	p.add("グループ通信", fmt.Sprintf("通信形態 %s / ハンドオフ規制 %s", form, ho))
}

func (p *rtParser) ccStructure() {
	b := p.r.take(3, "制御チャネル構造情報")
	if b == nil {
		return
	}
	np, nc := int(b[0]>>4), int(b[0]&0xF)
	ab, ap := int(b[1]>>4), int(b[1]&0xF)
	as2, as1 := int(b[2]>>4), int(b[2]&0xF)
	p.add("制御チャネル構造情報", fmt.Sprintf("Nc=%d Np=%d Ab=%d Ap=%d As1=%d As2=%d", nc, np, ab, ap, as1, as2))
	if np != 1 || nc != 1 {
		p.m.Notes = append(p.m.Notes, fmt.Sprintf("制御チャネル構造情報の Np=%d Nc=%d（§4.1.13.4 は Np = Nc = 1 固定）", np, nc))
	}
	if n := ab + as1 + (ap+as2)*max(np, 1); n > 18 {
		p.m.Notes = append(p.m.Notes, fmt.Sprintf("制御チャネル構造情報の配置が %d スロット（1 スーパーフレームは 18。図 4.1.13-1）", n))
	}
	if p.m.Broadcast != nil {
		p.m.Broadcast.Structure = &ChannelStructure{Np: np, Nc: nc, AB: ab, AP: ap, AS1: as1, AS2: as2}
	}
	for k := 1; k <= nc; k++ {
		_, _, mhz, ok := p.freq(fmt.Sprintf("制御チャネル F%d", k))
		if !ok {
			return
		}
		if p.m.Broadcast != nil {
			p.m.Broadcast.Control = append(p.m.Broadcast.Control, mhz)
		}
		if _, ok := p.byte1(fmt.Sprintf("制御チャネル S%d", k), func(v byte) string { t, _ := rtSlotText(v); return t }); !ok {
			return
		}
	}
}

func rtLocRegTimer(v byte) string {
	init := pick([]string{0: "タイマ起動なし", 7: "タイマ起動なし・即時位置登録"}, int(v&7), "予約")
	rep := "タイマ起動なし"
	if n := int(v >> 3); n > 0 {
		rep = fmt.Sprintf("%d 分", n*10)
	}
	return "初期 " + init + " / 繰り返し周期 " + rep
}

func rtSysExpStructure(v byte) string {
	sup := "応援無し"
	if v&0x80 != 0 {
		sup = "応援有り: " + pick([]string{"種別1 県内応援", "種別2 相互応援（県外応援）", "種別3 グループ限定応援"}, int(v>>4)&3, "予約")
	}
	iv := "システム拡張情報の送信無し"
	if n := v & 0xF; n > 0 {
		iv = fmt.Sprintf("送信間隔 %d スーパーフレーム", n)
	}
	return sup + " / " + iv
}

func rtTurnup(v byte) string {
	if v&0x20 == 0 {
		return "呼接続通信"
	}
	return fmt.Sprintf("折返通信 / 折返通信用スロット ST#%d", v>>6)
}

func rtPeriodText(v byte, ch string) string {
	if n := v & 0xF; n > 0 {
		return fmt.Sprintf("%d スーパーフレーム", n)
	}
	return ch + "受信不要"
}

func (p *rtParser) extension(mandatory bool) {
	if !mandatory && p.r.left() == 0 {
		return
	}
	k, ok := p.byte1("拡張情報要素長", func(v byte) string { return fmt.Sprint(v & 0x7F) })
	if !ok || k&0x7F == 0 {
		return
	}
	if b := p.r.take(int(k&0x7F), "拡張情報要素"); b != nil {
		p.add("拡張情報要素(免許人オプション)", hexOf(b))
		p.m.vendorRaw(extensionWhat, b)
	}
}

const extensionWhat = "拡張情報要素"


func (p *rtParser) systemExpanded() {
	p.byte1("システム拡張情報更新番号", rtDec)
	n, ok := p.byte1("専用チャネル周波数数", func(v byte) string {
		if v&0xF > 8 {
			return fmt.Sprintf("%d（予約）", v&0xF)
		}
		return fmt.Sprint(v & 0xF)
	})
	if !ok {
		return
	}
	for i := 1; i <= int(n&0xF); i++ {
		_, _, mhz, ok := p.freq(fmt.Sprintf("専用チャネル周波数コード %d", i))
		if !ok {
			return
		}
		mm, ok := p.byte1(fmt.Sprintf("周波数 %d の専用チャネル通信識別番号数", i), func(v byte) string {
			return pick([]string{1: "1 スロット", 2: "2 スロット", 3: "3 スロット", 4: "4 スロット"}, int(v&7), "予約")
		})
		if !ok {
			return
		}
		for j := 1; j <= int(mm&7); j++ {
			v, ok := p.byte1(fmt.Sprintf("周波数 %d の割当情報 #%d", i, j), func(v byte) string {
				id := fmt.Sprint(v & 0x1F)
				if v&0x1F == 0 {
					id = "0（予約）"
				}
				return fmt.Sprintf("ST#%d / 専用チャネル通信識別番号 %s", v>>6, id)
			})
			if !ok {
				return
			}
			p.m.Dedicated = append(p.m.Dedicated, Dedicated{MHz: mhz, Slot: int(v >> 6), ID: int(v & 0x1F)})
		}
	}
	p.extension(true)
}


func (p *rtParser) shortMessage(response bool) {
	sm := &ShortMessage{Response: response, Number: -1}
	p.m.Short = sm
	var ok bool
	if sm.From, ok = p.number("発信者識別"); !ok {
		return
	}
	if sm.To, ok = p.number("呼出番号"); !ok {
		return
	}
	if v, ok := p.byte1("メッセージ番号", rtMessageNumber); ok {
		sm.Number, sm.Kind = int(v), rtMessageKind(v)
		sm.Emergency = v == 0x0A
		if v&0xF > 0xA || v>>4 > 9 {
			p.m.vendorRaw(fmt.Sprintf("メッセージ番号 0x%02X", v), p.r.b)
		}
	}
	sm.Text = fmt.Sprintf("%s → %s %s", orQ(sm.From), orQ(sm.To), sm.Kind)
	p.byte1("警報・表示", func(v byte) string { return rtWarning(v, response) })
	if response {
		p.byte1("理由表示", rtCauseText)
	}
	if p.r.err == "" {
		p.extension(false)
	}
}

var rtNumberTypes = []string{0: "不定", 2: "国内番号", 3: "免許人特有番号", 4: "市内番号", 6: "短縮番号", 7: "予約(1.1版の応援通信)"}

func (p *rtParser) number(name string) (string, bool) {
	n, ok := p.r.byte1(name + "内容長")
	if !ok {
		return "", false
	}
	body := p.r.take(int(n), name)
	if body == nil {
		return "", false
	}
	if len(body) == 0 {
		p.add(name, "（内容長 0）")
		return "", true
	}
	odd := body[0]&0x80 != 0
	typ := pick(rtNumberTypes, int(body[0]>>4)&7, "予約")
	d := rtDigits(body[1:], odd)
	p.add(name, fmt.Sprintf("%s（%s）", d, typ))
	return d, true
}

func orQ(s string) string {
	if s == "" {
		return "?"
	}
	return s
}

func rtMessageKind(v byte) string {
	ones, tens := int(v>>4), int(v&0xF)
	switch {
	case tens <= 4 && ones <= 9:
		return fmt.Sprintf("定型文 %d%d「%s」", tens, ones, FixedText(10*tens+ones))
	case tens <= 9 && ones <= 9:
		return fmt.Sprintf("免許人定義文 %d%d", tens, ones)
	case tens == 0xA:
		return pick([]string{"緊急連絡", "チャネル状態表示要求", "チャネル状態表示応答", "強制切断要求", "強制切断応答", "発信規制／解除"}, ones, "制御メッセージ 予約")
	}
	return fmt.Sprintf("免許人固有メッセージ 0x%02X", v)
}

func rtDigits(b []byte, odd bool) string {
	var sb strings.Builder
	for i, o := range b {
		for k, nib := range []byte{o & 0xF, o >> 4} {
			if (i == len(b)-1 && odd && k == 1) || nib == 0 {
				continue
			}
			sb.WriteByte(digitChar(nib))
		}
	}
	return sb.String()
}

func rtMessageNumber(v byte) string {
	ones, tens := int(v>>4), int(v&0xF)
	switch {
	case tens <= 4 && ones <= 9:
		return fmt.Sprintf("%d%d（免許人共通定型文「%s」）", tens, ones, FixedText(10*tens+ones))
	case tens <= 9 && ones <= 9:
		return fmt.Sprintf("%d%d（免許人毎に定義）", tens, ones)
	case tens == 0xA:
		t := pick([]string{"緊急連絡", "チャネル状態表示要求", "チャネル状態表示応答", "強制切断要求", "強制切断応答", "発信規制／解除"}, ones, "予約")
		return fmt.Sprintf("%X%X（%s・免許人共通）", tens, ones, t)
	}
	return fmt.Sprintf("0x%02X（免許人固有メッセージ）", v)
}

func rtWarning(v byte, response bool) string {
	al := pick([]string{"警報なし", "警報音1", "警報音2", "予約"}, int(v&3), "予約")
	disp := "メッセージ表示なし"
	if v&4 != 0 {
		disp = "メッセージ表示あり"
	}
	s := al + " / " + disp
	if response {
		if v&8 != 0 {
			s += " / 伝送NG"
		} else {
			s += " / 伝送OK"
		}
	}
	return fmt.Sprintf("%s / 免許人固有=0x%X", s, v>>4)
}
