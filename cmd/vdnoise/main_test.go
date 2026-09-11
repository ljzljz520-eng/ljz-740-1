package main

import (
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// TestCLIEndToEnd builds the CLI and processes a generated WAV when a native
// library is available (VDNOISE_LIB); skips otherwise.
func TestCLIEndToEnd(t *testing.T) {
	lib := os.Getenv("VDNOISE_LIB")
	if lib == "" {
		t.Skip("VDNOISE_LIB not set; skipping CLI end-to-end test")
	}
	if _, err := os.Stat(lib); err != nil {
		t.Skipf("native library missing: %v", err)
	}
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go toolchain unavailable")
	}

	dir := t.TempDir()
	bin := filepath.Join(dir, "vdnoise")
	build := exec.Command("go", "build", "-o", bin, ".")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build CLI: %v\n%s", err, out)
	}

	in := filepath.Join(dir, "in.wav")
	out := filepath.Join(dir, "out.wav")
	makeNoisyS16(t, in, 16000, 16000)

	cmd := exec.Command(bin, "-in", in, "-out", out, "-lib", lib, "-frame", "480", "-quiet")
	if run, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("cli run: %v\n%s", err, run)
	}

	rawIn, _ := os.ReadFile(in)
	rawOut, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if len(rawIn) != len(rawOut) {
		t.Fatalf("size mismatch: %d -> %d", len(rawIn), len(rawOut))
	}
	// output must not be silent: the tone region carries energy
	if rmsWAV(rawOut, 0.5, 0.9) < 0.01 {
		t.Fatal("output tone region unexpectedly silent")
	}
	// and the leading noise-only region should be attenuated
	before := rmsWAV(rawIn, 0.0, 0.2)
	after := rmsWAV(rawOut, 0.0, 0.2)
	if !math.IsNaN(before) && before/after < 1.2 {
		t.Fatalf("noise region not attenuated: %.4f -> %.4f", before, after)
	}
}
