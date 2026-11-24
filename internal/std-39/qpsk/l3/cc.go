package l3

import (
	"fmt"
	"strings"
)


type ccIE struct {
	cs      int
	id      byte
	single  bool
	shiftTo int
	body    []byte
	octets  int
	empty   bool
	short   bool
	known   bool
	def     *ccIEDef
	ignore  string
}

type ccIEKind int

const (
	ccLen   ccIEKind = iota
	ccFixed
	ccExt
)

type ccIEDef struct {
	name  string
	ref   string
	kind  ccIEKind
	fixed int
	dec   func(b []byte, st *ccState) string
}

var ccIE0 = map[byte]*ccIEDef{
	0x04: {"伝達能力", "5.3", ccLen, 0, ccDecBearer},
	0x08: {"理由表示", "5.8", ccLen, 0, ccDecCause},
	0x14: {"呼状態", "5.19", ccLen, 0, ccDecCallState},
	0x18: {"チャネル識別子", "5.9", ccLen, 0, ccDecChannelID},
	0x1C: {"ファシリティ", "5.20", ccLen, 0, ccDecFacility},
	0x1E: {"経過識別子", "5.15", ccFixed, 2, ccDecProgress},
	0x28: {"表示", "5.10", ccLen, 0, ccDecDisplay},
	0x2C: {"キーパッドファシリティ", "5.14", ccLen, 0, ccDecKeypad},
	0x34: {"シグナル", "5.16", ccFixed, 1, ccDecSignal},
	0x38: {"フィーチャアクティベーション", "5.11", ccLen, 0, ccDecFeatureAct},
	0x39: {"フィーチャインディケーション", "5.12", ccLen, 0, ccDecFeatureInd},
	0x6C: {"発番号", "5.6", ccLen, 0, ccDecCalling},
	0x6D: {"発サブアドレス（第 1）", "5.7", ccLen, 0, ccDecSubaddress},
	0x70: {"着番号", "5.4", ccLen, 0, ccDecCalled},
	0x71: {"着サブアドレス（第 1）", "5.5", ccLen, 0, ccDecSubaddress},
	0x7D: {"高位レイヤ整合性", "5.13", ccFixed, 2, ccDecHLC},
	0x7F: {"ユーザ・ユーザ", "5.21", ccLen, 0, ccDecUserUser},
}

var ccIE6 = map[byte]*ccIEDef{
	0x0F: {"移動局番号", "5.27", ccExt, 0, ccDecMSNumber},
	0x1F: {"旧ゾーン情報", "5.23", ccFixed, 2, ccDecPrimaryZone},
	0x6E: {"発サブアドレス（第 2）", "5.25", ccLen, 0, ccDecSubaddress},
	0x72: {"着サブアドレス（第 2）", "5.24", ccLen, 0, ccDecSubaddress},
}

type ccState struct {
	call  *Call
	dir   int
	apply bool
	notes []string
	ie    *ccIE
}

func (st *ccState) note(format string, a ...any) {
	st.notes = append(st.notes, fmt.Sprintf(format, a...))
}

