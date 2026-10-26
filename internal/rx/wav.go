package rx

import (
	"encoding/binary"
	"os"

	"github.com/symysak/arib/internal/wavio"
)

type WavWriter struct {
	f    *os.File
	fs   int
	n    int64
	buf  []byte
	path string
	hdrN int64
}

const WavHeaderEverySec = 5

func PathOrDiscard(path string, discard bool) string {
	if discard {
		return ""
	}
	return path
}

func NewWavWriter(path string, fs int) (*WavWriter, error) {
	if path == "" {
		return &WavWriter{fs: fs}, nil
	}
	f, err := os.Create(path)
	if err != nil {
		return nil, err
	}
	if _, err := f.Write(make([]byte, wavio.HeaderSize)); err != nil {
		f.Close()
		return nil, err
	}
	return &WavWriter{f: f, fs: fs, path: path}, nil
}

func (w *WavWriter) Write(x []complex64, peak float32) {
	scale := float32(30000) / peak
	w.buf = w.buf[:0]
	for _, v := range x {
		w.buf = binary.LittleEndian.AppendUint16(w.buf, uint16(int16(clampF(real(v)*scale))))
		w.buf = binary.LittleEndian.AppendUint16(w.buf, uint16(int16(clampF(imag(v)*scale))))
	}
	if w.f != nil {
		w.f.Write(w.buf)
	}
	w.n += int64(len(x))
	if w.f != nil && w.n-w.hdrN >= int64(w.fs)*WavHeaderEverySec {
		w.f.WriteAt(wavio.Header(int(w.n*4), 2, w.fs), 0)
		w.hdrN = w.n
	}
}

func clampF(v float32) float32 {
	if v > 32767 {
		return 32767
	}
	if v < -32768 {
		return -32768
	}
	return v
}

func (w *WavWriter) Close() float64 {
	if w.f == nil {
		return float64(w.n) / float64(w.fs)
	}
	w.f.WriteAt(wavio.Header(int(w.n*4), 2, w.fs), 0)
	w.f.Close()
	return float64(w.n) / float64(w.fs)
}
