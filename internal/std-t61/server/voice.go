package server

import (
	"encoding/binary"
	"fmt"
	"math"
	"os"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/symysak/arib/internal/iqsrc"
	"github.com/symysak/arib/internal/rec"
	dec "github.com/symysak/arib/internal/std-t61/decoder"
	t61voice "github.com/symysak/arib/internal/std-t61/voice"
	"github.com/symysak/arib/internal/voice"
	"github.com/symysak/arib/internal/wavio"
)


const codecPending = 75

const (
	codecMinChecks = 10
	codecMinRatio  = 0.5
)

type voiceRun struct {
	finished bool
	p        *Pipeline
	s        *stationState
	idx      int
	mhz      float64
	chosen *voice.Codec
	tr    *voice.Tracker
	cands []*codecScore
	pend  []pendFrame
	curT float64
	tNow atomic.Uint64
	skipStart bool
	up upGate
	setUpScramble func(init int)

	mu  sync.Mutex
	seq int64
	recOn  bool
	base   string
	reason string
	pcm    []int16
	ring   []int16
	ringN  int
	evs    []timedLine
	pre    float64
	log    *os.File
	tch    *os.File
	tchIdx *os.File
}

type timedLine struct {
	t    float64
	line string
}

type codecScore struct {
	c        voice.Codec
	v        voice.Voice
	n, ok    int
	failedAt int
}

type pendFrame struct {
	f       voice.Frame
	isVoice bool
}

func codecByName(name string) (voice.Codec, bool) {
	for _, c := range voice.Codecs(t61voice.TCH, t61voice.TCH1) {
		if strings.EqualFold(c.Name, name) {
			return c, true
		}
	}
	return voice.Codec{}, false
}

func CheckCodec(name string) error {
	if name == "" || name == "auto" {
		return nil
	}
	c, ok := codecByName(name)
	if !ok {
		return fmt.Errorf("知らない符号器 %q（auto / %s）", name, voice.Names(t61voice.TCH, t61voice.TCH1))
	}
	if c.Ch != t61voice.TCH && c.Ch != t61voice.TCH1 {
		return fmt.Errorf("符号器 %q は論理チャネル %s を読むが、復号器がまだ %s を出さない", c.Name, c.Ch, c.Ch)
	}
	return nil
}

func CheckCodecOptions(name string, opt voice.Options) (notes []string, err error) {
	if name == "" || name == "auto" || name == "off" {
		if len(opt) != 0 {
			return nil, fmt.Errorf("-codec-opt は -codec <符号器の名前> と一緒に使う（auto / off には設定が無い）")
		}
		return nil, nil
	}
	if err := CheckCodec(name); err != nil {
		return nil, err
	}
	c, _ := codecByName(name)
	v, err := c.New(opt)
	if err != nil {
		return nil, fmt.Errorf("-codec %s: %v", c.Name, err)
	}
	v.Close()
	if c.Available != nil && !c.Available() {
		notes = append(notes, fmt.Sprintf("注意: 符号器 %s の外部デコーダが無いので音声 WAV は書けません（通話ログと TCH は残します）。", c.Name))
	}
	if c.Tentative {
		notes = append(notes, fmt.Sprintf("注意: 符号器 %s の伝送路は未確定です（候補のひとつで復号するだけ。録音の .calls.txt に「未確定」と書きます）。", c.Name))
	}
	return notes, nil
}

func newVoiceRun(p *Pipeline, s *stationState) *voiceRun {
	r := &voiceRun{p: p, s: s, idx: s.st.Idx, mhz: s.st.MHz, pre: p.cfg.Watch.PreS}
	r.ringN = int(r.pre * voice.SampleRate)
	if p.cfg.TCHDump && (p.cfg.Codec == "" || p.cfg.Codec == "auto") {
		r.tr = r.newTracker(nil, "")
	}
	switch p.cfg.Codec {
	case "":
		return r
	case "auto":
	default:
		if c, ok := codecByName(p.cfg.Codec); ok {
			if c.Available != nil && !c.Available() {
				r.start(nil, c.Name, "指定。外部デコーダが無いので鳴らさない")
			} else {
				r.start(&c, c.Name, "指定")
			}
		}
		return r
	}
	for _, c := range voice.Codecs(t61voice.TCH, t61voice.TCH1) {
		if (c.Ch != t61voice.TCH && c.Ch != t61voice.TCH1) || c.Tentative || c.ManualOnly || (c.Available != nil && !c.Available()) {
			continue
		}
		if v, err := c.New(r.codecOpts(c, nil)); err == nil {
			r.cands = append(r.cands, &codecScore{c: c, v: v})
		}
	}
	return r
}

