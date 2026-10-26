package l2

import (
	"bytes"
	"fmt"
	"github.com/symysak/arib/internal/std-t79/numbering"
	"strings"
)



const maxMSIOctets = 10

const GlobalMSI = 127

const (
	SAPICallControl = 0
	SAPIL2Mgmt      = 7
)

type Address struct {
	AC   uint8
	AI   uint8
	SAPI uint8
	CR   bool
	SMSI uint8
	MSI  uint64
	MSIBits int
	Octets  int
}

func (a Address) HasSMSI() bool { return a.AI&2 != 0 }
func (a Address) HasMSI() bool  { return a.AI&1 != 0 }

func (a Address) IsGlobal() bool {
	return a.HasMSI() && a.MSIBits == 7 && a.MSI == GlobalMSI
}

func (a Address) ValidIDCombo() bool {
	switch a.AC {
	case 0:
		return true
	case 1:
		return a.AI == 3
	default:
		return a.AI == 2
	}
}

func (a Address) msiHex() string {
	digits := (a.MSIBits + 3) / 4
	if digits < 1 {
		digits = 1
	}
	return fmt.Sprintf("%0*X", digits, a.MSI)
}

func (a Address) MSInfo() (numbering.MSI, bool) {
	if !a.HasMSI() || a.IsGlobal() {
		return numbering.MSI{}, false
	}
	return numbering.ParseMSI(a.MSI, a.MSIBits)
}

func (a Address) Key() string {
	switch {
	case a.HasMSI():
		return fmt.Sprintf("SAPI%d/MSI%s", a.SAPI, a.msiHex())
	case a.HasSMSI():
		return fmt.Sprintf("SAPI%d/SMSI%02X", a.SAPI, a.SMSI)
	}
	return fmt.Sprintf("SAPI%d/none", a.SAPI)
}

var acNames = [...]string{"制御なし", "SMSI 割当", "SMSI 解除", "MSI＋SMSI 要求"}

func (a Address) String() string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "SAPI=%d C/R=%d", a.SAPI, b2i(a.CR))
	if a.HasSMSI() {
		fmt.Fprintf(&sb, " SMSI=%02X", a.SMSI)
	}
	if a.HasMSI() {
		if a.IsGlobal() {
			sb.WriteString(" MSI=全局(127)")
		} else {
			fmt.Fprintf(&sb, " MSI=%s", a.msiHex())
		}
	}
	if !a.HasMSI() && !a.HasSMSI() {
		sb.WriteString(" ID無し")
	}
	if a.AC != 0 {
		sb.WriteString(" AC=" + acNames[a.AC&3])
	}
	return sb.String()
}


type Format int

const (
	FormatI Format = iota
	FormatS
	FormatU
)

func (f Format) String() string {
	if f < 0 || int(f) >= 3 {
		return "?"
	}
	return [...]string{"I", "S", "U"}[f]
}


type uDef struct {
	name, ja string
	info     bool
}

var uDefs = map[uint8]uDef{
	0x6F: {"SABME", "拡張非同期平衡モード設定", false},
	0x6E: {"SABMEI", "情報付拡張非同期平衡モード設定", true},
	0x0F: {"DM", "切断モード", false},
	0x03: {"UI", "非番号制情報", true},
	0x43: {"DISC", "切断", false},
	0x63: {"UA", "非番号制確認", false},
	0x87: {"FRMR", "フレームリジェクト", true},
	0xAF: {"XID", "識別情報交換", true},
}

var sNames = [4][2]string{
	{"RR", "受信可"},
	{"RNR", "受信不可"},
	{"REJ", "リジェクト"},
	{"S?", "未定義の監視機能"},
}

type Control struct {
	Format Format
	Name   string
	NameJa string
	NS, NR uint8
	PF     bool
	SBits uint8
	Reserved uint8
	Partial bool
	Units  []uint8
	Octets int
	Defined bool
}