func parseCC(b []byte, c Ctx) Message {
	m := Message{Entity: "CC", Raw: append([]byte(nil), b...)}
	r := &reader{b: m.Raw}
	pd, ok := r.byte1("プロトコル識別子")
	if !ok {
		m.Err = "欠損メッセージ（プロトコル識別子が無い。§4.3.7.4.2: 無視する）"
		return m
	}
	m.Fields = append(m.Fields, Field{"プロトコル識別子", fmt.Sprintf("0x%02X %s", pd, ccPDText(pd))})
	if pd != 0x08 {
		m.Err = "プロトコル識別子が CC (0x08) でない（§4.3.7.4.1: 無視する）"
		return m
	}
	cr, ok := r.byte1("呼番号／付加サービス識別子")
	if !ok {
		m.Err = "欠損メッセージ（呼番号が無い。§4.3.7.4.2: 無視する）"
		return m
	}
	call := &Call{ServiceID: int(cr >> 4), Flag: bit(cr, 4), Ref: int(cr & 7)}
	m.Call = call
	m.Fields = append(m.Fields,
		Field{"付加サービス識別子", pick([]string{"移動通信網特有付加サービス手順", "Q.932 準拠付加サービス手順"}, call.ServiceID, "予約")},
		Field{"呼番号フラグ", pick([]string{"0 呼の生起側から送出", "1 呼の生起側へ送出"}, call.Flag, "")},
		Field{"呼番号", ccRefText(call.Ref)})
	if call.ServiceID > 1 {
		m.Notes = append(m.Notes, fmt.Sprintf("付加サービス識別子 %d は予約（§4.3.7.4.3: 0000・0001 以外はこのメッセージを無視する）", call.ServiceID))
	}
	t, ok := r.byte1("メッセージ種別")
	if !ok {
		m.Err = "欠損メッセージ（メッセージ種別が無い。§4.3.7.4.2: 無視する）"
		call.Text = ccCallText(call, "")
		return m
	}
	m.Type = t & 0x7F
	call.MsgType = m.Type
	def := ccMsgs[m.Type]
	if t&0x80 != 0 {
		m.Notes = append(m.Notes, "メッセージ種別のビット 8 が 1（図 4.3.7.3-4 は 0）")
	}
	if def != nil {
		m.Name = def.name
		m.Fields = append(m.Fields,
			Field{"メッセージ種別", fmt.Sprintf("0x%02X %s［%s］（%s。§%s）", m.Type, def.name, def.en, def.group, def.sec)},
			Field{"定義区間・方向", fmt.Sprintf("%s・%s（%s）（%s）", def.area, def.dir.text(), def.chans, def.table)})
		call.Setup = ccSetupTypes[m.Type]
		call.Release = ccReleaseTypes[m.Type]
		switch {
		case def.dir == ccDown && c.Dir == 2:
			m.Notes = append(m.Notes, fmt.Sprintf("%sは下りのメッセージ（%s）だが上りで受けた", def.name, def.table))
		case def.dir == ccUp && c.Dir == 1:
			m.Notes = append(m.Notes, fmt.Sprintf("%sは上りのメッセージ（%s）だが下りで受けた", def.name, def.table))
		}
	} else {
		m.Fields = append(m.Fields, Field{"メッセージ種別", fmt.Sprintf("0x%02X %s", m.Type, ccGroupText(m.Type))})
		m.Err = fmt.Sprintf("認識されないメッセージ種別 0x%02X（表 4.3.7.3-5 の予約。§4.3.7.4.5: 現状維持）", m.Type)
	}

	ies, scanErr := ccScan(r)
	if def != nil {
		ccValidate(&m, def, ies, c)
	} else {
		for _, ie := range ies {
			if !ie.known && ie.shiftTo < 0 {
				m.Notes = append(m.Notes, ccUnknownNote(ie))
			}
		}
	}
	st := &ccState{call: call, dir: c.Dir}
	for _, ie := range ies {
		st.ie = ie
		st.apply = ie.ignore == "" && !ie.empty
		name, val := ccDecode(ie, st)
		if ie.ignore != "" {
			val += "（無視: " + ie.ignore + "）"
		}
		m.Fields = append(m.Fields, Field{name, val})
	}
	m.Notes = append(m.Notes, st.notes...)
	if scanErr != "" && (m.Err == "" || def != nil) {
		m.Err = scanErr
	}
	call.Text = ccCallText(call, m.Name)
	return m
}

