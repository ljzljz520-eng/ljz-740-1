// Minimal example: load RNNoise, denoise 1 s of 48 kHz mono float32 PCM,
// report progress, release everything.
package main

import (
	"fmt"
	"math"

	"github.com/solomanager/go-rnnoise/rnnoise"
)

func main() {
	// New() = Open() + create context; Close() frees both.
	// Use rnnoise.WithLibraryPath("/path/to/librnnoise.so") to pin the file.
	den, err := rnnoise.New()
	if err != nil {
		panic(err)
	}
	defer den.Close()
	fmt.Println("loaded:", den.LibraryPath())

	// 1 second of fake noisy PCM at the only supported rate.
	in := make([]float32, rnnoise.SampleRate)
	for i := range in {
		t := float64(i) / rnnoise.SampleRate
		in[i] = float32(0.2*math.Sin(2*math.Pi*440*t) + 0.05*math.Sin(2*math.Pi*4000*t))
	}

	out, err := den.Process(in, func(done, total int, vad float32) bool {
		fmt.Printf("\rprogress: %5.1f%%  vad: %.2f", 100*float64(done)/float64(total), vad)
		return false // return true to abort
	})
	if err != nil {
		panic(err)
	}
	fmt.Printf("\ndenoised %d samples\n", len(out))
}
