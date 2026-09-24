package noisego

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

// WAV 文件要求：PCM 格式（format tag 1）、16-bit、单声道。
const (
	wavFormatPCM    = 1
	bitsPerSample16 = 16
)

// WaveData 保存解码后的 WAV 信息与采样数据（float32，范围 [-1,1]）。
type WaveData struct {
	SampleRate uint32
	Samples    []float32
}

// ReadWAV 读取 16-bit 单声道 PCM WAV 文件。
func ReadWAV(r io.Reader) (*WaveData, error) {
	var riff [12]byte
	if _, err := io.ReadFull(r, riff[:]); err != nil {
		return nil, fmt.Errorf("读取 WAV 头部失败: %w", err)
	}
	if string(riff[0:4]) != "RIFF" || string(riff[8:12]) != "WAVE" {
		return nil, errors.New("不是合法的 RIFF/WAVE 文件")
	}

	var (
		audioFormat uint16
		channels    uint16
		sampleRate  uint32
		bits        uint16
		pcm         []byte
	)

	for {
		var hdr [8]byte
		if _, err := io.ReadFull(r, hdr[:]); err != nil {
			if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
				break
			}
			return nil, err
		}
		id := string(hdr[0:4])
		size := binary.LittleEndian.Uint32(hdr[4:8])

		body := make([]byte, size)
		if _, err := io.ReadFull(r, body); err != nil {
			return nil, fmt.Errorf("读取 chunk %q 失败: %w", id, err)
		}
		// chunk 按字对齐，奇数长度带 1 字节填充。
		if size%2 == 1 {
			if _, err := r.Read(make([]byte, 1)); err != nil {
				return nil, err
			}
		}

		switch id {
		case "fmt ":
			if size < 16 {
				return nil, errors.New("fmt chunk 长度异常")
			}
			audioFormat = binary.LittleEndian.Uint16(body[0:2])
			channels = binary.LittleEndian.Uint16(body[2:4])
			sampleRate = binary.LittleEndian.Uint32(body[4:8])
			bits = binary.LittleEndian.Uint16(body[14:16])
		case "data":
			pcm = body
		}
	}

	if audioFormat != wavFormatPCM {
		return nil, fmt.Errorf("仅支持 PCM(format=1) WAV，实际 format=%d", audioFormat)
	}
	if channels != 1 {
		return nil, fmt.Errorf("仅支持单声道 WAV，实际声道数=%d（请先用 ffmpeg 转换: -ac 1）", channels)
	}
	if bits != bitsPerSample16 {
		return nil, fmt.Errorf("仅支持 16-bit WAV，实际位深=%d", bits)
	}
	if sampleRate == 0 || pcm == nil {
		return nil, errors.New("WAV 缺少 fmt 或 data chunk")
	}
	if len(pcm)%2 != 0 {
		pcm = pcm[:len(pcm)-1]
	}

	return &WaveData{
		SampleRate: sampleRate,
		Samples:    pcm16ToFloat32(pcm),
	}, nil
}

// WriteWAV 将 16-bit 单声道 PCM 采样写入 w。
func WriteWAV(w io.Writer, data *WaveData) error {
	pcm := float32ToPCM16(data.Samples)
	dataSize := uint32(len(pcm))
	byteRate := data.SampleRate * 1 * (bitsPerSample16 / 8)

	var buf bytes.Buffer
	buf.WriteString("RIFF")
	_ = binary.Write(&buf, binary.LittleEndian, uint32(36+dataSize))
	buf.WriteString("WAVE")

	buf.WriteString("fmt ")
	_ = binary.Write(&buf, binary.LittleEndian, uint32(16))
	_ = binary.Write(&buf, binary.LittleEndian, uint16(wavFormatPCM))
	_ = binary.Write(&buf, binary.LittleEndian, uint16(1))
	_ = binary.Write(&buf, binary.LittleEndian, data.SampleRate)
	_ = binary.Write(&buf, binary.LittleEndian, byteRate)
	_ = binary.Write(&buf, binary.LittleEndian, uint16(1*(bitsPerSample16/8)))
	_ = binary.Write(&buf, binary.LittleEndian, uint16(bitsPerSample16))

	buf.WriteString("data")
	_ = binary.Write(&buf, binary.LittleEndian, dataSize)
	buf.Write(pcm)

	_, err := w.Write(buf.Bytes())
	return err
}
