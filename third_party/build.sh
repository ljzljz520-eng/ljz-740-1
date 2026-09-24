#!/usr/bin/env bash
# Build the RNNoise shared library from upstream source into third_party/.
#
# Usage:
#   ./third_party/build.sh                 # native library for this machine
#   CC=x86_64-w64-mingw32-gcc ./third_party/build.sh windows
#   ./third_party/build.sh macos-universal # needs clang + macOS SDK
#
# Targets: native (default), linux, macos, macos-universal, windows
set -euo pipefail

TARGET="${1:-native}"
HERE="$(cd "$(dirname "$0")" && pwd)"
SRC="$HERE/rnnoise"
OUT="$HERE"

# Pin a known-good upstream revision. Override with RNNOISE_REV=<rev>.
REV="${RNNOISE_REV:-master}"
URL="https://github.com/xiph/rnnoise.git"

log() { printf '\033[1;34m==>\033[0m %s\n' "$*"; }

if [ ! -d "$SRC" ]; then
  log "cloning RNNoise ($REV)"
  git clone --depth 1 "$URL" "$SRC"
fi

# Upstream keeps the trained weights out of git; download_model.sh fetches
# and extracts src/rnnoise_data*.c.
if [ ! -f "$SRC/src/rnnoise_data_little.c" ]; then
  log "downloading model weights"
  (cd "$SRC" && ./download_model.sh)
fi

SOURCES="src/denoise.c src/rnn.c src/pitch.c src/kiss_fft.c src/celt_lpc.c \
src/nnet.c src/nnet_default.c src/parse_lpcnet_weights.c \
src/rnnoise_data_little.c src/rnnoise_tables.c"
INCLUDES="-Iinclude -Isrc"
CFLAGS="-O2 -fPIC -fvisibility=hidden -DRNNOISE_BUILD"

cd "$SRC"
OUT_LIB=""

if [ "$TARGET" = "native" ]; then
  case "$(uname -s)" in
    Linux*)  TARGET=linux ;;
    Darwin*) TARGET=macos ;;
    MINGW*|MSYS*|CYGWIN*) TARGET=windows ;;
      *) echo "unsupported host: $(uname -s)" >&2; exit 1 ;;
  esac
fi

case "$TARGET" in
  linux)
    CC_BIN="${CC:-cc}"
    OUT_LIB="$OUT/librnnoise.so"
    $CC_BIN $CFLAGS $INCLUDES -shared $SOURCES -lm -o "$OUT_LIB"
    ;;
  macos)
    CC_BIN="${CC:-cc}"
    OUT_LIB="$OUT/librnnoise.dylib"
    $CC_BIN $CFLAGS $INCLUDES -dynamiclib $SOURCES -lm -o "$OUT_LIB"
    ;;
  macos-universal)
    CC_BIN="${CC:-cc}"
    OUT_LIB="$OUT/librnnoise.dylib"
    $CC_BIN $CFLAGS -arch arm64 -arch x86_64 $INCLUDES -dynamiclib $SOURCES -lm -o "$OUT_LIB"
    ;;
  windows)
    CC_BIN="${CC:-x86_64-w64-mingw32-gcc}"
    OUT_LIB="$OUT/rnnoise.dll"
    $CC_BIN -O2 -fvisibility=hidden -DRNNOISE_BUILD $INCLUDES -shared \
      $SOURCES -o "$OUT_LIB" -Wl,--out-implib,"$OUT/librnnoise.dll.a" -lm
    ;;
  *)
    echo "unknown target: $TARGET (want linux|macos|macos-universal|windows)" >&2
    exit 1
    ;;
esac

log "built $OUT_LIB"
