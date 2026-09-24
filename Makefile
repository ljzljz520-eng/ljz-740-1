# go-rnnoise: cgo-free RNNoise binding
GO ?= go

.PHONY: all lib lib-windows lib-macos test build clean fmt vet demo

all: build

## Build the native RNNoise shared library for the host platform into third_party/
lib:
	./third_party/build.sh native

lib-linux:
	./third_party/build.sh linux

## Cross build the Windows DLL (needs x86_64-w64-mingw32-gcc)
lib-windows:
	CC=x86_64-w64-mingw32-gcc ./third_party/build.sh windows

## Build the macOS dylib (run on macOS, or with an osxcross toolchain)
lib-macos:
	./third_party/build.sh macos

fmt:
	$(GO) fmt ./...

vet:
	$(GO) vet ./...

test:
	$(GO) test ./...

build:
	CGO_ENABLED=0 $(GO) build ./...

## Build the example CLI binary into bin/
demo:
	mkdir -p bin
	CGO_ENABLED=0 $(GO) build -o bin/rnnoise-cli ./cmd/rnnoise-cli

clean:
	rm -rf bin
	rm -f third_party/librnnoise.so third_party/librnnoise.dylib \
	      third_party/rnnoise.dll third_party/librnnoise.dll.a
