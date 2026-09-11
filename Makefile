# Top-level convenience targets.
#
#   make            build the native reference library + the CLI
#   make lib        build native/lib/libvdnoise.* for the host
#   make cli        build build/vdnoise
#   make test       run unit/integration tests against the built library
#   make clean
#
# Cross targets live in native/Makefile (linux / darwin / windows).

GO ?= go
UNAME_S := $(shell uname -s)
ifeq ($(UNAME_S),Darwin)
LIBNAME := libvdnoise.dylib
else
LIBNAME := libvdnoise.so
endif
NATIVE_LIB := native/lib/$(LIBNAME)

.PHONY: all lib cli test test-unit fmt vet clean

all: lib cli

lib:
	$(MAKE) -C native native

cli:
	$(GO) build -o build/vdnoise ./cmd/vdnoise

# Full suite needs a built native library.
test: lib
	VDNOISE_LIB=$(abspath $(NATIVE_LIB)) $(GO) test -count=1 ./...

# Tests that don't need the library (integration tests skip themselves).
test-unit:
	$(GO) test -count=1 ./...

fmt:
	$(GO) fmt ./...

vet:
	$(GO) vet ./...
	CGO_ENABLED=0 $(GO) vet ./...

clean:
	$(MAKE) -C native clean
	rm -rf build
