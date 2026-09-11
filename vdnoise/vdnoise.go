// Package vdnoise provides a cgo-free Go binding for a voice denoising shared
// library (libvdnoise.so / libvdnoise.dylib / vdnoise.dll), loaded at runtime
// through github.com/ebitengine/purego.
//
// The native side exposes the C ABI declared in native/vdnoise.h:
//
//   - vd_abi_version / vd_version
//   - vd_open / vd_close        create and release a denoiser context
//   - vd_process_s16 / vd_process_f32  process one interleaved PCM frame
//   - vd_set_total / vd_finish  progress bookkeeping
//
// Typical use:
//
//	lib, err := vdnoise.Open() // or vdnoise.Open(vdnoise.WithPath("/opt/libvdnoise.so"))
//	if err != nil {
//	    log.Fatal(err) // *vdnoise.LoadError explains every attempted path
//	}
//	defer lib.Close()
//
//	ctx, err := lib.NewContext(vdnoise.ContextConfig{
//	    SampleRate: 48000, Channels: 1, FrameSize: 480, Format: vdnoise.FormatS16,
//	    Progress: func(pct int) { fmt.Printf("\r%d%%", pct) },
//	})
//	if err != nil {
//	    log.Fatal(err)
//	}
//	defer ctx.Close()
//
//	if _, err := ctx.Process(in, out); err != nil { // 480 samples per frame
//	    log.Fatal(err)
//	}
//
// Everything is safe to close more than once; a Context must not be used
// concurrently from multiple goroutines (the underlying engine is stateful).
package vdnoise

import (
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"unsafe"
)

// Sample format codes, identical to VD_FMT_* in the native header.
const (
	// FormatS16 is interleaved signed 16-bit little-endian PCM.
	FormatS16 = 0
	// FormatF32 is interleaved 32-bit IEEE float PCM, samples in [-1, 1].
	FormatF32 = 1
)

const (
	abiVersionMajor = 1
	// abiVersionMinor mirrors VD_ABI_VERSION_MINOR; only the major number is
	// enforced during Open.
	abiVersionMinor = 0

	envLibPath = "VDNOISE_LIB"
)

// Frame size limits enforced by the reference library.
const (
	MinFrameSize = 1
	MaxFrameSize = 8192
)

// Library is a loaded dynamic library plus resolved symbols.
type Library struct {
	handle uintptr

	abiVersion uintptr
	versionFn  func() string
	open       uintptr
	setTotal   uintptr
	processS16 uintptr
	processF32 uintptr
	finish     uintptr
	closeFn    uintptr

	closed bool
	mu     sync.Mutex
}

// Option configures library loading.
type Option func(*loadConfig)

type loadConfig struct {
	paths []string // explicit paths, tried first
}

// WithPath adds an explicit library location (file or directory). Directories
// are searched with the platform default file name. It may be passed multiple
// times; the CLI flag -lib maps to this option.
func WithPath(path string) Option {
	return func(c *loadConfig) {
		if path != "" {
			c.paths = append(c.paths, path)
		}
	}
}

// DefaultLibName returns the platform specific library file name.
func DefaultLibName() string {
	switch runtime.GOOS {
	case "darwin":
		return "libvdnoise.dylib"
	case "windows":
		return "vdnoise.dll"
	default:
		return "libvdnoise.so"
	}
}

