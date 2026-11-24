package l3

import (
	"fmt"
	"math/big"
	"strings"
)


func ccBits(o byte, hi, lo int) int { return int(o>>(lo-1)) & (1<<(hi-lo+1) - 1) }

func ccJoin(parts []string) string { return strings.Join(parts, "、") }

func ccExtNote(st *ccState, o byte, want int, what string) {
	if bit(o, 8) != want {
		st.note("%sの%sの拡張ビットが %d（図は %d）", ccIEName(st.ie), what, bit(o, 8), want)
	}
}


var ccCodingStd = []string{0: "π／4 シフト QPSK 方式標準", 3: "網特有標準"}

var ccInfoCaps = map[int]string{0x00: "音声", 0x17: "音声＋非音声", 0x09: "高速非音声"}

var ccModes = map[int]string{0: "単信個別", 2: "複信個別", 4: "グループ", 6: "同報", 7: "強制一斉指令", 1: "デフォルト"}

var ccL1Protocols = map[int]string{0x11: "π／4 シフト QPSK 方式標準規格速度整合", 0x02: "勧告 G.711 μ−law 音声", 0x1A: "ELCELP 音声"}

var ccL1Codec = map[int]string{0x1A: "elcelp"}

func ccDecBearer(b []byte, st *ccState) string {
	br := Bearer{Raw: append([]byte(nil), b...)}
	var parts []string
	i := 0
	next := func(what string) (byte, bool) {
		if i >= len(b) {
			st.note("伝達能力のオクテット %s が無い", what)
			return 0, false
		}
		i++
		return b[i-1], true
	}
	rate := func(v int) string {
		if v == 0x18 {
			return "回線交換 6.4kb/s"
		}
		return fmt.Sprintf("予約(%d)", v)
	}
	if o, ok := next("3"); ok {
		ccExtNote(st, o, 1, "オクテット 3")
		ic := ccBits(o, 5, 1)
		parts = append(parts, "コーディング標準="+pick(ccCodingStd, ccBits(o, 7, 6), "予約"),
			"情報転送能力="+pickMap(ccInfoCaps, ic, "予約"))
		br.NonVoice = ic == 0x09
	}
	o4, ok := next("4")
	if ok {
		parts = append(parts, "転送モード="+pick([]string{"回線交換"}, ccBits(o4, 7, 6), "予約"), "情報転送速度="+rate(ccBits(o4, 5, 1)))
		if bit(o4, 8) == 0 {
			if a, ok := next("4a"); ok {
				mode := ccBits(a, 4, 2)
				name := pickMap(ccModes, mode, "予約")
				parts = append(parts, "構造="+pick([]string{"デフォルト", "50Hz 構造保存", "25Hz 構造保存"}, ccBits(a, 7, 5), "予約"),
					"通信形態="+name, "呼設定法="+pick([]string{"即時接続"}, bit(a, 1), "予約"))
				if _, def := ccModes[mode]; def && mode != 1 {
					br.Mode = name
				}
				if bit(a, 8) == 0 {
					if bb, ok := next("4b"); ok {
						ccExtNote(st, bb, 1, "オクテット 4b")
						parts = append(parts, "対称性="+pick([]string{"両方向対称"}, ccBits(bb, 7, 6), "予約"),
							"情報転送速度（着から発へ）="+rate(ccBits(bb, 5, 1)))
					}
				} else if ccBits(a, 7, 5) == 0 && mode == 1 && bit(a, 1) == 0 {
					st.note("伝達能力のオクテット 4a が全てデフォルト値（図 4.3.7.3-7 注 1: 4a・4b は省略しなければならない）")
				}
			}
		}
	}
	if o5, ok := next("5"); ok {
		proto := ccBits(o5, 5, 1)
		parts = append(parts, "レイヤ 1 識別="+pick([]string{1: "レイヤ 1 識別"}, ccBits(o5, 7, 6), "予約"),
			"ユーザ情報レイヤ 1 プロトコル="+pickMap(ccL1Protocols, proto, "予約"))
		br.Codec = ccL1Codec[proto]
		has5a := bit(o5, 8) == 0
		if proto == 0x11 && !has5a {
			st.note("伝達能力のユーザ情報レイヤ 1 プロトコルが速度整合だがオクテット 5a が無い（表 4.3.7.3-8 (10)）")
		}
		if has5a {
			if a, ok := next("5a"); ok {
				ccExtNote(st, a, 1, "オクテット 5a")
				parts = append(parts, "同期／非同期="+pick([]string{"同期"}, bit(a, 7), "予約"),
					"インバンド交渉="+pick([]string{"インバンド交渉不可"}, bit(a, 6), "予約"),
					"ユーザ速度="+pick([]string{1: "π／4 シフト QPSK 方式標準"}, ccBits(a, 5, 1), "予約"))
			}
		}
	}
	if i < len(b) {
		parts = append(parts, "余り="+hexOf(b[i:]))
		st.note("伝達能力の後ろに図 4.3.7.3-7 に無いオクテット %s", hexOf(b[i:]))
	}
	br.Text = ccJoin(parts)
	if st.apply {
		st.call.Bearers = append(st.call.Bearers, br)
	}
	return br.Text
}


