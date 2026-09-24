//go:build windows

package noisego

import "syscall"

import "github.com/ebitengine/purego"

func openDynamicLib(path string) (uintptr, error) {
	h, err := syscall.LoadLibrary(path)
	return uintptr(h), err
}

func closeDynamicLib(handle uintptr) error {
	return syscall.FreeLibrary(syscall.Handle(handle))
}

func lookupSymbol(handle uintptr, name string) (uintptr, error) {
	return syscall.GetProcAddress(syscall.Handle(handle), name)
}

func bindFunc(fptr any, addr uintptr) {
	purego.RegisterFunc(fptr, addr)
}
