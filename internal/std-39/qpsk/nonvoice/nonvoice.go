package nonvoice

import (
	"fmt"
	"sort"
	"strings"

	"github.com/symysak/arib/internal/rcr39qpsk/l1"
	"github.com/symysak/arib/internal/rcr39qpsk/l2"
	"github.com/symysak/arib/internal/std-39/qpsk/l3"
)

const (
	MethodPolling    = "ポーリング"
	MethodContention = "コンテンション"
)

func AppendixOf(method string) string {
	switch method {
	case MethodPolling:
		return "付録 1"
	case MethodContention:
		return "付録 2"
	}
	return "付録"
}

const (
	KindUnit     = "ユニット"
	KindMessage  = "メッセージ"
	KindCRCError = "CRC 誤り"
)

type Frame struct {
	Method string
	Dir    int
	Slot   int
	T      float64
	Kind   string
	Format Format
	Fields []l3.Field
	Payload []byte
	User   bool
	Signal *l3.Signal
	L2     *l2.Frame
	Text   string
	Err    string
	Notes  []string
}

func (f Frame) Confirms() bool { return f.Kind == KindMessage }

func (f *Frame) add(name, value string) {
	f.Fields = append(f.Fields, l3.Field{Name: name, Value: value})
}

func (f *Frame) note(format string, a ...any) {
	f.Notes = append(f.Notes, fmt.Sprintf(format, a...))
}

func (f *Frame) finish() {
	for _, n := range f.Notes {
		f.add("規格の検査", n)
	}
}

func (f *Frame) prefix() string {
	arrow := "↓"
	if f.Dir == 2 {
		arrow = "↑"
	}
	return fmt.Sprintf("[%s%s ST#%d] ", f.Method, arrow, f.Slot)
}

type Decoder struct {
	Ctx l3.Ctx
	SearchBudget int
	LinkTimeout float64

	method   string
	lastDown string
	init     [3]int
	known    [3]bool
	cand     [3]int
	pend     [3][]pending
	sweep    [3]int
	groups   map[[2]int]*pollGroup
	links    map[[3]int]*linkBuf
	conts    map[[2]int]*contStream
	ups      []upEcho
	polls    map[int]float64
	seenT map[int]float64
}

type pending struct {
	slot int
	t    float64
	fd   Field
}

type upEcho struct {
	crc  uint16
	slot int
	t    float64
}

func New() *Decoder {
	d := &Decoder{groups: map[[2]int]*pollGroup{}, links: map[[3]int]*linkBuf{}, conts: map[[2]int]*contStream{}, polls: map[int]float64{}}
	for i := range d.init {
		d.init[i], d.cand[i] = -1, -1
	}
	return d
}

func (d *Decoder) SetMethod(m string) { d.method = m }

func (d *Decoder) Method() string {
	if d.method != "" {
		return d.method
	}
	return d.lastDown
}

func (d *Decoder) SetScramble(pattern int) { d.SetInit(l1.InitFromPattern(pattern)) }

func (d *Decoder) SetInit(init int) {
	for _, dir := range []int{1, 2} {
		d.init[dir], d.known[dir], d.cand[dir], d.pend[dir] = init&0x1FF, true, -1, nil
	}
}

func (d *Decoder) Init(dir int) int {
	if dir < 1 || dir > 2 || !d.known[dir] {
		return -1
	}
	return d.init[dir]
}

func (d *Decoder) Observe(s l3.Signal) {
	if m := MethodFromSignal(s); m != "" {
		d.method = m
	}
	for _, m := range s.Messages {
		if _, ok := MessageDef(m.Type); ok && m.Entity == "RT" && m.Assign != nil && m.Assign.Scramble >= 0 {
			d.SetScramble(m.Assign.Scramble)
		}
	}
}

func (d *Decoder) FeedHard(slot int, up bool, hard []uint8, t float64) []Frame {
	return d.Feed(slot, up, SoftFromHard(hard), t)
}

