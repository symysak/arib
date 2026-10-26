package l2

import (
	"bytes"
	"fmt"
	"github.com/symysak/arib/internal/bitpack"
	"strings"
)

type Format int

const (
	FormatI Format = iota
	FormatS
	FormatU
)

func (f Format) String() string { return [...]string{"I", "S", "U"}[f] }

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

func (a Address) HasMSI() bool  { return a.AI&1 == 1 }
func (a Address) HasSMSI() bool { return a.AI&2 == 2 }

func (a Address) IsGlobal() bool { return a.HasMSI() && a.MSIBits == 7 && a.MSI == 127 }

func (a Address) String() string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "SAPI=%d", a.SAPI)
	if a.CR {
		sb.WriteString(" C")
	} else {
		sb.WriteString(" R")
	}
	if a.HasSMSI() {
		fmt.Fprintf(&sb, " SMSI=%d", a.SMSI)
	}
	if a.HasMSI() {
		if a.MSIBits == 0 {
			sb.WriteString(" MSI=?")
		} else if a.IsGlobal() {
			sb.WriteString(" MSI=全局(127)")
		} else {
			fmt.Fprintf(&sb, " MSI=%d", a.MSI)
		}
	}
	if a.AC != 0 {
		sb.WriteString(" AC=" + [...]string{"", "SMSI割当", "SMSI解除", "MSI+SMSI要求"}[a.AC])
	}
	return sb.String()
}

type Control struct {
	Format Format
	Name   string
	NS, NR uint8
	PF     bool
	Partial []uint8
	Octets  int
}

func (c Control) PartialUndefined() bool {
	return c.Format == FormatS && len(c.Partial) > 0 && c.Name != "REJ'"
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
	if len(c.Partial) > 0 {
		fmt.Fprintf(&sb, " n=%v", c.Partial)
	}
	return sb.String()
}

