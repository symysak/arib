package iqsrc

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"strconv"
	"time"
)


const (
	frameIQ     = 0x01
	frameTuning = 0x02

	frameHeaderLen = 5
	maxFrameLen    = 16 << 20
	maxTuningLen   = 4 << 10
)

var handshakeTimeout = 10 * time.Second

type sdrsharpSource struct {
	conn net.Conn
	spec string
	feed *tuneFeed

	buf      []byte
	off      int
	iqLeft   int
	skipLeft int
	eof      bool
}

func openSDRSharp(host string, port int, spec string) (*sdrsharpSource, error) {
	addr := net.JoinHostPort(host, strconv.Itoa(port))
	conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		return nil, fmt.Errorf("%s へ接続できません: %w", addr, err)
	}
	if tc, ok := conn.(*net.TCPConn); ok {
		_ = tc.SetNoDelay(true)
	}
	s := &sdrsharpSource{conn: conn, spec: spec, feed: newTuneFeed()}
	t, err := s.readFirstTuning()
	if err != nil {
		conn.Close()
		return nil, err
	}
	s.feed.push(t)
	return s, nil
}

func (s *sdrsharpSource) readFirstTuning() (Tuning, error) {
	_ = s.conn.SetReadDeadline(time.Now().Add(handshakeTimeout))
	head := make([]byte, frameHeaderLen)
	if _, err := io.ReadFull(s.conn, head); err != nil {
		var ne net.Error
		if errors.As(err, &ne) && ne.Timeout() {
			return Tuning{}, fmt.Errorf("%s: SDR# プラグインから最初の通知が %v 届きません"+
				"（SDR# で再生（▶）を始めてからパネルの Start server を押してください）", s.spec, handshakeTimeout)
		}
		return Tuning{}, fmt.Errorf("%s: 最初の通知を読めません: %w", s.spec, err)
	}
	ln := binary.LittleEndian.Uint32(head[1:])
	if head[0] != frameTuning || ln == 0 || ln > maxTuningLen {
		return Tuning{}, fmt.Errorf("%s: SDR# プラグインの形式ではないようです（最初の枠 種類 0x%02x・長さ %d）。"+
			"生の I/Q を流す相手なら tcp:// を使ってください", s.spec, head[0], ln)
	}
	body := make([]byte, ln)
	if _, err := io.ReadFull(s.conn, body); err != nil {
		return Tuning{}, fmt.Errorf("%s: 最初の通知を読めません: %w", s.spec, err)
	}
	return parseTuning(body)
}

func parseTuning(b []byte) (Tuning, error) {
	var v struct {
		Center *float64 `json:"center"`
		VFO    *float64 `json:"vfo"`
		Rate   *float64 `json:"rate"`
	}
	if err := json.Unmarshal(b, &v); err != nil {
		return Tuning{}, fmt.Errorf("SDR# プラグインの通知を読めません: %w", err)
	}
	if v.Center == nil || v.VFO == nil || v.Rate == nil {
		return Tuning{}, fmt.Errorf("SDR# プラグインの通知に center / vfo / rate が揃っていません: %s", b)
	}
	if !(*v.Rate > 0) || math.IsInf(*v.Rate, 0) {
		return Tuning{}, fmt.Errorf("SDR# プラグインの通知のレートが不正です: %v", *v.Rate)
	}
	return Tuning{CenterHz: *v.Center, VfoHz: *v.VFO, Rate: *v.Rate}, nil
}

func (s *sdrsharpSource) SampleRate() float64 {
	t, _ := s.feed.current()
	return t.Rate
}

func (s *sdrsharpSource) CenterHz() (float64, bool) {
	t, ok := s.feed.current()
	return t.CenterHz, ok
}

func (s *sdrsharpSource) Tune() <-chan Tuning { return s.feed.ch }
func (s *sdrsharpSource) Lossy() bool         { return true }
func (s *sdrsharpSource) Len() int64          { return 0 }

