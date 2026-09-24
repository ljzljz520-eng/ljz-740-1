package wav

import (
	"bytes"
	"encoding/binary"
	"math"
	"os"
)

// WriteFile writes mono 16-bit PCM WAV at the given sample rate.
func WriteFile(path string, sampleRate int, mono []float32) error {
	var buf bytes.Buffer
	if err := Encode(&buf, sampleRate, mono); err != nil {
		return err
	}
	return os.WriteFile(path, buf.Bytes(), 0o644)
}

// Encode writes a 16-bit PCM mono WAV stream.
func Encode(buf *bytes.Buffer, sampleRate int, mono []float32) error {
	const (
		channels   = 1
		bitsSample = 16
	)
	byteRate := sampleRate * channels * bitsSample / 8
	blockAlign := channels * bitsSample / 8
	dataLen := len(mono) * 2

	write := func(v any) error { return binary.Write(buf, binary.LittleEndian, v) }

	if _, err := buf.WriteString("RIFF"); err != nil {
		return err
	}
	if err := write(uint32(36 + dataLen)); err != nil {
		return err
	}
	if _, err := buf.WriteString("WAVEfmt "); err != nil {
		return err
	}
	if err := write(uint32(16)); err != nil { // fmt chunk size
		return err
	}
	if err := write(uint16(fmtPCM)); err != nil {
		return err
	}
	if err := write(uint16(channels)); err != nil {
		return err
	}
	if err := write(uint32(sampleRate)); err != nil {
		return err
	}
	if err := write(uint32(byteRate)); err != nil {
		return err
	}
	if err := write(uint16(blockAlign)); err != nil {
		return err
	}
	if err := write(uint16(bitsSample)); err != nil {
		return err
	}
	if _, err := buf.WriteString("data"); err != nil {
		return err
	}
	if err := write(uint32(dataLen)); err != nil {
		return err
	}

	pcm := make([]byte, 2)
	for _, s := range mono {
		if s > 1 {
			s = 1
		} else if s < -1 {
			s = -1
		}
		v := int16(math.Round(float64(s) * 32767))
		binary.LittleEndian.PutUint16(pcm, uint16(v))
		_, _ = buf.Write(pcm)
	}
	return nil
}