type Frame struct {
	Raw     []byte
	Address Address
	Control Control
	Info    []byte
	Err string
	Note string
	Dir Dir
	Channel string
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

var noInfoU = map[string]string{"SABME": "4.2.2.3.8.2", "DISC": "4.2.2.3.8.4", "UA": "4.2.2.3.8.9", "DM": "4.2.2.3.8.10"}

func (f Frame) Notes() []string {
	if f.Err != "" && f.Address.Octets == 0 {
		return nil
	}
	var n []string
	a := f.Address
	if !a.ValidIDCombo() {
		n = append(n, fmt.Sprintf("ID 制御 AC=%02b と ID 表示 AI=%02b の組合せは許されない（表 4.2.2.2-1。§4.2.2.2.8 (4) の無効フレーム）", a.AC, a.AI))
	}
	if a.SAPI != 0 && a.SAPI != 7 {
		n = append(n, fmt.Sprintf("SAPI %d は予約（0 呼制御・7 レイヤ 2 マネジメントだけ。§4.2.2.2.8 (3) の無効フレーム）", a.SAPI))
	}
	c := f.Control
	switch {
	case c.Format == FormatS && len(f.Info) > 0:
		n = append(n, fmt.Sprintf("監視フレームの後ろに %d オクテット余る（長さが正しくない監視フレーム。§4.2.2.3.8.11 (2)）", len(f.Info)))
	case c.Format == FormatU && len(f.Info) > 0 && noInfoU[c.Name] != "":
		n = append(n, fmt.Sprintf("%s は情報フィールドを持ってはならない（§%s）", c.Name, noInfoU[c.Name]))
	}
	if c.PartialUndefined() {
		n = append(n, c.Name+" は規格に無い（部分再送の監視フレームは REJ' だけ。図 4.2.2.3-4）")
	}
	if cmd, ok := f.Command(); ok && c.Octets > 0 {
		switch c.Name {
		case "UA", "DM", "FRMR":
			if cmd {
				n = append(n, c.Name+" はレスポンスに限るがコマンドで来た（表 4.2.2.3-1 の C/R の向き）")
			}
		case "SABME", "SABMEI", "DISC", "UI", "I", "I'":
			if !cmd {
				n = append(n, c.Name+" はコマンドに限るがレスポンスで来た（表 4.2.2.3-1 の C/R の向き）")
			}
		}
	}
	if (f.Channel == "SACCH" || f.Channel == "FACCH") && a.AI != 0 && a.SAPI != 7 {
		n = append(n, fmt.Sprintf("%s は常に AI=00（MSI・SMSI なし）のはずが AI=%02b（§4.2.2.3.3.7）", f.Channel, a.AI))
	}
	return n
}

type Dir int

const (
	DirUnknown Dir = iota
	DirDown
	DirUp
)

func (f Frame) Command() (cmd, ok bool) {
	switch f.Dir {
	case DirDown:
		return f.Address.CR, true
	case DirUp:
		return !f.Address.CR, true
	}
	return false, false
}

func (f Frame) CarriesInfo() bool {
	if f.Address.SAPI == 7 {
		return false
	}
	switch f.Control.Name {
	case "I", "UI", "SABMEI":
		return true
	}
	return false
}

type FRMR struct {
	Rejected [2]byte
	VS, VR   uint8
	CR         bool
	W, X, Y, Z bool
	Reserved uint8
	Short    bool
}

func (f Frame) FRMRInfo() (FRMR, bool) {
	if f.Control.Name != "FRMR" {
		return FRMR{}, false
	}
	var r FRMR
	b := f.Info
	if len(b) < 5 {
		r.Short = true
		b = append(append([]byte(nil), b...), make([]byte, 5-len(b))...)
	}
	r.Rejected = [2]byte{b[0], b[1]}
	r.VS, r.VR = b[2]>>1, b[3]>>1
	r.CR = b[3]&1 == 1
	r.W, r.X, r.Y, r.Z = b[4]&1 != 0, b[4]&2 != 0, b[4]&4 != 0, b[4]&8 != 0
	r.Reserved = b[2]&1 | (b[4]>>4)<<1
	return r, true
}

func (r FRMR) Text() string {
	var why []string
	for _, c := range []struct {
		on   bool
		name string
	}{{r.W, "W 未定義の制御フィールド"}, {r.X, "X 無効な制御フィールド"}, {r.Y, "Y 情報フィールド長超過"}, {r.Z, "Z 無効な N(R)"}} {
		if c.on {
			why = append(why, c.name)
		}
	}
	if len(why) == 0 {
		why = append(why, "理由ビット無し")
	}
	kind := "コマンド"
	if r.CR {
		kind = "レスポンス"
	}
	s := fmt.Sprintf("リジェクトされた制御=%02X %02X（%s） V(S)=%d V(R)=%d 理由=%s", r.Rejected[0], r.Rejected[1], kind, r.VS, r.VR,
		strings.Join(why, "・"))
	if r.X && !r.W {
		s += "（X=1 なら W=1 のはず）"
	}
	if r.Reserved != 0 {
		s += fmt.Sprintf(" 0 のはずのビット=%d", r.Reserved)
	}
	if r.Short {
		s += "（5 オクテットに足りない）"
	}
	return s
}

type Management struct {
	Entity string
	Name   string
	Rest   []byte
}

func (f Frame) Management() (Management, bool) {
	if f.Address.SAPI != 7 || f.Control.Name != "UI" || len(f.Info) == 0 {
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
		m.Rest = f.Info[2:]
	}
	return m, true
}

func Parse(b []byte) Frame {
	f := Frame{Raw: b}
	if len(b) < 2 {
		f.Err = "2 オクテット未満"
		return f
	}
	a := Address{}
	a.AC = b[0] >> 6
	a.AI = (b[0] >> 4) & 3
	a.SAPI = (b[0] >> 1) & 7
	a.CR = b[0]&1 == 1
	i := 1
	if a.HasSMSI() {
		if i >= len(b) {
			f.Err = "SMSI が足りない"
			return f
		}
		a.SMSI = b[i]
		i++
	}
	if a.HasMSI() {
		var v uint64
		bits := 0
		for {
			if i >= len(b) {
				f.Err = "MSI が終端しない"
				return f
			}
			v = v<<7 | uint64(b[i]>>1)
			bits += 7
			ea := b[i]&1 == 1
			i++
			if ea || bits >= 70 {
				break
			}
		}
		a.MSI, a.MSIBits = v, bits
	}
	a.Octets = i
	f.Address = a
	if i >= len(b) {
		f.Err = "制御フィールドが無い"
		return f
	}
	c := Control{}
	c0 := b[i]
	start := i
	switch {
	case c0&3 == 0:
		c.Format, c.Name = FormatI, "I"
		c.NS = c0 >> 2
		i++
		if i >= len(b) {
			f.Err = "I フォーマットの第 2 オクテットが無い"
			return f
		}
		c.NR = b[i] >> 2
		c.PF = b[i]&2 == 2
		ec := b[i]&1 == 1
		i++
		for !ec {
			if i >= len(b) {
				f.Err = "I' の n(S) が終端しない"
				return f
			}
			c.Partial = append(c.Partial, b[i]>>1)
			ec = b[i]&1 == 1
			i++
		}
		if len(c.Partial) > 0 {
			c.Name = "I'"
		}
	case c0&3 == 1:
		c.Format = FormatS
		switch (c0 >> 2) & 3 {
		case 0:
			c.Name = "RR"
		case 1:
			c.Name = "RNR"
		case 2:
			c.Name = "REJ"
		default:
			c.Name = "S?"
		}
		i++
		if i >= len(b) {
			f.Err = "S フォーマットの第 2 オクテットが無い"
			return f
		}
		c.NR = b[i] >> 2
		c.PF = b[i]&2 == 2
		ec := b[i]&1 == 1
		i++
		for !ec {
			if i >= len(b) {
				f.Err = "S' の n(R) が終端しない"
				return f
			}
			c.Partial = append(c.Partial, b[i]>>1)
			ec = b[i]&1 == 1
			i++
		}
		if len(c.Partial) > 0 {
			c.Name += "'"
		}
	default:
		c.Format = FormatU
		c.PF = c0&0x10 == 0x10
		switch c0 &^ 0x10 {
		case 0x6F:
			c.Name = "SABME"
		case 0x6E:
			c.Name = "SABMEI"
		case 0x0F:
			c.Name = "DM"
		case 0x03:
			c.Name = "UI"
		case 0x43:
			c.Name = "DISC"
		case 0x63:
			c.Name = "UA"
		case 0x87:
			c.Name = "FRMR"
		case 0xAF:
			c.Name = "XID"
		default:
			c.Name = fmt.Sprintf("U?%02X", c0)
			f.Err = "未定義の制御フィールド"
		}
		i++
	}
	c.Octets = i - start
	f.Control = c
	f.Info = b[i:]
	return f
}

type Unit struct {
	First, Last bool
	WO          uint8
	Data        []byte
}

type Reassembler struct {
	slots   [][]byte
	have    []bool
	n       int
	open    bool
	Dropped int
	prev *partial
}

func (r *Reassembler) Pending() (open bool, haveOctets, remainUnits int) {
	if !r.open {
		return false, 0, 0
	}
	oct, miss := 0, 0
	for i, ok := range r.have {
		if ok {
			oct += len(r.slots[i])
		} else {
			miss++
		}
	}
	return true, oct, miss
}

func (r *Reassembler) Push(u Unit) []byte {
	switch {
	case u.First && u.Last:
		r.keep()
		r.reset()
		return u.Data[:clampLen(int(u.WO), len(u.Data))]

	case u.First:
		n := int(u.WO) + 1
		if r.open && (r.n != n || !bytes.Equal(r.slots[0], u.Data)) {
			r.Dropped++
			r.keep()
			r.reset()
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
		r.put(r.n-1, u.Data[:clampLen(int(u.WO), len(u.Data))])
		return r.finish()

	case u.WO == 0:
		return nil

	default:
		if !r.open {
			r.Dropped++
			return nil
		}
		idx := r.n - 1 - int(u.WO)
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

type partial struct {
	slots [][]byte
	have  []bool
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
	if f.Address.Octets == 0 || f.Control.Name != "I'" || len(f.Control.Partial) == 0 {
		return false
	}
	orig := Parse(head(p.slots, p.have))
	if orig.Control.Format != FormatI || len(orig.Control.Partial) != 0 || orig.Control.NS != f.Control.NS ||
		!sameAddress(orig.Address, f.Address) {
		return false
	}
	n := len(p.slots)
	units := f.Control.Partial
	if len(units) == 2 && units[1] == 0 && units[0] > 0 {
		var all []uint8
		for k := int(units[0]); k < n; k++ {
			all = append(all, uint8(k))
		}
		units = all
	}
	h, off := 0, 0
	for hl := f.Address.Octets + f.Control.Octets; off < hl && h < len(ip); h++ {
		off += len(ip[h])
	}
	for j, u := range units {
		if k := h + j; k < len(ip) && ipHave[k] && int(u) < n {
			p.slots[u], p.have[u] = ip[k], true
		}
	}
	return true
}

func sameAddress(a, b Address) bool {
	return a.AC == b.AC && a.AI == b.AI && a.SAPI == b.SAPI && a.SMSI == b.SMSI && a.MSI == b.MSI && a.MSIBits == b.MSIBits
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
	r.reset()
	return msg
}

func (r *Reassembler) reset() { r.slots, r.have, r.n, r.open = nil, nil, 0, false }

func clampLen(l, max int) int {
	if l > max {
		return max
	}
	return l
}

func BitsToBytes(bits []uint8) []byte { return bitpack.ToBytes(bits) }
