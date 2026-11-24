package l3

import (
	"fmt"
	"sort"
	"strings"
)


const SeqNone = -1

const (
	CodeRTOrigReport    uint8 = 0x01
	CodeRTPaging        uint8 = 0x02
	CodeRTTermReport    uint8 = 0x03
	CodeRTMSRelease     uint8 = 0x21
	CodeRTChanAssign    uint8 = 0x43
	CodeRTHandover      uint8 = 0x61
	CodeRTSysInfo       uint8 = 0x62
	CodeRTSysInfoAck    uint8 = 0x63
	CodeRTFallbackReq   uint8 = 0x71
	CodeRTRecallTx      uint8 = 0x72
	CodeRTRecallRx      uint8 = 0x73
	CodeRTDisconnect    uint8 = 0x81
	CodeRTDisconnectAck uint8 = 0x82
	CodeRTBroadcast     uint8 = 0xA1
	CodeRTBCCHRecv      uint8 = 0xA2
	CodeRTTxRelease     uint8 = 0xC1
	CodeRTTxStopReq     uint8 = 0xC2
	CodeRTLicensee      uint8 = 0xFF
)

const (
	CodeCCAlert         uint8 = 0x01
	CodeCCCallProc      uint8 = 0x02
	CodeCCProgress      uint8 = 0x03
	CodeCCSetup         uint8 = 0x05
	CodeCCConnect       uint8 = 0x07
	CodeCCConnectAck    uint8 = 0x0F
	CodeCCRecallProc    uint8 = 0x11
	CodeCCCommMode      uint8 = 0x12
	CodeCCCommPossible  uint8 = 0x13
	CodeCCRecallReject  uint8 = 0x14
	CodeCCSimplexSetup  uint8 = 0x15
	CodeCCSimplexRecall uint8 = 0x16
	CodeCCDuplexSetup   uint8 = 0x17
	CodeCCDuplexRecall  uint8 = 0x18
	CodeCCUserInfo      uint8 = 0x20
	CodeCCDisconnect    uint8 = 0x45
	CodeCCRelease       uint8 = 0x4D
	CodeCCReleaseComp   uint8 = 0x5A
	CodeCCFacility      uint8 = 0x62
	CodeCCCongestion    uint8 = 0x79
	CodeCCInfo          uint8 = 0x7B
	CodeCCStatus        uint8 = 0x7D
)

var seqRTNames = map[int]string{
	0x01: "発信無線状態報告", 0x02: "ページング", 0x03: "着信無線状態報告", 0x21: "移動局解放", 0x43: "無線チャネル指定",
	0x61: "切替先無線チャネル指定", 0x62: "システム情報", 0x63: "システム情報確認", 0x71: "切り戻し要求", 0x72: "送信中再発呼",
	0x73: "受信中再発呼", 0x81: "無線チャネル切断", 0x82: "無線チャネル切断確認", 0xA1: "報知情報", 0xA2: "BCCH 受信",
	0xC1: "送信権解放", 0xC2: "送信停止要求", 0xFF: "免許人固有情報",
}

var seqCCNames = map[int]string{
	0x01: "呼出", 0x02: "呼設定受付", 0x03: "経過表示", 0x05: "呼設定", 0x07: "応答", 0x0F: "応答確認", 0x11: "再呼設定受付",
	0x12: "通信形態", 0x13: "通信可否", 0x14: "再呼設定拒否", 0x15: "単信呼設定", 0x16: "単信再呼設定", 0x17: "複信呼設定",
	0x18: "複信再呼設定", 0x20: "ユーザ情報", 0x45: "切断", 0x4D: "解放", 0x5A: "解放完了", 0x62: "ファシリティ",
	0x79: "輻輳制御", 0x7B: "付加情報", 0x7D: "状態表示",
}

func CodeName(entity string, code int) string {
	var n string
	var ok bool
	switch entity {
	case "RT":
		n, ok = seqRTNames[code]
	case "CC":
		n, ok = seqCCNames[code]
	case "MM":
		if s, found := mmSpecs[uint8(code)]; found {
			n, ok = s.name, true
		}
	}
	if !ok {
		return fmt.Sprintf("%s:0x%02X", entity, code)
	}
	return n
}


type Combination struct {
	No       int
	RT, CC   uint8
	Kinds    string
	Phases   []string
	Abnormal bool
	Note     string
}

func (c Combination) Text() string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "(%d) %s", c.No, c.Kinds)
	for _, p := range c.Phases {
		sb.WriteString("【" + p + "】")
	}
	fmt.Fprintf(&sb, " %s（RT）＋%s（CC）", CodeName("RT", int(c.RT)), CodeName("CC", int(c.CC)))
	if c.Abnormal {
		sb.WriteString("［準正常信号］")
	}
	return sb.String()
}

var Combinations = []Combination{
	{No: 1, RT: CodeRTOrigReport, CC: CodeCCSimplexSetup, Kinds: "単信個別通信、グループ通信", Phases: []string{"通信リンク確立フェーズ"}},
	{No: 2, RT: CodeRTOrigReport, CC: CodeCCDuplexSetup, Kinds: "複信個別通信", Phases: []string{"通信リンク確立フェーズ"}},
	{No: 3, RT: CodeRTPaging, CC: CodeCCCommMode, Kinds: "単信個別通信、グループ通信、同報通信、複信個別通信", Phases: []string{"通信リンク確立フェーズ"}},
	{No: 4, RT: CodeRTTermReport, CC: CodeCCCommPossible, Kinds: "単信個別通信、複信個別通信", Phases: []string{"通信リンク確立フェーズ"}},
	{No: 5, RT: CodeRTMSRelease, CC: CodeCCReleaseComp, Kinds: "単信個別通信、グループ通信、同報通信、複信個別通信", Phases: []string{"通信リンク確立フェーズ"}, Abnormal: true},
	{No: 6, RT: CodeRTChanAssign, CC: CodeCCSetup, Kinds: "単信個別通信、グループ通信", Phases: []string{"通信リンク確立フェーズ"},
		Note: "単信シーケンス【通信リンク確立フェーズ】で発移動局と着移動局が異なるゾーンに居る場合は必須ではない（※1）"},
	{No: 7, RT: CodeRTDisconnect, CC: CodeCCDisconnect, Kinds: "単信個別通信、グループ通信", Phases: []string{"通信リンク解放フェーズ"}},
	{No: 8, RT: CodeRTRecallTx, CC: CodeCCSimplexRecall, Kinds: "チャネル切替",
		Phases: []string{"単信個別通信、グループ通信、送信中チャネル切替", "単信個別通信、グループ通信切り戻し（送信中）"}},
	{No: 9, RT: CodeRTRecallRx, CC: CodeCCSimplexRecall, Kinds: "チャネル切替",
		Phases: []string{"単信個別通信、グループ通信、受信中チャネル切替", "同報通信チャネル切替", "単信個別通信、グループ通信切り戻し（受信中）", "同報通信切り戻し"}},
	{No: 10, RT: CodeRTRecallTx, CC: CodeCCDuplexRecall, Kinds: "チャネル切替", Phases: []string{"複信個別通信チャネル切替"}},
	{No: 11, RT: CodeRTFallbackReq, CC: CodeCCRecallReject, Kinds: "チャネル切替", Phases: []string{"全般"}},
}

func Piggyback(rtType, ccType uint8) (Combination, bool) {
	for _, c := range Combinations {
		if c.RT == rtType && c.CC == ccType {
			return c, true
		}
	}
	return Combination{}, false
}

func PiggybackSignal(s Signal) (Combination, bool) {
	rt, _, cc := seqCodes(s)
	if rt < 0 || cc < 0 {
		return Combination{}, false
	}
	return Piggyback(uint8(rt), uint8(cc))
}

