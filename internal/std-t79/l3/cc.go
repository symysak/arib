package l3

import (
	"fmt"
	"github.com/symysak/arib/internal/std-t79/numbering"
	"strings"
)


var ccNames = map[uint8]string{
	0x01: "呼出",
	0x02: "呼設定受付",
	0x07: "応答",
	0x0F: "応答確認",
	0x03: "経過表示",
	0x05: "呼設定",
	0x11: "再呼設定受付",
	0x12: "通信形態",
	0x13: "通信可否",
	0x14: "再呼設定拒否",
	0x15: "単信呼設定",
	0x16: "単信再呼設定",
	0x17: "複信呼設定",
	0x18: "複信再呼設定",
	0x19: "グループ呼設定",
	0x1A: "直接通信単信呼設定",
	0x1B: "直接通信通信形態",
	0x20: "ユーザ情報",
	0x45: "切断",
	0x4D: "解放",
	0x5A: "解放完了",
	0x62: "ファシリティ",
	0x79: "輻輳制御",
	0x7B: "付加情報",
	0x7D: "状態表示",
}

var ccSetupTypes = map[uint8]bool{0x05: true, 0x15: true, 0x16: true, 0x17: true, 0x18: true, 0x19: true, 0x1A: true}

var ccReleaseTypes = map[uint8]bool{0x45: true, 0x4D: true, 0x5A: true}

type ccKind int

const (
	ccLen   ccKind = iota
	ccFixed
	ccExt
)

type ccIE struct {
	name  string
	kind  ccKind
	fixed int
	dec   func(b []byte, st *ccState) string
}

var ccIE0 = map[byte]ccIE{
	0x04: {"伝達能力", ccLen, 0, ccDecBearer},
	0x08: {"理由表示", ccLen, 0, ccDecCause},
	0x14: {"呼状態", ccLen, 0, ccDecCallState},
	0x18: {"チャネル識別子", ccLen, 0, ccDecChannelID},
	0x1C: {"ファシリティ", ccLen, 0, ccDecFacility},
	0x1E: {"経過識別子", ccFixed, 2, ccDecProgress},
	0x28: {"表示", ccLen, 0, ccDecDisplay},
	0x2C: {"キーパッドファシリティ", ccLen, 0, ccDecKeypad},
	0x34: {"シグナル", ccFixed, 1, ccDecSignal},
	0x38: {"フィーチャアクティベーション", ccLen, 0, ccDecFeatureAct},
	0x39: {"フィーチャインディケーション", ccLen, 0, ccDecFeatureInd},
	0x6C: {"発番号", ccLen, 0, ccDecCalling},
	0x6D: {"発サブアドレス（第 1）", ccLen, 0, ccDecSubaddress1},
	0x70: {"着番号", ccLen, 0, ccDecCalled},
	0x71: {"着サブアドレス（第 1）", ccLen, 0, ccDecSubaddress1},
	0x7D: {"高位レイヤ整合性", ccFixed, 2, ccDecHLC},
	0x7E: {"ユーザ・ユーザ", ccLen, 0, ccDecUserUser},
}

var ccIE6 = map[byte]ccIE{
	0x01: {"製造者番号", ccFixed, 2, ccDecProducer},
	0x04: {"非音声通信用伝達能力", ccLen, 0, ccDecDataBearer},
	0x0F: {"移動局番号", ccExt, 0, ccDecMSNumber},
	0x1F: {"旧ゾーン情報", ccFixed, 2, ccDecPrimaryZone},
	0x6E: {"発サブアドレス（第 2）", ccLen, 0, ccDecSubaddress},
	0x72: {"着サブアドレス（第 2）", ccLen, 0, ccDecSubaddress},
	0x7E: {"免許人固有ユーザ・ユーザ", ccLen, 0, ccDecHex},
}

type ccState struct {
	call *Call
	seen []ccSeen
	dir  int
}

func parseCC(b []byte) Message { return parseCCDir(b, 0) }

