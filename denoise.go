// Package noisego 是 Xiph RNNoise 语音降噪库的纯 Go 绑定（基于 purego，无需 CGO）。
//
// 它在运行时动态加载 RNNoise 共享库（Linux 为 librnnoise.so、
// macOS 为 librnnoise.dylib、Windows 为 rnnoise.dll），封装了降噪上下文的
// 创建、PCM 帧处理、进度回调与资源释放。
package noisego

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"unsafe"
)

// SampleRate 是 RNNoise 要求的输入采样率（Hz）。
const SampleRate = 48000

// FrameSize 是 RNNoise 每次处理的帧数（单声道 float32 采样数，10ms）。
const FrameSize = 480

// 默认动态库文件名（按平台）。
var defaultLibNames = map[string][]string{
	"linux":   {"librnnoise.so", "librnnoise.so.0"},
	"darwin":  {"librnnoise.dylib"},
	"windows": {"rnnoise.dll"},
}

// RNNoise C API 对应的函数签名：
//
//	void* rnnoise_create(void* model);   // model 传 NULL 使用内置模型
//	int   rnnoise_get_frame_size(void);
//	float rnnoise_process_frame(void* st, float* out, const float* in);
//	void  rnnoise_destroy(void* st);
type (
	cCreate  func(model uintptr) uintptr
	cFrameSz func() int32
	cProcess func(st uintptr, out, in uintptr) float32
	cDestroy func(st uintptr)
)

// Library 表示一个已加载的 RNNoise 动态库。
// 同一个 Library 可并发创建多个 Denoiser；库本身只加载一次。
type Library struct {
	path    string
	handle  uintptr
	create  cCreate
	frameSz cFrameSz
	process cProcess
	destroy cDestroy

	closeOnce sync.Once
	closeErr  error
}

// LoadOptions 控制动态库的查找行为。
type LoadOptions struct {
	// Path 显式指定动态库路径。为空时按 SearchPaths、环境变量、系统默认顺序查找。
	Path string
	// SearchPaths 追加的查找目录（仅在 Path 为空时生效）。
	SearchPaths []string
	// EnvVar 读取库路径的环境变量名，默认为 RNNOISE_LIBRARY。
	EnvVar string
}

// Load 加载 RNNoise 动态库。opts 可为 nil 使用默认查找规则。
func Load(opts *LoadOptions) (*Library, error) {
	o := LoadOptions{EnvVar: "RNNOISE_LIBRARY"}
	if opts != nil {
		if opts.EnvVar != "" {
			o.EnvVar = opts.EnvVar
		}
		o.Path = opts.Path
		o.SearchPaths = opts.SearchPaths
	}

	candidates, sources := buildCandidates(o)

	var lastErr error
	var tried []string
	for _, p := range candidates {
		lib, err := tryLoad(p)
		if err == nil {
			return lib, nil
		}
		lastErr = err
		tried = append(tried, p)
	}

	return nil, &LoadError{
		Candidates: tried,
		Sources:    sources,
		GOOS:       runtime.GOOS,
		lastErr:    lastErr,
	}
}

// buildCandidates 按优先级生成候选库路径，并记录每类路径的来源说明。
func buildCandidates(o LoadOptions) (candidates []string, sources []string) {
	names := defaultLibNames[runtime.GOOS]
	add := func(dir string, mustExist bool) {
		for _, n := range names {
			p := n
			if dir != "" {
				p = filepath.Join(dir, n)
			}
			if mustExist {
				if abs, err := filepath.Abs(p); err == nil {
					p = abs
				}
				if _, err := os.Stat(p); err != nil {
					continue
				}
			}
			candidates = append(candidates, p)
		}
	}

	if o.Path != "" {
		candidates = append(candidates, o.Path)
		sources = append(sources, "显式指定的路径 (LoadOptions.Path)")
		return
	}

	if env := os.Getenv(o.EnvVar); env != "" {
		candidates = append(candidates, env)
		sources = append(sources, fmt.Sprintf("环境变量 %s", o.EnvVar))
	}

	for _, dir := range o.SearchPaths {
		add(dir, true)
	}
	if len(o.SearchPaths) > 0 {
		sources = append(sources, "SearchPaths 指定的目录（其中存在的库文件）")
	}

	// 仅文件名：交给系统加载器在默认搜索路径中查找（LD_LIBRARY_PATH、
	// DYLD_LIBRARY_PATH、PATH 及系统库目录）。
	add("", false)
	sources = append(sources, "系统默认库搜索路径（如 LD_LIBRARY_PATH / DYLD_LIBRARY_PATH / PATH、/usr/lib 等）")

	return
}

