package l3

import (
	"encoding/binary"
	"fmt"
	"strings"
)


const (
	TypeSelVoice     uint8 = 0xE1
	TypeGPSHeader    uint8 = 0xE2
	TypeGPSData      uint8 = 0xE3
	TypeBearerHeader uint8 = 0xE4
	TypeBearerData   uint8 = 0xE5
	TypeCallerName   uint8 = 0xE7
	TypeSelEnd       uint8 = 0xE8
	TypeMsgHeader    uint8 = 0x09
	TypeMsgData      uint8 = 0x0B
	TypeMsgResp      uint8 = 0x0C
	TypeDummy        uint8 = 0x0F
	TypeStatusReq    uint8 = 0x32
	TypeStatusResp   uint8 = 0x33
)

var b54Names = map[uint8]string{
	TypeSelVoice:     "選択呼出音声通信",
	TypeGPSHeader:    "GPS ヘッダ",
	TypeGPSData:      "GPS データ",
	TypeBearerHeader: "ベアラヘッダ",
	TypeBearerData:   "ベアラデータ",
	TypeCallerName:   "発信者名情報",
	TypeSelEnd:       "選択呼出終話",
	TypeMsgHeader:    "メッセージヘッダ",
	TypeMsgData:      "メッセージデータ",
	TypeMsgResp:      "メッセージ応答",
	TypeDummy:        "ダミーヘッダ",
	TypeStatusReq:    "ステータス要求",
	TypeStatusResp:   "ステータス応答",
}

func IsB54Type(t uint8) bool { _, ok := b54Names[t]; return ok }

func b54HasMaker(t uint8) bool { return t>>6 == 3 }

const (
	FormGroup      = 1
	FormUnspecific = 2
	FormIndividual = 4
)

const (
	NumberNone     = 0
	GroupAll       = 0xFFFF
	numberReserved = 0xFFF0
)

type B54 struct {
	Type uint8 `json:"type"`
	Emergency bool `json:"emergency,omitempty"`
	Form int `json:"form"`
	Info int `json:"info"`
	Caller int `json:"caller"`
	Dest   int `json:"dest"`
	Enc   int `json:"enc"`
	KeyNo int `json:"key_no"`

	Blocks   int  `json:"blocks,omitempty"`
	Null     int  `json:"null,omitempty"`
	First    bool `json:"first,omitempty"`
	Confirm  bool `json:"confirm,omitempty"`
	Select   bool `json:"select,omitempty"`
	Fragment int  `json:"fragment"`
	FrameNo int `json:"frame_no"`
	BlockNo int `json:"block_no"`
	Payload []byte `json:"payload,omitempty"`
	Seg  int `json:"seg,omitempty"`
	Segs int `json:"segs,omitempty"`
	Dummy int `json:"dummy"`
	Status int `json:"status"`
	Class    int    `json:"class"`
	RespType int    `json:"resp_type"`
	RecvFrag int    `json:"recv_frag"`
	ErrFlags uint16 `json:"err_flags,omitempty"`
	Reason int `json:"reason"`
}

func newB54(t uint8) *B54 {
	return &B54{Type: t, Form: -1, Info: -1, Caller: -1, Dest: -1, Enc: -1, KeyNo: -1, Fragment: -1, FrameNo: -1, BlockNo: -1,
		Dummy: -1, Status: -1, Class: -1, RespType: -1, RecvFrag: -1, Reason: -1}
}

var b54Len = map[uint8]int{
	TypeSelVoice: 9, TypeSelEnd: 8, TypeGPSHeader: 9, TypeGPSData: 10, TypeBearerHeader: 12, TypeBearerData: 22,
	TypeCallerName: 9, TypeMsgHeader: 11, TypeMsgData: 22, TypeMsgResp: 11, TypeDummy: 9, TypeStatusReq: 9, TypeStatusResp: 8,
}