func (d *Decoder) Feed(slot int, up bool, soft []float32, t float64) []Frame {
	out := d.Flush(t)
	if len(soft) < l1.SlotBits {
		return out
	}
	dir := 1
	if up {
		dir = 2
	}
	fmts := d.formats(up)
	if !up {
		if d.seenT == nil {
			d.seenT = map[int]float64{}
		}
		if prev, ok := d.seenT[slot]; ok && t-prev > 0.06 {
			d.groupGap(slot)
		}
		d.seenT[slot] = t
	}
	if d.known[dir] {
		var first Field
		for k, f := range fmts {
			fd := DecodeField(f, soft, d.init[dir])
			if k == 0 {
				first = fd
			}
			if d.valid(fd) {
				return append(out, d.process(slot, dir, t, fd)...)
			}
		}
		if !up {
			d.groupGap(slot)
		}
		if hit, ok := d.search(dir, fmts, soft); ok && hit.Init != d.init[dir] {
			return append(out, d.candidate(slot, dir, t, hit)...)
		}
		if g := geoms[first.Format]; first.Corrected*10 < g.rows*g.cols {
			fr := Frame{Method: d.methodFor(first.Format), Dir: dir, Slot: slot, T: t, Kind: KindCRCError, Format: first.Format, Err: "CRC16 が合わない"}
			fr.add("訂正数", fmt.Sprint(first.Corrected))
			fr.Text = fr.prefix() + fmt.Sprintf("CRC 誤り（%s・訂正 %d）", first.Format, first.Corrected)
			out = append(out, fr)
		}
		return out
	}
	if hit, ok := d.search(dir, fmts, soft); ok {
		return append(out, d.candidate(slot, dir, t, hit)...)
	}
	return out
}

func (d *Decoder) candidate(slot, dir int, t float64, fd Field) []Frame {
	if len(d.pend[dir]) > 0 && t-d.pend[dir][0].t > 5 {
		d.pend[dir], d.cand[dir] = nil, -1
	}
	if d.cand[dir] != fd.Init {
		d.cand[dir] = fd.Init
		d.pend[dir] = []pending{{slot, t, fd}}
		return nil
	}
	d.init[dir], d.known[dir] = fd.Init, true
	ps := append(d.pend[dir], pending{slot, t, fd})
	d.pend[dir], d.cand[dir] = nil, -1
	var out []Frame
	for k, p := range ps {
		fr := d.process(p.slot, dir, p.t, p.fd)
		if k == 0 && len(fr) > 0 {
			fr[0].add("スクランブル", fmt.Sprintf("初期値を当て込みで決めた: 0x%03X（パターン %s。同じ初期値で 2 回 CRC16 が通った）", fd.Init, patternText(fd.Init)))
		}
		out = append(out, fr...)
	}
	return out
}

func patternText(init int) string {
	if p := l1.PatternOf(init); p >= 0 {
		return fmt.Sprintf("0x%02X", p)
	}
	return "なし"
}

func (d *Decoder) search(dir int, fmts []Format, soft []float32) (Field, bool) {
	budget := d.SearchBudget
	if budget <= 0 {
		budget = 32
	}
	if c := d.cand[dir]; c >= 0 {
		for _, f := range fmts {
			if fd := DecodeField(f, soft, c); d.valid(fd) {
				return fd, true
			}
		}
	}
	const n = 257
	for k := 0; k < budget && k < n; k++ {
		i := d.sweep[dir] % n
		d.sweep[dir]++
		init := l1.InitAllOnes
		if i > 0 {
			init = l1.InitFromPattern(i - 1)
		}
		for _, f := range fmts {
			fd := DecodeField(f, soft, init)
			if d.valid(fd) {
				return fd, true
			}
		}
	}
	return Field{}, false
}

func (d *Decoder) formats(up bool) []Format {
	if !up {
		return []Format{FmtPollDown}
	}
	switch d.method {
	case MethodPolling:
		return []Format{FmtPollUp}
	case MethodContention:
		return []Format{FmtContUpN, FmtContUp1}
	}
	return []Format{FmtPollUp, FmtContUpN, FmtContUp1}
}

func downN(b []uint8) (poll, cont bool) { return allZero(b[112:119]), allZero(b[96:101]) }

