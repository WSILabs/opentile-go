/* C smoke test for libopentile: exercises every entry point against one
 * slide. Usage: smoke <slide-path>. Exit 0 on success.
 * Builds as C99 with gcc/clang/MSVC; links against libopentile. */
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include "opentile.h"

static int failures = 0;

#define CHECK(cond, ...) do { if (!(cond)) { \
    fprintf(stderr, "FAIL %s:%d: ", __FILE__, __LINE__); \
    fprintf(stderr, __VA_ARGS__); fprintf(stderr, "\n"); failures++; } } while (0)

static void print_error(const char* what, int rc) {
    char msg[1024];
    size_t n;
    ot_last_error(msg, sizeof msg, &n);
    fprintf(stderr, "  %s -> %d (code %d): %s\n", what, rc, ot_last_error_code(), msg);
}

#define OK(call) do { int rc_ = (call); if (rc_ != OT_OK) { print_error(#call, rc_); failures++; } } while (0)

/* Decode with a size query first, then into a right-sized buffer. */
static int decode_two_step(const char* what, int (*fn)(ot_image*, void*), void* ctx, ot_image* out) {
    ot_image q = {sizeof q};
    int rc = fn(&q, ctx);
    if (rc != OT_OK) { print_error(what, rc); return rc; }
    size_t need = (size_t)q.stride * (q.height - 1) + (size_t)q.width * (q.pixel_format == OT_PIXEL_RGBA8 ? 4 : 3);
    ot_image img = {sizeof img};
    img.data = malloc(need);
    img.capacity = need;
    rc = fn(&img, ctx);
    if (rc != OT_OK) print_error(what, rc);
    else printf("  %-22s %dx%d stride %d fmt %d cm %d\n", what, img.width, img.height, img.stride, img.pixel_format, img.color_managed);
    *out = img;
    return rc;
}

typedef struct { ot_slide s; int32_t level; ot_decode_opts* o; } ctx_t;

static int do_tile(ot_image* img, void* c) { ctx_t* x = c; return ot_tile_decoded(x->s, 0, x->level, 0, 0, x->o, img); }
static int do_stitched(ot_image* img, void* c) { ctx_t* x = c; return ot_tile_stitched(x->s, 0, x->level, 0, 0, x->o, img); }
static int do_region(ot_image* img, void* c) { ctx_t* x = c; return ot_read_region(x->s, 0, x->level, 100, 100, 300, 200, x->o, img); }
static int do_scaled(ot_image* img, void* c) { ctx_t* x = c; return ot_read_region_scaled(x->s, 0, 0, 0, 4000, 4000, 250, 250, x->o, img); }
static int do_thumb(ot_image* img, void* c) { ctx_t* x = c; return ot_thumbnail(x->s, 256, 256, x->o, img); }

