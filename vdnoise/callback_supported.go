//go:build linux || darwin || windows

package vdnoise

import (
	"sync"
	"sync/atomic"

	"github.com/ebitengine/purego"
)

// progressRegistry bridges the C function-pointer callback back into Go.
//
// purego.NewCallback turns a single Go func into a native ABI trampoline; we
// create it once and multiplex by the opaque "user" token handed to vd_open,
// so any number of Contexts can coexist.
type progressRegistry struct {
	token atomic.Uint64
	m     sync.RWMutex
	fns   map[uintptr]ProgressFunc
}

var registry = &progressRegistry{fns: map[uintptr]ProgressFunc{}}

// trampoline is the one native function pointer passed to vd_open.
var trampoline uintptr

func init() {
	trampoline = purego.NewCallback(progressTrampoline)
}

// progressTrampoline runs on the calling (native) goroutine.
//
//go:nocheckptr
func progressTrampoline(percent int, user uintptr) int {
	registry.m.RLock()
	fn := registry.fns[user]
	registry.m.RUnlock()
	if fn == nil {
		return 0
	}
	// Never let a user panic propagate through native frames.
	defer func() { _ = recover() }()
	fn(percent)
	return 0
}

// add registers fn and returns (native callback pointer, user token).
// A nil fn still gets a token so the native side can validate the pointer.
func (r *progressRegistry) add(fn ProgressFunc) (cb uintptr, tok uintptr) {
	tok = uintptr(r.token.Add(1))
	r.m.Lock()
	r.fns[tok] = fn
	r.m.Unlock()
	return trampoline, tok
}

func (r *progressRegistry) remove(tok uintptr) {
	r.m.Lock()
	delete(r.fns, tok)
	r.m.Unlock()
}