var ccNumTypes = map[int]string{0: "不定", 2: "国内番号", 3: "免許人特有番号", 4: "市内番号", 6: "短縮番号"}
var ccNumPlans = map[int]string{0: "不定", 1: "ISDN／電話番号計画（E.164/E.163）", 3: "データ番号計画（X.121）", 4: "テレックス番号計画（F.69）",
	8: "国内番号計画", 9: "私設網番号計画"}

func ccDigits(b []byte, odd bool, st *ccState, what string) string {
	var sb strings.Builder
	n := 0
	for i, o := range b {
		for k, nib := range []byte{o & 0xF, o >> 4} {
			lastHigh := i == len(b)-1 && k == 1
			switch {
			case nib == 0:
				if !lastHigh {
					st.note("%sの途中にフィラー（0000）がある", what)
				}
			case nib >= 1 && nib <= 0xC:
				sb.WriteByte(digitChar(nib))
				n++
			default:
				sb.WriteByte('?')
				n++
				st.note("%sに予約の番号デジット %X がある", what, nib)
			}
		}
	}
	if (n%2 == 1) != odd {
		st.note("%sの奇数／偶数表示（%s）と桁数 %d が合わない", what, map[bool]string{true: "奇数", false: "偶数"}[odd], n)
	}
	return sb.String()
}

func ccDecCalled(b []byte, st *ccState) string {
	if len(b) < 1 {
		return "（オクテット 3 が無い）"
	}
	o := b[0]
	odd := bit(o, 8) == 1
	ty := ccBits(o, 7, 5)
	tyText := pickMap(ccNumTypes, ty, "予約")
	if ty == 2 || ty == 4 {
		tyText += "（PSTN 接続通信。表 4.3.7.3-9 注 5）"
	}
	digits := ccDigits(b[1:], odd, st, "着番号")
	if st.apply {
		st.call.Called = digits
	}
	return ccJoin([]string{"番号=" + digits, "奇数／偶数=" + map[bool]string{true: "奇数", false: "偶数"}[odd],
		"番号種別=" + tyText, "番号計画識別子=" + pickMap(ccNumPlans, ccBits(o, 4, 1), "予約")})
}

func ccDecCalling(b []byte, st *ccState) string {
	if len(b) < 2 {
		return "（オクテット 3・4 が無い）" + hexOf(b)
	}
	o3, o4 := b[0], b[1]
	ccExtNote(st, o3, 1, "オクテット 3")
	odd := bit(o4, 8) == 1
	digits := ccDigits(b[2:], odd, st, "発番号")
	if st.apply {
		st.call.Calling = digits
	}
	parts := []string{"番号=" + digits, "番号種別=" + pickMap(ccNumTypes, ccBits(o3, 7, 5), "予約"),
		"番号計画識別子=" + pickMap(ccNumPlans, ccBits(o3, 4, 1), "予約"),
		"奇数／偶数=" + map[bool]string{true: "奇数", false: "偶数"}[odd],
		"表示識別子=" + pick([]string{"表示可", "表示不可", "相互接続条件により表示できる番号なし"}, ccBits(o4, 4, 3), "予約"),
		"網検証識別子=" + pick([]string{"ユーザ投入、網検証なし", "ユーザ投入、網検証あり、成功", "ユーザ投入、網検証あり、失敗", "網投入"}, ccBits(o4, 2, 1), "")}
	if v := ccBits(o4, 7, 5); v != 0 {
		parts = append(parts, fmt.Sprintf("予備=%d", v))
	}
	return ccJoin(parts)
}

