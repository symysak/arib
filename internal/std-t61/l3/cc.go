package l3

import (
	"fmt"
	"strings"
)


var ccNames = map[uint8]string{
	0x01: "呼出 ALERT", 0x02: "呼設定受付 CALL PROC", 0x07: "応答 CONN", 0x0F: "応答確認 CONN ACK",
	0x03: "経過表示 PROG", 0x05: "呼設定 SETUP", 0x11: "再呼設定受付 ReCALL PROC",
	0x12: "通信形態", 0x13: "通信可否", 0x14: "再呼設定拒否", 0x15: "単信呼設定", 0x16: "単信再呼設定",
	0x17: "複信呼設定", 0x18: "複信再呼設定", 0x19: "グループ呼設定",
	0x20: "ユーザ情報 USER INFO",
	0x45: "切断 DISC", 0x4D: "解放 REL", 0x5A: "解放完了 REL COMP",
	0x62: "ファシリティ FAC", 0x79: "輻輳制御 CONG CON", 0x7B: "付加情報 INFO", 0x7D: "状態表示 STATUS",
}

type ieKind int

const (
	ieLen   ieKind = iota
	ieFixed
	ieEA
)

type ieDef struct {
	name  string
	kind  ieKind
	fixed int
	dec   func(b []byte) string
}

var ccIE0Dir = map[byte]func(b []byte, dir Dir) string{0x14: decCallState}

var ccIE0 = map[byte]ieDef{
	0x04: {"伝達能力", ieLen, 0, decBearer},
	0x08: {"理由表示", ieLen, 0, decCause},
	0x14: {"呼状態", ieLen, 0, nil},
	0x18: {"チャネル識別子", ieLen, 0, decChannelID},
	0x1C: {"ファシリティ", ieLen, 0, decFacility},
	0x1E: {"経過識別子", ieFixed, 2, decProgress},
	0x28: {"表示", ieLen, 0, func(b []byte) string { return ia5(b) }},
	0x2C: {"キーパッドファシリティ", ieLen, 0, decKeypad},
	0x34: {"シグナル", ieFixed, 1, decSignal},
	0x38: {"フィーチャアクティベーション", ieLen, 0, decFeatureAct},
	0x39: {"フィーチャインディケーション", ieLen, 0, decFeatureInd},
	0x6C: {"発番号", ieLen, 0, decCallingNumber},
	0x6D: {"発サブアドレス(第1)", ieLen, 0, decSubaddress},
	0x70: {"着番号", ieLen, 0, decCalledNumber},
	0x71: {"着サブアドレス(第1)", ieLen, 0, decSubaddress},
	0x7D: {"高位レイヤ整合性", ieFixed, 2, decHLC},
	0x7E: {"ユーザ・ユーザ", ieLen, 0, decUserUser},
}

var ccIE6 = map[byte]ieDef{
	0x6E: {"発サブアドレス(第2)", ieLen, 0, decSubaddress},
	0x72: {"着サブアドレス(第2)", ieLen, 0, decSubaddress},
	0x1F: {"旧ゾーン情報", ieFixed, 2, decPrimaryZone},
	0x3F: {"旧ゾーン情報", ieFixed, 2, decPrimaryZone},
	0x0F: {"移動局番号", ieEA, 0, decMSNumber},
}

