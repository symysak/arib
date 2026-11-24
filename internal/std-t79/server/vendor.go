package server


import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/symysak/arib/internal/rcr39qpsk/decoder"
	"github.com/symysak/arib/internal/rcr39qpsk/l2"
	"github.com/symysak/arib/internal/rec"
	"github.com/symysak/arib/internal/std-t79/l3"
)

const (
	vendorSameSec = 300
	vendorKindSec = 10
	vendorClockSlack = 2
)

type vendorFile struct {
	mu sync.Mutex
}

type vendorState struct {
	seen      map[string]float64
	clockSeen bool
	clockOff  int
	clockText string
	pendOff, pendN int
}

const vendorHeader = "時刻\t周波数[MHz]\t製造者\tスロット\t論理チャネル\tL2 アドレス\t実体\t種別\t独自データ\tメッセージ(16進)\tフレーム(16進)\n"

func (v *vendorFile) write(logDir, line string) {
	if logDir == "" {
		return
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	dir := rec.SystemDir(logDir, System)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return
	}
	path := filepath.Join(dir, "vendor.tsv")
	_, err := os.Stat(path)
	isNew := os.IsNotExist(err)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	if isNew {
		f.WriteString(vendorHeader)
	}
	f.WriteString(line)
}

func (l *link) makerNum() int {
	n := l.maker
	if n == 0 && l.h.Up() {
		n = l.sh.nets.makerAt(l.h.DownMHz())
	}
	return n
}

func (l *link) setMaker(n int, why string) {
	if n <= 0 {
		return
	}
	old := l.maker
	l.maker = n
	if old == n {
		return
	}
	tail := "独自の読みは無い（拡張情報要素などは 16 進のまま）"
	if l3.HasVendor(n) {
		tail = "製造者ごとの独自の読みを使う"
	}
	l.h.Logf(0, "net", "info", "網の製造者 %s（%s）: %s", makerLabel(n), why, tail)
	l.updateVendorInfo()
}

func makerLabel(n int) string {
	if name := l3.MakerName(n); name != "" {
		return fmt.Sprintf("%d %s", n, name)
	}
	return fmt.Sprint(n)
}

func (l *link) updateVendorInfo() {
	n := l.makerNum()
	t := ""
	if n > 0 {
		t = makerLabel(n)
		if !l3.HasVendor(n) {
			t += "（独自の読み無し）"
		}
		if l.vend.clockText != "" {
			t += "・" + l.vend.clockText
		}
	}
	l.h.SetVendor(t)
}

func (l *link) parseL3(info []byte, dir int) l3.Signal {
	maker := l.makerNum()
	x := l3.ParseCtx(info, l3.Ctx{Dir: dir, Maker: maker})
	if maker != 0 || l.h.Up() {
		return x
	}
	for _, m := range x.Messages {
		if b := m.Broadcast; b != nil && b.System != nil && b.System.Maker > 0 {
			l.setMaker(b.System.Maker, "網番号")
			return l3.ParseCtx(info, l3.Ctx{Dir: dir, Maker: b.System.Maker})
		}
	}
	return x
}

func (l *link) actOnVendorData(s *decoder.Slot, ch string, f l2.Frame, raw []byte, sig l3.Signal) {
	for _, m := range sig.Messages {
		if m.Clock != nil {
			l.vendorClock(s, m.Clock)
		}
		if m.Unknown() {
			l.vendorRecord(s, ch, f, raw, m)
		}
	}
}

