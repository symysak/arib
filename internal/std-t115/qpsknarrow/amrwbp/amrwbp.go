package amrwbp

import (
	"encoding/binary"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

const (
	FrameType = 17
	ISFIndex = 1
	FrameBytes = 30
	FramesPerSuperframe = 4
	OutputRate = 16000
	RadioFrameSec = 0.080
	MaxGapFrames = 63
)

type Frame struct {
	Payload []byte
	TFI int
	Lost bool
}

type Assembler struct {
	pending []Frame
	out     [][]Frame
	dropped int

	lastT    float64
	lastSN   int
	haveLast bool
	filled   int
	capped   int
}

func (a *Assembler) Push(tSec float64, sn int, voice []byte) {
	if a.haveLast {
		miss := int(math.Round((tSec-a.lastT)/RadioFrameSec)) - 1
		if miss > 0 {
			if miss > MaxGapFrames {
				a.capped += miss - MaxGapFrames
				miss = MaxGapFrames
			}
			s := a.lastSN
			for i := 0; i < miss; i++ {
				s = 1 - s
				a.push1(s, nil)
				a.filled++
			}
		}
	}
	a.lastT = tSec
	a.lastSN = sn
	a.haveLast = true
	a.push1(sn, voice)
}

func (a *Assembler) push1(sn int, voice []byte) {
	mk := func(idx, tfi int) Frame {
		f := Frame{TFI: tfi}
		lo, hi := idx*FrameBytes, (idx+1)*FrameBytes
		if len(voice) >= hi {
			f.Payload = voice[lo:hi]
		} else {
			f.Lost = true
			f.Payload = make([]byte, FrameBytes)
		}
		return f
	}
	switch sn {
	case 0:
		if len(a.pending) > 0 {
			a.dropped++
		}
		a.pending = []Frame{mk(0, 0), mk(1, 1)}
	case 1:
		if len(a.pending) != 2 {
			a.dropped++
			return
		}
		sf := append(a.pending, mk(0, 2), mk(1, 3))
		a.out = append(a.out, sf)
		a.pending = nil
	}
}

func (a *Assembler) PushLost(sn int) { a.push1(sn, nil) }

func (a *Assembler) Filled() int { return a.filled }

func (a *Assembler) Capped() int { return a.capped }

func (a *Assembler) Superframes() [][]Frame {
	out := a.out
	a.out = nil
	return out
}

func (a *Assembler) Dropped() int { return a.dropped }

func RawStream(superframes [][]Frame) (raw []byte, fer []int) {
	for _, sf := range superframes {
		for _, f := range sf {
			raw = append(raw, byte(FrameType&0x7F))
			raw = append(raw, byte(f.TFI<<6)|byte(ISFIndex&0x1F))
			p := f.Payload
			if len(p) < FrameBytes {
				p = append(append([]byte{}, p...), make([]byte, FrameBytes-len(p))...)
			}
			raw = append(raw, p[:FrameBytes]...)
			if f.Lost {
				fer = append(fer, 1)
			} else {
				fer = append(fer, 0)
			}
		}
	}
	return raw, fer
}

func binaryPath() (string, error) {
	names := []string{"amrwbp_decoder", "amrwbp_decoder.exe"}
	if runtime.GOOS == "windows" {
		names = []string{"amrwbp_decoder.exe", "amrwbp_decoder"}
	}
	exeDir := ""
	if exe, err := os.Executable(); err == nil {
		exeDir = filepath.Dir(exe)
	}
	dirs := searchDirs(os.Getenv("STDT115_AMRWBP_DIR"), exeDir)
	var cands []string
	for _, d := range dirs {
		for _, n := range names {
			cands = append(cands, filepath.Join(d, n))
		}
	}
	for _, c := range cands {
		if st, err := os.Stat(c); err == nil && !st.IsDir() {
			abs, err := filepath.Abs(c)
			if err == nil {
				return abs, nil
			}
			return c, nil
		}
	}
	return "", fmt.Errorf("amrwbp_decoder が見つかりません（bash build_amrwbplus.sh / "+
		"pwsh build_amrwbplus.ps1 を実行してください。場所を指定するなら STDT115_AMRWBP_DIR）: "+
		"探した先 %s", strings.Join(cands, ", "))
}

func searchDirs(env, exeDir string) []string {
	var dirs []string
	if env != "" {
		dirs = append(dirs, env)
	}
	if exeDir != "" {
		dirs = append(dirs, filepath.Join(exeDir, "build", "amrwbplus"), exeDir)
	}
	up := ""
	for i := 0; i < 6; i++ {
		dirs = append(dirs, filepath.Join(up, "build", "amrwbplus"))
		up = filepath.Join(up, "..")
	}
	return dirs
}

func Available() bool {
	_, err := binaryPath()
	return err == nil
}

func Decode(superframes [][]Frame) ([]int16, error) {
	if len(superframes) == 0 {
		return nil, nil
	}
	bin, err := binaryPath()
	if err != nil {
		return nil, err
	}
	raw, fer := RawStream(superframes)

	dir, err := os.MkdirTemp("", "amrwbp")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)

	inPath := filepath.Join(dir, "in.wbp")
	ferPath := filepath.Join(dir, "fer.txt")
	outPath := filepath.Join(dir, "out.wav")
	if err := os.WriteFile(inPath, raw, 0o600); err != nil {
		return nil, err
	}
	var sb strings.Builder
	for _, v := range fer {
		sb.WriteString(strconv.Itoa(v))
		sb.WriteByte('\n')
	}
	if err := os.WriteFile(ferPath, []byte(sb.String()), 0o600); err != nil {
		return nil, err
	}

	cmd := exec.Command(bin,
		"-ff", "raw",
		"-fs", strconv.Itoa(OutputRate),
		"-mono",
		"-limiter",
		"-if", inPath,
		"-of", outPath,
		"-fer", ferPath,
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("amrwbp_decoder 失敗: %v: %s", err, truncate(out, 400))
	}
	return readMonoWav(outPath)
}