func ccScan(r *reader) ([]*ccIE, string) {
	var ies []*ccIE
	codeset := 0
	for r.left() > 0 {
		id, _ := r.byte1("情報要素識別子")
		if id&0x80 != 0 {
			ie := &ccIE{cs: codeset, id: id, single: true, shiftTo: -1, octets: 1}
			if id&0xF8 == 0x90 {
				ie.shiftTo = int(id & 7)
				ie.known = true
				codeset = ie.shiftTo
			} else {
				ie.known = ccSingleKnown(codeset, id)
			}
			ies = append(ies, ie)
			continue
		}
		var def *ccIEDef
		switch codeset {
		case 0:
			def = ccIE0[id]
		case 6:
			def = ccIE6[id]
		}
		ie := &ccIE{cs: codeset, id: id, shiftTo: -1, known: def != nil, def: def}
		ies = append(ies, ie)
		kind, fixed := ccLen, 0
		if def != nil {
			kind, fixed = def.kind, def.fixed
		}
		switch kind {
		case ccLen:
			n, ok := r.byte1("内容長")
			if !ok {
				ie.short = true
				ie.octets = 1
				return ies, fmt.Sprintf("%s の内容長が無い", ccIEName(ie))
			}
			if int(n) > r.left() {
				ie.short = true
				ie.body = r.rest()
				ie.octets = 2 + len(ie.body)
				return ies, fmt.Sprintf("%s が足りない（内容長 %d、残り %d）", ccIEName(ie), n, len(ie.body))
			}
			ie.body = r.take(int(n), "内容")
			ie.octets = 2 + int(n)
			ie.empty = n == 0
		case ccFixed:
			if fixed > r.left() {
				ie.short = true
				ie.body = r.rest()
				ie.octets = 1 + len(ie.body)
				return ies, fmt.Sprintf("%s が足りない（%d オクテット中 %d）", ccIEName(ie), fixed, len(ie.body))
			}
			ie.body = r.take(fixed, "内容")
			ie.octets = 1 + fixed
		case ccExt:
			j := r.i
			for j < len(r.b) && r.b[j]&0x80 == 0 {
				j++
			}
			if j >= len(r.b) {
				ie.short = true
				ie.body = r.rest()
				ie.octets = 1 + len(ie.body)
				return ies, ccIEName(ie) + " の終わり（拡張ビット 1）が無い"
			}
			ie.body = r.take(j+1-r.i, "内容")
			ie.octets = 1 + len(ie.body)
		}
	}
	return ies, ""
}

func ccSingleKnown(cs int, id byte) bool {
	switch cs {
	case 0:
		return id == 0xA0 || id&0xF0 == 0xB0
	case 5:
		return id == 0x82
	case 6:
		return id&0xF0 == 0xF0
	}
	return false
}

func ccIEName(ie *ccIE) string {
	if ie.shiftTo >= 0 {
		return "固定シフト"
	}
	if ie.single {
		switch {
		case ie.cs == 0 && ie.id == 0xA0:
			return "モアデータ"
		case ie.cs == 0 && ie.id&0xF0 == 0xB0:
			return "輻輳制御レベル"
		case ie.cs == 5 && ie.id == 0x82:
			return "再呼表示"
		case ie.cs == 6 && ie.id&0xF0 == 0xF0:
			return "通信可否"
		}
		return fmt.Sprintf("単数オクテット情報要素 0x%02X（コード群 %d）", ie.id, ie.cs)
	}
	if ie.def != nil {
		return ie.def.name
	}
	return fmt.Sprintf("情報要素 0x%02X（コード群 %d）", ie.id, ie.cs)
}

func ccUnknownNote(ie *ccIE) string {
	switch ie.cs {
	case 6:
		return fmt.Sprintf("%s はコード群 6 の表に無い（表 4.3.7.3-6(ウ): 免許人が任意に設定可能）", ccIEName(ie))
	case 7:
		return fmt.Sprintf("%s はコード群 7（ユーザ特有情報要素。規格は中身を決めていない）", ccIEName(ie))
	}
	return fmt.Sprintf("認識できない情報要素 %s（表 4.3.7.3-6 の予約。§4.3.7.4.8 (1): 認識できるものだけ処理する）", ccIEName(ie))
}

func ccDecode(ie *ccIE, st *ccState) (string, string) {
	name := ccIEName(ie)
	if ie.single {
		return name, ccSingleText(ie, st)
	}
	if ie.empty {
		return name, "（内容長 0: 無いものとして扱う。§4.3.7.3.5.1）"
	}
	var val string
	if ie.def != nil {
		val = ie.def.dec(ie.body, st)
	} else {
		val = hexOf(ie.body)
	}
	if ie.short {
		val += "（途中まで）"
	}
	return name, val
}

func ccSingleText(ie *ccIE, st *ccState) string {
	id := ie.id
	if ie.shiftTo >= 0 {
		return pickMap(map[int]string{0: "0 適用不可", 5: "コード群 5（国内特有情報要素）", 6: "コード群 6（網（免許人）特有情報要素）", 7: "コード群 7（ユーザ特有情報要素）"},
			ie.shiftTo, "予約 コード群")
	}
	switch {
	case ie.cs == 0 && id == 0xA0:
		return "あり（同じブロックのユーザ情報が続く）"
	case ie.cs == 0 && id&0xF0 == 0xB0:
		return pickMap(map[int]string{0: "RR（受信者受信可）", 15: "RNR（受信者受信不可）"}, int(id&0xF), "予約")
	case ie.cs == 5 && id == 0x82:
		return "あり（移動局切断後、網側から再呼が起動される可能性がある）"
	case ie.cs == 6 && id&0xF0 == 0xF0:
		v := pick([]string{"可", "", "", "不可"}, int(id&3), "予約")
		if id&0x0C != 0 {
			v += fmt.Sprintf("（予備ビット %d）", (id>>2)&3)
		}
		return v
	}
	return fmt.Sprintf("0x%02X", id)
}

