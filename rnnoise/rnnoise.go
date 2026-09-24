// Package rnnoise is a cgo-free binding to the RNNoise speech denoising
// shared library (https://github.com/xiph/rnnoise).
//
// The native library is loaded at runtime with github.com/ebitengine/purego,
// so programs that import this package can be built with CGO_ENABLED=0 and
// cross compiled for the three supported desktop platforms:
// Linux (librnnoise.so), macOS (librnnoise.dylib) and Windows (rnnoise.dll).
//
// The binding covers the complete lifecycle of the C API:
//
//   - Open / New:   create a denoising context (rnnoise_create)
//   - Process:      feed PCM frames and read back cleaned PCM (rnnoise_process_frame)
//   - progress:     a Go callback is invoked after every processed frame
//   - Close/Free:   destroy the context and release the native library
//
// A Denoiser is serialized internally: methods may be called concurrently,
// but they will block one another. For real parallel throughput create one
// Denoiser (and one goroutine) per audio stream.
package rnnoise

import (
	"errors"
	"fmt"
	"runtime"
	"sync"
)

const (
	// SampleRate is the only sample rate RNNoise was trained on. Input at any
	// other rate must be resampled before processing.
	SampleRate = 48000

	// FrameSize is the fixed number of samples processed by one call to
	// rnnoise_process_frame (10 ms at 48 kHz).
	FrameSize = 480
)

// ErrClosed is returned when a closed Denoiser or Library is used.
var ErrClosed = errors.New("rnnoise: resource already closed")

// ErrAborted is returned from Process when the progress callback asks to stop.
var ErrAborted = errors.New("rnnoise: processing aborted by progress callback")

// FrameSizeMismatchError is returned when a frame handed to ProcessFrame does
// not contain exactly FrameSize samples.
type FrameSizeMismatchError struct {
	Got int
}

func (e FrameSizeMismatchError) Error() string {
	return fmt.Sprintf("rnnoise: frame must contain %d samples, got %d", FrameSize, e.Got)
}

// ProgressFunc is called after each processed frame.
//
// done is the number of samples consumed so far, total is the number of
// samples in the whole stream (0 when unknown) and vadProb is the voice
// activity probability reported by RNNoise for that frame, in [0, 1].
//
// Returning true stops processing early; Process then returns ErrAborted.
type ProgressFunc func(done, total int, vadProb float32) (abort bool)

// Library is a successfully loaded RNNoise shared library. It holds the
// native handle and the resolved C entry points.
type Library struct {
	handle libHandle
	path   string

	create     func(model uintptr) uintptr
	destroy    func(st uintptr)
	process    func(st uintptr, out, in uintptr) float32
	getFrameSz func() int32

	mu     sync.Mutex
	refs   int64 // live Denoisers created from this library
	closed bool
}

// Option configures library loading.
type Option func(*options)

type options struct {
	libPath string
}

// WithLibraryPath pins the loader to an exact file (or a name that the
// platform loader resolves, e.g. "librnnoise.so"). When not set the package
// searches a list of default locations and finally the system search path.
func WithLibraryPath(path string) Option {
	return func(o *options) { o.libPath = path }
}

// Open loads the RNNoise shared library and verifies that every symbol the
// binding needs is present. Use WithLibraryPath to control where the library
// is loaded from; without it a platform specific default search is performed.
//
// If loading fails, the returned *LoadError describes every path that was
// tried, the OS error for each one and usually how to fix the problem.
func Open(opts ...Option) (*Library, error) {
	cfg := options{}
	for _, o := range opts {
		o(&cfg)
	}

	handle, usedPath, err := loadLibrary(cfg.libPath)
	if err != nil {
		return nil, err
	}

	lib := &Library{handle: handle, path: usedPath}
	if err := lib.bind(); err != nil {
		// No Denoiser exists yet, nothing else can reference the handle.
		_ = closeLib(handle)
		return nil, err
	}
	return lib, nil
}

// Path reports the file/name the library was actually loaded from.
func (l *Library) Path() string { return l.path }

// FrameSize returns the frame size declared by the loaded library. Every
// known RNNoise build returns 480.
func (l *Library) FrameSize() int {
	if l.getFrameSz != nil {
		if n := l.getFrameSz(); n > 0 {
			return int(n)
		}
	}
	return FrameSize
}

// NewContext creates a denoising context (rnnoise_create with the built-in
// model). The context must be closed with Close to free native memory.
func (l *Library) NewContext() (*Denoiser, error) {
	l.mu.Lock()
	if l.closed {
		l.mu.Unlock()
		return nil, ErrClosed
	}
	st := l.create(0)
	if st == 0 {
		l.mu.Unlock()
		return nil, errors.New("rnnoise: rnnoise_create returned NULL (out of memory?)")
	}
	l.refs++
	l.mu.Unlock()
	d := &Denoiser{lib: l, state: st, ownsLib: false}
	runtime.SetFinalizer(d, func(x *Denoiser) { _ = x.Close() })
	return d, nil
}