func PiggybackCheck(s Signal) []string {
	rt, _, cc := seqCodes(s)
	if rt >= 0 && cc >= 0 {
		if _, ok := Piggyback(uint8(rt), uint8(cc)); ok {
			return nil
		}
	}
	var notes []string
	optionalRT := map[uint8]bool{CodeRTChanAssign: true, CodeRTDisconnect: true}
	optionalCC := map[uint8]bool{CodeCCSetup: true, CodeCCDisconnect: true}
	if rt >= 0 && !optionalRT[uint8(rt)] {
		var want []string
		for _, c := range Combinations {
			if c.RT == uint8(rt) {
				want = append(want, fmt.Sprintf("%s（(%d)）", CodeName("CC", int(c.CC)), c.No))
			}
		}
		if len(want) > 0 {
			have := "CC 無し"
			if cc >= 0 {
				have = "CC は " + CodeName("CC", cc)
			}
			notes = append(notes, fmt.Sprintf("%s（RT）には %s の相乗りが必須だが%s（§4.3.3.2）",
				CodeName("RT", rt), strings.Join(want, " か "), have))
		}
	}
	if cc >= 0 && !optionalCC[uint8(cc)] {
		var want []string
		for _, c := range Combinations {
			if c.CC == uint8(cc) {
				want = append(want, fmt.Sprintf("%s（(%d)）", CodeName("RT", int(c.RT)), c.No))
			}
		}
		if len(want) > 0 {
			have := "RT 無し"
			if rt >= 0 {
				have = "RT は " + CodeName("RT", rt)
			}
			notes = append(notes, fmt.Sprintf("%s（CC）には %s の相乗りが必須だが%s（§4.3.3.2）",
				CodeName("CC", cc), strings.Join(want, " か "), have))
		}
	}
	return notes
}

func seqCodes(s Signal) (rt, mm, cc int) {
	rt, mm, cc = SeqNone, SeqNone, SeqNone
	for _, m := range s.Messages {
		switch m.Entity {
		case "RT":
			if s.HasRT {
				rt = int(m.Type)
			}
		case "MM":
			if s.HasMM {
				mm = int(m.Type)
			}
		case "CC":
			if s.HasCC {
				cc = int(m.Type)
			}
		}
	}
	return
}


type CommKind string

const (
	KindSimplex         CommKind = "単信個別通信"
	KindGroupSimplex    CommKind = "グループ通信［単信］"
	KindGroupHalfDuplex CommKind = "グループ通信［半複信］"
	KindBroadcast       CommKind = "同報通信［単向］"
	KindDuplexMM        CommKind = "複信個別通信（移動局～移動局）"
	KindDuplexBM        CommKind = "複信個別通信（基地局～移動局）"
	KindLocation        CommKind = "位置登録"
	KindBroadcastInfo   CommKind = "報知情報"
)

var CommKinds = []CommKind{KindSimplex, KindGroupSimplex, KindGroupHalfDuplex, KindBroadcast, KindDuplexMM, KindDuplexBM, KindLocation, KindBroadcastInfo}

func (k CommKind) IsCall() bool { return k != KindLocation && k != KindBroadcastInfo }

const (
	SeqDown = 1
	SeqUp   = 2
)

type SeqStep struct {
	RT, MM, CC int
	Dir        int
	Ch         string
	Frame      string
	Abnormal   bool
	Repeat     bool
	Note       string
}

func (s SeqStep) Text() string {
	part := func(e string, c int) string {
		if c == SeqNone {
			return "－"
		}
		return CodeName(e, c)
	}
	d := map[int]string{SeqDown: "下り", SeqUp: "上り"}[s.Dir]
	t := fmt.Sprintf("%s(%s, %s, %s) %s %s", s.Frame, part("RT", s.RT), part("MM", s.MM), part("CC", s.CC), d, s.Ch)
	if s.Abnormal {
		t += " ［準正常］"
	}
	if s.Note != "" {
		t += " ※" + s.Note
	}
	return t
}

type Sequence struct {
	ID     string
	Figure string
	Kinds  []CommKind
	Phase  string
	Title  string
	Steps  []SeqStep
	Timers []string
	Variant string
	Note    string
}

func seqStep(dir int, ch, frame string, rt, mm, cc int) SeqStep {
	return SeqStep{RT: rt, MM: mm, CC: cc, Dir: dir, Ch: ch, Frame: frame}
}

func (s SeqStep) abn(note string) SeqStep { s.Abnormal = true; s.Note = note; return s }
func (s SeqStep) rep() SeqStep            { s.Repeat = true; return s }

const (
	sqOrig    = int(CodeRTOrigReport)
	sqPage    = int(CodeRTPaging)
	sqTerm    = int(CodeRTTermReport)
	sqMSRel   = int(CodeRTMSRelease)
	sqAssign  = int(CodeRTChanAssign)
	sqSys     = int(CodeRTSysInfo)
	sqSysAck  = int(CodeRTSysInfoAck)
	sqFback   = int(CodeRTFallbackReq)
	sqRecTx   = int(CodeRTRecallTx)
	sqRecRx   = int(CodeRTRecallRx)
	sqDisc    = int(CodeRTDisconnect)
	sqDiscAck = int(CodeRTDisconnectAck)
	sqBcast   = int(CodeRTBroadcast)
	sqBCCH    = int(CodeRTBCCHRecv)
	sqTxRel   = int(CodeRTTxRelease)
	sqTxStop  = int(CodeRTTxStopReq)

	sqAlert   = int(CodeCCAlert)
	sqProc    = int(CodeCCCallProc)
	sqSetup   = int(CodeCCSetup)
	sqConn    = int(CodeCCConnect)
	sqConnAck = int(CodeCCConnectAck)
	sqRecProc = int(CodeCCRecallProc)
	sqMode    = int(CodeCCCommMode)
	sqPoss    = int(CodeCCCommPossible)
	sqRecRej  = int(CodeCCRecallReject)
	sqSSetup  = int(CodeCCSimplexSetup)
	sqSRecall = int(CodeCCSimplexRecall)
	sqDSetup  = int(CodeCCDuplexSetup)
	sqDRecall = int(CodeCCDuplexRecall)
	sqCDisc   = int(CodeCCDisconnect)
	sqRel     = int(CodeCCRelease)
	sqRelComp = int(CodeCCReleaseComp)

	sqN = SeqNone
)

var (
	sqStepMSDisc   = seqStep(SeqUp, "USC", "UI", sqN, sqN, sqCDisc)
	sqStepDiscDisc = seqStep(SeqDown, "USC", "UI", sqDisc, sqN, sqCDisc).rep()
	sqStepTxRel    = seqStep(SeqUp, "USC", "UI", sqTxRel, sqN, sqN)
	sqStepTxStop   = seqStep(SeqDown, "USC", "UI", sqTxStop, sqN, sqN)
	sqStepRelNG    = seqStep(SeqDown, "CAC", "I", sqMSRel, sqN, sqRelComp)
	sqStepRelUI    = seqStep(SeqDown, "CAC", "UI", sqMSRel, sqN, sqRelComp).rep()
	sqStepProc     = seqStep(SeqDown, "CAC", "I", sqN, sqN, sqProc)
	sqStepPage     = seqStep(SeqDown, "CAC", "UI", sqPage, sqN, sqMode)
	sqStepTerm     = seqStep(SeqUp, "CAC", "UI", sqTerm, sqN, sqPoss)
	sqStepAssignCS = seqStep(SeqDown, "CAC", "UI", sqAssign, sqN, sqSetup).rep()
	sqStepAssign   = seqStep(SeqDown, "CAC", "UI", sqAssign, sqN, sqN)
	sqStepDDisc    = seqStep(SeqDown, "USC", "UI", sqDisc, sqN, sqN)
	sqStepDDiscAck = seqStep(SeqUp, "USC", "UI", sqDiscAck, sqN, sqN)
	sqStepRecProc  = seqStep(SeqDown, "CAC", "I", sqN, sqN, sqRecProc)
	sqStepFback    = seqStep(SeqDown, "CAC", "I", sqFback, sqN, sqRecRej)
	sqStepCC       = func(dir int, cc int) SeqStep { return seqStep(dir, "USC", "I", sqN, sqN, cc) }
)

func sqSimplexRelease(id, fig string, kinds []CommKind, prefix string) []Sequence {
	return []Sequence{
		{ID: id + "(1)", Figure: fig + "-1", Kinds: kinds, Phase: "通信リンク解放フェーズ", Title: prefix + "・通信リンク解放フェーズ／切断要求",
			Steps: []SeqStep{sqStepMSDisc, sqStepDiscDisc}, Timers: []string{"T307", "TR324"}},
		{ID: id + "(2)", Figure: fig + "-2", Kinds: kinds, Phase: "通信リンク解放フェーズ", Title: prefix + "・通信リンク解放フェーズ／通信時限による切断",
			Steps: []SeqStep{sqStepDiscDisc}, Timers: []string{"TR327", "TR324"}},
		{ID: id + "(3)", Figure: fig + "-3", Kinds: kinds, Phase: "通信リンク解放フェーズ", Title: prefix + "・通信リンク解放フェーズ／無通信による切断",
			Steps: []SeqStep{sqStepTxRel, sqStepDiscDisc}, Timers: []string{"TR325", "TR324"}, Note: "基地局は移動局のキャリア無しを検出する（無通信検出タイマはレイヤ 1）"},
	}
}

