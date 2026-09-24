package wav

import (
	"bytes"
	"math"
	"testing"
)

func TestRoundTripMono16(t *testing.T) {
	rate := 48000
	src := make([]float32, rate) // 1 second
	for i := range src {
		src[i] = float32(math.Sin(2 * math.Pi * 440 * float64(i) / float64(rate)))
	}

	var buf bytes.Buffer
	if err := Encode(&buf, rate, src); err != nil {
		t.Fatalf("encode: %v", err)
	}
	f, err := Decode(&buf)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if f.Format.SampleRate != uint32(rate) || f.Format.Channels != 1 || f.Format.BitsPerSample != 16 {
		t.Fatalf("unexpected format: %+v", f.Format)
	}
	if len(f.Samples) != len(src) {
		t.Fatalf("sample count: got %d want %d", len(f.Samples), len(src))
	}
	var maxErr float64
	for i := range src {
		if e := math.Abs(float64(f.Samples[i] - src[i])); e > maxErr {
			maxErr = e
		}
	}
	if maxErr > 2.0/32768.0+1e-6 {
		t.Fatalf("round trip error too large: %v", maxErr)
	}
}

func TestStereo8And32(t *testing.T) {
	for _, bits := range []int{8, 24, 32} {
		raw := encodePCM(bits, [][]float32{{0.5, -0.5}, {-1, 1}, {0, 0.25}})
		w := buildWAV(2, 16000, uint16(bits), raw)
		f, err := Decode(bytes.NewReader(w))
		if err != nil {
			t.Fatalf("bits=%d decode: %v", bits, err)
		}
		if f.Format.Channels != 2 {
			t.Fatalf("bits=%d channels", bits)
		}
		mono := Mono(f.Samples, 2)
		if len(mono) != 3 {
			t.Fatalf("bits=%d mono len %d", bits, len(mono))
		}
		tol := 1.0 / 128.0
		if bits >= 16 {
			tol = 1.0 / 32768.0
		}
		if math.Abs(float64(mono[0])) > tol {
			t.Fatalf("bits=%d expected midpoint ~0, got %v", bits, mono[0])
		}
	}
}

func TestResampleIdentityAndRatio(t *testing.T) {
	in := make([]float32, 4800)
	for i := range in {
		in[i] = float32(i)
	}
	if got := Resample(in, 48000, 48000); len(got) != len(in) {
		t.Fatalf("identity resample changed length: %d", len(got))
	}
	half := Resample(in, 48000, 24000)
	if len(half) != 2400 {
		t.Fatalf("2:1 resample length: got %d want 2400", len(half))
	}
	up := Resample(half, 24000, 48000)
	if len(up) != 4800 {
		t.Fatalf("1:2 resample length: got %d want 4800", len(up))
	}
}

func TestDecodeErrors(t *testing.T) {
	if _, err := Decode(bytes.NewReader([]byte("not a wav"))); err == nil {
		t.Fatal("expected error for garbage input")
	}
}
