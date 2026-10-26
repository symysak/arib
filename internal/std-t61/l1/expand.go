package l1

import "fmt"

type Row struct {
	Name  string `json:"name"`
	Bits  string `json:"bits"`
	Value int    `json:"value"`
	Text  string `json:"text"`
}

func fieldBits(v uint32, width int) string { return fmt.Sprintf("%0*b", width, v) }

func table(t []string, i uint8) string {
	if int(i) < len(t) {
		return t[i]
	}
	return "?"
}

func (r RICH) ExpandRICH(ed Edition) []Row {
	if ed == EditionSCPC {
		return []Row{
			{"未定義(b17)", fieldBits(r.Raw>>16, 1), int(r.Raw >> 16), ""},
			{"F 機能", fieldBits(uint32(r.SF), 3), int(r.SF), table(scpcF, r.SF)},
			{"M 通信モード", fieldBits(uint32(r.SM), 3), int(r.SM), table(scpcM, r.SM)},
			{"D 回線種別", fieldBits(uint32(r.SD), 3), int(r.SD), table(scpcD, r.SD)},
			{"未定義(下位7)", fieldBits(r.Raw&0x7F, 7), int(r.Raw & 0x7F), ""},
		}
	}
	rows := []Row{{"S 種別", fieldBits(uint32(r.S), 2), int(r.S), table(fdmaS, r.S)}}
	if r.S == 2 {
		return append(rows, Row{"ユーザー情報", fieldBits(uint32(r.User), 15), int(r.User), fmt.Sprintf("0x%04X", r.User)})
	}
	return append(rows,
		Row{"F 機能", fieldBits(uint32(r.F), 3), int(r.F), table(fdmaF, r.F)},
		Row{"M 通信モード", fieldBits(uint32(r.M), 3), int(r.M), table(fdmaM, r.M)},
		Row{"D 回線種別", fieldBits(uint32(r.D), 3), int(r.D), table(fdmaD, r.D)},
		Row{"CC カラーコード", fieldBits(uint32(r.CC), 6), int(r.CC), ""},
	)
}

func (s SyncBurst) ExpandSyncBurst() []Row {
	burst := "初期(SB1/SB2)"
	if s.B {
		burst = "完了(SB3/SB4)"
	}
	b := uint32(0)
	if s.B {
		b = 1
	}
	return []Row{
		{"B バースト識別", fieldBits(b, 1), int(b), burst},
		{"I アイドル", fieldBits(uint32(s.I), 3), int(s.I), ""},
		{"TA タイムアライメント", fieldBits(uint32(s.TA), 4), int(s.TA), ""},
		{"SSC スーパーフレーム同期カウンタ", fieldBits(uint32(s.SSC), 8), int(s.SSC), s.SSCText()},
		{"ID 呼出符号", fieldBits(s.ID, 24), int(s.ID), s.IDText() + "（§4.1.7.8。下位 23 ビットの内容は規定しない。上位方式が割る）"},
	}
}

func (s SyncBurst) IDText() string {
	kind := "グループ番号"
	if s.ID>>23&1 == 1 {
		kind = "移動局個別番号"
	}
	return fmt.Sprintf("#%06X（%s）", s.ID, kind)
}

func (s SyncBurst) SSCText() string {
	if s.SSC > 251 {
		return fmt.Sprintf("%d（範囲外。規格は 0〜251）", s.SSC)
	}
	return fmt.Sprintf("フレーム位置 %d（SSC mod 18）・第 %d スーパーフレーム", int(s.SSC)%SuperframeFrames, int(s.SSC)/SuperframeFrames+1)
}