func sqDuplexRelease(id, fig string, kind CommKind, mm bool) []Sequence {
	k := []CommKind{kind}
	var s1, s2 []SeqStep
	if mm {
		s1 = []SeqStep{sqStepCC(SeqUp, sqCDisc), sqStepCC(SeqDown, sqCDisc), sqStepCC(SeqDown, sqRel), sqStepCC(SeqUp, sqRel),
			sqStepCC(SeqUp, sqRelComp), sqStepCC(SeqDown, sqRelComp), sqStepDDisc, sqStepDDiscAck}
		s2 = []SeqStep{sqStepCC(SeqDown, sqCDisc), sqStepCC(SeqUp, sqRel), sqStepCC(SeqDown, sqRelComp), sqStepDDisc, sqStepDDiscAck}
	} else {
		s1 = []SeqStep{sqStepCC(SeqUp, sqCDisc), sqStepCC(SeqDown, sqRel), sqStepCC(SeqUp, sqRelComp), sqStepDDisc, sqStepDDiscAck}
		s2 = []SeqStep{sqStepCC(SeqDown, sqCDisc), sqStepCC(SeqUp, sqRel), sqStepCC(SeqDown, sqRelComp), sqStepDDisc, sqStepDDiscAck}
	}
	t2 := "通信リンク解放フェーズ／通信時限満了時による切断"
	if !mm {
		t2 = "通信リンク解放フェーズ／基地局側切断"
	}
	return []Sequence{
		{ID: id + "(1)", Figure: fig + "-1", Kinds: k, Phase: "通信リンク解放フェーズ", Title: "複信個別通信・通信リンク解放フェーズ／切断要求",
			Steps: s1, Timers: []string{"T390", "TR304", "TR317"}},
		{ID: id + "(2)", Figure: fig + "-2", Kinds: k, Phase: "通信リンク解放フェーズ", Title: "複信個別通信・" + t2,
			Steps: s2, Timers: []string{"TR327", "T390", "TR304", "TR317"}},
		{ID: id + "(3)", Figure: fig + "-3", Kinds: k, Phase: "通信リンク解放フェーズ", Title: "複信個別通信・通信リンク解放フェーズ／電波断による切断",
			Steps: []SeqStep{sqStepDDisc, sqStepDDiscAck}, Note: "基地局側は無線チャネル切断確認が受信できない場合を前提として切断処理を行う"},
	}
}

var (
	sqGroupKinds  = []CommKind{KindGroupSimplex, KindGroupHalfDuplex}
	sqSimplexLike = []CommKind{KindSimplex, KindGroupSimplex, KindGroupHalfDuplex}
	sqDuplexKinds = []CommKind{KindDuplexMM, KindDuplexBM}
)

