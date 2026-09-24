// rnnoise-cli denoises a WAV speech recording with RNNoise.
//
// Usage:
//
//	rnnoise-cli -input noisy.wav -output clean.wav [-lib /path/to/librnnoise.so] [-no-progress]
//
// The input may be any PCM/IEEE-float WAV at any sample rate and channel
// count; it is downmixed to mono and resampled to 48 kHz for RNNoise. The
// output is a 48 kHz mono 16-bit PCM WAV.
package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/solomanager/go-rnnoise/rnnoise"
	wavpkg "github.com/solomanager/go-rnnoise/wav"
)

func main() {
	var (
		input      = flag.String("input", "", "input WAV file (required)")
		output     = flag.String("output", "", "output WAV file (required)")
		libPath    = flag.String("lib", os.Getenv("RNNOISE_LIB"), "path to the RNNoise shared library (defaults to $RNNOISE_LIB, then built-in search)")
		noProgress = flag.Bool("no-progress", false, "disable the progress bar")
	)
	flag.Parse()

	if *input == "" || *output == "" {
		fmt.Fprintln(os.Stderr, "error: -input and -output are required")
		flag.Usage()
		os.Exit(2)
	}
	if err := run(*input, *output, *libPath, *noProgress); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func run(inPath, outPath, libPath string, noProgress bool) error {
	inFile, err := wavpkg.ReadFile(inPath)
	if err != nil {
		return fmt.Errorf("read input wav: %w", err)
	}
	inRate := int(inFile.Format.SampleRate)
	ch := int(inFile.Format.Channels)
	frames := inFile.Frames()
	fmt.Fprintf(os.Stderr, "input : %s (%d Hz, %d channel(s), %.2fs)\n",
		inPath, inRate, ch, float64(frames)/float64(inRate))

	mono := wavpkg.Mono(inFile.Samples, ch)
	src48k := wavpkg.Resample(mono, inRate, rnnoise.SampleRate)
	fmt.Fprintf(os.Stderr, "prep  : downmixed to mono and resampled to %d Hz\n", rnnoise.SampleRate)

	// Load the native library (path configurable) and create a context.
	var opts []rnnoise.Option
	if libPath != "" {
		opts = append(opts, rnnoise.WithLibraryPath(libPath))
	}
	den, err := rnnoise.New(opts...)
	if err != nil {
		return err
	}
	defer den.Close()
	fmt.Fprintf(os.Stderr, "library loaded: %s\n", den.LibraryPath())

	// Progress callback: one tick per 480-sample frame.
	var bar *progressBar
	var progress rnnoise.ProgressFunc
	if !noProgress {
		bar = newProgressBar(os.Stderr, len(src48k))
		progress = func(done, total int, vad float32) bool {
			bar.tick(done, total, vad)
			return false
		}
	}

	start := time.Now()
	clean, err := den.Process(src48k, progress)
	if err != nil {
		return fmt.Errorf("denoise: %w", err)
	}
	if bar != nil {
		bar.finish()
	}
	fmt.Fprintf(os.Stderr, "denoise: %d samples processed in %s\n", len(clean), time.Since(start).Round(time.Millisecond))

	if err := wavpkg.WriteFile(outPath, rnnoise.SampleRate, clean); err != nil {
		return fmt.Errorf("write output wav: %w", err)
	}
	fmt.Fprintf(os.Stderr, "output: %s (%d Hz, mono, 16-bit PCM)\n",
		outPath, rnnoise.SampleRate)
	return nil
}

// progressBar draws a simple textual bar on stderr.
type progressBar struct {
	w       io.Writer
	total   int
	start   time.Time
	lastPct int
}

func newProgressBar(w io.Writer, total int) *progressBar {
	return &progressBar{w: w, total: total, start: time.Now(), lastPct: -1}
}

func (p *progressBar) tick(done, total int, _ float32) {
	if total <= 0 {
		return
	}
	pct := done * 100 / total
	if pct == p.lastPct {
		return
	}
	p.lastPct = pct
	const width = 30
	filled := pct * width / 100
	elapsed := time.Since(p.start).Truncate(time.Second)
	fmt.Fprintf(p.w, "\r[")
	for i := 0; i < width; i++ {
		if i < filled {
			fmt.Fprint(p.w, "#")
		} else {
			fmt.Fprint(p.w, "-")
		}
	}
	fmt.Fprintf(p.w, "] %3d%%  %s elapsed", pct, elapsed)
}

func (p *progressBar) finish() {
	fmt.Fprintln(p.w)
}
