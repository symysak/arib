package server


import (
	"fmt"
	"strings"

	"github.com/symysak/arib/internal/nxdn/decoder"
	nxl1 "github.com/symysak/arib/internal/nxdn/l1"
	"github.com/symysak/arib/internal/nxdn/scramble"
	nx "github.com/symysak/arib/internal/nxdn/server"
	"github.com/symysak/arib/internal/std-t98/fsk4/l1"
	"github.com/symysak/arib/internal/std-t98/fsk4/l3"
	"github.com/symysak/arib/internal/vocoder/ambe"
)

func (k *link) FrameKey(p nxl1.Parsed) (key, info string) {
	var sb strings.Builder
	r := l1.ReadRICH(p.RICH)
	fmt.Fprintf(&sb, "%02X", p.RICH)
	info = r.String()
	if s := p.SACCH; s != nil {
		fmt.Fprintf(&sb, "/S%02X %v %v", s.Head, s.CRCOK, s.Data)
		switch u := l1.ReadUnit(s.Head); {
		case !s.CRCOK:
			info += "・SACCH CRC 誤り"
		case u.First:
			call, uc, maker := l3.First(s.Data[:])
			info += fmt.Sprintf("・SACCH %s UC %d 製造者 %d", l3.TypeName(u.Type), uc, maker)
			if call != l3.CallNormal {
				info += " " + l3.CallName(call)
			}
		default:
			info += fmt.Sprintf("・SACCH %s（続きのユニット・残り %d）", l3.TypeName(u.Type), u.Remain)
		}
	}
	mode := r.Mode()
	for i, h := range p.Half {
		if h.Coded == nil {
			continue
		}
		b := nxl1.CodedBytes(h.Coded)
		fmt.Fprintf(&sb, "/C%d %v %X", i, h.Coded.CRCOK, b)
		name := mode.HalfName(i)
		switch {
		case !h.Coded.CRCOK:
			info += "・" + name + " CRC 誤り"
		case mode == l1.ModeSync:
			info += "・" + l3.ParsePICH(b).Summary()
		default:
			info += fmt.Sprintf("・%s 非音声 2 %X…", name, b[:4])
		}
	}
	return sb.String(), info
}

