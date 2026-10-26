package control

import "fmt"

const (
	MsgBCCHInfo       = 0x10
	MsgIdlePCH        = 0x20
	MsgBCCHChanged    = 0x21
	MsgBroadcastStart = 0x22
	MsgDelayedStart   = 0x23
	MsgForcedRelease  = 0x30
	MsgIdleSCCH       = 0x40
	MsgNumberNotify   = 0x63
	MsgRadioControl   = 0x78
)

type messageInfo struct {
	Name    string
	Channel string
	Section string
}

var messages = map[int]messageInfo{
	MsgBCCHInfo:       {"報知情報", "BCCH", "§4.3.4.1"},
	MsgIdlePCH:        {"アイドル信号(PCH)", "PCH", "§4.3.5.1"},
	MsgBCCHChanged:    {"BCCH変更通知", "PCH", "§4.3.5.2"},
	MsgBroadcastStart: {"通報開始指示", "PCH", "§4.3.5.3"},
	MsgDelayedStart:   {"時差通報開始指示", "PCH", "§4.3.5.4"},
	MsgForcedRelease:  {"強制切断指示", "PCH", "§4.3.5.5"},
	MsgIdleSCCH:       {"アイドル信号(SCCH)", "SCCH", "§4.3.6.1"},
	MsgNumberNotify:   {"番号通知", "FACCH", "§4.3.7.1"},
	MsgRadioControl:   {"無線制御要求", "SB", "§4.3.8.1"},
}

func KnownType(t int) bool { _, ok := messages[t]; return ok }

func TypeName(t int) string {
	if m, ok := messages[t]; ok {
		return m.Name
	}
	return fmt.Sprintf("不明(0x%02x)", t)
}

func SpecSection(t int) string { return messages[t].Section }

func channelOfType(t int) string {
	if m, ok := messages[t]; ok {
		return m.Channel
	}
	return "CAC"
}

func isBCCHFormat(t int) bool {
	return t == MsgBCCHInfo || t == MsgIdlePCH || t == MsgBCCHChanged || t == MsgIdleSCCH
}

func hasCCHHeader(t int) bool {
	switch t {
	case MsgBCCHInfo, MsgIdlePCH, MsgBCCHChanged, MsgBroadcastStart,
		MsgDelayedStart, MsgForcedRelease, MsgIdleSCCH:
		return true
	}
	return false
}

var Manufacturers = map[int]string{
	2: "沖電気工業", 3: "東芝", 5: "日本電気(NEC)", 6: "日本無線(JRC)",
	7: "日立国際電気", 8: "富士通", 10: "松下電器産業(パナソニック)", 11: "三菱電機",
	13: "富士通ゼネラル", 171: "日立国際電気",
}

func ManufacturerName(code int) string {
	if n, ok := Manufacturers[code]; ok {
		return n
	}
	return fmt.Sprintf("不明(%d)", code)
}

var parentMode = map[int]string{0b01: "間欠送信モード", 0b10: "通話時送信モード", 0b00: "予約", 0b11: "予約"}

var mediaType = map[int]string{
	0: "予約/音声なし", 1: "音声", 2: "FAX", 3: "文字", 4: "画像", 5: "テレメータ",
	6: "その他", 7: "予備",
}

var transProtocol = map[int]string{
	0b00: "デジタル非音声", 0b01: "推奨規格拡声音声/連絡用音声1",
	0b10: "予約/連絡用音声2", 0b11: "予約",
}

var releaseReasons = map[int]string{
	0b0000: "正常切断/正常解放", 0b0001: "親局からの強制切断", 0b0010: "ビジー",
	0b0011: "相手無応答", 0b0100: "通信時限満了", 0b0110: "チャネル使用不可",
	0b0111: "サービス利用不可", 0b1000: "通信不可", 0b1001: "緊急通話不可",
	0b1010: "無効メッセージ", 0b1011: "同期はずれ", 0b1100: "通信時限以外のタイマ満了",
	0b1110: "番号通知送受失敗/市区町村コード不一致", 0b1111: "その他異常時",
}