func ccDecSubaddress(b []byte, st *ccState) string {
	if len(b) < 1 {
		return "（オクテット 3 が無い）"
	}
	o := b[0]
	ccExtNote(st, o, 1, "オクテット 3")
	parts := []string{"サブアドレス種別=" + pick([]string{2: "π／4 シフト QPSK 方式特有サブアドレス"}, ccBits(o, 7, 5), "予約"),
		"偶数／奇数表示=" + pick([]string{"アドレス信号の数が偶数", "アドレス信号の数が奇数"}, bit(o, 4), ""),
		"サブアドレス情報=" + hexOf(b[1:])}
	if v := ccBits(o, 3, 1); v != 0 {
		parts = append(parts, fmt.Sprintf("予備=%d", v))
	}
	return ccJoin(parts)
}


var ccCauseLocations = map[int]string{0: "移動局自身", 1: "移動局が直接接続する私設網", 2: "移動局が直接接続する国内網", 3: "中継網",
	4: "相手ユーザが直接接続する国内網", 5: "相手ユーザが直接接続する私設網", 10: "インタワーキング先の網"}

var ccCauseClasses = []string{"正常イベントクラス", "正常イベントクラス", "リソース使用不可クラス", "サービス利用不可クラス",
	"サービス未提供クラス", "無効メッセージクラス", "手順誤りクラス", "インタワーキングクラス"}

var ccCauses = map[int]string{
	1: "欠番", 2: "指定中継網へのルートなし", 3: "相手へのルートなし", 4: "着側単信移動局", 5: "着側複信移動局", 6: "チャネル利用不可",
	7: "呼が設定済のチャネルへ着呼", 8: "ユーザ電源 OFF", 9: "ユーザ電源異常",
	16: "正常切断", 17: "着ユーザビジー", 18: "着ユーザレスポンスなし", 19: "着ユーザ呼出中／応答なし", 21: "通信拒否", 22: "相手加入者端末番号変更",
	26: "選択されなかったユーザの切断復旧", 27: "相手端末故障中", 28: "無効番号フォーマット", 29: "ファシリティ拒否", 30: "状態問合わせの応答", 31: "その他の正常クラス",
	34: "利用可回線／チャネルなし", 38: "網障害", 41: "一時的障害", 42: "交換機輻輳", 43: "アクセス情報廃棄", 44: "要求回線／チャネル利用不可",
	47: "その他のリソース使用不可クラス",
	49: "QOS 利用不可", 50: "要求ファシリティ未契約", 57: "伝達能力不許可", 58: "現在利用不可伝達能力", 63: "その他のサービス又はオプションの利用不可クラス",
	65: "未提供伝達能力指定", 66: "未提供チャネル種別指定", 69: "未提供ファシリティ要求", 70: "非制限デジタル情報伝達能力",
	79: "その他のサービス又はオプションの未提供クラス",
	81: "無効呼番号使用", 82: "無効チャネル番号使用", 83: "指定された中継呼識別番号使用中", 84: "中断呼識別番号使用中", 85: "中断呼なし",
	86: "指定中断呼切断復旧済", 88: "端末属性不一致", 91: "無効中継網選択", 95: "その他の無効メッセージクラス",
	96: "必須情報要素不足", 97: "メッセージ種別未定義又は未提供", 98: "呼状態とメッセージ不一致又はメッセージ種別未定義", 99: "情報要素未定義",
	100: "情報要素内容無効", 101: "呼状態とメッセージ不一致", 102: "タイマー満了による回復", 111: "その他の手順誤りクラス",
	127: "その他のインタワーキングクラス",
}

func CauseText(v int) string {
	name, ok := ccCauses[v]
	if !ok {
		name = "予約"
	}
	return fmt.Sprintf("#%d %s（%s）", v, name, ccCauseClasses[(v>>4)&7])
}