func (d *Decoder) valid(fd Field) bool {
	if !fd.OK {
		return false
	}
	switch fd.Format {
	case FmtPollDown, FmtContDown:
		p, c := downN(fd.Bits)
		switch d.method {
		case MethodPolling:
			return p
		case MethodContention:
			return c
		}
		return p || c
	case FmtPollUp:
		return allZero(fd.Bits[80:84])
	case FmtContUpN:
		return allZero(fd.Bits[88:91])
	}
	return true
}

func (d *Decoder) methodFor(f Format) string {
	switch f {
	case FmtPollUp:
		return MethodPolling
	case FmtContUp1, FmtContUpN, FmtContDown:
		return MethodContention
	}
	if m := d.Method(); m != "" {
		return m
	}
	return MethodPolling
}

func (d *Decoder) process(slot, dir int, t float64, fd Field) []Frame {
	if fd.Format == FmtPollDown || fd.Format == FmtContDown {
		p, c := downN(fd.Bits)
		m := d.method
		var why string
		if m == "" {
			switch {
			case p && !c:
				m = MethodPolling
			case c && !p:
				m = MethodContention
			default:
				m = d.lastDown
				if m == "" {
					m = MethodPolling
				}
				why = "N だけでは方式を見分けられない（付録 1 の N 7 個と付録 2 の N 5 個がどちらも 0）。" + m + "方式として読んだ"
			}
			d.lastDown = m
		}
		var out []Frame
		if m == MethodContention {
			fd.Format = FmtContDown
			out = d.contUnit(slot, dir, t, fd)
		} else {
			fd.Format = FmtPollDown
			out = d.pollDown(slot, t, fd)
		}
		if why != "" && len(out) > 0 {
			out[0].Fields = append(out[0].Fields, l3.Field{Name: "方式の見分け", Value: why})
		}
		return out
	}
	if fd.Format == FmtPollUp {
		return d.pollUp(slot, t, fd)
	}
	return d.contUnit(slot, dir, t, fd)
}


func (d *Decoder) pollDown(slot int, t float64, fd Field) []Frame {
	u := parsePollDown(fd.Bits)
	fr := Frame{Method: MethodPolling, Dir: 1, Slot: slot, T: t, Kind: KindUnit, Format: fd.Format, Payload: u.Info}
	full, _ := controlDef(u.Ctl.Name)
	fr.add("P アドレス", AddrText(u.P))
	fr.add("*1（予備）", fmt.Sprint(u.Rsv))
	fr.add("Ar", map[int]string{0: "0（制御信号要求以外）", 1: "1（制御信号要求: チャネル移行・最優先データ受信など）"}[u.Ar])
	fr.add("制御フィールド", fmt.Sprintf("CR=%d %s %s", u.Ctl.CR, u.Ctl, full))
	fr.add("受信アドレス（情報アドレスフィールド）", AddrText(u.Dest))
	fr.add("W", fmt.Sprintf("%08b（%s）", u.W.Raw, u.W))
	fr.add("情報", fmt.Sprintf("%X", u.Info))
	fr.add("N", fmt.Sprintf("%07b", u.N))
	fr.add("訂正数", fmt.Sprint(fd.Corrected))
	if u.Rsv != 0 {
		fr.note("*1（予備）が 1")
	}
	if u.P > 150 {
		fr.note("P アドレス %d は個別アドレスでない（§3.4.5.2: 設定可能なアドレスは個別アドレスのみ。§3.4.2.1.1）", u.P)
	}
	if u.Dest >= 151 && u.Dest <= 191 {
		fr.note("受信アドレス %d は予備の範囲（§3.4.2.1.1）", u.Dest)
	}
	if u.Ctl.Name == "" {
		fr.note("制御フィールドの制御ビット %05b は表に無い（§3.4.5.7）", u.Ctl.Bits)
	} else if _, cmd := controlDef(u.Ctl.Name); cmd != u.Ctl.isCommand(1) {
		fr.note("%s は%sだが CR=%d（下り: コマンド 1・レスポンス 0。§3.4.5.7）", u.Ctl.Name, map[bool]string{true: "コマンド", false: "レスポンス"}[cmd], u.Ctl.CR)
	}
	if u.P != 0 {
		d.polls[u.P] = t
	}
	var tx []string
	if u.P != 0 {
		tx = append(tx, fmt.Sprintf("P=%d", u.P))
	} else {
		tx = append(tx, "P=無し")
	}
	if u.Ar == 1 {
		tx = append(tx, "Ar=1")
	}
	tx = append(tx, u.Ctl.String(), "→ 宛先 "+AddrText(u.Dest), u.W.String())
	fr.Text = fr.prefix() + strings.Join(tx, " ")
	out := []Frame{fr}

	link := u.W.Raw == 0 && (u.Ctl.Name == "I" || u.Ctl.Name == "SABMI")
	switch {
	case link || u.Ctl.Name == "DISC":
		out = append(out, d.linkUnit(1, slot, t, u.Dest, u.Ctl, u.Info, false)...)
		if u.Ctl.Name == "DISC" && u.P != 0 {
			out = append(out, d.closeLink(2, slot, u.P, t, "基地局の DISC で中止")...)
		}
	case u.W.Raw != 0:
		out = append(out, d.groupUnit(slot, t, u)...)
	}
	out[0].finish()
	return out
}