var volumeSetting = map[int]string{0: "通常", 1: "最小", 2: "最大", 3: "予約"}

var emergencyCallLimit = map[int]string{
	0b000: "緊急通話不可", 0b001: "30秒", 0b010: "60秒", 0b011: "90秒",
	0b100: "120秒", 0b101: "150秒", 0b110: "180秒", 0b111: "無制限",
}

var validNumberID = map[int]string{
	0b00: "番号指定なし", 0b01: "予約", 0b10: "予約", 0b11: "予約",
}

var relatedSlotID = map[int]string{
	0b00: "識別なし", 0b01: "現スロット", 0b10: "全スロット", 0b11: "関連スロット指定",
}

var followingBurst = map[int]string{
	0b000: "制御用(CCH)", 0b001: "制御用(FACCH)", 0b010: "制御用(TCH(B))",
	0b011: "同期バースト(残カウンタ制御無し)",
	0b100: "通信用(TCH(I))", 0b101: "通信用(FACCH)", 0b110: "通信用(TCH(B))",
	0b111: "同期バースト(残カウンタ制御有り)",
}

type CCHHeader struct {
	Busy bool

	ReservedOct2 int
	FrameNo      int

	StatusFlags2      int
	VoiceLinkOK       bool
	DataTxOK          bool
	StatusFlags2Spare int
	SuperframeFrames  int

	StatusFlags1   int
	EmergencyOK    bool
	NonEmergencyOK bool
	SlotUsage      int
	UsedSlots      []int

	Broadcasting      bool
	MediaCode         int
	Media             string
	TransProtocolCode int
	TransProtocol     string
	ReservedOct5      int

	TrafficRestricted bool
}

type BCCHFields struct {
	SpareOct6      int
	BCCHUpdateNo   int
	ParentModeCode int
	ParentMode     string
	ReservedOct7   int
	SuperframeLenS int

	LicenseInfo     int
	LicenseInfoHigh int
	LicenseInfoLow  int

	NumPCH           int
	NumSCCHBeforePCH int

	IDValidBits int

	UplinkLoopback         bool
	EmergencyCallLimitCode int
	EmergencyCallLimit     string

	ManufacturerCode int
	ManufacturerName string
}

type BroadcastFields struct {
	SpareOct6   int
	SplitNo     int
	HasSplitNo  bool
	CallNo      int
	ID1         int
	ID2         int
	SpareOct11  int
	ReservedB4  int
	ForceVolume bool
	VolumeCode  int
	Volume      string
	N2          bool
	N1          bool
	StartPos    int
}

type ReleaseFields struct {
	SpareOct6  int
	CallNo     int
	SpareOct7  int
	ReasonCode int
	Reason     string
	SpareTail  int
	SpareTailX string
}

type NotifyFields struct {
	Spare0        int
	SpareOct2To5  int
	SpareOct2To5X string
	SpareOct6     int

	CallNo           int
	SubStationID     int
	MunicipalCode    int
	CityName         string
	ManufacturerCode int
	ManufacturerName string

	LicenseInfoLen int
	LicenseInfo string
}

type RadioControlFields struct {
	Spare0        int
	SpareOct2To4  int
	SpareOct2To4X string
	SpareOct5     int

	ValidNumberIDCode int
	ValidNumberID     string

	SpareOct6To8  int
	SpareOct6To8X string

	SpareOct9B8     int
	RemainCounter   int
	RemainCounterHi bool

	RelatedSlotIDCode int
	RelatedSlotID     string
	RelatedSlotMask   int
	RelatedSlots      []int

	SpareOct11To12 int
}

type SyncTransCtrl struct {
	Raw int

	Spare int

	SlotNo      int
	SlotNoValid bool

	RemainCounterCtrl bool

	FollowingBurstCode int
	FollowingBurst     string
}

