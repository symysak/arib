package server

import (
	"fmt"
	"sync"

	"github.com/symysak/stdt86/internal/citycodes"
	"github.com/symysak/stdt86/internal/control"
)


type target struct {
	Kind         string `json:"kind"`
	Label        string `json:"label"`
	IDs          []int  `json:"ids"`
	EffectiveIDs []int  `json:"effective_ids"`
	ValidBits    *int   `json:"valid_bits"`
	CallNo       *int   `json:"call_no"`
	Note         string `json:"note"`
}

func toTarget(t *control.Target) *target {
	if t == nil {
		return nil
	}
	out := &target{
		Kind: string(t.Kind), Label: t.Label, Note: t.Note,
		IDs: t.IDs, EffectiveIDs: t.EffectiveIDs,
	}
	if out.IDs == nil {
		out.IDs = []int{}
	}
	if out.EffectiveIDs == nil {
		out.EffectiveIDs = []int{}
	}
	if t.ValidBits > 0 {
		v := t.ValidBits
		out.ValidBits = &v
	}
	c := t.CallNo
	out.CallNo = &c
	return out
}

type audioStatus struct {
	Frames    int     `json:"frames"`
	CRC7OK    int     `json:"crc7_ok"`
	CRC7Fail  int     `json:"crc7_fail"`
	CRC7Rate  float64 `json:"crc7_rate"`
	Filled    int     `json:"filled"`
	Stale     int     `json:"stale"`
	PLCRepeat int     `json:"plc_repeat"`
	PLCMute   int     `json:"plc_mute"`

	CDistMax  int     `json:"c_dist_max"`
	CDistMean float64 `json:"c_dist_mean"`
	CDistBad  int     `json:"c_dist_bad"`

	DecodedSeconds  float64 `json:"decoded_seconds"`
	DecodeAttempted bool    `json:"decode_attempted"`
	Note            string  `json:"note"`
	WavPath         string  `json:"wav_path"`

	Quality string `json:"quality"`

	cdistSum int
}

type iqStatus struct {
	Path    string  `json:"path"`
	Seconds float64 `json:"seconds"`
	FS      float64 `json:"fs"`
	Done    bool    `json:"done"`
	Note    string  `json:"note"`
}

type windowInfo struct {
	TStart    *float64     `json:"t_start"`
	TEnd      *float64     `json:"t_end"`
	WallStart *int64       `json:"wall_start"`
	WallEnd   *int64       `json:"wall_end"`
	Target    *target      `json:"target"`
	Audio     *audioStatus `json:"audio"`
	IQ        *iqStatus    `json:"iq"`
}

type broadcastState struct {
	Active   bool     `json:"active"`
	WindowID *int     `json:"window_id"`
	StartedT *float64 `json:"started_t"`
}

type controlSummary struct {
	Seed      *int `json:"seed"`
	Searching bool `json:"searching,omitempty"`
	SeedPinned    bool    `json:"seed_pinned,omitempty"`
	MunicipalCode *int    `json:"municipal_code,omitempty"`
	Municipality  *string `json:"municipality"`

	MunicipalityConfirmed *int `json:"municipality_confirmed,omitempty"`
	MunicipalitySource string `json:"municipality_source,omitempty"`

	Candidates      []candidate   `json:"candidates"`
	TypeCounts      orderedCounts `json:"type_counts"`
	Manufacturers   orderedCounts `json:"manufacturers"`
	SlotUsage       []int         `json:"slot_usage"`
	BroadcastActive bool          `json:"broadcast_active"`
	ParentMode      *string       `json:"parent_mode,omitempty"`
	SuperframeLen   *int          `json:"superframe_len,omitempty"`
	Total           int           `json:"total"`
	Valid           int           `json:"valid"`
	CRCOK           int           `json:"crc_ok"`
}

type tuningInfo struct {
	F0Hz             *float64 `json:"f0_hz"`
	CenterHz         *float64 `json:"center_hz"`
	Seed             *int     `json:"seed"`
	MunicipalityCode *int     `json:"municipality_code"`
}

type qualityInfo struct {
	CFOHz      *float64 `json:"cfo_hz"`
	EVMMedian  *float64 `json:"evm_median"`
	EVMBest    *float64 `json:"evm_best"`
	CRCOKRate  *float64 `json:"crc_ok_rate"`
	MsgsPerS   float64  `json:"msgs_per_s"`
	LevelDBFS  *float64 `json:"level_dbfs"`
	SyncLocked bool     `json:"sync_locked"`
	Overflows  int      `json:"overflows"`
}

