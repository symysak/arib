package nonvoice

import (
	"fmt"
	"strings"

	"github.com/symysak/arib/internal/rcr39qpsk/l1"
	"github.com/symysak/arib/internal/std-39/qpsk/l3"
)


type MsgDef struct {
	Type   uint8
	Name   string
	Method string
	Dir    int
	Ch     string
	Ref    string
	IEs    []string
}

var Messages = []MsgDef{
	{0x08, "着信要求", MethodPolling, 2, "SCCH／UACCH1", "付録 1 §3.3.1.2.1", []string{"要求種別"}},
	{0x09, "着信要求受付", MethodPolling, 1, "SCCH／UACCH1", "付録 1 §3.3.1.2.2", nil},
	{0x0A, "着信要求拒否", MethodPolling, 1, "SCCH／UACCH1", "付録 1 §3.3.1.2.3", nil},
	{0x0B, "圏内通知", MethodPolling, 2, "SCCH", "付録 1 §3.3.1.2.15", nil},
	{0x0C, "圏内通知受付", MethodPolling, 1, "SCCH", "付録 1 §3.3.1.2.16", []string{"グループ情報"}},
	{0x48, "無線チャネル指定 1", MethodPolling, 1, "SCCH", "付録 1 §3.3.1.2.4",
		[]string{"周波数コード", "スロット番号 1", "下りグループ通信チャネル用グループアドレス", "下りグループ通信チャネル用個別アドレス", "上り通信チャネル用個別アドレス", "グループ種別", "スクランブルコード"}},
	{0x49, "無線チャネル指定 2", MethodPolling, 1, "SCCH", "付録 1 §3.3.1.2.5",
		[]string{"周波数コード", "スロット番号 2", "高速データ伝送用個別アドレス 1", "高速データ伝送用個別アドレス 2", "スクランブルコード", "伝送方向"}},
	{0x74, "データ通信中再発呼", MethodPolling, 2, "SCCH", "付録 1 §3.3.1.2.6", []string{"受信レベル"}},
	{0x68, "切替先無線チャネル指定 1", MethodPolling, 1, "UACCH1", "付録 1 §3.3.1.2.7",
		[]string{"周波数コード", "スロット番号 1", "下りグループ通信チャネル用グループアドレス", "下りグループ通信チャネル用個別アドレス", "上り通信チャネル用個別アドレス", "グループ種別", "スクランブルコード"}},
	{0x69, "切替先無線チャネル指定 2", MethodPolling, 1, "UACCH1", "付録 1 §3.3.1.2.8",
		[]string{"周波数コード", "スロット番号 2", "高速データ伝送用個別アドレス 1", "高速データ伝送用個別アドレス 2", "スクランブルコード", "伝送方向"}},
	{0x6A, "高速データ伝送モード終了", MethodPolling, 2, "UACCH1", "付録 1 §3.3.1.2.10", nil},
	{0x6B, "高速データ伝送モード終了応答", MethodPolling, 1, "UACCH1", "付録 1 §3.3.1.2.11", nil},
	{0x6C, "高速データ伝送中止", MethodPolling, 0, "UACCH1", "付録 1 §3.3.1.2.12", nil},
	{0x6D, "高速データ伝送中止応答", MethodPolling, 1, "UACCH1", "付録 1 §3.3.1.2.13", nil},
	{0x6E, "応答", MethodPolling, 2, "UACCH1", "付録 1 §3.3.1.2.14", []string{"応答種別"}},
	{0xC3, "データ送信停止指令", MethodPolling, 1, "UACCH1", "付録 1 §3.3.1.2.9", nil},
	{0x4A, "無線チャネル指定－3", MethodContention, 1, "SCCH", "付録 2 §3.3.1.1.1",
		[]string{"周波数コード", "スロット番号", "スクランブルコード", "移動局送信電力指定", "チャネル情報", "ランダム遅延", "初期タイマ", "送信停止タイマ", "再送回数"}},
	{0xC4, "送信権要求", MethodContention, 2, "UACCH2", "付録 2 §3.3.1.1.2", nil},
	{0xC5, "送信権獲得", MethodContention, 1, "UACCH2", "付録 2 §3.3.1.1.3", nil},
	{0xC6, "送信権拒否", MethodContention, 1, "UACCH2", "付録 2 §3.3.1.1.4", nil},
}