func (d *Decoder) groupGap(slot int) {
	for k, g := range d.groups {
		if k[0] == slot && !g.done {
			g.markGap()
		}
	}
}

func (d *Decoder) groupUnit(slot int, t float64, u PollDown) []Frame {
	k := [2]int{slot, u.Dest}
	g := d.groups[k]
	switch {
	case g == nil, u.W.First && u.W.Count == 0, g.done && u.W.Count == 0:
		g = newPollGroup(t)
		d.groups[k] = g
	}
	info, ok := g.push(u.W, u.Info)
	if !ok {
		return nil
	}
	fr := d.message(MethodPolling, 1, slot, t, u.Dest, info, false, false)
	fr.add("組立", fmt.Sprintf("W による組立（§3.1.4.3.1）・%d ユニット・受けた回 %s", g.finalAt+1, countsText(g.counts)))
	fr.finish()
	return []Frame{fr}
}

func countsText(m map[int]bool) string {
	var s []string
	for i := 0; i < 4; i++ {
		if m[i] {
			s = append(s, fmt.Sprintf("%d 回目", i+1))
		}
	}
	return strings.Join(s, "・")
}

func (d *Decoder) pollUp(slot int, t float64, fd Field) []Frame {
	u := parsePollUp(fd.Bits)
	fr := Frame{Method: MethodPolling, Dir: 2, Slot: slot, T: t, Kind: KindUnit, Format: fd.Format, Payload: u.Info}
	full, _ := controlDef(u.Ctl.Name)
	fr.add("発信アドレス", AddrText(u.Src))
	fr.add("Ak", map[int]string{0: "0（上記以外 = レイヤ 3 応答の ACK）", 1: "1（チャネル移行・最優先データ受信・制御電文・ユーザ情報）"}[u.Ak])
	fr.add("MT", map[int]string{0: "0（制御信号）", 1: "1（ユーザ情報）"}[u.MT])
	fr.add("制御フィールド", fmt.Sprintf("CR=%d %s %s", u.Ctl.CR, u.Ctl, full))
	fr.add("情報", fmt.Sprintf("%X", u.Info))
	fr.add("N", fmt.Sprintf("%04b", u.N))
	fr.add("訂正数", fmt.Sprint(fd.Corrected))
	if tp, ok := d.polls[u.Src]; ok && t-tp >= 0 && t-tp < 1 {
		fr.add("ポーリング", fmt.Sprintf("%.0f ms 前に下りの P アドレスで指名された", (t-tp)*1000))
	}
	if u.Src == 0 || u.Src > 150 {
		fr.note("発信アドレス %d は個別アドレスでない（§3.4.2.1.1: 発信は個別割当だけ）", u.Src)
	}
	if u.Ctl.Name == "" {
		fr.note("制御フィールドの制御ビット %05b は表に無い（§3.4.5.7）", u.Ctl.Bits)
	} else {
		if _, cmd := controlDef(u.Ctl.Name); cmd != u.Ctl.isCommand(2) {
			fr.note("%s は%sだが CR=%d（上り: コマンド 0・レスポンス 1。§3.4.5.7）", u.Ctl.Name, map[bool]string{true: "コマンド", false: "レスポンス"}[cmd], u.Ctl.CR)
		}
		if u.Ctl.Name == "INV" {
			fr.note("INV は基地局だけが設定できる（§3.4.5.7）が上りで来た")
		}
	}
	mt := "制御"
	if u.MT == 1 {
		mt = "ユーザ"
	}
	fr.Text = fr.prefix() + fmt.Sprintf("発信 %s Ak=%d MT=%s %s", AddrText(u.Src), u.Ak, mt, u.Ctl)
	out := []Frame{fr}
	switch u.Ctl.Name {
	case "SABMI", "I", "DISC":
		out = append(out, d.linkUnit(2, slot, t, u.Src, u.Ctl, u.Info, u.MT == 1)...)
		if u.Ctl.Name == "DISC" {
			out = append(out, d.closeLink(1, slot, u.Src, t, "移動局の DISC で中止")...)
		}
	case "UI":
		if len(trimPad(u.Info)) > 0 {
			m := d.message(MethodPolling, 2, slot, t, u.Src, u.Info, u.MT == 1, true)
			m.add("組立", "UI（非確認型情報転送）の 1 ユニット")
			m.finish()
			out = append(out, m)
		}
	}
	out[0].finish()
	return out
}