func parseCC(b []byte, dir Dir) Message {
	m := Message{Entity: "CC", Raw: b}
	if len(b) < 3 {
		m.Err = "3 オクテット未満"
		return m
	}
	pd := b[0]
	if pd != 0x08 {
		m.Fields = append(m.Fields, Field{"プロトコル識別子", fmt.Sprintf("0x%02X（標準 CC ではない）", pd)})
	}
	ss := b[1] >> 4
	flag := (b[1] >> 3) & 1
	cr := b[1] & 7
	crText := fmt.Sprint(cr)
	switch cr {
	case 0:
		crText = "0(単信通信)"
	case 7:
		crText = "7(ダミー)"
	}
	side := "生起側から"
	if flag == 1 {
		side = "生起側へ"
	}
	m.Fields = append(m.Fields, Field{"呼番号", fmt.Sprintf("%s %s / 付加サービス=%s", crText, side,
		pick([]string{"移動通信網特有", "Q.932準拠"}, int(ss), "予約"))})
	m.Type = b[2] & 0x7F
	m.Name = ccNames[m.Type]
	if m.Name == "" {
		m.Name = fmt.Sprintf("予約(0x%02X)", m.Type)
	}
	cc := &CCFacts{Ref: int(cr), ToOrigin: flag == 1, Cause: -1, CallState: -1}
	m.CC = cc
	i := 3
	codeset := 0
	for i < len(b) {
		id := b[i]
		if id&0x80 != 0 {
			i++
			switch {
			case id&0xF0 == 0x90:
				codeset = int(id & 7)
				m.Fields = append(m.Fields, Field{"固定シフト", fmt.Sprintf("コード群 %d", codeset)})
			case codeset == 0 && id == 0xA0:
				m.Fields = append(m.Fields, Field{"モアデータ", "あり"})
			case codeset == 0 && id&0xF0 == 0xB0:
				m.Fields = append(m.Fields, Field{"輻輳制御レベル", pickMap(map[int]string{0: "RR(受信可)", 15: "RNR(受信不可)"}, int(id&0xF), "値")})
			case codeset == 5 && id == 0x82:
				m.Fields = append(m.Fields, Field{"再呼表示", "あり"})
			case codeset == 6 && id&0xF0 == 0xF0:
				m.Fields = append(m.Fields, Field{"通信可否", pick([]string{"可", "予約", "予約", "不可"}, int(id&3), "")})
			default:
				m.Fields = append(m.Fields, Field{fmt.Sprintf("単数オクテットIE(群%d)", codeset), fmt.Sprintf("0x%02X", id)})
			}
			continue
		}
		if codeset == 5 && id == 0x02 {
			i++
			m.Fields = append(m.Fields, Field{"再呼表示", "あり（0x02。図 4.3.7.3-26 の印字の形。表 4.3.7.3-6(イ) は 0x82）"})
			continue
		}
		var def ieDef
		var known bool
		switch codeset {
		case 0:
			def, known = ccIE0[id]
		case 6:
			def, known = ccIE6[id]
		}
		if !known {
			def = ieDef{fmt.Sprintf("情報要素 0x%02X(群%d)", id, codeset), ieLen, 0, nil}
		}
		i++
		var body []byte
		switch def.kind {
		case ieLen:
			if i >= len(b) {
				m.Err = def.name + " の内容長が無い"
				return m
			}
			n := int(b[i])
			i++
			if i+n > len(b) {
				m.Err = def.name + " が短い"
				body = b[i:]
				i = len(b)
			} else {
				body = b[i : i+n]
				i += n
			}
		case ieFixed:
			if i+def.fixed > len(b) {
				m.Err = def.name + " が短い"
				body = b[i:]
				i = len(b)
			} else {
				body = b[i : i+def.fixed]
				i += def.fixed
			}
		case ieEA:
			j := i
			for j < len(b) && b[j]&0x80 == 0 {
				j++
			}
			if j < len(b) {
				j++
			}
			body = b[i:j]
			i = j
		}
		if codeset == 0 {
			cc.note(id, body)
		}
		val := hexOf(body)
		if f, ok := ccIE0Dir[id]; ok && codeset == 0 {
			val = f(body, dir)
		} else if def.dec != nil {
			val = def.dec(body)
		}
		m.Fields = append(m.Fields, Field{def.name, val})
		if m.Err != "" {
			return m
		}
	}
	return m
}