func parseB54(b []byte) (*B54, string) {
	t := b[0]
	need := b54Len[t]
	if len(b) < need {
		return nil, fmt.Sprintf("%d オクテットしかない（%d 要る）", len(b), need)
	}
	for i := need; i < len(b); i++ {
		if b[i] != 0 {
			return nil, fmt.Sprintf("使わないオクテット %d が 0 でない", i+1)
		}
	}
	o := 1
	if b54HasMaker(t) {
		o = 2
	}
	x := newB54(t)
	head := func(enc bool) string {
		if b[o]&0x7F != 0 {
			return "呼制御付加情報の予約が 0 でない"
		}
		x.Emergency = b[o]&0x80 != 0
		x.Form, x.Info = int(b[o+1]>>5), int(b[o+1]&0x1F)
		switch x.Form {
		case FormGroup, FormUnspecific, FormIndividual:
		default:
			return fmt.Sprintf("通話形態 %03b は予約", x.Form)
		}
		x.Caller = int(binary.BigEndian.Uint16(b[o+2:]))
		x.Dest = int(binary.BigEndian.Uint16(b[o+4:]))
		if enc {
			x.Enc, x.KeyNo = int(b[o+6]>>6), int(b[o+6]&0x3F)
			if x.Enc >= 2 {
				return fmt.Sprintf("暗号化モード %02b は予約", x.Enc)
			}
			if x.Enc == 0 && x.KeyNo != 0 {
				return "非暗号化なのに鍵番号が 0 でない"
			}
		}
		return ""
	}
	var bad string
	switch t {
	case TypeSelVoice, TypeGPSHeader, TypeBearerHeader:
		bad = head(true)
		if bad == "" && x.Info&0x0F != 0 && t != TypeSelVoice {
			bad = "データ通信付加情報の予約が 0 でない"
		}
		if bad == "" && t == TypeSelVoice && x.Info&0x08 != 0 {
			bad = "音声通信付加情報の予約が 0 でない"
		}
		if bad == "" && t == TypeBearerHeader {
			p := b[o+7:]
			if p[0]&0xF0 != 0 || p[1]&0x40 != 0 {
				bad = "パケット情報の予約が 0 でない"
			}
			x.Blocks = int(p[0]&0x0F) + 1
			x.First = p[1]&0x80 != 0
			x.Null = int(p[1] >> 1 & 0x1F)
			x.Fragment = int(p[1]&1)<<8 | int(p[2])
			if x.Null > 18 {
				bad = "Null オクテット数が 18 を超える"
			}
		}
	case TypeSelEnd:
		bad = head(false)
		if bad == "" && x.Info != 0 {
			bad = "予約が 0 でない"
		}
	case TypeDummy:
		bad = head(false)
		if bad == "" && x.Info != 0 {
			bad = "予約が 0 でない"
		}
		x.Dummy = int(binary.BigEndian.Uint16(b[o+6:]))
		if bad == "" && x.Dummy > 0x3FF {
			bad = "ダミーカウントが予約の値"
		}
	case TypeMsgHeader:
		bad = head(true)
		if bad == "" && x.Info&0x0F != 0 {
			bad = "メッセージ付加情報の予約が 0 でない"
		}
		p := b[o+7:]
		x.Confirm, x.Select = p[0]&0x80 != 0, p[0]&0x20 != 0
		x.Blocks = int(p[0]&0x0F) + 1
		x.Null = int(p[1] >> 3)
		x.First = p[1]&0x04 != 0
		x.Fragment = int(p[2])
		if bad == "" && (p[0]&0x50 != 0 || p[1]&0x03 != 0) {
			bad = "メッセージパケット情報の予約が 0 でない"
		}
		if bad == "" && x.Null > 19 {
			bad = "Null オクテット数が 19 を超える"
		}
	case TypeMsgResp:
		bad = head(false)
		if bad == "" && x.Info&0x0F != 0 {
			bad = "メッセージ付加情報の予約が 0 でない"
		}
		r := b[o+6:]
		x.Class, x.RespType = int(r[0]>>4&3), int(r[0]>>1&7)
		x.RecvFrag = int(r[1])
		x.ErrFlags = binary.BigEndian.Uint16(r[2:])
		if bad == "" && r[0]&0xC1 != 0 {
			bad = "応答情報の予約が 0 でない"
		}
	case TypeStatusReq:
		bad = head(false)
		if bad == "" && x.Info&0x17 != 0 {
			bad = "ステータス付加情報の予約が 0 でない"
		}
		x.Confirm = x.Info&0x08 != 0
		x.Status = int(b[o+7])
		if bad == "" && b[o+6] != 0 {
			bad = "予約が 0 でない"
		}
	case TypeStatusResp:
		bad = head(false)
		if bad == "" && x.Info != 0 {
			bad = "予約が 0 でない"
		}
		x.Reason = int(b[o+6] & 0x7F)
		if bad == "" && b[o+6]&0x80 != 0 {
			bad = "理由表示の予約が 0 でない"
		}
	case TypeGPSData:
		x.Payload = append([]byte(nil), b[o:o+8]...)
	case TypeBearerData, TypeMsgData:
		x.FrameNo, x.BlockNo = int(b[o]>>4), int(b[o]&0x0F)
		x.Payload = append([]byte(nil), b[o+1:]...)
	case TypeCallerName:
		x.Seg, x.Segs = int(b[o]>>4), int(b[o]&0x0F)
		if x.Seg < 1 || x.Seg > 3 || x.Segs != 3 {
			bad = fmt.Sprintf("区画 %d/%d（1/3〜3/3 のはず）", x.Seg, x.Segs)
		}
		x.Payload = append([]byte(nil), b[o+1:o+7]...)
	}
	if bad != "" {
		return nil, bad
	}
	return x, ""
}