var sequences = func() []Sequence {
	var q []Sequence
	add := func(s ...Sequence) { q = append(q, s...) }

	add(Sequence{ID: "4.3.8.1.1", Figure: "図 4.3.8.1.1", Kinds: []CommKind{KindSimplex}, Phase: "通信リンク確立フェーズ",
		Title: "単信個別通信・通信リンク確立フェーズ",
		Steps: []SeqStep{
			seqStep(SeqUp, "CAC", "I", sqOrig, sqN, sqSSetup),
			sqStepRelNG.abn("呼設定情報 NG の場合等"),
			sqStepProc,
			sqStepPage,
			sqStepTerm,
			sqStepRelUI.abn("中継回線無し・通信形態に応じられない・無線回線無しの場合等"),
			sqStepAssignCS,
		},
		Timers: []string{"TR305", "TR301", "TR315", "TR313", "TR306", "TR323", "TR314"}})
	add(Sequence{ID: "4.3.8.1.2", Figure: "図 4.3.8.1.2", Kinds: []CommKind{KindSimplex}, Phase: "通信フェーズ",
		Title: "単信個別通信・通信フェーズ", Steps: []SeqStep{sqStepTxRel}, Timers: []string{"TR325"},
		Note: "送信権の獲得（SB2〜SB4・B/I）はレイヤ 1"})
	add(sqSimplexRelease("4.3.8.1.3", "図 4.3.8.1.3", []CommKind{KindSimplex}, "単信個別通信")...)
	add(Sequence{ID: "4.3.8.1.4", Figure: "図 4.3.8.1.4", Kinds: []CommKind{KindSimplex}, Phase: "送信権制御",
		Title: "単信個別通信・送信権制御", Steps: []SeqStep{sqStepTxStop}, Timers: []string{"TR331"},
		Note: "異なる基地局で送信権要求が衝突した場合。「送信停止要求」はオプション"})

	groupMS := []SeqStep{
		seqStep(SeqUp, "CAC", "I", sqOrig, sqN, sqSSetup),
		sqStepRelNG.abn("呼設定情報 NG の場合等"),
		sqStepProc,
		sqStepRelUI.abn("中継回線無し・無線回線無しの場合等"),
		sqStepPage.rep(),
		sqStepAssignCS,
	}
	add(Sequence{ID: "4.3.8.2.1", Figure: "図 4.3.8.2.1", Kinds: []CommKind{KindGroupSimplex}, Phase: "通信リンク確立フェーズ",
		Title: "グループ通信［単信］・通信リンク確立フェーズ", Steps: groupMS,
		Timers: []string{"TR305", "TR313", "TR326", "TR306", "TR323", "TR314"}})
	add(Sequence{ID: "4.3.8.2.2", Figure: "図 4.3.8.2.2", Kinds: []CommKind{KindGroupSimplex}, Phase: "通信フェーズ",
		Title: "グループ通信［単信］・通信フェーズ", Steps: []SeqStep{sqStepTxRel}, Timers: []string{"TR325"}})
	add(sqSimplexRelease("4.3.8.2.3", "図 4.3.8.2.3", []CommKind{KindGroupSimplex}, "グループ通信［単信］")...)
	add(Sequence{ID: "4.3.8.2.4", Figure: "図 4.3.8.2.4", Kinds: []CommKind{KindGroupSimplex}, Phase: "送信権制御",
		Title: "グループ通信［単信］・送信権制御", Steps: []SeqStep{sqStepTxStop}, Timers: []string{"TR331"}})

	add(Sequence{ID: "4.3.8.3.1", Variant: "移動局側発信", Figure: "図 4.3.8.3.1", Kinds: []CommKind{KindGroupHalfDuplex}, Phase: "通信リンク確立フェーズ（移動局側発信）",
		Title: "グループ通信［半複信］・通信リンク確立フェーズ／移動局発信", Steps: groupMS,
		Timers: []string{"TR305", "TR313", "TR326", "TR323", "TR314"}})
	add(Sequence{ID: "4.3.8.3.2", Variant: "基地局側発信", Figure: "図 4.3.8.3.2", Kinds: []CommKind{KindGroupHalfDuplex}, Phase: "通信リンク確立フェーズ（基地局側発信）",
		Title:  "グループ通信［半複信］・通信リンク確立フェーズ／基地局発信",
		Steps:  []SeqStep{sqStepPage.rep(), sqStepRelUI.abn("中継回線無し・無線回線無しの場合等"), sqStepAssignCS},
		Timers: []string{"TR326", "TR306", "TR313", "TR323", "TR314"}})
	add(Sequence{ID: "4.3.8.3.3", Figure: "図 4.3.8.3.3", Kinds: []CommKind{KindGroupHalfDuplex}, Phase: "通信フェーズ",
		Title: "グループ通信［半複信］・通信フェーズ", Steps: []SeqStep{sqStepTxRel}, Timers: []string{"TR325"}})
	add(Sequence{ID: "4.3.8.3.4(1)", Figure: "図 4.3.8.3.4-1", Kinds: []CommKind{KindGroupHalfDuplex}, Phase: "通信リンク解放フェーズ",
		Title: "グループ通信・通信リンク解放フェーズ／移動局側切断時", Steps: []SeqStep{sqStepMSDisc, sqStepDiscDisc}, Timers: []string{"T307", "TR324"}})
	add(Sequence{ID: "4.3.8.3.4(2)", Figure: "図 4.3.8.3.4-2", Kinds: []CommKind{KindGroupHalfDuplex}, Phase: "通信リンク解放フェーズ",
		Title: "グループ通信・通信リンク解放フェーズ／基地局側切断時", Steps: []SeqStep{sqStepDiscDisc}, Timers: []string{"TR324"}})
	add(Sequence{ID: "4.3.8.3.4(3)", Figure: "図 4.3.8.3.4-3", Kinds: []CommKind{KindGroupHalfDuplex}, Phase: "通信リンク解放フェーズ",
		Title: "グループ通信・通信リンク解放フェーズ／通信時限満了時", Steps: []SeqStep{sqStepDiscDisc}, Timers: []string{"TR327", "TR324"}})
	add(Sequence{ID: "4.3.8.3.5", Figure: "図 4.3.8.3.5", Kinds: []CommKind{KindGroupHalfDuplex}, Phase: "送信権制御",
		Title: "グループ通信［半複信］・送信権制御", Steps: []SeqStep{sqStepTxStop}, Timers: []string{"TR331"}})

	add(Sequence{ID: "4.3.8.4.1", Figure: "図 4.3.8.4.1", Kinds: []CommKind{KindBroadcast}, Phase: "通信リンク確立フェーズ",
		Title:  "同報通信・制御シーケンス／単向通信",
		Steps:  []SeqStep{sqStepPage.rep(), sqStepRelUI.abn("中継回線無し・無線回線無しの場合等"), sqStepAssignCS},
		Timers: []string{"TR326", "TR313", "TR306", "TR323", "TR314"}})
	add(Sequence{ID: "4.3.8.4.2(1)", Figure: "図 4.3.8.4.2-1", Kinds: []CommKind{KindBroadcast}, Phase: "通信リンク解放フェーズ",
		Title: "単向方式・同報通信・通信リンク解放フェーズ／切断操作", Steps: []SeqStep{sqStepDiscDisc}, Timers: []string{"TR324"}})
	add(Sequence{ID: "4.3.8.4.2(2)", Figure: "図 4.3.8.4.2-2", Kinds: []CommKind{KindBroadcast}, Phase: "通信リンク解放フェーズ",
		Title: "単向方式・同報通信・通信リンク解放フェーズ／通信時限による切断", Steps: []SeqStep{sqStepDiscDisc}, Timers: []string{"TR327", "TR324"}})

	add(Sequence{ID: "4.3.8.5.1", Figure: "図 4.3.8.5.1", Kinds: []CommKind{KindDuplexMM}, Phase: "通信リンク確立フェーズ",
		Title: "複信個別通信・通信リンク確立フェーズ",
		Steps: []SeqStep{
			seqStep(SeqUp, "CAC", "I", sqOrig, sqN, sqDSetup),
			sqStepRelNG.abn("呼設定情報 NG の場合等"),
			sqStepProc,
			sqStepPage,
			sqStepTerm,
			seqStep(SeqDown, "CAC", "I", sqN, sqN, sqCDisc).abn("中継回線無し・通信形態に応じられない場合等"),
			seqStep(SeqUp, "CAC", "I", sqN, sqN, sqRel).abn("中継回線無し・通信形態に応じられない場合等"),
			sqStepRelNG.abn("中継回線無し・通信形態に応じられない場合等"),
			sqStepRelUI.abn("中継回線無し・無線回線無しの場合等"),
			sqStepAssign,
			sqStepCC(SeqDown, sqSetup),
			sqStepCC(SeqUp, sqAlert),
			sqStepCC(SeqDown, sqAlert),
			sqStepCC(SeqUp, sqConn),
			sqStepCC(SeqDown, sqConn),
			sqStepCC(SeqUp, sqConnAck),
			sqStepCC(SeqDown, sqConnAck),
		},
		Timers: []string{"TR305", "TR301", "TR315", "TR306", "TR303", "TR314"},
		Note:   "発移動局側・着移動局側のタイミング（無線チャネル指定の順序等）は規定しない（注 2）"})
	add(sqDuplexRelease("4.3.8.5.2", "図 4.3.8.5.2", KindDuplexMM, true)...)

	add(Sequence{ID: "4.3.8.6.1", Variant: "移動局側発信", Figure: "図 4.3.8.6.1", Kinds: []CommKind{KindDuplexBM}, Phase: "通信リンク確立フェーズ（移動局側発信）",
		Title: "複信個別通信・通信リンク確立フェーズ／移動局発信",
		Steps: []SeqStep{
			seqStep(SeqUp, "CAC", "I", sqOrig, sqN, sqDSetup),
			sqStepRelNG.abn("呼設定情報 NG の場合等"),
			sqStepProc,
			seqStep(SeqDown, "CAC", "I", sqN, sqN, sqCDisc).abn("中継回線無し・通信形態に応じられない場合等"),
			seqStep(SeqUp, "CAC", "I", sqN, sqN, sqRel).abn("中継回線無し・通信形態に応じられない場合等"),
			sqStepRelNG.abn("中継回線無し・通信形態に応じられない場合等"),
			seqStep(SeqDown, "CAC", "UI", sqMSRel, sqN, sqRelComp).abn("無線回線無しの場合等"),
			sqStepAssign,
			sqStepCC(SeqDown, sqAlert),
			sqStepCC(SeqDown, sqConn),
			sqStepCC(SeqUp, sqConnAck),
		},
		Timers: []string{"TR305", "TR303", "TR314"}})
	add(Sequence{ID: "4.3.8.6.2", Variant: "基地局側発信", Figure: "図 4.3.8.6.2", Kinds: []CommKind{KindDuplexBM}, Phase: "通信リンク確立フェーズ（基地局側発信）",
		Title: "複信個別通信・通信リンク確立フェーズ／基地局側発信",
		Steps: []SeqStep{
			sqStepPage,
			sqStepTerm,
			seqStep(SeqDown, "CAC", "UI", sqMSRel, sqN, sqRelComp).abn("中継回線無し・無線回線無しの場合等"),
			sqStepAssign,
			sqStepCC(SeqDown, sqSetup),
			sqStepCC(SeqUp, sqAlert),
			sqStepCC(SeqUp, sqConn),
			sqStepCC(SeqDown, sqConnAck),
		},
		Timers: []string{"TR301", "TR315", "TR306", "TR303", "TR314"}})
	add(sqDuplexRelease("4.3.8.6.3", "図 4.3.8.6.3", KindDuplexBM, false)...)

	switchSteps := func(rt, cc int) []SeqStep {
		return []SeqStep{
			seqStep(SeqUp, "CAC", "I", rt, sqN, cc),
			sqStepFback.abn("再呼設定情報 NG・無線回線無しの場合等"),
			sqStepRecProc,
			sqStepAssign.rep(),
		}
	}
	sw := []struct {
		id      string
		kinds   []CommKind
		phase   string
		title   string
		rt, cc  int
		timers  []string
		fbackOf string
	}{
		{"4.3.8.7.1", sqSimplexLike, "チャネル切替（送信中）", "単信個別通信、グループ通信チャネル切替（再発呼型）／送信中", sqRecTx, sqSRecall, []string{"TR329", "TR323"}, ""},
		{"4.3.8.7.2", sqSimplexLike, "チャネル切替（受信中）", "単信個別通信、グループ通信チャネル切替（再発呼型）／受信中", sqRecRx, sqSRecall, []string{"TR329", "TR323"}, ""},
		{"4.3.8.7.3", sqDuplexKinds, "チャネル切替", "複信個別通信チャネル切替（再発呼型）", sqRecTx, sqDRecall, []string{"TR329", "TR303"}, ""},
		{"4.3.8.7.4", []CommKind{KindBroadcast}, "チャネル切替", "同報通信チャネル切替（再発呼型）", sqRecRx, sqSRecall, []string{"TR329", "TR323"}, ""},
		{"4.3.8.7.5", sqSimplexLike, "切り戻し（送信中）", "単信個別通信、グループ通信切り戻し（送信中）", sqRecTx, sqSRecall, []string{"TR329", "TR323", "TR332"}, "4.3.8.7.1"},
		{"4.3.8.7.6", sqSimplexLike, "切り戻し（受信中）", "単信個別通信、グループ通信切り戻し（受信中）", sqRecRx, sqSRecall, []string{"TR329", "TR323", "TR332"}, "4.3.8.7.2"},
		{"4.3.8.7.7", sqDuplexKinds, "切り戻し", "複信個別通信切り戻し", sqRecTx, sqDRecall, []string{"TR329", "TR303", "TR332"}, "4.3.8.7.3"},
		{"4.3.8.7.8", []CommKind{KindBroadcast}, "切り戻し", "同報通信切り戻し", sqRecRx, sqSRecall, []string{"TR329", "TR323", "TR332"}, "4.3.8.7.4"},
	}
	for _, s := range sw {
		steps := switchSteps(s.rt, s.cc)
		if s.cc == sqDRecall {
			steps[3] = sqStepAssign
		}
		note := ""
		if s.fbackOf != "" {
			note = "レイヤ 3 の段は §" + s.fbackOf + " と同じ。切り戻しは移動先の SB1・TCH（複信は SB3）の未受信で起こる"
		}
		add(Sequence{ID: s.id, Figure: "図 " + s.id, Kinds: s.kinds, Phase: s.phase, Title: s.title, Steps: steps, Timers: s.timers, Note: note})
	}

	add(Sequence{ID: "4.3.8.8", Figure: "図 4.3.8.8", Kinds: []CommKind{KindLocation}, Phase: "位置登録",
		Title: "位置登録シーケンス",
		Steps: []SeqStep{
			seqStep(SeqUp, "CAC", "UI", sqN, int(CodeMMRegRequest), sqN),
			seqStep(SeqDown, "CAC", "UI", sqN, int(CodeMMRegAck), sqN),
			seqStep(SeqDown, "CAC", "UI", sqN, int(CodeMMRegReject), sqN).abn("位置登録失敗等"),
		},
		Timers: []string{"TM301"}})

	add(Sequence{ID: "4.3.8.9.1(1)", Figure: "図 4.3.8.9.1-1", Kinds: sqDuplexKinds, Phase: "通信フェーズ（システム情報指定）",
		Title:  "システム情報指定シーケンス（複信）",
		Steps:  []SeqStep{seqStep(SeqDown, "USC", "UI", sqSys, sqN, sqN), seqStep(SeqUp, "USC", "UI", sqSysAck, sqN, sqN)},
		Timers: []string{"TR307", "TR318"}})
	add(Sequence{ID: "4.3.8.9.1(2)", Figure: "図 4.3.8.9.1-2", Kinds: []CommKind{KindSimplex, KindGroupSimplex, KindGroupHalfDuplex, KindBroadcast},
		Phase: "通信フェーズ（システム情報指定）", Title: "システム情報指定シーケンス（単信個別、グループ、同報）",
		Steps:  []SeqStep{seqStep(SeqDown, "USC", "UI", sqSys, sqN, sqN).rep()},
		Timers: []string{"TR328"}})

	add(Sequence{ID: "4.3.8.10.1", Figure: "図 4.3.8.10.1", Kinds: []CommKind{KindBroadcastInfo}, Phase: "報知情報",
		Title: "報知情報シーケンス", Steps: []SeqStep{seqStep(SeqDown, "CAC", "UI", sqBcast, sqN, sqN)},
		Note: "基地局側から BCCH で 1 スーパーフレームごとに 1 回報告"})
	add(Sequence{ID: "4.3.8.10.2", Figure: "図 4.3.8.10.2", Kinds: []CommKind{KindBroadcastInfo}, Phase: "BCCH 受信",
		Title: "BCCH 受信シーケンス", Steps: []SeqStep{seqStep(SeqDown, "CAC", "UI", sqBCCH, sqN, sqN)},
		Note: "基地局が移動局に BCCH 受信を指示する場合に PCH で送る"})
	return q
}()

