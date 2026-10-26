package l3

import "fmt"


var rtNames = map[uint8]string{
	0x01: "発信無線状態報告", 0x02: "ページング", 0x03: "着信無線状態報告", 0x04: "グループ通信呼出",
	0x21: "移動局解放",
	0x43: "無線チャネル指定",
	0x61: "切替先無線チャネル指定", 0x62: "システム情報", 0x63: "システム情報確認",
	0x71: "切り戻し要求", 0x72: "送信中再発呼", 0x73: "受信中再発呼",
	0x81: "無線チャネル切断", 0x82: "無線チャネル切断確認",
	0xA1: "報知情報", 0xA2: "BCCH受信",
	0xB1: "ショートメッセージ発信", 0xB2: "ショートメッセージ発信応答",
	0xB3: "ショートメッセージ着信", 0xB4: "ショートメッセージ着信応答",
	0xC1: "送信権解放", 0xC2: "送信停止要求",
	0xFF: "免許人固有情報",
}

func parseRT(b []byte) Message {
	m := Message{Entity: "RT", Raw: b}
	if len(b) == 0 {
		m.Err = "空"
		return m
	}
	m.Type = b[0]
	m.Name = rtNames[m.Type]
	if m.Name == "" {
		m.Name = fmt.Sprintf("予約(0x%02X)", m.Type)
	}
	m.RT = rtFacts(b)
	r := &reader{b: b, i: 1}
	add := func(f Field, ok bool) {
		if ok {
			m.Fields = append(m.Fields, f)
		}
	}
	switch m.Type {
	case 0x01:
		add(msType(r))
		add(rxLevel(r, "受信レベル"))
	case 0x02:
		add(byteField(r, "着信識別番号", nil))
	case 0x03:
		add(byteField(r, "着信識別番号", nil))
		add(msType(r))
		add(rxLevel(r, "受信レベル"))
	case 0x04:
		add(freqCode(r, "グループ通信用通信チャネル番号"))
		add(groupCallType(r))
		add(scrambleCode(r))
		add(txPower(r, "移動局送信電力指定"))
		add(channelInfo(r))
	case 0x21, 0x71, 0x81:
		add(rtCause(r))
	case 0x43, 0x61:
		add(freqCode(r, "周波数コード"))
		add(scrambleCode(r))
		add(txPower(r, "移動局送信電力指定"))
		add(channelInfo(r))
	case 0x62:
		ccsInfo(r, &m)
		add(freqCode(r, "制御チャネル番号(自ゾーン)"))
		restriction(r, &m)
		add(txPower(r, "移動局送信電力指定(制御CH)"))
		add(byteField(r, "レベル測定回数", func(v byte) string { return fmt.Sprintf("%d フレーム", v) }))
		add(chSwitchLevel(r))
		add(byteField(r, "チャネル切替間隔時間", func(v byte) string {
			switch v & 0xF {
			case 0:
				return "移動局デフォルト"
			case 15:
				return "CH切替禁止"
			}
			return fmt.Sprintf("%d 秒", int(v&0xF)*10)
		}))
		networkID(r, &m)
		neighborCCH(r, &m)
		extension(r, &m)
	case 0x72, 0x73:
		add(rxLevel(r, "受信レベル"))
	case 0xA1:
		networkID(r, &m)
		restriction(r, &m)
		ccsInfo(r, &m)
		add(txPower(r, "移動局送信電力指定(制御CH)"))
		add(rxLevel(r, "待ち受け許可レベル"))
		add(rxLevel(r, "待ち受け劣化レベル"))
		neighborCCH(r, &m)
		if n, ok := r.byte1("折り返し通信用通信チャネル数"); ok {
			n &= 0xF
			m.Fields = append(m.Fields, Field{"折り返し通信用通信チャネル数", fmt.Sprint(n)})
			for k := 0; k < int(n); k++ {
				add(freqCode(r, fmt.Sprintf("通信チャネル番号 #%d", k+1)))
			}
		}
		add(locRegTimer(r))
		extension(r, &m)
	case 0xA2:
		add(byteField(r, "BCCH受信区間長", func(v byte) string {
			if v&0xF == 0 {
				return "BCCH受信不要"
			}
			return fmt.Sprintf("%d スーパーフレーム", v&0xF)
		}))
	case 0xB1, 0xB3:
		add(numberIE(r, "発信者識別"))
		add(numberIE(r, "呼出番号"))
		add(byteField(r, "メッセージ番号", func(v byte) string { return fmt.Sprintf("%d%d", v&0xF, v>>4) }))
		add(warning(r))
	case 0xB2, 0xB4:
		add(numberIE(r, "発信者識別"))
		add(numberIE(r, "呼出番号"))
		add(warning(r))
	case 0xFF:
		networkID(r, &m)
		if r.left() > 0 {
			m.Fields = append(m.Fields, Field{"免許人固有情報", hexOf(r.b[r.i:])})
			r.i = len(r.b)
		}
	}
	if r.err != "" {
		m.Err = r.err
	} else if r.left() > 0 {
		m.Fields = append(m.Fields, Field{"余剰", hexOf(r.b[r.i:])})
	}
	return m
}

