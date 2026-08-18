package iqsrc

import (
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"
)

type Format string

const (
	FormatS16 Format = "s16"
	FormatF32 Format = "f32"
	FormatCU8 Format = "cu8"
)

type Reader struct {
	f          *os.File
	format     Format
	sampleRate float64
	dataStart  int64
	dataLen    int64
	pos        int64
	bytesPer   int
}

func OpenFile(path string, fs float64) (*Reader, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	r := &Reader{f: f, sampleRate: fs}
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".wav":
		if err := r.readWavHeader(fs); err != nil {
			f.Close()
			return nil, err
		}
	case ".cf32", ".fc32":
		r.format, r.bytesPer = FormatF32, 8
		r.dataStart, r.dataLen = 0, fileSize(f)
	case ".cu8", ".u8":
		r.format, r.bytesPer = FormatCU8, 2
		r.dataStart, r.dataLen = 0, fileSize(f)
	default:
		f.Close()
		return nil, fmt.Errorf("未対応の拡張子 %q（.wav/.cf32/.cu8 のみ）", ext)
	}
	if r.sampleRate <= 0 {
		f.Close()
		return nil, fmt.Errorf("サンプルレートが分かりません（--fs で指定してください）")
	}
	if _, err := f.Seek(r.dataStart, io.SeekStart); err != nil {
		f.Close()
		return nil, err
	}
	return r, nil
}

func fileSize(f *os.File) int64 {
	st, err := f.Stat()
	if err != nil {
		return 0
	}
	return st.Size()
}

func (r *Reader) readWavHeader(wantFS float64) error {
	head := make([]byte, 12)
	if _, err := io.ReadFull(r.f, head); err != nil {
		return err
	}
	if string(head[0:4]) != "RIFF" || string(head[8:12]) != "WAVE" {
		return fmt.Errorf("RIFF/WAVE ではありません")
	}
	pos := int64(12)
	var channels, bits, audioFmt int
	var rate float64
	for {
		hdr := make([]byte, 8)
		if _, err := r.f.ReadAt(hdr, pos); err != nil {
			return fmt.Errorf("data チャンクが見つかりません")
		}
		id := string(hdr[0:4])
		size := int64(binary.LittleEndian.Uint32(hdr[4:8]))
		body := pos + 8
		switch id {
		case "fmt ":
			b := make([]byte, size)
			if _, err := r.f.ReadAt(b, body); err != nil {
				return err
			}
			if len(b) < 16 {
				return fmt.Errorf("fmt チャンクが短い")
			}
			audioFmt = int(binary.LittleEndian.Uint16(b[0:2]))
			channels = int(binary.LittleEndian.Uint16(b[2:4]))
			rate = float64(binary.LittleEndian.Uint32(b[4:8]))
			bits = int(binary.LittleEndian.Uint16(b[14:16]))
		case "data":
			if channels != 2 {
				return fmt.Errorf("I/Q は 2ch WAV が前提です（このファイルは %dch）", channels)
			}
			switch {
			case bits == 16:
				r.format, r.bytesPer = FormatS16, 4
			case bits == 32 && audioFmt == 3:
				r.format, r.bytesPer = FormatF32, 8
			default:
				return fmt.Errorf("未対応の WAV 標本形式（%dbit, fmt=%d）", bits, audioFmt)
			}
			r.dataStart = body
			r.dataLen = size
			if avail := fileSize(r.f) - body; r.dataLen <= 0 || r.dataLen > avail {
				r.dataLen = avail
			}
			if wantFS <= 0 {
				r.sampleRate = rate
			}
			return nil
		}
		pos = body + size
		if size%2 == 1 {
			pos++
		}
	}
}

func (r *Reader) SampleRate() float64 { return r.sampleRate }

func (r *Reader) Len() int64 { return r.dataLen / int64(r.bytesPer) }

func (r *Reader) Close() error { return r.f.Close() }

func (r *Reader) Skip(n int64) error {
	off := n * int64(r.bytesPer)
	if off > r.dataLen {
		off = r.dataLen
	}
	if _, err := r.f.Seek(r.dataStart+off, io.SeekStart); err != nil {
		return err
	}
	r.pos = off
	return nil
}

func (r *Reader) Read(n int) ([]complex64, error) {
	if n <= 0 {
		return nil, nil
	}
	remain := (r.dataLen - r.pos) / int64(r.bytesPer)
	if remain <= 0 {
		return nil, io.EOF
	}
	if int64(n) > remain {
		n = int(remain)
	}
	buf := make([]byte, n*r.bytesPer)
	got, err := io.ReadFull(r.f, buf)
	if got == 0 {
		if err == nil {
			err = io.EOF
		}
		return nil, err
	}
	buf = buf[:got-got%r.bytesPer]
	r.pos += int64(len(buf))
	out := make([]complex64, len(buf)/r.bytesPer)
	switch r.format {
	case FormatS16:
		for i := range out {
			re := int16(binary.LittleEndian.Uint16(buf[i*4:]))
			im := int16(binary.LittleEndian.Uint16(buf[i*4+2:]))
			out[i] = complex(float32(re), float32(im))
		}
	case FormatF32:
		for i := range out {
			re := math.Float32frombits(binary.LittleEndian.Uint32(buf[i*8:]))
			im := math.Float32frombits(binary.LittleEndian.Uint32(buf[i*8+4:]))
			out[i] = complex(re, im)
		}
	case FormatCU8:
		for i := range out {
			out[i] = complex(float32(buf[i*2])-127.5, float32(buf[i*2+1])-127.5)
		}
	}
	return out, nil
}