func (c Control) String() string {
	var sb strings.Builder
	sb.WriteString(c.Name)
	switch c.Format {
	case FormatI:
		fmt.Fprintf(&sb, " N(S)=%d N(R)=%d", c.NS, c.NR)
	case FormatS:
		fmt.Fprintf(&sb, " N(R)=%d", c.NR)
	}
	if c.PF {
		sb.WriteString(" P/F")
	}
	if c.Partial {
		if c.Format == FormatI {
			fmt.Fprintf(&sb, " n(S)=%v", c.Units)
		} else {
			fmt.Fprintf(&sb, " n(R)=%v", c.Units)
		}
	}
	return sb.String()
}


type Frame struct {
	Addr Address
	Ctl  Control
	Info []byte
	Raw  []byte
	Err string
	Dir Dir
}

type Dir int

const (
	DirUnknown Dir = iota
	DirDown
	DirUp
)

func ParseDir(b []byte, d Dir) Frame {
	f := Parse(b)
	f.Dir = d
	if cmd, ok := f.Command(); ok && f.Ctl.Octets > 0 {
		switch f.Ctl.Name {
		case "UA", "DM", "FRMR":
			if cmd && f.Err == "" {
				f.Err = f.Ctl.Name + " はレスポンスに限る（表 4.2.2.3-7）がコマンドで来た"
			}
		case "SABME", "SABMEI", "DISC", "UI", "XID":
			if !cmd && f.Ctl.Name != "XID" && f.Err == "" {
				f.Err = f.Ctl.Name + " はコマンドに限る（表 4.2.2.3-7）がレスポンスで来た"
			}
		}
	}
	return f
}

func (f Frame) Command() (cmd, ok bool) {
	switch f.Dir {
	case DirDown:
		return f.Addr.CR, true
	case DirUp:
		return !f.Addr.CR, true
	}
	return false, false
}

func (f Frame) CarriesInfo() bool {
	if f.Err != "" || f.Addr.SAPI == 7 {
		return false
	}
	switch f.Ctl.Name {
	case "I", "UI", "SABMEI":
		return true
	}
	return false
}

func (f Frame) Summary() string {
	if f.Addr.Octets == 0 {
		return "L2? " + f.Err
	}
	s := f.Addr.Key()
	if f.Ctl.Octets > 0 {
		s += " " + f.Ctl.String()
	}
	if len(f.Info) > 0 {
		s += fmt.Sprintf(" +%dB", len(f.Info))
	}
	if f.Err != "" {
		s += " [" + f.Err + "]"
	}
	return s
}

func Parse(b []byte) Frame {
	f := Frame{Raw: b}
	if len(b) < 2 {
		f.Err = "2 オクテット未満（無効フレーム）"
		return f
	}
	a, i, err := parseAddress(b)
	f.Addr = a
	if err != "" {
		f.Err = err
		return f
	}
	c, n, err := parseControl(b[i:])
	f.Ctl = c
	if err != "" {
		f.Err = err
		return f
	}
	f.Info = b[i+n:]
	switch {
	case !a.ValidIDCombo():
		f.Err = fmt.Sprintf("許されない ID 制御・ID 表示の組合せ AC=%02b AI=%02b（表 4.2.2.2-1）", a.AC, a.AI)
	case !c.Defined:
		f.Err = "未定義の制御フィールド（表 4.2.2.3-7 に無い）"
	case len(f.Info) > 0 && c.Format == FormatU && !uDefs[uKey(b[i])].info:
		f.Err = c.Name + " は情報フィールドを持ってはならない"
	}
	return f
}

func uKey(c0 byte) uint8 { return c0 &^ 0x10 }