func (r *voiceRun) codecOpts(c voice.Codec, base voice.Options) voice.Options {
	o := voice.Options{}
	for k, v := range base {
		o[k] = v
	}
	if c.Ch == t61voice.TCH1 {
		if wc, ok := r.p.stationWC(r.mhz); ok {
			o["wc"] = fmt.Sprint(wc)
		} else if _, fixed := o["wc"]; !fixed {
			if h := r.ledgerHint(); h != "" {
				o["hint"] = h
			}
		}
	}
	return o
}

func (r *voiceRun) ledgerHint() string {
	if r.p.led == nil {
		return ""
	}
	name, wc := r.p.led.Codec(r.mhz)
	if name == "" || wc < 0 {
		return ""
	}
	if c, ok := codecByName(name); !ok || c.Ch != t61voice.TCH1 {
		return ""
	}
	return fmt.Sprintf("%s:%d", name, wc)
}

func (r *voiceRun) recordChoice(name string, wc int) {
	if r.p.led != nil && name != "" {
		r.p.led.SetCodec(r.mhz, name, wc)
	}
}

func (r *voiceRun) start(codec *voice.Codec, name, why string) {
	old := r.tr
	tr := r.newTracker(codec, name)
	r.tr = tr
	if old != nil {
		r.skipStart = old.InCall()
		old.Finish()
		r.event(r.curT, "voice_note", "符号器が決まった: "+name+"（"+why+"）")
	}
	r.p.mu.Lock()
	r.p.voiceCodec = name + "（" + why + "）"
	r.p.mu.Unlock()
	if codec != nil && codec.Ch == t61voice.TCH {
		r.recordChoice(codec.Name, -1)
	}
	r.p.addLogFor(r.idx, "call", "音声の符号器: %s（%s）", name, why)
	if r.p.cfg.Codec == "auto" {
		r.p.notice("%.4f MHz 音声の符号器: %s（%s）", r.mhz, name, why)
	}
	for _, s := range r.cands {
		s.v.Close()
	}
	r.cands = nil
	r.mu.Lock()
	if r.recOn && r.log == nil {
		r.openLogLocked()
	}
	r.mu.Unlock()
	for _, pf := range r.pend {
		r.curT = pf.f.TimeSec
		tr.Push(pf.f, pf.isVoice)
	}
	r.pend = nil
	r.skipStart = false
}

func (r *voiceRun) newTracker(codec *voice.Codec, name string) *voice.Tracker {
	tr := voice.New()
	tr.Lossless = !iqsrc.IsNetwork(r.p.cfg.Source)
	check, tentative := "検査", ""
	if codec != nil {
		cc := *codec
		r.chosen = &cc
		check = cc.Check
		if cc.Tentative {
			tentative = " 未確定（伝送路が候補のひとつ）"
		}
		opt := r.codecOpts(cc, r.p.cfg.CodecOpt)
		tr.NewCodec = func() (voice.Voice, error) { return cc.New(opt) }
		tr.Sink = r.sink
	}
	tr.OnStart = func(voice.Frame) {
		if r.tr == tr && r.skipStart {
			r.skipStart = false
			return
		}
		if r.tr == tr {
			r.event(r.curT, "voice_start", name+tentative)
		}
	}
	tr.OnEnd = func(cl voice.Call) {
		if r.tr != tr {
			return
		}
		text := fmt.Sprintf("音声 %d フレーム（%.1f 秒） %s OK %d", cl.Frames, float64(cl.Frames)*voice.FrameSec, check, cl.OK)
		r.event(cl.TimeSec, "voice_end", text+tentative)
		r.p.addLogFor(r.idx, "call", "%s", text)
	}
	notes := map[string]bool{}
	tr.OnUnit = func(u voice.Unit, _, _ int64) {
		if r.tr == tr && u.Note != "" && !notes[u.Note] {
			notes[u.Note] = true
			r.event(r.curT, "voice_note", u.Note)
			r.p.addLogFor(r.idx, "call", "音声: %s", u.Note)
		}
		if r.tr == tr && u.Choice != nil {
			wc := -1
			if v, ok := u.Choice.Opts["wc"]; ok {
				fmt.Sscan(v, &wc)
			}
			r.recordChoice(u.Choice.Codec, wc)
		}
	}
	return tr
}

