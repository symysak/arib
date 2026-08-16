package iq

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
	FormatCU8  Format = "cu8"
	FormatCF32 Format = "cf32"
	FormatWAV  Format = "wav"
)

func DetectFormat(path string) (Format, error) {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".cu8", ".bin":
		return FormatCU8, nil
	case ".cf32", ".fc32", ".iq":
		return FormatCF32, nil
	case ".wav":
		return FormatWAV, nil
	default:
		return "", fmt.Errorf("拡張子から I/Q 形式を判別できません: %s", path)
	}
}

type Reader struct {
	f          *os.File
	format     Format
	sampleRate float64
	bytesPer   int
	remaining  int64
	bits       int
	isFloat    bool
	buf        []byte
}

func (r *Reader) SampleRate() float64 { return r.sampleRate }

func (r *Reader) Len() int64 {
	if r.bytesPer == 0 {
		return -1
	}
	return r.remaining / int64(r.bytesPer)
}

func (r *Reader) Close() error { return r.f.Close() }

func Open(path string, fs float64) (*Reader, error) {
	format, err := DetectFormat(path)
	if err != nil {
		return nil, err
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	r := &Reader{f: f, format: format, sampleRate: fs}
	switch format {
	case FormatWAV:
		if err := r.readWavHeader(fs); err != nil {
			f.Close()
			return nil, err
		}
	case FormatCU8:
		r.bytesPer = 2
		r.remaining = fileSize(f)
	case FormatCF32:
		r.bytesPer = 8
		r.remaining = fileSize(f)
	}
	if format != FormatWAV && fs <= 0 {
		f.Close()
		return nil, fmt.Errorf("%s は生 I/Q なので fs（録音サンプルレート [Hz]）が必須です", format)
	}
	return r, nil
}

func fileSize(f *os.File) int64 {
	st, err := f.Stat()
	if err != nil {
		return 0
	}
	pos, _ := f.Seek(0, io.SeekCurrent)
	return st.Size() - pos
}

func (r *Reader) readWavHeader(wantFS float64) error {
	var hdr [12]byte
	if _, err := io.ReadFull(r.f, hdr[:]); err != nil {
		return err
	}
	riff := string(hdr[0:4])
	if (riff != "RIFF" && riff != "RF64") || string(hdr[8:12]) != "WAVE" {
		return fmt.Errorf("WAV ではありません（%q）", riff)
	}

	var ds64DataSize int64 = -1
	channels, bitsPer := 0, 0
	var fmtTag uint16
	for {
		var ch [8]byte
		if _, err := io.ReadFull(r.f, ch[:]); err != nil {
			return fmt.Errorf("data チャンクが見つかりません: %w", err)
		}
		id := string(ch[0:4])
		size := int64(binary.LittleEndian.Uint32(ch[4:8]))

		switch id {
		case "ds64":
			body := make([]byte, size)
			if _, err := io.ReadFull(r.f, body); err != nil {
				return err
			}
			if len(body) >= 16 {
				ds64DataSize = int64(binary.LittleEndian.Uint64(body[8:16]))
			}
		case "fmt ":
			body := make([]byte, size)
			if _, err := io.ReadFull(r.f, body); err != nil {
				return err
			}
			if len(body) < 16 {
				return fmt.Errorf("fmt チャンクが短すぎます")
			}
			fmtTag = binary.LittleEndian.Uint16(body[0:2])
			channels = int(binary.LittleEndian.Uint16(body[2:4]))
			r.sampleRate = float64(binary.LittleEndian.Uint32(body[4:8]))
			bitsPer = int(binary.LittleEndian.Uint16(body[14:16]))
			if fmtTag == 0xFFFE && len(body) >= 26 {
				fmtTag = binary.LittleEndian.Uint16(body[24:26])
			}
		case "data":
			if size == int64(^uint32(0)) && ds64DataSize >= 0 {
				size = ds64DataSize
			}
			if channels != 2 {
				return fmt.Errorf("WAV は I=L, Q=R のステレオが必要です（%d ch）", channels)
			}
			switch {
			case fmtTag == 1 && bitsPer == 16:
				r.bits, r.isFloat, r.bytesPer = 16, false, 4
			case fmtTag == 3 && bitsPer == 32:
				r.bits, r.isFloat, r.bytesPer = 32, true, 8
			default:
				return fmt.Errorf("未対応の WAV 形式（fmt=%d, %dbit）", fmtTag, bitsPer)
			}
			if wantFS > 0 && math.Abs(r.sampleRate-wantFS) > 1e-6 {
				return fmt.Errorf("WAV のヘッダレート %g Hz と指定 fs %g Hz が不一致です",
					r.sampleRate, wantFS)
			}
			r.remaining = size
			return nil
		default:
			if _, err := r.f.Seek(size+size%2, io.SeekCurrent); err != nil {
				return err
			}
			continue
		}
		if size%2 == 1 {
			if _, err := r.f.Seek(1, io.SeekCurrent); err != nil {
				return err
			}
		}
	}
}

func (r *Reader) Skip(n int64) error {
	b := n * int64(r.bytesPer)
	if b > r.remaining {
		b = r.remaining
	}
	if _, err := r.f.Seek(b, io.SeekCurrent); err != nil {
		return err
	}
	r.remaining -= b
	return nil
}

func (r *Reader) Read(n int) ([]complex64, error) {
	if r.remaining <= 0 {
		return nil, io.EOF
	}
	want := int64(n) * int64(r.bytesPer)
	if want > r.remaining {
		want = r.remaining - r.remaining%int64(r.bytesPer)
	}
	if want <= 0 {
		return nil, io.EOF
	}
	if int64(cap(r.buf)) < want {
		r.buf = make([]byte, want)
	}
	b := r.buf[:want]
	got, err := io.ReadFull(r.f, b)
	if err != nil && got == 0 {
		return nil, err
	}
	got -= got % r.bytesPer
	b = b[:got]
	r.remaining -= int64(got)

	out := make([]complex64, got/r.bytesPer)
	switch {
	case r.format == FormatCU8:
		for i := range out {
			re := (float32(b[2*i]) - 127.5) / 127.5
			im := (float32(b[2*i+1]) - 127.5) / 127.5
			out[i] = complex(re, im)
		}
	case r.format == FormatCF32 || r.isFloat:
		for i := range out {
			re := math.Float32frombits(binary.LittleEndian.Uint32(b[8*i:]))
			im := math.Float32frombits(binary.LittleEndian.Uint32(b[8*i+4:]))
			out[i] = complex(re, im)
		}
	default:
		for i := range out {
			re := float32(int16(binary.LittleEndian.Uint16(b[4*i:]))) / 32768.0
			im := float32(int16(binary.LittleEndian.Uint16(b[4*i+2:]))) / 32768.0
			out[i] = complex(re, im)
		}
	}
	return out, nil
}
