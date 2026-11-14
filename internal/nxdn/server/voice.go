package server


import (
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/symysak/arib/internal/nxdn/decoder"
	"github.com/symysak/arib/internal/nxdn/l1"
	"github.com/symysak/arib/internal/rec"
	"github.com/symysak/arib/internal/voice"
	"github.com/symysak/arib/internal/voice/agc"
	"github.com/symysak/arib/internal/wavio"
)

const CodecMinChecks = 10

const (
	codecPending   = 75
	codecMinChecks = CodecMinChecks
	codecMinRatio  = 0.5
	codecWindow    = 2 * codecMinChecks
)

func Codecs(ch voice.Channel) []voice.Codec { return voice.Codecs(ch) }

func CodecNames(ch voice.Channel) string { return voice.Names(ch) }

func CheckCodec(ch voice.Channel, name string, opt voice.Options) (notes []string, err error) {
	switch name {
	case "", "off", "auto":
		if len(opt) != 0 {
			return nil, fmt.Errorf("-codec-opt は -codec <符号器の名前> と一緒に使う（auto / off には設定が無い）")
		}
		if name == "auto" && len(Codecs(ch)) == 0 {
			notes = append(notes, "注意: TCH を読む符号器が 1 つも登録されていないので音声は出ません（TCH の生ビットは残します）。")
		}
		return notes, nil
	}
	c, ok := voice.Lookup(ch, name)
	if !ok {
		return nil, fmt.Errorf("知らない符号器 %q（auto / off / %s）", name, CodecNames(ch))
	}
	v, err := c.New(opt)
	if err != nil {
		return nil, fmt.Errorf("-codec %s: %v", c.Name, err)
	}
	v.Close()
	if c.Available != nil && !c.Available() {
		notes = append(notes, fmt.Sprintf("注意: 符号器 %s の外部デコーダが無いので音声 WAV は書けません（送信の記録と TCH は残します）。", c.Name))
	}
	return notes, nil
}

type codecScore struct {
	c     voice.Codec
	v     voice.Voice
	n, ok int
	win   [codecWindow]bool
	next  int
}

func (s *codecScore) add(ok bool) {
	if s.n == codecWindow {
		if s.win[s.next] {
			s.ok--
		}
	} else {
		s.n++
	}
	s.win[s.next] = ok
	if ok {
		s.ok++
	}
	s.next = (s.next + 1) % codecWindow
}

func (s *codecScore) ratio() float64 {
	if s.n == 0 {
		return 0
	}
	return float64(s.ok) / float64(s.n)
}

type audioBuf struct {
	mu  sync.Mutex
	pcm []int16
}

type voiceState struct {
	mode   string
	chosen *voice.Codec
	why    string
	cands  []*codecScore
	tried  bool
	hist []voice.Frame
	trial *codecTrial
	hint string
	ledName string
}

type codecTrial struct{ n, ok int }

func (c *Channel) initVoice() {
	c.vs.mode = c.std.cfg.Codec
	switch c.vs.mode {
	case "", "off":
		c.vs.mode = "off"
	case "auto":
		if full := c.std.ledgerCodecOf(c.cfg.MHz); full != "" {
			name, hint := parseLedgerCodec(full)
			if cd, ok := voice.Lookup(c.std.spec.VoiceChannel, name); ok && !cd.Tentative && (cd.Available == nil || cd.Available()) {
				c.vs.trial = &codecTrial{}
				if hint != "" {
					c.vs.hint = name + ":" + hint
				}
				c.setCodec(&cd, "台帳の符号器。検査で確かめる")
			}
		}
	default:
		if cd, ok := voice.Lookup(c.std.spec.VoiceChannel, c.vs.mode); ok {
			c.setCodec(&cd, "指定")
		}
	}
}

func (c *Channel) setCodec(cd *voice.Codec, why string) {
	c.vs.chosen, c.vs.why = cd, why
	for _, s := range c.vs.cands {
		s.v.Close()
	}
	c.vs.cands = nil
	if cd.Available != nil && !cd.Available() {
		why += "。外部デコーダが無いので鳴らさない"
	}
	c.mu.Lock()
	c.codecName = cd.Name + "（" + why + "）"
	c.mu.Unlock()
	c.logf("call", "info", "音声の符号器: %s（%s）", cd.Name, why)
}