func Sequences() []Sequence {
	out := make([]Sequence, len(sequences))
	copy(out, sequences)
	return out
}

func SequenceByID(id string) (Sequence, bool) {
	for _, s := range sequences {
		if s.ID == id {
			return s, true
		}
	}
	return Sequence{}, false
}


type Obs struct {
	RT, MM, CC int
	Dir        int
	Ch         string
	T          float64
	Whole bool
	Hint CommKind
}

func ObsFromSignal(s Signal, dir int, ch string, t float64) Obs {
	rt, mm, cc := seqCodes(s)
	return Obs{RT: rt, MM: mm, CC: cc, Dir: dir, Ch: ch, T: t, Whole: true}
}

func ObsFromMessage(entity string, typ uint8, dir int, ch string, t float64) Obs {
	o := Obs{RT: SeqNone, MM: SeqNone, CC: SeqNone, Dir: dir, Ch: ch, T: t}
	switch entity {
	case "RT":
		o.RT = int(typ)
	case "MM":
		o.MM = int(typ)
	case "CC":
		o.CC = int(typ)
	}
	return o
}

func (o Obs) Text() string {
	part := func(e string, c int) string {
		if c == SeqNone {
			return "－"
		}
		return CodeName(e, c)
	}
	t := fmt.Sprintf("(%s, %s, %s)", part("RT", o.RT), part("MM", o.MM), part("CC", o.CC))
	if d, ok := map[int]string{SeqDown: "下り", SeqUp: "上り"}[o.Dir]; ok {
		t += " " + d
	}
	return t
}

type obsKey struct {
	rt, mm, cc, dir int
	whole           bool
}

func (o Obs) key() obsKey { return obsKey{o.RT, o.MM, o.CC, o.Dir, o.Whole} }

func (o Obs) matches(s SeqStep) (hit, exact bool) {
	if o.Dir != 0 && o.Dir != s.Dir {
		return false, false
	}
	if o.Whole {
		ok := o.RT == s.RT && o.MM == s.MM && o.CC == s.CC
		return ok, ok
	}
	part := func(ov, sv int) bool { return ov == SeqNone || ov == sv }
	if o.RT == SeqNone && o.MM == SeqNone && o.CC == SeqNone {
		return false, false
	}
	return part(o.RT, s.RT) && part(o.MM, s.MM) && part(o.CC, s.CC), false
}

type Guess struct {
	Kind       CommKind
	Candidates []CommKind
	Phase      string
	Sequence   string
	Alt        []string
	Score      int
	Confidence string
	Text       string
	Why        string
}

type Tracker struct {
	MaxGap float64

	obs   []Obs
	seen  map[obsKey]int
	order []obsKey
	hint  CommKind
	last  float64
}

func NewTracker() *Tracker { return &Tracker{} }

func (t *Tracker) Reset() {
	t.obs, t.seen, t.order, t.hint, t.last = nil, nil, nil, "", 0
}

func (t *Tracker) Add(o Obs) {
	if o.RT == SeqNone && o.MM == SeqNone && o.CC == SeqNone {
		return
	}
	if t.MaxGap > 0 && len(t.obs) > 0 && o.T-t.last > t.MaxGap {
		t.Reset()
	}
	if t.seen == nil {
		t.seen = map[obsKey]int{}
	}
	k := o.key()
	if _, ok := t.seen[k]; !ok {
		t.order = append(t.order, k)
	}
	t.obs = append(t.obs, o)
	t.seen[k] = len(t.obs) - 1
	t.last = o.T
	if o.Hint != "" {
		t.hint = o.Hint
	}
}

func (t *Tracker) AddSignal(s Signal, dir int, ch string, at float64) {
	t.Add(ObsFromSignal(s, dir, ch, at))
}

func (t *Tracker) AddMessage(entity string, typ uint8, dir int, ch string, at float64) {
	t.Add(ObsFromMessage(entity, typ, dir, ch, at))
}

func (t *Tracker) SetHint(k CommKind) { t.hint = k }

func (t *Tracker) distinct() []Obs {
	out := make([]Obs, 0, len(t.order))
	for _, k := range t.order {
		out = append(out, t.obs[t.seen[k]])
	}
	return out
}

func seqKindsOf(o Obs) map[CommKind]bool {
	out := map[CommKind]bool{}
	for _, s := range sequences {
		for _, st := range s.Steps {
			if hit, exact := o.matches(st); hit {
				for _, k := range s.Kinds {
					out[k] = out[k] || exact
				}
			}
		}
	}
	return out
}

type seqScore struct {
	score    int
	num, den int
	variant  string
	unique   []string
}

func (a seqScore) better(b seqScore) bool {
	if a.score != b.score {
		return a.score > b.score
	}
	return a.num*max(b.den, 1) > b.num*max(a.den, 1)
}

func (a seqScore) same(b seqScore) bool {
	return a.score == b.score && a.num*max(b.den, 1) == b.num*max(a.den, 1)
}

func seqOfVariant(k CommKind, v string) []Sequence {
	var out []Sequence
	for _, s := range sequences {
		if seqHasKind(s, k) && (s.Variant == "" || s.Variant == v) {
			out = append(out, s)
		}
	}
	return out
}

func seqVariants(k CommKind) []string {
	var vs []string
	seen := map[string]bool{}
	for _, s := range sequences {
		if seqHasKind(s, k) && s.Variant != "" && !seen[s.Variant] {
			seen[s.Variant] = true
			vs = append(vs, s.Variant)
		}
	}
	if len(vs) == 0 {
		return []string{""}
	}
	return vs
}

