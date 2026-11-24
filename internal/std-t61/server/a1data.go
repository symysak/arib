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
	rd212   *data.Station
	use212  bool
	wc212   int
	known   int
	tsv     *os.File
	day     string
	raw     *os.File
	rawDay  string
	rawSeen map[string]float64
}

const URawRepeatSec = 600

var uChannels = []data.Channel{scpca1u292.Channel, scpca1u264.Channel, scpca1u264.DataChannel}

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
	spec212 := dataSpec(scpca1u264.DataChannel, p.cfg.DataFormat)
	a := &a1Data{key: scpca1u292.NewKeyer(wc, hint), rd: data.NewStation(scpca1u292.Channel, dataSpec(scpca1u292.Channel, p.cfg.DataFormat)),
		k264: scpca1u264.NewKeyer(-1, -1), rd264: data.NewStation(scpca1u264.Channel, dataSpec(scpca1u264.Channel, p.cfg.DataFormat)),
		rd212: data.NewStation(scpca1u264.DataChannel, spec212), use212: !strings.EqualFold(spec212, data.Off)}
	if wc > 0 {
		a.known = wc
	} else if hint > 0 {
		a.known = hint
	}
	return a
}

func (a *a1Data) u212Keys() []int {
	var ks []int
	for _, wc := range []int{a.wc212, a.known} {
		if wc > 0 && (len(ks) == 0 || ks[0] != wc) {
			ks = append(ks, wc)
		}
	}
	return ks
}

type uTag struct {
	no      int64
	read212 int
}