func parseAddress(b []byte) (Address, int, string) {
	a := Address{
		AC:   b[0] >> 6,
		AI:   (b[0] >> 4) & 3,
		SAPI: (b[0] >> 1) & 7,
		CR:   b[0]&1 == 1,
	}
	i := 1
	if a.HasSMSI() {
		if i >= len(b) {
			return a, i, "SMSI が足りない"
		}
		a.SMSI = b[i]
		i++
	}
	if a.HasMSI() {
		var v uint64
		bits := 0
		for {
			if i >= len(b) {
				a.MSI, a.MSIBits = v, bits
				return a, i, "MSI が終端しない（EA=1 が無い）"
			}
			if bits/7 >= maxMSIOctets {
				a.MSI, a.MSIBits = v, bits
				return a, i, fmt.Sprintf("MSI が %d オクテットを超える（最大 64 ビット）", maxMSIOctets)
			}
			v = v<<7 | uint64(b[i]>>1)
			bits += 7
			ea := b[i]&1 == 1
			i++
			if ea {
				break
			}
		}
		a.MSI, a.MSIBits = v, bits
	}
	a.Octets = i
	return a, i, ""
}

func parseControl(b []byte) (Control, int, string) {
	c := Control{}
	if len(b) == 0 {
		return c, 0, "制御フィールドが無い"
	}
	c0 := b[0]
	switch {
	case c0&3 == 0:
		c.Format, c.Name, c.NameJa = FormatI, "I", "情報"
		c.NS = c0 >> 2
	case c0&3 == 1:
		c.Format = FormatS
		c.SBits = (c0 >> 2) & 3
		c.Reserved = c0 >> 4
		c.Name, c.NameJa = sNames[c.SBits][0], sNames[c.SBits][1]
	default:
		c.Format = FormatU
		c.PF = c0&0x10 != 0
		c.Octets = 1
		if d, ok := uDefs[uKey(c0)]; ok {
			c.Name, c.NameJa, c.Defined = d.name, d.ja, true
		} else {
			c.Name, c.NameJa = fmt.Sprintf("U?%02X", c0), "未定義の非番号制"
		}
		return c, 1, ""
	}
	if len(b) < 2 {
		c.Octets = 1
		return c, 1, c.Format.String() + " フォーマットの第 2 オクテットが無い（無効フレーム）"
	}
	c.NR = b[1] >> 2
	c.PF = b[1]&2 != 0
	ec := b[1]&1 == 1
	i := 2
	if !ec {
		c.Partial = true
		for {
			if i >= len(b) {
				c.Octets = i
				return c, i, "部分再送のユニット番号が終端しない（EC=1 が無い）"
			}
			c.Units = append(c.Units, b[i]>>1)
			ec = b[i]&1 == 1
			i++
			if ec {
				break
			}
		}
	}
	c.Octets = i
	switch {
	case c.Format == FormatI:
		c.Defined = true
		if c.Partial {
			c.Name, c.NameJa = "I'", "部分再送制御情報"
		}
	case c.SBits == 3:
	case c.Partial:
		if c.SBits == 2 {
			c.Name, c.NameJa, c.Defined = "REJ'", "部分再送制御リジェクト", true
		} else {
			c.Name += "'"
			c.NameJa += "（部分再送・未定義）"
		}
	default:
		c.Defined = true
	}
	return c, i, ""
}


type Field struct{ Name, Value string }

func bitsStr(v uint64, n int) string {
	if n <= 0 {
		return ""
	}
	return fmt.Sprintf("%0*b", n, v)
}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}