// Close releases the native library handle (dlclose / FreeLibrary). It fails
// with ErrClosed if already closed, and refuses to unload while Denoisers
// created from this library are still open.
func (l *Library) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		return ErrClosed
	}
	if l.refs > 0 {
		return fmt.Errorf("rnnoise: cannot close library while %d denoiser(s) are still open", l.refs)
	}
	if err := closeLib(l.handle); err != nil {
		return err
	}
	l.closed = true
	return nil
}

// Denoiser wraps a native DenoiseState created by rnnoise_create.
type Denoiser struct {
	mu      sync.Mutex
	lib     *Library
	state   uintptr
	ownsLib bool
	closed  bool
}

// New is a convenience function: it loads the library with Open (honouring
// the supplied options), creates one context and marks the Denoiser as the
// owner of the library: closing the Denoiser also unloads the library.
//
// This is the easiest entry point for "load once, process, release" usage.
// For several concurrent streams, Open a single Library and call
// Library.NewContext repeatedly instead.
func New(opts ...Option) (*Denoiser, error) {
	lib, err := Open(opts...)
	if err != nil {
		return nil, err
	}
	d, err := lib.NewContext()
	if err != nil {
		_ = lib.Close()
		return nil, err
	}
	d.ownsLib = true
	return d, nil
}

// Process runs RNNoise over a whole PCM stream of mono 48 kHz float32 samples
// and returns the cleaned stream with exactly the same length.
//
// Samples are processed in frames of 480; the final, partial frame is
// zero-padded before processing and the padding is trimmed from the output.
// If progress is non-nil it is invoked after each frame; returning true from
// it aborts processing and makes Process return ErrAborted.
func (d *Denoiser) Process(in []float32, progress ProgressFunc) ([]float32, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closed {
		return nil, ErrClosed
	}
	frameN := d.lib.FrameSize()
	if len(in) == 0 {
		return []float32{}, nil
	}

	out := make([]float32, len(in))
	inFrame := make([]float32, frameN)
	outFrame := make([]float32, frameN) // native always writes frameN samples
	total := len(in)

	for off := 0; off < total; off += frameN {
		n := total - off
		if n > frameN {
			n = frameN
		}
		// Clear then copy so the native side never reads uninitialized data
		// past the end of a partial final frame.
		for i := range inFrame {
			inFrame[i] = 0
		}
		copy(inFrame, in[off:off+n])

		vad := d.lib.process(d.state,
			uintptr(ptr(outFrame)),
			uintptr(ptr(inFrame)))
		// rnnoise always emits a full frame; keep only the samples that
		// correspond to real (non-padded) input.
		copy(out[off:off+n], outFrame[:n])

		if progress != nil {
			done := off + n
			if done > total {
				done = total
			}
			if progress(done, total, vad) {
				return out[:off+n], ErrAborted
			}
		}
	}
	return out, nil
}

// ProcessFrame processes exactly one frame of FrameSize samples and returns
// the cleaned frame and the frame's voice-activity probability in [0, 1].
func (d *Denoiser) ProcessFrame(in []float32) (out []float32, vadProb float32, err error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closed {
		return nil, 0, ErrClosed
	}
	if len(in) != FrameSize {
		return nil, 0, FrameSizeMismatchError{Got: len(in)}
	}
	out = make([]float32, FrameSize)
	vadProb = d.lib.process(d.state, uintptr(ptr(out)), uintptr(ptr(in)))
	return out, vadProb, nil
}

// ProcessInt16 is a convenience wrapper around Process for interleaved signed
// 16-bit PCM (the format used by WAV files).
func (d *Denoiser) ProcessInt16(in []int16, progress ProgressFunc) ([]int16, error) {
	floats := make([]float32, len(in))
	for i, v := range in {
		floats[i] = float32(v) / 32768.0
	}
	out, err := d.Process(floats, progress)
	if err != nil {
		return nil, err
	}
	pcm := make([]int16, len(out))
	for i, v := range out {
		// Clamp before scaling back; RNNoise can overshoot slightly.
		if v > 1 {
			v = 1
		} else if v < -1 {
			v = -1
		}
		pcm[i] = int16(v * 32767.0)
	}
	return pcm, nil
}

// Close destroys the native denoising state (rnnoise_destroy). If the
// Denoiser was created with New it also unloads the shared library. Close is
// safe to call multiple times.
func (d *Denoiser) Close() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closed {
		return ErrClosed
	}
	d.closed = true
	if d.state != 0 && d.lib.destroy != nil {
		d.lib.destroy(d.state)
		d.state = 0
	}
	d.lib.mu.Lock()
	d.lib.refs--
	d.lib.mu.Unlock()
	if d.ownsLib {
		d.ownsLib = false
		return d.lib.Close()
	}
	return nil
}

// LibraryPath reports the file the underlying native library was loaded
// from (empty if the Denoiser has already been closed).
func (d *Denoiser) LibraryPath() string {
	if d.lib == nil {
		return ""
	}
	return d.lib.Path()
}
