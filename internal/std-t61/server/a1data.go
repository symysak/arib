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
	"github.com/symysak/arib/internal/std-t61/l1"
	scpca1u264 "github.com/symysak/arib/internal/std-t61/scpc-a1u264"
	scpca1u292 "github.com/symysak/arib/internal/std-t61/scpc-a1u292"
)

const dataMax = 2000

type DataEvent struct {
	Idx int     `json:"idx"`
	MHz float64 `json:"mhz"`
	data.Event
}

type a1Data struct {
	key     *scpca1u292.Keyer
	rd      *data.Station
	ok292   bool
	k264    *scpca1u264.Keyer
	rd264   *data.Station
	tsv     *os.File
	day     string
	raw     *os.File
	rawDay  string
	rawSeen map[string]float64
}

const URawRepeatSec = 600

var uChannels = []data.Channel{scpca1u292.Channel, scpca1u264.Channel}

func CheckDataFormat(name string) error {
	err := data.Valid(uChannels[0], name)
	for _, ch := range uChannels[1:] {
		if err == nil {
			return nil
		}
		if data.Valid(ch, name) == nil {
			return nil
		}
	}
	if err != nil {
		return fmt.Errorf("データの書式 %q は無い（%s・auto・off）", name, data.Names(uChannels...))
	}
	return nil
}

func DataFormats() string { return data.Names(uChannels...) }

func dataSpec(ch data.Channel, spec string) string {
	if spec == "" || strings.EqualFold(spec, data.Auto) || strings.EqualFold(spec, data.Off) {
		return spec
	}
	if data.Valid(ch, spec) != nil {
		return data.Off
	}
	return spec
}

func (p *Pipeline) newA1Data(mhz float64) *a1Data {
	wc, hint := -1, -1
	if w, ok := p.stationWC(mhz); ok {
		wc = w
	} else if p.led != nil {
		if _, w := p.led.Codec(mhz); w >= 0 {
			hint = w
		}
	}
	return &a1Data{key: scpca1u292.NewKeyer(wc, hint), rd: data.NewStation(scpca1u292.Channel, dataSpec(scpca1u292.Channel, p.cfg.DataFormat)),
		k264: scpca1u264.NewKeyer(-1, -1), rd264: data.NewStation(scpca1u264.Channel, dataSpec(scpca1u264.Channel, p.cfg.DataFormat))}
}

type uRes struct {
	U292 *scpca1u292.Result
	U264 *scpca1u264.Result
}