func (d *Decoder) linkUnit(dir, slot int, t float64, addr int, c Control, info []byte, user bool) []Frame {
	k := [3]int{dir, slot, addr}
	lb := d.links[k]
	var out []Frame
	switch c.Name {
	case "SABMI":
		if lb != nil {
			out = append(out, d.emitLink(k, lb, "次の SABMI が来た（DISC を受けていない）")...)
		}
		d.links[k] = &linkBuf{data: append([]byte(nil), info...), lastS: -1, last: info, units: 1, user: user, tFirst: t, tLast: t}
	case "I":
		if lb == nil {
			lb = &linkBuf{lastS: -1, user: user, tFirst: t}
			lb.notes = append(lb.notes, "先頭（SABMI）を受けていない")
			d.links[k] = lb
		}
		lb.tLast = t
		if c.S == lb.lastS {
			if string(info) == string(lb.last) {
				return out
			}
			lb.notes = append(lb.notes, "同じ S で中身の違う I（間のユニットの取りこぼしかもしれない）")
		}
		lb.data = append(lb.data, info...)
		lb.last, lb.lastS = info, c.S
		lb.units++
	case "DISC":
		if lb != nil {
			out = append(out, d.emitLink(k, lb, "")...)
		}
	}
	return out
}

func (d *Decoder) closeLink(dir, slot, addr int, t float64, why string) []Frame {
	k := [3]int{dir, slot, addr}
	if lb := d.links[k]; lb != nil {
		return d.emitLink(k, lb, why)
	}
	return nil
}

func (d *Decoder) emitLink(k [3]int, lb *linkBuf, why string) []Frame {
	delete(d.links, k)
	if len(lb.data) == 0 {
		return nil
	}
	fr := d.message(MethodPolling, k[0], k[1], lb.tLast, k[2], lb.data, lb.user, true)
	fr.add("組立", fmt.Sprintf("再送制御手順（SABMI〜DISC。§3.1.4.3.2）・%d ユニット", lb.units))
	for _, n := range lb.notes {
		fr.note("組立: %s", n)
	}
	if why != "" {
		fr.note("組立: %s", why)
	}
	fr.finish()
	return []Frame{fr}
}