func (k *link) Tree(f decoder.Frame, kd nxl1.Keyed) []Node {
	p := kd.Parsed
	r := l1.ReadRICH(p.RICH)
	mode := r.Mode()
	wh := Node{Label: "ホワイトニング", Value: fmt.Sprintf("PN(9,5) 初期値 0x%03X（規格の値）", kd.Init), Range: "20〜383",
		Note: "§4.1.10。RICH から後ろの記号に ±1 を掛ける。初期値 S8…S0 = 011100100 は規格が決めた値で、局ごとに変わらない"}
	fText := "同期バースト"
	if r.F == 1 {
		fText = "通信用チャネル"
	}
	mText := "予約"
	if m := r.Mode(); m != l1.ModeReserved {
		mText = m.String()
	} else if r.ParityOK() && r.Rsv == 0 && r.D == 0 {
		mText = "予約（表 4-5・表 4-6 に無い値）"
	}
	rich := Node{Label: "RICH", Value: r.String(), Bits: fmt.Sprintf("%08b", r.Raw), Range: "20〜35", Children: []Node{
		{Label: "F 無線チャネル構造識別", Bits: fmt.Sprintf("%d", r.F), Value: fText},
		{Label: "予約", Bits: fmt.Sprintf("%02b", r.Rsv), Value: map[bool]string{true: "標準値", false: "標準値（0）でない"}[r.Rsv == 0]},
		{Label: "M 通信モード識別", Bits: fmt.Sprintf("%03b", r.M), Value: mText},
		{Label: "D 動作モード識別", Bits: fmt.Sprintf("%d", r.D), Value: map[uint8]string{0: "直接通信", 1: "予約"}[r.D]},
		{Label: "P 偶数パリティ（先頭 7 ビット）", Bits: fmt.Sprintf("%d", r.P), Value: map[bool]string{true: "一致", false: "不一致"}[r.ParityOK()]},
	}}
	out := []Node{nx.SyncNode(f), wh, rich}
	if s := p.SACCH; s != nil {
		u := l1.ReadUnit(s.Head)
		n := Node{Label: "SACCH", Range: "36〜95", Value: nx.OKText(s.CRCOK, s.Fixed), Children: []Node{
			{Label: "M ユニット情報", Bits: fmt.Sprintf("%08b", s.Head), Children: []Node{
				{Label: "F 先頭ユニット", Bits: fmt.Sprintf("%d", s.Head>>7), Value: map[bool]string{true: "先頭ユニット", false: "続きのユニット"}[u.First]},
				{Label: "Wr 残りユニット数", Bits: fmt.Sprintf("%02b", u.Remain), Value: fmt.Sprintf("%d", u.Remain)},
				{Label: "メッセージ種別", Bits: fmt.Sprintf("%05b", u.Type), Value: l3.TypeName(u.Type)},
			}},
		}}
		if u.First {
			call, uc, maker := l3.First(s.Data[:])
			n.Children = append(n.Children,
				Node{Label: "通話識別", Bits: nx.BitStr(s.Data[0:2]), Value: l3.CallName(call)},
				Node{Label: "ユーザコード UC", Bits: nx.BitStr(s.Data[2:11]), Value: fmt.Sprintf("%d", uc)},
				Node{Label: "製造者番号", Bits: nx.BitStr(s.Data[11:18]), Value: fmt.Sprintf("%d", maker)})
		} else {
			n.Children = append(n.Children, Node{Label: "製造者定義 18 ビット", Bits: nx.BitStr(s.Data[:])})
		}
		n.Children = append(n.Children, Node{Label: "CRC6", Value: nx.OKText(s.CRCOK, s.Fixed)})
		out = append(out, n)
	}
	key, how := 0, ""
	if k.tx.uc >= 0 && k.tx.call == l3.CallCipher {
		key, how = k.keyFor(k.tx.uc)
	}
	var ks []uint8
	if key > 0 {
		ks = scramble.Keystream(uint16(key), 4*49)
	}
	for i, h := range p.Half {
		at := [2]string{"96〜239", "240〜383"}[i]
		name := mode.HalfName(i)
		switch {
		case h.Coded != nil && mode == l1.ModeSync:
			b := nxl1.CodedBytes(h.Coded)
			n := Node{Label: name, Range: at, Value: nx.OKText(h.Coded.CRCOK, h.Coded.Corrected),
				Note: "§4.1.7.4。情報 80 ビット = 呼出名称 36 + 予約 44。符号は TCH の非音声 2 と同じ（CRC12・K=5・11/01・9 × 16）"}
			if h.Coded.CRCOK {
				for _, row := range l3.ParsePICH(b).Rows() {
					n.Children = append(n.Children, Node{Label: row.Name, Value: row.Text, Bits: row.Bits})
				}
			}
			n.Children = append(n.Children, Node{Label: "情報 80 ビット", Value: fmt.Sprintf("% X", b)},
				Node{Label: "CRC12", Value: nx.OKText(h.Coded.CRCOK, h.Coded.Corrected)})
			out = append(out, n)
		case h.Coded != nil:
			b := nxl1.CodedBytes(h.Coded)
			out = append(out, Node{Label: name + "（非音声 2）", Range: at, Value: nx.OKText(h.Coded.CRCOK, h.Coded.Corrected),
				Note: "§4.1.8.3.3。誤り訂正あり。中身の書式は規格に無い",
				Children: []Node{{Label: "情報 80 ビット", Value: fmt.Sprintf("% X", b)}, {Label: "文字として", Value: printable(b[:])},
					{Label: "CRC12", Value: nx.OKText(h.Coded.CRCOK, h.Coded.Corrected)}}})
		case h.Kind == nxl1.HalfTCH && len(h.TCH) == nxl1.TCHBits:
			n := Node{Label: name + "（音声）", Range: at, Value: "AMBE+2 3600x2450 × 2（第 5 章の推奨）"}
			if key > 0 {
				n.Value += fmt.Sprintf("・秘話鍵 %d（%s）で外した 49 ビットを添える", key, how)
			} else if k.tx.call == l3.CallCipher {
				n.Value += "・秘話通信（秘話鍵は分からない）"
			}
			for j := 0; j < 2; j++ {
				var a [ambe.Bits3600]uint8
				copy(a[:], h.TCH[72*j:72*j+72])
				c := Node{Label: fmt.Sprintf("20 ms #%d", j+1), Value: fmt.Sprintf("%X", nx.PackBits(a[:])),
					Note: map[bool]string{true: "C0・C1 が通る", false: "C0・C1 が通らない"}[ambe.Valid3600(a)]}
				d := ambe.Params49(a)
				c.Children = append(c.Children, Node{Label: "誤り訂正の後の 49 ビット", Bits: nx.BitStr(d[:])})
				if ks != nil {
					pos := 2*i + j
					for x := range d {
						d[x] ^= ks[49*pos+x]
					}
					c.Children = append(c.Children, Node{Label: "秘話スクランブルを外した 49 ビット", Bits: nx.BitStr(d[:]),
						Note: fmt.Sprintf("系列の S%d〜S%d", 49*pos, 49*pos+48)})
				}
				n.Children = append(n.Children, c)
			}
			out = append(out, n)
		case len(h.TCH) == nxl1.TCHBits:
			what := "非音声 1・誤り訂正なし。信号分解組立は規定しない"
			switch mode {
			case l1.ModeSync:
				what = "同期バーストの未定義の欄"
			case l1.ModeIdle:
				what = "終話のフレーム（中身は規定しない）"
			}
			out = append(out, Node{Label: name, Range: at, Value: what,
				Children: []Node{{Label: "144 ビット", Value: fmt.Sprintf("%X", nx.PackBits(h.TCH))}}})
		}
	}
	return out
}
