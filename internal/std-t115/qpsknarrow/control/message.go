package control

import "fmt"

const (
	MsgBroadcastInfo     = 0x01
	MsgIdleCCH           = 0x02
	MsgNotifyStart       = 0x03
	MsgVoiceDataStart    = 0x04
	MsgSirenStart        = 0x05
	MsgForcedDisconnect  = 0x06
	MsgDisconnect        = 0x07
	MsgOutdoorCallReq    = 0x08
	MsgOutdoorCallResp   = 0x09
	MsgMasterCallReq     = 0x0A
	MsgMasterCallResp    = 0x0B
	MsgDLRelayCallReq    = 0x0C
	MsgDLRelayCallResp   = 0x0D
	MsgULRelayCallReq    = 0x0E
	MsgULRelayCallResp   = 0x0F
	MsgDLDataReq         = 0x10
	MsgDLDataReq2        = 0x11
	MsgULDataReq         = 0x12
	MsgULDataResp        = 0x13
	MsgTxRightAcquireReq = 0x14
	MsgMonitorNotifyReq  = 0x15
	MsgMonitorNotify     = 0x16
	MsgControlReq        = 0x17
	MsgControlResp       = 0x18
	MsgIdleFACCH       = 0x50
	MsgDisconnectFACCH = 0x51
	MsgNumberNotify    = 0x52
	MsgTxRightRelease  = 0x53
	MsgIdleTCH           = 0x70
	MsgVoiceGroupIndiv   = 0x71
	MsgVoiceSimultaneous = 0x72
	MsgVoiceEmergency    = 0x73
	MsgVoiceContactCall  = 0x74
	MsgData              = 0x75
)

var msgNames = map[int]string{
	MsgBroadcastInfo: "報知情報(規制要求)", MsgIdleCCH: "アイドル信号(CCH)",
	MsgNotifyStart: "通報開始指示", MsgVoiceDataStart: "音声・データ通報開始指示",
	MsgSirenStart: "サイレン通報開始指示", MsgForcedDisconnect: "強制切断指示",
	MsgDisconnect: "切断指示", MsgOutdoorCallReq: "屋外子局呼出開始要求",
	MsgOutdoorCallResp: "屋外子局呼出開始応答", MsgMasterCallReq: "親局呼出開始要求",
	MsgMasterCallResp: "親局呼出開始応答", MsgDLRelayCallReq: "下り中継局呼出開始要求",
	MsgDLRelayCallResp: "下り中継局呼出開始応答", MsgULRelayCallReq: "上り中継局呼出開始要求",
	MsgULRelayCallResp: "上り中継局呼出開始応答", MsgDLDataReq: "親局起動データ伝送開始要求",
	MsgDLDataReq2: "親局起動データ伝送開始要求(上り)", MsgULDataReq: "子局起動データ伝送開始要求",
	MsgULDataResp: "子局起動データ伝送開始応答", MsgTxRightAcquireReq: "送信権取得要求",
	MsgMonitorNotifyReq: "監視通知要求", MsgMonitorNotify: "監視通知",
	MsgControlReq: "制御要求", MsgControlResp: "制御応答",
	MsgIdleFACCH: "アイドル信号(FACCH)", MsgDisconnectFACCH: "切断指示(FACCH)",
	MsgNumberNotify: "番号通知", MsgTxRightRelease: "送信権解放通知",
	MsgIdleTCH: "アイドル信号(TCH)", MsgVoiceGroupIndiv: "群/個別拡声通報音声",
	MsgVoiceSimultaneous: "一斉拡声通報音声", MsgVoiceEmergency: "緊急一斉拡声通報音声",
	MsgVoiceContactCall: "連絡通信音声", MsgData: "データ",
}

func TypeName(t int) string {
	if s, ok := msgNames[t]; ok {
		return s
	}
	switch {
	case (t >= 0x28 && t <= 0x4F) || (t >= 0x60 && t <= 0x6F) || (t >= 0x7B && t <= 0x7F):
		return fmt.Sprintf("システム固有情報(0x%02X)", t)
	default:
		return fmt.Sprintf("予約/未知(0x%02X)", t)
	}
}

