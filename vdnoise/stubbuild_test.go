package vdnoise

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

const badABISrc = `
#include <stdint.h>
int vd_abi_version(void){return (99<<16)|0;}
const char *vd_version(void){return "fake";}
int vd_open(uint32_t a,uint32_t b,uint32_t c,void*p,void*u,void**o){(void)a;(void)b;(void)c;(void)p;(void)u;(void)o;return 0;}
int vd_set_total(void*c,long long t){(void)c;(void)t;return 0;}
int vd_process_s16(void*c,const short*i,short*o){(void)c;(void)i;(void)o;return 0;}
int vd_process_f32(void*c,const float*i,float*o){(void)c;(void)i;(void)o;return 0;}
int vd_finish(void*c){(void)c;return 0;}
void vd_close(void*c){(void)c;}
`

func libFileName() string {
	switch runtime.GOOS {
	case "darwin":
		return "a.dylib"
	case "windows":
		return "a.dll"
	default:
		return "a.so"
	}
}

// buildStubLib compiles a tiny C source into a shared object so tests can
// exercise symbol/ABI validation without shipping fake binaries. Requires gcc
// (or clang on macOS); skips when no compiler is available.
func buildStubLib(t *testing.T, src string) string {
	t.Helper()
	cc := os.Getenv("CC")
	if cc == "" {
		if runtime.GOOS == "darwin" {
			cc = "clang"
		} else {
			cc = "gcc"
		}
	}
	if _, err := exec.LookPath(cc); err != nil {
		t.Skipf("%s not found, skipping stub-library test", cc)
	}
	dir := t.TempDir()
	srcPath := filepath.Join(dir, "a.c")
	if err := os.WriteFile(srcPath, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	libPath := filepath.Join(dir, libFileName())
	cmd := exec.Command(cc, "-shared", "-fPIC", "-O0", "-o", libPath, srcPath)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("compile stub: %v\n%s", err, out)
	}
	return libPath
}