func (f Frame) Fields() []Field {
	var out []Field
	add := func(name, format string, args ...any) {
		out = append(out, Field{name, fmt.Sprintf(format, args...)})
	}
	b := f.Raw
	add("フレーム長", "%d オクテット", len(b))
	if len(b) == 0 {
		return out
	}
	a := f.Addr
	add("アドレス 第1オクテット", "%08b", b[0])
	add("ID 制御 AC（ビット 8-7）", "%s = %s", bitsStr(uint64(b[0]>>6), 2), acNames[b[0]>>6])
	ai := (b[0] >> 4) & 3
	aiMean := [...]string{"SMSI なし・MSI なし", "MSI のみ", "SMSI のみ", "SMSI・MSI あり"}[ai]
	add("ID 表示 AI（ビット 6-5）", "%s = %s", bitsStr(uint64(ai), 2), aiMean)
	sapi := (b[0] >> 1) & 7
	sapiMean := "予約"
	switch sapi {
	case SAPICallControl:
		sapiMean = "呼制御手順"
	case SAPIL2Mgmt:
		sapiMean = "レイヤ 2 マネジメント手順"
	}
	add("SAPI（ビット 4-2）", "%s = %d %s", bitsStr(uint64(sapi), 3), sapi, sapiMean)
	cr := b[0] & 1
	crMean := "基地局→移動局ならレスポンス / 移動局→基地局ならコマンド"
	if cr == 1 {
		crMean = "基地局→移動局ならコマンド / 移動局→基地局ならレスポンス"
	}
	if cmd, ok := f.Command(); ok {
		crMean = map[bool]string{true: "コマンド", false: "レスポンス"}[cmd] + map[Dir]string{DirDown: "（基地局→移動局）", DirUp: "（移動局→基地局）"}[f.Dir]
	}
	add("C/R（ビット 1）", "%d = %s", cr, crMean)
	i := 1
	if ai&2 != 0 && i < len(b) {
		add("SMSI", "%08b = %02X", b[i], b[i])
		i++
	}
	if ai&1 != 0 {
		for k := 0; i < len(b) && k < maxMSIOctets; k++ {
			ea := b[i] & 1
			add(fmt.Sprintf("MSI 第%dオクテット", k+1), "%s EA=%d", bitsStr(uint64(b[i]>>1), 7), ea)
			i++
			if ea == 1 {
				break
			}
		}
		if a.HasMSI() && a.MSIBits > 0 {
			if a.IsGlobal() {
				add("MSI", "127 = グローバル MSI（放送）")
			} else {
				add("MSI", "%s（%d ビット格納, 10 進 %d）", a.msiHex(), a.MSIBits, a.MSI)
				if m, ok := a.MSInfo(); ok {
					call := a.MSI & 0xFFFFF
					kind := "グループ番号"
					if m.Individual {
						kind = "個別番号"
					}
					add("呼出符号（MSI 下位 20 ビット）", "識別子=%d（%s） 局番号=%d", call>>19, kind, m.Number)
					if a.MSI>>20 == 0 {
						add("システムコード（MSI 上位）", "0（グループ番号 90 番台など）")
					} else {
						add("システムコード（MSI 上位）", "%s", m.System.Text())
						add("団体（MSI）", "%s", m.System.Org)
					}
				}
			}
		}
	}
	if a.Octets > 0 {
		add("アドレス長", "%d オクテット", a.Octets)
		if !a.ValidIDCombo() {
			add("ID の組合せ", "AC=%02b AI=%02b は許されない（表 4.2.2.2-1）", a.AC, a.AI)
		}
	}
	c := f.Ctl
	if a.Octets > 0 && a.Octets < len(b) {
		cb := b[a.Octets:]
		c0 := cb[0]
		add("制御 第1オクテット", "%08b", c0)
		switch c.Format {
		case FormatI:
			add("形式（ビット 2-1）", "00 = I（情報転送）")
			add("N(S)（ビット 8-3）", "%s = %d", bitsStr(uint64(c0>>2), 6), c0>>2)
		case FormatS:
			add("形式（ビット 2-1）", "01 = S（監視）")
			add("予備（ビット 8-5）", "%s", bitsStr(uint64(c0>>4), 4))
			add("S1 S2（ビット 4-3）", "%s = %s（%s）", bitsStr(uint64(c.SBits), 2), sNames[c.SBits][0], sNames[c.SBits][1])
		case FormatU:
			add("形式（ビット 2）", "1 = U（非番号制）")
			add("修飾機能ビット M（ビット 8-6, 4-3, 1）", "%s %s %d", bitsStr(uint64(c0>>5), 3), bitsStr(uint64((c0>>2)&3), 2), c0&1)
			add("P/F（ビット 5）", "%d", b2i(c.PF))
			add("コマンド/レスポンス", "%s（%s）", c.Name, c.NameJa)
		}
		if c.Format != FormatU && len(cb) >= 2 {
			c1 := cb[1]
			add("制御 第2オクテット", "%08b", c1)
			add("N(R)（ビット 8-3）", "%s = %d", bitsStr(uint64(c1>>2), 6), c1>>2)
			pf := "P/F"
			if c.Format == FormatI {
				pf = "P"
			}
			add(pf+"（ビット 2）", "%d", (c1>>1)&1)
			ecMean := "最終オクテット（基本再送制御）"
			if c1&1 == 0 {
				ecMean = "続く（部分再送制御）"
			}
			add("EC（ビット 1）", "%d = %s", c1&1, ecMean)
			un := "n(R)"
			if c.Format == FormatI {
				un = "n(S)"
			}
			for k, u := range c.Units {
				ec := cb[2+k] & 1
				add(fmt.Sprintf("%s 第%d（ビット 8-2）", un, k+1), "%s = %d EC=%d", bitsStr(uint64(u), 7), u, ec)
			}
			add("フレーム種別", "%s（%s）", c.Name, c.NameJa)
		}
		if c.Octets > 0 {
			add("制御長", "%d オクテット", c.Octets)
		}
	}
	if f.Err == "" || len(f.Info) > 0 {
		add("情報フィールド長", "%d オクテット", len(f.Info))
	}
	if m, ok := f.Management(); ok {
		add("マネジメントエンティティ識別子", "%s", m.Entity)
		add("メッセージ種別（SMSI チェック手順）", "%s", m.Name)
		if m.Rest != "" {
			add("後続", "%s", m.Rest)
		}
	}
	switch {
	case f.Ctl.Name == "FRMR" && len(f.Info) > 0:
		out = append(out, f.frmrFields()...)
	case f.Ctl.Name == "XID" && len(f.Info) > 0:
		add("識別情報（XID）", "%X（手順は規定しない。受信側は通知せず廃棄する §4.2.2.3.8.12）", f.Info)
	}
	return out
}