func FormName(f int) string {
	switch f {
	case FormGroup:
		return "グループ呼出"
	case FormUnspecific:
		return "未指定"
	case FormIndividual:
		return "個別呼出"
	}
	return fmt.Sprintf("予約 %03b", f)
}

func NumberText(n int) string {
	switch {
	case n == NumberNone:
		return "0（個別番号無指定）"
	case n >= numberReserved:
		return fmt.Sprintf("%d（予約）", n)
	}
	return fmt.Sprintf("%d", n)
}

func GroupText(n int) string {
	switch {
	case n == NumberNone:
		return "0（グループ番号無指定 = ユーザコード通信と同等）"
	case n == GroupAll:
		return "65535（グループ番号全指定 = 一斉呼出）"
	case n >= numberReserved:
		return fmt.Sprintf("%d（予約）", n)
	}
	return fmt.Sprintf("グループ %d", n)
}

func (x *B54) DestText() string {
	if x.Form == FormGroup {
		return GroupText(x.Dest)
	}
	return NumberText(x.Dest)
}

func (x *B54) CallText() string {
	if x == nil || x.Caller < 0 {
		return ""
	}
	dest := fmt.Sprintf("%d", x.Dest)
	switch {
	case x.Form == FormGroup && x.Dest == GroupAll:
		dest = "一斉"
	case x.Form == FormGroup && x.Dest == NumberNone:
		dest = "ユーザコード"
	case x.Form == FormGroup:
		dest = fmt.Sprintf("G%d", x.Dest)
	}
	s := fmt.Sprintf("%s %d → %s", FormName(x.Form), x.Caller, dest)
	if x.Emergency {
		s = "緊急 " + s
	}
	if x.Enc == 1 {
		s += fmt.Sprintf(" 暗号（鍵番号 %d）", x.KeyNo)
	}
	return s
}

type GPSFix struct {
	Lat   float64 `json:"lat"`
	Lon   float64 `json:"lon"`
	Alt   int     `json:"alt"`
	Valid bool    `json:"valid"`
	LatRaw uint32 `json:"lat_raw"`
	LonRaw uint32 `json:"lon_raw"`
	Sane bool `json:"sane"`
}

