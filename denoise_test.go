package noisego

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func mockLibPath(t *testing.T) string {
	t.Helper()
	// 由 testdata/mock_rnnoise.c 编译：见 testdata/build_mock.sh
	names := map[string]string{
		"linux":   "librnnoise_mock.so",
		"darwin":  "librnnoise_mock.dylib",
		"windows": "rnnoise_mock.dll",
	}
	name, ok := names[osShorthand()]
	if !ok {
		t.Skipf("平台 %s 无 mock 库", osShorthand())
	}
	p := filepath.Join("testdata", name)
	if _, err := os.Stat(p); err != nil {
		t.Skipf("mock 库不存在（%s），跳过", p)
	}
	return p
}

func TestLoadErrorExplainsReason(t *testing.T) {
	_, err := Load(&LoadOptions{Path: "/definitely/not/here/librnnoise.so"})
	if err == nil {
		t.Fatal("期望加载失败")
	}
	var le *LoadError
	if !errors.As(err, &le) {
		t.Fatalf("期望 *LoadError，得到 %T", err)
	}
	msg := err.Error()
	for _, want := range []string{"/definitely/not/here/librnnoise.so", "已尝试", "解决办法", "RNNOISE_LIBRARY"} {
		if !strings.Contains(msg, want) {
			t.Errorf("错误信息缺少 %q\n完整信息:\n%s", want, msg)
		}
	}
}

func TestLoadAndDenoise(t *testing.T) {
	lib, err := Load(&LoadOptions{Path: mockLibPath(t)})
	if err != nil {
		t.Skipf("无法加载 mock 库: %v", err)
	}
	defer func() { _ = lib.Close() }()

	if lib.Path() == "" {
		t.Fatal("Path 为空")
	}

	dn, err := lib.NewDenoiser()
	if err != nil {
		t.Fatalf("NewDenoiser: %v", err)
	}

	in := make([]float32, FrameSize)
	in[0] = 0.8
	out := make([]float32, FrameSize)
	vad, err := dn.ProcessFrame(out, in)
	if err != nil {
		t.Fatalf("ProcessFrame: %v", err)
	}
	if out[0] < 0.39 || out[0] > 0.41 {
		t.Errorf("mock 应将 0.8 减半到约 0.4，得到 %v", out[0])
	}
	if vad < 0 || vad > 1 {
		t.Errorf("VAD 越界: %v", vad)
	}

	// 长度非法
	if _, err := dn.ProcessFrame(make([]float32, 10), in); err == nil {
		t.Error("期望帧长度错误")
	}

	if err := dn.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}
	// Close 可重复调用
	if err := dn.Close(); err != nil {
		t.Errorf("重复 Close: %v", err)
	}
	// 关闭后处理应报错
	if _, err := dn.ProcessFrame(out, in); err == nil {
		t.Error("关闭后 ProcessFrame 应报错")
	}
}

func TestProcessProgressAndCancel(t *testing.T) {
	lib, err := Load(&LoadOptions{Path: mockLibPath(t)})
	if err != nil {
		t.Skipf("mock 库不可用: %v", err)
	}
	defer func() { _ = lib.Close() }()
	dn, _ := lib.NewDenoiser()
	defer func() { _ = dn.Close() }()

	pcm := make([]float32, FrameSize*4+100) // 含非整数倍尾帧
	pcm[0] = 0.6

	var frames []int
	err = dn.Process(pcm, func(done, total int, vad float32) error {
		frames = append(frames, done)
		if total != 5 {
			t.Errorf("总帧数应为 5，得到 %d", total)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	if len(frames) != 5 || frames[0] != 1 || frames[4] != 5 {
		t.Errorf("进度回调异常: %v", frames)
	}
	if pcm[0] < 0.29 || pcm[0] > 0.31 {
		t.Errorf("首采样处理结果异常: %v", pcm[0])
	}
	// 输出长度与输入一致（尾帧截断）
	if len(pcm) != FrameSize*4+100 {
		t.Errorf("长度被改变: %d", len(pcm))
	}

	// 取消
	pcm2 := make([]float32, FrameSize*10)
	err = dn.Process(pcm2, func(done, total int, vad float32) error {
		if done >= 3 {
			return ErrCanceled
		}
		return nil
	})
	if !errors.Is(err, ErrCanceled) {
		t.Fatalf("期望 ErrCanceled，得到 %v", err)
	}
}

func TestPCMConversionsRoundTrip(t *testing.T) {
	samples := []float32{-1, -0.5, 0, 0.5, 1}
	b := float32ToPCM16(samples)
	back := pcm16ToFloat32(b)
	if len(back) != len(samples) {
		t.Fatalf("长度不一致: %d", len(back))
	}
	for i := range samples {
		if d := back[i] - samples[i]; d > 0.001 || d < -0.001 {
			t.Errorf("第 %d 个采样往返误差过大: %v vs %v", i, samples[i], back[i])
		}
	}
}