func (s *sdrsharpSource) Describe() string {
	t, _ := s.feed.current()
	return fmt.Sprintf("%s（%.0f Hz, cf32, 中心 %.6f MHz, VFO %.6f MHz）",
		s.spec, t.Rate, t.CenterHz/1e6, t.VfoHz/1e6)
}

func (s *sdrsharpSource) Close() error {
	s.feed.close()
	return s.conn.Close()
}

func (s *sdrsharpSource) Read(n int) ([]complex64, error) {
	if n <= 0 {
		return nil, nil
	}
	out, err := s.parse(n)
	if err != nil || len(out) > 0 {
		return out, err
	}
	if s.eof {
		return nil, io.EOF
	}
	if err := s.fill(n); err != nil {
		var ne net.Error
		switch {
		case errors.As(err, &ne) && ne.Timeout():
		case errors.Is(err, io.EOF):
			s.eof = true
		default:
			return nil, err
		}
	}
	out, err = s.parse(n)
	if err != nil || len(out) > 0 {
		return out, err
	}
	if s.eof {
		return nil, io.EOF
	}
	return nil, nil
}

func (s *sdrsharpSource) fill(n int) error {
	if s.off > 0 {
		k := copy(s.buf, s.buf[s.off:])
		s.buf = s.buf[:k]
		s.off = 0
	}
	want := n*8 + frameHeaderLen
	if want < 4096 {
		want = 4096
	}
	if cap(s.buf)-len(s.buf) < want {
		nb := make([]byte, len(s.buf), len(s.buf)+want)
		copy(nb, s.buf)
		s.buf = nb
	}
	_ = s.conn.SetReadDeadline(time.Now().Add(recvTimeout))
	got, err := s.conn.Read(s.buf[len(s.buf):cap(s.buf)])
	s.buf = s.buf[:len(s.buf)+got]
	return err
}

func (s *sdrsharpSource) parse(n int) ([]complex64, error) {
	var out []complex64
	for len(out) < n {
		raw := s.buf[s.off:]
		switch {
		case s.skipLeft > 0:
			k := min(s.skipLeft, len(raw))
			if k == 0 {
				return out, nil
			}
			s.off += k
			s.skipLeft -= k

		case s.iqLeft > 0:
			k := min(s.iqLeft, len(raw), (n-len(out))*8)
			k -= k % 8
			if k == 0 {
				return out, nil
			}
			out = append(out, convert("cf32", raw[:k])...)
			s.off += k
			s.iqLeft -= k

		default:
			if len(raw) < frameHeaderLen {
				return out, nil
			}
			typ := raw[0]
			ln := int(binary.LittleEndian.Uint32(raw[1:frameHeaderLen]))
			switch typ {
			case frameIQ:
				if ln%8 != 0 || ln > maxFrameLen {
					return out, fmt.Errorf("%s: I/Q の枠の長さが不正です（%d バイト）", s.spec, ln)
				}
				s.off += frameHeaderLen
				s.iqLeft = ln
			case frameTuning:
				if ln > maxTuningLen {
					return out, fmt.Errorf("%s: 通知の枠が長すぎます（%d バイト）", s.spec, ln)
				}
				if len(out) > 0 {
					return out, nil
				}
				if len(raw) < frameHeaderLen+ln {
					return out, nil
				}
				t, err := parseTuning(raw[frameHeaderLen : frameHeaderLen+ln])
				if err != nil {
					return out, fmt.Errorf("%s: %w", s.spec, err)
				}
				s.off += frameHeaderLen + ln
				s.feed.push(t)
			default:
				if ln > maxFrameLen {
					return out, fmt.Errorf("%s: 枠の長さが不正です（種類 0x%02x・%d バイト）", s.spec, typ, ln)
				}
				s.off += frameHeaderLen
				s.skipLeft = ln
			}
		}
	}
	return out, nil
}
