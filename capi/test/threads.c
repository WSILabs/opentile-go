/* Concurrency test for libopentile: N threads decode tiles from one handle
 * while the main thread closes it mid-flight. Every call must return either
 * OT_OK or OT_ERR_HANDLE (never crash), errors must stay per-thread, and
 * ot_close must not return while calls are in flight.
 * Usage: threads <slide-path>. POSIX threads (MinGW winpthreads on Windows). */
#include <pthread.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include "opentile.h"

#define NTHREADS 8

static ot_slide slide;
static ot_level_info l0;
static volatile int bad = 0;
static volatile long ok_calls = 0, handle_errs = 0;

static void* worker(void* arg) {
    int id = (int)(size_t)arg;
    ot_decode_opts o = {sizeof o, OT_PIXEL_RGBA8, OT_DECODE_SRGB, 0, 0};
    size_t need = (size_t)l0.tile_w * l0.tile_h * 4;
    ot_image img = {sizeof img, malloc(need), need, 0, 0, 0, 0, 0};
    for (int i = 0; i < 4000; i++) {
        int32_t x = (id * 7 + i) % l0.grid_w, y = (id * 3 + i / l0.grid_w) % l0.grid_h;
        int rc = ot_tile_decoded(slide, 0, 0, x, y, &o, &img);
        if (rc == OT_OK) { __sync_fetch_and_add(&ok_calls, 1); continue; }
        if (rc == OT_ERR_HANDLE) {
            /* thread-local error: this thread's last error must be HANDLE */
            if (ot_last_error_code() != OT_ERR_HANDLE) bad = 1;
            __sync_fetch_and_add(&handle_errs, 1);
            break;
        }
        char m[512]; size_t n; ot_last_error(m, sizeof m, &n);
        fprintf(stderr, "thread %d tile (%d,%d): rc %d %s\n", id, x, y, rc, m);
        bad = 1; break;
    }
    /* provoke a different error on this thread; must not leak to others */
    ot_level_info li = {sizeof li};
    ot_level_info_get(slide, 0, 9999, &li);
    free(img.data);
    return NULL;
}

int main(int argc, char** argv) {
    if (argc < 2) return 2;
    if (ot_open(argv[1], NULL, &slide) != OT_OK) return 1;
    l0.struct_size = sizeof l0;
    ot_level_info_get(slide, 0, 0, &l0);
    pthread_t th[NTHREADS];
    for (int i = 0; i < NTHREADS; i++) pthread_create(&th[i], NULL, worker, (void*)(size_t)i);
    /* let decodes run, then close underneath them */
    while (ok_calls < 200) {}
    int rc = ot_close(slide);
    if (rc != OT_OK) { fprintf(stderr, "close rc %d\n", rc); bad = 1; }
    for (int i = 0; i < NTHREADS; i++) pthread_join(th[i], NULL);
    printf("ok calls %ld, handle errors after close %ld\n", ok_calls, handle_errs);
    if (bad) { fprintf(stderr, "FAIL\n"); return 1; }
    printf("OK\n");
    return 0;
}
