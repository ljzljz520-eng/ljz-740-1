# govdnoise — Go 语音降噪库绑定（purego，无 cgo）

从 Go 侧通过 [purego](https://github.com/ebitengine/purego) 在运行时加载
三平台（Linux / macOS / Windows）的降噪动态库，封装：

- **创建/释放上下文**：`Library.NewContext` / `Context.Close`
- **处理 PCM**：16-bit PCM 与 32-bit float，单声道/交错双声道，按帧处理
- **进度回调**：native 线程直接回调 Go 函数（0–100%），不使用 cgo
- **库路径可配置**：`-lib` / `VDNOISE_LIB` / `WithPath(...)`，附带多级默认搜索
- **加载失败说明原因**：逐一列出尝试过的路径及操作系统返回的错误

仓库自带一个零依赖的参考降噪实现（`native/`），定义稳定 C ABI；真实项目可把
同一套 ABI 对到 RNNoise 等引擎上，Go 侧无需改动。

## 目录结构

```
native/
  vdnoise.h            # 稳定 C ABI（vd_open/vd_process_*/vd_close ...）
  vdnoise.c            # 参考实现：DC 阻断 + 自适应噪声底 + 软维纳门限
  Makefile             # linux / darwin / windows 三个目标
vdnoise/               # purego 绑定（核心包）
  vdnoise.go           # 加载、符号解析、ABI 校验、Library
  context.go           # Context：NewContext/Process/Finish/Close
  callback_supported.go# purego.NewCallback 回调桥（按平台）
  dl_unix.go           # dlopen: linux/darwin（purego）
  dl_windows.go        # LoadLibrary: windows（syscall，纯 Go）
  dl_other.go          # 其它平台：返回 ErrUnsupportedPlatform，保证可编译
internal/wav/          # 最小 WAV 读写（PCM16 / FLOAT32，mono/stereo）
cmd/vdnoise/           # 命令行示例：输入 wav -> 输出 wav
```

## 快速开始

```sh
# 1) 构建当前平台的动态库与命令行
make           # -> native/lib/libvdnoise.so（或 .dylib）
               #    build/vdnoise

# 2) 处理一个 wav
./build/vdnoise -in noisy.wav -out clean.wav -frame 480
```

输出：

```
[##############----------------]  46%
done: noisy.wav -> clean.wav (s16, 48000 Hz, 2 ch, frame=480, native=vdnoise-ref 1.0.0)
```

### 命令行参数

| 参数       | 说明                                                           |
|------------|----------------------------------------------------------------|
| `-in`      | 输入 WAV（PCM16 / FLOAT32，单声道/双声道）                     |
| `-out`     | 输出 WAV（编码格式与输入一致）                                 |
| `-lib`     | 动态库路径，可传文件或目录；默认取 `VDNOISE_LIB`               |
| `-frame`   | 每帧每声道采样数，1..8192，默认 480（10ms @48k）               |
| `-quiet`   | 不显示进度条                                                   |
| `-version` | 打印绑定与 native 库版本                                       |

最后不足一帧的采样会用静音补齐处理，但只写入实际长度。

## 三平台构建动态库

```sh
make -C native linux      # gcc  -> native/lib/libvdnoise.so
make -C native darwin     # clang -> native/lib/libvdnoise.dylib
make -C native windows    # 需要 x86_64-w64-mingw32-gcc -> native/lib/vdnoise.dll
make -C native all        # 三个目标
```

交叉编译器可用 `make -C native darwin CC=o64-clang` 覆盖。Go 程序本身用标准
命令交叉编译即可：

```sh
GOOS=windows GOARCH=amd64 go build -o vdnoise.exe ./cmd/vdnoise
```

库文件名与平台对应关系：

| GOOS      | 文件名               |
|-----------|----------------------|
| linux     | `libvdnoise.so`      |
| darwin    | `libvdnoise.dylib`   |
| windows   | `vdnoise.dll`        |

## 库路径搜索与错误说明

显式路径（`WithPath` / `-lib` / `VDNOISE_LIB`）具有最高优先级且**只尝试这些
位置**——指定了却加载不到会直接报错，不会静默改用别的库。未显式指定时依次
搜索：

1. 可执行文件同目录及上级 4 层内的 `native/lib/`（方便 `go run` / 打包分发）
2. 当前工作目录的 `native/lib/`、`lib/`
3. 裸库名，交给操作系统加载器（`LD_LIBRARY_PATH`、`DYLD_LIBRARY_PATH`、
   `PATH`、`/usr/local/lib` 等）

加载失败时返回 `*vdnoise.LoadError`，逐个列出尝试路径和原因，例如：

```
vdnoise: failed to load the native denoiser library, attempted locations:
  - /opt/lib/libvdnoise.so
      /opt/lib/libvdnoise.so: cannot open shared object file: No such file or directory
  - libvdnoise.so
      libvdnoise.so: wrong ELF class: ELFCLASS32
fix: pass -lib / use WithPath, set VDNOISE_LIB, or run "make -C native"
```

另外两类校验失败会单独报错：缺少导出符号（`required symbol not exported:
vd_process_f32`）和 ABI 主版本不匹配（`incompatible ABI: library reports
99.0, binding requires 1.x`）。

## 作为库使用

```go
import "govdnoise/vdnoise"

lib, err := vdnoise.Open(vdnoise.WithPath("/opt/libvdnoise.so"))
if err != nil {
    var le *vdnoise.LoadError
    if errors.As(err, &le) { log.Fatalf("无法加载降噪库:\n%v", le) }
    log.Fatal(err)
}
defer lib.Close()

ctx, err := lib.NewContext(vdnoise.ContextConfig{
    SampleRate:  48000,
    Channels:    1,                      // 或 2（交错）
    FrameSize:   480,                    // 每次 Process 的每声道采样数
    Format:      vdnoise.FormatS16,      // 或 vdnoise.FormatF32
    TotalFrames: 1000,                   // >0 时进度为精确百分比；0 表示未知
    Progress: func(pct int) { fmt.Printf("\r%d%%", pct) },
})
if err != nil { log.Fatal(err) }
defer ctx.Close()

in  := make([]int16, 480)
out := make([]int16, 480)
if _, err := ctx.Process(in, out); err != nil { // 支持同切片原地处理
    log.Fatal(err)
}
if err := ctx.Finish(); err != nil { log.Fatal(err) } // 保证收到最后一次 100%
```

注意：

- `Context` 是有状态引擎，**不能**被多个 goroutine 并发使用；`Library` 可创建
  多个独立 Context。
- `Process` 的输入输出切片长度必须 ≥ `FrameSize()=帧长×声道数`。
- 回调在 native 调用线程上同步执行，应快速返回，且不要在回调里再次调用同一
  Context。

## C ABI（供替换底层引擎）

见 [`native/vdnoise.h`](native/vdnoise.h)，核心函数：

| 符号               | 作用                                             |
|--------------------|--------------------------------------------------|
| `vd_abi_version`   | `(major<<16)\|minor`，主版本必须为 1             |
| `vd_version`       | 返回静态 UTF-8 版本字符串                        |
| `vd_open`          | 建上下文（采样率/声道/帧长/进度回调/user 指针）  |
| `vd_set_total`     | 声明总帧数（可选，用于精确进度）                 |
| `vd_process_s16`   | 处理一帧交错 int16                               |
| `vd_process_f32`   | 处理一帧交错 float                               |
| `vd_finish`        | 结束并发出最终 100% 进度                         |
| `vd_close`         | 释放上下文                                       |

## 测试

```sh
make test        # 先构建 native 库，再以 VDNOISE_LIB 指向它运行全部测试
make test-unit   # 不依赖动态库（集成测试自动 t.Skip）
```

## 依赖

- Go ≥ 1.25（purego v0.11 要求）；构建/使用均**不需要 cgo**（CGO_ENABLED=0 可用）
- 构建参考动态库：Linux 用 gcc，macOS 用 clang，Windows 交叉用 mingw-w64
