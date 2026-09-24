package wav

import (
	"bytes"
	"encoding/binary"
	"math"
)

// encodePCM builds interleaved little-endian PCM for [frame][channel].
func encodePCM(bits int, frames [][]float32) []byte {
	bps := bits / 8
	var b bytes.Buffer
	for _, fr := range frames {
		for _, s := range fr {
			switch bits {
			case 8:
				b.WriteByte(byte(math.Round(float64(s)*127 + 128)))
			case 16:
				binary.Write(&b, binary.LittleEndian, int16(math.Round(float64(s)*32767)))
			case 24:
				v := int32(math.Round(float64(s) * 8388607))
				b.WriteByte(byte(v))
				b.WriteByte(byte(v >> 8))
				b.WriteByte(byte(v >> 16))
			case 32:
				binary.Write(&b, binary.LittleEndian, int32(math.Round(float64(s)*2147483647)))
			}
			_ = bps
		}
	}
	return b.Bytes()
}

// buildWAV assembles a minimal PCM WAV.
func buildWAV(channels, rate int, bits uint16, data []byte) []byte {
	var b bytes.Buffer
	put := func(v any) { binary.Write(&b, binary.LittleEndian, v) }
	b.WriteString("RIFF")
	put(uint32(36 + len(data)))
	b.WriteString("WAVEfmt ")
	put(uint32(16))
	put(uint16(fmtPCM))
	put(uint16(channels))
	put(uint32(rate))
	put(uint32(int(rate) * int(channels) * int(bits) / 8))
	put(uint16(channels * int(bits) / 8))
	put(uint16(bits))
	b.WriteString("data")
	put(uint32(len(data)))
	b.Write(data)
	return b.Bytes()
}