int main(int argc, char** argv) {
    if (argc < 2) { fprintf(stderr, "usage: smoke <slide>\n"); return 2; }

    int32_t maj, min;
    OK(ot_api_version(&maj, &min));
    CHECK(maj == OT_API_VERSION_MAJOR && min == OT_API_VERSION_MINOR, "api version %d.%d", maj, min);
    char ver[64]; size_t vlen;
    OK(ot_library_version(ver, sizeof ver, &vlen));
    int32_t jpeg, cm;
    OK(ot_codec_available(OT_COMPRESSION_JPEG, &jpeg));
    OK(ot_color_management_available(&cm));
    printf("libopentile api %d.%d, opentile-go %s, jpeg=%d lcms2=%d\n", maj, min, ver, jpeg, cm);

    /* A release build decodes everything but Iris; Unknown is never available. */
    for (int32_t c = OT_COMPRESSION_UNKNOWN; c <= OT_COMPRESSION_PNG; c++) {
        int32_t avail = -1;
        OK(ot_codec_available(c, &avail));
        int want = c != OT_COMPRESSION_UNKNOWN && c != OT_COMPRESSION_IRIS;
        CHECK(avail == want, "ot_codec_available(%d) = %d, want %d", c, avail, want);
    }
    int32_t avail_bad;
    CHECK(ot_codec_available(OT_COMPRESSION_PNG + 1, &avail_bad) == OT_ERR_ARG, "unknown compression code accepted");

    /* errors */
    ot_slide bad = 0;
    int rc = ot_open("does-not-exist.svs", NULL, &bad);
    CHECK(rc == OT_ERR_IO && ot_last_error_code() == OT_ERR_IO, "open missing file: rc %d", rc);
    size_t elen;
    OK(ot_last_error(NULL, 0, &elen));
    CHECK(elen > 0, "empty error message");
    CHECK(ot_close(12345) == OT_ERR_HANDLE, "close of bogus handle");
    ot_slide_info tiny = {4};
    CHECK(ot_slide_info_get(1, &tiny) == OT_ERR_ARG, "short struct_size accepted");

    /* open */
    ot_slide s = 0;
    ot_open_opts oo = {sizeof oo};
    OK(ot_open(argv[1], &oo, &s));
    if (!s) return 1;

    ot_slide_info si = {sizeof si};
    OK(ot_slide_info_get(s, &si));
    printf("format %d, %d pyramid(s), %d associated, mag %.1f, mpp %.4f/%.4f, icc %llu bytes\n",
           si.format, si.pyramid_count, si.associated_count, si.magnification, si.mpp_x, si.mpp_y,
           (unsigned long long)si.icc_size);
    CHECK(si.format != OT_FORMAT_UNKNOWN, "unknown format");

    ot_pyramid_info pi = {sizeof pi};
    OK(ot_pyramid_info_get(s, 0, &pi));
    CHECK(ot_pyramid_info_get(s, si.pyramid_count, &pi) == OT_ERR_RANGE, "pyramid OOR");
    int32_t stitched_level = -1;
    for (int32_t l = 0; l < pi.level_count; l++) {
        ot_level_info li = {sizeof li};
        OK(ot_level_info_get(s, 0, l, &li));
        printf("  L%d %dx%d tile %dx%d grid %dx%d sgrid %dx%d overlap %d comp %d ds %.2f max %llu\n", l,
               li.width, li.height, li.tile_w, li.tile_h, li.grid_w, li.grid_h,
               li.stitched_grid_w, li.stitched_grid_h, li.overlap_mode, li.compression, li.downsample,
               (unsigned long long)li.tile_max_bytes);
        if (li.overlap_mode != OT_OVERLAP_NONE && stitched_level < 0) stitched_level = l;
    }
    ot_level_info oor = {sizeof oor};
    CHECK(ot_level_info_get(s, 0, pi.level_count, &oor) == OT_ERR_RANGE, "level OOR");

    /* JSON + ICC */
    size_t jlen;
    OK(ot_metadata_json(s, NULL, 0, &jlen));
    char small[8];
    CHECK(ot_metadata_json(s, small, sizeof small, &jlen) == OT_ERR_BUFFER, "small json buffer");
    char* json = malloc(jlen + 1);
    OK(ot_metadata_json(s, json, jlen + 1, &jlen));
    CHECK(strlen(json) == jlen && json[0] == '{', "json not NUL-terminated object");
    printf("metadata json %zu bytes: %.120s...\n", jlen, json);
    free(json);
    size_t ilen;
    OK(ot_icc_profile(s, NULL, 0, &ilen));
    CHECK(ilen == si.icc_size, "icc size mismatch");
    rc = ot_tiff_tags_json(s, 0, 0, NULL, 0, &jlen);
    CHECK(rc == OT_OK || rc == OT_ERR_UNSUPPORTED, "tiff tags rc %d", rc);
    if (rc == OT_OK) printf("tiff tags json %zu bytes\n", jlen);

    /* raw tile */
    ot_level_info l0 = {sizeof l0};
    OK(ot_level_info_get(s, 0, 0, &l0));
    size_t rlen;
    OK(ot_tile_raw(s, 0, 0, 0, 0, NULL, 0, &rlen));
    uint8_t* raw = malloc(rlen);
    OK(ot_tile_raw(s, 0, 0, 0, 0, raw, rlen, &rlen));
    printf("raw tile (0,0) %zu bytes, first %02x %02x\n", rlen, raw[0], raw[1]);
    CHECK(l0.tile_max_bytes == 0 || rlen <= l0.tile_max_bytes, "raw tile exceeds tile_max_bytes");
    free(raw);
    CHECK(ot_tile_raw(s, 0, 0, l0.grid_w, 0, NULL, 0, &rlen) == OT_ERR_RANGE, "tile OOR");

    /* sRGB: no profile passes through; a profile needs lcms2 and must be
     * one lcms2 can use (the Iris cervix sample's can't be linked). */
    int srgb_rc = ot_srgb_check(s);
    CHECK(srgb_rc == OT_OK || srgb_rc == OT_ERR_UNSUPPORTED, "srgb check rc %d", srgb_rc);
    if (si.icc_size == 0) CHECK(srgb_rc == OT_OK, "no profile must pass ot_srgb_check");
    if (si.icc_size != 0 && !cm) CHECK(srgb_rc == OT_ERR_UNSUPPORTED, "profile without lcms2 must fail");
    printf("srgb check %d\n", srgb_rc);
    if (srgb_rc != OT_OK) {
        ot_decode_opts bad = {sizeof bad, OT_PIXEL_RGBA8, OT_DECODE_SRGB};
        ot_image q = {sizeof q};
        CHECK(ot_tile_decoded(s, 0, 0, 0, 0, &bad, &q) == OT_ERR_UNSUPPORTED, "srgb decode must fail like ot_srgb_check");
    }

    /* pixels */
    ot_decode_opts rgba = {sizeof rgba, OT_PIXEL_RGBA8, srgb_rc == OT_OK ? OT_DECODE_SRGB : 0};
    ot_decode_opts rgb = {sizeof rgb, OT_PIXEL_RGB8, 0};
    ot_image img;
    ctx_t c = {s, 0, &rgba};
    if (decode_two_step("tile rgba+srgb", do_tile, &c, &img) == OT_OK) {
        CHECK(img.width == l0.tile_w && img.height == l0.tile_h, "tile size");
        CHECK(img.data[3] == 0xFF, "alpha not opaque");
        CHECK(img.color_managed == (srgb_rc == OT_OK && si.icc_size != 0), "color_managed %d", img.color_managed);
    }
    free(img.data);

    /* caller stride + exact-capacity BUFFER check */
    {
        ot_image p = {sizeof p};
        p.stride = 300 * 3 + 16;
        size_t need = (size_t)p.stride * 199 + 300 * 3;
        p.data = malloc(need);
        p.capacity = need - 1;
        CHECK(ot_read_region(s, 0, 0, 100, 100, 300, 200, &rgb, &p) == OT_ERR_BUFFER, "short capacity");
        p.capacity = need;
        OK(ot_read_region(s, 0, 0, 100, 100, 300, 200, &rgb, &p));
        CHECK(p.stride == 300 * 3 + 16 && p.width == 300 && p.height == 200, "strided region");
        free(p.data);
    }
    c.o = &rgb;
    decode_two_step("region rgb", do_region, &c, &img); free(img.data);
    decode_two_step("region scaled", do_scaled, &c, &img); free(img.data);
    decode_two_step("thumbnail", do_thumb, &c, &img); free(img.data);
    if (stitched_level >= 0) {
        c.level = stitched_level;
        decode_two_step("stitched tile", do_stitched, &c, &img); free(img.data);
    }

    /* associated */
    for (int32_t i = 0; i < si.associated_count; i++) {
        ot_associated_info ai = {sizeof ai};
        OK(ot_associated_info_get(s, i, &ai));
        ot_image q = {sizeof q};
        OK(ot_associated_decode(s, i, &rgb, &q));
        size_t need = (size_t)q.stride * (q.height - 1) + (size_t)q.width * 3;
        ot_image a = {sizeof a};
        a.data = malloc(need); a.capacity = need;
        rc = ot_associated_decode(s, i, &rgb, &a);
        if (rc != OT_OK) print_error("ot_associated_decode", rc);
        printf("  associated[%d] %-10s %dx%d comp %d -> %dx%d\n", i, ai.type, ai.width, ai.height, ai.compression, a.width, a.height);
        free(a.data);
    }

    OK(ot_close(s));
    CHECK(ot_close(s) == OT_ERR_HANDLE, "double close");
    ot_slide_info after = {sizeof after};
    CHECK(ot_slide_info_get(s, &after) == OT_ERR_HANDLE, "use after close");

    if (failures) { fprintf(stderr, "%d failure(s)\n", failures); return 1; }
    printf("OK\n");
    return 0;
}