func ParseSyncTransCtrl(octet int) SyncTransCtrl {
	octet &= 0xFF
	slotNo := (octet >> 4) & 0b111
	fb := octet & 0b111
	return SyncTransCtrl{
		Raw:                octet,
		Spare:              (octet >> 7) & 1,
		SlotNo:             slotNo,
		SlotNoValid:        slotNo <= 5,
		RemainCounterCtrl:  (octet>>3)&1 == 1,
		FollowingBurstCode: fb,
		FollowingBurst:     lookup(followingBurst, fb, "予約"),
	}
}

type Message struct {
	RawHex   string
	Type     int
	TypeName string
	Channel  string
	Section  string
	CRCOK    bool

	Busy bool

	Header *CCHHeader

	BCCH         *BCCHFields
	Broadcast    *BroadcastFields
	Release      *ReleaseFields
	Notify       *NotifyFields
	RadioControl *RadioControlFields

	SyncCtrl *SyncTransCtrl

	Seed int
}

func bitsToInt(b []uint8) int {
	v := 0
	for _, x := range b {
		v = (v << 1) | int(x&1)
	}
	return v
}

func bitsToHex(b []uint8) string {
	width := (len(b) + 3) / 4
	v := make([]byte, 0, width)
	pad := width*4 - len(b)
	acc, n := 0, 0
	for i := 0; i < pad; i++ {
		acc, n = acc<<1, n+1
	}
	for _, x := range b {
		acc = (acc << 1) | int(x&1)
		n++
		if n == 4 {
			v = append(v, "0123456789abcdef"[acc])
			acc, n = 0, 0
		}
	}
	return string(v)
}

func slotsFromMask(mask int) []int {
	out := []int{}
	for i := 0; i < 6; i++ {
		if (mask>>uint(5-i))&1 == 1 {
			out = append(out, i)
		}
	}
	return out
}

func parseCCHHeader(b []uint8) *CCHHeader {
	slotUsage := bitsToInt(b[34:40])
	media := bitsToInt(b[41:44])
	proto := bitsToInt(b[44:46])
	h := &CCHHeader{
		Busy: b[8] != 0,

		ReservedOct2: bitsToInt(b[16:19]),
		FrameNo:      bitsToInt(b[19:24]),

		StatusFlags2:      bitsToInt(b[24:27]),
		VoiceLinkOK:       b[24] != 0,
		DataTxOK:          b[25] != 0,
		StatusFlags2Spare: int(b[26] & 1),
		SuperframeFrames:  bitsToInt(b[27:32]),

		StatusFlags1:   bitsToInt(b[32:34]),
		EmergencyOK:    b[32] != 0,
		NonEmergencyOK: b[33] != 0,
		SlotUsage:      slotUsage,
		UsedSlots:      slotsFromMask(slotUsage),

		Broadcasting:      b[40] != 0,
		MediaCode:         media,
		Media:             lookup(mediaType, media, "予約"),
		TransProtocolCode: proto,
		TransProtocol:     lookup(transProtocol, proto, "予約"),
		ReservedOct5:      bitsToInt(b[46:48]),
	}
	h.TrafficRestricted = !h.EmergencyOK && !h.NonEmergencyOK && !h.VoiceLinkOK && !h.DataTxOK
	return h
}