type snapshot struct {
	Type string  `json:"type"`
	T    float64 `json:"t"`
	T0WallMS int64 `json:"t0_wall_ms"`
	T0Estimated bool `json:"t0_estimated"`
	T0Source string `json:"t0_source"`

	Source          string                 `json:"source"`
	Control         *controlSummary        `json:"control"`
	Tuning          tuningInfo             `json:"tuning"`
	Broadcast       broadcastState         `json:"broadcast"`
	SquelchEnabled  bool                   `json:"squelch_enabled"`
	BroadcastStrict bool                   `json:"broadcast_strict"`
	CFOEnabled      bool                   `json:"cfo_enabled"`
	Windows         map[string]*windowInfo `json:"windows"`
	TCHCounts       map[string]int         `json:"tch_counts"`
	Quality         qualityInfo            `json:"quality"`
	RecentLog       []Event                `json:"recent_log"`
}

const (
	recentMsgsCap = 2000
	logLinesCap   = 500
)

type liveState struct {
	mu sync.Mutex

	seed int
	lastSeed int
	seedPinned    bool
	municipalCode int
	municipalityCode int
	f0Hz             float64
	hasF0            bool
	centerHz         float64
	hasCenter        bool

	msgs      []control.Message
	logLines  []Event
	tchCounts map[string]int

	t          float64
	cfoHz      *float64
	evmMedian  *float64
	evmBest    *float64
	levelDBFS  *float64
	msgsPerS   float64
	crcOKRate  *float64
	syncLocked bool
	overflows  int

	squelchEnabled  bool
	broadcastStrict bool
	cfoEnabled      bool
	broadcast       broadcastState
	windows         map[int]*windowInfo
	sourceDesc      string

	origin timeOrigin
}

func newLiveState(seed, municipalCode int) *liveState {
	return &liveState{
		seed: seed, municipalCode: municipalCode,
		tchCounts:       map[string]int{},
		windows:         map[int]*windowInfo{},
		squelchEnabled:  true,
		broadcastStrict: true,
		cfoEnabled:      true,
	}
}


func (s *liveState) addControl(m control.Message) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.msgs = append(s.msgs, m)
	if len(s.msgs) > recentMsgsCap {
		s.msgs = s.msgs[len(s.msgs)-recentMsgsCap:]
	}
}

func (s *liveState) addTCH(ctype string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tchCounts[ctype]++
}

func (s *liveState) addLog(ev Event) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.logLines = append(s.logLines, ev)
	if len(s.logLines) > logLinesCap {
		s.logLines = s.logLines[len(s.logLines)-logLinesCap:]
	}
}

func (s *liveState) resetControl(clearBroadcast bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.msgs = nil
	s.tchCounts = map[string]int{}
	s.crcOKRate = nil
	s.msgsPerS = 0
	s.evmMedian = nil
	s.evmBest = nil
	if clearBroadcast {
		s.broadcast = broadcastState{}
	}
}

func (s *liveState) broadcastStarted(id int, t float64, wallMS int64, tg *target) {
	s.mu.Lock()
	defer s.mu.Unlock()
	idCopy, tCopy := id, t
	s.broadcast = broadcastState{Active: true, WindowID: &idCopy, StartedT: &tCopy}
	ts, wm := t, wallMS
	s.windows[id] = &windowInfo{TStart: &ts, WallStart: &wm, Target: tg}
}

func (s *liveState) setWindowTarget(id int, tg *target) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if w, ok := s.windows[id]; ok {
		w.Target = tg
	}
}

func (s *liveState) broadcastEnded(id int, t float64, wallMS int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.broadcast.WindowID != nil && *s.broadcast.WindowID == id {
		s.broadcast = broadcastState{}
	}
	if w, ok := s.windows[id]; ok {
		te, wm := t, wallMS
		w.TEnd = &te
		w.WallEnd = &wm
	}
}

func (s *liveState) window(id int) *windowInfo {
	if w, ok := s.windows[id]; ok {
		return w
	}
	w := &windowInfo{}
	s.windows[id] = w
	return w
}

func (s *liveState) setAudioStatus(id int, st audioStatus) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c := st
	s.window(id).Audio = &c
}

func (s *liveState) setWindowIQ(id int, info iqStatus) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c := info
	s.window(id).IQ = &c
}

func (s *liveState) windowInfoCopy(id int) *windowInfo {
	s.mu.Lock()
	defer s.mu.Unlock()
	w, ok := s.windows[id]
	if !ok {
		return nil
	}
	c := *w
	return &c
}

