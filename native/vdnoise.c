/*
 * vdnoise.c - reference implementation of the vdnoise C ABI.
 *
 * The algorithm is a small, dependency-free single-band spectral-gate-ish
 * denoiser that is good enough to demo the binding and to verify progress
 * callbacks end to end:
 *
 *   1. DC blocker (high-pass around 30 Hz)
 *   2. adaptive noise-floor estimator (fast attack / slow release), with the
 *      first frames treated as noise-only ("warmup")
 *   3. soft Wiener-style gain plus an expander floor, smoothed to avoid
 *      pumping and zipper noise
 *
 * It is intentionally simple; a production binding would point this same ABI
 * at RNNoise/RNNoise4Mics or a vendor engine instead.
 */
#include "vdnoise.h"

#include <math.h>
#include <stdlib.h>
#include <string.h>

#define WARMUP_FRAMES   8     /* frames assumed noise-only */
#define DC_R            0.992f/* DC blocker pole */
#define FLOOR_ALPHA_UP  0.25f /* noise floor tracks quickly when we are quiet */
#define FLOOR_ALPHA_DN  0.004f/* and barely moves when we are not */
#define FLOOR_BIAS      1.5f  /* gate threshold multiplier over the floor */
#define GAIN_SMOOTH     0.12f/* per-frame gain smoothing */
#define GAIN_FLOOR      0.04f/* maximum suppression */
#define OVERDRIVE       2.0f /* Wiener exponent */

typedef struct {
    float x1;        /* previous input  */
    float y1;        /* previous output */
    float noise;     /* estimated noise RMS */
    float gain;      /* smoothed gain    */
} vd_channel;

struct vd_ctx {
    uint32_t sample_rate;
    uint32_t channels;
    uint32_t frame_size;
    vd_progress_fn progress;
    void *user;
    vd_channel *ch;
    float *scratch;
    int64_t total_frames;
    int64_t done_frames;
    int last_percent;
};

static void *xcalloc(size_t n, size_t sz) { return calloc(n, sz); }

VD_API int vd_abi_version(void) {
    return (VD_ABI_VERSION_MAJOR << 16) | VD_ABI_VERSION_MINOR;
}

VD_API const char *vd_version(void) { return "vdnoise-ref 1.0.0"; }

VD_API int vd_open(uint32_t sample_rate, uint32_t channels, uint32_t frame_size,
                   vd_progress_fn progress, void *user, void **out) {
    if (out == NULL) return VD_ERR_INVALID;
    *out = NULL;
    if (sample_rate < 8000 || sample_rate > 384000) return VD_ERR_INVALID;
    if (channels != 1 && channels != 2) return VD_ERR_INVALID;
    if (frame_size == 0 || frame_size > 8192) return VD_ERR_INVALID;

    struct vd_ctx *c = (struct vd_ctx *)xcalloc(1, sizeof(*c));
    if (c == NULL) return VD_ERR_NOMEM;
    c->ch = (vd_channel *)xcalloc(channels, sizeof(vd_channel));
    c->scratch = (float *)malloc((size_t)frame_size * sizeof(float));
    if (c->ch == NULL || c->scratch == NULL) {
        free(c->scratch);
        free(c->ch);
        free(c);
        return VD_ERR_NOMEM;
    }
    c->sample_rate = sample_rate;
    c->channels = channels;
    c->frame_size = frame_size;
    c->progress = progress;
    c->user = user;
    c->total_frames = -1;
    c->done_frames = 0;
    c->last_percent = -1;
    /* start pessimistically so the warmup frames are not loud. */
    for (uint32_t i = 0; i < channels; ++i) {
        c->ch[i].gain = 1.0f;
        c->ch[i].noise = 0.0f;
    }
    *out = c;
    return VD_OK;
}

VD_API int vd_set_total(void *ctx, int64_t frames) {
    if (ctx == NULL) return VD_ERR_INVALID;
    struct vd_ctx *c = (struct vd_ctx *)ctx;
    c->total_frames = frames;
    c->done_frames = 0;
    c->last_percent = -1;
    return VD_OK;
}

static int emit_progress(struct vd_ctx *c) {
    if (c->progress == NULL) return VD_OK;
    int pct;
    if (c->total_frames > 0) {
        pct = (int)(c->done_frames * 100 / c->total_frames);
        if (pct > 99) pct = 99;
    } else {
        /* unknown length: crawl towards 99 */
        pct = (int)(99.0 * (1.0 - exp(-(double)c->done_frames / 200.0)));
    }
    if (pct != c->last_percent) {
        c->last_percent = pct;
        int rc = c->progress(pct, c->user);
        if (rc != VD_OK) return rc;
    }
    return VD_OK;
}

/*
 * Process one channel's worth of float samples (length = frame_size).
 * warmup forces the noise estimator to track unconditionally.
 */
