package rnnoise

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// attemptError records one load attempt and why it failed.
type attemptError struct {
	Path string
	Err  error
}

// LoadError is returned by Open/New when no candidate library could be
// loaded. It collects every path that was tried together with the native
// loader error for each one, plus an actionable hint.
type LoadError struct {
	Library string         // requested library path/name, may be empty
	Op      string         // "load library" or "lookup symbol ..."
	Errs    []attemptError // every candidate that was tried
	hint    string
}

func (e *LoadError) Error() string {
	var b strings.Builder
	fmt.Fprintf(&b, "rnnoise: failed to %s", e.Op)
	if e.Library != "" {
		fmt.Fprintf(&b, " %q", e.Library)
	}
	b.WriteString(":\n")
	for _, a := range e.Errs {
		fmt.Fprintf(&b, "  tried %s: %v\n", a.Path, a.Err)
	}
	if e.hint != "" {
		fmt.Fprintf(&b, "hint: %s\n", e.hint)
	}
	return strings.TrimRight(b.String(), "\n")
}

// Unwrap returns the first underlying error, so errors.Is works for common
// sentinel values such as os.ErrNotExist.
func (e *LoadError) Unwrap() error {
	if len(e.Errs) == 0 {
		return nil
	}
	return e.Errs[0].Err
}

func libNames() []string {
	switch runtime.GOOS {
	case "darwin":
		return []string{"librnnoise.dylib", "rnnoise.dylib"}
	case "windows":
		return []string{"rnnoise.dll", "librnnoise.dll"}
	default: // linux, *bsd
		return []string{"librnnoise.so", "rnnoise.so"}
	}
}

func defaultDirs() []string {
	dirs := []string{
		"./third_party",
		"./lib",
		"/usr/local/lib",
		"/usr/lib",
	}
	switch runtime.GOOS {
	case "darwin":
		dirs = append(dirs, "/opt/homebrew/lib", "/usr/local/lib")
	case "windows":
		dirs = append(dirs, filepath.Join(os.Getenv("ProgramFiles"), "rnnoise", "bin"))
	}
	if home, err := os.UserHomeDir(); err == nil {
		dirs = append(dirs, filepath.Join(home, ".local", "lib"))
	}
	return dirs
}

// candidates expands the configured path (or the default search set) into an
// ordered list of concrete files/names to try.
func candidates(configured string) []string {
	var c []string
	if configured != "" {
		c = append(c, configured)
		// A bare name is still worth handing to the system loader below;
		// an explicit path is tried verbatim only.
	} else {
		names := libNames()
		for _, dir := range defaultDirs() {
			for _, name := range names {
				c = append(c, filepath.Join(dir, name))
			}
		}
		// Finally the bare names, resolved by the OS loader (LD_LIBRARY_PATH,
		// DYLD_LIBRARY_PATH, PATH, system cache...).
		c = append(c, names...)
	}
	// For an explicit relative/absolute file we also let the OS loader try
	// the bare basename as a fallback (e.g. when only the name was given).
	return dedupe(c)
}

func dedupe(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range in {
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}

func loadHint() string {
	switch runtime.GOOS {
	case "darwin":
		return "set DYLD_LIBRARY_PATH to the directory containing librnnoise.dylib, " +
			"or pass the full path via WithLibraryPath"
	case "windows":
		return "copy rnnoise.dll next to the executable, add its directory to PATH, " +
			"or pass the full path via WithLibraryPath"
	default:
		return "install RNNoise (e.g. build it and run ldconfig), set LD_LIBRARY_PATH, " +
			"or pass the full path via WithLibraryPath"
	}
}

// loadLibrary tries every candidate and returns the first handle. On total
// failure it returns a *LoadError containing all individual errors.
func loadLibrary(configured string) (libHandle, string, error) {
	cands := candidates(configured)
	var attempts []attemptError

	for _, c := range cands {
		h, err := openLib(c)
		if err == nil {
			return h, c, nil
		}
		attempts = append(attempts, attemptError{Path: c, Err: decorateErr(c, err)})
	}

	return 0, "", &LoadError{
		Library: configured,
		Op:      "load library",
		Errs:    attempts,
		hint:    loadHint(),
	}
}

func decorateErr(path string, err error) error {
	if strings.ContainsAny(path, string([]rune{filepath.Separator, '/'})) {
		if _, statErr := os.Stat(path); statErr != nil {
			if errors.Is(statErr, os.ErrNotExist) {
				return fmt.Errorf("%w: file does not exist", err)
			}
			return fmt.Errorf("%w: cannot stat file: %v", err, statErr)
		}
	}
	return err
}