func ParseGPS(p []byte) GPSFix {
	v := binary.BigEndian.Uint64(p[:8])
	g := GPSFix{
		LatRaw: uint32(v >> 41),
		LonRaw: uint32(v >> 17 & 0xFFFFFF),
		Alt:    int(v >> 4 & 0x1FFF),
		Valid:  v>>3&1 == 0,
	}
	latDeg, latMin := g.LatRaw/100000, float64(g.LatRaw%100000)/1000
	lonDeg, lonMin := g.LonRaw/100000, float64(g.LonRaw%100000)/1000
	g.Lat = float64(latDeg) + latMin/60
	g.Lon = float64(lonDeg) + lonMin/60
	g.Sane = latMin < 60 && lonMin < 60 && latDeg <= 83 && lonDeg <= 167 && v&7 == 0
	return g
}

func (g GPSFix) Text() string {
	s := fmt.Sprintf("N%d°%06.3f′ E%d°%06.3f′ 高度 %d m", g.LatRaw/100000, float64(g.LatRaw%100000)/1000,
		g.LonRaw/100000, float64(g.LonRaw%100000)/1000, g.Alt)
	if !g.Valid {
		s += "（V = 無効）"
	}
	return s
}

func b54Rows(x *B54) []Row {
	var rows []Row
	add := func(name, text string) { rows = append(rows, Row{Name: name, Text: text}) }
	if x.Form >= 0 {
		em := "通常通信"
		if x.Emergency {
			em = "緊急通信"
		}
		add("呼制御付加情報", em)
		add("通話形態", FormName(x.Form))
		switch x.Type {
		case TypeSelVoice:
			sd := "単信及び半複信"
			if x.Info&0x10 != 0 {
				sd = "予約（複信）"
			}
			tm := "AMBE+2 EHR"
			if x.Info&7 != 0 {
				tm = fmt.Sprintf("予約 %03b", x.Info&7)
			}
			add("音声通信付加情報", sd+"・伝送モード "+tm)
		case TypeGPSHeader, TypeBearerHeader, TypeMsgHeader, TypeMsgResp:
			if x.Info&0x10 != 0 {
				add("付加情報", "単 / 複 = 予約（複信）")
			} else {
				add("付加情報", "単信及び半複信")
			}
		case TypeStatusReq:
			add("ステータス付加情報", map[bool]string{true: "確認型（宛先からの応答が必要）", false: "非確認型"}[x.Confirm])
		}
		add("発信者番号", NumberText(x.Caller))
		if x.Type == TypeMsgResp || x.Type == TypeStatusResp {
			add("着信者番号", NumberText(x.Dest))
		} else {
			add("着信者番号又はグループ番号", x.DestText())
		}
	}
	if x.Enc >= 0 {
		if x.Enc == 0 {
			add("暗号化モード", "非暗号化")
		} else {
			add("暗号化モード", "スクランブル暗号")
			add("鍵番号", fmt.Sprintf("%d", x.KeyNo))
		}
	}
	switch x.Type {
	case TypeBearerHeader:
		add("パケット情報", fmt.Sprintf("ブロック数 %d・Null オクテット数 %d・先頭 %v・フラグメント番号 %d", x.Blocks, x.Null, x.First, x.Fragment))
	case TypeMsgHeader:
		add("メッセージパケット情報", fmt.Sprintf("%s・%s・ブロック数 %d・Null オクテット数 %d・先頭 %v・送信フラグメント番号 %d",
			map[bool]string{true: "確認型", false: "非確認型"}[x.Confirm], map[bool]string{true: "選択的再送", false: "通常の送信"}[x.Select],
			x.Blocks, x.Null, x.First, x.Fragment))
	case TypeMsgResp:
		add("応答情報", fmt.Sprintf("%s・受信フラグメント番号 %d", RespText(x.Class, x.RespType), x.RecvFrag))
		add("エラーブロックフラグ", fmt.Sprintf("%016b", x.ErrFlags))
	case TypeDummy:
		add("ダミーカウント", fmt.Sprintf("残り %d", x.Dummy))
	case TypeStatusReq:
		st := fmt.Sprintf("ステータス %d", x.Status)
		switch {
		case x.Status == 0:
			st = "0（予約）"
		case x.Status > 207:
			st = fmt.Sprintf("%d（未定義）", x.Status)
		}
		add("ステータス番号", st)
	case TypeStatusResp:
		add("理由表示", ReasonText(x.Reason))
	case TypeGPSData:
		g := ParseGPS(x.Payload)
		add("GPS 情報", fmt.Sprintf("% X", x.Payload))
		add("GPS 情報の読み（暗号なら外す前）", g.Text())
	case TypeBearerData, TypeMsgData:
		add("フレーム番号", frameNoText(x.FrameNo))
		add("ブロック番号", frameNoText(x.BlockNo))
		add("ユーザデータ", fmt.Sprintf("% X", x.Payload))
	case TypeCallerName:
		add("区画", fmt.Sprintf("%d/%d", x.Seg, x.Segs))
		if x.Seg == 3 {
			add("ユーザネーム 13〜14", fmt.Sprintf("% X", x.Payload[:2]))
			add("チェックビット（CRC-32）", fmt.Sprintf("% X", x.Payload[2:]))
		} else {
			add(fmt.Sprintf("ユーザネーム %d〜%d", 6*x.Seg-5, 6*x.Seg), fmt.Sprintf("% X", x.Payload))
		}
	}
	return rows
}

