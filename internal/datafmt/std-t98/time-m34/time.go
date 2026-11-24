package timem34

import (
	"fmt"
	"time"

	"github.com/symysak/arib/internal/data"
	"github.com/symysak/arib/internal/std-t98/fsk4/l1"
)

const Name = "time-m34"

func init() {
	data.Register(data.Format{
		Name:  Name,
		Desc:  "製造者番号 34 の機器の日付と時刻の配信（非音声 1。PN9 のスクランブル + CRC-16。送信ごとに 1 件）",
		Ch:    l1.DataChannel,
		New:   func(data.Options) (data.Reader, error) { return &reader{}, nil },
		Probe: probe,
	})
}

const (
	FrameBits = 288
	frameLen  = FrameBits / 8
	GapSec = 20.0
	slackSec = 3.0
)

var Keystream = func() [frameLen]byte {
	s := []uint8{0, 1, 1, 0, 0, 1, 0, 0, 1}
	for len(s) < FrameBits {
		s = append(s, s[len(s)-5]^s[len(s)-9])
	}
	var out [frameLen]byte
	for i, b := range s {
		out[i/8] |= b << (7 - i%8)
	}
	return out
}()

type Msg struct {
	Plain [frameLen]byte
	CRCOK bool
	Kind uint16
	Time   time.Time
	TimeOK bool
}

func Decode(bits []uint8) (m Msg, ok bool) {
	if len(bits) != FrameBits {
		return m, false
	}
	for i, b := range bits {
		m.Plain[i/8] |= (b & 1) << (7 - i%8)
	}
	for i := range m.Plain {
		m.Plain[i] ^= Keystream[i]
	}
	m.CRCOK = CRC16(m.Plain[3:32]) == uint16(m.Plain[32])<<8|uint16(m.Plain[33])
	m.Kind = uint16(m.Plain[3])<<8 | uint16(m.Plain[4])
	m.Time, m.TimeOK = bcdTime(m.Plain[5:11])
	return m, true
}

func CRC16(b []byte) uint16 {
	var r uint16
	for _, x := range b {
		r ^= uint16(x) << 8
		for i := 0; i < 8; i++ {
			if r&0x8000 != 0 {
				r = r<<1 ^ 0x1021
			} else {
				r <<= 1
			}
		}
	}
	return r
}

func bcd(b byte) (int, bool) {
	hi, lo := int(b>>4), int(b&0x0F)
	if hi > 9 || lo > 9 {
		return 0, false
	}
	return hi*10 + lo, true
}

func bcdTime(b []byte) (time.Time, bool) {
	var v [6]int
	for i := range v {
		n, ok := bcd(b[i])
		if !ok {
			return time.Time{}, false
		}
		v[i] = n
	}
	if v[1] < 1 || v[1] > 12 || v[2] < 1 || v[2] > 31 || v[3] > 23 || v[4] > 59 || v[5] > 59 {
		return time.Time{}, false
	}
	t := time.Date(2000+v[0], time.Month(v[1]), v[2], v[3], v[4], v[5], 0, time.UTC)
	if t.Day() != v[2] {
		return time.Time{}, false
	}
	return t, true
}

func (m Msg) Valid() bool { return m.CRCOK && m.Kind == KindTime && m.TimeOK }

const KindTime = 0x0100

func probe(frames []data.Frame) (ok, n int) {
	for _, f := range frames {
		m, good := Decode(f.Bits)
		if !good || !m.CRCOK {
			continue
		}
		n++
		if m.Valid() {
			ok++
		}
	}
	return ok, n
}

type reader struct {
	have    bool
	tx      int64
	lastT   float64
	first   Msg
	firstF  data.Frame
	emitted bool
	ev      data.Event
	n       int
}

func (r *reader) newTx(f data.Frame) bool {
	if !r.have {
		return true
	}
	if f.Tx != 0 || r.tx != 0 {
		return f.Tx != r.tx
	}
	return f.TimeSec-r.lastT > GapSec
}

func (r *reader) Read(f data.Frame) []data.Event {
	m, ok := Decode(f.Bits)
	if !ok || !m.Valid() {
		return nil
	}
	if r.newTx(f) {
		r.have, r.tx, r.first, r.firstF, r.emitted, r.n = true, f.Tx, m, f, false, 0
	}
	r.lastT = f.TimeSec
	r.n++
	if r.emitted {
		return []data.Event{data.AgainOf(r.ev, f, 1)}
	}
	if m.Time.Equal(r.first.Time) {
		return nil
	}
	r.emitted = true
	dt := m.Time.Sub(r.first.Time).Seconds() - (f.TimeSec - r.firstF.TimeSec)
	if dt >= -slackSec && dt <= slackSec {
		r.ev = event(r.firstF, r.first)
	} else {
		r.ev = event(f, m)
	}
	r.ev.Count = r.n
	return []data.Event{r.ev}
}

func label(m Msg) string { return "時刻の配信 " + m.Time.Format("2006-01-02 15:04:05") }

func event(f data.Frame, m Msg) data.Event {
	rows := []data.Row{
		{Name: "種別", Text: fmt.Sprintf("%02X %02X（日付と時刻）", m.Plain[3], m.Plain[4])},
		{Name: "配った時刻", Text: m.Time.Format("2006-01-02 15:04:05")},
		{Name: "CRC-16", Text: fmt.Sprintf("%02X%02X（合った）", m.Plain[32], m.Plain[33])},
		{Name: "見出し・末尾", Text: fmt.Sprintf("% X … % X", m.Plain[0:3], m.Plain[34:36])},
	}
	return data.Event{Kind: data.KindMessage, No: f.No, TimeSec: f.TimeSec, Format: Name, Label: label(m), Rows: rows,
		Raw: append([]byte(nil), m.Plain[:]...)}
}
