//go:build linux || darwin

package vdnoise

import "github.com/ebitengine/purego"

func dlopen(path string) (uintptr, error) {
	return purego.Dlopen(path, purego.RTLD_NOW|purego.RTLD_GLOBAL)
}

func dlsym(handle uintptr, name string) (uintptr, error) {
	return purego.Dlsym(handle, name)
}

func dlclose(handle uintptr) error {
	return purego.Dlclose(handle)
}
