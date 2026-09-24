package noisego

import (
	"bytes"
	"testing"
)

func TestWAVRoundTrip(t *testing.T) {
	orig := &WaveData{
		SampleRate: 48000,
		Samples:    []float32{0, 0.1, -0.1, 0.5, -0.5, 1, -1},
	}
	var buf bytes.Buffer
	if err := WriteWAV(&buf, orig); err != nil {
		t.Fatalf("WriteWAV: %v", err)
	}
	got, err := ReadWAV(&buf)
	if err != nil {
		t.Fatalf("ReadWAV: %v", err)
	}
	if got.SampleRate != orig.SampleRate {
		t.Errorf("采样率: got %d want %d", got.SampleRate, orig.SampleRate)
	}
	if len(got.Samples) != len(orig.Samples) {
		t.Fatalf("采样数: got %d want %d", len(got.Samples), len(orig.Samples))
	}
	for i := range orig.Samples {
		if d := got.Samples[i] - orig.Samples[i]; d > 0.001 || d < -0.001 {
			t.Errorf("第 %d 个采样: got %v want %v", i, got.Samples[i], orig.Samples[i])
		}
	}
}

func TestReadWAVRejectsStereo(t *testing.T) {
	// 构造双声道 WAV
	var b bytes.Buffer
	b.WriteString("RIFF")
	b.Write(make([]byte, 4))
	b.WriteString("WAVEfmt ")
	writeLE32(&b, 16)
	writeLE16(&b, 1) // PCM
	writeLE16(&b, 2) // 2 声道
	writeLE32(&b, 48000)
	writeLE32(&b, 48000*2*2)
	writeLE16(&b, 4)
	writeLE16(&b, 16)
	if _, err := ReadWAV(&b); err == nil {
		t.Fatal("双声道 WAV 应报错")
	}
}

func writeLE16(b *bytes.Buffer, v uint16) { b.WriteByte(byte(v)); b.WriteByte(byte(v >> 8)) }
func writeLE32(b *bytes.Buffer, v uint32) {
	b.WriteByte(byte(v))
	b.WriteByte(byte(v >> 8))
	b.WriteByte(byte(v >> 16))
	b.WriteByte(byte(v >> 24))
}

func TestResampleLinear(t *testing.T) {
	// 恒值信号重采样后应保持恒值。
	in := make([]float32, 480)
	for i := range in {
		in[i] = 0.7
	}
	out := ResampleLinear(in, 48000, 16000)
	if len(out) != 160 {
		t.Fatalf("重采样长度应为 160，得到 %d", len(out))
	}
	for i, v := range out {
		if v < 0.699 || v > 0.701 {
			t.Fatalf("第 %d 个采样不是恒值: %v", i, v)
		}
	}
	// 相同采样率应返回拷贝。
	out2 := ResampleLinear(in, 48000, 48000)
	if len(out2) != len(in) {
		t.Fatalf("同采样率长度错误: %d", len(out2))
	}
}
