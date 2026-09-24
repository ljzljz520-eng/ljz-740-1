package noisego

import "math"

// PCM16 转 float32（范围归一化到 [-1,1]）。
func pcm16ToFloat32(in []byte) []float32 {
	n := len(in) / 2
	out := make([]float32, n)
	for i := 0; i < n; i++ {
		lo := int16(in[2*i])
		hi := int16(in[2*i+1]) << 8
		s := lo | hi
		out[i] = float32(s) / 32768.0
	}
	return out
}

// float32 转 PCM16（带削波与抖动四舍五入）。
func float32ToPCM16(in []float32) []byte {
	out := make([]byte, 2*len(in))
	for i, v := range in {
		v = float32(math.Max(-1, math.Min(1, float64(v))))
		s := int16(math.Round(float64(v) * 32767))
		out[2*i] = byte(s)
		out[2*i+1] = byte(s >> 8)
	}
	return out
}