func (s *liveState) setT(t float64) {
	s.mu.Lock()
	s.t = t
	s.mu.Unlock()
}

func (s *liveState) currentT() float64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.t
}


func (s *liveState) controlSummary() *controlSummary {
	s.mu.Lock()
	msgs := append([]control.Message(nil), s.msgs...)
	seed := s.seed
	pinned := s.seedPinned
	muniFull := s.municipalityCode
	muniCode := s.municipalCode
	s.mu.Unlock()

	if seed == 0 {
		return &controlSummary{
			Searching: true, Candidates: []candidate{},
			TypeCounts: orderedCounts{}, Manufacturers: orderedCounts{},
			SlotUsage: []int{},
		}
	}
	sum := control.Summarize(msgs, seed, muniCode)
	out := &controlSummary{
		Seed:            &seed,
		SeedPinned:      pinned,
		SlotUsage:       sum.SlotUsage,
		BroadcastActive: sum.BroadcastActive,
		Total:           sum.Total,
		Valid:           sum.Valid,
		CRCOK:           sum.CRCOK,
		TypeCounts:      toOrdered(sum.TypeCounts),
		Manufacturers:   toOrdered(sum.Manufacturers),
		Candidates:      toCandidates(sum.Candidates),
	}
	if muniCode != 0 {
		c := muniCode
		out.MunicipalCode = &c
	}
	if sum.Municipality != "" {
		m := sum.Municipality
		out.Municipality = &m
	}
	if sum.ParentMode != "" {
		p := sum.ParentMode
		out.ParentMode = &p
	}
	if sum.SuperframeLen != 0 {
		v := sum.SuperframeLen
		out.SuperframeLen = &v
	}
	switch {
	case muniFull != 0:
		c := muniFull
		out.MunicipalityConfirmed = &c
		out.MunicipalitySource = "facch"
		if name, ok := citycodes.Name(muniFull); ok {
			m := name
			out.Municipality = &m
		} else if out.Municipality == nil {
			m := fmt.Sprintf("コード %d", muniFull)
			out.Municipality = &m
		}
	case muniCode != 0:
		out.MunicipalitySource = "flag"
	}
	return out
}

func toOrdered(v []control.NameCount) orderedCounts {
	out := make(orderedCounts, len(v))
	for i, x := range v {
		out[i] = nameCount{x.Name, x.Count}
	}
	return out
}

func toCandidates(v []citycodes.Candidate) []candidate {
	out := make([]candidate, len(v))
	for i, c := range v {
		out[i] = candidate{c.Code, c.Name}
	}
	return out
}

func (s *liveState) snapshot() snapshot {
	summary := s.controlSummary()
	s.mu.Lock()
	defer s.mu.Unlock()

	windows := make(map[string]*windowInfo, len(s.windows))
	for k, v := range s.windows {
		c := *v
		windows[fmt.Sprint(k)] = &c
	}
	tch := make(map[string]int, len(s.tchCounts))
	for k, v := range s.tchCounts {
		tch[k] = v
	}
	tuning := tuningInfo{}
	if s.hasF0 {
		v := s.f0Hz
		tuning.F0Hz = &v
	}
	if s.hasCenter {
		v := s.centerHz
		tuning.CenterHz = &v
	}
	if s.seed != 0 {
		v := s.seed
		tuning.Seed = &v
	}
	if s.municipalityCode != 0 {
		v := s.municipalityCode
		tuning.MunicipalityCode = &v
	}
	return snapshot{
		Type:            "snapshot",
		T:               round(s.t, 1),
		T0WallMS:        s.origin.WallMS,
		T0Estimated:     s.origin.Estimated,
		T0Source:        s.origin.Source,
		Source:          s.sourceDesc,
		Control:         summary,
		Tuning:          tuning,
		Broadcast:       s.broadcast,
		SquelchEnabled:  s.squelchEnabled,
		BroadcastStrict: s.broadcastStrict,
		CFOEnabled:      s.cfoEnabled,
		Windows:         windows,
		TCHCounts:       tch,
		Quality: qualityInfo{
			CFOHz: s.cfoHz, EVMMedian: s.evmMedian, EVMBest: s.evmBest,
			CRCOKRate: s.crcOKRate, MsgsPerS: s.msgsPerS, LevelDBFS: s.levelDBFS,
			SyncLocked: s.syncLocked, Overflows: s.overflows,
		},
		RecentLog: append([]Event(nil), s.logLines...),
	}
}
