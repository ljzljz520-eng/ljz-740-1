//go:build linux || darwin || windows

package vdnoise

import "github.com/ebitengine/purego"

func syscallN(fn uintptr, args ...uintptr) uintptr {
	r1, _, _ := purego.SyscallN(fn, args...)
	return r1
}

func registerFunc(fptr any, addr uintptr) { purego.RegisterFunc(fptr, addr) }
