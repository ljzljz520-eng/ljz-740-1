// Command vdnoise denoises a WAV file with the libvdnoise shared library.
//
// Usage:
//
//	vdnoise -in noisy.wav -out clean.wav [-lib /path/to/libvdnoise.so]
//	        [-frame 480] [-quiet]
//
// Supported inputs: mono/stereo, 16-bit PCM or 32-bit IEEE float WAV. The
// output keeps the same format. The library is searched (in order) from
// -lib, the VDNOISE_LIB env var, locations next to the executable, ./native/lib
// and finally the OS loader's default search path.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"govdnoise/internal/wav"
	"govdnoise/vdnoise"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	fs := flag.NewFlagSet("vdnoise", flag.ContinueOnError)
	inPath := fs.String("in", "", "input WAV file (16-bit PCM or 32-bit float, mono/stereo)")
	outPath := fs.String("out", "", "output WAV file")
	libPath := fs.String("lib", os.Getenv("VDNOISE_LIB"), "path to the denoiser shared library (file or directory); defaults to $VDNOISE_LIB")
	frame := fs.Int("frame", 480, "frame size in samples per channel (1..8192)")
	quiet := fs.Bool("quiet", false, "suppress the progress bar")
	showVer := fs.Bool("version", false, "print version information and exit")
	if err := fs.Parse(args); err != nil {
		return err
	}

	var opts []vdnoise.Option
	if libPath != nil && *libPath != "" {
		opts = append(opts, vdnoise.WithPath(*libPath))
	}
	lib, err := vdnoise.Open(opts...)
	if err != nil {
		return err // *vdnoise.LoadError already explains every attempt
	}
	defer lib.Close()

	nv, _ := lib.Version()
	maj, min := lib.ABI()
	if *showVer {
		fmt.Printf("binding: govdnoise (ABI %d.%d)\nnative:  %s\n", maj, min, nv)
		return nil
	}

	if *inPath == "" || *outPath == "" {
		return errors.New("both -in and -out are required (see -h for usage)")
	}
	if *frame < vdnoise.MinFrameSize || *frame > vdnoise.MaxFrameSize {
		return fmt.Errorf("-frame must be in [%d,%d]", vdnoise.MinFrameSize, vdnoise.MaxFrameSize)
	}

	in, err := os.Open(*inPath)
	if err != nil {
		return fmt.Errorf("open input: %w", err)
	}
	defer in.Close()
	audio, err := wav.Decode(in)
	if err != nil {
		return fmt.Errorf("read input: %w", err)
	}

	out, err := os.Create(*outPath)
	if err != nil {
		return fmt.Errorf("create output: %w", err)
	}
	defer out.Close()

	format := vdnoise.FormatS16
	wfmt := wav.FormatPCM
	isFloat := audio.Format == wav.FormatIEEEFloat
	if isFloat {
		format = vdnoise.FormatF32
		wfmt = wav.FormatIEEEFloat
	}
	total := int64(audio.NumSamples() / (int(audio.Channels) * *frame))

	bar := newProgressBar(os.Stderr, *quiet)
	ctx, err := lib.NewContext(vdnoise.ContextConfig{
		SampleRate:  audio.SampleRate,
		Channels:    uint32(audio.Channels),
		FrameSize:   uint32(*frame),
		Format:      format,
		TotalFrames: total,
		Progress:    bar.update,
	})
	if err != nil {
		return fmt.Errorf("create denoiser context: %w", err)
	}
	defer ctx.Close()

	ww, err := wav.NewWriter(out, audio.SampleRate, audio.Channels, wfmt)
	if err != nil {
		return err
	}

	stride := int(audio.Channels) * *frame
	if isFloat {
		inS, outS := audio.F32, make([]float32, stride)
		for off := 0; off < len(inS); off += stride {
			chunk := inS[off:]
			if len(chunk) > stride {
				chunk = chunk[:stride]
			}
			if len(chunk) < stride {
				// pad the final partial frame with digital silence
				padded := make([]float32, stride)
				copy(padded, chunk)
				if _, err := ctx.Process(padded, outS); err != nil {
					return fmt.Errorf("process: %w", err)
				}
				if err := ww.WriteF32(outS[:len(chunk)]); err != nil {
					return err
				}
			} else {
				if _, err := ctx.Process(chunk, outS); err != nil {
					return fmt.Errorf("process: %w", err)
				}
				if err := ww.WriteF32(outS); err != nil {
					return err
				}
			}
		}
	} else {
		inS, outS := audio.S16, make([]int16, stride)
		for off := 0; off < len(inS); off += stride {
			chunk := inS[off:]
			if len(chunk) > stride {
				chunk = chunk[:stride]
			}
			if len(chunk) < stride {
				padded := make([]int16, stride)
				copy(padded, chunk)
				if _, err := ctx.Process(padded, outS); err != nil {
					return fmt.Errorf("process: %w", err)
				}
				if err := ww.WriteS16(outS[:len(chunk)]); err != nil {
					return err
				}
			} else {
				if _, err := ctx.Process(chunk, outS); err != nil {
					return fmt.Errorf("process: %w", err)
				}
				if err := ww.WriteS16(outS); err != nil {
					return err
				}
			}
		}
	}
	if err := ctx.Finish(); err != nil {
		return fmt.Errorf("finish: %w", err)
	}
	bar.finish()
	if err := ww.Close(); err != nil {
		return fmt.Errorf("finalize output: %w", err)
	}

	fmt.Fprintf(os.Stderr, "%s: %s -> %s (%s, %d Hz, %d ch, frame=%d, native=%s)\n",
		"done", *inPath, *outPath, formatName(isFloat), audio.SampleRate, audio.Channels, *frame, nv)
	return nil
}

func formatName(isFloat bool) string {
	if isFloat {
		return "f32"
	}
	return "s16"
}

// progressBar draws an in-place 30-cell bar with a percentage.
type progressBar struct {
	w     io.Writer
	quiet bool
	last  int
}

func newProgressBar(w io.Writer, quiet bool) *progressBar {
	return &progressBar{w: w, quiet: quiet, last: -1}
}

const barWidth = 30

func (p *progressBar) update(percent int) {
	if p.quiet || percent == p.last {
		return
	}
	p.last = percent
	filled := percent * barWidth / 100
	bar := strings.Repeat("#", filled) + strings.Repeat("-", barWidth-filled)
	fmt.Fprintf(p.w, "\r[%s] %3d%%", bar, percent)
}

func (p *progressBar) finish() {
	if p.quiet {
		return
	}
	if p.last != 100 {
		p.update(100)
	}
	fmt.Fprintln(p.w)
}
