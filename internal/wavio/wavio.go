package wavio

import (
	"encoding/binary"
	"os"
	"path/filepath"
)

const HeaderSize = 44

func Header(dataBytes, channels, sampleRate int) []byte {
	blockAlign := channels * 2
	byteRate := sampleRate * blockAlign

	h := make([]byte, HeaderSize)
	copy(h[0:], "RIFF")
	binary.LittleEndian.PutUint32(h[4:], uint32(36+dataBytes))
	copy(h[8:], "WAVEfmt ")
	binary.LittleEndian.PutUint32(h[16:], 16)
	binary.LittleEndian.PutUint16(h[20:], 1)
	binary.LittleEndian.PutUint16(h[22:], uint16(channels))
	binary.LittleEndian.PutUint32(h[24:], uint32(sampleRate))
	binary.LittleEndian.PutUint32(h[28:], uint32(byteRate))
	binary.LittleEndian.PutUint16(h[32:], uint16(blockAlign))
	binary.LittleEndian.PutUint16(h[34:], 16)
	copy(h[36:], "data")
	binary.LittleEndian.PutUint32(h[40:], uint32(dataBytes))
	return h
}

func Bytes(data []int16, channels, sampleRate int) []byte {
	dataBytes := len(data) * 2
	b := make([]byte, 0, HeaderSize+dataBytes)
	b = append(b, Header(dataBytes, channels, sampleRate)...)
	for _, v := range data {
		b = binary.LittleEndian.AppendUint16(b, uint16(v))
	}
	return b
}

func Write(path string, data []int16, channels, sampleRate int) error {
	return WriteBytes(path, Bytes(data, channels, sampleRate))
}

func WriteBytes(path string, b []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
