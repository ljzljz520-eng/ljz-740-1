/*
 * vdnoise.h - stable C ABI for the voice denoiser shared library.
 *
 * This is the binary contract implemented by native/libvdnoise.* and loaded
 * from Go through purego. Any conforming third-party denoiser may be dropped
 * in as long as it exports these symbols.
 *
 * Calling convention:
 *   - All functions use the platform default C ABI (cdecl on Windows x86,
 *     native ABI elsewhere).
 *   - Functions return an int status code (VD_OK == 0).
 *   - Strings are NUL terminated UTF-8 and stay owned by the library.
 */
#ifndef VDNOISE_H
#define VDNOISE_H

#include <stddef.h>
#include <stdint.h>

#if defined(_WIN32)
  #define VD_API __declspec(dllexport)
#else
  #define VD_API __attribute__((visibility("default")))
#endif

#ifdef __cplusplus
extern "C" {
#endif

/* Status codes. */
#define VD_OK            0
#define VD_ERR_INVALID   1  /* bad argument (NULL, unsupported value...) */
#define VD_ERR_NOMEM     2  /* allocation failed */
#define VD_ERR_FORMAT    3  /* frame size / format mismatch */
#define VD_ERR_INTERNAL  4

/* Interleaved sample format codes. */
#define VD_FMT_S16 0  /* signed 16 bit little endian              */
#define VD_FMT_F32 1  /* native-endian IEEE 754 float, range [-1,1] */

/* ABI version: major must match, minor may differ. */
#define VD_ABI_VERSION_MAJOR 1
#define VD_ABI_VERSION_MINOR 0

/*
 * Progress callback invoked from inside vd_process*.
 *   percent : 0..100 (100 is always emitted on the final call/flush)
 *   user    : opaque pointer registered with vd_open
 * Return VD_OK to continue, anything else to abort processing.
 */
typedef int (*vd_progress_fn)(int percent, void *user);

/* Returns the static ABI version as (major<<16)|minor. */
VD_API int vd_abi_version(void);

/* Human readable version string, e.g. "vdnoise-ref 1.0.0". */
VD_API const char *vd_version(void);

/*
 * Create a denoising context.
 *   sample_rate : 8000..384000 Hz
 *   channels    : 1 or 2 (interleaved)
 *   frame_size  : samples per channel per process call, 1..8192
 *   progress    : optional progress callback (may be NULL)
 *   user        : opaque pointer handed back to the callback
 *   out         : receives the context handle
 */
VD_API int vd_open(uint32_t sample_rate, uint32_t channels, uint32_t frame_size,
                   vd_progress_fn progress, void *user, void **out);

/*
 * Declare the total number of frames that will be processed, so the library
 * can emit meaningful percentages. Optional; call before vd_process*.
 *   frames < 0 means "unknown" (callbacks use 0..99 then a final 100).
 */
VD_API int vd_set_total(void *ctx, int64_t frames);

/* Process one interleaved signed-16 frame. in/out: frame_size*channels. */
VD_API int vd_process_s16(void *ctx, const int16_t *in, int16_t *out);

/* Process one interleaved float frame. in/out: frame_size*channels. */
VD_API int vd_process_f32(void *ctx, const float *in, float *out);

/*
 * Mark processing complete. Emits a final 100% progress event (unless the
 * known total already caused it). Processing may continue afterwards, but
 * the percentage counter is reset.
 */
VD_API int vd_finish(void *ctx);

/* Destroy a context created with vd_open. Safe with NULL. */
VD_API void vd_close(void *ctx);

#ifdef __cplusplus
}
#endif

#endif /* VDNOISE_H */
