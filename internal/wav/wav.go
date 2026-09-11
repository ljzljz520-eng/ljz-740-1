// Package wav is a minimal RIFF/WAVE reader and writer for the formats the
// denoiser CLI supports: 16-bit PCM and 32-bit IEEE float, mono or stereo.
package wav

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
)

// Format identifies the sample encoding.
type Format uint16

const (
	FormatPCM       Format = 1
	FormatIEEEFloat Format = 3
)

// File describes a decoded WAVE header and holds the interleaved samples.
type File struct {
	SampleRate uint32
	Channels   uint16
	Bits       uint16
	Format     Format
	// S16 is populated for 16-bit PCM, F32 for 32-bit float.
	S16 []int16
	F32 []float32
}

// FrameSize returns samples per channel.
func (f *File) FrameSize() int {
	if f.Channels == 0 {
		return 0
	}
	return f.NumSamples() / int(f.Channels)
}

// NumSamples returns the interleaved sample count.
func (f *File) NumSamples() int {
	if f.Format == FormatIEEEFloat {
		return len(f.F32)
	}
	return len(f.S16)
}

var (
	errNotWAV = errors.New("wav: not a RIFF/WAVE file")
	errShort  = errors.New("wav: unexpected end of file")
)

// Decode reads a whole WAVE stream into memory.
func Decode(r io.Reader) (*File, error) {
	br := &reader{r: r}

	riff, _ := br.ident()
	if riff != "RIFF" {
		return nil, errNotWAV
	}
	br.u32() // file size, unreliable for streaming reads
	wave, _ := br.ident()
	if wave != "WAVE" {
		return nil, errNotWAV
	}

	f := &File{}
	var data []byte
	dataSeen := false
	for {
		id, err := br.ident()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		size := br.u32()
		body := make([]byte, size)
		if _, err := io.ReadFull(br.r, body); err != nil {
			return nil, fmt.Errorf("wav: reading %q chunk: %w", id, err)
		}
		if size%2 == 1 { // chunks are word aligned
			if err := br.skip(1); err != nil {
				return nil, err
			}
		}

		switch id {
		case "fmt ":
			if size < 16 {
				return nil, fmt.Errorf("wav: fmt chunk too short (%d bytes)", size)
			}
			f.Format = Format(binary.LittleEndian.Uint16(body[0:2]))
			f.Channels = binary.LittleEndian.Uint16(body[2:4])
			f.SampleRate = binary.LittleEndian.Uint32(body[4:8])
			f.Bits = binary.LittleEndian.Uint16(body[14:16])
		case "data":
			data = body
			dataSeen = true
		default:
			// LIST, fact, bext, ... ignored
		}
	}
	if !dataSeen {
		return nil, errors.New("wav: no data chunk")
	}
	if f.Channels == 0 || f.SampleRate == 0 {
		return nil, errors.New("wav: missing fmt chunk")
	}
	if f.Channels != 1 && f.Channels != 2 {
		return nil, fmt.Errorf("wav: unsupported channel count %d (only mono/stereo)", f.Channels)
	}

	switch {
	case f.Format == FormatPCM && f.Bits == 16:
		if len(data)%2 != 0 {
			return nil, errors.New("wav: truncated 16-bit data")
		}
		f.S16 = make([]int16, len(data)/2)
		for i := range f.S16 {
			f.S16[i] = int16(binary.LittleEndian.Uint16(data[i*2:]))
		}
	case f.Format == FormatIEEEFloat && f.Bits == 32:
		if len(data)%4 != 0 {
			return nil, errors.New("wav: truncated float data")
		}
		f.F32 = make([]float32, len(data)/4)
		for i := range f.F32 {
			bits := binary.LittleEndian.Uint32(data[i*4:])
			f.F32[i] = math.Float32frombits(bits)
		}
	default:
		return nil, fmt.Errorf("wav: unsupported format: tag=%d bits=%d (need PCM/16 or FLOAT/32)",
			f.Format, f.Bits)
	}
	return f, nil
}

// Encode writes f as a complete RIFF/WAVE stream (header sizes are patched on
// Close). Always call Close after all samples are written.
type Writer struct {
	w          io.WriteSeeker
	format     Format
	channels   uint16
	sampleRate uint32
	bits       uint16
	dataBytes  uint32
	closed     bool
}

