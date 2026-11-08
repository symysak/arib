package server


import (
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/symysak/arib/internal/bitpack"
	"github.com/symysak/arib/internal/data"
	"github.com/symysak/arib/internal/rec"
	dec "github.com/symysak/arib/internal/std-t61/decoder"
	scpca1u292 "github.com/symysak/arib/internal/std-t61/scpc-a1u292"
)

const dataMax = 2000

type DataEvent struct {
	Idx int     `json:"idx"`
	MHz float64 `json:"mhz"`
	data.Event
}

type a1Data struct {
	key *scpca1u292.Keyer
	rd  *data.Station
	tsv *os.File
	day string
}

func CheckDataFormat(name string) error { return data.Valid(scpca1u292.Channel, name) }

func DataFormats() string { return data.Names(scpca1u292.Channel) }

func (p *Pipeline) newA1Data(mhz float64) *a1Data {
	wc, hint := -1, -1
	if w, ok := p.stationWC(mhz); ok {
		wc = w
	} else if p.led != nil {
		if _, w := p.led.Codec(mhz); w >= 0 {
			hint = w
		}
	}
	return &a1Data{key: scpca1u292.NewKeyer(wc, hint), rd: data.NewStation(scpca1u292.Channel, p.cfg.DataFormat)}
}

func (p *Pipeline) onA1U(s *stationState, f dec.Frame) *scpca1u292.Result {
	r := f.A1RICH
	if r == nil || !r.OK || r.IsVoice() || len(f.TCH1) != scpca1u292.Bits || strings.EqualFold(p.cfg.DataFormat, data.Off) {
		return nil
	}
	if s.a1 == nil {
		s.a1 = p.newA1Data(s.st.MHz)
	}
	a := s.a1
	route := "経由"
	if r.REP == 0 {
		route = "直接"
	}
	var cur *scpca1u292.Result
	for _, d := range a.key.Feed(scpca1u292.Item{TCH1: f.TCH1, Soft: f.TCH1Soft, Tag: f.Number}) {
		if d.Tag == f.Number {
			res := d.Result
			cur = &res
		}
		if !d.OK || d.IsIdle() {
			continue
		}
		evs := a.rd.Read(data.Frame{Ch: scpca1u292.Channel, No: d.Tag.(int64), TimeSec: f.TimeSec, Bits: d.Body,
			Route: route, Idle: r.IsIdleLine()})
		for _, ev := range evs {
			p.onDataEvent(s, ev)
		}
	}
	if cur != nil {
		p.mu.Lock()
		s.st.Ext["u_key"] = fmt.Sprintf("%s wc=%d", scpca1u292.Name, cur.WC)
		if n := a.rd.Format(); n != "" {
			s.st.Ext["data_format"] = n
		}
		s.dirty = true
		p.mu.Unlock()
	}
	return cur
}

func (p *Pipeline) onDataEvent(s *stationState, ev data.Event) {
	de := &DataEvent{Idx: s.st.Idx, MHz: s.st.MHz, Event: ev}
	de.TimeSec = p.now()
	kind := string(ev.Kind)
	p.mu.Lock()
	if p.dataEvs == nil {
		p.dataEvs = map[string][]*DataEvent{}
	}
	l := append(p.dataEvs[kind], de)
	if len(l) > dataMax {
		l = l[len(l)-dataMax:]
	}
	p.dataEvs[kind] = l
	p.mu.Unlock()
	p.emit(Event{Type: kind, Data: de})
	if p.led != nil {
		p.led.Confirm(s.st.MHz, time.Now())
	}
	p.addLogFor(s.st.Idx, "data", "%.4f MHz %s", s.st.MHz, ev.Label)
	p.writeDataTSV(s, de)
}

func (p *Pipeline) writeDataTSV(s *stationState, de *DataEvent) {
	if p.cfg.Watch.LogDir == "" || s.a1 == nil {
		return
	}
	a := s.a1
	now := time.Now()
	day := now.Format("2006-01-02")
	if a.tsv == nil || a.day != day {
		if a.tsv != nil {
			a.tsv.Close()
			a.tsv = nil
		}
		path := rec.DataLog(rec.SystemDir(p.cfg.Watch.LogDir, p.cfg.Watch.SystemName()), now, s.st.MHz)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return
		}
		fh, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
		if err != nil {
			return
		}
		if st, err := fh.Stat(); err == nil && st.Size() == 0 {
			fmt.Fprintln(fh, "# 時刻\t種類\t書式\t送り元\t要約\t緯度\t経度\t本体")
		}
		a.tsv, a.day = fh, day
	}
	lat, lon := "", ""
	if de.Pos != nil {
		lat, lon = fmt.Sprintf("%.6f", de.Pos.Lat), fmt.Sprintf("%.6f", de.Pos.Lon)
	}
	fmt.Fprintf(a.tsv, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n", now.Format("15:04:05.000"), de.Kind, de.Format, de.From,
		strings.ReplaceAll(de.Label, "\t", " "), lat, lon, strings.ToUpper(hex.EncodeToString(de.Raw)))
}

func (s *stationState) closeA1Data() {
	if s.a1 != nil && s.a1.tsv != nil {
		s.a1.tsv.Close()
		s.a1.tsv = nil
	}
}

func (p *Pipeline) dataSnapshot(ext map[string]any) map[string]any {
	if len(p.dataEvs) == 0 {
		return ext
	}
	if ext == nil {
		ext = map[string]any{}
	}
	for k, l := range p.dataEvs {
		if _, taken := ext[k]; !taken {
			ext[k] = append([]*DataEvent(nil), l...)
		}
	}
	return ext
}

func uBody(r *scpca1u292.Result) string { return fmt.Sprintf("%X", bitpack.ToBytes(r.Body)) }
