//go:build darwin || linux || freebsd || netbsd

package noisego

import "github.com/ebitengine/purego"

// openDynamicLib 通过 dlopen 打开动态库。
func openDynamicLib(path string) (uintptr, error) {
	return purego.Dlopen(path, purego.RTLD_LAZY|purego.RTLD_GLOBAL)
}

// closeDynamicLib 关闭动态库句柄。
func closeDynamicLib(handle uintptr) error {
	return purego.Dlclose(handle)
}

// lookupSymbol 查找动态库中的符号地址。
func lookupSymbol(handle uintptr, name string) (uintptr, error) {
	return purego.Dlsym(handle, name)
}

// bindFunc 将 C 函数地址绑定到 fptr（函数指针变量的地址）。
func bindFunc(fptr any, addr uintptr) {
	purego.RegisterFunc(fptr, addr)
}