func MessageDef(t uint8) (MsgDef, bool) {
	for _, m := range Messages {
		if m.Type == t {
			return m, true
		}
	}
	return MsgDef{}, false
}

var dirName = []string{"双方向", "下り（基地局→移動局）", "上り（移動局→基地局）"}

type rtp struct {
	b    []byte
	i    int
	m    *l3.Message
	stop bool
}

func (p *rtp) add(name, value string) {
	p.m.Fields = append(p.m.Fields, l3.Field{Name: name, Value: value})
}
func (p *rtp) note(f string, a ...any) {
	p.m.Notes = append(p.m.Notes, fmt.Sprintf(f, a...))
}

func (p *rtp) ie(name string, n int) []byte {
	if p.stop {
		return nil
	}
	left := len(p.b) - p.i
	if left < n {
		p.stop = true
		if left > 0 {
			p.add(name+"（途中まで）", fmt.Sprintf("%X", p.b[p.i:]))
			p.i = len(p.b)
		}
		p.note("必須の情報要素「%s」が無い", name)
		if p.m.Err == "" {
			p.m.Err = "必須の情報要素が無い: " + name
		}
		return nil
	}
	v := p.b[p.i : p.i+n]
	p.i += n
	return v
}

func (p *rtp) addr(name string, lo, hi int) int {
	b := p.ie(name, 1)
	if b == nil {
		return -1
	}
	v := int(b[0])
	p.add(name, AddrText(v))
	if v < lo || v > hi {
		p.note("%s = %d が範囲（%d〜%d）の外", name, v, lo, hi)
	}
	return v
}

func (p *rtp) nibble4(name string, table map[int]string, other string) {
	b := p.ie(name, 1)
	if b == nil {
		return
	}
	v := int(b[0] & 0xF)
	t, ok := table[v]
	if !ok {
		t = other
	}
	p.add(name, fmt.Sprintf("%s (%04b)", t, v))
	if b[0]>>4 != 0 {
		p.add(name+" 予約（ビット 8〜5）", fmt.Sprintf("%04b", b[0]>>4))
	}
}

func ParseRT(b []byte, c l3.Ctx) (l3.Message, bool) {
	if len(b) == 0 {
		return l3.Message{}, false
	}
	def, ok := MessageDef(b[0])
	if !ok {
		return l3.Message{}, false
	}
	m := l3.Message{Entity: "RT", Type: b[0], Name: def.Name, Raw: append([]byte(nil), b...)}
	p := &rtp{b: m.Raw, i: 1, m: &m}
	p.add("メッセージ種別", fmt.Sprintf("0x%02X（%s・%s・%s・%s）", b[0], def.Name, def.Ref, dirName[def.Dir], def.Ch))
	p.add("非音声方式", def.Method+"方式（"+AppendixOf(def.Method)+"）")
	if (c.Dir == 1 || c.Dir == 2) && def.Dir != 0 && c.Dir != def.Dir {
		p.note("%s は%sのメッセージ（%s）だが%sで受けた", def.Name, dirName[def.Dir], def.Ref, dirName[c.Dir])
	}
	switch b[0] {
	case 0x08:
		p.nibble4("要求種別", map[int]string{0: "グループ通信", 1: "高速データ伝送モード"}, "免許人固有情報")
	case 0x0C:
		p.nibble4("グループ情報", map[int]string{}, "免許人固有情報")
	case 0x48, 0x68:
		m.Assign = p.assign1(c, b[0] == 0x68)
	case 0x49, 0x69:
		m.Assign = p.assign2(c, b[0] == 0x69)
	case 0x74:
		p.level()
	case 0x6E:
		p.answer()
	case 0x4A:
		m.Assign = p.assign3(c)
	}
	if !p.stop && p.i < len(p.b) {
		rest := p.b[p.i:]
		p.add("認識できない情報要素（無視）", fmt.Sprintf("%X", rest))
		p.note("%s の表に無い後続のオクテットが %d 個（本編 §4.3.3.3 (2): 認識できない情報要素として無視）", def.Ref, len(rest))
	}
	for _, n := range m.Notes {
		m.Fields = append(m.Fields, l3.Field{Name: "規格の検査", Value: n})
	}
	return m, true
}