func (r *voiceRun) frame(f dec.Frame) {
	isVoice := f.RICH.OK && f.RICH.IsVoice() && len(f.TCH) == 256 && !f.TCHIdle
	vf := voice.Frame{Ch: t61voice.TCH, No: f.Number, TimeSec: f.TimeSec, Bits: f.TCH, Soft: f.TCHSoft}
	if f.A1RICH != nil {
		isVoice = f.A1RICH.IsVoice() && len(f.TCH1) == 292
		vf = voice.Frame{Ch: t61voice.TCH1, No: f.Number, TimeSec: f.TimeSec, Bits: f.TCH1, Soft: f.TCH1Soft}
	}
	r.tNow.Store(math.Float64bits(f.TimeSec))
	if f.Uplink && f.A1RICH == nil && r.upFrame(f, vf, isVoice) {
		return
	}
	r.deliver(f, vf, isVoice)
}

func (r *voiceRun) deliver(f dec.Frame, vf voice.Frame, isVoice bool) {
	if isVoice && r.p.cfg.TCHDump {
		r.dumpTCH(vf)
	}
	if r.tr != nil {
		r.curT = f.TimeSec
		r.tr.Push(vf, isVoice)
		if r.chosen != nil || r.cands == nil {
			return
		}
	}
	if r.cands == nil {
		return
	}
	if b := BearerCodec(f); b != "" {
		if c, ok := codecByName(b); ok && c.Ch == vf.Ch {
			r.start(&c, c.Name, "呼設定の伝達能力 "+b)
			r.curT = f.TimeSec
			r.tr.Push(vf, isVoice)
			return
		}
	}
	r.pend = append(r.pend, pendFrame{vf, isVoice})
	if len(r.pend) > codecPending {
		r.pend = r.pend[len(r.pend)-codecPending:]
	}
	for _, s := range r.cands {
		if s.c.Ch != vf.Ch {
			continue
		}
		if !isVoice {
			s.v.Erase()
			continue
		}
		u, ok, err := s.v.Transport(vf)
		if err != nil || !ok || !u.Check.Valid {
			continue
		}
		s.n++
		if u.Check.OK {
			s.ok++
		}
	}
	sort.SliceStable(r.cands, func(i, j int) bool { return ratio(r.cands[i]) > ratio(r.cands[j]) })
	if best := r.cands; len(best) > 0 && best[0].n >= codecMinChecks && ratio(best[0]) >= codecMinRatio {
		b := best[0]
		r.start(&b.c, b.c.Name, fmt.Sprintf("伝送路の検査 %s %d/%d", b.c.Check, b.ok, b.n))
	}
}

func ratio(s *codecScore) float64 {
	if s.n == 0 {
		return 0
	}
	return float64(s.ok) / float64(s.n)
}

func (r *voiceRun) sink(pcm []int16) {
	r.mu.Lock()
	seq := r.seq
	r.seq += int64(len(pcm))
	if r.recOn {
		r.pcm = append(r.pcm, pcm...)
	} else {
		r.ring = append(r.ring, pcm...)
		if over := len(r.ring) - r.ringN; over > 0 {
			r.ring = append(r.ring[:0], r.ring[over:]...)
		}
	}
	r.mu.Unlock()
	if r.p.audio != nil {
		b := make([]byte, 12+2*len(pcm))
		binary.LittleEndian.PutUint64(b[0:8], uint64(seq))
		binary.LittleEndian.PutUint16(b[8:10], uint16(r.idx))
		binary.LittleEndian.PutUint16(b[10:12], uint16(voice.SampleRate))
		for i, v := range pcm {
			binary.LittleEndian.PutUint16(b[12+2*i:], uint16(v))
		}
		r.p.audio.Broadcast(b)
	}
	if r.p.cfg.AudioTap != nil {
		r.p.cfg.AudioTap(r.idx, pcm, voice.SampleRate)
	}
}