func ccDecCause(b []byte, st *ccState) string {
	if len(b) < 1 {
		return "（オクテット 3 が無い）"
	}
	o := b[0]
	parts := []string{"コーディング標準=" + pick([]string{0: "π／4 シフト QPSK 方式標準", 3: "生成源特有標準"}, ccBits(o, 7, 6), "予約"),
		"生成源=" + pickMap(ccCauseLocations, ccBits(o, 4, 1), "予約")}
	i := 1
	if bit(o, 8) == 0 {
		if i >= len(b) {
			st.note("理由表示のオクテット 3a が無い")
			return ccJoin(parts)
		}
		a := b[i]
		i++
		ccExtNote(st, a, 1, "オクテット 3a")
		parts = append(parts, "仕様種別="+pick([]string{"π／4 シフト QPSK 方式標準"}, ccBits(a, 7, 1), "予約"))
	} else {
		parts = append(parts, "仕様種別=π／4 シフト QPSK 方式標準（オクテット 3a 省略）")
	}
	if i >= len(b) {
		st.note("理由表示のオクテット 4（理由表示値）が無い")
		return ccJoin(parts)
	}
	v4 := b[i]
	i++
	ccExtNote(st, v4, 1, "オクテット 4")
	cause := CauseText(int(v4 & 0x7F))
	parts = append([]string{"理由表示値=" + cause}, parts...)
	if i < len(b) {
		parts = append(parts, "余り="+hexOf(b[i:]))
	}
	if st.apply && st.call.Cause == "" {
		st.call.Cause = cause
	}
	return ccJoin(parts)
}


var ccStatesMS = map[int]string{
	0: "UCC0 空", 1: "UCC1 発呼 複信個別", 2: "UCC2 発呼受付 複信個別", 3: "UCC3 呼出通知", 6: "UCC6 発呼グループ", 7: "UCC7 発呼受付グループ",
	10: "UCC10 発呼 単信個別", 11: "UCC11 発呼受付 単信個別", 13: "UCC13 通信形態受信", 14: "UCC14 呼設定受信 複信個別",
	15: "UCC15 呼設定受信 単信個別", 16: "UCC16 呼設定受信 グループ", 17: "UCC17 着呼 複信個別", 18: "UCC18 着呼 単信個別",
	19: "UCC19 着呼 グループ", 20: "UCC20 呼出中", 21: "UCC21 応答", 22: "UCC22 通信中", 23: "UCC23 単信通信中",
	30: "UCC30 切断要求", 31: "UCC31 解放要求", 32: "UCC32 発呼動作中切断要求", 33: "UCC33 切断要求 単、グ", 34: "UCC34 切断通知",
	40: "UCC40 チャネル切替受付 単、グ", 41: "UCC41 チャネル切替受付 複信個別", 42: "UCC42 チャネル切替起動 複信個別", 43: "UCC43 チャネル切替中 複信個別",
}

var ccStatesBS = map[int]string{
	0: "NCC0 空", 1: "NCC1 発呼 複信", 2: "NCC2 発呼受付 複信個別", 3: "NCC3 呼出通知", 4: "NCC4 着呼出 単信個別", 5: "NCC5 発呼 単信",
	6: "NCC6 発呼受付 グループ", 9: "NCC9 発呼受付 単信個別", 11: "NCC11 通信要求 複信個別", 12: "NCC12 通信要求 グループ",
	13: "NCC13 通信要求 単信個別", 14: "NCC14 通信可否受信 複信個別", 15: "NCC15 通信可否受信 単信個別", 16: "NCC16 着呼", 17: "NCC17 呼出中",
	18: "NCC18 応答", 20: "NCC20 通信中", 21: "NCC21 単信通信中", 30: "NCC30 切断要求", 31: "NCC31 切断通知", 32: "NCC32 解放要求",
	33: "NCC33 切断要求 単、グ", 34: "NCC34 接続放棄", 40: "NCC40 チャネル切替 複信", 41: "NCC41 チャネル切替 単信",
}

func CallStateText(v, dir int) string {
	ms, okM := ccStatesMS[v]
	bs, okB := ccStatesBS[v]
	switch dir {
	case 2:
		if okM {
			return ms
		}
		return fmt.Sprintf("予約(%d)（移動局側）", v)
	case 1:
		if okB {
			return bs
		}
		return fmt.Sprintf("予約(%d)（基地局側）", v)
	}
	switch {
	case okM && okB:
		return ms + "／" + bs
	case okM:
		return ms + "（基地局側は予約）"
	case okB:
		return bs + "（移動局側は予約）"
	}
	return fmt.Sprintf("予約(%d)", v)
}