func decBearer(b []byte) string {
	if len(b) < 2 {
		return hexOf(b)
	}
	var s []string
	s = append(s, "コーディング標準="+pickMap(map[int]string{0: "本標準規格", 3: "網特有標準"}, int(b[0]>>5)&3, "予約"))
	s = append(s, "情報転送能力="+pickMap(map[int]string{0: "音声", 0x17: "高速非音声(訂正無し)", 0x09: "低速非音声(訂正有り)", 1: "デフォルト"}, int(b[0]&0x1F), "予約"))
	s = append(s, "転送モード="+pickMap(map[int]string{0: "回線交換"}, int(b[1]>>5)&3, "予約"))
	s = append(s, transferRate(b[1]))
	i := 2
	if b[1]&0x80 == 0 && i < len(b) {
		o := b[i]
		i++
		s = append(s, "構造="+pickMap(map[int]string{0: "デフォルト", 1: "25Hz構造保存"}, int(o>>5)&3, "予約"))
		mode := pick([]string{"単信個別", "デフォルト", "複信個別", "", "グループ", "", "同報"}, int(o>>1)&7, "通信形態")
		if o&0x10 != 0 {
			mode = "緊急" + mode
		}
		s = append(s, "通信形態="+mode)
		s = append(s, "呼設定法="+pick([]string{"即時接続", "予約"}, int(o&1), ""))
		if o&0x80 == 0 && i < len(b) {
			o4b := b[i]
			i++
			s = append(s, "対称性="+pickMap(map[int]string{0: "両方向対称"}, int(o4b>>5)&3, "予約"))
			s = append(s, "着→発 "+transferRate(o4b))
		}
	}
	if i < len(b) {
		o := b[i]
		i++
		if id := int(o>>5) & 3; id != 1 {
			s = append(s, fmt.Sprintf("レイヤ1識別=予約(%d)", id))
		}
		s = append(s, "L1="+pickMap(map[int]string{0x11: "本標準速度整合", 0x02: "G.711 μ-law", 0x1A: "ELCELP"}, int(o&0x1F), "予約"))
		if o&0x80 == 0 && i < len(b) {
			a := b[i]
			i++
			s = append(s, pick([]string{"同期", "非同期(予約)"}, int(a>>6)&1, ""))
			s = append(s, pick([]string{"インバンド交渉不可", "インバンド交渉(予約)"}, int(a>>5)&1, ""))
			s = append(s, "ユーザ速度="+pickMap(map[int]string{1: "本標準規格"}, int(a&0x1F), "予約"))
		}
	}
	if i < len(b) {
		s = append(s, "余り "+hexOf(b[i:]))
	}
	return joinComma(s) + " [" + hexOf(b) + "]"
}

func transferRate(o byte) string {
	if o&0x1F == 0x18 {
		return "6.4kb/s回線交換"
	}
	return fmt.Sprintf("速度=予約(%d)", o&0x1F)
}

func decCause(b []byte) string {
	if len(b) < 2 {
		return hexOf(b)
	}
	src := pickMap(map[int]string{0: "移動局自身", 1: "移動局直結私設網", 2: "移動局直結国内網", 3: "中継網", 4: "相手直結国内網", 5: "相手直結私設網", 10: "インタワーキング先"}, int(b[0]&0xF), "値")
	var extra []string
	switch cs := int(b[0]>>5) & 3; cs {
	case 0:
	case 3:
		extra = append(extra, "コーディング標準=生成源特有標準")
	default:
		extra = append(extra, fmt.Sprintf("コーディング標準=予約(%d)", cs))
	}
	i := 1
	if b[0]&0x80 == 0 {
		if v := b[i] & 0x7F; v != 0 {
			extra = append(extra, fmt.Sprintf("仕様種別=予約(%d)", v))
		} else {
			extra = append(extra, "仕様種別=本標準規格")
		}
		i++
	}
	tail := ""
	if len(extra) > 0 {
		tail = " / " + joinComma(extra)
	}
	if i >= len(b) {
		return "生成源=" + src + tail
	}
	v := b[i] & 0x7F
	return fmt.Sprintf("%s / 生成源=%s%s", ccCauseName(v), src, tail)
}