func (d *Decoder) message(method string, dir, slot int, t float64, addr int, info []byte, upUser, noLen bool) Frame {
	fr := Frame{Method: method, Dir: dir, Slot: slot, T: t, Kind: KindMessage, Payload: info}
	if dir == 1 {
		fr.add("受信アドレス", AddrText(addr))
	} else {
		fr.add("発信アドレス", AddrText(addr))
	}
	body := info
	user := upUser
	if dir == 1 && method == MethodPolling {
		if len(info) == 0 {
			fr.Err = "情報フィールドが空"
			fr.Text = fr.prefix() + "空の情報フィールド"
			return fr
		}
		di := parseDataInfo(info[0])
		fs, ns := di.fields()
		fr.Fields = append(fr.Fields, fs...)
		fr.Notes = append(fr.Notes, ns...)
		user = di.User
		body = info[1:]
	}
	fr.User = user
	who := "宛先 " + AddrText(addr)
	if dir == 2 {
		who = "発信 " + AddrText(addr)
	}
	if user {
		fr.add("ユーザ情報", fmt.Sprintf("%d オクテット %X", len(body), body))
		t := fmt.Sprintf("%s ユーザ情報 %d オクテット: %s", who, len(body), userText(body))
		if dir == 1 && method == MethodPolling {
			di := parseDataInfo(info[0])
			ack := ""
			if di.Ack {
				ack = "・ACK 要"
			}
			t = fmt.Sprintf("%s ユーザ情報 No.%d%s %d オクテット: %s", who, di.No, ack, len(body), userText(body))
		}
		fr.Text = fr.prefix() + t
		return fr
	}
	c := d.Ctx
	c.Dir = dir
	sig := ParseSignal(body, c)
	if noLen {
		best, cut := signalScore(sig), 0
		for k := 1; k <= len(body)-len(trimPad(body)) && k < len(body); k++ {
			if s2 := ParseSignal(body[:len(body)-k], c); signalScore(s2) < best {
				best, cut, sig = signalScore(s2), k, s2
			}
		}
		if cut > 0 {
			fr.add("詰め物", fmt.Sprintf("末尾の 0 を %d オクテット落とした（長さの欄が無い組立）", cut))
		}
	}
	fr.Signal = &sig
	fr.add("制御信号", summary(sig))
	appendSignal(&fr, sig)
	if sig.Err != "" {
		fr.Err = sig.Err
	}
	fr.Text = fr.prefix() + who + " 制御信号 " + summary(sig)
	return fr
}

func signalScore(s l3.Signal) int {
	n := 0
	if s.Err != "" {
		n += 100
	}
	for _, m := range s.Messages {
		if m.Err != "" {
			n += 100
		}
		n += len(m.Notes)
	}
	return n
}

func appendSignal(fr *Frame, sig l3.Signal) {
	for _, m := range sig.Messages {
		fr.add(m.Entity, m.Name)
		fr.Fields = append(fr.Fields, m.Fields...)
		if m.Err != "" {
			fr.add(m.Entity+" の誤り", m.Err)
		}
	}
}


func (d *Decoder) contUnit(slot, dir int, t float64, fd Field) []Frame {
	u := parseContUnit(fd.Format, fd.Bits)
	fr := Frame{Method: MethodContention, Dir: dir, Slot: slot, T: t, Kind: KindUnit, Format: fd.Format}
	fr.Payload = append(append([]byte(nil), u.Ctl...), u.User...)
	fr.add("形式", fd.Format.String())
	fr.add("W", fmt.Sprintf("%016b（%s）", u.W.Raw, u.W))
	if fd.Format != FmtContUp1 {
		fr.add("UACCH2 の領域", fmt.Sprintf("%X", u.Ctl))
	}
	fr.add("UDCH2 の領域", fmt.Sprintf("%X", u.User))
	if fd.Format != FmtContUp1 {
		fr.add("N", fmt.Sprintf("%b", u.N))
	}
	fr.add("訂正数", fmt.Sprint(fd.Corrected))
	ctlCap, usrCap := len(u.Ctl), len(u.userArea())
	if u.W.F12 && fd.Format != FmtContUp1 && u.W.W0 > ctlCap {
		fr.note("制御信号の有効オクテット数 L=%d が領域（%d オクテット）を越える", u.W.W0, ctlCap)
	}
	if u.W.W1 > usrCap {
		fr.note("ユーザ情報の有効オクテット数 L=%d が領域（%d オクテット）を越える", u.W.W1, usrCap)
	}
	var tx []string
	tx = append(tx, u.W.String())
	if dir == 1 {
		ib := map[int]string{0: "B（送信禁止）", 1: "I（送信許可）"}[u.IB]
		rn := map[int]string{0: "N（非受信）", 1: "R（受信）"}[u.RN]
		fr.add("衝突制御ビット I/B", ib)
		fr.add("衝突制御ビット R/N", rn)
		fr.add("部分エコー PE", fmt.Sprintf("%04X", u.PE))
		e := fmt.Sprintf("E: %s %s PE=%04X", ib[:1], rn[:1], u.PE)
		if u.RN == 1 {
			if ue, ok := d.matchEcho(u.PE, t); ok {
				fr.add("部分エコーの照合", fmt.Sprintf("上り ST#%d（%.0f ms 前）の CRC と一致 = 基地局が受信（§3.1.6.1 (2) ウ）", ue.slot, (t-ue.t)*1000))
				e += "（上りを受信確認）"
			} else {
				fr.add("部分エコーの照合", "対応する上りのバーストを受けていない")
			}
		} else if u.PE != 0 {
			fr.note("R/N = N なのに PE が 0 でない")
		}
		if u.IB == 0 && u.RN == 0 && u.PE == 0 {
			fr.add("E の注", "B・N・PE 0 は複信時の固定値（§3.1.6.3 (2)）でもある")
		}
		tx = append(tx, e)
	} else {
		d.ups = append(d.ups, upEcho{fd.CRC, slot, t})
		fr.add("CRC16", fmt.Sprintf("%04X（下りの部分エコー PE と突き合わせる）", fd.CRC))
	}
	fr.Text = fr.prefix() + strings.Join(tx, " ")
	out := []Frame{fr}
	out = append(out, d.contAssemble(slot, dir, t, u, &out[0])...)
	out[0].finish()
	return out
}