func (f Frame) frmrFields() []Field {
	b := f.Info
	var out []Field
	add := func(name, format string, args ...any) { out = append(out, Field{name, fmt.Sprintf(format, args...)}) }
	if len(b) < 5 {
		add("FRMR 情報フィールド", "%X（%d オクテット。図 4.2.2.3-5 は 5 オクテット）", b, len(b))
		return out
	}
	rej, _, _ := parseControl(b[:2])
	rejText := rej.Name
	if rej.Format == FormatU {
		rejText += fmt.Sprintf("（非番号制。オクテット 2 = %08b、規定は 0）", b[1])
	} else {
		rejText = rej.String()
	}
	add("リジェクトされたフレームの制御フィールド", "%02X %02X = %s", b[0], b[1], rejText)
	add("V(S)（オクテット 3 ビット 8-2）", "%d", b[2]>>1)
	if b[2]&1 != 0 {
		add("オクテット 3 ビット 1", "1（規定は 0）")
	}
	add("V(R)（オクテット 4 ビット 8-2）", "%d", b[3]>>1)
	add("C/R（オクテット 4 ビット 1）", "%d = リジェクトされたフレームは%s", b[3]&1, map[byte]string{0: "コマンド", 1: "レスポンス"}[b[3]&1])
	var why []string
	for _, r := range []struct {
		bit  byte
		name string
	}{{1, "W: 制御フィールドが未定義・未実装"}, {2, "X: 情報フィールドを持てないフレームに情報フィールド、または長さの正しくない監視・非番号制フレーム"},
		{4, "Y: 情報フィールドが N201 を超える"}, {8, "Z: N(R) が無効（V(A) ≦ N(R) ≦ V(S) の外）"}} {
		if b[4]&r.bit != 0 {
			why = append(why, r.name)
		}
	}
	if len(why) == 0 {
		why = []string{"理由ビット無し"}
	}
	add("理由（オクテット 5 ビット 4-1 Z Y X W）", "%04b = %s", b[4]&0xF, strings.Join(why, " / "))
	if b[4]&2 != 0 && b[4]&1 == 0 {
		add("規格の検査", "X = 1 のとき W も 1（§4.2.2.3.8.11 カ）")
	}
	if b[4]>>4 != 0 {
		add("オクテット 5 ビット 8-5", "%04b（規定は 0）", b[4]>>4)
	}
	if len(b) > 5 {
		add("後続", "%X", b[5:])
	}
	return out
}