func ccCauseName(v byte) string {
	names := map[byte]string{
		0x01: "欠番", 0x02: "指定中継網へのルートなし", 0x03: "相手へのルートなし", 0x04: "着側単信移動局", 0x05: "着側複信移動局",
		0x06: "チャネル利用不可", 0x07: "呼が設定済みのチャネルへ着呼", 0x08: "ユーザ電源OFF", 0x09: "ユーザ電源異常",
		0x10: "正常切断", 0x11: "着ユーザビジー", 0x12: "着ユーザレスポンスなし", 0x13: "着ユーザ呼出中/応答なし", 0x15: "通信拒否",
		0x16: "相手加入者端末番号変更", 0x1A: "選択されなかったユーザの切断復旧", 0x1B: "相手端末故障中", 0x1C: "無効番号フォーマット",
		0x1D: "ファシリティ拒否", 0x1E: "状態問合わせの応答", 0x1F: "その他の正常クラス",
		0x22: "利用可回線/チャネルなし", 0x26: "網障害", 0x29: "一時的障害", 0x2A: "交換機輻輳", 0x2B: "アクセス情報廃棄",
		0x2C: "要求回線/チャネル利用不可", 0x2F: "その他のリソース使用不可",
		0x31: "QOS利用不可", 0x32: "要求ファシリティ未契約", 0x39: "伝達能力不許可", 0x3A: "現在利用不可伝達能力", 0x3F: "その他のサービス利用不可",
		0x41: "未提供伝達能力指定", 0x42: "未提供チャネル種別指定", 0x45: "未提供ファシリティ要求", 0x46: "非制限デジタル情報伝達能力", 0x4F: "その他のサービス未提供",
		0x51: "無効呼番号使用", 0x52: "無効チャネル番号使用", 0x53: "指定中継呼識別番号使用中", 0x54: "中断呼識別番号使用中", 0x55: "中断呼なし",
		0x56: "指定中断呼切断復旧済", 0x58: "端末属性不一致", 0x5B: "無効中継網選択", 0x5F: "その他の無効メッセージ",
		0x60: "必須情報要素不足", 0x61: "メッセージ種別未定義/未提供", 0x62: "呼状態とメッセージ不一致/種別未定義", 0x63: "情報要素未定義",
		0x64: "情報要素内容無効", 0x65: "呼状態とメッセージ不一致", 0x66: "タイマ満了による回復", 0x6F: "その他の手順誤り",
		0x7F: "その他のインタワーキング",
	}
	if n := names[v]; n != "" {
		return n
	}
	if v == 0x50 {
		return "端末属性不一致（0x50。表 4.3.7.3-13 の印字の形。Q.931 の値は 0x58）"
	}
	return fmt.Sprintf("理由 0x%02X", v)
}

var ccUCC = map[int]string{
	0: "空", 1: "発呼 複信個別", 2: "発呼受付 複信個別", 3: "呼出通知", 6: "発呼グループ", 7: "発呼受付グループ",
	10: "発呼 単信個別", 11: "発呼受付 単信個別", 13: "通信形態受信", 14: "呼設定受信 複信個別", 15: "呼設定受信 単信個別",
	16: "呼設定受信 グループ", 17: "着呼 複信個別", 18: "着呼 単信個別", 19: "着呼 グループ", 20: "呼出中", 21: "応答",
	22: "通信中", 23: "単信通信中", 30: "切断要求", 31: "解放要求", 32: "発呼動作中切断要求", 33: "切断要求 単、グ", 34: "切断通知",
	40: "チャネル切替受付 単、グ", 41: "チャネル切替受付 複信個別", 42: "チャネル切替起動 複信個別", 43: "チャネル切替中 複信個別",
}

var ccNCC = map[int]string{
	0: "空", 1: "発呼 複信", 2: "発呼受付 複信個別", 3: "呼出通知", 4: "着呼出 単信個別", 5: "発呼 単信", 6: "発呼受付 グループ",
	9: "発呼受付 単信個別", 11: "通信要求 複信個別", 12: "通信要求 グループ", 13: "通信要求 単信個別", 14: "通信可否受信 複信個別",
	15: "通信可否受信 単信個別", 16: "着呼", 17: "呼出中", 18: "応答", 20: "通信中", 21: "単信通信中",
	30: "切断要求", 31: "切断通知", 32: "解放要求", 33: "切断要求 単、グ", 34: "接続放棄", 40: "チャネル切替 複信", 41: "チャネル切替 単信",
}

func decCallState(b []byte, dir Dir) string {
	if len(b) < 1 {
		return ""
	}
	v := int(b[0] & 0x3F)
	var who []string
	if n, ok := ccUCC[v]; ok && dir != DirDown {
		who = append(who, fmt.Sprintf("UCC%d %s", v, n))
	}
	if n, ok := ccNCC[v]; ok && dir != DirUp {
		who = append(who, fmt.Sprintf("NCC%d %s", v, n))
	}
	var s string
	switch {
	case len(who) == 0:
		s = fmt.Sprintf("予約(%d)", v)
	case dir == DirUnknown && len(who) == 2:
		s = strings.Join(who, " / ") + "（向き不明: 移動局なら UCC、基地局なら NCC）"
	default:
		s = strings.Join(who, " / ")
	}
	if b[0]>>6 != 0 {
		s += fmt.Sprintf(" コーディング標準=予約(%d)", b[0]>>6)
	}
	return s
}

