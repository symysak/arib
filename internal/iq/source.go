package iq

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)


type Source interface {
	SampleRate() float64
	Lossy() bool
	Read(n int) ([]complex64, error)
	CenterHz() (float64, bool)
	Close() error
}

const (
	rtlSetFreq       = 0x01
	rtlSetSampleRate = 0x02
	rtlSetGainMode   = 0x03
	rtlSetAGCMode    = 0x08
)

var recvTimeout = 1 * time.Second

var sampleBytes = map[string]int{"cu8": 2, "cs16": 4, "cf32": 8}

func convertSamples(format string, b []byte) []complex64 {
	switch format {
	case "cu8":
		out := make([]complex64, len(b)/2)
		for i := range out {
			out[i] = complex((float32(b[2*i])-127.5)/127.5, (float32(b[2*i+1])-127.5)/127.5)
		}
		return out
	case "cs16":
		out := make([]complex64, len(b)/4)
		for i := range out {
			re := float32(int16(binary.LittleEndian.Uint16(b[4*i:]))) / 32768.0
			im := float32(int16(binary.LittleEndian.Uint16(b[4*i+2:]))) / 32768.0
			out[i] = complex(re, im)
		}
		return out
	default:
		out := make([]complex64, len(b)/8)
		for i := range out {
			re := math.Float32frombits(binary.LittleEndian.Uint32(b[8*i:]))
			im := math.Float32frombits(binary.LittleEndian.Uint32(b[8*i+4:]))
			out[i] = complex(re, im)
		}
		return out
	}
}

type socketSource struct {
	conn    net.Conn
	fs      float64
	format  string
	bps     int
	pending []byte
	center  float64
	hasCntr bool
	closeMu sync.Mutex
	closed  bool
	readBuf []byte
}

func newSocketSource(host string, port int, fs float64, format string,
	dialTimeout time.Duration) (*socketSource, error) {
	bps, ok := sampleBytes[format]
	if !ok {
		return nil, fmt.Errorf("fmt は cu8/cs16/cf32 のいずれか（%q 受領）", format)
	}
	conn, err := net.DialTimeout("tcp", net.JoinHostPort(host, strconv.Itoa(port)),
		dialTimeout)
	if err != nil {
		return nil, err
	}
	return &socketSource{conn: conn, fs: fs, format: format, bps: bps,
		readBuf: make([]byte, 65536)}, nil
}

func (s *socketSource) SampleRate() float64 { return s.fs }
func (s *socketSource) Lossy() bool         { return true }

func (s *socketSource) CenterHz() (float64, bool) { return s.center, s.hasCntr }

func (s *socketSource) Close() error {
	s.closeMu.Lock()
	defer s.closeMu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true
	return s.conn.Close()
}

func (s *socketSource) recvExact(n int, timeout time.Duration) ([]byte, error) {
	buf := make([]byte, n)
	if err := s.conn.SetReadDeadline(time.Now().Add(timeout)); err != nil {
		return nil, err
	}
	_, err := io.ReadFull(s.conn, buf)
	return buf, err
}

func (s *socketSource) Read(n int) ([]complex64, error) {
	need := n * s.bps
	for len(s.pending) < need {
		if err := s.conn.SetReadDeadline(time.Now().Add(recvTimeout)); err != nil {
			return nil, io.EOF
		}
		want := need - len(s.pending)
		if want > len(s.readBuf) {
			want = len(s.readBuf)
		}
		got, err := s.conn.Read(s.readBuf[:want])
		if got > 0 {
			s.pending = append(s.pending, s.readBuf[:got]...)
		}
		if err != nil {
			var ne net.Error
			if errors.As(err, &ne) && ne.Timeout() {
				return nil, nil
			}
			return nil, io.EOF
		}
	}
	raw := s.pending[:need]
	s.pending = append([]byte(nil), s.pending[need:]...)
	return convertSamples(s.format, raw), nil
}

func NewRawTCPSource(host string, port int, fs float64, format string) (Source, error) {
	return newSocketSource(host, port, fs, format, 10*time.Second)
}

