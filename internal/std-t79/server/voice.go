package server


import (
	"fmt"
	"sort"
	"sync"

	"github.com/symysak/arib/internal/rec"
	"github.com/symysak/arib/internal/std-t79/decoder"
	"github.com/symysak/arib/internal/std-t79/l1"
	"github.com/symysak/arib/internal/voice"
	"github.com/symysak/arib/internal/wavio"
)

const (
	codecPending   = 75
	codecMinChecks = 10
	codecMinRatio  = 0.5
)

func Codecs() []voice.Codec { return voice.Codecs(l1.VoiceChannel) }

func CodecNames() string { return voice.Names(l1.VoiceChannel) }

func CheckCodec(name string, opt voice.Options) (notes []string, err error) {
	switch name {
	case "", "off", "auto":
		if len(opt) != 0 {
			return nil, fmt.Errorf("-codec-opt は -codec <符号器の名前> と一緒に使う（auto / off には設定が無い）")
		}
		if name == "auto" && len(Codecs()) == 0 {
			notes = append(notes, "注意: T79 の TCH を読む符号器が 1 つも登録されていないので音声は出ません（TCH の生ビットは残します）。")
		}
		return notes, nil
	}
	c, ok := voice.Lookup(l1.VoiceChannel, name)
	if !ok {
		return nil, fmt.Errorf("知らない符号器 %q（auto / off / %s）", name, CodecNames())
	}
	v, err := c.New(opt)
	if err != nil {
		return nil, fmt.Errorf("-codec %s: %v", c.Name, err)
	}
	v.Close()
	if c.Available != nil && !c.Available() {
		notes = append(notes, fmt.Sprintf("注意: 符号器 %s の外部デコーダが無いので音声 WAV は書けません（通話の記録と TCH は残します）。", c.Name))
	}
	if c.Tentative {
		notes = append(notes, fmt.Sprintf("注意: 符号器 %s の伝送路は未確定です（候補のひとつで復号するだけ。.calls.txt に「未確定」と書きます）。", c.Name))
	}
	return notes, nil
}

type codecScore struct {
	c     voice.Codec
	v     voice.Voice
	n, ok int
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

type audioMixer struct {
	mu     sync.Mutex
	fifo   [4][]int16
	active [4]bool
	out    func(pcm []int16)
}

const mixLag = voice.SampleRate / 2

func (m *audioMixer) start(n int) {
	m.mu.Lock()
	m.active[n] = true
	m.mu.Unlock()
}

func (m *audioMixer) push(n int, pcm []int16) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.fifo[n] = append(m.fifo[n], pcm...)
	for {
		k, longest, any := -1, 0, false
		for i, on := range m.active {
			if !on {
				continue
			}
			any = true
			l := len(m.fifo[i])
			if k < 0 || l < k {
				k = l
			}
			longest = max(longest, l)
		}
		if !any || longest == 0 {
			return
		}
		if k == 0 {
			if longest < mixLag {
				return
			}
			k = longest
		}
		m.emit(k)
	}
}

func (m *audioMixer) end(n int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if k := len(m.fifo[n]); k > 0 {
		m.emit(k)
	}
	m.active[n] = false
	m.fifo[n] = nil
}

func (m *audioMixer) emit(k int) {
	out := make([]int16, k)
	for i := range m.fifo {
		f := m.fifo[i]
		n := min(k, len(f))
		for j := 0; j < n; j++ {
			out[j] = sat16(int32(out[j]) + int32(f[j]))
		}
		m.fifo[i] = f[n:]
	}
	if m.out != nil {
		m.out(out)
	}
}

func sat16(v int32) int16 {
	switch {
	case v > 32767:
		return 32767
	case v < -32768:
		return -32768
	}
	return int16(v)
}

type voiceState struct {
	mode   string
	chosen *voice.Codec
	why    string
	cands  [4][]*codecScore
}

func (c *Channel) initVoice() {
	c.vs.mode = c.std.cfg.Codec
	switch c.vs.mode {
	case "", "off":
		c.vs.mode = "off"
	case "auto":
	default:
		if cd, ok := voice.Lookup(l1.VoiceChannel, c.vs.mode); ok {
			c.setCodec(&cd, "指定")
		}
	}
}

func (c *Channel) setCodec(cd *voice.Codec, why string) {
	c.vs.chosen, c.vs.why = cd, why
	for n := range c.vs.cands {
		for _, s := range c.vs.cands[n] {
			s.v.Close()
		}
		c.vs.cands[n] = nil
	}
	name := cd.Name
	if cd.Available != nil && !cd.Available() {
		why += "。外部デコーダが無いので鳴らさない"
	}
	if cd.Tentative {
		why += "。伝送路は未確定"
	}
	c.mu.Lock()
	c.car.Codec = name + "（" + why + "）"
	c.codecName = name
	c.mu.Unlock()
	c.logf(-1, "call", "info", "音声の符号器: %s（%s）", name, why)
}