func decChannelID(b []byte) string {
	if len(b) < 1 {
		return ""
	}
	o := b[0]
	var s []string
	if o&0x08 != 0 {
		s = append(s, "変更不可")
	}
	if o&0x04 != 0 {
		s = append(s, "制御チャネル")
	}
	s = append(s, pick([]string{"チャネルなし", "チャネルあり"}, int(o&3), "予約"))
	return joinComma(s)
}

func decProgress(b []byte) string {
	if len(b) < 2 {
		return hexOf(b)
	}
	return pickMap(map[int]string{1: "呼がISDNエンド・エンドでない", 2: "非ISDN着側アドレス", 3: "非ISDN発側アドレス", 4: "呼のISDNへの復帰", 8: "インバンド信号/パターン利用可能"}, int(b[1]&0x7F), "経過")
}

func decSignal(b []byte) string {
	if len(b) < 1 {
		return ""
	}
	v := b[0]
	names := map[byte]string{0: "ダイヤルトーン", 1: "呼出音(RBT)", 2: "割込みトーン", 3: "網輻輳トーン", 4: "ビジートーン", 5: "確認トーン", 6: "応答トーン",
		8: "オフフック警告トーン", 9: "切断予告トーン", 0x3F: "トーンオフ", 0x4F: "呼出オフ"}
	if n := names[v]; n != "" {
		return n
	}
	if v >= 0x40 && v <= 0x47 {
		return fmt.Sprintf("呼出オン パターン%d", v-0x40)
	}
	return fmt.Sprintf("シグナル 0x%02X", v)
}

func decKeypad(b []byte) string {
	if len(b) < 1 {
		return ""
	}
	i := 1
	kind := "IA5転送"
	if b[0]&0x7F == 0 {
		kind = "PB信号送出要求"
		if b[0]&0x80 == 0 && i < len(b) {
			kind += fmt.Sprintf("(seq %d)", b[i]&0x7F)
			i++
		}
	}
	return kind + ": " + ia5(b[i:])
}

func numberPlan(v byte) string {
	return pickMap(map[int]string{0: "不定", 1: "ISDN/電話", 3: "データ", 4: "テレックス", 8: "国内", 9: "私設網"}, int(v&0xF), "予約")
}

func numberType(o byte) string {
	return pick([]string{"不定", "", "国内番号", "免許人特有番号", "市内番号", "", "短縮番号"}, int(o>>4)&7, "番号種別")
}

func decCalledNumber(b []byte) string {
	if len(b) < 1 {
		return ""
	}
	odd := b[0]&0x80 != 0
	return fmt.Sprintf("%s (%s/%s)", bcdDigits(b[1:], odd), numberType(b[0]), numberPlan(b[0]))
}

func decCallingNumber(b []byte) string {
	if len(b) < 2 {
		return hexOf(b)
	}
	odd := b[1]&0x80 != 0
	pres := pick([]string{"表示可", "表示不可", "表示できる番号なし", "予約"}, int(b[1]>>2)&3, "")
	scr := pick([]string{"ユーザ投入・網検証なし", "ユーザ投入・網検証成功", "ユーザ投入・網検証失敗", "網投入"}, int(b[1]&3), "")
	return fmt.Sprintf("%s (%s/%s/%s/%s)", bcdDigits(b[2:], odd), numberType(b[0]), numberPlan(b[0]), pres, scr)
}

func decSubaddress(b []byte) string {
	if len(b) < 1 {
		return ""
	}
	odd := b[0]&0x08 != 0
	typ := pickMap(map[int]string{2: "本標準規格特有"}, int(b[0]>>4)&7, "種別予約")
	return fmt.Sprintf("%s (%s)", bcdDigits(b[1:], odd), typ)
}

func decFacility(b []byte) string {
	if len(b) < 1 {
		return ""
	}
	var s []string
	sid := b[0]
	name := pickMap(map[int]string{0x11: "付加サービスアプリケーション"}, int(sid&0x1F), "予約")
	if sid&0xE0 != 0x80 {
		name += fmt.Sprintf("（拡張・予備ビット %d）", sid>>5)
	}
	s = append(s, "サービス識別子="+name)
	i := 1
	for i < len(b) {
		tag := b[i]
		kind := pickMap(map[int]string{1: "インボーク", 2: "リターンリザルト", 3: "リターンエラー", 4: "リジェクト"}, int(tag&0x1F), "予約")
		if tag&0xE0 != 0xA0 {
			kind += fmt.Sprintf("（クラス/形式 %d）", tag>>5)
		}
		if i+1 >= len(b) {
			s = append(s, "コンポーネント "+kind+" の長さが無い")
			break
		}
		l := b[i+1]
		if l&0x80 != 0 {
			s = append(s, "コンポーネント "+kind+" の長さフォーマット=予約 "+hexOf(b[i+1:]))
			break
		}
		start, end := i+2, i+2+int(l)
		if end > len(b) {
			s = append(s, fmt.Sprintf("コンポーネント %s が短い（長さ %d）%s", kind, l, hexOf(b[start:])))
			break
		}
		s = append(s, fmt.Sprintf("コンポーネント %s 長さ %d: %s", kind, l, berText(b[start:end])))
		i = end
	}
	return joinComma(s)
}