func ccPDText(pd byte) string {
	switch {
	case pd <= 0x07:
		return "メッセージのプロトコル識別子としての使用はできない"
	case pd == 0x08:
		return "π／4 シフト QPSK 方式標準エアインタフェース CC メッセージ"
	case pd >= 0x10 && pd <= 0x3F, pd >= 0x50 && pd <= 0xFE:
		return "他のネットワークレイヤもしくはレイヤ 3 プロトコルのための予約"
	case pd >= 0x40 && pd <= 0x4F:
		return "国内利用"
	}
	return "予約"
}

func ccRefText(ref int) string {
	switch ref {
	case 0:
		return "0 単信通信時"
	case 7:
		return "7 ダミー（どの呼にも関連しないメッセージ）"
	}
	return fmt.Sprintf("%d 一般", ref)
}

func ccGroupText(t uint8) string {
	return []string{"呼設定用メッセージ", "通信中メッセージ", "呼解放用メッセージ", "その他のメッセージ"}[(t>>5)&3] + "の予約"
}

func ccValidate(m *Message, def *ccMsg, ies []*ccIE, c Ctx) {
	last := map[int]byte{}
	for _, ie := range ies {
		if ie.single {
			continue
		}
		if p, ok := last[ie.cs]; ok && ie.id < p {
			ie.ignore = "情報要素順序エラー"
			m.Notes = append(m.Notes, fmt.Sprintf("「%s」(0x%02X) が先行した 0x%02X より小さい（コード群 %d。§4.3.7.4.6 (1): 情報要素順序エラーとして無視する）",
				ccIEName(ie), ie.id, p, ie.cs))
			continue
		}
		last[ie.cs] = ie.id
	}
	cs := 0
	for _, ie := range ies {
		if ie.shiftTo < 0 {
			continue
		}
		switch {
		case ie.shiftTo == 0 || (ie.shiftTo >= 1 && ie.shiftTo <= 4):
			m.Notes = append(m.Notes, fmt.Sprintf("固定シフトのコード群 %d は適用不可／予約（表 4.3.7.3-7）", ie.shiftTo))
		case ie.shiftTo <= cs:
			m.Notes = append(m.Notes, fmt.Sprintf("固定シフトがコード群 %d から %d へ（§4.3.7.3.5.2: 高いコード群へのシフトだけ）", cs, ie.shiftTo))
		}
		cs = ie.shiftTo
	}

	used := make([]bool, len(ies))
	for _, row := range def.rows {
		name := ccRowName(row)
		count := 0
		for i, ie := range ies {
			if !row.matches(ie) {
				continue
			}
			used[i] = true
			if ie.ignore != "" {
				continue
			}
			if ie.empty {
				if row.mandatory {
					m.Notes = append(m.Notes, fmt.Sprintf("必須の情報要素「%s」の内容長が 0（§4.3.7.4.7 (2): 必須情報要素内容エラー）", name))
				}
				continue
			}
			count++
			limit := row.rep
			if limit == 0 {
				limit = 1
			}
			if limit > 0 && count > limit {
				ie.ignore = "重複"
				m.Notes = append(m.Notes, fmt.Sprintf("「%s」が %d 回目（許されるのは %d 回。§4.3.7.4.6 (2): 最初のものだけ処理し、以降は無視する）", name, count, limit))
				continue
			}
			if row.max > 0 && ie.octets > row.max {
				m.Notes = append(m.Notes, fmt.Sprintf("「%s」が %d オクテット（最大 %d。%s・§4.3.7.4.7 (2) / §4.3.7.4.8 (2): 最大長を越えるものは内容エラー）",
					name, ie.octets, row.max, def.table))
			}
			if row.min > 0 && ie.octets < row.min && !ie.short {
				m.Notes = append(m.Notes, fmt.Sprintf("「%s」が %d オクテット（最小 %d。%s）", name, ie.octets, row.min, def.table))
			}
			switch {
			case row.dir == ccDown && c.Dir == 2:
				m.Notes = append(m.Notes, fmt.Sprintf("「%s」は下りだけ（%s）だが上りで受けた", name, def.table))
			case row.dir == ccUp && c.Dir == 1:
				m.Notes = append(m.Notes, fmt.Sprintf("「%s」は上りだけ（%s）だが下りで受けた", name, def.table))
			}
		}
		if row.mandatory && count == 0 {
			msg := fmt.Sprintf("必須の情報要素「%s」（§4.3.7.3.%s）が無い（%s）", name, row.ref, def.table)
			m.Notes = append(m.Notes, msg+"。"+ccMissingAction(m.Type))
			if m.Err == "" {
				m.Err = msg
			}
		}
	}
	var unknown, extra []string
	for i, ie := range ies {
		if used[i] {
			continue
		}
		if !ie.known && ie.shiftTo < 0 {
			m.Notes = append(m.Notes, ccUnknownNote(ie))
			unknown = append(unknown, ccIEName(ie))
			continue
		}
		n := ccIEName(ie)
		if ie.shiftTo >= 0 {
			n = fmt.Sprintf("固定シフト（コード群 %d）", ie.shiftTo)
		}
		extra = append(extra, n)
	}
	if len(extra) > 0 {
		m.Notes = append(m.Notes, fmt.Sprintf("このメッセージの表（%s）に無い情報要素: %s", def.table, strings.Join(extra, "・")))
	}
	if len(unknown) > 0 {
		switch m.Type {
		case 0x45:
			m.Notes = append(m.Notes, "認識されない情報要素を含む切断（§4.3.7.4.8 (1) ア: 理由表示 #99 情報要素未定義を伴う解放を返す）")
		case 0x4D:
			m.Notes = append(m.Notes, "認識されない情報要素を含む解放（§4.3.7.4.8 (1) イ: 理由表示 #99 情報要素未定義を伴う解放完了を返す）")
		case 0x5A:
			m.Notes = append(m.Notes, "認識されない情報要素を含む解放完了（§4.3.7.4.8 (1) ウ: エラーを報告しない）")
		}
	}
}

