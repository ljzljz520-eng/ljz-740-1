package vdnoise

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func testLib(t *testing.T) *Library {
	t.Helper()
	opts := []Option{}
	if p := os.Getenv("VDNOISE_LIB"); p != "" {
		opts = append(opts, WithPath(p))
	}
	lib, err := Open(opts...)
	if err != nil {
		t.Skipf("native library unavailable, skipping integration test: %v", err)
	}
	t.Cleanup(func() { lib.Close() })
	return lib
}

func TestLoadFailureExplainsReasons(t *testing.T) {
	t.Setenv("VDNOISE_LIB", "") // isolate from the integration-test env var
	dir := t.TempDir()
	_, err := Open(WithPath(filepath.Join(dir, DefaultLibName())))
	if err == nil {
		t.Fatal("expected error")
	}
	var le *LoadError
	if !errors.As(err, &le) {
		t.Fatalf("want *LoadError, got %T: %v", err, le)
	}
	if len(le.Attempts) == 0 {
		t.Fatal("expected at least one attempted location")
	}
	if le.Attempts[0].Err == nil {
		t.Fatal("expected an OS-level reason")
	}
	t.Logf("\n%v", err)
}

func TestMissingSymbol(t *testing.T) {
	// A valid shared object that exports nothing the loader needs.
	path := buildStubLib(t, "int vd_abi_version(void){return 0;}")
	_, err := Open(WithPath(path))
	var le *LoadError
	if !errors.As(err, &le) || le.Symbol == "" {
		t.Fatalf("expected LoadError with missing symbol, got %v", err)
	}
}

func TestABIMismatch(t *testing.T) {
	path := buildStubLib(t, badABISrc)
	_, err := Open(WithPath(path))
	var le *LoadError
	if !errors.As(err, &le) || le.ABIVersion>>16 != 99 {
		t.Fatalf("expected ABI mismatch error, got %v", err)
	}
}

func TestVersionAndABI(t *testing.T) {
	lib := testLib(t)
	maj, min := lib.ABI()
	if maj != abiVersionMajor {
		t.Fatalf("major ABI = %d, want %d", maj, abiVersionMajor)
	}
	if maj < 1 || min < 0 {
		t.Fatalf("bad ABI %d.%d", maj, min)
	}
	v, err := lib.Version()
	if err != nil || v == "" {
		t.Fatalf("Version: %q, %v", v, err)
	}
}

func TestInvalidContextConfig(t *testing.T) {
	lib := testLib(t)
	cases := []ContextConfig{
		{SampleRate: 1000, Channels: 1, FrameSize: 480, Format: FormatS16},
		{SampleRate: 48000, Channels: 5, FrameSize: 480, Format: FormatS16},
		{SampleRate: 48000, Channels: 1, FrameSize: 0, Format: FormatS16},
		{SampleRate: 48000, Channels: 1, FrameSize: 480, Format: 42},
	}
	for i, cfg := range cases {
		if _, err := lib.NewContext(cfg); !errors.Is(err, ErrInvalidArgument) {
			t.Fatalf("case %d: want ErrInvalidArgument, got %v", i, err)
		}
	}
}

func TestProcessS16ProgressAndClose(t *testing.T) {
	lib := testLib(t)
	var pcts []int
	ctx, err := lib.NewContext(ContextConfig{
		SampleRate: 48000, Channels: 1, FrameSize: 480, Format: FormatS16,
		TotalFrames: 5, Progress: func(p int) { pcts = append(pcts, p) },
	})
	if err != nil {
		t.Fatal(err)
	}

	in := make([]int16, 480)
	out := make([]int16, 480)
	for i := range in {
		in[i] = int16(((i * 37) % 2000) - 1000)
	}
	for i := 0; i < 5; i++ {
		n, err := ctx.Process(in, out)
		if err != nil || n != 480 {
			t.Fatalf("frame %d: n=%d err=%v", i, n, err)
		}
	}
	if err := ctx.Finish(); err != nil {
		t.Fatal(err)
	}
	if len(pcts) == 0 || pcts[len(pcts)-1] != 100 {
		t.Fatalf("progress never reached 100%%: %v", pcts)
	}
	for _, p := range pcts {
		if p < 0 || p > 100 {
			t.Fatalf("progress out of range: %d", p)
		}
	}
	if err := ctx.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := ctx.Process(in, out); !errors.Is(err, ErrClosed) {
		t.Fatalf("use after Close: want ErrClosed, got %v", err)
	}
	// double close is fine
	if err := ctx.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestProcessF32AndWrongType(t *testing.T) {
	lib := testLib(t)
	ctx, err := lib.NewContext(ContextConfig{
		SampleRate: 48000, Channels: 2, FrameSize: 480, Format: FormatF32,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer ctx.Close()

	in := make([]float32, 960)
	out := make([]float32, 960)
	in[0] = 0.5
	if _, err := ctx.Process(in, out); err != nil {
		t.Fatal(err)
	}
	// passing []int16 to an f32 context must fail with ErrInvalidArgument.
	if _, err := ctx.Process(make([]int16, 960), make([]int16, 960)); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("want ErrInvalidArgument, got %v", err)
	}
	// short buffers must fail too.
	if _, err := ctx.Process(make([]float32, 10), out); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("want ErrInvalidArgument, got %v", err)
	}
}

func TestLibraryDoubleClose(t *testing.T) {
	lib := testLib(t)
	if err := lib.Close(); err != nil {
		t.Fatal(err)
	}
	if err := lib.Close(); err != nil {
		t.Fatalf("double Close: %v", err)
	}
}