func (t *Tracker) Guess() Guess {
	obs := t.distinct()
	if len(obs) == 0 {
		return Guess{Confidence: "なし", Text: "（信号なし）", Why: "信号を見ていない"}
	}
	type ev struct {
		o     Obs
		kinds map[CommKind]bool
		call  bool
	}
	var evs []ev
	var stray []string
	anyCall, anyUp := false, false
	for _, o := range obs {
		ks := seqKindsOf(o)
		if len(ks) == 0 {
			stray = append(stray, o.Text())
			continue
		}
		call := false
		for k := range ks {
			if k.IsCall() {
				call = true
			}
		}
		anyCall = anyCall || call
		anyUp = anyUp || o.Dir == SeqUp || o.Dir == 0
		evs = append(evs, ev{o, ks, call})
	}
	if len(evs) == 0 {
		return Guess{Confidence: "なし", Text: "（シーケンス図に無い信号だけ）", Why: "シーケンス図に無い: " + strings.Join(stray, "、")}
	}
	scores := map[CommKind]seqScore{}
	for _, k := range CommKinds {
		if anyCall != k.IsCall() || (t.hint != "" && k != t.hint) {
			continue
		}
		var bestV seqScore
		for i, v := range seqVariants(k) {
			seqs := seqOfVariant(k, v)
			sc := seqScore{variant: v}
			for _, e := range evs {
				if anyCall && !e.call {
					continue
				}
				hit, exact := false, false
				for _, s := range seqs {
					for _, st := range s.Steps {
						if h, x := e.o.matches(st); h {
							hit, exact = true, exact || x
						}
					}
				}
				switch {
				case hit && exact:
					sc.score += 2
				case hit:
					sc.score++
				default:
					sc.score -= 2
				}
				if hit && len(e.kinds) == 1 {
					sc.unique = append(sc.unique, e.o.Text())
				}
			}
			for _, s := range seqs {
				num, den := 0, 0
				for _, st := range s.Steps {
					if st.Abnormal || (!anyUp && st.Dir == SeqUp) {
						continue
					}
					den++
					for _, e := range evs {
						if h, _ := e.o.matches(st); h {
							num++
							break
						}
					}
				}
				if num >= 2 || (num > 0 && num == den) {
					sc.num += num
					sc.den += den
				}
			}
			if i == 0 || sc.better(bestV) {
				bestV = sc
			}
		}
		scores[k] = bestV
	}
	var top seqScore
	var cands []CommKind
	for _, k := range CommKinds {
		sc, ok := scores[k]
		if !ok {
			continue
		}
		switch {
		case len(cands) == 0 || sc.better(top):
			top, cands = sc, []CommKind{k}
		case sc.same(top):
			cands = append(cands, k)
		}
	}
	if len(cands) == 0 {
		return Guess{Confidence: "なし", Text: "（候補なし）", Why: "ヒント " + string(t.hint) + " に当てはまらない"}
	}
	g := Guess{Kind: cands[0], Candidates: cands, Score: top.score}
	var second seqScore
	secondKind := CommKind("")
	for _, k := range CommKinds {
		sc, ok := scores[k]
		if !ok || sc.same(top) {
			continue
		}
		if secondKind == "" || sc.better(second) {
			second, secondKind = sc, k
		}
	}
	switch {
	case len(cands) > 1:
		g.Confidence = "低"
	case len(top.unique) > 0 || secondKind == "" || top.score-second.score >= 4:
		g.Confidence = "高"
	default:
		g.Confidence = "中"
	}

	seqs := seqOfVariant(g.Kind, top.variant)
	var lastObs *Obs
	for i := len(t.obs) - 1; i >= 0 && lastObs == nil; i-- {
		for _, s := range seqs {
			if seqContains(s, t.obs[i]) {
				lastObs = &t.obs[i]
				break
			}
		}
	}
	type cand struct {
		s   Sequence
		hit int
	}
	var phases []cand
	if lastObs != nil {
		for _, s := range seqs {
			if !seqContains(s, *lastObs) {
				continue
			}
			hit := 0
			for _, o := range obs {
				if seqContains(s, o) {
					hit++
				}
			}
			phases = append(phases, cand{s, hit})
		}
	}
	sort.SliceStable(phases, func(i, j int) bool { return phases[i].hit > phases[j].hit })
	if len(phases) > 0 {
		g.Phase = phases[0].s.Phase
		g.Sequence = phases[0].s.ID
		for _, p := range phases[1:] {
			if p.hit == phases[0].hit {
				g.Alt = append(g.Alt, p.s.ID+" "+p.s.Phase)
			}
		}
	}

	var names []string
	for _, k := range cands {
		names = append(names, string(k))
	}
	g.Text = strings.Join(names, " または ")
	if g.Phase != "" && g.Phase != string(g.Kind) {
		g.Text += "・" + g.Phase
	}

	why := []string{fmt.Sprintf("点 %d・網羅率 %d/%d（信号 %d 種）", top.score, top.num, top.den, len(evs))}
	if len(top.unique) > 0 && len(cands) == 1 {
		why = append(why, "決め手: "+strings.Join(top.unique, "、"))
	}
	if len(cands) > 1 {
		why = append(why, "メッセージ種別だけでは見分けられない（CC の通信形態の情報要素などで決まる）")
	}
	if secondKind != "" {
		why = append(why, fmt.Sprintf("次点 %s（点 %d・網羅率 %d/%d）", secondKind, second.score, second.num, second.den))
	}
	if g.Sequence != "" {
		why = append(why, "§"+g.Sequence)
	}
	if len(g.Alt) > 0 {
		why = append(why, "同じ信号を含む別の図: "+strings.Join(g.Alt, "、"))
	}
	if len(stray) > 0 {
		why = append(why, "シーケンス図に無い: "+strings.Join(stray, "、"))
	}
	if t.hint != "" {
		why = append(why, "ヒント: "+string(t.hint))
	}
	g.Why = strings.Join(why, "。")
	return g
}

func seqHasKind(s Sequence, k CommKind) bool {
	for _, x := range s.Kinds {
		if x == k {
			return true
		}
	}
	return false
}

func seqContains(s Sequence, o Obs) bool {
	for _, st := range s.Steps {
		if hit, _ := o.matches(st); hit {
			return true
		}
	}
	return false
}


type TimerRow struct {
	Kind     string
	Default  string
	State    string
	Start    string
	Stop     string
	Timeouts []string
	Note     string
}

type Timer struct {
	Name         string
	Side         string
	Entity       string
	Table        string
	Phase        string
	Rows         []TimerRow
	TimeoutHeads []string
	Missing      bool
	Note         string
}

var (
	tmHeadMS   = []string{"1 回目タイムアウト", "2 回目以降タイムアウト"}
	tmHeadBS   = []string{"1〜(n−1) 回目タイムアウト", "n 回目以降タイムアウト"}
	tmHeadBSCC = []string{"1 回目タイムアウト", "2〜(n−1) 回目タイムアウト", "n 回目タイムアウト"}
)

func tmRow(kind, def, state, start, stop string, to ...string) TimerRow {
	return TimerRow{Kind: kind, Default: def, State: state, Start: start, Stop: stop, Timeouts: to}
}

func (r TimerRow) note(n string) TimerRow { r.Note = n; return r }

func tmKinds(kinds []string, r TimerRow) []TimerRow {
	out := make([]TimerRow, len(kinds))
	for i, k := range kinds {
		out[i] = r
		out[i].Kind = k
	}
	return out
}

func tmJoin(rs ...[]TimerRow) []TimerRow {
	var out []TimerRow
	for _, r := range rs {
		out = append(out, r...)
	}
	return out
}

var (
	tmAll     = []string{"複信", "個別単信", "グループ", "同報"}
	tmSimplex = []string{"個別単信", "グループ", "同報"}
)

