# Build the reference denoiser shared library for the three supported targets.
#
#   make            # build for the host (auto detected)
#   make linux      # libvdnoise.so    (linux/amd64 or linux/arm64)
#   make darwin     # libvdnoise.dylib
#   make windows    # vdnoise.dll      (needs x86_64-w64-mingw32-gcc)
#   make all
#
# Cross compilers can be overridden, e.g.
#   make darwin CC=o64-clang
#   make windows CC=x86_64-w64-mingw32-gcc

UNAME_S := $(shell uname -s)
OUT := lib
SRC := vdnoise.c
# make's built-in default is "cc"; pick the native toolchain unless CC came
# from the environment or the command line.
ifeq ($(origin CC),default)
ifeq ($(UNAME_S),Darwin)
CC = clang
else
CC = gcc
endif
endif
CFLAGS := -O2 -fPIC -Wall -Wextra -fvisibility=hidden
LDFLAGS_UNIX := -lm

ifeq ($(UNAME_S),Darwin)
NATIVE := darwin
else ifeq ($(UNAME_S),Linux)
NATIVE := linux
else
NATIVE := windows
endif

.PHONY: all native linux darwin windows clean

all: linux darwin windows

native: $(NATIVE)

linux:
	@mkdir -p $(OUT)
	$(CC) $(CFLAGS) -shared -Wl,-soname,libvdnoise.so \
		-o $(OUT)/libvdnoise.so $(SRC) $(LDFLAGS_UNIX)
	@echo "-> $(OUT)/libvdnoise.so"

darwin:
	@mkdir -p $(OUT)
	$(CC) $(CFLAGS) -shared -dynamiclib \
		-install_name @rpath/libvdnoise.dylib \
		-o $(OUT)/libvdnoise.dylib $(SRC) $(LDFLAGS_UNIX)
	@echo "-> $(OUT)/libvdnoise.dylib"

windows:
	@mkdir -p $(OUT)
	x86_64-w64-mingw32-gcc -O2 -Wall -Wextra -shared \
		-o $(OUT)/vdnoise.dll $(SRC) -Wl,--out-implib,$(OUT)/libvdnoise.dll.a
	@echo "-> $(OUT)/vdnoise.dll"

clean:
	rm -rf $(OUT)
