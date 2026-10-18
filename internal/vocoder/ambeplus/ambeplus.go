package ambeplus

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"io"
	"os/exec"

	"github.com/symysak/arib/internal/extbin"
)

const SampleRate = 8000

const SamplesPerFrame = 160

const EnvDir = "MBELIB_NEO_DIR"

var spec = extbin.Spec{
	Name:   "ambeplus_decode",
	EnvVar: EnvDir,
	Dir:    "mbelib-neo",
	Hint:   "scripts/std-t79/build_mbelib_neo.sh でビルドしてください",
}

func BinaryPath() (string, error) { return spec.Find() }

func Available() bool { return spec.Available() }

type Stream struct {
	cmd  *exec.Cmd
	pipe io.WriteCloser
	in   *bufio.Writer
	out  *bufio.Reader
	err  error
}

func NewStream() (*Stream, error) {
	bin, err := BinaryPath()
	if err != nil {
		return nil, err
	}
	cmd := exec.Command(bin)
	w, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	r, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return &Stream{cmd: cmd, pipe: w, in: bufio.NewWriter(w), out: bufio.NewReader(r)}, nil
}

func (s *Stream) Push(params []uint8) ([]int16, error) {
	if s.err != nil {
		return nil, s.err
	}
	if len(params) != ParamBits {
		return nil, fmt.Errorf("ビット数が %d ではなく %d", ParamBits, len(params))
	}
	var buf [ParamBits / 8]byte
	for i, b := range params {
		buf[i>>3] |= (b & 1) << (7 - i&7)
	}
	if _, err := s.in.Write(buf[:]); err != nil {
		s.err = err
		return nil, err
	}
	if err := s.in.Flush(); err != nil {
		s.err = err
		return nil, err
	}
	raw := make([]byte, SamplesPerFrame*2)
	if _, err := io.ReadFull(s.out, raw); err != nil {
		s.err = err
		return nil, err
	}
	pcm := make([]int16, SamplesPerFrame)
	for i := range pcm {
		pcm[i] = int16(binary.LittleEndian.Uint16(raw[2*i:]))
	}
	return pcm, nil
}

func (s *Stream) Close() error {
	if s.cmd == nil {
		return nil
	}
	s.in.Flush()
	s.pipe.Close()
	_ = s.cmd.Process.Kill()
	_ = s.cmd.Wait()
	s.cmd = nil
	return nil
}
