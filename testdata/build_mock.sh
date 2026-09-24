#!/usr/bin/env bash
# 编译用于单元测试的 mock RNNoise 动态库。
# 用法: ./testdata/build_mock.sh
set -euo pipefail
cd "$(dirname "$0")"
CC="${CC:-cc}"
case "$(uname -s)" in
  Linux*)   out=librnnoise_mock.so;;
  Darwin*)  out=librnnoise_mock.dylib;;
  MINGW*|MSYS*|CYGWIN*) out=rnnoise_mock.dll;;
  *) echo "不支持的平台: $(uname -s)"; exit 1;;
esac
"$CC" -O2 -shared -fPIC -o "$out" mock_rnnoise.c
echo "已生成 testdata/$out"
