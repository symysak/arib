package l3

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/symysak/arib/internal/sjis"
)


func CRC32(p []byte) uint32 {
	c := uint32(0xFFFFFFFF)
	for _, b := range p {
		c ^= uint32(b) << 24
		for i := 0; i < 8; i++ {
			if c&0x80000000 != 0 {
				c = c<<1 ^ 0x04C11DB7
			} else {
				c <<= 1
			}
		}
	}
	return c
}

func Keystream(key uint16, n int) []uint8 {
	l := uint32(key) & 0x7FFF
	out := make([]uint8, n)
	for i := range out {
		out[i] = uint8(l & 1)
		fb := (l ^ l>>1) & 1
		l = l>>1 | fb<<14
	}
	return out
}

func Descramble(p []byte, key uint16) []byte {
	ks := Keystream(key, 8*len(p))
	out := make([]byte, len(p))
	for i := range p {
		var m byte
		for k := 0; k < 8; k++ {
			m |= ks[8*i+k] << (7 - k)
		}
		out[i] = p[i] ^ m
	}
	return out
}

type B54Event struct {
	Kind string
	Call *B54
	Name string
	GPS *GPSFix
	Data []byte
	Command int
	Text string
	CRCOK bool
	Key        uint16
	KeyFound   bool
	KeyUnknown bool
	Packets int
	Raw []byte
}

func (e B54Event) Label() string {
	who := ""
	if e.Call != nil {
		who = e.Call.CallText()
	}
	switch e.Kind {
	case "name":
		return fmt.Sprintf("発信者名「%s」", e.Name)
	case "gps":
		if e.GPS == nil {
			return fmt.Sprintf("GPS（暗号・鍵不明）%s", who)
		}
		return fmt.Sprintf("GPS %s %s", e.GPS.Text(), who)
	case "status":
		return fmt.Sprintf("ステータス %d %s", e.Call.Status, who)
	}
	s := map[string]string{"bearer": "ベアラ", "message": "メッセージ"}[e.Kind]
	switch {
	case e.KeyUnknown:
		s += "（暗号・鍵不明）"
	case !e.CRCOK:
		s += "（CRC 誤り）"
	case e.Text != "":
		s += "「" + e.Text + "」"
	case e.Command >= 0 && e.Command != 0x83 && e.Command != 0x84:
		s += fmt.Sprintf("（コマンド 0x%02X = 未定義・%d バイト）", e.Command, len(e.Data))
	default:
		s += fmt.Sprintf(" %d バイト", len(e.Data))
	}
	return s + " " + who
}

type B54Asm struct {
	Keys map[int]uint16
	found map[int]uint16
	seen bool

	name     [3][]byte
	nameHave [3]bool
	lastName string
	call     *B54

	gpsHead *B54

	pkt  *packet
	sess *session
}

type packet struct {
	head   *B54
	blocks [16][]byte
	have   [16]bool
}

type session struct {
	kind    string
	head    *B54
	data    []byte
	packets int
	lastFrag int
	crcOK    bool
	key      uint16
	found    bool
	unknown  bool
	command  int
	raw      []byte
}

func NewB54Asm(keys map[int]uint16) *B54Asm {
	return &B54Asm{Keys: keys, found: map[int]uint16{}}
}

func (a *B54Asm) KeyFor(no int) (uint16, bool) {
	if k, ok := a.Keys[no]; ok && k != 0 {
		return k, true
	}
	k, ok := a.found[no]
	return k, ok
}

func (a *B54Asm) KnownKeys() map[int]struct {
	Key   uint16
	Found bool
} {
	out := map[int]struct {
		Key   uint16
		Found bool
	}{}
	for no, k := range a.Keys {
		out[no] = struct {
			Key   uint16
			Found bool
		}{k, false}
	}
	for no, k := range a.found {
		if _, set := out[no]; !set {
			out[no] = struct {
				Key   uint16
				Found bool
			}{k, true}
		}
	}
	return out
}

func (a *B54Asm) Seen() bool { return a.seen }

func (a *B54Asm) Call() *B54 { return a.call }

func (a *B54Asm) Reset() []B54Event {
	var out []B54Event
	if a.sess != nil && a.sess.kind == "bearer" {
		out = a.flushSession()
	}
	a.name = [3][]byte{}
	a.nameHave = [3]bool{}
	a.gpsHead, a.pkt, a.call = nil, nil, nil
	return out
}