func (c *Channel) probe(n int, f decoder.TCHFrame) {
	if c.vs.mode != "auto" || c.vs.chosen != nil || c.calls[n].data {
		return
	}
	if c.vs.cands[n] == nil {
		for _, cd := range Codecs() {
			if cd.Tentative || cd.ManualOnly || (cd.Available != nil && !cd.Available()) {
				continue
			}
			if v, err := cd.New(voice.Options{}); err == nil {
				c.vs.cands[n] = append(c.vs.cands[n], &codecScore{c: cd, v: v})
			}
		}
		if c.vs.cands[n] == nil {
			c.vs.cands[n] = []*codecScore{}
			return
		}
	}
	isVoice := f.Voice && len(f.Bits) == l1.TCHBits
	for _, s := range c.vs.cands[n] {
		if !isVoice {
			s.v.Erase()
			continue
		}
		u, ok, err := s.v.Transport(voice.Frame{Ch: l1.VoiceChannel, No: f.Frame, TimeSec: f.TSec, Bits: f.Bits, Soft: f.Soft})
		if err != nil || !ok || !u.Check.Valid {
			continue
		}
		s.n++
		if u.Check.OK {
			s.ok++
		}
	}
	cs := c.vs.cands[n]
	sort.SliceStable(cs, func(i, j int) bool { return cs[i].ratio() > cs[j].ratio() })
	if len(cs) == 0 || cs[0].n < codecMinChecks || cs[0].ratio() < codecMinRatio {
		return
	}
	b := cs[0]
	cd := b.c
	c.setCodec(&cd, fmt.Sprintf("伝送路の検査 %s %d/%d", cd.Check, b.ok, b.n))
	for m := range c.calls {
		cs := &c.calls[m]
		if !cs.on {
			continue
		}
		c.endTracker(m)
		cs.tr = c.newTracker(m)
		c.note(m, "voice_note 符号器が決まった: "+cd.Name+"（"+c.vs.why+"）")
		for _, h := range cs.hist {
			c.pushTracker(m, h)
		}
		cs.hist = nil
	}
}

func (c *Channel) newTracker(n int) *voice.Tracker {
	if c.vs.mode == "off" {
		return nil
	}
	cs := &c.calls[n]
	tr := voice.New()
	tr.Lossless = c.std.cfg.Lossless
	cd := c.vs.chosen
	if cd == nil || (cd.Available != nil && !cd.Available()) {
		return tr
	}
	cc := *cd
	opt := voice.Options{}
	if c.vs.mode != "auto" {
		opt = c.std.cfg.CodecOpt
	}
	tr.NewCodec = func() (voice.Voice, error) { return cc.New(opt) }
	buf := &audioBuf{}
	cs.audio = buf
	id := c.cfg.ID
	if audio := c.std.audio; audio != nil && c.mix.out == nil {
		c.mix.out = func(pcm []int16) { audio(id, pcm, voice.SampleRate) }
	}
	c.mix.start(n)
	tr.Sink = func(pcm []int16) {
		buf.mu.Lock()
		buf.pcm = append(buf.pcm, pcm...)
		buf.mu.Unlock()
		c.mix.push(n, pcm)
	}
	tentative := ""
	if cc.Tentative {
		tentative = " 未確定（伝送路が候補のひとつ）"
	}
	tr.OnEnd = func(cl voice.Call) {
		c.note(n, fmt.Sprintf("voice %s 音声 %d フレーム %s OK %d%s", cc.Name, cl.Frames, cc.Check, cl.OK, tentative))
	}
	notes := map[string]bool{}
	tr.OnUnit = func(u voice.Unit, _, _ int64) {
		if u.Note != "" && !notes[u.Note] {
			notes[u.Note] = true
			c.note(n, "voice_note "+u.Note)
		}
	}
	return tr
}

func (c *Channel) pushTracker(n int, f decoder.TCHFrame) {
	cs := &c.calls[n]
	if cs.tr == nil {
		return
	}
	isVoice := f.Voice && len(f.Bits) == l1.TCHBits && !cs.data
	cs.tr.Push(voice.Frame{Ch: l1.VoiceChannel, No: f.Frame, TimeSec: f.TSec, Bits: f.Bits, Soft: f.Soft}, isVoice)
	if c.vs.mode == "auto" && c.vs.chosen == nil {
		cs.hist = append(cs.hist, f)
		if len(cs.hist) > codecPending {
			cs.hist = cs.hist[len(cs.hist)-codecPending:]
		}
	}
}

func (c *Channel) endTracker(n int) {
	if tr := c.calls[n].tr; tr != nil {
		tr.Finish()
	}
	c.mix.end(n)
}

func (c *Channel) finishVoice(n int) float64 {
	cs := &c.calls[n]
	if cs.tr != nil {
		c.endTracker(n)
		if err := cs.tr.Error(); err != nil {
			c.logf(n, "call", "warn", "ST#%d の音声の復号器: %v", n, err)
		}
		if st := cs.tr.Snapshot(); st.Frames > 0 {
			c.note(n, fmt.Sprintf("voice_stats 検査 %d/%d 欠落 %d 回（埋め %d 枚）捨て %d", st.CheckOK, st.Frames, st.Gaps, st.Filled, st.Dropped))
		}
	}
	if cs.audio == nil || cs.base == "" {
		return 0
	}
	cs.audio.mu.Lock()
	pcm := cs.audio.pcm
	cs.audio.mu.Unlock()
	if len(pcm) == 0 {
		return 0
	}
	if err := wavio.Write(rec.Path(cs.base, rec.Audio), pcm, 1, voice.SampleRate); err != nil {
		c.logf(n, "record", "warn", "音声 WAV を書けない: %v", err)
		return 0
	}
	return float64(len(pcm)) / voice.SampleRate
}

func (c *Channel) closeVoice() {
	for n := range c.vs.cands {
		for _, s := range c.vs.cands[n] {
			s.v.Close()
		}
		c.vs.cands[n] = nil
	}
}
