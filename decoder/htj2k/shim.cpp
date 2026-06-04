//go:build cgo && !nocgo && !nohtj2k

// shim.cpp — C wrappers around OpenJPH decode for cgo.
// All OpenJPH C++ state lives inside this file; cgo only sees extern "C"
// declarations via shim.h.

#include <stdint.h>
#include <stdlib.h>
#include <string.h>
#include <exception>

#include <openjph/ojph_codestream.h>
#include <openjph/ojph_params.h>
#include <openjph/ojph_mem.h>
#include <openjph/ojph_file.h>
#include <openjph/ojph_arch.h>
#include <openjph/ojph_message.h>

#include "shim.h"

// wsi_htj2k_dimensions reads only the codestream header.
int wsi_htj2k_dimensions(const uint8_t *src, size_t src_len,
                          int resolution_factor, int *out_w, int *out_h,
                          int *actual_reduce) {
    if (!src || src_len == 0 || !out_w || !out_h || !actual_reduce ||
        resolution_factor < 0) return -1;
    try {
        using namespace ojph;
        mem_infile in;
        in.open(reinterpret_cast<const ui8 *>(src), src_len);

        codestream cs;
        cs.enable_resilience();
        cs.read_headers(&in);

        param_siz siz = cs.access_siz();
        int fw = (int)(siz.get_image_extent().x - siz.get_image_offset().x);
        int fh = (int)(siz.get_image_extent().y - siz.get_image_offset().y);
        // restrict_input_resolution beyond the codestream's decomposition
        // levels FAILS (it does not clamp), so clamp the factor to what the
        // codestream supports; the caller box-finishes the residual.
        int maxr = (int)cs.access_cod().get_num_decompositions();
        int r = resolution_factor > maxr ? maxr : resolution_factor;
        *out_w = (fw + (1 << r) - 1) >> r; // ceil(full / 2^r)
        *out_h = (fh + (1 << r) - 1) >> r;
        *actual_reduce = r;
        cs.close();
        return 0;
    } catch (...) {
        return -1;
    }
}

// wsi_htj2k_encode_test encodes RGB888 as a lossless HTJ2K codestream (test only).
int wsi_htj2k_encode_test(const uint8_t *rgb, int width, int height,
                           int num_decomp,
                           uint8_t **outbuf, size_t *outsize) {
    *outbuf = NULL;
    *outsize = 0;
    if (!rgb || width <= 0 || height <= 0 || num_decomp < 0) return -1;
    try {
        using namespace ojph;
        codestream cs;

        param_siz siz = cs.access_siz();
        siz.set_image_extent(point((ui32)width, (ui32)height));
        siz.set_num_components(3);
        for (ui32 c = 0; c < 3; ++c)
            siz.set_component(c, point(1, 1), 8, false);
        siz.set_image_offset(point(0, 0));
        siz.set_tile_size(size((ui32)width, (ui32)height));
        siz.set_tile_offset(point(0, 0));

        param_cod cod = cs.access_cod();
        cod.set_num_decomposition((ui32)num_decomp);  // resolution levels (>=1 for lossless)
        cod.set_block_dims(64, 64);
        cod.set_reversible(true);   // lossless
        cod.set_color_transform(false); // no YCbCr transform; components stay as-is

        cs.set_planar(false);

        mem_outfile out;
        out.open();
        cs.write_headers(&out);

        ui32 next_comp = 0;
        line_buf *cur = cs.exchange(NULL, next_comp);
        for (int y = 0; y < height; ++y) {
            for (int c = 0; c < 3; ++c) {
                if (!cur || !cur->i32) return -1;
                si32 *target = cur->i32;
                const uint8_t *row = rgb + (size_t)y * width * 3;
                for (int x = 0; x < width; ++x)
                    target[x] = (si32)row[x * 3 + c];
                cur = cs.exchange(cur, next_comp);
            }
        }

        cs.flush();

        si64 sz = out.tell();
        if (sz <= 0) { cs.close(); return -1; }
        *outbuf = (uint8_t *)malloc((size_t)sz);
        if (!*outbuf) { cs.close(); return -1; }
        memcpy(*outbuf, out.get_data(), (size_t)sz);
        *outsize = (size_t)sz;
        cs.close();
        return 0;
    } catch (...) {
        if (*outbuf) { free(*outbuf); *outbuf = NULL; *outsize = 0; }
        return -1;
    }
}