func (c *Channel) voiceFrame(f decoder.Frame, p l1.Parsed) {
	if c.tx == nil {
		return
	}
	isVoice := p.Format == l1.FormatVoice
	if isVoice {
		c.writeTCH(f.No, p)
	}
	vch := c.std.spec.VoiceChannel
	for h := 0; h < 2; h++ {
		vf := voice.Frame{Ch: vch, No: 2*f.No + int64(h), TimeSec: f.TimeSec + float64(h)*voice.FrameSec, Sub: c.sub*2 + h}
		tch := isVoice && p.Half[h].Kind == l1.HalfTCH
		if tch {
			vf.Bits, vf.Soft = p.Half[h].TCH, p.Half[h].TCHSoft
			c.tx.rec.Voice++
			c.mu.Lock()
			c.st.Talking = true
			c.mu.Unlock()
		}
		c.probe(vf, tch)
		c.pushTracker(vf, tch)
	}
}

func (c *Channel) pushVoiceGap(f decoder.Frame) {
	if c.tx == nil || c.tx.tr == nil {
		return
	}
	for h := 0; h < 2; h++ {
		c.tx.tr.Push(voice.Frame{Ch: c.std.spec.VoiceChannel, No: 2*f.No + int64(h), TimeSec: f.TimeSec + float64(h)*voice.FrameSec}, false)
	}
}

func (c *Channel) probe(f voice.Frame, tch bool) {
	if c.vs.mode != "auto" || c.vs.chosen != nil || !tch {
		return
	}
	if !c.vs.tried {
		c.vs.tried = true
		for _, cd := range Codecs(c.std.spec.VoiceChannel) {
			if cd.Tentative || cd.ManualOnly || (cd.Available != nil && !cd.Available()) {
				continue
			}
			if v, err := cd.New(voice.Options{}); err == nil {
				c.vs.cands = append(c.vs.cands, &codecScore{c: cd, v: v})
			}
		}
	}
	for _, s := range c.vs.cands {
		u, ok, err := s.v.Transport(f)
		if err != nil || !ok || !u.Check.Valid {
			continue
		}
		s.add(u.Check.OK)
	}
	cs := c.vs.cands
	sort.SliceStable(cs, func(i, j int) bool { return cs[i].ratio() > cs[j].ratio() })
	if len(cs) == 0 || cs[0].n < codecMinChecks || cs[0].ratio() < codecMinRatio {
		return
	}
	b := cs[0]
	cd := b.c
	c.setCodec(&cd, fmt.Sprintf("伝送路の検査 %s %d/%d", cd.Check, b.ok, b.n))
	c.setLedgerCodec(cd.Name)
	hist := c.vs.hist
	c.vs.hist = nil
	if t := c.tx; t != nil {
		if t.tr != nil {
			t.tr.Finish()
		}
		t.tr = c.newTracker()
		c.note("voice_note 符号器が決まった: " + cd.Name + "（" + c.vs.why + "）")
		for _, h := range hist {
			t.tr.Push(h, h.Bits != nil)
		}
	}
}

func (c *Channel) checkTrial() {
	tr := c.vs.trial
	if tr == nil || tr.n < codecMinChecks || c.vs.chosen == nil {
		return
	}
	name := c.vs.chosen.Name
	c.vs.trial = nil
	if float64(tr.ok) >= codecMinRatio*float64(tr.n) {
		c.vs.hist = nil
		c.setLedgerCodec(name)
		c.logf("call", "info", "台帳の符号器 %s を確かめた（検査 %d/%d）", name, tr.ok, tr.n)
		return
	}
	c.vs.chosen, c.vs.why = nil, ""
	c.mu.Lock()
	c.codecName = ""
	c.mu.Unlock()
	c.logf("call", "warn", "台帳の符号器 %s は検査が通らない（%d/%d）ので選び直す", name, tr.ok, tr.n)
	if t := c.tx; t != nil && t.tr != nil {
		t.tr.Finish()
		t.audio = nil
		t.tr = c.newTracker()
		c.note(fmt.Sprintf("voice_note 台帳の符号器 %s は検査が通らないので選び直す（%d/%d）", name, tr.ok, tr.n))
	}
}

func (c *Channel) setLedgerCodec(name string) {
	if c.vs.mode != "auto" {
		return
	}
	c.vs.ledName = name
	c.publishLedgerCodec()
}

func (c *Channel) publishLedgerCodec() {
	s := c.vs.ledName
	if s == "" {
		return
	}
	if key, ok := strings.CutPrefix(c.vs.hint, s+":"); ok && key != "" && key != "0" {
		s += " scramble=" + key
	}
	c.mu.Lock()
	c.ledCodec = s
	c.mu.Unlock()
}

func parseLedgerCodec(s string) (name, scramble string) {
	fields := strings.Fields(s)
	if len(fields) == 0 {
		return "", ""
	}
	name = fields[0]
	for _, tok := range fields[1:] {
		if v, ok := strings.CutPrefix(tok, "scramble="); ok {
			scramble = v
		}
	}
	return name, scramble
}

func (c *Channel) LedgerCodec() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.ledCodec
}

