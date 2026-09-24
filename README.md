# noisego

基于 [purego](https://github.com/ebitengine/purego) 的 [RNNoise](https://jmvalin.ca/demo/rnnoise/) 语音降噪 Go 绑定——**零 CGO**，运行时动态加载三平台共享库（Linux/macOS/Windows）。

## 特性

- **免 CGO**：用 purego 直接 dlopen/LoadLibrary，普通 `go build` 即可交叉编译
- **上下文管理**：创建（`NewDenoiser`）/ 释放（`Close`）RNNoise 状态，带 finalizer 兜底
- **处理 PCM**：单帧（`ProcessFrame`）或整段（`Process`）48kHz/单声道/float32 PCM，尾帧自动零填充
- **进度回调**：每帧回调帧数与 VAD 语音概率，回调返回错误可取消处理
- **库路径可配置**：显式路径 > 环境变量 `RNNOISE_LIBRARY` > 搜索目录 > 系统默认路径
- **加载失败可诊断**：`LoadError` 列出所有尝试过的路径、来源及解决办法
- **WAV 示例 CLI**：读入 16-bit 单声道 WAV（非 48k 自动线性重采样），输出降噪后新文件

## 快速开始

### 1. 准备动态库

见 [`third_party/rnnoise/README.md`](third_party/rnnoise/README.md)。
编译好后可通过环境变量指定：

```bash
export RNNOISE_LIBRARY=/path/to/librnnoise.so   # Linux
# export RNNOISE_LIBRARY=/path/to/librnnoise.dylib  # macOS
# set RNNOISE_LIBRARY=C:\path\to\rnnoise.dll       # Windows
```

### 2. 作为库使用

```go
package main

import (
    "fmt"

    "github.com/example/noisego"
)

func main() {
    // 加载动态库（路径可配置）
    lib, err := noisego.Load(&noisego.LoadOptions{
        // Path: "/opt/lib/librnnoise.so",  // 不填则按环境变量/系统路径查找
        // SearchPaths: []string{"./lib"},
    })
    if err != nil {
        panic(err) // *LoadError 含详细原因与已尝试路径
    }
    defer lib.Close()

    // 创建降噪上下文
    dn, err := lib.NewDenoiser()
    if err != nil {
        panic(err)
    }
    defer dn.Close()

    // 处理整段 48kHz 单声道 float32 PCM（尾帧自动零填充）
    pcm := make([]float32, 480*100) // 1 秒
    err = dn.Process(pcm, func(done, total int, vadProb float32) error {
        fmt.Printf("\r进度 %d/%d，语音概率 %.2f", done, total, vadProb)
        return nil // 返回 noisego.ErrCanceled 可中止
    })
}
```

### 3. 命令行示例

```bash
go run ./example/denoise-wav -i noisy.wav -o clean.wav
go run ./example/denoise-wav -i in.wav -o out.wav -lib /opt/lib/librnnoise.so
go run ./example/denoise-wav -i in.wav -o out.wav -libdir ./third_party/lib -vad
```

输入要求：**16-bit、单声道、PCM WAV**（多声道/其他位深请先用
`ffmpeg -ac 1 -c:a pcm_s16le` 转换；采样率任意，CLI 会自动与 48kHz 互转）。

## API 概览

| 符号 | 说明 |
|------|------|
| `Load(opts *LoadOptions) (*Library, error)` | 按优先级查找并加载动态库 |
| `(*Library).NewDenoiser() (*Denoiser, error)` | 创建降噪上下文（`rnnoise_create`） |
| `(*Denoiser).ProcessFrame(out, in []float32) (vad float32, err error)` | 处理一帧 480 采样 |
| `(*Denoiser).Process(pcm []float32, cb ProgressFunc) error` | 处理整段 PCM，带进度回调/取消 |
| `(*Denoiser).Close() error` | 释放上下文（`rnnoise_destroy`），可重复调用 |
| `ReadWAV` / `WriteWAV` | 16-bit 单声道 WAV 读写 |
| `ResampleLinear` | 轻量线性重采样 |
| `ErrCanceled` | 在进度回调中返回以取消处理 |

## RNNoise C ABI 映射

| C 函数 | 绑定 |
|--------|------|
| `void *rnnoise_create(void *model)` | 使用内置模型（`NULL`） |
| `int rnnoise_get_frame_size()` | 恒为 480（库侧校验） |
| `float rnnoise_process_frame(void*, float* out, const float* in)` | 480 float32/帧，返回 VAD |
| `void rnnoise_destroy(void*)` | 释放上下文 |

## 实现注意事项（purego 踩坑记录）

`purego.RegisterFunc` 生成的调用桩走 reflect 回调路径（非带
`//go:uintptrescapes` 的 `SyscallN`），经 `runtime.cgocall` 切到 g0 栈执行
C 函数。**传给 C 的输入/输出缓冲不能是 Go 栈数组**——跨栈调用后地址可能失效，
表现为 C 正常写入但 Go 读回全 0（在 arm64/linux 上首个调用路径稳定复现，
增加任何额外调用或栈分配又会改变现象，极易误判）。本库因此把每帧缓冲放在
`Denoiser` 持有的堆数组（`inBuf`/`outBuf`）中，并在 C 调用后
`runtime.KeepAlive`，调用方传入的切片只用于复制结果。

## 测试

单元/端到端测试使用一个模拟 RNNoise ABI 的小型共享库：

```bash
./testdata/build_mock.sh   # 生成 testdata/librnnoise_mock.so
go test ./...
```

## 平台与架构

已验证 `linux/amd64`、`linux/arm64`、`darwin/amd64`、`darwin/arm64`、
`windows/amd64` 的交叉编译（`CGO_ENABLED=0`）。要求 Go 1.21+。

## License

示例与绑定代码可按需使用；RNNoise 本体遵循其上游 BSD 许可证。