func ParseMessage(payload []uint8, seed int) Message {
	if len(payload) > PayloadLen {
		payload = payload[:PayloadLen]
	}
	b := payload
	msg := Message{
		RawHex: bitsToHex(b),
		Seed:   seed,
	}
	msg.Type = bitsToInt(b[9:16])
	msg.TypeName = TypeName(msg.Type)
	msg.Channel = channelOfType(msg.Type)
	msg.Section = SpecSection(msg.Type)

	if hasCCHHeader(msg.Type) {
		msg.Header = parseCCHHeader(b)
		msg.Busy = msg.Header.Busy
	}

	switch {
	case isBCCHFormat(msg.Type):
		mfr := bitsToInt(b[88:104])
		hi, lo := bitsToInt(b[64:68]), bitsToInt(b[72:76])
		limit := bitsToInt(b[85:88])
		mode := bitsToInt(b[56:58])
		msg.BCCH = &BCCHFields{
			SpareOct6:      bitsToInt(b[48:52]),
			BCCHUpdateNo:   bitsToInt(b[52:56]),
			ParentModeCode: mode,
			ParentMode:     lookup(parentMode, mode, "?"),
			ReservedOct7:   int(b[58] & 1),
			SuperframeLenS: bitsToInt(b[59:64]),

			LicenseInfo:     hi<<4 | lo,
			LicenseInfoHigh: hi,
			LicenseInfoLow:  lo,

			NumPCH:           bitsToInt(b[68:72]),
			NumSCCHBeforePCH: bitsToInt(b[76:80]),

			IDValidBits:            bitsToInt(b[80:84]),
			UplinkLoopback:         b[84] != 0,
			EmergencyCallLimitCode: limit,
			EmergencyCallLimit:     lookup(emergencyCallLimit, limit, "?"),

			ManufacturerCode: mfr,
			ManufacturerName: ManufacturerName(mfr),
		}

	case msg.Type == MsgBroadcastStart || msg.Type == MsgDelayedStart:
		vol := bitsToInt(b[94:96])
		f := &BroadcastFields{
			SpareOct6:   bitsToInt(b[48:52]),
			CallNo:      bitsToInt(b[52:56]),
			ID1:         bitsToInt(b[56:72]),
			ID2:         bitsToInt(b[72:88]),
			SpareOct11:  bitsToInt(b[88:92]),
			ReservedB4:  int(b[92] & 1),
			ForceVolume: b[93] != 0,
			VolumeCode:  vol,
			Volume:      lookup(volumeSetting, vol, "予約"),
			N2:          b[96] != 0,
			N1:          b[97] != 0,
			StartPos:    bitsToInt(b[98:104]),
		}
		if msg.Type == MsgDelayedStart {
			f.SplitNo = f.SpareOct6
			f.HasSplitNo = true
		}
		msg.Broadcast = f

	case msg.Type == MsgForcedRelease:
		reason := bitsToInt(b[60:64])
		msg.Release = &ReleaseFields{
			SpareOct6:  bitsToInt(b[48:52]),
			CallNo:     bitsToInt(b[52:56]),
			SpareOct7:  bitsToInt(b[56:60]),
			ReasonCode: reason,
			Reason:     lookup(releaseReasons, reason, fmt.Sprintf("予約(%d)", reason)),
			SpareTail:  bitsToInt(b[64:104]),
			SpareTailX: bitsToHex(b[64:104]),
		}

	case msg.Type == MsgRadioControl:
		sc := ParseSyncTransCtrl(bitsToInt(b[0:8]))
		msg.SyncCtrl = &sc

		vn := bitsToInt(b[46:48])
		rs := bitsToInt(b[80:82])
		mask := bitsToInt(b[82:88])
		cnt := bitsToInt(b[73:80])
		msg.RadioControl = &RadioControlFields{
			Spare0:        int(b[8] & 1),
			SpareOct2To4:  bitsToInt(b[16:40]),
			SpareOct2To4X: bitsToHex(b[16:40]),
			SpareOct5:     bitsToInt(b[40:46]),

			ValidNumberIDCode: vn,
			ValidNumberID:     lookup(validNumberID, vn, "予約"),

			SpareOct6To8:  bitsToInt(b[48:72]),
			SpareOct6To8X: bitsToHex(b[48:72]),

			SpareOct9B8:     int(b[72] & 1),
			RemainCounter:   cnt,
			RemainCounterHi: cnt >= 121,

			RelatedSlotIDCode: rs,
			RelatedSlotID:     lookup(relatedSlotID, rs, "?"),
			RelatedSlotMask:   mask,
			RelatedSlots:      slotsFromMask(mask),

			SpareOct11To12: bitsToInt(b[88:104]),
		}
	}
	return msg
}

func lookup(m map[int]string, k int, dflt string) string {
	if v, ok := m[k]; ok {
		return v
	}
	return dflt
}
