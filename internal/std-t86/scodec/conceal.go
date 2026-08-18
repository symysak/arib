package scodec

func VoiceSpan(fers []uint8, win, need int) (int, int) {
	n := len(fers)
	good := make([]int, n)
	total := 0
	for i, f := range fers {
		if f == 0 {
			good[i] = 1
			total++
		}
	}
	if total == 0 {
		return 0, 0
	}
	csum := make([]int, n+1)
	for i := 0; i < n; i++ {
		csum[i+1] = csum[i] + good[i]
	}
	start := -1
	for i := 0; i < n; i++ {
		if good[i] == 1 && csum[min(n, i+win)]-csum[i] >= need {
			start = i
			break
		}
	}
	if start < 0 {
		return 0, 0
	}
	end := start
	for i := n - 1; i >= 0; i-- {
		if good[i] == 1 && csum[i+1]-csum[max(0, i-win+1)] >= need {
			end = i + 1
			break
		}
	}
	return start, end
}

type Concealer struct {
	MaxRepeat int

	lastGood []uint8
	run      int
}

type PLCAction uint8

const (
	PLCNone   PLCAction = iota
	PLCRepeat
	PLCMute
)

func (c *Concealer) Apply(frames [][]uint8, fers []uint8) [][]uint8 {
	out, _ := c.ApplyTraced(frames, fers)
	return out
}

func (c *Concealer) ApplyTraced(frames [][]uint8, fers []uint8) ([][]uint8, []PLCAction) {
	maxRepeat := c.MaxRepeat
	if maxRepeat == 0 {
		maxRepeat = 2
	}
	out := make([][]uint8, len(frames))
	acts := make([]PLCAction, len(frames))
	for i := range frames {
		if fers[i] == 0 {
			c.lastGood = frames[i]
			c.run = 0
			out[i] = frames[i]
			acts[i] = PLCNone
			continue
		}
		c.run++
		if c.lastGood != nil && c.run <= maxRepeat {
			out[i] = c.lastGood
			acts[i] = PLCRepeat
		} else {
			out[i] = make([]uint8, G7221FrameBits)
			acts[i] = PLCMute
		}
	}
	return out, acts
}

func ConcealFrameErrors(frames [][]uint8, fers []uint8, maxRepeat int) [][]uint8 {
	c := &Concealer{MaxRepeat: maxRepeat}
	return c.Apply(frames, fers)
}

type SlotGapTracker struct {
	SlotSamples float64
	MaxFill int
	ActiveSlots int
	SettleFrames int
	MinSightings int

	hasLast  bool
	lastPos  int
	ring     int
	startPos int
	hasStart bool
	rings    map[int]*ringStat
}

type ringStat struct {
	count    int
	lastPos  int
	firstPos int
}

func NewSlotGapTracker(slotSamples float64, maxFill int) *SlotGapTracker {
	if slotSamples <= 0 {
		slotSamples = 1200
	}
	if maxFill <= 0 {
		maxFill = 1500
	}
	return &SlotGapTracker{
		SlotSamples:  slotSamples,
		MaxFill:      maxFill,
		ActiveSlots:  150,
		SettleFrames: 4,
		MinSightings: 8,
		rings:        map[int]*ringStat{},
	}
}

func (t *SlotGapTracker) isVoiceRing(r int) bool {
	s := t.rings[r]
	if s == nil {
		return false
	}
	need := t.MinSightings
	if float64(s.firstPos-t.startPos) < float64(t.SettleFrames*6)*t.SlotSamples {
		need = 1
	}
	if s.count < need {
		return false
	}
	return float64(t.lastPos-s.lastPos) <= float64(t.ActiveSlots)*t.SlotSamples
}

func (t *SlotGapTracker) voiceRingCount() int {
	n := 0
	for r := 0; r < 6; r++ {
		if t.isVoiceRing(r) {
			n++
		}
	}
	return n
}

func (t *SlotGapTracker) observe(r, pos int) {
	if !t.hasStart {
		t.hasStart, t.startPos = true, pos
	}
	s := t.rings[r]
	if s == nil {
		s = &ringStat{firstPos: pos}
		t.rings[r] = s
	}
	s.count++
	s.lastPos = pos
}

func (t *SlotGapTracker) Step(pos int) (int, bool) {
	if !t.hasLast {
		t.hasLast = true
		t.lastPos = pos
		t.observe(0, pos)
		return 0, true
	}
	dq := roundHalfEven(float64(pos-t.lastPos) / t.SlotSamples)
	if dq <= 0 {
		return 0, false
	}
	full, rem := (dq-1)/6, (dq-1)%6
	missing := full * t.voiceRingCount()
	for k := 1; k <= rem; k++ {
		if t.isVoiceRing((t.ring + k) % 6) {
			missing++
		}
	}
	t.ring = (t.ring + dq) % 6
	t.observe(t.ring, pos)
	t.lastPos = pos
	return min(missing, t.MaxFill), true
}

func roundHalfEven(x float64) int {
	f := float64(int(x))
	if x < 0 && x != f {
		f--
	}
	d := x - f
	i := int(f)
	switch {
	case d > 0.5:
		return i + 1
	case d < 0.5:
		return i
	default:
		if i%2 == 0 {
			return i
		}
		return i + 1
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
