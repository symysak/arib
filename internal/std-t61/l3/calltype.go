package l3


type CallType struct {
	Name string
	Duplex string
	Section string
	Downlink string
	Emergency bool
	Basis string
}

type CallEvidence struct {
	Mode         *CommMode
	CCType       int
	GroupType    int
	SBIndividual *bool
	Route        int
}

func NewCallEvidence() CallEvidence { return CallEvidence{CCType: -1, GroupType: -1, Route: -1} }

func ClassifyCall(e CallEvidence) (CallType, bool) {
	var ct CallType
	ok := true
	switch {
	case e.Mode != nil && e.Mode.Form != FormDefault:
		ct = byForm(e.Mode.Form)
		ct.Emergency = e.Mode.Emergency
		ct.Basis = "伝達能力の通信形態"
	case e.CCType == 0x17 || e.CCType == 0x18:
		ct = byForm(FormDuplexIndividual)
		ct.Basis = "CC 複信呼設定"
	case e.CCType == 0x19 || e.GroupType >= 0:
		ct = groupOrBroadcast()
		ct.Basis = "グループ呼設定 / グループ通信呼出"
	case e.CCType == 0x15 || e.CCType == 0x16:
		ct = CallType{Name: "単信（個別かグループかは不明）", Duplex: "単信", Section: "4.3.8.1 / 4.3.8.2",
			Downlink: "送信権を持つ移動局の声を基地局が下りの通信チャネルへ中継する（単信なので一度に一方向）", Basis: "CC 単信呼設定"}
	case e.SBIndividual != nil && *e.SBIndividual:
		ct = CallType{Name: "個別（単信か複信かは不明）", Section: "4.3.8.1 / 4.3.8.5 / 4.3.8.6",
			Downlink: "単信なら送信中の移動局の声を基地局が中継する。複信なら相手側の声", Basis: "同期バーストの呼出符号（移動局個別番号）"}
	case e.SBIndividual != nil:
		ct = groupOrBroadcast()
		ct.Basis = "同期バーストの呼出符号（グループ番号）"
	default:
		ok = false
	}
	if !ok && e.Route <= 0 {
		return CallType{}, false
	}
	if e.GroupType == 1 || e.GroupType == 3 {
		ct.Emergency = true
	}
	if ct.Emergency && ct.Name != "" {
		ct.Name = "緊急" + ct.Name
		ct.Downlink += "。緊急通信は通信中の回線を強制的に切断して始まる（表 2.1-2）"
	}
	if e.GroupType == 2 || e.GroupType == 3 {
		ct.Name += "（後追い参入）"
	}
	switch e.Route {
	case 1:
		ct = withRoute(ct, "中継局経由", "4.3.8.13", "")
	case 2:
		ct = withRoute(ct, "基地局折り返し", "4.3.8.14", "制御局からの制御が不能になったとき（または基地局の手動操作時）、基地局単体で移動局どうしの声を折り返して下りへ送る（表 2.1-2）")
	case 3:
		ct = withRoute(ct, "移動局間直接", "4.3.8.12", "基地局を通らない。聞こえるのは送信中の移動局そのものの電波")
	}
	return ct, true
}

func withRoute(ct CallType, name, sec, down string) CallType {
	if ct.Name == "" {
		ct.Name, ct.Section, ct.Basis = name, sec, "RICH の回線種別"
		if down != "" {
			ct.Downlink = down
		}
		return ct
	}
	ct.Name += "・" + name
	ct.Section += " / " + sec
	if down != "" {
		ct.Downlink = down
	}
	return ct
}

func groupOrBroadcast() CallType {
	return CallType{Name: "グループ［単信／半複信］", Duplex: "単信／半複信", Section: "4.3.8.2 / 4.3.8.3",
		Downlink: "単信（移動局〜移動局 §4.3.8.2）なら送信中の移動局の声を基地局が中継する。半複信（基地局〜移動局 §4.3.8.3）なら基地局側の声" +
			"（どちらかは L3 の値では決まらない）"}
}

func byForm(f CommForm) CallType {
	switch f {
	case FormSimplexIndividual:
		return CallType{Name: "個別［単信］", Duplex: "単信", Section: "4.3.8.1",
			Downlink: "送信権を持つ移動局の声を基地局が下りの通信チャネルへ中継する（単信なので一度に一方向）"}
	case FormDuplexIndividual:
		return CallType{Name: "個別［複信］", Duplex: "複信", Section: "4.3.8.5 / 4.3.8.6",
			Downlink: "相手側の声（基地局〜移動局なら基地局側の声。この移動局自身の声は上り）"}
	case FormGroup:
		return groupOrBroadcast()
	case FormBroadcast:
		return CallType{Name: "同報［単向］", Duplex: "単向", Section: "4.3.8.4", Downlink: "基地局側の声だけ（移動局は送信しない）"}
	}
	return CallType{Name: "通信形態 予約", Section: "表 4.3.7.3-8 (6)"}
}