const (
	CCHInfoBits   = 96
	CCHOctets     = 12
	FACCHInfoBits = 504
	FACCHOctets   = 63
	TCHInfoBits   = 504
	TCHOctets     = 63
	HeaderOctets  = 3
	MessageBits = CCHInfoBits
)

const (
	ChIDCCH   = 0
	ChIDFACCH = 1
	ChIDTCH   = 2
)

var chIDNames = [4]string{"CCH", "FACCH", "TCH", "予約"}

type Header struct {
	Busy bool

	Type     int
	TypeName string

	LicenseeInfo     int
	ManufacturerCode int
	ManufacturerName string

	AMRWBPlusSN int
	SpareOct3B8 int

	ChID     int
	ChIDName string

	ChSwitchToSC bool
	ChSwitchTiming int
}

var manufacturers = map[int]string{
	1: "予備", 2: "沖電気工業", 3: "東芝", 4: "予備", 5: "日本電気(NEC)",
	6: "日本無線", 7: "国際電気", 8: "富士通", 9: "予備",
	10: "パナソニックグループ", 11: "三菱電機", 12: "予備", 13: "富士通ゼネラル",
	14: "アイコム", 15: "アンリツ", 16: "JVCケンウッド", 17: "電気興業",
	18: "野村エンジニアリング", 19: "双葉電子工業", 20: "モトローラ・ソリューションズ・ジャパン",
	21: "リズム",
}

type NotifyStart struct {
	MediaCode int
	Media     string
	TransProt int
	CallNo    int

	SpareOct5     int
	NumberNotify  bool
	RecordRelease bool
	Emergency     bool
	ForcedVolume  bool
	VolumeSetting int

	SpareOct6   int
	TimeSplitOK bool
	SplitNo     int

	SubStationID int
	Simultaneous bool

	SpareOct9to12 uint32
}

var mediaNames = [8]string{"予約", "音声", "FAX", "文字", "画像", "予約(テレメトリ)", "予備", "予備"}

type Message struct {
	Raw []byte

	Header Header

	NotifyStart *NotifyStart
	NumberNotify *NumberNotify
}

type NumberNotify struct {
	CallNo int
	Count int
	IDs []int
	Continuation int
	SpareOct5    int
	SpareOct6    int
	SpareOct63   int
}

func bitsToOctets(bits []uint8) ([]byte, error) {
	if len(bits) < HeaderOctets*8 {
		return nil, fmt.Errorf("情報部が %d bit しかない（最低 %d 必要）",
			len(bits), HeaderOctets*8)
	}
	n := len(bits) / 8
	o := make([]byte, n)
	for i := 0; i < n; i++ {
		var v byte
		for j := 0; j < 8; j++ {
			v = v<<1 | (bits[i*8+j] & 1)
		}
		o[i] = v
	}
	return o, nil
}