func (d *Decoder) matchEcho(pe uint16, t float64) (upEcho, bool) {
	for i := len(d.ups) - 1; i >= 0; i-- {
		u := d.ups[i]
		if t-u.t > 1 {
			break
		}
		if u.crc == pe && t >= u.t {
			return u, true
		}
	}
	return upEcho{}, false
}

func (d *Decoder) contAssemble(slot, dir int, t float64, u ContUnit, unit *Frame) []Frame {
	k := [2]int{dir, slot}
	st := d.conts[k]
	if st == nil {
		st = &contStream{ctlWr: -1}
		d.conts[k] = st
	}
	st.units++
	var out []Frame
	w := u.W
	first := u.Format == FmtContUp1

	if w.F11 {
		if st.ctlOpen && len(st.ctl) > 0 {
			out = append(out, d.contControl(slot, dir, st.ctlT, st.ctl, "最終ユニットを受けないまま次の先頭が来た"))
		}
		st.ctl, st.ctlOpen, st.ctlT, st.ctlWr = nil, true, t, -1
	}
	if w.SF && st.ctlOpen && !w.F11 && !first {
		unit.note("制御信号の伝送中にスチール（§3.1.5.1 (2): レイヤ 2 情報の 1 メッセージの途中からスチールしてはならない）")
	}
	if first && (w.F11 || w.F12) {
		unit.add("制御信号の注", "上り第 1 ユニットには制御信号の領域が無い（制御信号は第 2 ユニットから。図 3.1.5.2 (6) 注）")
	}
	if st.ctlOpen && !first {
		area := u.Ctl
		if w.SF {
			area = nil
		}
		if w.F12 {
			st.ctl = append(st.ctl, area[:min(w.W0, len(area))]...)
			out = append(out, d.contControl(slot, dir, st.ctlT, st.ctl, ""))
			st.ctl, st.ctlOpen, st.ctlWr = nil, false, -1
		} else {
			if st.ctlWr >= 0 && w.W0 != st.ctlWr-1 {
				unit.note("制御信号の残りユニット数 Wr が続かない（%d の次が %d）", st.ctlWr, w.W0)
			}
			st.ctlWr = w.W0
			st.ctl = append(st.ctl, area...)
		}
	} else if !st.ctlOpen && !w.F11 && (w.F12 || w.W0 != 0) && !first {
		unit.add("制御信号の注", "先頭ユニットを受けていない制御信号のユニット（組み立てない）")
	}

	area := u.userArea()
	l := min(w.W1, len(area))
	if w.F21 {
		if st.usrOpen && len(st.user) > 0 {
			out = append(out, d.contUser(slot, dir, st.usrT, st.user, "最終ユニットを受けないまま次の先頭が来た"))
		}
		st.user, st.usrOpen, st.usrT = nil, true, t
	}
	if st.usrOpen {
		st.user = append(st.user, area[:l]...)
		if w.F22 {
			out = append(out, d.contUser(slot, dir, st.usrT, st.user, ""))
			st.user, st.usrOpen = nil, false
		}
	} else if l > 0 {
		unit.add("ユーザ情報の注", "先頭ユニットを受けていないユーザ情報のユニット（組み立てない）")
	}
	return out
}