static int process_channel(vd_channel *st, float *buf, uint32_t n, int warmup) {
    /* 1. frame RMS + DC blocker, measure energy first. */
    double energy = 0.0;
    for (uint32_t i = 0; i < n; ++i) {
        float x = buf[i];
        float y = x - st->x1 + DC_R * st->y1;
        st->x1 = x;
        st->y1 = y;
        buf[i] = y;
        energy += (double)y * y;
    }
    float rms = (float)sqrt(energy / n);
    if (rms < 1e-12f) rms = 1e-12f;

    /* 2. update the noise floor estimate. */
    if (warmup) {
        /* noise-only frames: integrate quickly in either direction. */
        st->noise = 0.6f * st->noise + 0.4f * rms;
    } else if (rms < st->noise * 1.3f) {
        st->noise += FLOOR_ALPHA_UP * (rms - st->noise);
    } else {
        st->noise += FLOOR_ALPHA_DN * (rms - st->noise);
    }
    if (st->noise < 1e-7f) st->noise = 1e-7f;

    /* 3. target gain: soft Wiener gate + expander floor. */
    float target;
    if (warmup) {
        target = GAIN_FLOOR;
    } else {
        float snr2 = (rms * rms) / (FLOOR_BIAS * st->noise * st->noise * FLOOR_BIAS);
        if (snr2 > 1e6f) snr2 = 1e6f;
        float wiener = snr2 / (snr2 + 1.0f);
        wiener = powf(wiener, OVERDRIVE * 0.5f);
        target = GAIN_FLOOR + (1.0f - GAIN_FLOOR) * wiener;
    }
    st->gain += GAIN_SMOOTH * (target - st->gain);
    if (st->gain < GAIN_FLOOR) st->gain = GAIN_FLOOR;
    if (st->gain > 1.0f) st->gain = 1.0f;

    /* 4. apply gain (constant per frame -> no intra-frame clicks). */
    for (uint32_t i = 0; i < n; ++i) buf[i] *= st->gain;
    return VD_OK;
}

VD_API int vd_process_f32(void *ctx, const float *in, float *out) {
    if (ctx == NULL || in == NULL || out == NULL) return VD_ERR_INVALID;
    struct vd_ctx *c = (struct vd_ctx *)ctx;
    uint32_t n = c->frame_size * c->channels;
    memcpy(out, in, n * sizeof(float));
    int warmup = (int64_t)c->done_frames < WARMUP_FRAMES;
    for (uint32_t ch = 0; ch < c->channels; ++ch) {
        /* deinterleave into the per-context scratch, then back. */
        float *tmp = c->scratch;
        for (uint32_t i = 0; i < c->frame_size; ++i) tmp[i] = out[i * c->channels + ch];
        int rc = process_channel(&c->ch[ch], tmp, c->frame_size, warmup);
        if (rc != VD_OK) return rc;
        for (uint32_t i = 0; i < c->frame_size; ++i) out[i * c->channels + ch] = tmp[i];
    }
    c->done_frames++;
    int rc = emit_progress(c);
    if (rc != VD_OK) return rc;
    return VD_OK;
}

VD_API int vd_process_s16(void *ctx, const int16_t *in, int16_t *out) {
    /* GCC's alias analysis cannot prove fin/fout stay inside buf and emits a
       bogus -Wmaybe-uninitialized here. */
#if defined(__GNUC__) && !defined(__clang__)
#pragma GCC diagnostic push
#pragma GCC diagnostic ignored "-Wmaybe-uninitialized"
#endif
    if (ctx == NULL || in == NULL || out == NULL) return VD_ERR_INVALID;
    struct vd_ctx *c = (struct vd_ctx *)ctx;
    uint32_t n = c->frame_size * c->channels;
    /* one allocation holds both input and output float buffers */
    float *buf = (float *)malloc((size_t)n * 2 * sizeof(float));
    if (buf == NULL) return VD_ERR_NOMEM;
    float *fin = buf;
    float *fout = buf + n;
    for (uint32_t i = 0; i < n; ++i) fin[i] = (float)in[i] / 32768.0f;
    int rc = vd_process_f32(ctx, fin, fout);
    if (rc == VD_OK) {
        for (uint32_t i = 0; i < n; ++i) {
            float v = fout[i] * 32768.0f;
            if (v > 32767.0f) v = 32767.0f;
            if (v < -32768.0f) v = -32768.0f;
            out[i] = (int16_t)lrintf(v);
        }
    }
    free(buf);
    return rc;
}
#if defined(__GNUC__) && !defined(__clang__)
#pragma GCC diagnostic pop
#endif

VD_API int vd_finish(void *ctx) {
    if (ctx == NULL) return VD_ERR_INVALID;
    struct vd_ctx *c = (struct vd_ctx *)ctx;
    if (c->progress != NULL && c->last_percent != 100) {
        c->last_percent = 100;
        int rc = c->progress(100, c->user);
        if (rc != VD_OK) return rc;
    }
    c->done_frames = 0;
    c->last_percent = -1;
    return VD_OK;
}

VD_API void vd_close(void *ctx) {
    if (ctx == NULL) return;
    struct vd_ctx *c = (struct vd_ctx *)ctx;
    free(c->scratch);
    free(c->ch);
    free(c);
}