func truncate(b []byte, n int) string {
	s := strings.TrimSpace(string(b))
	if len(s) > n {
		return s[:n] + "…"
	}
	return s
}

func readMonoWav(path string) ([]int16, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if len(b) < 12 || string(b[0:4]) != "RIFF" || string(b[8:12]) != "WAVE" {
		return nil, fmt.Errorf("%s: RIFF/WAVE ではない", path)
	}
	pos := 12
	channels, bits := 1, 16
	for pos+8 <= len(b) {
		id := string(b[pos : pos+4])
		size := int(binary.LittleEndian.Uint32(b[pos+4 : pos+8]))
		body := pos + 8
		switch id {
		case "fmt ":
			if body+16 <= len(b) {
				channels = int(binary.LittleEndian.Uint16(b[body+2 : body+4]))
				bits = int(binary.LittleEndian.Uint16(b[body+14 : body+16]))
			}
		case "data":
			end := body + size
			if size <= 0 || end > len(b) {
				end = len(b)
			}
			if bits != 16 {
				return nil, fmt.Errorf("%s: %dbit は未対応", path, bits)
			}
			n := (end - body) / 2
			all := make([]int16, n)
			for i := 0; i < n; i++ {
				all[i] = int16(binary.LittleEndian.Uint16(b[body+2*i:]))
			}
			if channels <= 1 {
				return all, nil
			}
			out := make([]int16, 0, n/channels)
			for i := 0; i+channels <= n; i += channels {
				out = append(out, all[i])
			}
			return out, nil
		}
		pos = body + size
		if size%2 == 1 {
			pos++
		}
	}
	return nil, fmt.Errorf("%s: data チャンクが無い", path)
}

func RMS(pcm []int16) float64 {
	if len(pcm) == 0 {
		return 0
	}
	var s float64
	for _, v := range pcm {
		s += float64(v) * float64(v)
	}
	return math.Sqrt(s / float64(len(pcm)))
}