func (d *Decoder) contControl(slot, dir int, t float64, b []byte, why string) Frame {
	fr := Frame{Method: MethodContention, Dir: dir, Slot: slot, T: t, Kind: KindMessage, Payload: append([]byte(nil), b...)}
	ld := l2.DirDown
	if dir == 2 {
		ld = l2.DirUp
	}
	f := l2.ParseDir(b, ld)
	fr.L2 = &f
	fr.add("レイヤ 2", f.Summary())
	for _, x := range f.Fields() {
		fr.add(x.Name, x.Value)
	}
	text := "制御信号 L2 " + f.Summary()
	if f.Err != "" {
		fr.Err = f.Err
	}
	if f.CarriesInfo() && len(f.Info) > 0 {
		c := d.Ctx
		c.Dir = dir
		sig := ParseSignal(f.Info, c)
		fr.Signal = &sig
		fr.add("制御信号", summary(sig))
		appendSignal(&fr, sig)
		text += " " + summary(sig)
		if sig.Err != "" && fr.Err == "" {
			fr.Err = sig.Err
		}
	}
	if why != "" {
		fr.note("組立: %s", why)
	}
	fr.add("組立", "W（F11〜F12）による制御信号の組立（§3.1.5）")
	fr.Text = fr.prefix() + text
	fr.finish()
	return fr
}

func (d *Decoder) contUser(slot, dir int, t float64, b []byte, why string) Frame {
	fr := Frame{Method: MethodContention, Dir: dir, Slot: slot, T: t, Kind: KindMessage, User: true, Payload: append([]byte(nil), b...)}
	fr.add("ユーザ情報", fmt.Sprintf("%d オクテット %X", len(b), b))
	fr.add("組立", "W（F21〜F22・W1）によるユーザ情報の組立（§3.1.5）")
	if why != "" {
		fr.note("組立: %s", why)
	}
	fr.Text = fr.prefix() + fmt.Sprintf("ユーザ情報 %d オクテット: %s", len(b), userText(b))
	fr.finish()
	return fr
}

func (d *Decoder) Flush(t float64) []Frame {
	to := d.LinkTimeout
	if to <= 0 {
		to = 3
	}
	var out []Frame
	lk := make([][3]int, 0, len(d.links))
	for k := range d.links {
		lk = append(lk, k)
	}
	sort.Slice(lk, func(i, j int) bool { return fmt.Sprint(lk[i]) < fmt.Sprint(lk[j]) })
	for _, k := range lk {
		if lb := d.links[k]; t-lb.tLast > to {
			out = append(out, d.emitLink(k, lb, fmt.Sprintf("解除（DISC）を %.0f 秒待たずに出した", to))...)
		}
	}
	ck := make([][2]int, 0, len(d.conts))
	for k := range d.conts {
		ck = append(ck, k)
	}
	sort.Slice(ck, func(i, j int) bool { return fmt.Sprint(ck[i]) < fmt.Sprint(ck[j]) })
	for _, k := range ck {
		st := d.conts[k]
		if st.ctlOpen && t-st.ctlT > to {
			if len(st.ctl) > 0 {
				out = append(out, d.contControl(k[1], k[0], st.ctlT, st.ctl, "最終ユニットを待たずに出した"))
			}
			st.ctl, st.ctlOpen = nil, false
		}
		if st.usrOpen && t-st.usrT > to {
			if len(st.user) > 0 {
				out = append(out, d.contUser(k[1], k[0], st.usrT, st.user, "最終ユニットを待たずに出した"))
			}
			st.user, st.usrOpen = nil, false
		}
	}
	for k, g := range d.groups {
		if t-g.tFirst > 10*to {
			delete(d.groups, k)
		}
	}
	for len(d.ups) > 0 && t-d.ups[0].t > 2 {
		d.ups = d.ups[1:]
	}
	return out
}
