package rnnoise

import (
	"fmt"
	"unsafe"

	"github.com/ebitengine/purego"
)

// requiredSymbols are the C entry points without which the binding cannot
// work. rnnoise_get_frame_size is optional: older forks do not export it and
// the documented value is always 480.
var requiredSymbols = []string{
	"rnnoise_create",
	"rnnoise_destroy",
	"rnnoise_process_frame",
}

const optionalFrameSizeSymbol = "rnnoise_get_frame_size"

func ptr[T any](s []T) unsafe.Pointer {
	if len(s) == 0 {
		return nil
	}
	return unsafe.Pointer(&s[0])
}

// bind verifies every required symbol exists, then registers Go trampolines.
func (l *Library) bind() error {
	for _, name := range requiredSymbols {
		if err := lookup(l.handle, name); err != nil {
			return &LoadError{
				Library: l.path,
				Op:      "lookup symbol " + name,
				Errs: []attemptError{{
					Path: l.path,
					Err:  fmt.Errorf("library was loaded but symbol %q is missing: %w", name, err),
				}},
				hint: "the file is not a compatible RNNoise build; build it from " +
					"https://github.com/xiph/rnnoise",
			}
		}
	}

	// Required symbols were verified above; RegisterLibFunc may still panic
	// (e.g. on an internal ABI problem), so register defensively.
	if err := register(l.handle, &l.create, "rnnoise_create"); err != nil {
		return err
	}
	if err := register(l.handle, &l.destroy, "rnnoise_destroy"); err != nil {
		return err
	}
	if err := register(l.handle, &l.process, "rnnoise_process_frame"); err != nil {
		return err
	}
	// Optional on forks without rnnoise_get_frame_size.
	if err := register(l.handle, &l.getFrameSz, optionalFrameSizeSymbol); err != nil {
		l.getFrameSz = nil
	}
	return nil
}

func register[T any](handle libHandle, fptr *T, name string) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = &LoadError{
				Library: "",
				Op:      "register symbol " + name,
				Errs:    []attemptError{{Path: name, Err: fmt.Errorf("%v", r)}},
			}
		}
	}()
	purego.RegisterLibFunc(fptr, uintptr(handle), name)
	return nil
}
