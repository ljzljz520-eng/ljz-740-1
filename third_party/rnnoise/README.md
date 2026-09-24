# 获取 RNNoise 动态库

本绑定本身**不包含** RNNoise 二进制，需要自行准备对应平台的动态库：

| 平台    | 文件名                              |
|---------|-------------------------------------|
| Linux   | `librnnoise.so`（或 `librnnoise.so.0`） |
| macOS   | `librnnoise.dylib`                  |
| Windows | `rnnoise.dll`                       |

## 方式一：系统包管理器

```bash
# Debian/Ubuntu
apt-get install rnnoise-dev
# macOS（Homebrew）
brew install rnnoise
```

## 方式二：从源码编译

先获取源码并生成构建文件（release 包通常已包含生成文件）：

```bash
git clone https://gitlab.xiph.org/xiph/rnnoise.git
cd rnnoise
./autogen.sh && ./configure    # 需要 autotools；release 包可跳过
```

RNNoise 核心源文件很少，可绕过 autotools 直接用 gcc/clang 编译：

```bash
SRCS="src/rnnoise.c src/celt_lpc.c src/pitch.c src/kiss_fft.c src/rnn.c src/denoise.c"

# Linux
cc -O2 -shared -fPIC -o librnnoise.so  $SRCS -Iinclude -Isrc -lm

# macOS
cc -O2 -shared -fPIC -o librnnoise.dylib $SRCS -Iinclude -Isrc -lm

# Windows（MSYS2 / mingw-w64）
gcc -O2 -shared -o rnnoise.dll $SRCS -Iinclude -Isrc -lm
```

> 库的架构必须与 Go 程序一致（amd64 / arm64）。交叉编译时使用对应工具链，
> 如 `aarch64-linux-gnu-gcc`、`x86_64-w64-mingw32-gcc`。

## 告诉绑定库在哪里

```bash
export RNNOISE_LIBRARY=/opt/rnnoise/librnnoise.so
```

或在代码中显式指定：

```go
lib, err := noisego.Load(&noisego.LoadOptions{
    Path: "/opt/rnnoise/librnnoise.so",
})
```