func ccRowName(row ccRow) string {
	if row.ref == "5.2" {
		return fmt.Sprintf("固定シフト（コード群 %d）", row.shift)
	}
	id := ccRefIdent[row.ref]
	ie := &ccIE{cs: id.cs, id: id.id, single: id.single, shiftTo: -1}
	switch id.cs {
	case 0:
		ie.def = ccIE0[id.id]
	case 6:
		ie.def = ccIE6[id.id]
	}
	return ccIEName(ie)
}

func ccMissingAction(t uint8) string {
	switch t {
	case 0x05, 0x4D:
		return "§4.3.7.4.7 (1): 理由表示 #96 必須情報要素不足を含む解放完了を返す"
	case 0x45:
		return "§4.3.7.4.7 (1): #31 その他の正常クラスの切断と同じに扱い、返す解放に #96 を入れる"
	case 0x5A:
		return "§4.3.7.4.7 (1): #31 その他の正常クラスの解放完了とみなす"
	}
	return "§4.3.7.4.7 (1): メッセージに対する動作をとらず、理由表示 #96 必須情報要素不足を含む解放を返す"
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
	if c.MSNumber != "" {
		sb.WriteString(" 移動局番号 " + c.MSNumber)
	}
	if len(c.Bearers) > 0 {
		var names []string
		for _, br := range c.Bearers {
			names = append(names, ccBearerShort(br))
		}
		sb.WriteString(" 伝達能力 " + strings.Join(names, "・"))
	}
	if c.Cause != "" {
		sb.WriteString(" 理由 " + c.Cause)
	}
	if c.Display != "" {
		sb.WriteString(" 表示「" + c.Display + "」")
	}
	return sb.String()
}

func ccBearerShort(br Bearer) string {
	parts := []string{}
	if len(br.Raw) > 0 {
		parts = append(parts, pickMap(ccInfoCaps, int(br.Raw[0]&0x1F), "予約"))
	}
	if br.Mode != "" {
		parts = append(parts, br.Mode)
	}
	if br.Codec != "" {
		parts = append(parts, strings.ToUpper(br.Codec))
	}
	return strings.Join(parts, "/")
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