func (p *rtp) freq(c l3.Ctx) (fc int, mhz float64) {
	b := p.ie("周波数コード", 2)
	if b == nil {
		return -1, 0
	}
	band := int(b[0] >> 4)
	fc = int(b[0]&0xF)<<8 | int(b[1])
	bt := fmt.Sprintf("予約 (%04b)", band)
	if band == 0 {
		bt = "0000（周波数は未定 = 網ごと）"
	}
	p.add("周波数帯識別子", bt)
	t := fmt.Sprintf("Fc %d", fc)
	if c.FreqMHz != nil {
		if mhz = c.FreqMHz(fc); mhz != 0 {
			t += fmt.Sprintf("（基地局送信 %.4f MHz）", mhz)
		}
	}
	p.add("周波数コード", t)
	return fc, mhz
}

func (p *rtp) scramble() int {
	b := p.ie("スクランブルコード", 1)
	if b == nil {
		return -1
	}
	p.add("スクランブルコード", fmt.Sprintf("0x%02X（通信チャネル用。S1〜S8 = ビット 1〜8、S0 = S1。初期値 0x%03X）", b[0], l1.InitFromPattern(int(b[0]))))
	return int(b[0])
}

func (p *rtp) level() {
	b := p.ie("受信レベル", 1)
	if b == nil {
		return
	}
	v := int(b[0] & 0x7F)
	t := fmt.Sprintf("%d", v)
	switch {
	case v == 124:
		t = "80dBμV以上 (124)"
	case v == 38:
		t = "−5dBμV未満 (38)"
	case v > 38 && v < 124:
		t = fmt.Sprintf("%ddBμV以上 %ddBμV未満 (%d)", v-44, v-43, v)
	default:
		p.note("受信レベル = %d が範囲（38〜124）の外", v)
	}
	p.add("受信レベル", t+"（自ゾーン受信レベル。本編 §4.3.5.3.3.5）")
}

func (p *rtp) assign1(c l3.Ctx, handover bool) *l3.Assignment {
	a := &l3.Assignment{FC: -1, Slot: -1, Slot2: -1, Scramble: -1, Handover: handover}
	a.FC, a.MHz = p.freq(c)
	if b := p.ie("スロット番号 1", 1); b != nil {
		s1, s2, s3 := int(b[0]>>4&3), int(b[0]>>2&3), int(b[0]&3)
		p.add("通信スロット番号 1", fmt.Sprintf("スロット %d", s1))
		p.add("通信スロット番号 2", fmt.Sprintf("スロット %d", s2))
		p.add("通信スロット番号 3", fmt.Sprintf("スロット %d", s3))
		p.add("スロット番号 1 予備（ビット 8〜7）", fmt.Sprintf("%02b", b[0]>>6))
		p.add("スロット番号 1 の注", "各スロット番号の定義（上り／下り）は免許人固有（図 3.3.1.3-3 注）")
		a.Slot = s1
	}
	g := p.addr("下りグループ通信チャネル用グループアドレス", 192, 254)
	i := p.addr("下りグループ通信チャネル用個別アドレス", 1, 150)
	u := p.addr("上り通信チャネル用個別アドレス", 1, 150)
	p.nibble4("グループ種別", map[int]string{}, "免許人固有情報")
	a.Scramble = p.scramble()
	var t []string
	if handover {
		t = append(t, "切替先")
	}
	t = append(t, "グループ通信")
	if a.FC >= 0 {
		t = append(t, fmt.Sprintf("Fc %d", a.FC))
	}
	if a.Slot >= 0 {
		t = append(t, fmt.Sprintf("スロット %d", a.Slot))
	}
	if g >= 0 {
		t = append(t, fmt.Sprintf("グループ %d 個別 %d 上り %d", g, i, u))
	}
	if a.Scramble >= 0 {
		t = append(t, fmt.Sprintf("スクランブル 0x%02X", a.Scramble))
	}
	a.Text = strings.Join(t, " ")
	return a
}

