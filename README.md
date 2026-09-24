# go-rnnoise

纯 Go（**CGO_ENABLED=0**）的 [RNNoise](https://github.com/xiph/rnnoise) 语音降噪库绑定。
通过 [purego](https://github.com/ebitengine/purego) 在运行时加载动态库，不依赖 cgo 工具链，
可直接交叉编译到三大桌面平台：

| 平台    | 动态库文件              |
|---------|-------------------------|
| Linux   | `librnnoise.so`         |
| macOS   | `librnnoise.dylib`      |
| Windows | `rnnoise.dll`           |

## 功能

- 加载/卸载 RNNoise 动态库（`rnnoise_create` / `rnnoise_destroy` …）
- 流式处理 48 kHz、单声道、float32 / int16 PCM（`rnnoise_process_frame`）
- 每帧进度回调（已处理样本数、总数、VAD 概率；可提前中止）
- 资源生命周期管理：上下文、库句柄都可释放，重复关闭返回 `ErrClosed`
- 库路径可配置；加载失败时汇总**所有尝试过的路径 + 系统错误 + 修复建议**
- 自带 `wav` 包（PCM/浮点、8/16/24/32-bit、多声道降混、线性重采样）
- 命令行示例 `rnnoise-cli`：输入 WAV → 输出降噪后的 WAV

## 快速开始

### 1. 准备动态库

本仓库不内置二进制，提供脚本从官方源码一键编译（需要 `gcc`/`clang`）：

```bash
make lib                    # 本机平台 → third_party/librnnoise.{so,dylib}
make lib-windows            # 交叉编译 DLL（需要 x86_64-w64-mingw32-gcc）
make lib-macos              # 在 macOS 上编译 dylib
# 或直接：./third_party/build.sh [native|linux|macos|macos-universal|windows]
```

也可以使用包管理器安装 RNNoise，然后让程序走系统搜索路径。

### 2. 命令行处理 WAV

```bash
make demo
bin/rnnoise-cli -input noisy.wav -output clean.wav
# 指定库路径：
bin/rnnoise-cli -input in.wav -output out.wav -lib /opt/rnnoise/librnnoise.so
# 或用环境变量： RNNOISE_LIB=/path/to/lib bin/rnnoise-cli -input a.wav -output b.wav
```

输入可以是任意采样率、任意声道数的 PCM/IEEE-float WAV，会自动降混成单声道
并重采样到 48 kHz；输出固定为 48 kHz / 单声道 / 16-bit PCM。

```
$ rnnoise-cli -input noisy.wav -output clean.wav
input : noisy.wav (16000 Hz, 2 channel(s), 3.00s)
prep  : downmixed to mono and resampled to 48000 Hz
library loaded: third_party/librnnoise.so
[##############################] 100%  0s elapsed
denoise: 144000 samples processed in 12ms
output: clean.wav (48000 Hz, mono, 16-bit PCM)
```

### 3. 作为库使用

```go
import "github.com/solomanager/go-rnnoise/rnnoise"

// 加载 + 创建上下文（Close 时一起释放）
den, err := rnnoise.New(rnnoise.WithLibraryPath("/path/to/librnnoise.so"))
if err != nil {
    log.Fatal(err)
}
defer den.Close()

// 48kHz 单声道 PCM；progress 每 480 个样本（10ms）回调一次
out, err := den.Process(pcm, func(done, total int, vad float32) bool {
    fmt.Printf("\r%5.1f%% VAD=%.2f", 100*float64(done)/float64(total), vad)
    return false // 返回 true 可中止处理
})
```

需要多个并发流时，只加载一次库、创建多个上下文：

```go
lib, _ := rnnoise.Open(rnnoise.WithLibraryPath(path))
defer lib.Close()
d1, _ := lib.NewContext()   // 每个 goroutine 一个 Denoiser
d2, _ := lib.NewContext()
defer d1.Close()
defer d2.Close()
```

其他 API：

- `Denoiser.ProcessFrame(in [480]float32) (out, vadProb, err)`：单帧处理
- `Denoiser.ProcessInt16(in []int16, progress)`：直接处理 WAV 常见的 int16 PCM
- `Library.FrameSize()`：读取库实际声明的帧长（所有已知版本都是 480）

## 库的搜索规则

未显式指定路径时，依次尝试（去重后）：

1. `./third_party/`、`./lib/` 下按平台命名的文件
2. `/usr/local/lib`、`/usr/lib`、`~/.local/lib`
3. macOS 额外尝试 `/opt/homebrew/lib`；Windows 额外尝试 `%ProgramFiles%\rnnoise\bin`
4. 最后把裸文件名（`librnnoise.so` / `librnnoise.dylib` / `rnnoise.dll`）
   交给系统加载器（`LD_LIBRARY_PATH`、`DYLD_LIBRARY_PATH`、`PATH`、ldconfig 缓存）

指定方式：`WithLibraryPath(path)` 选项、CLI 的 `-lib` 参数或 `RNNOISE_LIB` 环境变量。

加载失败时返回 `*rnnoise.LoadError`，示例：

```
error: rnnoise: failed to load library "/nonexistent/libfoo.so":
  tried /nonexistent/libfoo.so: ...cannot open shared object file: No such file or directory: file does not exist
hint: install RNNoise (...), set LD_LIBRARY_PATH, or pass the full path via WithLibraryPath
```

## 目录结构

```
rnnoise/            核心绑定（生命周期、PCM 处理、进度回调、加载器）
  rnnoise.go        Library / Denoiser / Process / 选项与错误
  loader.go         跨平台搜索路径与 LoadError
  lib_unix.go       dlopen/dlsym/dlclose（Linux/macOS/*BSD，purego）
  lib_windows.go    LoadLibrary/GetProcAddress/FreeLibrary
wav/                零依赖 WAV 读写 + 降混 + 重采样
cmd/rnnoise-cli/    命令行示例
examples/simple/    库 API 最小示例
third_party/        动态库构建脚本与产物目录（产物不入库）
```

## 注意事项

- RNNoise 只接受 **48 kHz 单声道**，帧长固定 480 样本（10 ms）；`wav` 包提供重采样辅助。
- `Denoiser` 内部串行加锁：可并发调用但会互相阻塞；高并发请为每条音频流各建一个实例。
- RNNoise 输出长度与输入一致；最后不足一帧时内部补零处理，输出自动裁回原长度。
- 本项目只在运行时动态链接 RNNoise（BSD 许可），不复制其代码；`third_party/rnnoise/`
  仅是本地编译用的源码检出，不随仓库分发。

## 测试

```bash
make lib     # 先编译 third_party/librnnoise.so
make test
```

找不到动态库时集成测试会自动 skip；加载失败说明、WAV 编解码等测试无需原生库。

## License

Go 绑定代码：MIT。RNNoise 本体遵循其 [BSD 许可证](https://github.com/xiph/rnnoise/blob/master/COPYING)。
