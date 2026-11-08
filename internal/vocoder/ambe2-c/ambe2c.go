package ambe2c

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"io"
	"os/exec"
	"strconv"

	"github.com/symysak/arib/internal/extbin"
	"github.com/symysak/arib/internal/vocoder/ambe"
)

const EnvDir = "AMBE2C_DIR"

var spec = extbin.Spec{
	Name:   "ambe2c_decode",
	EnvVar: EnvDir,
	Dir:    "ambe2c",
	Hint:   "scripts/std-t61/build_ambe2c.sh（Windows は build_ambe2c.ps1）でビルドしてください",
}

func Available() bool { return spec.Available() }

type Stream struct {
	rate ambe.Rate
	cmd  *exec.Cmd
	pipe io.WriteCloser
	in   *bufio.Writer
	out  *bufio.Reader
	err  error
}

func NewStream(rate ambe.Rate) (*Stream, error) {
	if rate.Bits() == 0 {
		return nil, fmt.Errorf("知らないレート %d", rate)
	}
	bin, err := spec.Find()
	if err != nil {
		return nil, err
	}
	cmd := exec.Command(bin, strconv.Itoa(int(rate)))
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
	return &Stream{rate: rate, cmd: cmd, pipe: w, in: bufio.NewWriter(w), out: bufio.NewReader(r)}, nil
}

func (s *Stream) Push(bits []uint8) ([]int16, error) {
	if len(bits) != s.rate.Bits() {
		return nil, fmt.Errorf("ビット数が %d ではなく %d", s.rate.Bits(), len(bits))
	}
	buf := make([]byte, 1+(len(bits)+7)/8)
	for i, b := range bits {
		buf[1+i>>3] |= (b & 1) << (7 - i&7)
	}
	return s.push(buf)
}

func (s *Stream) Lost() ([]int16, error) {
	buf := make([]byte, 1+(s.rate.Bits()+7)/8)
	buf[0] = 1
	return s.push(buf)
}

func (s *Stream) push(buf []byte) ([]int16, error) {
	if s.err != nil {
		return nil, s.err
	}
	if _, err := s.in.Write(buf); err != nil {
		s.err = err
		return nil, err
	}
	if err := s.in.Flush(); err != nil {
		s.err = err
		return nil, err
	}
	raw := make([]byte, ambe.SamplesPerFrame*2)
	if _, err := io.ReadFull(s.out, raw); err != nil {
		s.err = err
		return nil, err
	}
	pcm := make([]int16, ambe.SamplesPerFrame)
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