func (p *rtp) assign2(c l3.Ctx, handover bool) *l3.Assignment {
	a := &l3.Assignment{FC: -1, Slot: -1, Slot2: -1, Scramble: -1, Handover: handover}
	a.FC, a.MHz = p.freq(c)
	if b := p.ie("スロット番号 2", 1); b != nil {
		s1, s2 := int(b[0]>>2&3), int(b[0]&3)
		p.add("高速データスロット番号 1", fmt.Sprintf("スロット %d", s1))
		p.add("高速データスロット番号 2", fmt.Sprintf("スロット %d", s2))
		p.add("スロット番号 2 予備（ビット 8〜5）", fmt.Sprintf("%04b", b[0]>>4))
		if d := (s2 - s1 + 4) % 4; d != 1 && d != 3 {
			p.note("スロット番号 2: 高速データスロットが連続した 2 スロットでない（§3.3.1.3.4・表 3.3.1.2-7 注 3）")
		}
		a.Slot, a.Slot2 = s1, s2
	}
	a1 := p.addr("高速データ伝送用個別アドレス 1", 1, 150)
	a2 := p.addr("高速データ伝送用個別アドレス 2", 1, 150)
	a.Scramble = p.scramble()
	dir := ""
	if b := p.ie("伝送方向", 1); b != nil {
		dir = "上り"
		if b[0]&1 == 1 {
			dir = "下り"
		}
		p.add("伝送方向", dir+"（高速データ伝送）")
		if b[0]>>1 != 0 {
			p.add("伝送方向 予約（ビット 8〜2）", fmt.Sprintf("%07b", b[0]>>1))
		}
	}
	var t []string
	if handover {
		t = append(t, "切替先")
	}
	t = append(t, "高速データ"+dir)
	if a.FC >= 0 {
		t = append(t, fmt.Sprintf("Fc %d", a.FC))
	}
	if a.Slot >= 0 {
		t = append(t, fmt.Sprintf("スロット %d+%d", a.Slot, a.Slot2))
	}
	if a1 >= 0 {
		t = append(t, fmt.Sprintf("アドレス %d・%d", a1, a2))
	}
	if a.Scramble >= 0 {
		t = append(t, fmt.Sprintf("スクランブル 0x%02X", a.Scramble))
	}
	a.Text = strings.Join(t, " ")
	return a
}

func slotNibble(v byte) (int, string) {
	switch v {
	case 0:
		return -1, "スロット無指定"
	case 1, 2, 4, 8:
		n := map[byte]int{1: 0, 2: 1, 4: 2, 8: 3}[v]
		return n, fmt.Sprintf("スロット %d", n)
	}
	return -2, fmt.Sprintf("予約 (%04b)", v)
}

