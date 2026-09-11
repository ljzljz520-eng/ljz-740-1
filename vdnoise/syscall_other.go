//go:build !linux && !darwin && !windows

package vdnoise

func syscallN(fn uintptr, args ...uintptr) uintptr { return 0 }

func registerFunc(fptr any, addr uintptr) {}
