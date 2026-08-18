package iqsrc

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"strconv"
	"strings"
	"time"
)


type Source interface {
	SampleRate() float64
	Read(n int) ([]complex64, error)
	Lossy() bool
	Len() int64
	Describe() string
	Close() error
}

type Options struct {
	SampleRate float64
	Format string
	FreqHz float64
	GainDB float64
	AGC bool
	Realtime bool
	Speed float64
}

func Open(spec string, o Options) (Source, error) {
	switch {
	case strings.HasPrefix(spec, "tcp://"):
		host, port, err := splitHostPort(strings.TrimPrefix(spec, "tcp://"))
		if err != nil {
			return nil, err
		}
		if o.SampleRate <= 0 {
			return nil, errors.New("tcp:// では -fs（サンプルレート）が必要です")
		}
		format := o.Format
		if format == "" {
			format = "cf32"
		}
		return newSocket(host, port, o.SampleRate, format, spec, nil)

	case strings.HasPrefix(spec, "rtltcp://"):
		host, port, err := splitHostPort(strings.TrimPrefix(spec, "rtltcp://"))
		if err != nil {
			return nil, err
		}
		fs := o.SampleRate
		if fs <= 0 {
			fs = 1024000
		}
		init := func(c net.Conn) error {
			return rtlHandshake(c, fs, o.FreqHz, o.GainDB, o.AGC)
		}
		return newSocket(host, port, fs, "cu8", spec, init)

	default:
		return OpenFileSource(spec, o.SampleRate, o.Realtime, o.Speed)
	}
}

func IsNetwork(spec string) bool {
	return strings.HasPrefix(spec, "tcp://") || strings.HasPrefix(spec, "rtltcp://")
}

func splitHostPort(s string) (string, int, error) {
	host, portStr, err := net.SplitHostPort(s)
	if err != nil {
		return "", 0, fmt.Errorf("host:port の形式ではありません: %q", s)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil || port <= 0 || port > 65535 {
		return "", 0, fmt.Errorf("ポート番号が不正です: %q", portStr)
	}
	if host == "" {
		host = "127.0.0.1"
	}
	return host, port, nil
}

var recvTimeout = 1 * time.Second

type socketSource struct {
	conn     net.Conn
	fs       float64
	format   string
	spec     string
	bytesPer int
	pending  []byte
	buf      []byte
}

func newSocket(host string, port int, fs float64, format, spec string,
	init func(net.Conn) error) (*socketSource, error) {
	bp, err := bytesPerSample(format)
	if err != nil {
		return nil, err
	}
	addr := net.JoinHostPort(host, strconv.Itoa(port))
	conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		return nil, fmt.Errorf("%s へ接続できません: %w", addr, err)
	}
	if tc, ok := conn.(*net.TCPConn); ok {
		_ = tc.SetNoDelay(true)
	}
	if init != nil {
		if err := init(conn); err != nil {
			conn.Close()
			return nil, err
		}
	}
	return &socketSource{conn: conn, fs: fs, format: format, spec: spec, bytesPer: bp}, nil
}

func bytesPerSample(format string) (int, error) {
	switch strings.ToLower(format) {
	case "cu8", "u8":
		return 2, nil
	case "s16", "cs16", "i16":
		return 4, nil
	case "f32", "cf32", "fc32":
		return 8, nil
	}
	return 0, fmt.Errorf("未対応の標本形式 %q（cu8 / s16 / cf32）", format)
}

func (s *socketSource) SampleRate() float64 { return s.fs }
func (s *socketSource) Lossy() bool         { return true }
func (s *socketSource) Len() int64          { return 0 }
func (s *socketSource) Close() error        { return s.conn.Close() }

func (s *socketSource) Describe() string {
	return fmt.Sprintf("%s（%.0f Hz, %s）", s.spec, s.fs, s.format)
}

func (s *socketSource) Read(n int) ([]complex64, error) {
	if n <= 0 {
		return nil, nil
	}
	want := n*s.bytesPer - len(s.pending)
	if want < s.bytesPer {
		want = s.bytesPer
	}
	if cap(s.buf) < want {
		s.buf = make([]byte, want)
	}
	_ = s.conn.SetReadDeadline(time.Now().Add(recvTimeout))
	got, err := s.conn.Read(s.buf[:want])
	if got > 0 {
		s.pending = append(s.pending, s.buf[:got]...)
	}
	if err != nil {
		var ne net.Error
		if errors.As(err, &ne) && ne.Timeout() {
			err = nil
		} else if errors.Is(err, io.EOF) {
			if len(s.pending) < s.bytesPer {
				return nil, io.EOF
			}
			err = nil
		} else {
			return nil, err
		}
	}
	usable := len(s.pending) / s.bytesPer
	if usable == 0 {
		return nil, nil
	}
	if usable > n {
		usable = n
	}
	nb := usable * s.bytesPer
	out := convert(s.format, s.pending[:nb])
	s.pending = append(s.pending[:0], s.pending[nb:]...)
	return out, nil
}

