//go:build !linux && !darwin && !windows

package vdnoise

// Backend stub for platforms purego does not support (freebsd is not part of
// the promised three, etc.). The package still compiles; Open returns a
// LoadError carrying ErrUnsupportedPlatform.

func dlopen(path string) (uintptr, error) { return 0, ErrUnsupportedPlatform }
func dlsym(uintptr, string) (uintptr, error) {
	return 0, ErrUnsupportedPlatform
}
func dlclose(uintptr) error { return nil }
