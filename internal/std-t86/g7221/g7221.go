package g7221

import (
	"encoding/binary"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

const (
	BitsPerFrame16k = 320
	SampleRate = 16000
)

func binaryPath(name string) (string, error) {
	names := []string{name}
	if runtime.GOOS == "windows" {
		names = append(names, name+".exe")
	}
	var dirs []string
	if env := os.Getenv("STDT86_G7221_DIR"); env != "" {
		dirs = append(dirs, env)
	}
	if exe, err := os.Executable(); err == nil {
		dirs = append(dirs, filepath.Join(filepath.Dir(exe), "build", "g7221"))
	}
	if wd, err := os.Getwd(); err == nil {
		dirs = append(dirs, filepath.Join(wd, "build", "g7221"),
			filepath.Join(wd, "..", "build", "g7221"))
	}
	for _, d := range dirs {
		for _, n := range names {
			p := filepath.Join(d, n)
			if st, err := os.Stat(p); err == nil && !st.IsDir() {
				return p, nil
			}
		}
	}
	for _, n := range names {
		if p, err := exec.LookPath(n); err == nil {
			return p, nil
		}
	}
	return "", fmt.Errorf("%s が見つかりません。`bash scripts/std-t86/build_g7221.sh`"+
		"（Windows は `pwsh scripts/std-t86/build_g7221.ps1`）を実行してください"+
		"（または STDT86_G7221_DIR を設定）", name)
}

func Available(scodec bool) error {
	name := "g7221_decode"
	if scodec {
		name = "g7221_sep_decode"
	}
	_, err := binaryPath(name)
	return err
}

func FramesToPacked(frames [][]uint8) ([]byte, error) {
	out := make([]byte, 0, len(frames)*BitsPerFrame16k/8)
	var w [2]byte
	for i, f := range frames {
		if len(f) != BitsPerFrame16k {
			return nil, fmt.Errorf("フレーム %d は %d bit 必要（%d 受領）",
				i, BitsPerFrame16k, len(f))
		}
		for k := 0; k < BitsPerFrame16k; k += 16 {
			var v uint16
			for b := 0; b < 16; b++ {
				v |= uint16(f[k+b]&1) << uint(15-b)
			}
			binary.LittleEndian.PutUint16(w[:], v)
			out = append(out, w[0], w[1])
		}
	}
	return out, nil
}

func Decode(frames [][]uint8, scodec bool) ([]float32, error) {
	if len(frames) == 0 {
		return nil, nil
	}
	packed, err := FramesToPacked(frames)
	if err != nil {
		return nil, err
	}
	name := "g7221_decode"
	if scodec {
		name = "g7221_sep_decode"
	}
	bin, err := binaryPath(name)
	if err != nil {
		return nil, err
	}

	dir, err := os.MkdirTemp("", "std-t86-g7221-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	bitPath := filepath.Join(dir, "in.bit")
	pcmPath := filepath.Join(dir, "out.pcm")
	if err := os.WriteFile(bitPath, packed, 0o644); err != nil {
		return nil, err
	}

	cmd := exec.Command(bin, "0", bitPath, pcmPath,
		fmt.Sprint(SampleRate), "7000")
	cmd.Env = os.Environ()
	if scodec {
		cmd.Env = append(cmd.Env, "STDT86_SCODEC=1")
	}
	if out, err := cmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("%s の実行に失敗: %w (%s)", name, err, trim(out))
	}

	raw, err := os.ReadFile(pcmPath)
	if err != nil {
		return nil, err
	}
	pcm := make([]float32, len(raw)/2)
	for i := range pcm {
		pcm[i] = float32(int16(binary.LittleEndian.Uint16(raw[2*i:]))) / 32768.0
	}
	return pcm, nil
}

func AdaptiveMultiplex(frames [][]uint8) ([][]uint8, error) {
	if len(frames) == 0 {
		return nil, nil
	}
	packed, err := FramesToPacked(frames)
	if err != nil {
		return nil, err
	}
	bin, err := binaryPath("g7221_sep_decode")
	if err != nil {
		return nil, err
	}

	dir, err := os.MkdirTemp("", "std-t86-g7221-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	bitPath := filepath.Join(dir, "in.bit")
	pcmPath := filepath.Join(dir, "out.pcm")
	muxPath := filepath.Join(dir, "mux.txt")
	if err := os.WriteFile(bitPath, packed, 0o644); err != nil {
		return nil, err
	}

	cmd := exec.Command(bin, "0", bitPath, pcmPath,
		fmt.Sprint(SampleRate), "7000")
	cmd.Env = append(os.Environ(), "STDT86_SCODEC=2", "STDT86_MUX_OUT="+muxPath)
	if out, err := cmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("g7221_sep_decode の実行に失敗: %w (%s)", err, trim(out))
	}

	raw, err := os.ReadFile(muxPath)
	if err != nil {
		return nil, err
	}
	lines := strings.Split(strings.TrimRight(string(raw), "\n"), "\n")
	if len(lines) != len(frames) {
		return nil, fmt.Errorf("適応多重化の出力フレーム数が不正: %d != %d",
			len(lines), len(frames))
	}
	mi := make([][]uint8, len(lines))
	for i, ln := range lines {
		ln = strings.TrimRight(ln, "\r")
		if len(ln) != BitsPerFrame16k {
			return nil, fmt.Errorf("適応多重化の出力長が不正: フレーム %d は %d bit（%d 必要）",
				i, len(ln), BitsPerFrame16k)
		}
		bits := make([]uint8, BitsPerFrame16k)
		for k := 0; k < BitsPerFrame16k; k++ {
			switch ln[k] {
			case '0':
				bits[k] = 0
			case '1':
				bits[k] = 1
			default:
				return nil, fmt.Errorf("適応多重化の出力に 0/1 以外の文字: フレーム %d 位置 %d (%q)",
					i, k, string(ln[k]))
			}
		}
		mi[i] = bits
	}
	return mi, nil
}

func Encode(pcm []float32) ([][]uint8, error) {
	if len(pcm) == 0 {
		return nil, nil
	}
	bin, err := binaryPath("g7221_encode")
	if err != nil {
		return nil, err
	}

	dir, err := os.MkdirTemp("", "std-t86-g7221-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	pcmPath := filepath.Join(dir, "in.pcm")
	bitPath := filepath.Join(dir, "out.bit")

	raw := make([]byte, 2*len(pcm))
	for i, v := range pcm {
		if v > 1 {
			v = 1
		} else if v < -1 {
			v = -1
		}
		binary.LittleEndian.PutUint16(raw[2*i:], uint16(int16(v*32767)))
	}
	if err := os.WriteFile(pcmPath, raw, 0o644); err != nil {
		return nil, err
	}

	cmd := exec.Command(bin, "0", pcmPath, bitPath,
		fmt.Sprint(SampleRate), "7000")
	cmd.Env = os.Environ()
	if out, err := cmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("g7221_encode の実行に失敗: %w (%s)", err, trim(out))
	}

	packed, err := os.ReadFile(bitPath)
	if err != nil {
		return nil, err
	}
	if len(packed)%(BitsPerFrame16k/8) != 0 {
		return nil, fmt.Errorf("g7221_encode の出力が %d byte でフレーム境界に揃いません",
			len(packed))
	}
	nFrames := len(packed) / (BitsPerFrame16k / 8)
	frames := make([][]uint8, nFrames)
	for i := range frames {
		bits := make([]uint8, BitsPerFrame16k)
		for k := 0; k < BitsPerFrame16k; k += 16 {
			v := binary.LittleEndian.Uint16(packed[i*BitsPerFrame16k/8+k/8:])
			for b := 0; b < 16; b++ {
				bits[k+b] = uint8((v >> uint(15-b)) & 1)
			}
		}
		frames[i] = bits
	}
	return frames, nil
}

func trim(b []byte) string {
	if len(b) > 300 {
		b = b[:300]
	}
	return string(b)
}