func ccDecCallState(b []byte, st *ccState) string {
	if len(b) < 1 {
		return "（オクテット 3 が無い）"
	}
	o := b[0]
	parts := []string{"呼状態値=" + CallStateText(ccBits(o, 6, 1), st.dir),
		"コーディング標準=" + pick([]string{"π／4 シフト QPSK 方式標準"}, ccBits(o, 8, 7), "予約")}
	if len(b) > 1 {
		parts = append(parts, "余り="+hexOf(b[1:]))
	}
	return ccJoin(parts)
}


func ccDecChannelID(b []byte, st *ccState) string {
	if len(b) < 1 {
		return "（オクテット 3 が無い）"
	}
	o := b[0]
	ccExtNote(st, o, 1, "オクテット 3")
	parts := []string{
		"インタフェース識別有無表示=" + pick([]string{"暗黙のうちに識別されるインタフェース"}, bit(o, 7), "予約"),
		"インタフェースタイプ=" + pick([]string{"基本インタフェース"}, bit(o, 6), "予約"),
		"チャネル変更可／不可=" + pick([]string{"指定したチャネルを変更してもよい", "指定されたチャネルは変更不可"}, bit(o, 4), ""),
		"制御チャネル ind=" + pick([]string{"指定チャネルは制御チャネルでない", "指定チャネルは制御チャネルである"}, bit(o, 3), ""),
		"情報チャネル選択=" + pick([]string{"チャネルなし", "チャネルあり"}, ccBits(o, 2, 1), "予約"),
	}
	if bit(o, 5) != 0 {
		parts = append(parts, "予備=1")
	}
	if len(b) > 1 {
		parts = append(parts, "余り="+hexOf(b[1:]))
	}
	return ccJoin(parts)
}


func ccChars(b []byte) string {
	var sb strings.Builder
	kana := false
	for _, c := range b {
		switch {
		case c == 0x0E:
			kana = true
		case c == 0x0F:
			kana = false
		case kana && c >= 0x21 && c <= 0x5F:
			sb.WriteRune(rune(0xFF61 + int(c) - 0x21))
		case kana && c == 0x20:
			sb.WriteByte(' ')
		default:
			sb.WriteString(IA5Kana([]byte{c}))
		}
	}
	return sb.String()
}

func ccDecDisplay(b []byte, st *ccState) string {
	s := ccChars(b)
	if st.apply && st.call.Display == "" {
		st.call.Display = s
	}
	return "「" + s + "」"
}

func ccDecKeypad(b []byte, st *ccState) string {
	if len(b) < 1 {
		return "（オクテット 3 が無い）"
	}
	o := b[0]
	id := ccBits(o, 7, 1)
	parts := []string{"キーパッド識別子=" + pick([]string{"PB 信号送出要求", "IA5 キャラクタ転送"}, id, "予約")}
	i := 1
	if bit(o, 8) == 0 {
		if i >= len(b) {
			st.note("キーパッドファシリティのオクテット 3a が無い")
			return ccJoin(parts)
		}
		a := b[i]
		i++
		ccExtNote(st, a, 1, "オクテット 3a")
		parts = append(parts, fmt.Sprintf("シーケンス番号=%d", a&0x7F))
		if id != 0 {
			st.note("キーパッドファシリティのオクテット 3a は PB 信号送出要求のときに使う（表 4.3.7.3-18 (2) 注）")
		}
	}
	if st.dir == 1 && id != 0 {
		st.note("下りのキーパッド識別子は PB 信号送出要求だけ（表 4.3.7.3-18 注 2）")
	}
	parts = append(parts, "情報=「"+ccChars(b[i:])+"」")
	return ccJoin(parts)
}

func ccUUPD(pd byte) string {
	switch {
	case pd == 0x00:
		return "ユーザ特有プロトコル"
	case pd == 0x04:
		return "IA5 キャラクタ"
	case pd == 0x08:
		return "π／4 シフト QPSK 方式標準エアインタフェース CC メッセージ"
	case pd >= 0x10 && pd <= 0x3F, pd >= 0x50 && pd <= 0xFE:
		return "他のネットワークレイヤもしくはレイヤ 3 プロトコルのための予備"
	case pd >= 0x40 && pd <= 0x4F:
		return "国内用"
	}
	return "予約"
}