func (a *B54Asm) Push(m Message) []B54Event {
	x := m.B54
	if x == nil {
		return nil
	}
	a.seen = true
	var out []B54Event
	switch x.Type {
	case TypeSelVoice:
		a.call = x
	case TypeSelEnd:
		if a.sess != nil && a.sess.kind == "bearer" {
			out = append(out, a.flushSession()...)
		}
	case TypeCallerName:
		out = append(out, a.pushName(x)...)
	case TypeGPSHeader:
		a.gpsHead, a.call = x, x
	case TypeGPSData:
		out = append(out, a.pushGPS(x))
	case TypeBearerHeader, TypeMsgHeader:
		a.call = x
		out = append(out, a.startPacket(x)...)
	case TypeBearerData, TypeMsgData:
		out = append(out, a.pushData(x)...)
	case TypeStatusReq:
		out = append(out, B54Event{Kind: "status", Call: x, Command: -1})
	}
	return out
}

func (a *B54Asm) pushName(x *B54) []B54Event {
	i := x.Seg - 1
	a.name[i], a.nameHave[i] = append([]byte(nil), x.Payload...), true
	if !a.nameHave[0] || !a.nameHave[1] || !a.nameHave[2] {
		return nil
	}
	raw := append(append(append([]byte(nil), a.name[0]...), a.name[1]...), a.name[2]...)
	a.nameHave = [3]bool{}
	body, crc := raw[:14], uint32(raw[14])<<24|uint32(raw[15])<<16|uint32(raw[16])<<8|uint32(raw[17])
	if CRC32(body) != crc {
		return nil
	}
	name := DecodeName(body)
	if name == a.lastName {
		return nil
	}
	a.lastName = name
	return []B54Event{{Kind: "name", Call: a.call, Name: name, CRCOK: true, Raw: raw, Command: -1}}
}

func DecodeName(p []byte) string {
	if i := bytes.IndexByte(p, 0); i >= 0 {
		p = p[:i]
	}
	s, _ := sjis.Decode(p)
	return s
}

func (a *B54Asm) pushGPS(x *B54) B54Event {
	h := a.gpsHead
	a.gpsHead = nil
	ev := B54Event{Kind: "gps", Call: h, Command: -1, Raw: x.Payload}
	p := x.Payload
	if h != nil && h.Enc == 1 {
		key, ok := a.KeyFor(h.KeyNo)
		if !ok {
			ev.KeyUnknown = true
			return ev
		}
		p, ev.Key = Descramble(p, key), key
	}
	g := ParseGPS(p)
	ev.GPS = &g
	return ev
}

func (a *B54Asm) startPacket(h *B54) []B54Event {
	var out []B54Event
	kind := "bearer"
	if h.Type == TypeMsgHeader {
		kind = "message"
	}
	if a.sess != nil && (a.sess.kind != kind || (h.First && !h.Select)) {
		out = append(out, a.flushSession()...)
	}
	if h.Select && a.pkt != nil && a.pkt.head.Type == h.Type {
		return out
	}
	a.pkt = &packet{head: h}
	return out
}

func (a *B54Asm) pushData(x *B54) []B54Event {
	p := a.pkt
	want := TypeBearerHeader
	if x.Type == TypeMsgData {
		want = TypeMsgHeader
	}
	if p == nil || p.head.Type != want {
		return nil
	}
	if x.BlockNo < 16 {
		p.blocks[x.BlockNo], p.have[x.BlockNo] = x.Payload, true
	}
	if x.FrameNo != 0 {
		return nil
	}
	n := p.head.Blocks
	var area []byte
	for b := n - 1; b >= 0; b-- {
		if !p.have[b] {
			return nil
		}
		area = append(area, p.blocks[b]...)
	}
	a.pkt = nil
	return a.finishPacket(p.head, area)
}

func (a *B54Asm) finishPacket(h *B54, area []byte) []B54Event {
	var out []B54Event
	kind := "bearer"
	if h.Type == TypeMsgHeader {
		kind = "message"
	}
	if a.sess == nil {
		a.sess = &session{kind: kind, head: h, command: -1}
	}
	s := a.sess
	if kind == "message" && s.packets > 0 && h.Fragment == s.lastFrag {
		return nil
	}
	s.lastFrag = h.Fragment
	s.packets++
	ok, key, found, unknown, body := a.unwrap(h, area)
	s.raw = append(s.raw, area...)
	s.key, s.found, s.unknown = key, s.found || found, s.unknown || unknown
	if s.packets == 1 {
		s.crcOK = ok
	} else {
		s.crcOK = s.crcOK && ok
	}
	if kind == "message" && len(body) >= 2 {
		if s.command < 0 {
			s.command = int(body[0])
		}
		body = body[2:]
	}
	s.data = append(s.data, body...)
	if kind == "message" && h.Fragment == 0 {
		out = append(out, a.flushSession()...)
	}
	return out
}