func NewRtlTCPSource(host string, port int, fs, freqHz float64, agc bool) (Source, error) {
	s, err := newSocketSource(host, port, fs, "cu8", 10*time.Second)
	if err != nil {
		return nil, err
	}
	header, err := s.recvExact(12, 10*time.Second)
	if err != nil || string(header[:4]) != "RTL0" {
		s.Close()
		return nil, fmt.Errorf("rtl_tcp ヘッダ（RTL0）を受信できませんでした")
	}
	if freqHz != 0 {
		s.center, s.hasCntr = freqHz, true
	}
	cmd := func(id byte, v uint32) error {
		var b [5]byte
		b[0] = id
		binary.BigEndian.PutUint32(b[1:], v)
		_, err := s.conn.Write(b[:])
		return err
	}
	if err := cmd(rtlSetSampleRate, uint32(fs)); err != nil {
		s.Close()
		return nil, err
	}
	if freqHz != 0 {
		if err := cmd(rtlSetFreq, uint32(freqHz)); err != nil {
			s.Close()
			return nil, err
		}
	}
	gainMode, agcMode := uint32(1), uint32(0)
	if agc {
		gainMode, agcMode = 0, 1
	}
	if err := cmd(rtlSetGainMode, gainMode); err != nil {
		s.Close()
		return nil, err
	}
	if err := cmd(rtlSetAGCMode, agcMode); err != nil {
		s.Close()
		return nil, err
	}
	return s, nil
}

type FileReplaySource struct {
	r        *Reader
	realtime bool
	speed    float64

	mu     sync.Mutex
	closed bool

	t0    time.Time
	begun bool
	sent  int64
}

func NewFileReplaySource(path string, fs float64, realtime bool, speed float64) (*FileReplaySource, error) {
	r, err := Open(path, fs)
	if err != nil {
		return nil, err
	}
	if speed <= 0 {
		speed = 1.0
	}
	return &FileReplaySource{r: r, realtime: realtime, speed: speed}, nil
}

func (f *FileReplaySource) SampleRate() float64       { return f.r.SampleRate() }
func (f *FileReplaySource) Lossy() bool               { return false }
func (f *FileReplaySource) CenterHz() (float64, bool) { return 0, false }

func (f *FileReplaySource) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closed {
		return nil
	}
	f.closed = true
	return f.r.Close()
}

func (f *FileReplaySource) Read(n int) ([]complex64, error) {
	f.mu.Lock()
	if f.closed {
		f.mu.Unlock()
		return nil, io.EOF
	}
	chunk, err := f.r.Read(n)
	f.mu.Unlock()
	if err != nil {
		return nil, io.EOF
	}
	if f.realtime && len(chunk) > 0 {
		if !f.begun {
			f.t0, f.begun = time.Now(), true
		}
		f.sent += int64(len(chunk))
		due := f.t0.Add(time.Duration(float64(f.sent) /
			(f.r.SampleRate() * f.speed) * float64(time.Second)))
		if d := time.Until(due); d > 0 {
			time.Sleep(d)
		}
	}
	return chunk, nil
}

func OpenSource(spec string, fs, freqHz float64, format string,
	realtime bool, speed float64) (Source, error) {
	if strings.HasPrefix(spec, "rtltcp://") || strings.HasPrefix(spec, "tcp://") {
		u, err := url.Parse(spec)
		if err != nil {
			return nil, err
		}
		port, err := strconv.Atoi(u.Port())
		if err != nil || u.Hostname() == "" {
			return nil, fmt.Errorf("ソース URI に host:port が必要です: %s", spec)
		}
		if fs <= 0 {
			return nil, fmt.Errorf("ネットワークソースには --fs（サンプルレート）が必須です")
		}
		if strings.HasPrefix(spec, "rtltcp://") {
			return NewRtlTCPSource(u.Hostname(), port, fs, freqHz, true)
		}
		if format == "auto" || format == "" {
			format = "cf32"
		}
		return NewRawTCPSource(u.Hostname(), port, fs, format)
	}
	return NewFileReplaySource(spec, fs, realtime, speed)
}
