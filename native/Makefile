# Build the reference denoiser shared library for the three supported targets.
#
#   make            # build for the host (auto detected)
#   make linux      # libvdnoise.so    (linux/amd64 or linux/arm64)
#   make darwin     # libvdnoise.dylib
#   make windows    # vdnoise.dll      (needs x86_64-w64-mingw32-gcc)
#   make all        # every target whose toolchain is installed (others skip)
#
# Each target drives its platform's own toolchain, so cross building only
# needs the matching compiler, e.g.
#   make darwin CC_DARWIN=o64-clang
#   make windows CC_WINDOWS=x86_64-w64-mingw32-gcc
# A traditional CC=... (command line/environment) applies to linux + darwin.

UNAME_S := $(shell uname -s)
OUT := lib
SRC := vdnoise.c

# Per-target toolchains: the ELF/macOS/PE linkers take different flags
# (-Wl,-soname vs -install_name vs --out-implib), so one CC cannot serve all.
CC_LINUX   ?= gcc
CC_DARWIN  ?= clang
CC_WINDOWS ?= x86_64-w64-mingw32-gcc

# Honor a traditional CC=... override for the POSIX targets.
ifneq ($(filter environment% command line,$(origin CC)),)
CC_LINUX  := $(CC)
CC_DARWIN := $(CC)
endif

CFLAGS_COMMON  := -O2 -Wall -Wextra -fvisibility=hidden
CFLAGS_POSIX   := $(CFLAGS_COMMON) -fPIC
CFLAGS_WINDOWS := $(CFLAGS_COMMON)

ifeq ($(UNAME_S),Darwin)
NATIVE := darwin
else ifeq ($(UNAME_S),Linux)
NATIVE := linux
else
NATIVE := windows
endif

.PHONY: all native linux darwin windows clean

# Build whichever targets have a toolchain on PATH; skip the rest with a
# note instead of feeding GNU-ld flags to the host linker (or vice versa).
have = $(shell command -v $(1) >/dev/null 2>&1 && echo yes)
ALL_TARGETS :=
$(if $(call have,$(CC_LINUX)),$(eval ALL_TARGETS += linux))
$(if $(call have,$(CC_DARWIN)),$(eval ALL_TARGETS += darwin))
$(if $(call have,$(CC_WINDOWS)),$(eval ALL_TARGETS += windows))

all:
	@targets='$(ALL_TARGETS)'; \
	if [ -z "$$targets" ]; then \
		echo "no supported C toolchain found (looked for $(CC_LINUX), $(CC_DARWIN), $(CC_WINDOWS))"; \
		exit 1; \
	fi; \
	$(MAKE) --no-print-directory $$targets
	@for t in linux darwin windows; do \
		case " $(ALL_TARGETS) " in *" $$t "*) ;; *) echo "skip $$t: toolchain not found";; esac; \
	done

native: $(NATIVE)

linux:
	@mkdir -p $(OUT)
	$(CC_LINUX) $(CFLAGS_POSIX) -shared -Wl,-soname,libvdnoise.so \
		-o $(OUT)/libvdnoise.so $(SRC) -lm
	@echo "-> $(OUT)/libvdnoise.so"

darwin:
	@mkdir -p $(OUT)
	$(CC_DARWIN) $(CFLAGS_POSIX) -shared -dynamiclib \
		-install_name @rpath/libvdnoise.dylib \
		-o $(OUT)/libvdnoise.dylib $(SRC) -lm
	@echo "-> $(OUT)/libvdnoise.dylib"

windows:
	@mkdir -p $(OUT)
	$(CC_WINDOWS) $(CFLAGS_WINDOWS) -shared \
		-o $(OUT)/vdnoise.dll $(SRC) -Wl,--out-implib,$(OUT)/libvdnoise.dll.a
	@echo "-> $(OUT)/vdnoise.dll"

clean:
	rm -rf $(OUT)
