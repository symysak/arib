package control


type Field struct {
	Oct int
	Hi, Lo int
	Name string
	Note string
	Enum map[int]string
	Fmt func(int) string
	Payload bool
}

const (
	OctetsCCH   = 12
	OctetsFACCH = 63
	OctetsTCH   = 63
)

var (
	enumChID       = map[int]string{0: "CCH", 1: "FACCH", 2: "TCH", 3: "予約"}
	enumChSwitch   = map[int]string{0: "待機", 1: "同方向の SC へ"}
	enumMedia      = map[int]string{0: "予約", 1: "音声", 2: "予約(FAX)", 3: "予約(文字)", 4: "予約(画像)", 5: "予約(テレメトリ)", 6: "予備", 7: "予備"}
	enumTransProt  = map[int]string{0: "デジタル非音声", 1: "拡声音声", 2: "予備(連絡音声)", 3: "予備"}
	enumDisconnect = map[int]string{
		0: "正常切断/正常解放", 1: "親局による強制切断/強制解放", 2: "話中", 3: "着側応答なし",
		4: "通信タイマ満了", 5: "予備", 6: "予備", 7: "サービス不可", 8: "通信不可", 9: "予備",
		10: "不正メッセージ", 11: "同期外れ", 12: "通信以外のタイマ満了", 13: "予備", 14: "予備",
		15: "その他異常",
	}
	enumResult      = map[int]string{0: "OK", 1: "子局なし", 2: "話中"}
	enumEffectiveNo = map[int]string{0: "予備", 1: "親局識別番号", 2: "子局識別番号", 3: "中継局識別番号"}
	enumSiren       = buildSiren()
	enumDataType    = buildDataType()
)

func buildSiren() map[int]string {
	m := map[int]string{0: "サイレン指定なし"}
	for i := 1; i <= 10; i++ {
		m[i] = "G1D サイレン " + itoa(i)
	}
	return m
}

func buildDataType() map[int]string {
	m := map[int]string{}
	for i := 0; i <= 15; i++ {
		m[i] = "データ種別 " + itoa(i)
	}
	return m
}

func fmtChSwitchTiming(v int) string {
	if v == 0 {
		return "次フレームで切替"
	}
	return itoa(v+1) + " フレーム目で切替"
}

func fmtContinuation(v int) string {
	switch v {
	case 0:
		return "このフレームで連送終了"
	case 31:
		return "連送継続中"
	}
	return "残り " + itoa(v) + " フレーム"
}

func fmtCallNo(v int) string {
	if v == 0 {
		return "0（子局起呼）"
	}
	return itoa(v)
}

func spare(from, to int) []Field {
	out := make([]Field, 0, to-from+1)
	for o := from; o <= to; o++ {
		out = append(out, Field{Oct: o, Hi: 8, Lo: 1, Name: "予備"})
	}
	return out
}

func payloadRange(from, to int, name string) []Field {
	out := make([]Field, 0, to-from+1)
	for o := from; o <= to; o++ {
		out = append(out, Field{Oct: o, Hi: 8, Lo: 1, Name: name, Payload: true})
	}
	return out
}

func concat(xs ...[]Field) []Field {
	var out []Field
	for _, x := range xs {
		out = append(out, x...)
	}
	return out
}

var CommonHeader = []Field{
	{Oct: 1, Hi: 8, Lo: 8, Name: "B/I", Note: "ビジー（下り）/ 予備（上り）"},
	{Oct: 1, Hi: 7, Lo: 1, Name: "メッセージ種別", Note: "7bit"},
	{Oct: 2, Hi: 8, Lo: 7, Name: "免許人固有情報", Note: "※1"},
	{Oct: 2, Hi: 6, Lo: 1, Name: "製造者識別番号", Note: "1〜63（Annex 3）"},
	{Oct: 3, Hi: 8, Lo: 8, Name: "AMR-WB+ SN / 予備", Note: "TCH 音声(71h-74h)のみ SN"},
	{Oct: 3, Hi: 7, Lo: 6, Name: "CH ID", Enum: enumChID},
	{Oct: 3, Hi: 5, Lo: 5, Name: "CH 切替情報", Enum: enumChSwitch},
	{Oct: 3, Hi: 4, Lo: 1, Name: "CH 切替タイミング", Fmt: fmtChSwitchTiming},
}