func berText(b []byte) string {
	var s []string
	for i := 0; i < len(b); {
		if i+1 >= len(b) || b[i]&0x1F == 0x1F || b[i+1]&0x80 != 0 || i+2+int(b[i+1]) > len(b) {
			return hexOf(b)
		}
		v := b[i+2 : i+2+int(b[i+1])]
		s = append(s, fmt.Sprintf("%02X=%s", b[i], hexOf(v)))
		i += 2 + len(v)
	}
	return strings.Join(s, " ")
}

func featureID(b []byte) (int, int) {
	v, n := 0, 0
	for n < len(b) && n < 2 {
		v = v<<7 | int(b[n]&0x7F)
		n++
		if b[n-1]&0x80 != 0 {
			break
		}
	}
	return v, n
}

func decFeatureAct(b []byte) string {
	if len(b) < 1 {
		return ""
	}
	id, n := featureID(b)
	s := fmt.Sprintf("フィーチャ識別子番号=%d（表 4.3.7.3-15 は内容未規定）", id)
	if n < len(b) {
		s += " 余り " + hexOf(b[n:])
	}
	return s
}

func decFeatureInd(b []byte) string {
	if len(b) < 1 {
		return ""
	}
	id, n := featureID(b)
	s := fmt.Sprintf("フィーチャ識別子番号=%d（表 4.3.7.3-15 は内容未規定）", id)
	if n < len(b) {
		o := b[n]
		s += " 状態=" + pick([]string{"非活性中", "活性中", "プロンプト（ユーザの入力待ち）", "実行中"}, int(o&0xF), "予約")
		if o>>4 != 0 {
			s += fmt.Sprintf(" 予備ビット=%d", o>>4)
		}
		n++
	}
	if n < len(b) {
		s += " 余り " + hexOf(b[n:])
	}
	return s
}

func decHLC(b []byte) string {
	if len(b) < 2 {
		return hexOf(b)
	}
	cs := pickMap(map[int]string{0: "本標準規格", 3: "免許人特有"}, int(b[0]>>5)&3, "予約")
	interp := pickMap(map[int]string{4: "最初の高位レイヤ特性識別"}, int(b[0]>>2)&7, "予約")
	prof := pickMap(map[int]string{0: "高位レイヤプロトコルプロファイル"}, int(b[0]&3), "予約")
	ch := pickMap(map[int]string{1: "電話"}, int(b[1]&0x7F), "予約")
	return fmt.Sprintf("特性=%s（コーディング標準=%s, 解釈法=%s, 表現法=%s）", ch, cs, interp, prof)
}

func decUserUser(b []byte) string {
	if len(b) < 1 {
		return ""
	}
	switch b[0] {
	case 0x04:
		return "IA5: " + ia5(b[1:])
	case 0x08:
		return "CCメッセージ: " + hexOf(b[1:])
	case 0x00:
		return "ユーザ特有: " + hexOf(b[1:])
	}
	return fmt.Sprintf("プロトコル 0x%02X: %s", b[0], hexOf(b[1:]))
}

func decPrimaryZone(b []byte) string {
	if len(b) < 2 {
		return hexOf(b)
	}
	return fmt.Sprintf("制御ゾーン=%d 基地局=%d", b[0], b[1])
}

func decMSNumber(b []byte) string {
	var v uint64
	for _, o := range b {
		v = v<<7 | uint64(o&0x7F)
	}
	return fmt.Sprint(v)
}

func pickMap(m map[int]string, v int, fallback string) string {
	if s, ok := m[v]; ok {
		return s
	}
	return fmt.Sprintf("%s(%d)", fallback, v)
}