func parseCCDir(b []byte, dir int) Message {
	m := Message{Entity: "CC", Raw: b}
	r := &reader{b: b}
	pd, ok := r.byte1("プロトコル識別子")
	if !ok {
		m.Err = r.err
		return m
	}
	pdText := "標準エアインタフェース CC"
	if pd != 0x08 {
		pdText = fmt.Sprintf("0x%02X（標準 CC ではない）", pd)
		m.Err = "プロトコル識別子が標準 CC (0x08) でない"
	}
	m.Fields = append(m.Fields, Field{"プロトコル識別子", pdText})
	cr, ok := r.byte1("呼番号／付加サービス識別子")
	if !ok {
		m.Err = r.err
		return m
	}
	call := &Call{ServiceID: int(cr >> 4), Ref: int(cr & 7), CalledPlan: -1, CallingPlan: -1, CalledType: -1, CallingType: -1}
	m.Call = call
	m.Fields = append(m.Fields,
		Field{"呼番号", ccRefText(call.Ref)},
		Field{"付加サービス識別子", pick([]string{"移動通信網特有付加サービス手順", "Q.932 準拠付加サービス手順"}, call.ServiceID, "予約")})
	if cr&0x08 != 0 {
		m.Fields = append(m.Fields, Field{"予約ビット（オクテット 2 ビット 4）", "1"})
	}
	t, ok := r.byte1("メッセージ種別")
	if !ok {
		m.Err = r.err
		call.Text = ccCallText(call, "")
		return m
	}
	m.Type = t & 0x7F
	m.Name = ccNames[m.Type]
	if t&0x80 != 0 {
		m.Fields = append(m.Fields, Field{"メッセージ種別のビット 8", "1（規定は 0）"})
	}
	call.MsgType = m.Type
	call.Setup = ccSetupTypes[m.Type]
	call.Release = ccReleaseTypes[m.Type]

	st := &ccState{call: call, dir: dir}
	if err := ccScanIEs(r, &m, st); err != "" && m.Err == "" {
		m.Err = err
	}
	ccValidate(&m, m.Type, st.seen, call.ServiceID)
	for _, n := range m.Notes {
		m.Fields = append(m.Fields, Field{"規格の検査", n})
	}
	name := m.Name
	if name == "" {
		name = fmt.Sprintf("未定義(0x%02X)", m.Type)
	}
	call.Text = ccCallText(call, name)
	return m
}

func ccScanIEs(r *reader, m *Message, st *ccState) string {
	codeset := 0
	for r.left() > 0 {
		id, _ := r.byte1("情報要素識別子")
		if id&0x80 != 0 {
			cs := codeset
			name, val := ccSingle(id, &codeset)
			m.Fields = append(m.Fields, Field{name, val})
			sh := -1
			if id&0xF8 == 0x90 {
				sh = int(id & 7)
			}
			st.seen = append(st.seen, ccSeen{cs: cs, id: id, single: true, shift: sh, octets: 1, name: name})
			continue
		}
		var def ccIE
		var known bool
		switch codeset {
		case 0:
			def, known = ccIE0[id]
		case 6:
			def, known = ccIE6[id]
		}
		if !known {
			def = ccIE{fmt.Sprintf("情報要素 0x%02X（コード群 %d）", id, codeset), ccLen, 0, ccDecHex}
		}
		var body []byte
		var short string
		switch def.kind {
		case ccLen:
			n, ok := r.byte1(def.name + " の内容長")
			if !ok {
				m.Fields = append(m.Fields, Field{def.name, "（内容長が無い）"})
				return r.err
			}
			if int(n) > r.left() {
				short = fmt.Sprintf("%s が足りない（内容長 %d、残り %d）", def.name, n, r.left())
				body = r.rest()
			} else {
				body = r.take(int(n), def.name)
			}
			if n == 0 {
				m.Fields = append(m.Fields, Field{def.name, "（内容長 0: 無いものとして扱う）"})
				continue
			}
		case ccFixed:
			if def.fixed > r.left() {
				short = fmt.Sprintf("%s が足りない（%d オクテット中 %d）", def.name, def.fixed, r.left())
				body = r.rest()
			} else {
				body = r.take(def.fixed, def.name)
			}
		case ccExt:
			j := r.i
			for j < len(r.b) && r.b[j]&0x80 == 0 {
				j++
			}
			if j < len(r.b) {
				j++
			} else {
				short = def.name + " の終わり（拡張ビット 1）が無い"
			}
			body = r.take(j-r.i, def.name)
		}
		val := def.dec(body, st)
		if short != "" {
			val += "（途中まで）"
		}
		octets := 1 + len(body)
		if def.kind == ccLen {
			octets++
		}
		st.seen = append(st.seen, ccSeen{cs: codeset, id: id, shift: -1, octets: octets, name: def.name})
		m.Fields = append(m.Fields, Field{def.name, val})
		if short != "" {
			return short
		}
	}
	return ""
}