var bodies = map[int][]Field{
	0x01: concat(
		[]Field{
			{Oct: 4, Hi: 8, Lo: 8, Name: "連絡通信 可否", Note: "1=可 0=不可（状況フラグ）"},
			{Oct: 4, Hi: 7, Lo: 7, Name: "データ伝送 可否", Note: "1=可 0=不可"},
			{Oct: 4, Hi: 6, Lo: 5, Name: "予備", Note: "状況フラグの予備"},
			{Oct: 4, Hi: 4, Lo: 1, Name: "予備"},
		},
		spare(5, 12),
	),
	0x02: concat(
		spare(4, 12),
	),
	0x03: concat(
		[]Field{
			{Oct: 4, Hi: 8, Lo: 6, Name: "メディア種別", Enum: enumMedia},
			{Oct: 4, Hi: 5, Lo: 4, Name: "伝送プロトコル", Enum: enumTransProt},
			{Oct: 4, Hi: 3, Lo: 1, Name: "呼番号", Fmt: fmtCallNo},
			{Oct: 5, Hi: 8, Lo: 7, Name: "予備"},
			{Oct: 5, Hi: 6, Lo: 6, Name: "番号通知フラグ", Note: "※6"},
			{Oct: 5, Hi: 5, Lo: 5, Name: "録音解除フラグ", Note: "※5"},
			{Oct: 5, Hi: 4, Lo: 4, Name: "緊急フラグ", Note: "※4"},
			{Oct: 5, Hi: 3, Lo: 3, Name: "戸別受信機強制音量フラグ", Note: "※3"},
			{Oct: 5, Hi: 2, Lo: 1, Name: "音量設定"},
			{Oct: 6, Hi: 8, Lo: 6, Name: "予備"},
			{Oct: 6, Hi: 5, Lo: 5, Name: "時差通報有効", Note: "※7"},
			{Oct: 6, Hi: 4, Lo: 1, Name: "分割番号"},
			{Oct: 7, Hi: 8, Lo: 1, Name: "子局識別番号(上位)", Note: "16bit 全 0 = 一斉通報番号（§2.3）"},
			{Oct: 8, Hi: 8, Lo: 1, Name: "子局識別番号(下位)"},
		},
		spare(9, 12),
	),
	0x04: concat(
		[]Field{
			{Oct: 4, Hi: 8, Lo: 6, Name: "メディア種別", Enum: enumMedia},
			{Oct: 4, Hi: 5, Lo: 4, Name: "伝送プロトコル", Enum: enumTransProt},
			{Oct: 4, Hi: 3, Lo: 1, Name: "呼番号", Fmt: fmtCallNo},
			{Oct: 5, Hi: 8, Lo: 7, Name: "予備"},
			{Oct: 5, Hi: 6, Lo: 6, Name: "番号通知フラグ"},
			{Oct: 5, Hi: 5, Lo: 5, Name: "録音停止フラグ"},
			{Oct: 5, Hi: 4, Lo: 4, Name: "緊急フラグ"},
			{Oct: 5, Hi: 3, Lo: 3, Name: "戸別受信機強制音量フラグ"},
			{Oct: 5, Hi: 2, Lo: 1, Name: "音量設定"},
			{Oct: 6, Hi: 8, Lo: 6, Name: "予備"},
			{Oct: 6, Hi: 5, Lo: 5, Name: "時差通報有効"},
			{Oct: 6, Hi: 4, Lo: 1, Name: "分割番号"},
			{Oct: 7, Hi: 8, Lo: 1, Name: "子局識別番号(上位)", Note: "16bit 全 0 = 一斉"},
			{Oct: 8, Hi: 8, Lo: 1, Name: "子局識別番号(下位)"},
		},
		spare(9, 12),
	),
	0x05: concat(
		[]Field{
			{Oct: 4, Hi: 8, Lo: 4, Name: "予備"},
			{Oct: 4, Hi: 3, Lo: 1, Name: "呼番号", Fmt: fmtCallNo},
			{Oct: 5, Hi: 8, Lo: 7, Name: "予備"},
			{Oct: 5, Hi: 6, Lo: 6, Name: "番号通知フラグ", Note: "※5"},
			{Oct: 5, Hi: 5, Lo: 5, Name: "録音停止フラグ", Note: "※4"},
			{Oct: 5, Hi: 4, Lo: 4, Name: "予備"},
			{Oct: 5, Hi: 3, Lo: 3, Name: "戸別受信機強制音量フラグ", Note: "※3"},
			{Oct: 5, Hi: 2, Lo: 1, Name: "音量設定"},
			{Oct: 6, Hi: 8, Lo: 6, Name: "予備"},
			{Oct: 6, Hi: 5, Lo: 1, Name: "サイレン種別", Enum: enumSiren},
			{Oct: 7, Hi: 8, Lo: 1, Name: "子局識別番号(上位)"},
			{Oct: 8, Hi: 8, Lo: 1, Name: "子局識別番号(下位)"},
		},
		spare(9, 12),
	),
	0x06: concat(
		[]Field{
			{Oct: 4, Hi: 8, Lo: 4, Name: "予備"},
			{Oct: 4, Hi: 3, Lo: 1, Name: "呼番号", Fmt: fmtCallNo},
			{Oct: 5, Hi: 8, Lo: 5, Name: "予備"},
			{Oct: 5, Hi: 4, Lo: 1, Name: "切断理由", Enum: enumDisconnect},
		},
		spare(6, 12),
	),
	0x07: concat(
		[]Field{
			{Oct: 4, Hi: 8, Lo: 4, Name: "予備"},
			{Oct: 4, Hi: 3, Lo: 1, Name: "呼番号", Fmt: fmtCallNo},
			{Oct: 5, Hi: 8, Lo: 5, Name: "予備"},
			{Oct: 5, Hi: 4, Lo: 1, Name: "切断理由", Enum: enumDisconnect},
		},
		spare(6, 12),
	),
	0x08: {
		{Oct: 4, Hi: 8, Lo: 6, Name: "メディア種別", Enum: enumMedia},
		{Oct: 4, Hi: 5, Lo: 4, Name: "伝送プロトコル", Enum: enumTransProt},
		{Oct: 4, Hi: 3, Lo: 1, Name: "呼番号", Fmt: fmtCallNo},
		{Oct: 5, Hi: 8, Lo: 1, Name: "予備"},
		{Oct: 6, Hi: 8, Lo: 1, Name: "予備"},
		{Oct: 7, Hi: 8, Lo: 1, Name: "子局識別番号(上位)"},
		{Oct: 8, Hi: 8, Lo: 1, Name: "子局識別番号(下位)"},
		{Oct: 9, Hi: 8, Lo: 1, Name: "ユーザデータ(上位)"},
		{Oct: 10, Hi: 8, Lo: 1, Name: "ユーザデータ(下位)"},
		{Oct: 11, Hi: 8, Lo: 1, Name: "予備"},
		{Oct: 12, Hi: 8, Lo: 1, Name: "予備"},
	},
	0x09: concat(
		[]Field{
			{Oct: 4, Hi: 8, Lo: 4, Name: "予備"},
			{Oct: 4, Hi: 3, Lo: 1, Name: "呼番号", Fmt: fmtCallNo},
			{Oct: 5, Hi: 8, Lo: 5, Name: "予備"},
			{Oct: 5, Hi: 4, Lo: 1, Name: "結果", Enum: enumResult},
			{Oct: 6, Hi: 8, Lo: 1, Name: "予備"},
			{Oct: 7, Hi: 8, Lo: 1, Name: "子局識別番号(上位)"},
			{Oct: 8, Hi: 8, Lo: 1, Name: "子局識別番号(下位)"},
		},
		spare(9, 12),
	),
	0x0A: {
		{Oct: 4, Hi: 8, Lo: 6, Name: "メディア種別", Enum: enumMedia},
		{Oct: 4, Hi: 5, Lo: 4, Name: "伝送プロトコル", Enum: enumTransProt},
		{Oct: 4, Hi: 3, Lo: 1, Name: "呼番号", Fmt: fmtCallNo},
		{Oct: 5, Hi: 8, Lo: 5, Name: "親局識別番号"},
		{Oct: 5, Hi: 4, Lo: 1, Name: "予備"},
		{Oct: 6, Hi: 8, Lo: 1, Name: "予備"},
		{Oct: 7, Hi: 8, Lo: 1, Name: "子局識別番号(上位)"},
		{Oct: 8, Hi: 8, Lo: 1, Name: "子局識別番号(下位)"},
		{Oct: 9, Hi: 8, Lo: 1, Name: "ユーザデータ(上位)"},
		{Oct: 10, Hi: 8, Lo: 1, Name: "ユーザデータ(下位)"},
		{Oct: 11, Hi: 8, Lo: 1, Name: "予備"},
		{Oct: 12, Hi: 8, Lo: 1, Name: "予備"},
	},
	0x0B: concat(
		[]Field{
			{Oct: 4, Hi: 8, Lo: 4, Name: "予備"},
			{Oct: 4, Hi: 3, Lo: 1, Name: "呼番号", Fmt: fmtCallNo},
			{Oct: 5, Hi: 8, Lo: 5, Name: "予備"},
			{Oct: 5, Hi: 4, Lo: 1, Name: "結果", Enum: enumResult},
			{Oct: 6, Hi: 8, Lo: 1, Name: "予備"},
			{Oct: 7, Hi: 8, Lo: 1, Name: "子局識別番号(上位)"},
			{Oct: 8, Hi: 8, Lo: 1, Name: "子局識別番号(下位)"},
		},
		spare(9, 12),
	),
	0x0C: {
		{Oct: 4, Hi: 8, Lo: 6, Name: "メディア種別", Enum: enumMedia},
		{Oct: 4, Hi: 5, Lo: 4, Name: "伝送プロトコル", Enum: enumTransProt},
		{Oct: 4, Hi: 3, Lo: 1, Name: "呼番号", Fmt: fmtCallNo},
		{Oct: 5, Hi: 8, Lo: 1, Name: "予備"},
		{Oct: 6, Hi: 8, Lo: 1, Name: "予備"},
		{Oct: 7, Hi: 8, Lo: 1, Name: "中継局識別番号"},
		{Oct: 8, Hi: 8, Lo: 1, Name: "予備"},
		{Oct: 9, Hi: 8, Lo: 1, Name: "ユーザデータ(上位)"},
		{Oct: 10, Hi: 8, Lo: 1, Name: "ユーザデータ(下位)"},
		{Oct: 11, Hi: 8, Lo: 1, Name: "予備"},
		{Oct: 12, Hi: 8, Lo: 1, Name: "予備"},
	},
	0x0D: concat(
		[]Field{
			{Oct: 4, Hi: 8, Lo: 4, Name: "予備"},
			{Oct: 4, Hi: 3, Lo: 1, Name: "呼番号", Fmt: fmtCallNo},
			{Oct: 5, Hi: 8, Lo: 5, Name: "予備"},
			{Oct: 5, Hi: 4, Lo: 1, Name: "結果", Enum: enumResult},
			{Oct: 6, Hi: 8, Lo: 1, Name: "予備"},
			{Oct: 7, Hi: 8, Lo: 1, Name: "中継局識別番号"},
		},
		spare(8, 12),
	),
	0x0E: {
		{Oct: 4, Hi: 8, Lo: 6, Name: "メディア種別", Enum: enumMedia},
		{Oct: 4, Hi: 5, Lo: 4, Name: "伝送プロトコル", Enum: enumTransProt},
		{Oct: 4, Hi: 3, Lo: 1, Name: "呼番号", Fmt: fmtCallNo},
		{Oct: 5, Hi: 8, Lo: 5, Name: "親局識別番号"},
		{Oct: 5, Hi: 4, Lo: 1, Name: "予備"},
		{Oct: 6, Hi: 8, Lo: 1, Name: "予備"},
		{Oct: 7, Hi: 8, Lo: 1, Name: "中継局識別番号"},
		{Oct: 8, Hi: 8, Lo: 1, Name: "予備"},
		{Oct: 9, Hi: 8, Lo: 1, Name: "ユーザデータ(上位)"},
		{Oct: 10, Hi: 8, Lo: 1, Name: "ユーザデータ(下位)"},
		{Oct: 11, Hi: 8, Lo: 1, Name: "予備"},
		{Oct: 12, Hi: 8, Lo: 1, Name: "予備"},
	},
	0x0F: concat(
		[]Field{
			{Oct: 4, Hi: 8, Lo: 4, Name: "予備"},
			{Oct: 4, Hi: 3, Lo: 1, Name: "呼番号", Fmt: fmtCallNo},
			{Oct: 5, Hi: 8, Lo: 5, Name: "予備"},
			{Oct: 5, Hi: 4, Lo: 1, Name: "結果", Enum: enumResult},
			{Oct: 6, Hi: 8, Lo: 1, Name: "予備"},
			{Oct: 7, Hi: 8, Lo: 1, Name: "中継局識別番号"},
		},
		spare(8, 12),
	),
	0x10: {
		{Oct: 4, Hi: 8, Lo: 6, Name: "メディア種別", Enum: enumMedia},
		{Oct: 4, Hi: 5, Lo: 4, Name: "伝送プロトコル", Enum: enumTransProt},
		{Oct: 4, Hi: 3, Lo: 1, Name: "呼番号", Fmt: fmtCallNo},
		{Oct: 5, Hi: 8, Lo: 5, Name: "予備"},
		{Oct: 5, Hi: 4, Lo: 1, Name: "データ種別", Enum: enumDataType},
		{Oct: 6, Hi: 8, Lo: 1, Name: "予備"},
		{Oct: 7, Hi: 8, Lo: 1, Name: "子局識別番号(上位)"},
		{Oct: 8, Hi: 8, Lo: 1, Name: "子局識別番号(下位)"},
		{Oct: 9, Hi: 8, Lo: 1, Name: "ユーザデータ(上位)"},
		{Oct: 10, Hi: 8, Lo: 1, Name: "ユーザデータ(下位)"},
		{Oct: 11, Hi: 8, Lo: 1, Name: "予備"},
		{Oct: 12, Hi: 8, Lo: 1, Name: "予備"},
	},
	0x11: concat(
		[]Field{
			{Oct: 4, Hi: 8, Lo: 4, Name: "予備"},
			{Oct: 4, Hi: 3, Lo: 1, Name: "呼番号", Fmt: fmtCallNo},
			{Oct: 5, Hi: 8, Lo: 5, Name: "予備"},
			{Oct: 5, Hi: 4, Lo: 1, Name: "結果", Enum: enumResult},
			{Oct: 6, Hi: 8, Lo: 1, Name: "予備"},
			{Oct: 7, Hi: 8, Lo: 1, Name: "子局識別番号(上位)"},
			{Oct: 8, Hi: 8, Lo: 1, Name: "子局識別番号(下位)"},
		},
		spare(9, 12),
	),
	0x12: {
		{Oct: 4, Hi: 8, Lo: 6, Name: "メディア種別", Enum: enumMedia},
		{Oct: 4, Hi: 5, Lo: 4, Name: "伝送プロトコル", Enum: enumTransProt},
		{Oct: 4, Hi: 3, Lo: 1, Name: "呼番号", Fmt: fmtCallNo},
		{Oct: 5, Hi: 8, Lo: 5, Name: "親局識別番号"},
		{Oct: 5, Hi: 4, Lo: 1, Name: "データ種別", Enum: enumDataType},
		{Oct: 6, Hi: 8, Lo: 1, Name: "予備"},
		{Oct: 7, Hi: 8, Lo: 1, Name: "子局識別番号(上位)"},
		{Oct: 8, Hi: 8, Lo: 1, Name: "子局識別番号(下位)"},
		{Oct: 9, Hi: 8, Lo: 1, Name: "ユーザデータ(上位)"},
		{Oct: 10, Hi: 8, Lo: 1, Name: "ユーザデータ(下位)"},
		{Oct: 11, Hi: 8, Lo: 1, Name: "予備"},
		{Oct: 12, Hi: 8, Lo: 1, Name: "予備"},
	},
	0x13: concat(
		[]Field{
			{Oct: 4, Hi: 8, Lo: 4, Name: "予備"},
			{Oct: 4, Hi: 3, Lo: 1, Name: "呼番号", Fmt: fmtCallNo},
			{Oct: 5, Hi: 8, Lo: 5, Name: "予備"},
			{Oct: 5, Hi: 4, Lo: 1, Name: "結果", Enum: enumResult},
			{Oct: 6, Hi: 8, Lo: 1, Name: "予備"},
			{Oct: 7, Hi: 8, Lo: 1, Name: "子局識別番号(上位)"},
			{Oct: 8, Hi: 8, Lo: 1, Name: "子局識別番号(下位)"},
		},
		spare(9, 12),
	),
	0x14: concat(
		[]Field{
			{Oct: 4, Hi: 8, Lo: 4, Name: "予備"},
			{Oct: 4, Hi: 3, Lo: 1, Name: "呼番号", Fmt: fmtCallNo},
			{Oct: 5, Hi: 8, Lo: 5, Name: "親局識別番号"},
			{Oct: 5, Hi: 4, Lo: 1, Name: "有効番号識別子", Enum: enumEffectiveNo},
			{Oct: 6, Hi: 8, Lo: 1, Name: "予備"},
			{Oct: 7, Hi: 8, Lo: 1, Name: "子局識別番号(上位) / 中継局識別番号", Note: "有効番号識別子=2 なら子局(oct7-8, 16bit)、=3 なら中継局(oct7, 8bit)"},
			{Oct: 8, Hi: 8, Lo: 1, Name: "子局識別番号(下位) / 予備"},
		},
		spare(9, 12),
	),
	0x15: concat(
		[]Field{
			{Oct: 4, Hi: 8, Lo: 4, Name: "予備"},
			{Oct: 4, Hi: 3, Lo: 1, Name: "呼番号", Fmt: fmtCallNo},
			{Oct: 5, Hi: 8, Lo: 5, Name: "予備"},
			{Oct: 5, Hi: 4, Lo: 1, Name: "有効番号識別子", Enum: enumEffectiveNo},
			{Oct: 6, Hi: 8, Lo: 1, Name: "予備"},
			{Oct: 7, Hi: 8, Lo: 1, Name: "子局識別番号(上位) / 中継局識別番号", Note: "有効番号識別子=2 なら子局(oct7-8, 16bit)、=3 なら中継局(oct7, 8bit)"},
			{Oct: 8, Hi: 8, Lo: 1, Name: "子局識別番号(下位) / 予備"},
		},
		spare(9, 12),
	),
	0x16: {
		{Oct: 4, Hi: 8, Lo: 4, Name: "予備"},
		{Oct: 4, Hi: 3, Lo: 1, Name: "呼番号", Fmt: fmtCallNo},
		{Oct: 5, Hi: 8, Lo: 8, Name: "後続応答の有無", Note: "0=なし 1=あり（監視情報指示子）"},
		{Oct: 5, Hi: 7, Lo: 5, Name: "シーケンス番号", Note: "0〜7"},
		{Oct: 5, Hi: 4, Lo: 1, Name: "有効番号識別子", Enum: enumEffectiveNo},
		{Oct: 6, Hi: 8, Lo: 1, Name: "予備"},
		{Oct: 7, Hi: 8, Lo: 1, Name: "子局識別番号(上位) / 中継局識別番号", Note: "有効番号識別子=2 なら子局(oct7-8, 16bit)、=3 なら中継局(oct7, 8bit)"},
		{Oct: 8, Hi: 8, Lo: 1, Name: "子局識別番号(下位) / 予備"},
		{Oct: 9, Hi: 8, Lo: 1, Name: "監視情報 (1/4)"},
		{Oct: 10, Hi: 8, Lo: 1, Name: "監視情報 (2/4)"},
		{Oct: 11, Hi: 8, Lo: 1, Name: "監視情報 (3/4)"},
		{Oct: 12, Hi: 8, Lo: 1, Name: "監視情報 (4/4)"},
	},
	0x17: {
		{Oct: 4, Hi: 8, Lo: 4, Name: "予備"},
		{Oct: 4, Hi: 3, Lo: 1, Name: "呼番号", Fmt: fmtCallNo},
		{Oct: 5, Hi: 8, Lo: 8, Name: "後続要求の有無", Note: "0=なし 1=あり（制御情報指示子）"},
		{Oct: 5, Hi: 7, Lo: 5, Name: "シーケンス番号", Note: "0〜7"},
		{Oct: 5, Hi: 4, Lo: 1, Name: "有効番号識別子", Enum: enumEffectiveNo},
		{Oct: 6, Hi: 8, Lo: 1, Name: "予備"},
		{Oct: 7, Hi: 8, Lo: 1, Name: "子局識別番号(上位) / 中継局識別番号", Note: "有効番号識別子=2 なら子局(oct7-8, 16bit)、=3 なら中継局(oct7, 8bit)"},
		{Oct: 8, Hi: 8, Lo: 1, Name: "子局識別番号(下位) / 予備"},
		{Oct: 9, Hi: 8, Lo: 1, Name: "制御情報 (1/4)"},
		{Oct: 10, Hi: 8, Lo: 1, Name: "制御情報 (2/4)"},
		{Oct: 11, Hi: 8, Lo: 1, Name: "制御情報 (3/4)"},
		{Oct: 12, Hi: 8, Lo: 1, Name: "制御情報 (4/4)"},
	},
	0x18: {
		{Oct: 4, Hi: 8, Lo: 4, Name: "予備"},
		{Oct: 4, Hi: 3, Lo: 1, Name: "呼番号", Fmt: fmtCallNo},
		{Oct: 5, Hi: 8, Lo: 8, Name: "後続応答の有無", Note: "0=なし 1=あり（制御情報指示子）"},
		{Oct: 5, Hi: 7, Lo: 5, Name: "シーケンス番号", Note: "0〜7"},
		{Oct: 5, Hi: 4, Lo: 1, Name: "有効番号識別子", Enum: enumEffectiveNo},
		{Oct: 6, Hi: 8, Lo: 1, Name: "予備"},
		{Oct: 7, Hi: 8, Lo: 1, Name: "子局識別番号(上位) / 中継局識別番号", Note: "有効番号識別子=2 なら子局(oct7-8, 16bit)、=3 なら中継局(oct7, 8bit)"},
		{Oct: 8, Hi: 8, Lo: 1, Name: "子局識別番号(下位) / 予備"},
		{Oct: 9, Hi: 8, Lo: 1, Name: "制御情報 (1/4)"},
		{Oct: 10, Hi: 8, Lo: 1, Name: "制御情報 (2/4)"},
		{Oct: 11, Hi: 8, Lo: 1, Name: "制御情報 (3/4)"},
		{Oct: 12, Hi: 8, Lo: 1, Name: "制御情報 (4/4)"},
	},
	0x50: concat(
		spare(4, 63),
	),
	0x51: concat(
		[]Field{
			{Oct: 4, Hi: 8, Lo: 4, Name: "予備"},
			{Oct: 4, Hi: 3, Lo: 1, Name: "呼番号", Fmt: fmtCallNo},
			{Oct: 5, Hi: 8, Lo: 5, Name: "予備"},
			{Oct: 5, Hi: 4, Lo: 1, Name: "切断理由", Enum: enumDisconnect},
		},
		spare(6, 63),
	),
	0x52: {
		{Oct: 4, Hi: 8, Lo: 4, Name: "予備"},
		{Oct: 4, Hi: 3, Lo: 1, Name: "呼番号", Fmt: fmtCallNo},
		{Oct: 5, Hi: 8, Lo: 1, Name: "予備"},
		{Oct: 6, Hi: 8, Lo: 6, Name: "予備"},
		{Oct: 6, Hi: 5, Lo: 1, Name: "子局識別番号数"},
		{Oct: 7, Hi: 8, Lo: 1, Name: "子局識別番号 1(上位)"},
		{Oct: 8, Hi: 8, Lo: 1, Name: "子局識別番号 1(下位)"},
		{Oct: 9, Hi: 8, Lo: 1, Name: "子局識別番号 2(上位)"},
		{Oct: 10, Hi: 8, Lo: 1, Name: "子局識別番号 2(下位)"},
		{Oct: 11, Hi: 8, Lo: 1, Name: "子局識別番号 3(上位)"},
		{Oct: 12, Hi: 8, Lo: 1, Name: "子局識別番号 3(下位)"},
		{Oct: 13, Hi: 8, Lo: 1, Name: "子局識別番号 4(上位)"},
		{Oct: 14, Hi: 8, Lo: 1, Name: "子局識別番号 4(下位)"},
		{Oct: 15, Hi: 8, Lo: 1, Name: "子局識別番号 5(上位)"},
		{Oct: 16, Hi: 8, Lo: 1, Name: "子局識別番号 5(下位)"},
		{Oct: 17, Hi: 8, Lo: 1, Name: "子局識別番号 6(上位)"},
		{Oct: 18, Hi: 8, Lo: 1, Name: "子局識別番号 6(下位)"},
		{Oct: 19, Hi: 8, Lo: 1, Name: "子局識別番号 7(上位)"},
		{Oct: 20, Hi: 8, Lo: 1, Name: "子局識別番号 7(下位)"},
		{Oct: 21, Hi: 8, Lo: 1, Name: "子局識別番号 8(上位)"},
		{Oct: 22, Hi: 8, Lo: 1, Name: "子局識別番号 8(下位)"},
		{Oct: 23, Hi: 8, Lo: 1, Name: "子局識別番号 9(上位)"},
		{Oct: 24, Hi: 8, Lo: 1, Name: "子局識別番号 9(下位)"},
		{Oct: 25, Hi: 8, Lo: 1, Name: "子局識別番号 10(上位)"},
		{Oct: 26, Hi: 8, Lo: 1, Name: "子局識別番号 10(下位)"},
		{Oct: 27, Hi: 8, Lo: 1, Name: "子局識別番号 11(上位)"},
		{Oct: 28, Hi: 8, Lo: 1, Name: "子局識別番号 11(下位)"},
		{Oct: 29, Hi: 8, Lo: 1, Name: "子局識別番号 12(上位)"},
		{Oct: 30, Hi: 8, Lo: 1, Name: "子局識別番号 12(下位)"},
		{Oct: 31, Hi: 8, Lo: 1, Name: "子局識別番号 13(上位)"},
		{Oct: 32, Hi: 8, Lo: 1, Name: "子局識別番号 13(下位)"},
		{Oct: 33, Hi: 8, Lo: 1, Name: "子局識別番号 14(上位)"},
		{Oct: 34, Hi: 8, Lo: 1, Name: "子局識別番号 14(下位)"},
		{Oct: 35, Hi: 8, Lo: 1, Name: "子局識別番号 15(上位)"},
		{Oct: 36, Hi: 8, Lo: 1, Name: "子局識別番号 15(下位)"},
		{Oct: 37, Hi: 8, Lo: 1, Name: "子局識別番号 16(上位)"},
		{Oct: 38, Hi: 8, Lo: 1, Name: "子局識別番号 16(下位)"},
		{Oct: 39, Hi: 8, Lo: 1, Name: "子局識別番号 17(上位)"},
		{Oct: 40, Hi: 8, Lo: 1, Name: "子局識別番号 17(下位)"},
		{Oct: 41, Hi: 8, Lo: 1, Name: "子局識別番号 18(上位)"},
		{Oct: 42, Hi: 8, Lo: 1, Name: "子局識別番号 18(下位)"},
		{Oct: 43, Hi: 8, Lo: 1, Name: "子局識別番号 19(上位)"},
		{Oct: 44, Hi: 8, Lo: 1, Name: "子局識別番号 19(下位)"},
		{Oct: 45, Hi: 8, Lo: 1, Name: "子局識別番号 20(上位)"},
		{Oct: 46, Hi: 8, Lo: 1, Name: "子局識別番号 20(下位)"},
		{Oct: 47, Hi: 8, Lo: 1, Name: "子局識別番号 21(上位)"},
		{Oct: 48, Hi: 8, Lo: 1, Name: "子局識別番号 21(下位)"},
		{Oct: 49, Hi: 8, Lo: 1, Name: "子局識別番号 22(上位)"},
		{Oct: 50, Hi: 8, Lo: 1, Name: "子局識別番号 22(下位)"},
		{Oct: 51, Hi: 8, Lo: 1, Name: "子局識別番号 23(上位)"},
		{Oct: 52, Hi: 8, Lo: 1, Name: "子局識別番号 23(下位)"},
		{Oct: 53, Hi: 8, Lo: 1, Name: "子局識別番号 24(上位)"},
		{Oct: 54, Hi: 8, Lo: 1, Name: "子局識別番号 24(下位)"},
		{Oct: 55, Hi: 8, Lo: 1, Name: "子局識別番号 25(上位)"},
		{Oct: 56, Hi: 8, Lo: 1, Name: "子局識別番号 25(下位)"},
		{Oct: 57, Hi: 8, Lo: 1, Name: "子局識別番号 26(上位)"},
		{Oct: 58, Hi: 8, Lo: 1, Name: "子局識別番号 26(下位)"},
		{Oct: 59, Hi: 8, Lo: 1, Name: "子局識別番号 27(上位)"},
		{Oct: 60, Hi: 8, Lo: 1, Name: "子局識別番号 27(下位)"},
		{Oct: 61, Hi: 8, Lo: 1, Name: "子局識別番号 28(上位)"},
		{Oct: 62, Hi: 8, Lo: 1, Name: "子局識別番号 28(下位)"},
		{Oct: 63, Hi: 8, Lo: 6, Name: "予備"},
		{Oct: 63, Hi: 5, Lo: 1, Name: "継続カウンタ", Fmt: fmtContinuation},
	},
	0x53: concat(
		[]Field{
			{Oct: 4, Hi: 8, Lo: 4, Name: "予備"},
			{Oct: 4, Hi: 3, Lo: 1, Name: "呼番号", Fmt: fmtCallNo},
		},
		spare(5, 63),
	),
	0x70: concat(
		spare(4, 63),
	),
	0x71: concat(
		payloadRange(4, 63, "通報用符号化音声"),
	),
	0x72: concat(
		payloadRange(4, 63, "通報用符号化音声"),
	),
	0x73: concat(
		payloadRange(4, 63, "通報用符号化音声"),
	),
	0x74: concat(
		payloadRange(4, 63, "連絡通信用符号化音声"),
	),
	0x75: concat(
		payloadRange(4, 63, "伝送データ"),
	),
}

func FieldsFor(msgType int) []Field { return bodies[msgType] }

func ChannelOf(msgType int) string {
	switch {
	case msgType >= 0x70:
		return "TCH"
	case msgType >= 0x50:
		return "FACCH"
	default:
		return "CCH"
	}
}

func OctetsFor(channel string) int {
	switch channel {
	case "FACCH":
		return OctetsFACCH
	case "TCH":
		return OctetsTCH
	default:
		return OctetsCCH
	}
}

func itoa(v int) string {
	if v == 0 {
		return "0"
	}
	neg := v < 0
	if neg {
		v = -v
	}
	var b [12]byte
	n := len(b)
	for v > 0 {
		n--
		b[n] = byte('0' + v%10)
		v /= 10
	}
	if neg {
		n--
		b[n] = '-'
	}
	return string(b[n:])
}
