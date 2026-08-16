package wavio

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
)

func Bytes(data []int16, channels, sampleRate int) []byte {
	dataBytes := len(data) * 2
	byteRate := sampleRate * channels * 2
	blockAlign := channels * 2

	buf := bytes.NewBuffer(make([]byte, 0, 44+dataBytes))
	buf.WriteString("RIFF")
	binary.Write(buf, binary.LittleEndian, uint32(36+dataBytes))
	buf.WriteString("WAVEfmt ")
	binary.Write(buf, binary.LittleEndian, uint32(16))
	binary.Write(buf, binary.LittleEndian, uint16(1))
	binary.Write(buf, binary.LittleEndian, uint16(channels))
	binary.Write(buf, binary.LittleEndian, uint32(sampleRate))
	binary.Write(buf, binary.LittleEndian, uint32(byteRate))
	binary.Write(buf, binary.LittleEndian, uint16(blockAlign))
	binary.Write(buf, binary.LittleEndian, uint16(16))
	buf.WriteString("data")
	binary.Write(buf, binary.LittleEndian, uint32(dataBytes))
	for _, v := range data {
		binary.Write(buf, binary.LittleEndian, v)
	}
	return buf.Bytes()
}

func Write(path string, data []int16, channels, sampleRate int) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, Bytes(data, channels, sampleRate), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