func ccDecUserUser(b []byte, st *ccState) string {
	if len(b) < 1 {
		return "（オクテット 3 が無い）"
	}
	pd := b[0]
	parts := []string{fmt.Sprintf("プロトコル識別子=0x%02X %s", pd, ccUUPD(pd))}
	if pd == 0x04 {
		parts = append(parts, "ユーザ情報=「"+ccChars(b[1:])+"」")
	} else {
		parts = append(parts, "ユーザ情報="+hexOf(b[1:]))
	}
	return ccJoin(parts)
}


func ccFeatureID(b []byte, st *ccState) (string, int) {
	if len(b) < 1 {
		return "（オクテット 3 が無い）", 0
	}
	v := int(b[0] & 0x7F)
	n := 1
	if bit(b[0], 8) == 0 {
		if len(b) < 2 {
			st.note("%sのオクテット 3a が無い", ccIEName(st.ie))
		} else {
			ccExtNote(st, b[1], 1, "オクテット 3a")
			v = v<<7 | int(b[1]&0x7F)
			n = 2
		}
	}
	return fmt.Sprintf("フィーチャ識別子番号=%d（表 4.3.7.3-15: 内容は今後規定する）", v), n
}

func ccDecFeatureAct(b []byte, st *ccState) string {
	s, n := ccFeatureID(b, st)
	if n < len(b) {
		s += "、余り=" + hexOf(b[n:])
	}
	return s
}

func ccDecFeatureInd(b []byte, st *ccState) string {
	s, n := ccFeatureID(b, st)
	parts := []string{s}
	if n < len(b) {
		o := b[n]
		parts = append(parts, "フィーチャ状態表示="+pick([]string{"非活性中", "活性中", "プロンプト（ユーザの入力待ち）", "実行中"}, ccBits(o, 4, 1), "予約"))
		if o>>4 != 0 {
			parts = append(parts, fmt.Sprintf("予備=%d", o>>4))
		}
		if n+1 < len(b) {
			parts = append(parts, "余り="+hexOf(b[n+1:]))
		}
	} else if n > 0 {
		st.note("フィーチャインディケーションのオクテット 4（フィーチャ状態表示）が無い")
	}
	return ccJoin(parts)
}


func ccDecHLC(b []byte, st *ccState) string {
	if len(b) < 2 {
		return hexOf(b)
	}
	o2, o3 := b[0], b[1]
	ccExtNote(st, o2, 1, "オクテット 2")
	ccExtNote(st, o3, 1, "オクテット 3")
	return ccJoin([]string{
		"コーディング標準=" + pick([]string{0: "π／4 シフト QPSK 方式標準", 3: "免許人特有"}, ccBits(o2, 7, 6), "予約"),
		"解釈法=" + pick([]string{4: "最初の高位レイヤ特性識別"}, ccBits(o2, 5, 3), "予約"),
		"プロトコルプロファイル表現法=" + pick([]string{1: "高位レイヤプロトコルプロファイル（属性の仕様なし）"}, ccBits(o2, 2, 1), "予約"),
		"高位レイヤ特性識別=" + pick([]string{1: "電話"}, ccBits(o3, 7, 1), "予約"),
	})
}

var ccProgLocations = map[int]string{0: "移動局自身", 1: "移動局が直接接続する私設網", 2: "移動局が直接接続する国内網",
	4: "相手ユーザが直接接続する国内網", 5: "相手ユーザが直接接続する私設網", 10: "インタワーキング先の網"}

var ccProgDescs = map[int]string{1: "呼が ISDN エンド・エンドでない", 2: "非 ISDN 着側アドレス", 3: "非 ISDN 発側アドレス", 4: "呼の ISDN への復帰",
	8: "インバンド信号ないし、適当なパターンが利用可能"}

