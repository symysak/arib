package decoder

import (
	"github.com/symysak/stdt86/internal/control"
	"github.com/symysak/stdt86/internal/dsp"
)

const cfoAcquireSeconds = 0.5

type PositionedMessage struct {
	Pos int
	Msg control.Message

	BurstQuality
}

type BurstQuality struct {
	SW      string
	Corr    float64
	EVM     float64
	PowerDB float64
}

type VoiceEvent struct {
	Burst    *dsp.TchBurst
	IsVoice  bool
	WindowID int
}

type SlotSample struct {
	EVM  float64
	Slot []complex64
}

type FeedResult struct {
	Control          []PositionedMessage
	TCH              []*dsp.TchBurst
	Voice            []VoiceEvent
	BroadcastStarted []*BroadcastWindow
	BroadcastEnded   []*BroadcastWindow
	BroadcastUpdated []*BroadcastWindow
	EVMs             []float64
	SWCounts         map[string]int
	Slots            []SlotSample
	SeedDetected *SeedInfo
}

type StreamingDecoder struct {
	FS   float64
	F0   float64
	Seed int

	frontend  *dsp.StreamFrontEnd
	tracker   *dsp.SlotTracker
	broadcast *BroadcastTracker
	smoother  *voiceSmoother
	seeder    *seedSearch
	endedBacklog []*BroadcastWindow
	pendingVoice []DecidedBurst
}

func NewStreamingDecoder(fs, f0 float64, seed int, syncThresh float64) *StreamingDecoder {
	return &StreamingDecoder{
		FS:        fs,
		F0:        f0,
		Seed:      seed,
		frontend:  dsp.NewStreamFrontEnd(fs, f0, cfoAcquireSeconds),
		tracker:   dsp.NewSlotTracker(syncThresh),
		broadcast: NewBroadcastTracker(),
		smoother:  newVoiceSmoother(2),
		seeder:    newSeedSearch(),
	}
}

func (d *StreamingDecoder) FSBB() float64 { return dsp.FSBB }

func (d *StreamingDecoder) CFOHz() (float64, bool) { return d.frontend.CFOHz() }

func (d *StreamingDecoder) Broadcast() *BroadcastTracker { return d.broadcast }

func (d *StreamingDecoder) SquelchEnabled() bool { return d.tracker.SquelchEnabled }

func (d *StreamingDecoder) SetSquelchEnabled(v bool) { d.tracker.SquelchEnabled = v }

func (d *StreamingDecoder) ReacquireCFO() { d.frontend.ReacquireCFO() }

func (d *StreamingDecoder) CFOEnabled() bool { return d.frontend.CFOEnabled() }

func (d *StreamingDecoder) SetCFOEnabled(v bool) { d.frontend.SetCFOEnabled(v) }

func (d *StreamingDecoder) ResetBroadcast() *BroadcastWindow {
	return d.broadcast.Abandon(d.tracker.FinalizedPos())
}

func (d *StreamingDecoder) ArmMidJoin() { d.broadcast.ArmMidJoin() }

func (d *StreamingDecoder) ResetSeed() {
	d.Seed = 0
	d.seeder.reset()
}

func (d *StreamingDecoder) PinSeed(seed int) FeedResult {
	res := FeedResult{SWCounts: map[string]int{}}
	if seed <= 0 {
		d.ResetSeed()
		return res
	}
	d.Seed = seed
	d.seeder.reset()
	d.flushPendingVoice(&res)
	return res
}

func (d *StreamingDecoder) applyDetectedSeed(info *SeedInfo, res *FeedResult) {
	d.Seed = info.Seed
	res.SeedDetected = info
	for _, sl := range d.seeder.slots {
		d.decodeControl(sl.pos, sl.bits, sl.qual, res)
	}
	d.seeder.reset()
}

func (d *StreamingDecoder) decodeControl(pos int, bits []uint8, qual BurstQuality,
	res *FeedResult) {
	msg, err := control.DecodeSlot(bits, d.Seed, control.CACOffset)
	if err != nil {
		return
	}
	res.Control = append(res.Control, PositionedMessage{pos, msg, qual})
	for _, w := range d.broadcast.OnControl(pos, msg) {
		if w.Closed {
			d.endedBacklog = append(d.endedBacklog, w)
		} else {
			res.BroadcastStarted = append(res.BroadcastStarted, w)
		}
	}
	if len(d.broadcast.TargetUpdated) > 0 {
		res.BroadcastUpdated = append(res.BroadcastUpdated, d.broadcast.TargetUpdated...)
		d.broadcast.TargetUpdated = nil
	}
}