func ccSingle(id byte, codeset *int) (string, string) {
	if id&0xF8 == 0x90 {
		cs := int(id & 7)
		v := pickMap(map[int]string{5: "コード群 5（国内特有）", 6: "コード群 6（網（免許人）特有）", 7: "コード群 7（ユーザ特有）"}, cs, "適用不可／予約 コード群")
		if cs <= *codeset {
			v += "（より低いコード群へのシフト。規定外）"
		}
		*codeset = cs
		return "固定シフト", v
	}
	switch *codeset {
	case 0:
		switch {
		case id == 0xA0:
			return "モアデータ", "あり"
		case id&0xF0 == 0xB0:
			return "輻輳制御レベル", pickMap(map[int]string{0: "RR（受信者受信可）", 15: "RNR（受信者受信不可）"}, int(id&0xF), "予約")
		}
	case 5:
		if id == 0x82 {
			return "再呼表示", "あり（網側から再呼の可能性）"
		}
	case 6:
		if id&0xF0 == 0xF0 {
			return "通信可否", pick([]string{"可", "", "", "不可"}, int(id&3), "予約")
		}
	}
	return fmt.Sprintf("単数オクテット情報要素（コード群 %d）", *codeset), fmt.Sprintf("0x%02X", id)
}

func ccRefText(ref int) string {
	switch ref {
	case 0:
		return "0（単信通信時／複信通信時）"
	case 7:
		return "7（ダミー）"
	}
	return fmt.Sprintf("%d（予約）", ref)
}

func ccCallText(c *Call, name string) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "呼番号 %d（付加 %d）", c.Ref, c.ServiceID)
	sb.WriteString(name)
	switch {
	case c.Calling != "" && c.Called != "":
		fmt.Fprintf(&sb, " 発 %s → 着 %s", c.Calling, c.Called)
	case c.Calling != "":
		fmt.Fprintf(&sb, " 発 %s", c.Calling)
	case c.Called != "":
		fmt.Fprintf(&sb, " 着 %s", c.Called)
	}
	if len(c.Bearers) > 0 {
		var names []string
		for _, br := range c.Bearers {
			names = append(names, ccBearerShort(br))
		}
		sb.WriteString(" 伝達能力 " + strings.Join(names, "/"))
	}
	if c.Cause != "" {
		sb.WriteString(" 理由 " + c.Cause)
	}
	if c.Display != "" {
		sb.WriteString(" 表示「" + c.Display + "」")
	}
	return sb.String()
}

func BearerCodec(m Message) string {
	if m.Call == nil {
		return ""
	}
	for _, br := range m.Call.Bearers {
		if br.Codec != "" {
			return br.Codec
		}
	}
	return ""
}

func BearerNonVoice(m Message) bool {
	return m.Call != nil && len(m.Call.Bearers) > 0 && m.Call.Bearers[0].NonVoice
}


func ccDecHex(b []byte, _ *ccState) string { return hexOf(b) }

