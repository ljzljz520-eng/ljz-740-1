package main

import (
	"encoding/binary"
	"math"
	"math/rand"
	"os"
	"testing"
)

// makeNoisyS16 writes a 16-bit mono WAV: noise for the first 0.3s, then a loud
// 300 Hz tone plus noise.
func makeNoisyS16(t *testing.T, path string, rate, n int) {
	t.Helper()
	buf := make([]byte, 44+n*2)
	copy(buf[0:4], "RIFF")
	binary.LittleEndian.PutUint32(buf[4:8], uint32(36+n*2))
	copy(buf[8:12], "WAVE")
	copy(buf[12:16], "fmt ")
	binary.LittleEndian.PutUint32(buf[16:20], 16)
	binary.LittleEndian.PutUint16(buf[20:22], 1) // PCM mono
	binary.LittleEndian.PutUint16(buf[22:24], 1)
	binary.LittleEndian.PutUint32(buf[24:28], uint32(rate))
	binary.LittleEndian.PutUint32(buf[28:32], uint32(rate*2))
	binary.LittleEndian.PutUint16(buf[32:34], 2)
	binary.LittleEndian.PutUint16(buf[34:36], 16)
	copy(buf[36:40], "data")
	binary.LittleEndian.PutUint32(buf[40:44], uint32(n*2))

	rng := rand.New(rand.NewSource(1))
	for i := 0; i < n; i++ {
		env := (float64(i) - 0.3*float64(rate)) / (0.05 * float64(rate))
		if env < 0 {
			env = 0
		}
		if env > 1 {
			env = 1
		}
		x := 0.5*env*math.Sin(2*math.Pi*300*float64(i)/float64(rate)) +
			(rng.Float64()*2-1)*0.1
		v := int16(math.Round(x * 30000))
		binary.LittleEndian.PutUint16(buf[44+i*2:], uint16(v))
	}
	if err := os.WriteFile(path, buf, 0o644); err != nil {
		t.Fatal(err)
	}
}

// rmsWAV computes the RMS of a time region (seconds) of a mono 16-bit WAV.
func rmsWAV(raw []byte, start, end float64) float64 {
	if len(raw) < 44 {
		return math.NaN()
	}
	rate := int(binary.LittleEndian.Uint32(raw[24:]))
	ch := int(binary.LittleEndian.Uint16(raw[22:]))
	bits := int(binary.LittleEndian.Uint16(raw[34:]))
	if bits != 16 {
		return math.NaN()
	}
	s0 := int(start*float64(rate)) * ch
	s1 := int(end*float64(rate)) * ch
	var e float64
	for i := s0; i < s1; i++ {
		off := 44 + i*2
		if off+2 > len(raw) {
			break
		}
		x := float64(int16(binary.LittleEndian.Uint16(raw[off:]))) / 32768
		e += x * x
	}
	n := s1 - s0
	if n <= 0 {
		return math.NaN()
	}
	return math.Sqrt(e / float64(n))
}