func (d *StreamingDecoder) handleBursts(bursts []dsp.DetectedBurst, res *FeedResult) {
	for i := range bursts {
		b := &bursts[i]
		res.EVMs = append(res.EVMs, b.EVM)
		res.SWCounts[b.SW]++
		res.Slots = append(res.Slots, SlotSample{b.EVM, b.Slot})

		if b.SW == "S1" || b.SW == "S5" || b.SW == "S6" {
			bits := dsp.SlotBits(b.Slot)
			if len(bits) < control.CACOffset+control.CACSpan {
				continue
			}
			qual := BurstQuality{SW: b.SW, Corr: b.Corr, EVM: b.EVM, PowerDB: b.PowerDB}
			if d.Seed == 0 {
				if info := d.seeder.push(b.Pos, bits, qual); info != nil {
					d.applyDetectedSeed(info, res)
				}
			} else {
				d.decodeControl(b.Pos, bits, qual, res)
			}
			continue
		}

		burst := dsp.TchBurstFromSlot(b.Slot, b.Pos)
		if burst == nil {
			continue
		}
		if burst.CType == "FACCH" && burst.CDist == 0 && d.Seed != 0 {
			msg, err := control.DecodeFACCH(burst.Bits, d.Seed)
			if err == nil && (msg.CRCOK || control.KnownType(msg.Type)) {
				res.Control = append(res.Control, PositionedMessage{b.Pos, msg,
					BurstQuality{SW: b.SW, Corr: b.Corr, EVM: b.EVM, PowerDB: b.PowerDB}})
			}
			continue
		}
		res.TCH = append(res.TCH, burst)
		d.emitVoice(d.smoother.push(burst), res)
	}
}

const pendingVoiceCap = 1000

func (d *StreamingDecoder) emitVoice(decided []DecidedBurst, res *FeedResult) {
	if d.Seed == 0 {
		d.pendingVoice = append(d.pendingVoice, decided...)
		if over := len(d.pendingVoice) - pendingVoiceCap; over > 0 {
			d.appendVoice(d.pendingVoice[:over], res)
			d.pendingVoice = append(d.pendingVoice[:0], d.pendingVoice[over:]...)
		}
		return
	}
	d.flushPendingVoice(res)
	d.appendVoice(decided, res)
}

func (d *StreamingDecoder) flushPendingVoice(res *FeedResult) {
	if len(d.pendingVoice) == 0 {
		return
	}
	d.appendVoice(d.pendingVoice, res)
	d.pendingVoice = nil
}

func (d *StreamingDecoder) appendVoice(decided []DecidedBurst, res *FeedResult) {
	for _, db := range decided {
		id := 0
		if w := d.broadcast.WindowFor(db.Burst.Pos); w != nil {
			id = w.WindowID
		}
		res.Voice = append(res.Voice, VoiceEvent{db.Burst, db.IsVoice, id})
	}
}

func (d *StreamingDecoder) releaseEnded(res *FeedResult) {
	watermark := d.tracker.FinalizedPos()
	if p, ok := d.smoother.oldestPendingPos(); ok && p < watermark {
		watermark = p
	}
	keep := d.endedBacklog[:0]
	for _, w := range d.endedBacklog {
		if w.ClosePos < watermark {
			res.BroadcastEnded = append(res.BroadcastEnded, w)
		} else {
			keep = append(keep, w)
		}
	}
	d.endedBacklog = keep
}

func (d *StreamingDecoder) Feed(chunk []complex64) FeedResult {
	res := FeedResult{SWCounts: map[string]int{}}
	mf := d.frontend.Process(chunk)
	if len(mf) == 0 {
		return res
	}
	if d.Seed == 0 {
		d.seeder.beginFeed()
	}
	d.handleBursts(d.tracker.Process(mf), &res)
	d.emitVoice(d.smoother.release(d.tracker.FinalizedPos()), &res)
	if d.Seed != 0 {
		d.flushPendingVoice(&res)
	}
	d.releaseEnded(&res)
	return res
}

func (d *StreamingDecoder) Flush() FeedResult {
	res := FeedResult{SWCounts: map[string]int{}}
	d.emitVoice(d.smoother.flush(), &res)
	d.appendVoice(d.pendingVoice, &res)
	d.pendingVoice = nil
	res.BroadcastEnded = append(res.BroadcastEnded, d.endedBacklog...)
	d.endedBacklog = nil
	return res
}