// NewWriter creates a WAVE writer. format must be FormatPCM (16-bit) or
// FormatIEEEFloat (32-bit).
func NewWriter(w io.WriteSeeker, sampleRate uint32, channels uint16, format Format) (*Writer, error) {
	if channels != 1 && channels != 2 {
		return nil, fmt.Errorf("wav: unsupported channel count %d", channels)
	}
	bits := uint16(16)
	if format == FormatIEEEFloat {
		bits = 32
	} else if format != FormatPCM {
		return nil, fmt.Errorf("wav: unsupported encode format %d", format)
	}
	ww := &Writer{w: w, format: format, channels: channels, sampleRate: sampleRate, bits: bits}
	if err := ww.writeHeader(); err != nil {
		return nil, err
	}
	return ww, nil
}

func (w *Writer) writeHeader() error {
	buf := make([]byte, 44)
	copy(buf[0:4], "RIFF")
	binary.LittleEndian.PutUint32(buf[4:8], 0) // patched
	copy(buf[8:12], "WAVE")
	copy(buf[12:16], "fmt ")
	binary.LittleEndian.PutUint32(buf[16:20], 16)
	tag := uint16(FormatPCM)
	if w.format == FormatIEEEFloat {
		tag = uint16(FormatIEEEFloat)
	}
	binary.LittleEndian.PutUint16(buf[20:22], tag)
	binary.LittleEndian.PutUint16(buf[22:24], w.channels)
	binary.LittleEndian.PutUint32(buf[24:28], w.sampleRate)
	blockAlign := w.channels * (w.bits / 8)
	binary.LittleEndian.PutUint32(buf[28:32], w.sampleRate*uint32(blockAlign))
	binary.LittleEndian.PutUint16(buf[32:34], blockAlign)
	binary.LittleEndian.PutUint16(buf[34:36], w.bits)
	copy(buf[36:40], "data")
	binary.LittleEndian.PutUint32(buf[40:44], 0) // patched
	_, err := w.w.Write(buf)
	return err
}

// WriteS16 writes interleaved 16-bit samples to a PCM writer.
func (w *Writer) WriteS16(s []int16) error {
	if w.format != FormatPCM {
		return errors.New("wav: writer is not 16-bit PCM")
	}
	buf := make([]byte, len(s)*2)
	for i, v := range s {
		binary.LittleEndian.PutUint16(buf[i*2:], uint16(v))
	}
	if _, err := w.w.Write(buf); err != nil {
		return err
	}
	w.dataBytes += uint32(len(buf))
	return nil
}

// WriteF32 writes interleaved float samples to an IEEE-float writer.
func (w *Writer) WriteF32(s []float32) error {
	if w.format != FormatIEEEFloat {
		return errors.New("wav: writer is not 32-bit float")
	}
	buf := make([]byte, len(s)*4)
	for i, v := range s {
		binary.LittleEndian.PutUint32(buf[i*4:], math.Float32bits(v))
	}
	if _, err := w.w.Write(buf); err != nil {
		return err
	}
	w.dataBytes += uint32(len(buf))
	return nil
}

// Close patches RIFF/data sizes. Calling Close more than once is a no-op.
func (w *Writer) Close() error {
	if w.closed {
		return nil
	}
	w.closed = true
	riffSize := uint32(36) + w.dataBytes
	patch := func(off uint32, v uint32) error {
		if _, err := w.w.Seek(int64(off), io.SeekStart); err != nil {
			return err
		}
		var b [4]byte
		binary.LittleEndian.PutUint32(b[:], v)
		_, err := w.w.Write(b[:])
		return err
	}
	if err := patch(4, riffSize); err != nil {
		return err
	}
	return patch(40, w.dataBytes)
}

type reader struct{ r io.Reader }

func (r *reader) ident() (string, error) {
	var b [4]byte
	if _, err := io.ReadFull(r.r, b[:]); err != nil {
		return "", err
	}
	return string(b[:]), nil
}

func (r *reader) u32() uint32 {
	var b [4]byte
	_, _ = io.ReadFull(r.r, b[:])
	return binary.LittleEndian.Uint32(b[:])
}

func (r *reader) skip(n int) error {
	if seeker, ok := r.r.(io.Seeker); ok {
		_, err := seeker.Seek(int64(n), io.SeekCurrent)
		return err
	}
	_, err := io.CopyN(io.Discard, r.r, int64(n))
	return err
}