func (l *link) vendorRecord(s *decoder.Slot, ch string, f l2.Frame, raw []byte, m l3.Message) {
	var what []string
	for _, v := range m.Vendor {
		if !v.Known {
			what = append(what, v.What)
		}
	}
	w := strings.Join(what, "; ")
	if l.vend.seen == nil {
		l.vend.seen = map[string]float64{}
	}
	maker := l.makerNum()
	same := fmt.Sprintf("%d/%s/%x", maker, w, m.Raw)
	kind := fmt.Sprintf("%d/%s/%d/%s", maker, m.Entity, m.Type, w)
	if t, ok := l.vend.seen[same]; ok && s.TSec-t < vendorSameSec {
		return
	}
	if t, ok := l.vend.seen[kind]; ok && s.TSec-t < vendorKindSec {
		return
	}
	l.vend.seen[same], l.vend.seen[kind] = s.TSec, s.TSec
	if len(l.vend.seen) > 2048 {
		for k, t := range l.vend.seen {
			if s.TSec-t > vendorSameSec {
				delete(l.vend.seen, k)
			}
		}
	}
	line := fmt.Sprintf("%s\t%.4f\t%s\t%d\t%s\t%s\t%s\t0x%02X\t%s\t%s\t%s\n",
		l.h.StreamTime().Format(time.RFC3339), l.h.MHz(), makerLabel(l.makerNum()), s.Num, ch, f.Addr.Key(), m.Entity, m.Type, w,
		hexUpper(m.Raw), hexUpper(raw))
	l.sh.vendor.write(l.h.LogDir(), line)
}

func hexUpper(b []byte) string { return strings.ToUpper(fmt.Sprintf("%x", b)) }

func (l *link) vendorClock(s *decoder.Slot, k *l3.Clock) {
	now := l.h.StreamTime()
	off := k.Sec - (now.Hour()*3600 + now.Minute()*60 + now.Second())
	switch {
	case off > 43200:
		off -= 86400
	case off < -43200:
		off += 86400
	}
	if k.Date != "" {
		if d, err := time.ParseInLocation("2006-01-02", k.Date, now.Location()); err == nil {
			off = int(d.Add(time.Duration(k.Sec)*time.Second).Sub(now).Round(time.Second) / time.Second)
		}
	}
	vendor := l3.MakerName(k.ByMaker)
	text := fmt.Sprintf("%s %s（受信の時刻との差 %s）", k.What, k.Text, offText(off))
	if k.PerCall {
		if s.Num < 0 || s.Num >= len(l.st) {
			return
		}
		cs := &l.st[s.Num]
		if cs.vclock == k.Text {
			return
		}
		cs.vclock = k.Text
		l.h.Note(s.Num, fmt.Sprintf("vendor_clock %s（独自・%s・推定）", text, vendor))
		l.h.Logf(s.Num, "call", "info", "ST#%d %s（独自・%s・推定）", s.Num, text, vendor)
		return
	}
	slack := max(vendorClockSlack, 2*k.Step)
	near := func(a, b int) bool { return abs(a-b) <= slack }
	v := &l.vend
	if v.pendN > 0 && near(off, v.pendOff) {
		v.pendN++
	} else {
		v.pendOff, v.pendN = off, 1
	}
	if v.pendN >= 2 && (!v.clockSeen || !near(off, v.clockOff)) {
		l.h.Logf(s.Num, "net", "info", "%s（独自・%s・推定）", text, vendor)
		v.clockSeen, v.clockOff = true, off
	}
	v.clockText = fmt.Sprintf("%s %s（受信の時刻 %s）", k.What, k.Text, offText(off))
	l.updateVendorInfo()
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

func offText(off int) string {
	sign := "+"
	if off < 0 {
		sign, off = "−", -off
	}
	switch {
	case off >= 3600:
		return fmt.Sprintf("%s%d 時間 %d 分 %d 秒", sign, off/3600, off%3600/60, off%60)
	case off >= 60:
		return fmt.Sprintf("%s%d 分 %d 秒", sign, off/60, off%60)
	}
	return fmt.Sprintf("%s%d 秒", sign, off)
}

func (l *link) markData(n int, call *l3.Call) {
	if call == nil || len(call.Bearers) == 0 || !call.Bearers[0].NonVoice || n < 0 || n >= len(l.st) {
		return
	}
	cs := &l.st[n]
	if cs.data {
		return
	}
	cs.data = true
	l.h.Note(n, "data 非音声の通信（伝達能力 "+call.Bearers[0].Text+"）: TCH はデータとして tch.bin に残し、音声の符号器は通さない")
	l.h.Logf(n, "call", "info", "ST#%d 非音声の通信（データ）: %s", n, call.Bearers[0].Text)
}