func Decode(bits []uint8) (*Message, error) {
	o, err := bitsToOctets(bits)
	if err != nil {
		return nil, err
	}
	m := &Message{Raw: o}

	h := &m.Header
	h.Busy = o[0]&0x80 != 0
	h.Type = int(o[0] & 0x7F)
	h.TypeName = TypeName(h.Type)

	h.LicenseeInfo = int(o[1] >> 6)
	h.ManufacturerCode = int(o[1] & 0x3F)
	if s, ok := manufacturers[h.ManufacturerCode]; ok {
		h.ManufacturerName = s
	} else {
		h.ManufacturerName = fmt.Sprintf("未割当(%d)", h.ManufacturerCode)
	}

	h.ChID = int((o[2] >> 5) & 0x03)
	h.ChIDName = chIDNames[h.ChID]
	h.ChSwitchToSC = o[2]&0x10 != 0
	h.ChSwitchTiming = int(o[2] & 0x0F)
	switch h.Type {
	case MsgVoiceGroupIndiv, MsgVoiceSimultaneous, MsgVoiceEmergency, MsgVoiceContactCall:
		h.AMRWBPlusSN = int(o[2] >> 7)
		h.SpareOct3B8 = -1
	default:
		h.AMRWBPlusSN = -1
		h.SpareOct3B8 = int(o[2] >> 7)
	}

	if h.Type == MsgNotifyStart || h.Type == MsgVoiceDataStart {
		n := &NotifyStart{}
		n.MediaCode = int(o[3] >> 5)
		n.Media = mediaNames[n.MediaCode]
		n.TransProt = int((o[3] >> 3) & 0x03)
		n.CallNo = int(o[3] & 0x07)

		n.SpareOct5 = int(o[4] >> 6)
		n.NumberNotify = o[4]&0x20 != 0
		n.RecordRelease = o[4]&0x10 != 0
		n.Emergency = o[4]&0x08 != 0
		n.ForcedVolume = o[4]&0x04 != 0
		n.VolumeSetting = int(o[4] & 0x03)

		n.SpareOct6 = int(o[5] >> 5)
		n.TimeSplitOK = o[5]&0x10 != 0
		n.SplitNo = int(o[5] & 0x0F)

		n.SubStationID = int(o[6])<<8 | int(o[7])
		n.Simultaneous = n.SubStationID == 0

		n.SpareOct9to12 = uint32(o[8])<<24 | uint32(o[9])<<16 |
			uint32(o[10])<<8 | uint32(o[11])
		m.NotifyStart = n
	}

	if h.Type == MsgNumberNotify && len(o) >= FACCHOctets {
		nn := &NumberNotify{
			CallNo:       int(o[3] & 0x07),
			SpareOct5:    int(o[4]),
			SpareOct6:    int(o[5] >> 5),
			Count:        int(o[5] & 0x1F),
			Continuation: int(o[62] & 0x1F),
			SpareOct63:   int(o[62] >> 5),
		}
		const maxIDs = 28
		cnt := nn.Count
		if cnt > maxIDs {
			cnt = maxIDs
		}
		for i := 0; i < cnt; i++ {
			hi := 6 + 2*i
			nn.IDs = append(nn.IDs, int(o[hi])<<8|int(o[hi+1]))
		}
		m.NumberNotify = nn
	}
	return m, nil
}

func NotifyStartTypes() []int { return []int{MsgNotifyStart, MsgVoiceDataStart} }

func (m *Message) String() string {
	s := fmt.Sprintf("%s [%s]", m.Header.TypeName, m.Header.ChIDName)
	if m.Header.Busy {
		s += " BUSY"
	}
	s += fmt.Sprintf(" 製造者=%s 切替=%d", m.Header.ManufacturerName, m.Header.ChSwitchTiming)
	if n := m.NumberNotify; n != nil {
		s += fmt.Sprintf(" 呼番号=%d 子局数=%d", n.CallNo, n.Count)
		if len(n.IDs) > 0 {
			s += fmt.Sprintf(" 識別番号=%v", n.IDs)
		}
		switch n.Continuation {
		case 0:
			s += " 連送終了"
		case 31:
			s += " 連送継続中"
		default:
			s += fmt.Sprintf(" 残り%dフレーム", n.Continuation)
		}
	}
	if n := m.NotifyStart; n != nil {
		target := fmt.Sprintf("子局=%d(群/個別)", n.SubStationID)
		if n.Simultaneous {
			target = "一斉"
		}
		s += fmt.Sprintf(" メディア=%s 呼番号=%d %s", n.Media, n.CallNo, target)
		if n.Emergency {
			s += " 緊急"
		}
		if n.ForcedVolume {
			s += " 強制音量"
		}
		if n.TimeSplitOK {
			s += fmt.Sprintf(" 時差(分割%d)", n.SplitNo)
		}
	}
	return s
}
