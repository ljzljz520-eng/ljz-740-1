package noisego

// ResampleLinear 用线性插值把采样率 fromRate 的数据重采样到 toRate。
//
// 这是不引入额外依赖的轻量实现，语音质量要求高的场景建议先离线转成
// 48kHz（如 ffmpeg -ar 48000）。
func ResampleLinear(in []float32, fromRate, toRate int) []float32 {
	if fromRate == toRate || len(in) == 0 {
		return append([]float32(nil), in...)
	}
	ratio := float64(fromRate) / float64(toRate)
	outLen := int(float64(len(in)) * float64(toRate) / float64(fromRate))
	if outLen < 1 {
		outLen = 1
	}
	out := make([]float32, outLen)
	for i := 0; i < outLen; i++ {
		src := float64(i) * ratio
		idx := int(src)
		frac := float32(src - float64(idx))
		switch {
		case idx+1 < len(in):
			out[i] = in[idx] + (in[idx+1]-in[idx])*frac
		case idx < len(in):
			out[i] = in[idx]
		default:
			out[i] = 0
		}
	}
	return out
}