func N201(ch string) int {
	if ch == "BCCH" {
		return 162
	}
	return 260
}

func (f Frame) ChannelNotes(ch string) []string {
	if f.Addr.Octets == 0 {
		return nil
	}
	var n []string
	a := f.Addr
	if a.SAPI != SAPICallControl && a.SAPI != SAPIL2Mgmt {
		n = append(n, fmt.Sprintf("SAPI %d は未サポート（0 呼制御・7 レイヤ 2 マネジメントだけ。§4.2.2.2.8 (3) の無効フレーム）", a.SAPI))
	}
	if ch != "CAC" && len(f.Info) > N201(ch) {
		n = append(n, fmt.Sprintf("情報フィールド %d オクテットが %s の N201 = %d を超える（表 4.2.2.5-3）", len(f.Info), ch, N201(ch)))
	}
	if a.SAPI == SAPIL2Mgmt {
		return n
	}
	switch ch {
	case "DCCH":
		if a.AI != 1 {
			n = append(n, fmt.Sprintf("DCCH のアドレスは常に AI = 01（§4.5.2.2.2）が AI = %02b", a.AI))
		}
	case "SACCH", "FACCH", "DACCH":
		if a.AI != 0 {
			n = append(n, fmt.Sprintf("%s のアドレスは常に AI = 00（§4.2.2.3.3.7）が AI = %02b", ch, a.AI))
		}
	case "SCCH", "UPCH":
		if a.AI == 0 || a.AI == 3 && a.AC == 0 {
			n = append(n, fmt.Sprintf("%s のアドレスは AI = 01（MSI のみ）か 10（SMSI のみ）（§4.2.2.3.3.7）が AI = %02b", ch, a.AI))
		}
	case "BCCH", "PCH":
		if a.HasSMSI() {
			n = append(n, fmt.Sprintf("SMSI は SCCH・UPCH だけ（§4.2.2.3.3.7）が %s で SMSI 0x%02X", ch, a.SMSI))
		}
	}
	return n
}

type Management struct {
	Entity string
	Name   string
	Rest   string
}

func (f Frame) Management() (Management, bool) {
	if f.Addr.SAPI != 7 || f.Ctl.Name != "UI" || len(f.Info) == 0 {
		return Management{}, false
	}
	m := Management{Entity: fmt.Sprintf("0x%02X（保留）", f.Info[0])}
	if f.Info[0] == 0x0F {
		m.Entity = "0x0F（SMSI チェック手順）"
	}
	if len(f.Info) < 2 {
		m.Name = "（メッセージ種別が無い）"
		return m, true
	}
	switch f.Info[1] {
	case 0x04:
		m.Name = "ID チェック要求"
	case 0x05:
		m.Name = "ID チェック応答"
	default:
		m.Name = fmt.Sprintf("予約(0x%02X)", f.Info[1])
	}
	if len(f.Info) > 2 {
		m.Rest = fmt.Sprintf("% X", f.Info[2:])
	}
	return m, true
}


type Unit struct {
	First, Last bool
	WO          int
	Data []byte
}

const MaxUnits = 64

type Reassembler struct {
	slots   [][]byte
	have    []bool
	n       int
	open    bool
	Dropped int
	prev *partial
}

type partial struct {
	slots [][]byte
	have  []bool
}

func (r *Reassembler) Reset() { r.slots, r.have, r.n, r.open = nil, nil, 0, false }

func (r *Reassembler) Pending() (open bool, haveOctets, missingUnits int) {
	if !r.open {
		return false, 0, 0
	}
	for i, ok := range r.have {
		if ok {
			haveOctets += len(r.slots[i])
		} else {
			missingUnits++
		}
	}
	return true, haveOctets, missingUnits
}