func powerText(v int) string {
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

func timerText(v int) string {
	if v == 0 {
		return "タイマ起動なし (0)"
	}
	return fmt.Sprintf("%d ms（40ms × %d）", 40*v, v)
}

func (p *rtp) assign3(c l3.Ctx) *l3.Assignment {
	a := &l3.Assignment{FC: -1, Slot: -1, Slot2: -1, Scramble: -1}
	a.FC, a.MHz = p.freq(c)
	if b := p.ie("スロット番号", 1); b != nil {
		f, t1 := slotNibble(b[0] & 0xF)
		s, t2 := slotNibble(b[0] >> 4)
		p.add("第 1 スロット番号", t1)
		p.add("第 2 スロット番号", t2)
		a.Slot = max(f, -1)
		if a.Slot >= 0 && s >= 0 {
			a.Slot2 = s
		}
	}
	a.Scramble = p.scramble()
	if b := p.ie("移動局送信電力指定", 1); b != nil {
		p.add("最大送信電力（通信チャネル用）", powerText(int(b[0]&7)))
		p.add("初期送信電力（通信チャネル用）", powerText(int(b[0]>>4)&7))
	}
	if b := p.ie("チャネル情報", 1); b != nil {
		p.add("物理チャネル種別", []string{"制御チャネル", "通信チャネル"}[b[0]&1])
		p.add("秘匿種別", map[int]string{0: "免許人固有秘匿", 1: "標準秘匿バージョン 1"}[int(b[0]>>2)&3]+fmt.Sprintf(" (%02b)", b[0]>>2&3))
		p.add("免許人固有定義領域（チャネル情報）", fmt.Sprintf("0x%X", b[0]>>4))
	}
	var tm [3]int
	for k, n := range []string{"ランダム遅延", "初期タイマ", "送信停止タイマ"} {
		if b := p.ie(n, 1); b != nil {
			tm[k] = int(b[0])
			p.add(n, timerText(int(b[0])))
		}
	}
	if b := p.ie("再送回数", 1); b != nil {
		v := int(b[0] & 0xF)
		t := fmt.Sprintf("%d 回", v)
		if v == 0 {
			t = "再送指定なし (0)"
		}
		p.add("再送回数", t)
		if b[0]>>4 != 0 {
			p.add("再送回数 予約（ビット 8〜5）", fmt.Sprintf("%04b", b[0]>>4))
		}
	}
	var t []string
	t = append(t, "データ伝送（コンテンション）")
	if a.FC >= 0 {
		t = append(t, fmt.Sprintf("Fc %d", a.FC))
	}
	if a.Slot >= 0 {
		t = append(t, fmt.Sprintf("スロット %d", a.Slot))
	}
	if a.Scramble >= 0 {
		t = append(t, fmt.Sprintf("スクランブル 0x%02X", a.Scramble))
	}
	t = append(t, fmt.Sprintf("T1 %dms T2 %dms T3 %dms", 40*tm[0], 40*tm[1], 40*tm[2]))
	a.Text = strings.Join(t, " ")
	return a
}

func (p *rtp) answer() {
	b := p.ie("応答種別", 2)
	if b == nil {
		return
	}
	k := int(b[0] & 0xF)
	name := map[int]string{1: "チャネル移行", 2: "最優先データ受信", 3: "応答"}[k]
	if name == "" {
		name = "予約"
	}
	p.add("応答種別", fmt.Sprintf("%s (%04b)", name, k))
	no := int(b[1] & 0x3F)
	p.add("メッセージ No", fmt.Sprint(no))
	if k == 2 && (no < 1 || no > 63) {
		p.note("応答種別: 最優先データ受信なのにメッセージ No が 1〜63 でない")
	}
	if k != 2 && no != 0 {
		p.note("応答種別: 最優先データ受信以外なのにメッセージ No が 0 でない（図 3.3.1.3-11 (2)）")
	}
}


func BearerMethod(raw []byte) (method, text string) {
	if len(raw) == 0 {
		return "", ""
	}
	ic := int(raw[0] & 0x1F)
	mode := -1
	if len(raw) > 1 {
		mode = int(raw[1] >> 5 & 3)
	}
	var parts []string
	switch ic {
	case 0x0A:
		parts = append(parts, "情報転送能力=パケットデータ 1（付録 1）")
		method = MethodPolling
	case 0x0B:
		parts = append(parts, "情報転送能力=パケットデータ 2（付録 2）")
		method = MethodContention
	}
	switch mode {
	case 1:
		parts = append(parts, "転送モード=ポーリング通信（付録 1）")
		if method == "" {
			method = MethodPolling
		}
	case 3:
		parts = append(parts, "転送モード=コンテンション方式（付録 2）")
		if method == "" {
			method = MethodContention
		}
	}
	return method, strings.Join(parts, " ")
}

func ParseSignal(info []byte, c l3.Ctx) l3.Signal {
	s := l3.ParseCtx(info, c)
	for i, m := range s.Messages {
		switch m.Entity {
		case "RT":
			if a, ok := ParseRT(m.Raw, c); ok {
				s.Messages[i] = a
			}
		case "CC":
			if m.Call == nil {
				continue
			}
			for _, b := range m.Call.Bearers {
				if meth, t := BearerMethod(b.Raw); meth != "" {
					s.Messages[i].Fields = append(s.Messages[i].Fields, l3.Field{Name: "伝達能力（付録）", Value: t + " → " + meth + "方式"})
				}
			}
		}
	}
	return s
}

func MethodFromSignal(s l3.Signal) string {
	for _, m := range s.Messages {
		if m.Entity == "RT" {
			if d, ok := MessageDef(m.Type); ok {
				return d.Method
			}
		}
		if m.Call != nil {
			for _, b := range m.Call.Bearers {
				if meth, _ := BearerMethod(b.Raw); meth != "" {
					return meth
				}
			}
		}
	}
	return ""
}

func summary(s l3.Signal) string {
	t := s.Summary()
	for _, m := range s.Messages {
		if m.Assign != nil && m.Assign.Text != "" {
			t += "（" + m.Assign.Text + "）"
		}
	}
	return t
}
