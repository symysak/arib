package callseq


import (
	"fmt"
	"strings"

	"github.com/symysak/arib/internal/std-t61/l3"
)

const mmIdleSec = 20.0

var mmProc = map[uint8]struct{ proc, sec string }{
	0x21: {"位置登録", "4.3.8.8.1"},
	0x24: {"グループ設定情報付位置登録", "4.3.8.8.2"},
	0x41: {"グループ設定変更", "4.3.8.16"},
}

var mmResult = map[uint8]struct {
	ok     bool
	detail string
}{
	0x22: {true, "位置登録を受け付けた"},
	0x23: {false, "位置登録拒否"},
	0x25: {true, "位置登録・設定グループ番号とも受け付けた"},
	0x26: {false, "位置登録は受け付けたが設定グループ番号は受け付けられなかった"},
	0x27: {false, "位置登録は受け付けられなかったが設定グループ番号は受け付けた"},
	0x28: {false, "位置登録・設定グループ番号とも受け付けられなかった"},
	0x42: {true, "グループ設定変更 OK"},
	0x43: {false, "グループ設定変更 NG"},
}

func field(x l3.Message, prefix string) string {
	var out []string
	for _, f := range x.Fields {
		if strings.HasPrefix(f.Name, prefix) {
			out = append(out, f.Value)
		}
	}
	return strings.Join(out, " / ")
}

func (t *Tracker) mm(m Msg, x l3.Message, who string) []Update {
	key := ctlKey(m.MHz, m.Addr) + "/MM"
	c := t.open[key]
	var ups []Update
	if p, ok := mmProc[x.Type]; ok {
		if c != nil && c.Proc != p.proc {
			ups = append(ups, t.close(c, m.TSec, "不明", "応答を見ないまま別の手順が始まった"))
			c = nil
		}
		if c == nil {
			c = t.newCall(m.TSec, m.Station, m.MHz, m.Addr)
			c.Proc, c.Type, c.Section, c.Basis, c.Phase, c.ctlKey, c.mm = p.proc, p.proc, p.sec, "MM のメッセージ種別", "要求", key, true
			t.open[key] = c
			text := fmt.Sprintf("%s: %s", who, x.Name)
			if v := field(x, "登録番号"); v != "" {
				text += "（登録番号 " + v + "）"
			}
			if v := field(x, "設定グループ番号"); v != "" {
				text += "（設定グループ番号 " + v + "）"
			}
			c.mmWho = who
			ups = append(ups, t.event(c, m.TSec, m.Station, m.MHz, "mm", text))
		} else {
			c.mmRetry++
			ups = append(ups, t.event(c, m.TSec, m.Station, m.MHz, "mm", fmt.Sprintf("%s: %s（再送 %d 回目）", who, x.Name, c.mmRetry)))
		}
		c.Last = m.TSec
		return ups
	}
	r, ok := mmResult[x.Type]
	if !ok {
		return nil
	}
	if c == nil {
		proc := "位置登録"
		switch {
		case x.Type >= 0x25 && x.Type <= 0x28:
			proc = "グループ設定情報付位置登録"
		case x.Type == 0x42 || x.Type == 0x43:
			proc = "グループ設定変更"
		}
		c = t.newCall(m.TSec, m.Station, m.MHz, m.Addr)
		c.Proc, c.Type, c.Basis, c.ctlKey, c.mm = proc, proc, "MM のメッセージ種別", key, true
		for _, p := range mmProc {
			if p.proc == proc {
				c.Section = p.sec
			}
		}
		t.open[key] = c
		ups = append(ups, t.event(c, m.TSec, m.Station, m.MHz, "note", "要求は見ていない（応答だけを受けた）"))
	} else if c.mmWho != "" && c.mmWho == who {
		ups = append(ups, t.event(c, m.TSec, m.Station, m.MHz, "anomaly",
			fmt.Sprintf("応答 %s が要求と同じ向き（%s）から来た（§4.3.6.2）", x.Name, who)))
		c.Anomalies++
	}
	text := fmt.Sprintf("%s: %s", who, x.Name)
	detail := r.detail
	if v := field(x, "理由表示"); v != "" {
		text += "（" + v + "）"
		detail += "・理由 " + v
	}
	ups = append(ups, t.event(c, m.TSec, m.Station, m.MHz, "mm", text))
	c.Phase = "応答"
	outcome := "正常終了"
	if !r.ok {
		outcome = "異常"
	}
	return append(ups, t.close(c, m.TSec, outcome, detail))
}