var ccInfoCaps = map[int]string{
	0x00: "音声",
	0x14: "非音声（TCH 型 1 スロット）", 0x15: "非音声（TCH 型 2 スロット）", 0x16: "非音声（TCH 型 3 スロット）", 0x17: "非音声（TCH 型 4 スロット）",
	0x18: "非音声（UDCH 型 1 スロット）", 0x19: "非音声（UDCH 型 2 スロット）", 0x1A: "非音声（UDCH 型 3 スロット）", 0x1B: "非音声（UDCH 型 4 スロット）",
	0x04: "音声＋非音声（TCH 型 1 スロット）", 0x05: "音声＋非音声（TCH 型 2 スロット）", 0x06: "音声＋非音声（TCH 型 3 スロット）",
	0x08: "音声＋非音声（UDCH 型 1 スロット）", 0x09: "音声＋非音声（UDCH 型 2 スロット）", 0x0A: "音声＋非音声（UDCH 型 3 スロット）",
}

var ccL1Protocols = map[int]string{0x11: "本標準規格速度整合", 0x02: "G.711 μ-law 音声", 0x1A: "ELCELP 音声"}

var ccL1Codec = map[int]string{0x1A: "elcelp"}

var ccModes = []string{0: "単信個別", 1: "複信個別", 2: "グループ", 3: "同報"}

func ccDecBearer(b []byte, st *ccState) string {
	br := Bearer{Raw: append([]byte(nil), b...)}
	var parts []string
	i := 0
	next := func() (byte, bool) {
		if i >= len(b) {
			return 0, false
		}
		i++
		return b[i-1], true
	}
	if o, ok := next(); ok {
		if o&0x40 != 0 {
			parts = append(parts, "網特有標準")
		}
		parts = append(parts, pickMap(ccInfoCaps, int(o&0x1F), "情報転送能力 予約"))
		br.NonVoice = o&0x1F >= 0x14 && o&0x1F <= 0x1B
	}
	if o, ok := next(); ok {
		mode := "回線交換"
		if (o>>5)&3 != 0 {
			mode = fmt.Sprintf("転送モード 予約(%d)", (o>>5)&3)
		}
		rate := "6.4kbps"
		if o&0x1F != 0x18 {
			rate = fmt.Sprintf("速度 予約(%d)", o&0x1F)
		}
		parts = append(parts, mode+" "+rate)
		if o&0x80 == 0 {
			if a, ok := next(); ok {
				cm := &CommMode{Emergency: a&0x10 != 0, Form: int(a>>2) & 3}
				m := cm.String()
				if a&0x02 != 0 {
					cm.Form = -1
					m = fmt.Sprintf("%s 通信形態 予約(%d)", map[bool]string{true: "緊急", false: "通常"}[cm.Emergency], (a>>1)&7)
				}
				br.Mode = cm
				parts = append(parts, m)
				if (a>>5)&3 != 1 {
					parts = append(parts, fmt.Sprintf("構造 予約(%d)", (a>>5)&3))
				}
				if a&1 != 0 {
					parts = append(parts, "呼設定法 予約")
				}
				if a&0x80 == 0 {
					if c, ok := next(); ok {
						if (c>>5)&3 != 0 {
							parts = append(parts, fmt.Sprintf("対称性 予約(%d)", (c>>5)&3))
						}
						if c&0x1F != 0x18 {
							parts = append(parts, fmt.Sprintf("着→発 速度 予約(%d)", c&0x1F))
						}
					}
				}
			}
		}
	}
	if o, ok := next(); ok {
		l1 := int(o & 0x1F)
		parts = append(parts, pickMap(ccL1Protocols, l1, "レイヤ 1 プロトコル 予約"))
		br.Codec = ccL1Codec[l1]
		if (o>>5)&3 != 1 {
			parts = append(parts, fmt.Sprintf("レイヤ 1 識別 予約(%d)", (o>>5)&3))
		}
		if o&0x80 == 0 {
			if a, ok := next(); ok {
				s := "同期"
				if a&0x40 != 0 {
					s = "同期／非同期 予約"
				}
				if a&0x1F == 0x01 {
					s += " ユーザ速度 標準"
				} else {
					s += fmt.Sprintf(" ユーザ速度 予約(%d)", a&0x1F)
				}
				parts = append(parts, s)
			}
		}
	}
	if i < len(b) {
		parts = append(parts, "後続 "+hexOf(b[i:]))
	}
	br.Text = strings.Join(parts, " / ")
	st.call.Bearers = append(st.call.Bearers, br)
	if st.call.Mode == nil && br.Mode != nil {
		st.call.Mode = br.Mode
	}
	return br.Text + " [" + hexOf(b) + "]"
}

