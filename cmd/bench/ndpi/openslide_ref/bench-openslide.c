#include <openslide.h>
#include <stdio.h>
#include <stdlib.h>
#include <time.h>

int main(int argc, char **argv) {
    if (argc < 2) { fprintf(stderr, "usage: %s <slide>\n", argv[0]); return 2; }
    openslide_t *s = openslide_open(argv[1]);
    if (!s) { fprintf(stderr, "open failed\n"); return 1; }
    int64_t w, h;
    openslide_get_level_dimensions(s, 0, &w, &h);
    printf("source L0: %lldx%lld\n", (long long)w, (long long)h);
    const int TS = 256;
    int64_t cols = (w + TS - 1) / TS;
    int64_t rows = (h + TS - 1) / TS;
    uint32_t *buf = malloc((size_t)TS * TS * 4);
    struct timespec t0, t1;
    clock_gettime(CLOCK_MONOTONIC, &t0);
    int64_t pix = 0;
    for (int64_t r = 0; r < rows; r++) {
        for (int64_t c = 0; c < cols; c++) {
            int64_t x = c * TS;
            int64_t y = r * TS;
            int64_t tw = (w - x) < TS ? (w - x) : TS;
            int64_t th = (h - y) < TS ? (h - y) : TS;
            openslide_read_region(s, buf, x, y, 0, tw, th);
            pix += tw * th;
        }
    }
    clock_gettime(CLOCK_MONOTONIC, &t1);
    double el = (t1.tv_sec - t0.tv_sec) + (t1.tv_nsec - t0.tv_nsec) / 1e9;
    printf("%lld tiles, %lld MiB pixels in %.2f s (%.1f Mpix/s, %.1f MiB/s)\n",
        (long long)(rows * cols), (long long)(pix * 4 >> 20), el,
        pix / el / 1e6, pix * 4 / el / 1024 / 1024);
    const char *err = openslide_get_error(s);
    if (err) fprintf(stderr, "openslide error: %s\n", err);
    openslide_close(s);
    free(buf);
    return 0;
}
