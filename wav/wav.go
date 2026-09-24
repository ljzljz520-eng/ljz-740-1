// Package wav is a small dependency-free WAV reader/writer used by the
// go-rnnoise example. It supports PCM (8/16/24/32 bit) and IEEE float
// (32/64 bit) files, including WAVE_FORMAT_EXTENSIBLE, and exposes helpers
// for downmixing to mono and resampling to the 48 kHz rate RNNoise needs.
package wav

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
)

const (
	fmtPCM        = 1
	fmtIEEEFloat  = 3
	fmtExtensible = 0xFFFE
)

// Format describes the audio format of a WAV file.
type Format struct {
	AudioFormat uint16 // 1 = PCM, 3 = IEEE float

	Channels      uint16
	SampleRate    uint32
	BitsPerSample uint16
}

// File holds a decoded WAV file: interleaved float32 samples in [-1, 1].
type File struct {
	Format  Format
	Samples []float32 // interleaved, SamplesPerFrame*Channels samples
}

// Frames returns the number of frames (one frame = one sample per channel).
func (f *File) Frames() int {
	if f.Format.Channels == 0 {
		return 0
	}
	return len(f.Samples) / int(f.Format.Channels)
}

// ReadFile decodes a WAV file from disk.
func ReadFile(path string) (*File, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return Decode(bytes.NewReader(b))
}

// Decode reads a WAV stream entirely into memory.
func Decode(r io.Reader) (*File, error) {
	data, fmtInfo, err := parseRIFF(r)
	if err != nil {
		return nil, err
	}

	ch := int(fmtInfo.Channels)
	bps := int(fmtInfo.BitsPerSample) / 8
	if ch <= 0 || bps <= 0 {
		return nil, fmt.Errorf("wav: invalid channels (%d) or bits/sample (%d)", fmtInfo.Channels, fmtInfo.BitsPerSample)
	}
	frames := len(data) / (ch * bps)
	samples := make([]float32, frames*ch)
	src := data[:frames*ch*bps]

	for i := 0; i < frames*ch; i++ {
		off := i * bps
		switch fmtInfo.AudioFormat {
		case fmtPCM:
			samples[i] = decodePCM(src[off:off+bps], int(fmtInfo.BitsPerSample))
		case fmtIEEEFloat:
			samples[i] = decodeFloat(src[off : off+bps])
		default:
			return nil, fmt.Errorf("wav: unsupported audio format %d", fmtInfo.AudioFormat)
		}
	}

	return &File{
		Format:  fmtInfo,
		Samples: samples,
	}, nil
}

func parseRIFF(r io.Reader) ([]byte, Format, error) {
	var info Format
	head := make([]byte, 12)
	if _, err := io.ReadFull(r, head); err != nil {
		return nil, info, errors.New("wav: file too short")
	}
	if string(head[0:4]) != "RIFF" || string(head[8:12]) != "WAVE" {
		return nil, info, errors.New("wav: not a RIFF/WAVE file")
	}

	var pcmData []byte
	id := make([]byte, 4)
	for {
		if _, err := io.ReadFull(r, id); err != nil {
			if err == io.EOF && pcmData != nil {
				break
			}
			if err == io.ErrUnexpectedEOF || err == io.EOF {
				break
			}
			return nil, info, err
		}
		var size32 uint32
		if err := binary.Read(r, binary.LittleEndian, &size32); err != nil {
			return nil, info, errors.New("wav: truncated chunk header")
		}
		size := int64(size32)
		body := make([]byte, size)
		if _, err := io.ReadFull(r, body); err != nil {
			return nil, info, fmt.Errorf("wav: truncated %q chunk", string(id))
		}
		if size%2 == 1 { // chunks are word-aligned
			if _, err := r.Read(make([]byte, 1)); err != nil {
				return nil, info, err
			}
		}

		switch string(id) {
		case "fmt ":
			f, err := parseFmt(body)
			if err != nil {
				return nil, info, err
			}
			info = f
		case "data":
			// The first data chunk wins; concatenate if more than one.
			pcmData = append(pcmData, body...)
		}
	}
	if pcmData == nil {
		return nil, info, errors.New("wav: no data chunk found")
	}
	if info.Channels == 0 {
		return nil, info, errors.New("wav: no fmt chunk found")
	}
	return pcmData, info, nil
}

func parseFmt(b []byte) (Format, error) {
	var f Format
	if len(b) < 16 {
		return f, errors.New("wav: fmt chunk too small")
	}
	f.AudioFormat = binary.LittleEndian.Uint16(b[0:2])
	f.Channels = binary.LittleEndian.Uint16(b[2:4])
	f.SampleRate = binary.LittleEndian.Uint32(b[4:8])
	f.BitsPerSample = binary.LittleEndian.Uint16(b[14:16])

	if f.AudioFormat == fmtExtensible && len(b) >= 40 {
		// WAVEFORMATEXTENSIBLE: the real format is the first two bytes of
		// the sub-format GUID at offset 24.
		switch binary.LittleEndian.Uint16(b[24:26]) {
		case fmtPCM:
			f.AudioFormat = fmtPCM
		case fmtIEEEFloat:
			f.AudioFormat = fmtIEEEFloat
		default:
			return f, fmt.Errorf("wav: unsupported extensible subformat %d", b[24])
		}
	}
	if f.AudioFormat != fmtPCM && f.AudioFormat != fmtIEEEFloat {
		return f, fmt.Errorf("wav: unsupported audio format %d (only PCM and IEEE float)", f.AudioFormat)
	}
	return f, nil
}

func decodePCM(b []byte, bits int) float32 {
	switch bits {
	case 8: // WAV 8-bit PCM is unsigned
		return (float32(b[0]) - 128.0) / 128.0
	case 16:
		v := int16(binary.LittleEndian.Uint16(b))
		return float32(v) / 32768.0
	case 24:
		v := int32(b[0]) | int32(b[1])<<8 | int32(b[2])<<16
		if v&0x800000 != 0 {
			v |= ^0xffffff // sign extend
		}
		return float32(v) / 8388608.0
	case 32:
		v := int32(binary.LittleEndian.Uint32(b))
		return float32(v) / 2147483648.0
	default:
		return 0
	}
}

func decodeFloat(b []byte) float32 {
	switch len(b) {
	case 4:
		bits := binary.LittleEndian.Uint32(b)
		return mathFloat32frombits(bits)
	case 8:
		return float32(mathFloat64frombits(binary.LittleEndian.Uint64(b)))
	default:
		return 0
	}
}
