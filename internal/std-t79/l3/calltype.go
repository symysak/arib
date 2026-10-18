package l3


type CallType struct {
	Name string
	Duplex string
	Section string
	Downlink string
	Emergency bool
}

func ClassifyCall(call *Call, groupHalf *bool) (CallType, bool) {
	if call == nil {
		return CallType{}, false
	}
	mode := call.Mode
	if mode == nil {
		switch call.MsgType {
		case 0x19:
			mode = &CommMode{Form: FormGroup}
		case 0x17, 0x18:
			mode = &CommMode{Form: FormDuplexIndividual}
		case 0x15, 0x16:
			return CallType{Name: "単信（個別かグループかは不明）", Duplex: "単信",
				Downlink: "話者の声を基地局が下りへ中継する"}, true
		default:
			return CallType{}, false
		}
	}
	pbx := call.CalledType == 2 || call.CalledType == 4 || call.CallingType == 2 || call.CallingType == 4 || call.SubStar
	return classifyMode(*mode, groupHalf, pbx), true
}

func classifyMode(m CommMode, groupHalf *bool, pbx bool) CallType {
	switch m.Form {
	case FormBroadcast:
		if m.Emergency {
			return CallType{Name: "一斉［単向］", Duplex: "単向", Section: "4.7.1.13", Emergency: true,
				Downlink: "統制局の声だけ（移動局は送信できない。通信中の局も強制的に引き込む）"}
		}
		return CallType{Name: "同報［単向］", Duplex: "単向", Section: "4.7.1.4",
			Downlink: "統制局の声だけ（移動局は送信できない）"}
	case FormGroup:
		name, sec := "グループ", "4.7.1.2"
		if m.Emergency {
			name, sec = "統制グループ", "4.7.1.14"
		}
		switch {
		case groupHalf == nil:
			return CallType{Name: name + "［単信／半複信］", Duplex: "", Section: sec, Emergency: m.Emergency,
				Downlink: "単信なら話者の声を基地局が中継する。半複信なら統制局側の声（報知情報の規制情報がまだ無いので決められない）"}
		case *groupHalf:
			if !m.Emergency {
				sec = "4.7.1.3"
			}
			return CallType{Name: name + "［半複信］", Duplex: "半複信", Section: sec, Emergency: m.Emergency,
				Downlink: "統制局側（基地局）の声。移動局の声を下りへ流し直すかは規格の図に無い（移動局の声は上り）"}
		default:
			return CallType{Name: name + "［単信］", Duplex: "単信", Section: sec, Emergency: m.Emergency,
				Downlink: "話者の声を基地局が下りへ中継する（移動局どうしの声が全部聞こえる）"}
		}
	case FormDuplexIndividual:
		ct := CallType{Name: "個別［複信］", Duplex: "複信", Section: "4.7.1.5 / 4.7.1.6",
			Downlink: "相手の声（統制局との通話なら統制局の声だけ。この移動局の声は上り。移動局どうしなら 2 スロットに互いの声）"}
		switch {
		case m.Emergency:
			ct.Name, ct.Section, ct.Emergency = "統制個別［複信］", "4.7.1.14", true
		case pbx:
			ct.Name, ct.Section = "PBX［複信］", "4.7.1.12"
			ct.Downlink = "電話側の声（移動局の声は上り）"
		}
		return ct
	case FormSimplexIndividual:
		ct := CallType{Name: "個別［単信］", Duplex: "単信", Section: "4.7.1.1",
			Downlink: "話者の声を基地局が下りへ中継する"}
		if m.Emergency {
			ct.Name, ct.Emergency = "個別［単信］（緊急）", true
		}
		return ct
	}
	return CallType{Name: "通信形態 予約", Downlink: ""}
}