func (p *Pipeline) readU212(s *stationState, a *a1Data, f dec.Frame, route string) int {
	for _, wc := range a.u212Keys() {
		d, errs, crc := scpca1u264.DecodeData(f.TCH1, wc)
		if !crc || errs > scpca1u264.DataMaxErrs {
			continue
		}
		a.wc212 = wc
		for _, ev := range a.rd212.Read(data.Frame{Ch: scpca1u264.DataChannel, No: f.Number, TimeSec: f.TimeSec, Bits: d,
			Route: route, Idle: f.A1RICH.IsIdleLine()}) {
			p.onDataEvent(s, ev)
		}
		return wc
	}
	return 0
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
	u292OK := false
	d292 := a.ok292
	_, d264 := a.k264.WC()
	if !d264 {
		for _, d := range a.key.Feed(scpca1u292.Item{TCH1: f.TCH1, Soft: f.TCH1Soft, Tag: f.Number}) {
			if d.Tag == f.Number {
				res := d.Result
				out.U292, u292OK = &res, d.OK
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
	read212 := 0
	if !d292 {
		if a.use212 {
			read212 = p.readU212(s, a, f, route)
		}
		for _, d := range a.k264.Feed(scpca1u264.Item{TCH1: f.TCH1, Tag: uTag{no: f.Number, read212: read212}}) {
			tag := d.Tag.(uTag)
			if tag.no == f.Number {
				res := d.Result
				out.U264 = &res
			}
			bits := append(append([]uint8(nil), d.Body...), d.Check...)
			wc212, dd := tag.read212, []uint8(nil)
			if wc212 == 0 {
				if wc, ok := scpca1u264.WCFromKey(d.WC); ok {
					if x, _, crc := scpca1u264.DecodeData(d.TCH1, wc); crc {
						wc212, dd = wc, x
					}
				}
			}
			via, key := scpca1u264.Name, d.WC
			if wc212 > 0 {
				via, key = scpca1u264.DataName, wc212
			}
			p.writeURaw(s, r, tag.no, d.TCH1, bits, d.Result, via, key)
			if a.use212 && wc212 > 0 {
				if dd != nil {
					a.wc212 = wc212
					for _, ev := range a.rd212.Read(data.Frame{Ch: scpca1u264.DataChannel, No: tag.no, TimeSec: f.TimeSec, Bits: dd,
						Route: route, Idle: r.IsIdleLine()}) {
						p.onDataEvent(s, ev)
					}
				}
				continue
			}
			if !d.OK {
				continue
			}
			for _, ev := range a.rd264.Read(data.Frame{Ch: scpca1u264.Channel, No: tag.no, TimeSec: f.TimeSec, Bits: bits,
				Route: route, Idle: r.IsIdleLine()}) {
				p.onDataEvent(s, ev)
			}
		}
	}
	if u292OK || out.U264 != nil || read212 > 0 {
		p.mu.Lock()
		switch {
		case u292OK:
			s.st.Ext["u_key"] = fmt.Sprintf("%s wc=%d", scpca1u292.Name, out.U292.WC)
			if n := a.rd.Format(); n != "" {
				s.st.Ext["data_format"] = n
			}
		case out.U264 != nil:
			s.st.Ext["u_key"] = fmt.Sprintf("%s wc=%d", scpca1u264.Name, out.U264.WC)
			if a.wc212 > 0 {
				s.st.Ext["u_key"] = fmt.Sprintf("%s wc=%d（%s wc=%d = 音声・制御語と同じ値）", scpca1u264.Name, out.U264.WC, scpca1u264.DataName, a.wc212)
			}
			if n := a.rd264.Format(); n != "" {
				s.st.Ext["data_format"] = n
			}
			if n := a.rd212.Format(); n != "" && a.wc212 > 0 {
				s.st.Ext["data_format"] = n
			}
		default:
			s.st.Ext["u_key"] = fmt.Sprintf("%s wc=%d（音声・制御語と同じ値）", scpca1u264.DataName, a.wc212)
			if n := a.rd212.Format(); n != "" {
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
	key := dataL3Key(s.st.Idx, ev)
	de.TimeSec = p.now()
	if ev.Again {
		p.againL3(key, de)
		return
	}
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
	r := dataL3(de)
	p.storeL3(r)
	p.mu.Lock()
	if p.dataL3 == nil || len(p.dataL3) > 2*maxL3 {
		p.dataL3 = map[string]*L3Rec{}
	}
	p.dataL3[key] = r
	p.mu.Unlock()
}

func dataL3Key(idx int, ev data.Event) string { return fmt.Sprintf("%d\x00%s", idx, ev.Key()) }

func (p *Pipeline) againL3(key string, de *DataEvent) {
	p.mu.Lock()
	r := p.dataL3[key]
	if r == nil {
		p.mu.Unlock()
		return
	}
	if r.Repeat < 1 {
		r.Repeat = 1
	}
	r.Repeat += de.N()
	r.TSec = de.TimeSec
	cp := *r
	p.mu.Unlock()
	p.emit(Event{Type: "l3", L3: &cp})
}

func dataL3(de *DataEvent) *L3Rec {
	tree := []Node{{Label: "書式", Value: de.Format}, {Label: "種類", Value: string(de.Kind)}}
	if de.From != "" {
		tree = append(tree, Node{Label: "送り元", Value: de.From})
	}
	if de.Pos != nil {
		tree = append(tree, Node{Label: "位置", Value: fmt.Sprintf("%.6f, %.6f", de.Pos.Lat, de.Pos.Lon)})
	}
	for _, r := range de.Rows {
		tree = append(tree, Node{Label: r.Name, Value: r.Text, Bits: r.Bits})
	}
	return &L3Rec{Idx: de.Idx, TSec: de.TimeSec, MHz: de.MHz, Entity: "データ", Name: de.Label, Tree: tree, RawHex: hexOf(de.Raw), Repeat: de.N()}
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

func (p *Pipeline) writeURaw(s *stationState, r *l1.A1RICH, no int64, tch1, bits []uint8, res scpca1u264.Result, via string, key int) {
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
	fmt.Fprintf(a.raw, "%s\t%d\t%d\t%d\t%s\t%d\t%s\t%s\t%s\n", now.Format("15:04:05.000"), no, r.CFN, r.FN, via, key, form,
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
