package rnnoise

import (
	"errors"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// findTestLibrary locates a built library in third_party/, ./ or $RNNOISE_LIB.
func findTestLibrary() (string, bool) {
	if p := os.Getenv("RNNOISE_LIB"); p != "" {
		if _, err := os.Stat(p); err == nil {
			return p, true
		}
	}
	names := libNames()
	dirs := []string{filepath.Join("..", "third_party"), "third_party", "."}
	for _, d := range dirs {
		for _, n := range names {
			p := filepath.Join(d, n)
			if _, err := os.Stat(p); err == nil {
				p, _ = filepath.Abs(p)
				return p, true
			}
		}
	}
	return "", false
}

func TestEndToEndDenoise(t *testing.T) {
	libPath, ok := findTestLibrary()
	if !ok {
		t.Skip("RNNoise shared library not found; run `make lib` first")
	}

	lib, err := Open(WithLibraryPath(libPath))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if got := lib.FrameSize(); got != FrameSize {
		t.Fatalf("frame size: got %d want %d", got, FrameSize)
	}

	d, err := lib.NewContext()
	if err != nil {
		t.Fatalf("new context: %v", err)
	}

	// 1 second: a 440 Hz tone ("voice-ish") plus loud white noise.
	n := SampleRate
	in := make([]float32, n)
	// deterministic pseudo-random noise so tests are reproducible
	seed := uint32(0x12345678)
	for i := 0; i < n; i++ {
		seed = seed*1664525 + 1013904223
		noise := (float32(seed)/float32(1<<31) - 0.5) * 0.25
		tone := float32(math.Sin(2*math.Pi*440*float64(i)/float64(SampleRate))) * 0.2
		in[i] = tone + noise
	}

	var calls int
	var lastVAD float32
	out, err := d.Process(in, func(done, total int, vad float32) bool {
		if total != n || done <= 0 || done > total {
			t.Errorf("bad progress done=%d total=%d", done, total)
		}
		calls++
		lastVAD = vad
		return false
	})
	if err != nil {
		t.Fatalf("process: %v", err)
	}
	if len(out) != n {
		t.Fatalf("output length: got %d want %d", len(out), n)
	}
	wantCalls := (n + FrameSize - 1) / FrameSize
	if calls != wantCalls {
		t.Fatalf("progress calls: got %d want %d", calls, wantCalls)
	}
	t.Logf("final VAD probability: %.3f", lastVAD)

	// Noise floor energy should drop after denoising.
	var beforeNoise, afterNoise float64
	for i := 0; i < FrameSize*5; i++ {
		// compare the noise-dominated segments loosely via RMS
		beforeNoise += float64(in[i]) * float64(in[i])
		afterNoise += float64(out[i]) * float64(out[i])
	}
	if afterNoise > beforeNoise {
		t.Fatalf("denoising did not reduce energy: before=%.4f after=%.4f", beforeNoise, afterNoise)
	}

	if err := d.Close(); err != nil {
		t.Fatalf("close denoiser: %v", err)
	}
	if err := lib.Close(); err != nil {
		t.Fatalf("close lib: %v", err)
	}

	// Reusing a closed resource reports ErrClosed.
	if _, err := d.Process(in, nil); !errors.Is(err, ErrClosed) {
		t.Fatalf("closed process err = %v, want ErrClosed", err)
	}
	if err := lib.Close(); !errors.Is(err, ErrClosed) {
		t.Fatalf("double close err = %v, want ErrClosed", err)
	}
}

func TestFrameSizeValidation(t *testing.T) {
	libPath, ok := findTestLibrary()
	if !ok {
		t.Skip("RNNoise shared library not found")
	}
	d, err := New(WithLibraryPath(libPath))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if _, _, err := d.ProcessFrame(make([]float32, 10)); err == nil {
		t.Fatal("expected frame size mismatch")
	} else {
		var fsErr FrameSizeMismatchError
		if !errors.As(err, &fsErr) {
			t.Fatalf("want FrameSizeMismatchError, got %T", err)
		}
	}
	out, vad, err := d.ProcessFrame(make([]float32, FrameSize))
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != FrameSize {
		t.Fatalf("out len %d", len(out))
	}
	if vad < 0 || vad > 1 {
		t.Fatalf("vad out of range: %v", vad)
	}
}

func TestProgressAbort(t *testing.T) {
	libPath, ok := findTestLibrary()
	if !ok {
		t.Skip("RNNoise shared library not found")
	}
	d, err := New(WithLibraryPath(libPath))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()

	in := make([]float32, FrameSize*4)
	stop := false
	_, err = d.Process(in, func(done, total int, vad float32) bool {
		if done >= FrameSize*2 {
			stop = true
			return true
		}
		return false
	})
	if !errors.Is(err, ErrAborted) || !stop {
		t.Fatalf("err=%v stop=%v, want ErrAborted", err, stop)
	}
}

func TestCandidatesIncludePlatformName(t *testing.T) {
	c := candidates("")
	var found bool
	for _, name := range libNames() {
		for _, c2 := range c {
			if c2 == name {
				found = true
			}
		}
	}
	if !found {
		t.Fatalf("platform name %v missing from candidates %v", libNames(), c)
	}
	if runtime.GOOS == "windows" {
		// nothing else platform-specific to assert elsewhere
	}
}

func TestArbitraryLengthsAndLibraryRefuseClose(t *testing.T) {
	libPath, ok := findTestLibrary()
	if !ok {
		t.Skip("RNNoise shared library not found")
	}
	lib, err := Open(WithLibraryPath(libPath))
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range []int{1, 479, 480, 481, 1000, 144001} {
		d, err := lib.NewContext()
		if err != nil {
			t.Fatal(err)
		}
		in := make([]float32, n)
		out, err := d.Process(in, nil)
		if err != nil {
			t.Fatalf("n=%d process: %v", n, err)
		}
		if len(out) != n {
			t.Fatalf("n=%d: got %d output samples", n, len(out))
		}
		// Library must refuse to unload while this context is alive.
		if err := lib.Close(); err == nil {
			t.Fatalf("n=%d: library closed with a live context", n)
		}
		if err := d.Close(); err != nil {
			t.Fatalf("n=%d close: %v", n, err)
		}
	}
	if err := lib.Close(); err != nil {
		t.Fatalf("final library close: %v", err)
	}
}

func TestNewOwnsLibrary(t *testing.T) {
	libPath, ok := findTestLibrary()
	if !ok {
		t.Skip("RNNoise shared library not found")
	}
	d, err := New(WithLibraryPath(libPath))
	if err != nil {
		t.Fatal(err)
	}
	if d.LibraryPath() == "" {
		t.Fatal("missing library path")
	}
	if err := d.Close(); err != nil {
		t.Fatal(err)
	}
	// After Close the library is unloaded too; processing must fail.
	if _, err := d.Process(make([]float32, FrameSize), nil); !errors.Is(err, ErrClosed) {
		t.Fatalf("got %v, want ErrClosed", err)
	}
}