func (r *Reassembler) Push(u Unit) []byte {
	switch {
	case u.First && u.Last:
		r.keep()
		r.Reset()
		n := clampLen(u.WO, len(u.Data))
		if n == 0 {
			return nil
		}
		return append([]byte(nil), u.Data[:n]...)

	case u.First:
		n := u.WO + 1
		if n < 2 || n > MaxUnits {
			r.Dropped++
			return nil
		}
		if r.open && (r.n != n || (r.have[0] && !bytes.Equal(r.slots[0], u.Data))) {
			r.Dropped++
			r.keep()
			r.Reset()
		}
		if !r.open {
			r.start(n)
		}
		r.put(0, u.Data)
		return r.finish()

	case u.Last:
		if !r.open {
			r.Dropped++
			return nil
		}
		r.put(r.n-1, u.Data[:clampLen(u.WO, len(u.Data))])
		return r.finish()

	case u.WO == 0:
		return nil

	default:
		if !r.open {
			r.Dropped++
			return nil
		}
		idx := r.n - 1 - u.WO
		if idx <= 0 || idx >= r.n-1 {
			r.Dropped++
			return nil
		}
		r.put(idx, u.Data)
		return r.finish()
	}
}

func (r *Reassembler) keep() {
	if !r.open {
		return
	}
	if r.prev != nil && r.prev.fill(r.slots, r.have) {
		return
	}
	r.prev = &partial{slots: r.slots, have: r.have}
}

func head(slots [][]byte, have []bool) []byte {
	var b []byte
	for k := range slots {
		if !have[k] {
			break
		}
		b = append(b, slots[k]...)
	}
	return b
}

func (p *partial) fill(ip [][]byte, ipHave []bool) bool {
	f := Parse(head(ip, ipHave))
	if f.Addr.Octets == 0 || f.Ctl.Name != "I'" || len(f.Ctl.Units) == 0 {
		return false
	}
	orig := Parse(head(p.slots, p.have))
	if orig.Ctl.Format != FormatI || orig.Ctl.Partial || orig.Ctl.NS != f.Ctl.NS || orig.Addr.Key() != f.Addr.Key() {
		return false
	}
	n := len(p.slots)
	units := f.Ctl.Units
	if len(units) == 2 && units[1] == 0 && units[0] > 0 {
		var all []uint8
		for k := int(units[0]); k < n; k++ {
			all = append(all, uint8(k))
		}
		units = all
	}
	h, off := 0, 0
	for hl := f.Addr.Octets + f.Ctl.Octets; off < hl && h < len(ip); h++ {
		off += len(ip[h])
	}
	for j, u := range units {
		if k := h + j; k < len(ip) && ipHave[k] && int(u) < n {
			p.slots[u], p.have[u] = ip[k], true
		}
	}
	return true
}

func (r *Reassembler) Recover(msg []byte, first, unit int) ([]byte, bool) {
	if r.prev == nil || first <= 0 || unit <= 0 {
		return nil, false
	}
	var ip [][]byte
	for off, k := 0, 0; off < len(msg); k++ {
		sz := unit
		if k == 0 {
			sz = first
		}
		end := min(off+sz, len(msg))
		ip = append(ip, msg[off:end])
		off = end
	}
	have := make([]bool, len(ip))
	for k := range have {
		have[k] = true
	}
	if !r.prev.fill(ip, have) {
		return nil, false
	}
	for _, ok := range r.prev.have {
		if !ok {
			return nil, false
		}
	}
	out := head(r.prev.slots, r.prev.have)
	r.prev = nil
	return out, true
}

func (r *Reassembler) start(n int) {
	r.slots = make([][]byte, n)
	r.have = make([]bool, n)
	r.n, r.open = n, true
}

func (r *Reassembler) put(i int, data []byte) {
	if i < 0 || i >= r.n {
		return
	}
	r.slots[i] = append([]byte(nil), data...)
	r.have[i] = true
}

func (r *Reassembler) finish() []byte {
	for _, ok := range r.have {
		if !ok {
			return nil
		}
	}
	var msg []byte
	for _, s := range r.slots {
		msg = append(msg, s...)
	}
	r.Reset()
	return msg
}

func clampLen(l, max int) int {
	if l < 0 {
		return 0
	}
	if l > max {
		return max
	}
	return l
}