// wsi_htj2k_decode decodes a HTJ2K codestream into packed RGB888.
// Uses pull() which is the decode-side API (exchange() is encode-only).
// Handles both lossless (i32) and lossy (f32) line buffers.
int wsi_htj2k_decode(const uint8_t *src, size_t src_len, int resolution_factor,
                     uint8_t *dst_rgb, size_t dst_stride,
                     int *out_w, int *out_h) {
    if (!src || src_len == 0 || !dst_rgb || !out_w || !out_h || resolution_factor < 0) return -1;
    try {
        using namespace ojph;
        mem_infile in;
        in.open(reinterpret_cast<const ui8 *>(src), src_len);

        codestream cs;
        cs.enable_resilience();
        cs.read_headers(&in);

        // DWT resolution-level downscale: skip the top `r` fine resolutions
        // for both data and reconstruction (1/2^r, anti-aliased). Must be
        // called after read_headers() and before create() (openjph contract).
        int r = resolution_factor;
        cs.restrict_input_resolution((ui32)r, (ui32)r);

        param_siz siz = cs.access_siz();
        int fw = (int)(siz.get_image_extent().x - siz.get_image_offset().x);
        int fh = (int)(siz.get_image_extent().y - siz.get_image_offset().y);
        int w = (fw + (1 << r) - 1) >> r; // ceil(full / 2^r)
        int h = (fh + (1 << r) - 1) >> r;
        *out_w = w;
        *out_h = h;

        if (w <= 0 || h <= 0) { cs.close(); return -1; }
        if (dst_stride < (size_t)(w * 3)) { cs.close(); return -1; }

        // set_planar(false) requests interleaved pull order: for each row,
        // component 0, then 1, then 2 — mirroring the encoder's set_planar(false).
        cs.set_planar(false);
        cs.create();

        // Pull h * num_components lines. Each pull() returns one component line
        // and sets comp_num. With set_planar(false) the order is:
        //   row 0 comp 0, row 0 comp 1, row 0 comp 2,
        //   row 1 comp 0, ...
        ui32 num_comps = siz.get_num_components();
        if (num_comps < 3) { cs.close(); return -1; }

        // Total pulls = h * num_comps.  pull() returns a line and sets comp_num
        // to the component that line belongs to.  We use comp_num (not the loop
        // index) to place samples in the correct channel of the packed RGB row.
        for (int y = 0; y < h; ++y) {
            uint8_t *row = dst_rgb + (size_t)y * dst_stride;
            for (ui32 c = 0; c < 3; ++c) {
                ui32 comp_num = 0;
                line_buf *line = cs.pull(comp_num);
                if (!line || !line->p) { cs.close(); return -1; }
                if (comp_num >= 3) { cs.close(); return -1; }

                // line->size is the ACTUAL sample count of this component row.
                // For a horizontally-subsampled component (e.g. 4:2:2 chroma)
                // it is smaller than w, so we must never index p[] with the
                // luma column x directly — that would read past the line buffer
                // (heap OOB). Map each output column to its source sample by
                // nearest neighbour: sx = x*lw/w. This is the identity when
                // lw == w (the 4:4:4 case wsi-tools emits today), and a correct
                // horizontal upsample when lw < w. Vertical subsampling under
                // interleaved pull is validated separately once a subsampled
                // HTJ2K fixture exists (see DICOM JP2K/HTJ2K milestone).
                const int lw = (int)line->size;
                if (lw <= 0) { cs.close(); return -1; }

                if (line->flags & line_buf::LFT_INTEGER) {
                    // lossless path: i32 samples
                    const si32 *p = line->i32;
                    for (int x = 0; x < w; ++x) {
                        int sx = (int)((long long)x * lw / w);
                        if (sx >= lw) sx = lw - 1;
                        int v = p[sx];
                        if (v < 0) v = 0;
                        else if (v > 255) v = 255;
                        row[x * 3 + comp_num] = (uint8_t)v;
                    }
                } else {
                    // lossy path: f32 samples
                    const float *p = line->f32;
                    for (int x = 0; x < w; ++x) {
                        int sx = (int)((long long)x * lw / w);
                        if (sx >= lw) sx = lw - 1;
                        float fv = p[sx];
                        int v = (int)(fv + 0.5f);
                        if (v < 0) v = 0;
                        else if (v > 255) v = 255;
                        row[x * 3 + comp_num] = (uint8_t)v;
                    }
                }
            }
        }

        cs.close();
        return 0;
    } catch (...) {
        return -1;
    }
}