func byteField(r *reader, name string, f func(byte) string) (Field, bool) {
	v, ok := r.byte1(name)
	if !ok {
		return Field{}, false
	}
	if f == nil {
		return Field{name, fmt.Sprint(v)}, true
	}
	return Field{name, f(v)}, true
}

func msType(r *reader) (Field, bool) {
	b := r.take(3, "移動局種別")
	if b == nil {
		return Field{}, false
	}
	cls := pick([]string{"一般", "優先(VIP・災害対策)", "無線通信卓", "保守用"}, int(b[0]>>6), "分類")
	pw := pick([]string{"予約", "3.0W", "2.0W", "1.0W"}, int(b[0]>>3)&7, "電力")
	kind := pick([]string{"予約", "単信", "複信", "単信+複信"}, int(b[0]&7), "種類")
	return Field{"移動局種別", fmt.Sprintf("%s / 最大%s / %s / Rev.%d 秘匿v%d / 免許人=%02X",
		cls, pw, kind, b[1]>>5, (b[1]>>2)&3, b[2]&0x7F)}, true
}

func rxLevel(r *reader, name string) (Field, bool) {
	v, ok := r.byte1(name)
	if !ok {
		return Field{}, false
	}
	return Field{name, levelText(v & 0x7F)}, true
}

func levelText(v byte) string {
	switch {
	case v >= 124:
		return "80dBμ以上"
	case v <= 38:
		return "−5dBμ未満"
	}
	return fmt.Sprintf("%ddBμ", int(v)-44)
}

func chSwitchLevel(r *reader) (Field, bool) {
	v, ok := r.byte1("チャネル切替レベル")
	if !ok {
		return Field{}, false
	}
	if v&0x7F == 0x7F {
		return Field{"チャネル切替レベル", "移動局デフォルト"}, true
	}
	return Field{"チャネル切替レベル", levelText(v & 0x7F)}, true
}

func freqCode(r *reader, name string) (Field, bool) {
	b := r.take(2, name)
	if b == nil {
		return Field{}, false
	}
	band := b[0] >> 4
	fc := int(b[0]&0xF)<<8 | int(b[1])
	return Field{name, fmt.Sprintf("帯=%d Fc=%d (Fo+%.5f MHz)", band, fc, float64(fc)*0.00625)}, true
}

func scrambleCode(r *reader) (Field, bool) {
	v, ok := r.byte1("スクランブルコード")
	if !ok {
		return Field{}, false
	}
	return Field{"スクランブルコード", fmt.Sprintf("0x%02X (PN初期値 0x%03X)", v, ScrambleInit(v))}, true
}

func ScrambleInit(code byte) int {
	return int(code)<<1 | int(code&1)
}

func AssignedScramble(s Signal) (init int, code byte, ok bool) {
	for _, m := range s.Messages {
		if m.Entity != "RT" {
			continue
		}
		at := -1
		switch m.Type {
		case 0x43, 0x61:
			at = 3
		case 0x04:
			at = 4
		}
		if at >= 0 && at < len(m.Raw) {
			return ScrambleInit(m.Raw[at]), m.Raw[at], true
		}
	}
	return 0, 0, false
}

func txPower(r *reader, name string) (Field, bool) {
	v, ok := r.byte1(name)
	if !ok {
		return Field{}, false
	}
	t := []string{"予約", "予約", "2.0W", "2.0W-5dB", "2.0W-10dB", "2.0W-15dB", "2.0W-20dB", "2.0W-25dB", "2.0W-30dB"}
	return Field{name, pick(t, int(v&0xF), "予約")}, true
}