var timers = func() []Timer {
	const (
		t1 = "表 4.3.8.11.1"
		t2 = "表 4.3.8.11.2"
		t3 = "表 4.3.8.11.3"
		t4 = "表 4.3.8.12.1"
		t5 = "表 4.3.8.12.2"
	)
	ms := func(name, phase string, rows ...TimerRow) Timer {
		return Timer{Name: name, Side: "移動局", Entity: "RT", Table: t1, Phase: phase, Rows: rows, TimeoutHeads: tmHeadMS}
	}
	bs := func(name, phase string, rows ...TimerRow) Timer {
		return Timer{Name: name, Side: "基地局", Entity: "RT", Table: t2, Phase: phase, Rows: rows, TimeoutHeads: tmHeadBS,
			Note: "基地局側のタイムアウト値・再送回数は任意に設定できる（注 1）"}
	}
	missing := func(name, side, table string) Timer {
		return Timer{Name: name, Side: side, Entity: "RT", Table: table, Missing: true}
	}
	const (
		idle  = "待ち受け状態に遷移"
		noRe  = "タイマ再設定無し"
		anyV  = "任意設定"
		tchUp = "レイヤ 1 へ TCH 起動要求"
		tchOK = "レイヤ 1 から TCH 起動確認・「移動局解放」受信"
	)
	var q []Timer

	q = append(q,
		ms("TR305", "通信リンク確立フェーズ", tmKinds([]string{"複信", "個別単信", "グループ"},
			tmRow("", "10 秒", "発信", "「発信無線状態報告」送信", "「無線チャネル指定」・「移動局解放」受信", idle, noRe))...),
		ms("TR306", "通信リンク確立フェーズ", tmJoin(
			tmKinds([]string{"複信", "個別単信"}, tmRow("", "5 秒", "着信", "「着信無線状態報告」送信", "「無線チャネル指定」・「移動局解放」受信", idle, noRe)),
			tmKinds([]string{"グループ", "同報"}, tmRow("", "5 秒", "着信", "「ページング」受信", "「無線チャネル指定」・「移動局解放」受信", idle, noRe)))...),
		missing("TR311", "移動局", t1),
		ms("TR314", "通信リンク確立フェーズ",
			tmRow("複信", "10〜15 秒", "無線チャネル指定", tchUp, tchOK, idle, noRe),
			tmRow("個別単信", "3 秒", "無線チャネル指定", tchUp, tchOK, idle, noRe),
			tmRow("グループ", "3 秒", "無線チャネル指定", tchUp, tchOK, idle, noRe),
			tmRow("同報", "3 秒", "無線チャネル指定", tchUp, tchOK, idle, noRe)),
		ms("TR325", "通信フェーズ（通信チャネル・制御チャネルで共用）", tmKinds([]string{"個別単信", "グループ"},
			tmRow("", "0.5 秒", "送信権解放", "「送信権解放」送信", "レイヤ 1 から TCH 起動確認（B/I=I）受信", "「送信権解放」再送・TR325 再設定", "アクセス許可状態に遷移"))...),
		ms("TR329", "通信フェーズ チャネル切替",
			tmRow("複信", "2 秒", "チャネル切替切り戻し", "「送信中再発呼」送信", "「無線チャネル指定」受信", "切り戻し状態に遷移", noRe),
			tmRow("個別単信", "2 秒", "チャネル切替切り戻し", "「送信中再発呼」・「受信中再発呼」送信", "「無線チャネル指定」受信", "切り戻し状態に遷移", noRe),
			tmRow("グループ", "2 秒", "チャネル切替切り戻し", "「送信中再発呼」・「受信中再発呼」送信", "「無線チャネル指定」受信", "切り戻し状態に遷移", noRe),
			tmRow("同報", "2 秒", "チャネル切替切り戻し", "「受信中再発呼」送信", "「無線チャネル指定」受信", "切り戻し状態に遷移", noRe)),
		ms("TR332", "通信フェーズ チャネル切替（通信チャネル・制御チャネルで共用）", tmKinds(tmAll,
			tmRow("", anyV, "TCH 起動", "チャネル切替起動", "－", "現状維持", noRe).note("チャネル切替間隔時間。基地局より指定（図 4.3.8.7.5 注 3）"))...),
		ms("TR333", "通信リンク確立フェーズ（制御 CH 通信）", tmKinds(tmAll,
			tmRow("", anyV, "無線チャネル指定", tchUp, tchOK, idle, noRe))...),
		Timer{Name: "TR315", Side: "移動局", Entity: "RT", Table: t1, Phase: "通信リンク確立フェーズ", Rows: tmKinds([]string{"複信", "個別単信"},
			tmRow("", "0.5 秒", "着信", "「ページング」受信", "「着信無線状態報告」返送").note("「ページング」を受信後、TR315 秒以内に「着信無線状態報告」を返送すること"))},
		missing("TR316", "移動局", t1),
		Timer{Name: "TR317", Side: "移動局", Entity: "RT", Table: t1, Phase: "通信リンク解放フェーズ", Rows: []TimerRow{
			tmRow("複信", "0.5 秒", "無線チャネル切断", "「無線チャネル切断」受信", "「無線チャネル切断確認」返送").note("「無線チャネル切断」受信後、TR317 秒以内に「無線チャネル切断確認」を返送すること")}},
		Timer{Name: "TR318", Side: "移動局", Entity: "RT", Table: t1, Phase: "通信フェーズ", Rows: []TimerRow{
			tmRow("複信", "0.5 秒", "システム情報", "「システム情報」受信", "「システム情報確認」返送").note("「システム情報」受信後、TR318 秒以内に「システム情報確認」を返送すること")},
			Note: "個別単信・グループ・同報はタイマ不使用"},
		missing("TR319", "移動局", t1), missing("TR320", "移動局", t1), missing("TR321", "移動局", t1),
		missing("TR322", "移動局", t1), missing("TR330", "移動局", t1),
	)

	resend := func(msg, name string) string { return "「" + msg + "」再送・" + name + " 再設定" }
	stopTx := func(msg string) string { return "「" + msg + "」送信停止" }
	q = append(q,
		bs("TR301", "通信リンク確立フェーズ", tmKinds([]string{"複信", "個別単信"},
			tmRow("", anyV, "着信", "「ページング」送信", "「着信無線状態報告」受信", resend("ページング", "TR301"), idle))...),
		missing("TR302", "基地局", t2),
		bs("TR303", "通信リンク確立フェーズ・通信フェーズ チャネル切替",
			tmRow("複信", anyV, "無線チャネル指定", "「無線チャネル指定」送信", "レイヤ 1 から TCH 起動確認", resend("無線チャネル指定", "TR303"), idle)),
		bs("TR304", "通信リンク解放フェーズ（通信チャネル・制御チャネルで共用）",
			tmRow("複信", anyV, "無線チャネル切断", "「無線チャネル切断」送信", "「無線チャネル切断確認」受信", resend("無線チャネル切断", "TR304"), idle)),
		bs("TR307", "通信フェーズ（通信チャネル・制御チャネルで共用）",
			tmRow("複信", anyV, "システム情報", "「システム情報」送信", "「システム情報確認」受信", resend("システム情報", "TR307"), "TCH 起動状態に遷移")),
		missing("TR308", "基地局", t2), missing("TR309", "基地局", t2), missing("TR310", "基地局", t2),
		bs("TR313", "通信リンク確立フェーズ", tmKinds(tmSimplex,
			tmRow("", anyV, "移動局解放", "「移動局解放,－,解放完了」送信", "－", stopTx("移動局解放,－,解放完了"), noRe))...),
		bs("TR323", "通信リンク確立フェーズ", tmKinds(tmSimplex,
			tmRow("", anyV, "無線チャネル指定", "「無線チャネル指定」送信", "－", stopTx("無線チャネル指定"), noRe))...),
		bs("TR324", "通信リンク解放フェーズ", tmKinds(tmSimplex,
			tmRow("", anyV, "無線チャネル切断", "「無線チャネル切断,－,切断」送信", "－", stopTx("無線チャネル切断,－,切断"), noRe).note("制御 CH 通信時も使用"))...),
		bs("TR326", "通信リンク確立フェーズ",
			tmRow("グループ", anyV, "ページング", "「ページング」送信", "－", stopTx("ページング"), noRe),
			tmRow("同報", anyV, "同報", "「ページング」送信", "－", stopTx("ページング"), noRe)),
		bs("TR327", "通信リンク解放フェーズ",
			tmRow("複信", anyV, "通信時限満了", "「無線チャネル指定」送信", "「切断」受信", "「切断」送信", noRe).note("1 回目のみ"),
			tmRow("個別単信", anyV, "通信時限満了", "「無線チャネル指定,－,呼設定」送信", "「切断」受信", "「無線チャネル切断,－,切断」又は「移動局解放,－,解放完了」送信", noRe).note("1 回目のみ"),
			tmRow("グループ", anyV, "通信時限満了", "「無線チャネル指定,－,呼設定」送信", "「切断」受信", "「無線チャネル切断,－,切断」又は「移動局解放,－,解放完了」送信", noRe).note("1 回目のみ"),
			tmRow("同報", anyV, "通信時限満了", "「無線チャネル指定,－,呼設定」送信", "－", "「無線チャネル切断,－,切断」又は「移動局解放,－,解放完了」送信", noRe).note("1 回目のみ")),
		bs("TR328", "通信フェーズ", tmKinds(tmSimplex,
			tmRow("", anyV, "システム情報", "「システム情報」送信", "－", stopTx("システム情報"), noRe).note("制御 CH 通信時も使用"))...),
		bs("TR331", "通信フェーズ", tmKinds([]string{"個別単信", "グループ"},
			tmRow("", anyV, "通信権制御", "「送信停止要求」送信", "キャリア無検出", "TCH 起動状態に遷移", noRe).note("1 回目のみ。制御 CH 通信時も使用"))...),
		bs("TR334", "通信リンク確立フェーズ（制御 CH 通信）", tmJoin(
			[]TimerRow{tmRow("複信", anyV, "無線チャネル指定", "「無線チャネル指定,－,－」送信", "－", stopTx("無線チャネル指定,－,－"), noRe)},
			tmKinds(tmSimplex, tmRow("", anyV, "無線チャネル指定", "「無線チャネル指定,－,呼設定」送信", "－", stopTx("無線チャネル指定,－,呼設定"), noRe)))...),
		missing("TR335", "基地局", t2),
		func() Timer {
			t := bs("TR336", "通信リンク解放フェーズ（制御 CH 通信）",
				tmRow("複信", anyV, "通信時限満了", "「無線チャネル指定」送信", "「切断」受信", "「切断」送信", noRe).note("1 回目のみ"),
				tmRow("個別単信", anyV, "通信時限満了", "「無線チャネル指定,－,呼設定」送信", "「切断」受信", "「無線チャネル切断,－,切断」又は「移動局解放,－,解放完了」送信", noRe).note("1 回目のみ"),
				tmRow("グループ", anyV, "通信時限満了", "「無線チャネル指定,－,呼設定」送信", "「切断」受信", "「無線チャネル切断,－,切断」又は「移動局解放,－,解放完了」送信", noRe).note("1 回目のみ"),
				tmRow("同報", anyV, "通信時限満了", "「無線チャネル指定,－,呼設定」送信", "－", "「無線チャネル切断,－,切断」又は「移動局解放,－,解放完了」送信", noRe).note("1 回目のみ"))
			t.Note += "。TR336 の値は TR327 で設定される値以下とする"
			return t
		}(),
		bs("TR337", "通信フェーズ チャネル切替（制御 CH 通信）", tmKinds(tmSimplex,
			tmRow("", anyV, "チャネル切替", "「切替先無線チャネル指定」送信", "－", stopTx("切替先無線チャネル指定"), noRe))...),
		bs("TR338", "通信フェーズ チャネル切替（制御 CH 通信）",
			tmRow("複信", anyV, "チャネル切替", "「切替先無線チャネル指定」送信", "レイヤ 1 から TCH 起動確認", resend("切替先無線チャネル指定", "TR338"), "TCH 起動状態へ遷移")),
	)

	q = append(q, Timer{Name: "TM301", Side: "移動局", Entity: "MM", Table: t3, Phase: "待ち受け", TimeoutHeads: tmHeadMS, Rows: []TimerRow{
		tmRow("", "6 秒", "位置登録", "「位置登録要求」送信", "「位置登録受付」又は「位置登録拒否」受信", "「位置登録要求」再送・TM301 再設定", "「位置登録要求」3 回再送の後、空き状態に遷移")}})

	mcc := func(name, phase string, rows ...TimerRow) Timer {
		return Timer{Name: name, Side: "移動局", Entity: "CC", Table: t4, Phase: phase, Rows: rows, TimeoutHeads: tmHeadMS}
	}
	q = append(q,
		mcc("T303", "通信リンク確立フェーズ",
			tmRow("複信", "7 秒", "発信", "「複信呼設定」送信", "「呼設定受付」・「呼出」・「応答」又は「解放完了」受信", "内部の接続を切断復旧し「空」へ遷移", noRe),
			tmRow("個別単信", "7 秒", "発信", "「単信呼設定」送信", "「呼設定受付」又は「解放完了」受信", "内部の接続を切断復旧し「空」へ遷移", noRe),
			tmRow("グループ", "7 秒", "発信", "「単信呼設定」送信", "「呼設定受付」又は「解放完了」受信", "内部の接続を切断復旧し「空」へ遷移", noRe)),
		mcc("T314", "通信フェーズ チャネル切替", tmJoin(
			[]TimerRow{tmRow("複信", "2 秒", "再発呼", "「複信再呼設定」送信", "「再呼設定受付」・「解放完了」受信", "内部の接続を切断復旧", noRe)},
			tmKinds(tmSimplex, tmRow("", "2 秒", "再発呼", "「単信再呼設定」送信", "「再呼設定受付」・「解放完了」受信", "内部の接続を切断復旧", noRe)))...),
		mcc("T305", "通信リンク解放フェーズ",
			tmRow("複信", "7 秒", "切断要求", "「切断」送信", "「解放」又は「切断」受信", "RT を「空」状態にする", noRe)),
		mcc("T307", "通信リンク解放フェーズ", tmKinds([]string{"個別単信", "グループ"},
			tmRow("", "3 秒", "切断要求", "「切断」送信", "「切断」受信", "「切断」送信", "RT を「空」状態にする。再送回数任意"))...),
		mcc("T308", "通信リンク確立フェーズ・通信リンク解放フェーズ",
			tmRow("複信", "7 秒", "解放要求", "「解放」送信", "「解放完了」又は「解放」受信", "RT を「空」状態にする", noRe)),
		func() Timer {
			t := mcc("T309", "通信フェーズ", tmKinds([]string{"複信", "個別単信", "グループ"},
				tmRow("", "90 秒", "任意状態", "データリンク切断", "データリンク再接続", "「空」状態にする", noRe))...)
			t.Note = "オプション（注 1）"
			return t
		}(),
		mcc("T313", "通信リンク確立フェーズ",
			tmRow("複信", "7 秒", "応答", "「応答」送信", "「応答確認」受信", "「切断」送信", noRe)),
		mcc("T390", "通信リンク解放フェーズ", tmKinds(tmAll,
			tmRow("", "10 秒", "任意状態", "最後の呼番号が解放されたとき", "新しい呼番号設定時・RT が待ち受け状態に入ったとき", "待ち受け状態（RT）に入る", noRe))...),
	)

	bcc := func(name, phase string, rows ...TimerRow) Timer {
		return Timer{Name: name, Side: "基地局", Entity: "CC", Table: t5, Phase: phase, Rows: rows, TimeoutHeads: tmHeadBSCC,
			Note: "基地局側のタイマ値・再送回数は任意に設定できる（注 1）"}
	}
	q = append(q,
		bcc("T301", "通信リンク確立フェーズ",
			tmRow("複信", anyV, "呼出中", "「呼出」受信時", "「応答」受信時", "呼を切断復旧する", "タイマ再設定無", "タイマ再設定無")),
		bcc("T303", "通信リンク確立フェーズ",
			tmRow("複信", anyV, "着信", "「呼設定」送信", "「呼出」・「応答」又は「解放完了」受信", "「呼設定」再送・T303 再設定", "「空」状態に遷移", "")),
		bcc("T305", "通信リンク解放フェーズ",
			tmRow("複信", anyV, "切断通知", "経過識別子（#8）無の「切断」送信", "「解放」又は「切断」受信", "「解放」送信", "タイマ再設定無", "基地局側コネクション解放・「空」状態に遷移")),
		func() Timer {
			t := bcc("T306", "通信リンク解放フェーズ",
				tmRow("複信", anyV, "切断通知", "経過識別子（#8）有の「切断」送信", "「解放」又は「切断」受信", "トーン／アナウンス送信停止・「解放」送信", "タイマ再設定無", "タイマ再設定無"))
			t.Note += "。T306 の値はアナウンスの長さによる（注 2）"
			return t
		}(),
		bcc("T308", "通信リンク解放フェーズ",
			tmRow("複信", anyV, "解放要求", "「解放」送信", "「解放完了」又は「解放」受信", "「解放」再送・T308 再設定", "「空」状態に遷移", "「空」状態に遷移")),
	)
	for i := range q {
		if q[i].Entity == "" {
			q[i].Entity = "RT"
		}
	}
	return q
}()

func Timers() []Timer {
	out := make([]Timer, len(timers))
	copy(out, timers)
	return out
}

func TimersByName(name string) []Timer {
	var out []Timer
	for _, t := range timers {
		if strings.EqualFold(t.Name, name) {
			out = append(out, t)
		}
	}
	return out
}