func ccBearerShort(br Bearer) string {
	if br.Codec == "elcelp" {
		return "ELCELP"
	}
	if s, _, ok := strings.Cut(br.Text, " / "); ok {
		return s
	}
	return br.Text
}

var ccCauseNames = map[int]string{
	0x01: "欠番", 0x02: "指定中継網へのルートなし", 0x03: "相手へのルートなし", 0x04: "着側単信移動局", 0x05: "着側複信移動局",
	0x06: "チャネル利用不可", 0x07: "呼が設定済のチャネルへ着呼", 0x08: "ユーザ電源 OFF", 0x09: "ユーザ電源異常",
	0x10: "正常切断", 0x11: "着ユーザビジー", 0x12: "着ユーザレスポンスなし", 0x13: "着ユーザ呼出中／応答なし",
	0x14: "ドライブモード設定による切断", 0x15: "通信拒否", 0x16: "相手加入者端末番号変更", 0x1A: "選択されなかったユーザの切断復旧",
	0x1B: "相手端末故障中", 0x1C: "無効番号フォーマット", 0x1D: "ファシリティ拒否", 0x1E: "状態問合わせの応答", 0x1F: "その他の正常クラス",
	0x22: "利用可回線／チャネルなし", 0x23: "要求チャネル数不足", 0x26: "網障害", 0x29: "一時的障害", 0x2A: "交換機輻輳",
	0x2B: "アクセス情報廃棄", 0x2C: "要求回線／チャネル利用不可", 0x2F: "その他のリソース使用不可クラス",
	0x31: "QOS 利用不可", 0x32: "要求ファシリティ未契約", 0x39: "伝達能力不許可", 0x3A: "現在利用不可伝達能力",
	0x3F: "その他のサービス又はオプションの利用不可クラス",
	0x41: "未提供伝達能力指定", 0x42: "未提供チャネル種別指定", 0x45: "未提供ファシリティ要求", 0x46: "非制限デジタル情報伝達能力",
	0x4F: "その他のサービス又はオプションの未提供クラス",
	0x51: "無効呼番号使用", 0x52: "無効チャネル番号使用", 0x53: "指定された中継呼識別番号使用中", 0x54: "中断呼識別番号使用中",
	0x55: "中断呼なし", 0x56: "指定中断呼切断復旧済", 0x58: "端末属性不一致", 0x5B: "無効中継網選択", 0x5F: "その他の無効メッセージクラス",
	0x60: "必須情報要素不足", 0x61: "メッセージ種別未定義又は未提供", 0x62: "呼状態とメッセージ不一致又はメッセージ種別未定義",
	0x63: "情報要素未定義", 0x64: "情報要素内容無効", 0x65: "呼状態とメッセージ不一致", 0x66: "タイマー満了による回復",
	0x6F: "その他の手順誤りクラス",
	0x7F: "その他のインタワーキングクラス",
}

var ccCauseClasses = []string{"正常イベント", "正常イベント", "リソース使用不可", "サービス利用不可", "サービス未提供", "無効メッセージ", "手順誤り", "インタワーキング"}

var ccLocations = map[int]string{0: "移動局自身", 1: "移動局が直接接続する私設網", 2: "移動局が直接接続する国内網", 3: "中継網",
	4: "相手ユーザが直接接続する国内網", 5: "相手ユーザが直接接続する私設網", 10: "インタワーキング先の網"}