func tryLoad(path string) (*Library, error) {
	handle, err := openDynamicLib(path)
	if err != nil {
		return nil, err
	}

	lib := &Library{path: path, handle: handle}

	// 按固定顺序绑定，符号与结构体字段一一对应，行为确定、便于排查。
	symbols := []struct {
		name string
		fptr any
	}{
		{"rnnoise_create", &lib.create},
		{"rnnoise_get_frame_size", &lib.frameSz},
		{"rnnoise_process_frame", &lib.process},
		{"rnnoise_destroy", &lib.destroy},
	}
	for _, s := range symbols {
		addr, lerr := lookupSymbol(handle, s.name)
		if lerr != nil {
			_ = closeDynamicLib(handle)
			return nil, fmt.Errorf("库 %q 缺少符号 %s: %w", path, s.name, lerr)
		}
		bindFunc(s.fptr, addr)
	}
	return lib, nil
}

// Path 返回实际加载的库路径。
func (l *Library) Path() string { return l.path }

// Close 卸载动态库。调用前必须先释放由该库创建的全部 Denoiser。
func (l *Library) Close() error {
	l.closeOnce.Do(func() {
		l.closeErr = closeDynamicLib(l.handle)
	})
	return l.closeErr
}

// NewDenoiser 创建一个降噪上下文（对应 rnnoise_create(NULL)）。
func (l *Library) NewDenoiser() (*Denoiser, error) {
	st := l.create(0)
	if st == 0 {
		return nil, errors.New("rnnoise_create 返回空指针（上下文创建失败）")
	}
	if fs := int(l.frameSz()); fs != FrameSize {
		// 正常情况下恒为 480；这里保留检查，避免库版本不一致导致越界。
		l.destroy(st)
		return nil, fmt.Errorf("动态库报告的帧大小为 %d，本绑定要求 %d", fs, FrameSize)
	}
	d := &Denoiser{lib: l, state: st}
	runtime.SetFinalizer(d, func(x *Denoiser) { _ = x.Close() })
	return d, nil
}

// ProgressFunc 是处理进度回调。
//
// doneFrames/totalFrames 分别为已完成帧数与总帧数；vadProb 为当前帧的
// 语音概率（0~1，RNNoise 返回值）。返回非 nil 错误可中止处理，
// 该错误会原样从 Process 返回（一般用 ErrCanceled）。
type ProgressFunc func(doneFrames, totalFrames int, vadProb float32) error

// ErrCanceled 用于在进度回调中主动取消处理。
var ErrCanceled = errors.New("降噪处理已取消")

// Denoiser 是单个 RNNoise 降噪上下文，非并发安全。
type Denoiser struct {
	lib   *Library
	state uintptr

	mu     sync.Mutex
	closed bool

	// inBuf/outBuf 必须放在由 d 持有的堆对象上，不能使用调用方栈内存：
	// purego 的 RegisterFunc 走 reflect 回调路径（非 SyscallN），没有
	// //go:uintptrescapes 保证；经 cgocall 切到 g0 栈执行 C 函数时，
	// Go 栈数组地址在跨栈调用后可能失效，导致 C 写出的数据读回为 0。
	// 由 Denoiser 持有缓冲并在调用后 KeepAlive，可保证地址与生命周期稳定。
	inBuf  [FrameSize]float32
	outBuf [FrameSize]float32
}

