package vdnoise

import "unsafe"

// ProgressFunc reports processing progress from the native thread.
// percent goes 0..100; 100 is delivered exactly once after Finish (or when a
// known total completes). Callbacks must return quickly and must not call back
// into the same Context: they run while the native library holds its locks.
type ProgressFunc func(percent int)

// ContextConfig configures a denoiser context.
type ContextConfig struct {
	// SampleRate in Hz, 8000..384000.
	SampleRate uint32
	// Channels is 1 (mono) or 2 (interleaved stereo).
	Channels uint32
	// FrameSize is samples per channel per Process call, 1..8192.
	FrameSize uint32
	// Format is FormatS16 or FormatF32.
	Format int
	// Progress is called from Process as the native engine advances.
	Progress ProgressFunc
	// TotalFrames, when > 0, yields exact percentages; 0 means "unknown"
	// and the estimate crawls toward 99% until Finish.
	TotalFrames int64
}

// Context is one denoising session. It must be closed after use and must not
// be shared between goroutines concurrently.
type Context struct {
	lib     *Library
	handle  ctxHandle
	format  int
	frame   uint32
	stride  int // samples per interleaved frame
	progTok uintptr
	closed  bool
}

// NewContext creates a denoiser context in the loaded library and installs
// the Go progress callback (if any) without using cgo.
func (l *Library) NewContext(cfg ContextConfig) (*Context, error) {
	l.mu.Lock()
	if l.closed {
		l.mu.Unlock()
		return nil, ErrClosed
	}
	l.mu.Unlock()

	if cfg.SampleRate < 8000 || cfg.SampleRate > 384000 {
		return nil, fmtErr("sample rate %d out of range [8000,384000]", cfg.SampleRate)
	}
	if cfg.Channels != 1 && cfg.Channels != 2 {
		return nil, fmtErr("channels must be 1 or 2, got %d", cfg.Channels)
	}
	if cfg.FrameSize < MinFrameSize || cfg.FrameSize > MaxFrameSize {
		return nil, fmtErr("frame size %d out of range [%d,%d]", cfg.FrameSize, MinFrameSize, MaxFrameSize)
	}
	if cfg.Format != FormatS16 && cfg.Format != FormatF32 {
		return nil, fmtErr("unknown sample format %d", cfg.Format)
	}

	cb, tok := registry.add(cfg.Progress)

	var h ctxHandle
	rc := l.invoke(l.open,
		uintptr(cfg.SampleRate),
		uintptr(cfg.Channels),
		uintptr(cfg.FrameSize),
		cb,
		tok,
		uintptr(unsafe.Pointer(&h)),
	)
	if rc != statusOK {
		registry.remove(tok)
		return nil, statusError(rc)
	}

	c := &Context{
		lib:     l,
		handle:  h,
		format:  cfg.Format,
		frame:   cfg.FrameSize,
		stride:  int(cfg.FrameSize) * int(cfg.Channels),
		progTok: tok,
	}
	if cfg.TotalFrames > 0 {
		if err := c.SetTotal(cfg.TotalFrames); err != nil {
			_ = c.Close()
			return nil, err
		}
	}
	return c, nil
}

// SetTotal declares (or redeclares) the number of frames still to process.
func (c *Context) SetTotal(frames int64) error {
	if c.closed {
		return ErrClosed
	}
	return statusError(c.lib.invoke(c.lib.setTotal, uintptr(c.handle), uintptr(frames)))
}

// FrameSize returns the interleaved-sample length each Process call expects.
func (c *Context) FrameSize() int { return c.stride }

// Process denoises exactly one frame.
//
// For FormatS16, in/out are []int16; for FormatF32 they are []float32. Both
// slices must have length >= FrameSize() (frame_size*channels), and may be the
// same slice for in-place processing. It returns the number of samples
// consumed (= FrameSize()). The progress callback runs synchronously here.
func (c *Context) Process(in, out any) (int, error) {
	if c.closed {
		return 0, ErrClosed
	}
	switch c.format {
	case FormatS16:
		si, ok := in.([]int16)
		if !ok || len(si) < c.stride {
			return 0, fmtErr("input must be []int16 of at least %d samples", c.stride)
		}
		so, ok := out.([]int16)
		if !ok || len(so) < c.stride {
			return 0, fmtErr("output must be []int16 of at least %d samples", c.stride)
		}
		rc := c.lib.invoke(c.lib.processS16,
			uintptr(c.handle),
			uintptr(unsafe.Pointer(&si[0])),
			uintptr(unsafe.Pointer(&so[0])),
		)
		return c.stride, statusError(rc)
	case FormatF32:
		si, ok := in.([]float32)
		if !ok || len(si) < c.stride {
			return 0, fmtErr("input must be []float32 of at least %d samples", c.stride)
		}
		so, ok := out.([]float32)
		if !ok || len(so) < c.stride {
			return 0, fmtErr("output must be []float32 of at least %d samples", c.stride)
		}
		rc := c.lib.invoke(c.lib.processF32,
			uintptr(c.handle),
			uintptr(unsafe.Pointer(&si[0])),
			uintptr(unsafe.Pointer(&so[0])),
		)
		return c.stride, statusError(rc)
	default:
		return 0, ErrInvalidArgument
	}
}

// Finish emits the terminal 100% progress event and resets counters.
func (c *Context) Finish() error {
	if c.closed {
		return ErrClosed
	}
	return statusError(c.lib.invoke(c.lib.finish, uintptr(c.handle)))
}

// Close destroys the native context and unregisters the callback.
func (c *Context) Close() error {
	if c.closed {
		return nil
	}
	c.closed = true
	c.lib.invoke(c.lib.closeFn, uintptr(c.handle))
	if c.progTok != 0 {
		registry.remove(c.progTok)
	}
	return nil
}

func fmtErr(format string, args ...any) error {
	return errWrapf(ErrInvalidArgument, format, args...)
}
