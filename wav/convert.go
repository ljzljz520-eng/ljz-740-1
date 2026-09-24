package wav

import "math"

func mathFloat32frombits(b uint32) float32 { return math.Float32frombits(b) }
func mathFloat64frombits(b uint64) float64 { return math.Float64frombits(b) }

// Mono downmixes interleaved samples with the given channel count to mono by
// simple averaging. Input whose length is not a multiple of channels is
// truncated to whole frames.
func Mono(interleaved []float32, channels int) []float32 {
	if channels <= 0 {
		return nil
	}
	if channels == 1 {
		out := make([]float32, len(interleaved))
		copy(out, interleaved)
		return out
	}
	frames := len(interleaved) / channels
	out := make([]float32, frames)
	for f := 0; f < frames; f++ {
		var sum float32
		base := f * channels
		for c := 0; c < channels; c++ {
			sum += interleaved[base+c]
		}
		out[f] = sum / float32(channels)
	}
	return out
}

// Resample converts mono PCM from srcRate to dstRate with linear
// interpolation. Good enough for feeding a neural denoiser; it is not a
// high-fidelity resampler.
func Resample(in []float32, srcRate, dstRate int) []float32 {
	if srcRate == dstRate || srcRate <= 0 || dstRate <= 0 || len(in) == 0 {
		out := make([]float32, len(in))
		copy(out, in)
		return out
	}
	ratio := float64(srcRate) / float64(dstRate)
	outLen := int(float64(len(in)) * float64(dstRate) / float64(srcRate))
	out := make([]float32, outLen)
	for i := 0; i < outLen; i++ {
		pos := float64(i) * ratio
		i0 := int(pos)
		frac := float32(pos - float64(i0))
		var s0, s1 float32
		s0 = in[i0]
		if i0+1 < len(in) {
			s1 = in[i0+1]
		} else {
			s1 = s0
		}
		out[i] = s0 + (s1-s0)*frac
	}
	return out
}
