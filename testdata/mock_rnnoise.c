/* 用于 Go 绑定端到端测试的 mock 动态库，模拟 RNNoise 的 C ABI：
 *   void* rnnoise_create(void* model);
 *   int   rnnoise_get_frame_size(void);
 *   float rnnoise_process_frame(void* st, float* out, const float* in);
 *   void  rnnoise_destroy(void* st);
 */
#include <stdlib.h>
#include <string.h>
#include <stdint.h>

typedef struct {
    int destroyed;
    long frames;
} mock_state;

void* rnnoise_create(void* model) {
    (void)model;
    mock_state* st = (mock_state*)calloc(1, sizeof(mock_state));
    return st;
}

int rnnoise_get_frame_size(void) { return 480; }

float rnnoise_process_frame(void* p, float* out, const float* in) {
    mock_state* st = (mock_state*)p;
    st->frames++;
    /* 确定性“降噪”：幅度减半并加一个小偏置，方便测试断言。 */
    for (int i = 0; i < 480; i++) {
        out[i] = in[i] * 0.5f + 0.001f;
    }
    /* 伪 VAD 概率，随帧变化。 */
    return 0.25f + 0.01f * (float)(st->frames % 10);
}

void rnnoise_destroy(void* p) {
    if (!p) return;
    mock_state* st = (mock_state*)p;
    st->destroyed = 1;
    free(st);
}