func (c *Channel) newTracker() *voice.Tracker {
	if c.vs.mode == "off" || c.tx == nil {
		return nil
	}
	t := c.tx
	tr := voice.New()
	tr.Lossless = c.std.cfg.Lossless
	cd := c.vs.chosen
	if cd == nil || (cd.Available != nil && !cd.Available()) {
		return tr
	}
	cc := *cd
	opt := voice.Options{}
	if c.vs.mode != "auto" {
		for k, v := range c.std.cfg.CodecOpt {
			opt[k] = v
		}
	}
	if _, set := opt["hint"]; !set && c.vs.hint != "" && strings.HasPrefix(c.vs.hint, cc.Name+":") {
		opt["hint"] = c.vs.hint
	}
	tr.NewCodec = func() (voice.Voice, error) {
		o := voice.Options{}
		for k, v := range opt {
			o[k] = v
		}
		c.link.VoiceOpt(cc.Name, o)
		return cc.New(o)
	}
	buf := &audioBuf{}
	t.audio = buf
	id := c.cfg.ID
	audio := c.std.audio
	tr.Sink = func(pcm []int16) {
		buf.mu.Lock()
		buf.pcm = append(buf.pcm, pcm...)
		buf.mu.Unlock()
		if audio != nil {
			audio(id, pcm, voice.SampleRate)
		}
	}
	if c.std.cfg.AudioAGC {
		if c.gain == nil {
			c.gain = agc.NewQuiet()
		}
		tr.Filter = c.gain.Process
		tr.OnOnset = c.gain.MarkOnset
	}
	tr.OnEnd = func(cl voice.Call) {
		c.note(fmt.Sprintf("voice %s 音声 %d フレーム %s OK %d", cc.Name, cl.Frames, cc.Check, cl.OK))
	}
	tr.OnUnit = func(u voice.Unit, _, _ int64) {
		if u.Check.Valid {
			t.rec.VoiceOK += map[bool]int{true: 1, false: 0}[u.Check.OK]
			c.mu.Lock()
			c.st.Voice++
			if u.Check.OK {
				c.st.VoiceOK++
			}
			c.mu.Unlock()
			if c.vs.trial != nil && t.tr == tr {
				c.vs.trial.n++
				if u.Check.OK {
					c.vs.trial.ok++
				}
			}
		}
		if u.Choice != nil && u.Choice.Codec == cc.Name {
			if v, ok := u.Choice.Opts["scramble"]; ok {
				c.vs.hint = cc.Name + ":" + v
				c.publishLedgerCodec()
			}
		}
		if u.Note != "" {
			c.note("voice_note " + u.Note)
			c.mu.Lock()
			c.st.Talking = true
			c.mu.Unlock()
		}
	}
	return tr
}

func (c *Channel) pushTracker(f voice.Frame, tch bool) {
	t := c.tx
	if t == nil || c.vs.mode == "off" {
		return
	}
	if t.tr == nil {
		if !tch {
			return
		}
		t.tr = c.newTracker()
	}
	t.tr.Push(f, tch)
	if c.vs.mode == "auto" && (c.vs.chosen == nil || c.vs.trial != nil) {
		h := f
		if tch {
			h.Bits = append([]uint8(nil), f.Bits...)
			h.Soft = append([]float32(nil), f.Soft...)
		} else {
			h.Bits, h.Soft = nil, nil
		}
		c.vs.hist = append(c.vs.hist, h)
		if len(c.vs.hist) > codecPending {
			c.vs.hist = c.vs.hist[len(c.vs.hist)-codecPending:]
		}
	}
	c.checkTrial()
}

func (c *Channel) finishVoice() float64 {
	t := c.tx
	if t == nil || t.tr == nil {
		return 0
	}
	t.tr.Finish()
	if err := t.tr.Error(); err != nil {
		c.logf("call", "warn", "音声の復号器: %v", err)
	}
	if st := t.tr.Snapshot(); st.Frames > 0 {
		c.note(fmt.Sprintf("voice_stats 検査 %d/%d 欠落 %d 回（埋め %d 枚）捨て %d", st.CheckOK, st.Frames, st.Gaps, st.Filled, st.Dropped))
	}
	if t.audio == nil || t.base == "" {
		return 0
	}
	t.audio.mu.Lock()
	pcm := t.audio.pcm
	t.audio.mu.Unlock()
	if len(pcm) == 0 {
		return 0
	}
	if err := wavio.Write(rec.Path(t.base, rec.Audio), pcm, 1, voice.SampleRate); err != nil {
		c.logf("record", "warn", "音声 WAV を書けない: %v", err)
		return 0
	}
	return float64(len(pcm)) / voice.SampleRate
}

func (c *Channel) closeVoice() {
	for _, s := range c.vs.cands {
		s.v.Close()
	}
	c.vs.cands = nil
}