// Open locates, loads and validates the denoiser shared library.
//
// Search order:
//  1. if WithPath (or VDNOISE_LIB / -lib) was given, only those locations are
//     tried, in order — a failure there is reported rather than silently
//     overridden by a default library
//  2. otherwise: next to the executable and its parents' "native/lib",
//     ./native/lib and ./lib relative to the working directory
//  3. finally the bare default name, letting the OS loader search its standard
//     paths (LD_LIBRARY_PATH, DYLD_LIBRARY_PATH, PATH, /usr/local/lib, ...)
//
// On total failure a *LoadError is returned listing every attempt and the
// underlying reason.
func Open(opts ...Option) (*Library, error) {
	cfg := loadConfig{}
	for _, o := range opts {
		o(&cfg)
	}

	var explicit []string
	explicit = append(explicit, cfg.paths...)
	if env := os.Getenv(envLibPath); env != "" {
		explicit = append(explicit, env)
	}

	var candidates []string
	if len(explicit) > 0 {
		// An explicit request is authoritative: resolve files/directories,
		// and report the OS-level reason instead of silently falling back.
		candidates = resolveExplicit(explicit)
	} else {
		candidates = defaultCandidates()
	}

	var attempts []LoadAttempt
	var handle uintptr
	for _, cand := range candidates {
		h, err := dlopen(cand)
		if err != nil {
			attempts = append(attempts, LoadAttempt{Path: cand, Err: err})
			continue
		}
		handle = h
		break
	}
	if handle == 0 {
		return nil, &LoadError{Attempts: attempts}
	}

	lib := &Library{handle: handle}
	required := map[string]*uintptr{
		"vd_abi_version": &lib.abiVersion,
		"vd_open":        &lib.open,
		"vd_set_total":   &lib.setTotal,
		"vd_process_s16": &lib.processS16,
		"vd_process_f32": &lib.processF32,
		"vd_finish":      &lib.finish,
		"vd_close":       &lib.closeFn,
	}
	for name, slot := range required {
		addr, err := dlsym(handle, name)
		if err != nil || addr == 0 {
			_ = dlclose(handle)
			return nil, &LoadError{Symbol: name}
		}
		*slot = addr
	}
	// vd_version is bound to a Go-typed func so purego copies the returned
	// NUL-terminated char* into a real Go string (no unsafe pointer reads).
	verAddr, err := dlsym(handle, "vd_version")
	if err != nil || verAddr == 0 {
		_ = dlclose(handle)
		return nil, &LoadError{Symbol: "vd_version"}
	}
	var versionFn func() string
	registerFunc(&versionFn, verAddr)
	lib.versionFn = versionFn

	ver := int(lib.call0(lib.abiVersion))
	if ver>>16 != abiVersionMajor {
		_ = dlclose(handle)
		return nil, &LoadError{ABIVersion: ver}
	}
	return lib, nil
}

// resolveExplicit turns user supplied -lib/VDNOISE_LIB entries into concrete
// file paths (directories get the platform default name appended).
func resolveExplicit(explicit []string) []string {
	name := DefaultLibName()
	var out []string
	seen := map[string]bool{}
	for _, p := range explicit {
		if p == "" {
			continue
		}
		if info, err := os.Stat(p); err == nil && info.IsDir() {
			p = filepath.Join(p, name)
		}
		if !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}
	return out
}

func defaultCandidates() []string {
	name := DefaultLibName()
	var out []string
	seen := map[string]bool{}
	add := func(p string) {
		if p == "" {
			return
		}
		if !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}

	// Bundled locations.
	exe, err := os.Executable()
	if err == nil {
		dir := filepath.Dir(exe)
		add(filepath.Join(dir, name))
		add(filepath.Join(dir, "lib", name))
		// walk at most 4 parents looking for native/lib/<name>
		d := dir
		for i := 0; i < 4; i++ {
			parent := filepath.Dir(d)
			if parent == d {
				break
			}
			d = parent
			add(filepath.Join(d, "native", "lib", name))
		}
	}
	add(filepath.Join("native", "lib", name))
	add(filepath.Join("lib", name))

	// Last resort: OS loader default search.
	add(name)
	return out
}

// Version returns the native library's human readable version string.
func (l *Library) Version() (string, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		return "", ErrClosed
	}
	return l.versionFn(), nil
}

// ABI returns (major, minor) spoken by the loaded library.
func (l *Library) ABI() (int, int) {
	v := int(l.call0(l.abiVersion))
	return v >> 16, v & 0xffff
}

// Close releases the dynamic library. After Close, contexts created from it
// must not be used.
func (l *Library) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		return nil
	}
	l.closed = true
	return dlclose(l.handle)
}

// call0 invokes a native symbol with no arguments.
func (l *Library) call0(fn uintptr) uintptr {
	return syscallN(fn)
}

// ctxHandle is an opaque pointer returned by vd_open.
type ctxHandle = unsafe.Pointer

// syscall helpers -----------------------------------------------------------

func (l *Library) invoke(fn uintptr, args ...uintptr) int {
	return int(syscallN(fn, args...))
}
