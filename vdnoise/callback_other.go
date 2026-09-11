//go:build !linux && !darwin && !windows

package vdnoise

// Stub callback plumbing for platforms without a purego backend; Open fails
// before a context is ever created, but the package still type-checks.

type progressRegistry struct{}

var registry = &progressRegistry{}

func (r *progressRegistry) add(fn ProgressFunc) (cb uintptr, tok uintptr) { return 0, 0 }
func (r *progressRegistry) remove(tok uintptr)                            {}