func ccDecCause(b []byte, st *ccState) string {
	if len(b) == 0 {
		return ""
	}
	loc := pickMap(ccLocations, int(b[0]&0xF), "生成源 予約")
	std := ""
	switch (b[0] >> 5) & 3 {
	case 0:
	case 3:
		std = " 生成源特有標準"
	default:
		std = " コーディング標準 予約"
	}
	i := 1
	spec := ""
	if b[0]&0x80 == 0 && i < len(b) {
		if b[i]&0x7F != 0 {
			spec = fmt.Sprintf(" 仕様種別 予約(%d)", b[i]&0x7F)
		}
		i++
	}
	if i >= len(b) {
		return fmt.Sprintf("（理由表示値なし）生成源 %s%s [%s]", loc, std, hexOf(b))
	}
	v := int(b[i] & 0x7F)
	sum := fmt.Sprintf("#%d %s", v, pickMap(ccCauseNames, v, ccCauseClasses[v>>4]+"クラス 予約"))
	if st.call.Cause == "" {
		st.call.Cause = sum
	}
	out := fmt.Sprintf("%s（%sクラス、生成源 %s%s%s）", sum, ccCauseClasses[v>>4], loc, std, spec)
	if i+1 < len(b) {
		out += " 後続 " + hexOf(b[i+1:])
	}
	return out
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

func ccDecCallState(b []byte, st *ccState) string {
	if len(b) == 0 {
		return ""
	}
	v := int(b[0] & 0x3F)
	var who []string
	if n, ok := ccUCC[v]; ok && st.dir != 1 {
		who = append(who, fmt.Sprintf("移動局 UCC%d %s", v, n))
	}
	if n, ok := ccNCC[v]; ok && st.dir != 2 {
		who = append(who, fmt.Sprintf("基地局 NCC%d %s", v, n))
	}
	if len(who) == 0 {
		who = append(who, "予約")
	}
	s := fmt.Sprintf("%d（%s）", v, strings.Join(who, " / "))
	if b[0]>>6 != 0 {
		s += fmt.Sprintf(" コーディング標準 予約(%d)", b[0]>>6)
	}
	return s
}

func ccDecChannelID(b []byte, _ *ccState) string {
	if len(b) == 0 {
		return ""
	}
	o := b[0]
	var s []string
	if o&0x40 != 0 {
		s = append(s, "インタフェース識別 予約")
	}
	if o&0x20 != 0 {
		s = append(s, "インタフェースタイプ 予約")
	}
	if o&0x08 != 0 {
		s = append(s, "変更不可")
	}
	if o&0x04 != 0 {
		s = append(s, "制御チャネル")
	}
	s = append(s, pick([]string{"チャネルなし", "チャネルあり"}, int(o&3), "情報チャネル選択 予約"))
	if len(b) > 1 {
		s = append(s, "後続 "+hexOf(b[1:]))
	}
	return strings.Join(s, "、")
}

func ccDecFacility(b []byte, _ *ccState) string {
	if len(b) == 0 {
		return ""
	}
	parts := []string{pickMap(map[int]string{0x11: "付加サービスアプリケーション"}, int(b[0]&0x1F), "サービス識別子 予約")}
	i := 1
	for i < len(b) {
		tag := b[i]
		name := pickMap(map[int]string{1: "インボーク", 2: "リターンリザルト", 3: "リターンエラー", 4: "リジェクト"}, int(tag&0x1F), "コンポーネント 予約")
		if i+1 >= len(b) {
			parts = append(parts, name+"（長さが無い）")
			break
		}
		n := int(b[i+1] & 0x7F)
		start := i + 2
		end := start + n
		if end > len(b) {
			parts = append(parts, fmt.Sprintf("%s %s（長さ %d に足りない）", name, hexOf(b[start:]), n))
			break
		}
		parts = append(parts, fmt.Sprintf("%s %s", name, hexOf(b[start:end])))
		i = end
	}
	return strings.Join(parts, " / ")
}

func ccDecProgress(b []byte, _ *ccState) string {
	if len(b) < 2 {
		return hexOf(b)
	}
	desc := pickMap(map[int]string{1: "呼が ISDN エンド・エンドでない", 2: "非 ISDN 着側アドレス", 3: "非 ISDN 発側アドレス",
		4: "呼の ISDN への復帰", 8: "インバンド信号ないし適当なパターンが利用可能"}, int(b[1]&0x7F), "経過内容 予約")
	return fmt.Sprintf("%s（生成源 %s）", desc, pickMap(ccLocations, int(b[0]&0xF), "予約"))
}

func ccDecDisplay(b []byte, st *ccState) string {
	s := ccIA5(b)
	if st.call.Display == "" {
		st.call.Display = s
	}
	return s
}

func ccIA5(b []byte) string {
	var sb strings.Builder
	kana := false
	for _, c := range b {
		switch {
		case c == 0x0E:
			kana = true
		case c == 0x0F:
			kana = false
		case kana && c >= 0x21 && c <= 0x5F:
			sb.WriteString(IA5Kana([]byte{c + 0x80}))
		default:
			sb.WriteString(IA5Kana([]byte{c}))
		}
	}
	return sb.String()
}

func ccDecKeypad(b []byte, _ *ccState) string {
	if len(b) == 0 {
		return ""
	}
	kind := pick([]string{"PB 信号送出要求", "IA5 キャラクタ転送"}, int(b[0]&0x7F), "キーパッド識別子 予約")
	i := 1
	if b[0]&0x80 == 0 && i < len(b) {
		kind += fmt.Sprintf("（シーケンス番号 %d）", b[i]&0x7F)
		i++
	}
	return kind + ": " + ccIA5(b[i:])
}

func ccFeatureID(b []byte) (int, int) {
	if len(b) == 0 {
		return 0, 0
	}
	v := int(b[0] & 0x7F)
	if b[0]&0x80 == 0 && len(b) > 1 {
		return v<<7 | int(b[1]&0x7F), 2
	}
	return v, 1
}

func ccDecFeatureAct(b []byte, _ *ccState) string {
	id, n := ccFeatureID(b)
	s := fmt.Sprintf("フィーチャ識別子 %d", id)
	if n < len(b) {
		s += " 後続 " + hexOf(b[n:])
	}
	return s
}

func ccDecFeatureInd(b []byte, _ *ccState) string {
	id, n := ccFeatureID(b)
	s := fmt.Sprintf("フィーチャ識別子 %d", id)
	if n < len(b) {
		s += " " + pick([]string{"非活性中", "活性中", "プロンプト", "実行中"}, int(b[n]&0xF), "状態 予約")
		if n+1 < len(b) {
			s += " 後続 " + hexOf(b[n+1:])
		}
	}
	return s
}

var ccPlans = map[int]string{0: "不定", 1: "ISDN／電話番号計画", 3: "データ番号計画", 4: "テレックス番号計画", 8: "国内番号計画", 9: "私設網番号計画"}

func ccDecCalled(b []byte, st *ccState) string {
	if len(b) == 0 {
		return ""
	}
	odd := b[0]&0x80 != 0
	d := bcdDigits(b[1:], odd, true)
	if st.call.Called == "" {
		st.call.Called, st.call.CalledPlan, st.call.CalledType = d, int(b[0]&0xF), int(b[0]>>4)&7
	}
	return fmt.Sprintf("%s（%s／%s）", d, pick(mmNumberTypes, int(b[0]>>4)&7, "番号種別 予約"), pickMap(ccPlans, int(b[0]&0xF), "番号計画 予約"))
}

func ccDecCalling(b []byte, st *ccState) string {
	if len(b) < 2 {
		return hexOf(b)
	}
	odd := b[1]&0x80 != 0
	d := bcdDigits(b[2:], odd, true)
	if st.call.Calling == "" {
		st.call.Calling, st.call.CallingPlan, st.call.CallingType = d, int(b[0]&0xF), int(b[0]>>4)&7
	}
	pres := pick([]string{"表示可", "表示不可", "相互接続条件により表示できる番号なし"}, int(b[1]>>2)&3, "表示識別子 予約")
	scr := pick([]string{"ユーザ投入・網検証なし", "ユーザ投入・網検証成功", "ユーザ投入・網検証失敗", "網投入"}, int(b[1]&3), "")
	return fmt.Sprintf("%s（%s／%s、%s、%s）", d, pick(mmNumberTypes, int(b[0]>>4)&7, "番号種別 予約"),
		pickMap(ccPlans, int(b[0]&0xF), "番号計画 予約"), pres, scr)
}

func ccDecSubaddress(b []byte, _ *ccState) string {
	if len(b) == 0 {
		return ""
	}
	if (b[0]>>4)&7 != 2 {
		return fmt.Sprintf("サブアドレス種別 予約(%d) %s", (b[0]>>4)&7, hexOf(b[1:]))
	}
	return bcdDigits(b[1:], b[0]&0x08 != 0, true)
}

func ccDecSubaddress1(b []byte, st *ccState) string {
	v := ccDecSubaddress(b, st)
	if len(v) > 0 && v[0] == '*' {
		st.call.SubStar = true
		v += "（＊: 移動局接続 PBX の番号）"
	}
	return v
}

func ccDecHLC(b []byte, _ *ccState) string {
	if len(b) < 2 {
		return hexOf(b)
	}
	return fmt.Sprintf("%s [%s]", pickMap(map[int]string{1: "電話"}, int(b[1]&0x7F), "特性識別 予約"), hexOf(b))
}

func ccDecSignal(b []byte, _ *ccState) string {
	if len(b) == 0 {
		return ""
	}
	v := int(b[0])
	names := map[int]string{0x00: "ダイヤルトーン オン", 0x01: "呼出音（RBT）オン", 0x02: "割込みトーン オン", 0x03: "網輻輳トーン オン",
		0x04: "ビジートーン オン", 0x05: "確認トーン オン", 0x06: "応答トーン オン", 0x08: "オフフック警告トーン オン",
		0x09: "切断予告トーン オン", 0x3F: "トーン オフ", 0x4F: "呼出 オフ"}
	if n, ok := names[v]; ok {
		return n
	}
	if v >= 0x40 && v <= 0x47 {
		return fmt.Sprintf("呼出 オン パターン %d", v-0x40)
	}
	return fmt.Sprintf("予約(0x%02X)", v)
}

func ccDecUserUser(b []byte, _ *ccState) string {
	if len(b) == 0 {
		return ""
	}
	switch b[0] {
	case 0x00:
		return "ユーザ特有: " + hexOf(b[1:])
	case 0x04:
		return "IA5: " + ccIA5(b[1:])
	case 0x08:
		return "CC メッセージ: " + hexOf(b[1:])
	}
	return fmt.Sprintf("プロトコル 0x%02X: %s", b[0], hexOf(b[1:]))
}

func ccDecProducer(b []byte, _ *ccState) string {
	if len(b) < 2 {
		return hexOf(b)
	}
	v := int(b[0])<<8 | int(b[1])
	return fmt.Sprintf("製造者番号 %s?（下位 10 ビット）免許人固有 0x%02X? [%s]", makerText(v&0x3FF), v>>10, hexOf(b))
}

func ccDecDataBearer(b []byte, _ *ccState) string {
	if len(b) == 0 {
		return ""
	}
	s := map[bool]string{true: "誤り訂正有り", false: "誤り訂正無し"}[b[0]&1 != 0]
	if len(b) > 1 {
		s += " 免許人オプション " + hexOf(b[1:])
	}
	return s
}

func ccDecMSNumber(b []byte, st *ccState) string {
	var v uint64
	for _, o := range b {
		v = v<<7 | uint64(o&0x7F)
	}
	if m, ok := numbering.ParseMSI(v, 7*len(b)); ok {
		if st != nil && st.call != nil && m.System.Org != "" {
			st.call.MSOrg = m.System.Org
		}
		return fmt.Sprintf("%s [%s]", m.Text(), hexOf(b))
	}
	return fmt.Sprintf("%d [%s]", v, hexOf(b))
}

func ccDecPrimaryZone(b []byte, _ *ccState) string {
	if len(b) < 2 {
		return hexOf(b)
	}
	return fmt.Sprintf("制御ゾーン番号 %d 基地局番号 %d", b[0], b[1])
}
