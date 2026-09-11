package vdnoise

import (
	"errors"
	"fmt"
	"strings"
)

// errWrapf wraps a sentinel with a formatted message while preserving errors.Is.
func errWrapf(err error, format string, args ...any) error {
	return fmt.Errorf("%w: %s", err, fmt.Sprintf(format, args...))
}

// Status codes returned by the native library (see native/vdnoise.h).
const (
	statusOK       = 0
	statusInvalid  = 1
	statusNoMem    = 2
	statusFormat   = 3
	statusInternal = 4
)

// Sentinel errors returned by the Go wrapper.
var (
	// ErrInvalidArgument is returned when Go-side parameters are invalid.
	ErrInvalidArgument = errors.New("vdnoise: invalid argument")
	// ErrNoMemory mirrors VD_ERR_NOMEM.
	ErrNoMemory = errors.New("vdnoise: native library ran out of memory")
	// ErrFormat mirrors VD_ERR_FORMAT: frame size or format mismatch.
	ErrFormat = errors.New("vdnoise: format mismatch in native library")
	// ErrInternal mirrors VD_ERR_INTERNAL.
	ErrInternal = errors.New("vdnoise: internal error in native library")
	// ErrABIMismatch means the loaded library speaks an incompatible ABI.
	ErrABIMismatch = errors.New("vdnoise: incompatible native library ABI version")
	// ErrUnsupportedPlatform means dlopen is unavailable on this GOOS.
	ErrUnsupportedPlatform = errors.New("vdnoise: dynamic library loading is not supported on this platform")
	// ErrClosed is returned when a closed Library/Context is used.
	ErrClosed = errors.New("vdnoise: resource already closed")
)

func statusError(code int) error {
	switch code {
	case statusOK:
		return nil
	case statusInvalid:
		return ErrInvalidArgument
	case statusNoMem:
		return ErrNoMemory
	case statusFormat:
		return ErrFormat
	case statusInternal:
		return ErrInternal
	default:
		return fmt.Errorf("vdnoise: native library returned unknown status %d", code)
	}
}

// LoadAttempt records one library lookup that did not succeed.
type LoadAttempt struct {
	Path string
	Err  error
}

// LoadError explains why every candidate library path failed to load.
//
// It always carries the full list of attempted locations with the OS level
// reason, so users can tell "file missing" apart from "wrong architecture",
// "missing symbol", "permission denied", ...
type LoadError struct {
	Attempts []LoadAttempt
	// Symbol is set when loading succeeded but a required symbol was absent.
	Symbol string
	// ABIVersion is set when loading succeeded but the ABI version mismatched.
	ABIVersion int
}

func (e *LoadError) Error() string {
	var b strings.Builder
	if e.Symbol != "" {
		b.WriteString("vdnoise: library loaded but required symbol not exported: ")
		b.WriteString(e.Symbol)
		return b.String()
	}
	if e.ABIVersion != 0 {
		fmt.Fprintf(&b, "vdnoise: incompatible ABI: library reports %d.%d, binding requires %d.x",
			e.ABIVersion>>16, e.ABIVersion&0xffff, abiVersionMajor)
		return b.String()
	}
	b.WriteString("vdnoise: failed to load the native denoiser library, attempted locations:\n")
	for _, a := range e.Attempts {
		fmt.Fprintf(&b, "  - %s\n      %v\n", a.Path, a.Err)
	}
	b.WriteString("fix: pass -lib / use WithPath, set VDNOISE_LIB, or run \"make -C native\"")
	return b.String()
}

// Is reports ErrUnsupportedPlatform when every attempt failed for that
// reason (package built on an OS without a dynamic loading backend).
func (e *LoadError) Is(target error) bool {
	if target != ErrUnsupportedPlatform || len(e.Attempts) == 0 {
		return false
	}
	for _, a := range e.Attempts {
		if !errors.Is(a.Err, ErrUnsupportedPlatform) {
			return false
		}
	}
	return true
}