func channelInfo(r *reader) (Field, bool) {
	v, ok := r.byte1("チャネル情報")
	if !ok {
		return Field{}, false
	}
	kind := "制御チャネル"
	if v&1 == 1 {
		kind = "通信チャネル"
	}
	sec := pick([]string{"免許人固有秘匿", "標準秘匿v1"}, int(v>>2)&3, "秘匿")
	return Field{"チャネル情報", fmt.Sprintf("%s / %s / 免許人=%X", kind, sec, v>>4)}, true
}

func groupCallType(r *reader) (Field, bool) {
	v, ok := r.byte1("グループ通信種別")
	if !ok {
		return Field{}, false
	}
	t := []string{"グループ通信", "緊急グループ通信", "グループ通信後追い参入", "緊急グループ通信後追い参入"}
	return Field{"グループ通信種別", fmt.Sprintf("%s / 免許人=%X", pick(t, int(v&7), "予約"), v>>3)}, true
}

func rtCause(r *reader) (Field, bool) {
	v, ok := r.byte1("理由表示")
	if !ok {
		return Field{}, false
	}
	src := "移動局自身"
	if v&0x80 != 0 {
		src = "中継網"
	}
	names := map[byte]string{
		0x10: "正常切断", 0x18: "移動局未登録", 0x19: "無通信検出", 0x1C: "通信時限満了", 0x1D: "緊急グループ通信", 0x1F: "その他の正常イベント",
		0x22: "利用可能チャネルなし", 0x26: "網障害", 0x29: "一時的障害(スケルチ断)", 0x2A: "装置輻輳", 0x2C: "要求チャネル利用不可", 0x2F: "その他のリソース使用不可",
		0x61: "メッセージ種別未定義/未提供", 0x64: "情報要素内容無効", 0x65: "呼状態とメッセージ不一致", 0x66: "タイマ満了による回復", 0x6F: "その他の手順誤り",
	}
	n := names[v&0x7F]
	if n == "" {
		n = fmt.Sprintf("予約(0x%02X)", v&0x7F)
	}
	return Field{"理由表示", n + " / 生成源=" + src}, true
}

func networkID(r *reader, m *Message) {
	n, ok := r.byte1("網番号情報オクテット数")
	if !ok {
		return
	}
	n &= 0xF
	b := r.take(int(n), "網番号")
	if b == nil {
		return
	}
	if len(b) < 2 {
		m.Fields = append(m.Fields, Field{"網番号", hexOf(b)})
		return
	}
	var sys uint64
	for _, v := range b[2:] {
		sys = sys<<8 | uint64(v)
	}
	m.Fields = append(m.Fields, Field{"網番号", fmt.Sprintf("制御ゾーン=%d 基地局=%d システムコード=%d", b[0], b[1], sys)})
}

func restriction(r *reader, m *Message) {
	b := r.take(4, "規制情報")
	if b == nil {
		return
	}
	o1, o2, o3, o4 := b[0], b[1], b[2], b[3]
	var s []string
	if o1&0x80 != 0 {
		s = append(s, "自ゾーン選択否")
	}
	if o1&0x40 != 0 {
		s = append(s, fmt.Sprintf("補正レベル%ddB", int(o1&0xF)*2))
	}
	if o1&0x20 != 0 {
		s = append(s, "自ゾーンアクセス否")
	}
	if o1&0x10 != 0 {
		per := "規制無し"
		if n := int(o4 & 0xF); n > 0 {
			per = fmt.Sprintf("720ms×%d", 4*n)
		}
		s = append(s, fmt.Sprintf("アクセス規制あり(規制=%08b 周期=%s)", o2, per))
	}
	if o3&0x1F != 0 {
		s = append(s, fmt.Sprintf("通信時限%d分", o3&0x1F))
	}
	if o3>>5 != 0 {
		s = append(s, fmt.Sprintf("制御CH通信時限%d秒", int(o3>>5)*30))
	}
	s = append(s, pick([]string{"単一基地局ゾーン", "複数基地局ゾーン", "複数制御ゾーン", "予約"}, int(o4>>4)&3, "構成"))
	if len(s) == 0 {
		s = append(s, "規制なし")
	}
	m.Fields = append(m.Fields, Field{"規制情報", fmt.Sprintf("%s [%s]", joinComma(s), hexOf(b))})
	m.Fields = append(m.Fields, Field{"規制内容（オクテット2）", restrictionBits(o1, o2)})
	m.Fields = append(m.Fields, Field{"免許人固有定義領域", fmt.Sprintf("%02b（オクテット 4 のビット 8〜7。免許人が定義）", o4>>6)})
}

