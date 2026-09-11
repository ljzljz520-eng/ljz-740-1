//go:build windows

package vdnoise

import "syscall"

// On Windows purego does not expose Dlopen/Dlsym; the standard library
// syscall package (pure-Go, no cgo) provides the equivalent kernel32 calls.
func dlopen(path string) (uintptr, error) {
	h, err := syscall.LoadLibrary(path)
	return uintptr(h), err
}

func dlsym(handle uintptr, name string) (uintptr, error) {
	return syscall.GetProcAddress(syscall.Handle(handle), name)
}

func dlclose(handle uintptr) error {
	return syscall.FreeLibrary(syscall.Handle(handle))
}
