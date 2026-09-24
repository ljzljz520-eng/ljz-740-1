package rnnoise

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestLoadFailureIsExplanatory(t *testing.T) {
	bad := filepath.Join(t.TempDir(), "definitely-missing-lib.so")
	_, err := Open(WithLibraryPath(bad))
	if err == nil {
		t.Fatal("expected load error")
	}
	var le *LoadError
	if !errors.As(err, &le) {
		t.Fatalf("expected *LoadError, got %T: %v", err, err)
	}
	msg := err.Error()
	for _, want := range []string{"failed to load library", bad, "file does not exist", "hint:"} {
		if !contains(msg, want) {
			t.Fatalf("error message missing %q:\n%s", want, msg)
		}
	}
	t.Logf("\n%s", msg)
}

func TestLoadGarbageFile(t *testing.T) {
	p := filepath.Join(t.TempDir(), "librnnoise.so")
	if err := os.WriteFile(p, []byte("this is not an ELF/Mach-O/PE file"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := Open(WithLibraryPath(p))
	if err == nil {
		t.Fatal("expected load error for garbage file")
	}
	if !contains(err.Error(), p) {
		t.Fatalf("error should mention path: %v", err)
	}
}

func contains(s, sub string) bool {
	return len(sub) == 0 || (len(s) >= len(sub) && indexOf(s, sub) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