func convert(format string, b []byte) []complex64 {
	switch strings.ToLower(format) {
	case "cu8", "u8":
		out := make([]complex64, len(b)/2)
		for i := range out {
			out[i] = complex(float32(b[2*i])-127.5, float32(b[2*i+1])-127.5)
		}
		return out
	case "s16", "cs16", "i16":
		out := make([]complex64, len(b)/4)
		for i := range out {
			re := int16(binary.LittleEndian.Uint16(b[4*i:]))
			im := int16(binary.LittleEndian.Uint16(b[4*i+2:]))
			out[i] = complex(float32(re), float32(im))
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

const (
	rtlCmdSetFreq       = 0x01
	rtlCmdSetSampleRate = 0x02
	rtlCmdSetGainMode   = 0x03
	rtlCmdSetGain       = 0x04
	rtlCmdSetAGCMode    = 0x08
)

func rtlHandshake(c net.Conn, fs, freqHz, gainDB float64, agc bool) error {
	_ = c.SetReadDeadline(time.Now().Add(5 * time.Second))
	head := make([]byte, 12)
	if _, err := io.ReadFull(c, head); err != nil {
		return fmt.Errorf("rtl_tcp のヘッダを読めません: %w", err)
	}
	if string(head[0:4]) != "RTL0" {
		return fmt.Errorf("rtl_tcp ではないようです（マジック %q）", head[0:4])
	}
	send := func(cmd byte, v uint32) error {
		b := []byte{cmd, 0, 0, 0, 0}
		binary.BigEndian.PutUint32(b[1:], v)
		_, err := c.Write(b)
		return err
	}
	if err := send(rtlCmdSetSampleRate, uint32(fs)); err != nil {
		return err
	}
	if freqHz > 0 {
		if err := send(rtlCmdSetFreq, uint32(freqHz)); err != nil {
			return err
		}
	}
	if agc {
		if err := send(rtlCmdSetGainMode, 0); err != nil {
			return err
		}
		if err := send(rtlCmdSetAGCMode, 1); err != nil {
			return err
		}
	} else {
		if err := send(rtlCmdSetGainMode, 1); err != nil {
			return err
		}
		if err := send(rtlCmdSetGain, uint32(gainDB*10)); err != nil {
			return err
		}
	}
	return nil
}

type fileSource struct {
	r        *Reader
	realtime bool
	speed    float64
	started  time.Time
	pos      int64
	spec     string
}

func OpenFileSource(path string, fs float64, realtime bool, speed float64) (Source, error) {
	r, err := OpenFile(path, fs)
	if err != nil {
		return nil, err
	}
	if speed <= 0 {
		speed = 1
	}
	return &fileSource{r: r, realtime: realtime, speed: speed, spec: path}, nil
}

func (f *fileSource) SampleRate() float64 { return f.r.SampleRate() }
func (f *fileSource) Lossy() bool         { return false }
func (f *fileSource) Len() int64          { return f.r.Len() }
func (f *fileSource) Close() error        { return f.r.Close() }

func (f *fileSource) Describe() string {
	return fmt.Sprintf("%s（%.0f Hz, %.1f 秒, %s）", f.spec, f.r.SampleRate(),
		float64(f.r.Len())/f.r.SampleRate(),
		map[bool]string{true: "実時間再生", false: "全速"}[f.realtime])
}

func (f *fileSource) Read(n int) ([]complex64, error) {
	if f.started.IsZero() {
		f.started = time.Now()
	}
	if f.realtime {
		target := f.started.Add(time.Duration(
			float64(f.pos) / f.r.SampleRate() / f.speed * float64(time.Second)))
		if d := time.Until(target); d > 0 {
			time.Sleep(d)
		}
	}
	x, err := f.r.Read(n)
	f.pos += int64(len(x))
	if len(x) == 0 && err == nil {
		return nil, io.EOF
	}
	return x, err
}

func (f *fileSource) Skip(n int64) error {
	if err := f.r.Skip(n); err != nil {
		return err
	}
	f.pos += n
	return nil
}

type Skipper interface{ Skip(int64) error }
