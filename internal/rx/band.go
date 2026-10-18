package rx

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/symysak/arib/internal/rec"
)

type bandState struct {
	mu    sync.Mutex
	until time.Time
	by    float64
}

func (b *bandState) note(mhz float64, d time.Duration) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.until, b.by = time.Now().Add(d), mhz
}

func (b *bandState) active() (bool, float64) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return time.Now().Before(b.until), b.by
}

type bandRecorder struct {
	cfg    *Config
	lg     *Logger
	rate   float64
	center float64
	band   *bandState

	ring     []complex64
	ringHead int
	ringFull bool
	peak     float32

	rec        *WavWriter
	sidecar    *os.File
	start, end time.Time
	reason     string
}

func newBandRecorder(cfg *Config, lg *Logger, rate, center float64, band *bandState) *bandRecorder {
	return &bandRecorder{cfg: cfg, lg: lg, rate: rate, center: center, band: band,
		ring: make([]complex64, int(cfg.PreS*rate)), peak: 1}
}

func (b *bandRecorder) feed(iq []complex64) {
	for _, v := range iq {
		if a := absC(v); a > b.peak {
			b.peak = a
		}
	}
	on, by := b.band.active()
	switch {
	case b.rec == nil && on:
		b.open(by)
	case b.rec != nil && on:
		b.end = time.Now()
	}
	if b.rec != nil {
		b.rec.Write(iq, b.peak)
		if time.Since(b.end).Seconds() > b.cfg.PostS || time.Since(b.start).Seconds() > b.cfg.MaxS {
			b.close()
		}
		return
	}
	b.push(iq)
}

func (b *bandRecorder) push(iq []complex64) {
	if len(b.ring) == 0 {
		return
	}
	for _, v := range iq {
		b.ring[b.ringHead] = v
		b.ringHead++
		if b.ringHead == len(b.ring) {
			b.ringHead, b.ringFull = 0, true
		}
	}
}

func (b *bandRecorder) open(by float64) {
	base, err := rec.NewBase(b.cfg.Dir(), time.Now(), b.center, "band")
	if err != nil {
		b.lg.Printf("ベースバンド録音を開けない: %v", err)
		return
	}
	wr, err := NewWavWriter(rec.Path(base, rec.IQ), int(b.rate))
	if err != nil {
		b.lg.Printf("ベースバンド録音を開けない: %v", err)
		return
	}
	b.rec, b.start, b.end = wr, time.Now(), time.Now()
	b.reason = fmt.Sprintf("%.4f MHz の通話", by)
	if b.ringFull {
		wr.Write(b.ring[b.ringHead:], b.peak)
	}
	wr.Write(b.ring[:b.ringHead], b.peak)
	b.ringHead, b.ringFull = 0, false
	b.sidecar, _ = os.Create(rec.Path(base, rec.Text))
	if b.sidecar != nil {
		fmt.Fprintf(b.sidecar, "ベースバンド録音\n中心 %.4f MHz\n入力レート %.0f Hz\n開始 %s\n理由 %s\n\n",
			b.center, b.rate, time.Now().Format(time.RFC3339), b.reason)
		if b.cfg.BandHint != nil {
			fmt.Fprint(b.sidecar, b.cfg.BandHint(b.center))
		}
	}
	b.lg.Printf("ベースバンド録音開始 %s (%s)", filepath.Base(rec.Path(base, rec.IQ)), b.reason)
}

func (b *bandRecorder) close() {
	if b.rec == nil {
		return
	}
	dur := b.rec.Close()
	b.lg.Printf("ベースバンド録音終了 %.1f 秒（%.0f MB）", dur, dur*b.rate*4/1e6)
	if b.sidecar != nil {
		fmt.Fprintf(b.sidecar, "終了 %s 長さ %.1f 秒\n", time.Now().Format(time.RFC3339), dur)
		b.sidecar.Close()
		b.sidecar = nil
	}
	b.rec = nil
}

func absC(v complex64) float32 {
	return float32(math.Hypot(float64(real(v)), float64(imag(v))))
}
