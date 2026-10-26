package iqsrc

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"sync"
	"time"
)

const (
	rtlCmdSetFreq       = 0x01
	rtlCmdSetSampleRate = 0x02
	rtlCmdSetGainMode   = 0x03
	rtlCmdSetGain       = 0x04
	rtlCmdSetAGCMode    = 0x08
)

type rtlSource struct {
	*socketSource
	wmu  sync.Mutex
	fs   float64
	feed *tuneFeed
}

func openRtlTCP(host string, port int, spec string, fs, freqHz, gainDB float64, agc bool) (*rtlSource, error) {
	s, err := newSocket(host, port, fs, "cu8", spec, rtlReadHeader)
	if err != nil {
		return nil, err
	}
	r := &rtlSource{socketSource: s, fs: fs, feed: newTuneFeed()}
	if err := r.init(freqHz, gainDB, agc); err != nil {
		r.Close()
		return nil, err
	}
	return r, nil
}

func (r *rtlSource) init(freqHz, gainDB float64, agc bool) error {
	if err := r.send(rtlCmdSetSampleRate, r.fs); err != nil {
		return err
	}
	if freqHz > 0 {
		if err := r.SetFreq(freqHz); err != nil {
			return err
		}
	}
	return r.SetGain(gainDB, agc)
}

func rtlReadHeader(c net.Conn) error {
	_ = c.SetReadDeadline(time.Now().Add(5 * time.Second))
	head := make([]byte, 12)
	if _, err := io.ReadFull(c, head); err != nil {
		return fmt.Errorf("rtl_tcp のヘッダを読めません: %w", err)
	}
	if string(head[0:4]) != "RTL0" {
		return fmt.Errorf("rtl_tcp ではないようです（マジック %q）", head[0:4])
	}
	return nil
}

func (r *rtlSource) send(cmd byte, v float64) error {
	if math.IsNaN(v) || v < 0 || v > math.MaxUint32 {
		return fmt.Errorf("rtl_tcp のコマンド 0x%02x の値が範囲外です: %v", cmd, v)
	}
	b := []byte{cmd, 0, 0, 0, 0}
	binary.BigEndian.PutUint32(b[1:], uint32(v))
	r.wmu.Lock()
	defer r.wmu.Unlock()
	_ = r.conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
	if _, err := r.conn.Write(b); err != nil {
		return fmt.Errorf("rtl_tcp へコマンドを送れません: %w", err)
	}
	return nil
}

func (r *rtlSource) SetFreq(hz float64) error {
	if hz <= 0 {
		return errors.New("rtl_tcp の周波数は正の値で指定してください")
	}
	if err := r.send(rtlCmdSetFreq, hz); err != nil {
		return err
	}
	r.feed.push(Tuning{CenterHz: hz, VfoHz: hz, Rate: r.fs})
	return nil
}

func (r *rtlSource) SetGain(db float64, agc bool) error {
	if agc {
		if err := r.send(rtlCmdSetGainMode, 0); err != nil {
			return err
		}
		return r.send(rtlCmdSetAGCMode, 1)
	}
	if err := r.send(rtlCmdSetGainMode, 1); err != nil {
		return err
	}
	return r.send(rtlCmdSetGain, math.Round(db*10))
}

func (r *rtlSource) Tune() <-chan Tuning { return r.feed.ch }

func (r *rtlSource) CenterHz() (float64, bool) {
	t, ok := r.feed.current()
	return t.CenterHz, ok
}

func (r *rtlSource) Close() error {
	r.feed.close()
	return r.socketSource.Close()
}