// ProcessFrame 处理一帧 480 个 float32 采样（48kHz、单声道、0~1 VAD）。
//
// 处理结果写入 out，in 不会被修改；out 与 in 可以是同一切片。
// 返回该帧的语音概率（VAD，0~1）。长度不是 FrameSize 时返回错误。
func (d *Denoiser) ProcessFrame(out, in []float32) (float32, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closed {
		return 0, errors.New("denoiser 已关闭")
	}
	if len(in) != FrameSize || len(out) != FrameSize {
		return 0, fmt.Errorf("帧长度必须为 %d，得到 in=%d out=%d", FrameSize, len(in), len(out))
	}
	copy(d.inBuf[:], in)
	vad := d.lib.process(d.state,
		uintptr(unsafe.Pointer(&d.outBuf[0])),
		uintptr(unsafe.Pointer(&d.inBuf[0])),
	)
	// C 调用结束并写回 outBuf 之后才允许 d 被回收。
	runtime.KeepAlive(d)
	copy(out, d.outBuf[:])
	return vad, nil
}

// Process 对整段 PCM 逐帧降噪。输入长度应为 FrameSize 的整数倍；
// 若非整数倍，末尾不足一帧的部分以零填充处理，输出仍截断为输入长度。
//
// pcm 会被原地处理（RNNoise 支持输入输出同址）。progress 可为 nil。
func (d *Denoiser) Process(pcm []float32, progress ProgressFunc) error {
	total := (len(pcm) + FrameSize - 1) / FrameSize
	if total == 0 {
		return nil
	}

	var in, out [FrameSize]float32
	done := 0
	for off := 0; off < len(pcm); off += FrameSize {
		end := min(off+FrameSize, len(pcm))
		clear(in[:])
		copy(in[:], pcm[off:end])

		vad, err := d.ProcessFrame(out[:], in[:])
		if err != nil {
			return err
		}
		// 仅拷回真实存在的采样，丢弃填充部分对应的输出。
		copy(pcm[off:end], out[:end-off])

		done++
		if progress != nil {
			if perr := progress(done, total, vad); perr != nil {
				return perr
			}
		}
	}
	return nil
}

// Close 释放 RNNoise 上下文（对应 rnnoise_destroy）。可重复调用。
func (d *Denoiser) Close() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closed {
		return nil
	}
	d.lib.destroy(d.state)
	d.closed = true
	d.state = 0
	runtime.SetFinalizer(d, nil)
	return nil
}

// LoadError 描述动态库加载失败的详细原因与已尝试的路径。
type LoadError struct {
	Candidates []string // 实际尝试过的路径
	Sources    []string // 路径来源说明
	GOOS       string
	lastErr    error
}

func (e *LoadError) Error() string {
	var b strings.Builder
	b.WriteString("无法加载 RNNoise 动态库：")
	if e.lastErr != nil {
		b.WriteString(e.lastErr.Error())
	}
	b.WriteString("\n已尝试以下路径：\n")
	for _, p := range e.Candidates {
		b.WriteString("  - " + p + "\n")
	}
	b.WriteString("查找来源：" + strings.Join(e.Sources, "；") + "\n")
	b.WriteString("解决办法：\n")
	b.WriteString("  1. 用 LoadOptions.Path 或环境变量 RNNOISE_LIBRARY 指定库文件完整路径；\n")
	b.WriteString("  2. 或将库放入 SearchPaths / 系统库目录（" + defaultHint(e.GOOS) + "）；\n")
	b.WriteString("  3. 参考 third_party/rnnoise 下的说明自行编译对应平台的动态库；\n")
	b.WriteString("  4. 确认库的架构（amd64/arm64）与当前程序一致。")
	return b.String()
}

// Unwrap 支持 errors.Is/As 拿到底层系统错误。
func (e *LoadError) Unwrap() error { return e.lastErr }

func defaultHint(goos string) string {
	switch goos {
	case "darwin":
		return "/usr/local/lib、/opt/homebrew/lib 或设置 DYLD_LIBRARY_PATH"
	case "windows":
		return "程序目录或 PATH 所列目录"
	default:
		return "/usr/lib、/usr/local/lib 或设置 LD_LIBRARY_PATH"
	}
}