func (r *voiceRun) recordStart(base, reason string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.base, r.reason, r.recOn = base, reason, true
	r.pcm = append(r.pcm[:0], r.ring...)
	r.ring = r.ring[:0]
	if r.tr != nil {
		r.openLogLocked()
	}
	r.evs = r.evs[:0]
	if r.p.cfg.TCHDump {
		r.tch, r.tchIdx = r.create(rec.Path(base, rec.TCH)), r.create(rec.Path(base, rec.TCHIdx))
	}
}

func (r *voiceRun) openLogLocked() {
	r.log = r.create(rec.Path(r.base, rec.Calls))
	if r.log == nil {
		return
	}
	fmt.Fprintf(r.log, "%.4f MHz 録音開始（%s）\n", r.mhz, r.reason)
	for _, l := range r.evs {
		r.log.WriteString("（録音開始前）" + l.line)
	}
	r.evs = r.evs[:0]
}

func (r *voiceRun) recordStop() (audio bool, sec float64) {
	r.mu.Lock()
	r.recOn = false
	pcm, base, log := r.pcm, r.base, r.log
	r.pcm, r.log = nil, nil
	for _, f := range []**os.File{&r.tch, &r.tchIdx} {
		if *f != nil {
			(*f).Close()
			*f = nil
		}
	}
	r.mu.Unlock()
	if len(pcm) > 0 {
		name := rec.Path(base, rec.Audio)
		if err := wavio.Write(name, pcm, 1, voice.SampleRate); err != nil {
			if log != nil {
				fmt.Fprintln(log, "音声 WAV 失敗:", err)
			}
		} else {
			audio, sec = true, float64(len(pcm))/voice.SampleRate
			if log != nil {
				fmt.Fprintf(log, "音声 %.1f 秒 → %s\n", sec, name)
			}
		}
	}
	if log != nil {
		log.Close()
	}
	return audio, sec
}

func (r *voiceRun) seqNote(kind, text string) {
	r.event(math.Float64frombits(r.tNow.Load()), kind, text)
}

func (r *voiceRun) event(t float64, kind, text string) {
	line := fmt.Sprintf("%9.3fs %-11s %s\n", t, kind, text)
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.log != nil {
		r.log.WriteString(line)
		return
	}
	r.evs = append(r.evs, timedLine{t, line})
	for len(r.evs) > 0 && r.evs[0].t < t-r.pre {
		r.evs = r.evs[1:]
	}
}

func (r *voiceRun) dumpTCH(f voice.Frame) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.tch == nil {
		return
	}
	b := make([]byte, (len(f.Bits)+7)/8)
	for i, v := range f.Bits {
		if v&1 != 0 {
			b[i/8] |= 0x80 >> (i % 8)
		}
	}
	r.tch.Write(b)
	if r.tchIdx != nil {
		var n [4]byte
		binary.LittleEndian.PutUint32(n[:], uint32(f.No))
		r.tchIdx.Write(n[:])
	}
}

func (r *voiceRun) create(path string) *os.File {
	f, err := os.Create(path)
	if err != nil {
		r.p.notice("%s を作れません: %v", path, err)
		return nil
	}
	return f
}

func (r *voiceRun) finish() {
	if r.finished {
		return
	}
	r.finished = true
	r.flushUp()
	for _, s := range r.cands {
		s.v.Close()
	}
	if r.tr != nil {
		r.tr.Finish()
		if err := r.tr.Error(); err != nil {
			r.p.addLogFor(r.idx, "call", "音声: %v", err)
			r.p.notice("%.4f MHz 音声: %v", r.mhz, err)
		}
	}
}

func (p *Pipeline) notice(format string, a ...any) {
	if w := p.cfg.Watch.LogOut; w != nil {
		fmt.Fprintf(w, "%s %s\n", time.Now().Format("2006-01-02 15:04:05"), fmt.Sprintf(format, a...))
	}
}

func BearerCodec(f dec.Frame) string {
	for _, m := range f.Messages {
		if m.L3 == nil {
			continue
		}
		for _, msg := range m.L3.Messages {
			for _, fl := range msg.Fields {
				i := strings.Index(fl.Value, "L1=")
				if i < 0 {
					continue
				}
				v := fl.Value[i+3:]
				if j := strings.IndexAny(v, "、, ["); j >= 0 {
					v = v[:j]
				}
				if v != "" && v != "本標準速度整合" {
					return v
				}
			}
		}
	}
	return ""
}

func (p *Pipeline) VoiceCodec() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.voiceCodec
}
