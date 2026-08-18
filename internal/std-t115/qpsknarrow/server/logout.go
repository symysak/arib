package server

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)


type logWriter struct {
	dir string

	mu      sync.Mutex
	logFile *os.File
	stamp   string
	wavPath map[int]string
}

func newLogWriter(dir string, startedAt time.Time) (*logWriter, error) {
	w := &logWriter{dir: dir, wavPath: map[int]string{}}
	if dir == "" {
		return w, nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("保存先を作れません（%s）: %w", dir, err)
	}
	w.stamp = startedAt.Format("20060102-150405")
	f, err := os.Create(filepath.Join(dir, w.stamp+"_decode.log"))
	if err != nil {
		return nil, fmt.Errorf("ログを作れません: %w", err)
	}
	w.logFile = f
	return w, nil
}

func (w *logWriter) Enabled() bool { return w != nil && w.dir != "" }

func (w *logWriter) Stamp() string {
	if !w.Enabled() {
		return ""
	}
	return w.stamp
}

func (w *logWriter) LogPath() string {
	if !w.Enabled() || w.logFile == nil {
		return ""
	}
	return w.logFile.Name()
}

func (w *logWriter) WriteLog(l LogInfo, wall time.Time) {
	if !w.Enabled() || w.logFile == nil {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	fmt.Fprintf(w.logFile, "%s [%s] %s\n",
		wall.Format("2006-01-02 15:04:05.000"), l.Level, l.Text)
}

func targetPart(target string) string {
	t := strings.TrimSpace(target)
	switch {
	case t == "":
		return ""
	case strings.Contains(t, "一斉"):
		return "_一斉"
	}
	t = strings.ReplaceAll(t, "群/個別", "群個別")
	t = strings.ReplaceAll(t, " ", "")
	t = strings.ReplaceAll(t, "#", "")
	t = sanitize(t)
	if t == "" {
		return ""
	}
	return "_" + t
}

func sanitize(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch r {
		case '/', '\\', ':', '*', '?', '"', '<', '>', '|', 0:
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

func (w *logWriter) SaveBroadcast(b BroadcastInfo, pcm []int16, rate int,
	extra map[string]string) (string, error) {
	if !w.Enabled() || len(pcm) == 0 {
		return "", nil
	}
	w.mu.Lock()
	path, ok := w.wavPath[b.ID]
	if !ok {
		path = filepath.Join(w.dir,
			fmt.Sprintf("%s_broadcast%d%s.wav", w.stamp, b.ID, targetPart(b.Target)))
		w.wavPath[b.ID] = path
	}
	w.mu.Unlock()

	if err := os.WriteFile(path, WavBytes(pcm, rate), 0o644); err != nil {
		return "", fmt.Errorf("音声 WAV を書けません: %w", err)
	}
	if err := w.writeSidecar(path, b, extra); err != nil {
		return path, err
	}
	return path, nil
}

func (w *logWriter) writeSidecar(wavPath string, b BroadcastInfo,
	extra map[string]string) error {
	var sb strings.Builder
	sb.WriteString("ARIB STD-T115 QPSK ナロー方式 通報記録\n")
	sb.WriteString("（規格書 Volume 2 / チャネル間隔 7.5kHz / 11.25kbps / SCPC）\n\n")
	fmt.Fprintf(&sb, "通報 ID       : %d\n", b.ID)
	fmt.Fprintf(&sb, "報知対象      : %s\n", b.Target)
	fmt.Fprintf(&sb, "呼番号        : %d\n", b.CallNo)
	fmt.Fprintf(&sb, "緊急          : %v\n", b.Emergency)
	fmt.Fprintf(&sb, "途中参加      : %v\n", b.MidJoin)
	if v, ok := extra["start_wall"]; ok {
		fmt.Fprintf(&sb, "開始（実時刻）: %s\n", v)
	}
	if v, ok := extra["end_wall"]; ok {
		fmt.Fprintf(&sb, "終了（実時刻）: %s\n", v)
	}
	fmt.Fprintf(&sb, "長さ          : %.1f 秒\n", b.EndSec-b.StartSec)
	fmt.Fprintf(&sb, "音声          : %.2f 秒（%d フレーム）\n", b.VoiceSeconds, b.VoiceFrames)
	fmt.Fprintf(&sb, "終了理由      : %s\n", b.EndReason)
	keys := []string{"source", "scramble", "municipality", "manufacturer", "notify_raw", "cch_crc",
		"tch_crc", "voice_filled", "voice_dropped", "iq_path"}
	for _, k := range keys {
		if v, ok := extra[k]; ok && v != "" {
			fmt.Fprintf(&sb, "%-14s: %s\n", k, v)
		}
	}
	return os.WriteFile(wavPath+".txt", []byte(sb.String()), 0o644)
}

func (w *logWriter) Close() error {
	if !w.Enabled() {
		return nil
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.logFile == nil {
		return nil
	}
	err := w.logFile.Close()
	w.logFile = nil
	return err
}