func frameNoText(n int) string {
	if n == 0 {
		return "第 0（最終）"
	}
	return fmt.Sprintf("第 %d", n)
}

func RespText(class, typ int) string {
	switch {
	case class == 0 && typ == 1:
		return "ACK（受信成功）"
	case class == 1 && typ == 1:
		return "ACK_S（選択的再送要求）"
	case class == 3 && typ == 1:
		return "NACK（全再送要求）"
	case class == 3 && typ == 2:
		return "NACK（メモリフル）"
	case class == 3 && typ == 3:
		return "NACK（中止）"
	}
	return fmt.Sprintf("予約（クラス %02b・タイプ %03b）", class, typ)
}

func ReasonText(r int) string {
	switch r {
	case 0x01:
		return "ACK（受信成功）"
	case 0x08:
		return "NACK（受信失敗／再送要求）"
	case 0x09:
		return "NACK（スタックメモリフル）"
	case 0x0A:
		return "NACK（中止）"
	}
	return fmt.Sprintf("予約 %07b", r)
}

func (m Message) b54Summary() string {
	x := m.B54
	parts := []string{m.Name}
	if m.Maker >= 0 {
		parts = append(parts, fmt.Sprintf("製造者 %d", m.Maker))
	}
	if c := x.CallText(); c != "" {
		parts = append(parts, c)
	}
	switch x.Type {
	case TypeMsgHeader, TypeBearerHeader:
		parts = append(parts, fmt.Sprintf("%d ブロック・フラグメント %d", x.Blocks, x.Fragment))
	case TypeMsgData, TypeBearerData:
		parts = append(parts, fmt.Sprintf("フレーム %d・ブロック %d", x.FrameNo, x.BlockNo))
	case TypeMsgResp:
		parts = append(parts, RespText(x.Class, x.RespType))
	case TypeDummy:
		parts = append(parts, fmt.Sprintf("残り %d", x.Dummy))
	case TypeStatusReq:
		parts = append(parts, fmt.Sprintf("ステータス %d", x.Status))
	case TypeStatusResp:
		parts = append(parts, ReasonText(x.Reason))
	case TypeCallerName:
		parts = append(parts, fmt.Sprintf("%d/%d %X", x.Seg, x.Segs, x.Payload))
	case TypeGPSData:
		parts = append(parts, fmt.Sprintf("%X", x.Payload))
	}
	return strings.Join(parts, "・")
}
