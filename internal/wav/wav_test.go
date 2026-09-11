package wav

import (
	"bytes"
	"math"
	"os"
	"path/filepath"
	"testing"
)

func tempWAV(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "a.wav")
}

func TestRoundTripS16(t *testing.T) {
	path := tempWAV(t)
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	w, err := NewWriter(f, 48000, 2, FormatPCM)
	if err != nil {
		t.Fatal(err)
	}
	src := make([]int16, 480*2*3)
	for i := range src {
		src[i] = int16((i*137)%30000 - 15000)
	}
	if err := w.WriteS16(src); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	f.Close()

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	dec, err := Decode(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	if dec.SampleRate != 48000 || dec.Channels != 2 || dec.Bits != 16 || dec.Format != FormatPCM {
		t.Fatalf("header mismatch: %+v", dec)
	}
	if len(dec.S16) != len(src) {
		t.Fatalf("len %d want %d", len(dec.S16), len(src))
	}
	for i := range src {
		if dec.S16[i] != src[i] {
			t.Fatalf("sample %d: %d != %d", i, dec.S16[i], src[i])
		}
	}
}

func TestRoundTripF32(t *testing.T) {
	path := tempWAV(t)
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	w, err := NewWriter(f, 16000, 1, FormatIEEEFloat)
	if err != nil {
		t.Fatal(err)
	}
	src := []float32{0, 0.25, -0.5, 1, -1, float32(math.Pi)}
	if err := w.WriteF32(src); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	f.Close()

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	dec, err := Decode(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	if dec.Format != FormatIEEEFloat || dec.Bits != 32 || dec.SampleRate != 16000 || dec.Channels != 1 {
		t.Fatalf("header mismatch: %+v", dec)
	}
	for i := range src {
		if dec.F32[i] != src[i] {
			t.Fatalf("sample %d: %v != %v", i, dec.F32[i], src[i])
		}
	}
}

func TestDecodeRejectsUnsupported(t *testing.T) {
	if _, err := Decode(bytes.NewReader(nil)); err == nil {
		t.Fatal("empty input should fail")
	}
	if _, err := Decode(bytes.NewReader([]byte("RIFFxxxxWAVE"))); err == nil {
		t.Fatal("headerless input should fail")
	}
}