func (p *Pipeline) onA1U(s *stationState, f dec.Frame) uRes {
	r := f.A1RICH
	if r == nil || !r.OK || r.IsVoice() || len(f.TCH1) != scpca1u292.Bits || strings.EqualFold(p.cfg.DataFormat, data.Off) {
		return uRes{}
	}
	if s.a1 == nil {
		s.a1 = p.newA1Data(s.st.MHz)
	}
	a := s.a1
	route := "経由"
	if r.REP == 0 {
		route = "直接"
	}
	var out uRes
	d292 := a.ok292
	_, d264 := a.k264.WC()
	if !d264 {
		for _, d := range a.key.Feed(scpca1u292.Item{TCH1: f.TCH1, Soft: f.TCH1Soft, Tag: f.Number}) {
			if d.Tag == f.Number {
				res := d.Result
				out.U292 = &res
			}
			if d.OK {
				a.ok292 = true
			}
			if !d.OK || d.IsIdle() {
				continue
			}
			for _, ev := range a.rd.Read(data.Frame{Ch: scpca1u292.Channel, No: d.Tag.(int64), TimeSec: f.TimeSec, Bits: d.Body,
				Route: route, Idle: r.IsIdleLine()}) {
				p.onDataEvent(s, ev)
			}
		}
		d292 = a.ok292
	}
	if !d292 {
		for _, d := range a.k264.Feed(scpca1u264.Item{TCH1: f.TCH1, Tag: f.Number}) {
			if d.Tag == f.Number {
				res := d.Result
				out.U264 = &res
			}
			bits := append(append([]uint8(nil), d.Body...), d.Check...)
			p.writeURaw(s, r, d.Tag.(int64), d.TCH1, bits, d.Result)
			if !d.OK {
				continue
			}
			for _, ev := range a.rd264.Read(data.Frame{Ch: scpca1u264.Channel, No: d.Tag.(int64), TimeSec: f.TimeSec, Bits: bits,
				Route: route, Idle: r.IsIdleLine()}) {
				p.onDataEvent(s, ev)
			}
		}
	}
	if out.U292 != nil || out.U264 != nil {
		p.mu.Lock()
		if out.U292 != nil {
			s.st.Ext["u_key"] = fmt.Sprintf("%s wc=%d", scpca1u292.Name, out.U292.WC)
			if n := a.rd.Format(); n != "" {
				s.st.Ext["data_format"] = n
			}
		} else {
			s.st.Ext["u_key"] = fmt.Sprintf("%s wc=%d", scpca1u264.Name, out.U264.WC)
			if n := a.rd264.Format(); n != "" {
				s.st.Ext["data_format"] = n
			}
		}
		s.dirty = true
		p.mu.Unlock()
	}
	return out
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

func (p *Pipeline) writeURaw(s *stationState, r *l1.A1RICH, no int64, tch1, bits []uint8, res scpca1u264.Result) {
	if p.cfg.Watch.LogDir == "" || s.a1 == nil {
		return
	}
	a := s.a1
	now := time.Now()
	h := strings.ToUpper(hex.EncodeToString(bitpack.ToBytes(bits)))
	sec := float64(now.UnixNano()) / 1e9
	if a.rawSeen == nil {
		a.rawSeen = map[string]float64{}
	}
	for k, t := range a.rawSeen {
		if sec-t > URawRepeatSec {
			delete(a.rawSeen, k)
		}
	}
	if _, dup := a.rawSeen[h]; dup {
		return
	}
	a.rawSeen[h] = sec
	day := now.Format("2006-01-02")
	if a.raw == nil || a.rawDay != day {
		if a.raw != nil {
			a.raw.Close()
			a.raw = nil
		}
		path := rec.URawLog(rec.SystemDir(p.cfg.Watch.LogDir, p.cfg.Watch.SystemName()), now, s.st.MHz)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return
		}
		fh, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
		if err != nil {
			return
		}
		if st, err := fh.Stat(); err == nil && st.Size() == 0 {
			fmt.Fprintln(fh, "# 時刻\t無線フレーム\tCFN\tFN\t伝送路\t鍵\t形\tマスクを外した 292 ビット\t送ったままの TCH1 292 ビット")
		}
		a.raw, a.rawDay = fh, day
	}
	form := "形が合わない"
	switch {
	case res.Long:
		form = "長い形"
	case res.OK:
		form = "短い形"
	}
	fmt.Fprintf(a.raw, "%s\t%d\t%d\t%d\t%s\t%d\t%s\t%s\t%s\n", now.Format("15:04:05.000"), no, r.CFN, r.FN, scpca1u264.Name, res.WC, form,
		h, strings.ToUpper(hex.EncodeToString(bitpack.ToBytes(tch1))))
}

func (s *stationState) closeA1Data() {
	if s.a1 == nil {
		return
	}
	if s.a1.tsv != nil {
		s.a1.tsv.Close()
		s.a1.tsv = nil
	}
	if s.a1.raw != nil {
		s.a1.raw.Close()
		s.a1.raw = nil
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

func uWords(r *scpca1u264.Result) string {
	var sb strings.Builder
	for i, w := range r.Words() {
		if i > 0 {
			sb.WriteByte(' ')
		}
		if i == len(r.Words())-1 {
			fmt.Fprintf(&sb, "%02X", w)
		} else {
			fmt.Fprintf(&sb, "%04X", w)
		}
	}
	return sb.String()
}