func (a *B54Asm) unwrap(h *B54, area []byte) (ok bool, key uint16, found, unknown bool, body []byte) {
	check := func(p []byte) ([]byte, bool) {
		n := len(p) - h.Null - 4
		if n < 0 {
			return nil, false
		}
		crc := uint32(p[n])<<24 | uint32(p[n+1])<<16 | uint32(p[n+2])<<8 | uint32(p[n+3])
		return p[:n], CRC32(p[:n]) == crc
	}
	if h.Enc != 1 {
		body, ok = check(area)
		return ok, 0, false, false, body
	}
	if k, have := a.KeyFor(h.KeyNo); have {
		if body, ok = check(Descramble(area, k)); ok {
			return true, k, false, false, body
		}
	}
	for k := uint16(1); k <= 0x7FFF; k++ {
		if b, good := check(Descramble(area, k)); good {
			a.found[h.KeyNo] = k
			return true, k, true, false, b
		}
	}
	return false, 0, false, true, nil
}

func (a *B54Asm) Flush() []B54Event { return a.flushSession() }

func (a *B54Asm) flushSession() []B54Event {
	s := a.sess
	a.sess = nil
	if s == nil {
		return nil
	}
	ev := B54Event{Kind: s.kind, Call: s.head, Data: s.data, Command: s.command, CRCOK: s.crcOK, Key: s.key,
		KeyFound: s.found, KeyUnknown: s.unknown, Packets: s.packets, Raw: s.raw}
	if s.kind == "message" && s.command == 0x83 && s.crcOK {
		ev.Text, _ = sjis.Decode(bytes.TrimRight(s.data, "\x00"))
	}
	if s.kind == "message" && s.command == 0x84 && s.crcOK && printable(s.data) {
		ev.Text = string(s.data)
	}
	return []B54Event{ev}
}

func printable(p []byte) bool {
	if len(p) == 0 {
		return false
	}
	for _, c := range p {
		if c < 0x20 || c > 0x7E {
			return false
		}
	}
	return true
}

func CommandName(c int) string {
	switch c {
	case 0x83:
		return "ショートメッセージ"
	case 0x84:
		return "ロングメッセージ"
	case -1:
		return "なし"
	}
	return fmt.Sprintf("0x%02X（未定義 = 網の独自）", c)
}

func (e B54Event) Rows() []Row {
	var rows []Row
	add := func(n, v string) { rows = append(rows, Row{Name: n, Text: v}) }
	if c := e.Call; c != nil && c.Caller >= 0 {
		add("通話形態", FormName(c.Form))
		add("発信者番号", NumberText(c.Caller))
		add("着信者番号又はグループ番号", c.DestText())
		if c.Emergency {
			add("呼制御付加情報", "緊急通信")
		}
		if c.Enc == 1 {
			add("暗号化", fmt.Sprintf("スクランブル暗号・鍵番号 %d", c.KeyNo))
		}
	}
	switch {
	case e.KeyUnknown:
		add("暗号鍵", "不明（設定に無く、CRC の総当たりでも当たらない）")
	case e.KeyFound:
		add("暗号鍵", fmt.Sprintf("%d（CRC で当てた）", e.Key))
	case e.Key != 0:
		add("暗号鍵", fmt.Sprintf("%d", e.Key))
	}
	switch e.Kind {
	case "name":
		add("発信者名", e.Name)
	case "gps":
		if g := e.GPS; g != nil {
			add("緯度", fmt.Sprintf("N%d°%06.3f′（%.6f）", g.LatRaw/100000, float64(g.LatRaw%100000)/1000, g.Lat))
			add("経度", fmt.Sprintf("E%d°%06.3f′（%.6f）", g.LonRaw/100000, float64(g.LonRaw%100000)/1000, g.Lon))
			add("高度", fmt.Sprintf("%d m", g.Alt))
			add("A/V", map[bool]string{true: "A（有効）", false: "V（無効）"}[g.Valid])
			if !g.Sane {
				add("注意", "分が 60 以上など表せない値（暗号の鍵違いか誤り）")
			}
		}
	case "status":
		add("ステータス番号", fmt.Sprintf("%d", e.Call.Status))
		add("確認", map[bool]string{true: "確認型", false: "非確認型"}[e.Call.Confirm])
	case "bearer", "message":
		add("パケット", fmt.Sprintf("%d 個", e.Packets))
		add("32 ビット CRC", map[bool]string{true: "OK", false: "誤り"}[e.CRCOK])
		if e.Kind == "message" {
			add("コマンド", CommandName(e.Command))
		}
		if e.Text != "" {
			add("文字", e.Text)
		}
		if len(e.Data) > 0 {
			h := strings.ToUpper(fmt.Sprintf("% x", e.Data))
			add("ユーザデータ", fmt.Sprintf("%d バイト %s", len(e.Data), h))
		}
	}
	return rows
}
