//go:build windows

package rnnoise

import (
	"fmt"
	"syscall"
)

type libHandle syscall.Handle

func openLib(path string) (libHandle, error) {
	h, err := syscall.LoadLibrary(path)
	if err != nil {
		return 0, err
	}
	return libHandle(h), nil
}

func closeLib(h libHandle) error {
	return syscall.FreeLibrary(syscall.Handle(h))
}

func lookup(h libHandle, name string) error {
	if _, err := syscall.GetProcAddress(syscall.Handle(h), name); err != nil {
		return fmt.Errorf("symbol %q: %w", name, err)
	}
	return nil
}