func ccDecProgress(b []byte, st *ccState) string {
	if len(b) < 2 {
		return hexOf(b)
	}
	o2, o3 := b[0], b[1]
	ccExtNote(st, o2, 1, "オクテット 2")
	ccExtNote(st, o3, 1, "オクテット 3")
	return ccJoin([]string{
		"経過内容=" + pickMap(ccProgDescs, ccBits(o3, 7, 1), "予約"),
		"コーディング標準=" + pick([]string{0: "π／4 シフト QPSK 方式標準", 3: "生成源特有標準"}, ccBits(o2, 7, 6), "予約"),
		"生成源=" + pickMap(ccProgLocations, ccBits(o2, 4, 1), "予約"),
	})
}

var ccSignals = map[int]string{
	0x00: "ダイヤルトーン オン", 0x01: "呼出音（RBT）オン", 0x02: "割込みトーン オン", 0x03: "網輻輳トーン オン", 0x04: "ビジートーン オン",
	0x05: "確認トーン オン", 0x06: "応答トーン オン", 0x08: "オフフック警告トーン オン", 0x09: "切断予告トーン オン", 0x3F: "トーン オフ",
	0x40: "呼出 オン パターン 0", 0x41: "呼出 オン パターン 1", 0x42: "呼出 オン パターン 2", 0x43: "呼出 オン パターン 3",
	0x44: "呼出 オン パターン 4", 0x45: "呼出 オン パターン 5", 0x46: "呼出 オン パターン 6", 0x47: "呼出 オン パターン 7", 0x4F: "呼出 オフ",
}

func ccDecSignal(b []byte, st *ccState) string {
	if len(b) < 1 {
		return ""
	}
	return fmt.Sprintf("0x%02X %s", b[0], pickMap(ccSignals, int(b[0]), "予約"))
}


func ccDecFacility(b []byte, st *ccState) string {
	if len(b) < 1 {
		return "（オクテット 3 が無い）"
	}
	o := b[0]
	ccExtNote(st, o, 1, "オクテット 3")
	parts := []string{"サービス識別子=" + pick([]string{17: "付加サービスアプリケーション"}, ccBits(o, 5, 1), "予約")}
	if v := ccBits(o, 7, 6); v != 0 {
		parts = append(parts, fmt.Sprintf("予備=%d", v))
	}
	i := 1
	for k := 1; i < len(b); k++ {
		tag := b[i]
		i++
		comp := fmt.Sprintf("コンポーネント %d=%s・%s・%s", k,
			pick([]string{2: "コンテクスト特有"}, ccBits(tag, 8, 7), "クラス 予約"),
			pick([]string{1: "コンストラクタ"}, bit(tag, 6), "形式 予約"),
			pick([]string{1: "インボーク", 2: "リターンリザルト", 3: "リターンエラー", 4: "リジェクト"}, ccBits(tag, 5, 1), "タグ 予約"))
		if i >= len(b) {
			st.note("ファシリティのコンポーネント %d の長さが無い", k)
			parts = append(parts, comp)
			break
		}
		l := b[i]
		i++
		if bit(l, 8) != 0 {
			st.note("ファシリティのコンポーネント %d の長さフォーマットが 1（表 4.3.7.3-24 (5): 予約）", k)
		}
		n := int(l & 0x7F)
		if i+n > len(b) {
			st.note("ファシリティのコンポーネント %d が足りない（長さ %d、残り %d）", k, n, len(b)-i)
			n = len(b) - i
		}
		comp += fmt.Sprintf("（長さ %d）%s", l&0x7F, hexOf(b[i:i+n]))
		i += n
		parts = append(parts, comp)
	}
	return ccJoin(parts)
}


func ccDecPrimaryZone(b []byte, st *ccState) string {
	if len(b) < 2 {
		return hexOf(b)
	}
	return fmt.Sprintf("制御ゾーン番号=%d、基地局番号=%d", b[0], b[1])
}

func ccDecMSNumber(b []byte, st *ccState) string {
	v := new(big.Int)
	for _, o := range b {
		v.Lsh(v, 7)
		v.Or(v, big.NewInt(int64(o&0x7F)))
	}
	bits := 7 * len(b)
	hex := fmt.Sprintf("%0*X", (bits+3)/4, v)
	if st.apply && st.call.MSNumber == "" {
		st.call.MSNumber = hex
	}
	return fmt.Sprintf("%s（10 進 %s、%d ビット）", hex, v.String(), bits)
}