func restrictionBits(o1, o2 byte) string {
	names := []struct{ name, off, on string }{
		{"優先 位置登録", "可", "否"},
		{"優先 発信", "可", "否"},
		{"優先 発信(緊急グループ通信)", "可", "否"},
		{"優先 ショートメッセージ伝送", "可", "否"},
		{"一般 位置登録規制", "無し", "有り"},
		{"一般 発信規制", "無し", "有り"},
		{"一般 発信規制(緊急グループ通信)", "無し", "有り"},
		{"一般 ショートメッセージ伝送規制", "無し", "有り"},
	}
	var s []string
	for i, n := range names {
		v := n.off
		if o2>>uint(i)&1 == 1 {
			v = n.on
		}
		s = append(s, n.name+"="+v)
	}
	out := joinComma(s)
	if o1&0x10 == 0 {
		out += "（移動局動作情報ビット 5 = 0 なので無効）"
	}
	return out
}

func joinComma(s []string) string {
	out := ""
	for i, v := range s {
		if i > 0 {
			out += ", "
		}
		out += v
	}
	return out
}

func ccsInfo(r *reader, m *Message) {
	b := r.take(3, "制御チャネル構造情報")
	if b == nil {
		return
	}
	nc, np := int(b[0]>>4), int(b[0]&0xF)
	ab, ap := int(b[1]>>4), int(b[1]&0xF)
	as2, as1 := int(b[2]>>4), int(b[2]&0xF)
	m.Fields = append(m.Fields, Field{"制御チャネル構造情報",
		fmt.Sprintf("Nc=%d Np=%d Ab=%d Ap=%d As1=%d As2=%d", nc, np, ab, ap, as1, as2)})
	for k := 0; k < nc; k++ {
		if f, ok := freqCode(r, fmt.Sprintf("制御チャネル F%d", k+1)); ok {
			m.Fields = append(m.Fields, f)
		}
	}
}

func neighborCCH(r *reader, m *Message) {
	n, ok := r.byte1("周辺ゾーン判定用制御チャネル数")
	if !ok {
		return
	}
	n &= 0x1F
	m.Fields = append(m.Fields, Field{"周辺ゾーン判定用制御チャネル数", fmt.Sprint(n)})
	for k := 0; k < int(n); k++ {
		if f, ok := freqCode(r, fmt.Sprintf("周辺制御チャネル #%d", k+1)); ok {
			m.Fields = append(m.Fields, f)
		}
	}
}

func locRegTimer(r *reader) (Field, bool) {
	v, ok := r.byte1("位置登録タイマ")
	if !ok {
		return Field{}, false
	}
	init := "起動なし"
	switch iv := v & 7; {
	case iv == 7:
		init = "即時位置登録"
	case iv > 0:
		init = fmt.Sprintf("%d分", iv)
	}
	rep := "起動なし"
	if rv := v >> 3; rv > 0 {
		rep = fmt.Sprintf("%d分", int(rv)*10)
	}
	return Field{"位置登録タイマ", "初期=" + init + " 繰り返し=" + rep}, true
}

func extension(r *reader, m *Message) {
	k, ok := r.byte1("拡張情報要素長")
	if !ok {
		return
	}
	k &= 0x3F
	if k == 0 {
		return
	}
	b := r.take(int(k), "拡張情報要素")
	if b != nil {
		m.Fields = append(m.Fields, Field{"拡張情報要素(免許人)", hexOf(b)})
	}
}

func warning(r *reader) (Field, bool) {
	v, ok := r.byte1("警報・表示")
	if !ok {
		return Field{}, false
	}
	al := pick([]string{"警報なし", "警報音1", "警報音2", "予約"}, int(v&3), "警報")
	disp := "表示なし"
	if v&4 != 0 {
		disp = "表示あり"
	}
	orig := "発信者識別=移動局番号"
	if v&8 != 0 {
		orig = "発信者識別=呼出番号"
	}
	return Field{"警報・表示", fmt.Sprintf("%s / %s / %s / 免許人=%X", al, disp, orig, v>>4)}, true
}
