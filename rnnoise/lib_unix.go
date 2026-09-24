//go:build darwin || linux || freebsd || netbsd

package rnnoise

import "github.com/ebitengine/purego"

// rtldNow binds every undefined symbol immediately (RTLD_NOW), which gives
// the clearest error if the library's own dependencies are missing.
const rtldNow = 2

type libHandle uintptr

func openLib(path string) (libHandle, error) {
	h, err := purego.Dlopen(path, rtldNow)
	if err != nil {
		return 0, err
	}
	return libHandle(h), nil
}

func closeLib(h libHandle) error {
	return purego.Dlclose(uintptr(h))
}

func lookup(h libHandle, name string) error {
	_, err := purego.Dlsym(uintptr(h), name)
	return err
}
