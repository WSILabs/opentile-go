# NDPI decode performance vs openslide — investigation brief

**Authored:** 2026-05-28 by Claude in the wsitools repo context, handed off to a fresh opentile-go session.

## TL;DR

opentile-go's NDPI single-thread tile-decode throughput is **~5× slower than openslide** on the same hardware and fixtures. ScaledStrips' internal parallelism (NumCPU workers by default) hides this in production pipelines, but the per-thread gap is real and likely fixable. Closing it would benefit every NDPI consumer downstream (wsitools, custom tools).

## Why we care

wsitools v0.17.0 (just shipped) made `convert --to dzi|szi` ~150× faster than its v0.16 path on NDPI sources, and lands at libvips parity on small/medium NDPI (CMU-1.ndpi: wsitools 14.25s vs vips 17.23s — **wsitools faster**). On the large OS-2.ndpi (126976×73728), wsitools is 1.95× libvips's wall time. Investigation of that gap led to comparing the raw NDPI tile-decode throughput of opentile-go vs openslide directly. Findings drove the rest of this brief.

## Measured numbers

Hardware: Apple Silicon Mac, 13 cores. Fixtures from `~/GitHub/opentile-go/sample_files/ndpi/`.

Both benchmarks read every 256×256 tile across L0 sequentially, single-threaded. openslide returns ARGB pixels; opentile-go returns RGB. Throughput normalized to Mpix/s (decoded-pixel count per second).

| Library | Fixture | Tile count | Wall | Throughput |
|---|---|---|---|---|
| openslide 4.0.0 (C) | CMU-1.ndpi (51200×38144) | 29,800 | 8.58s | 227.6 Mpix/s |
| opentile-go v0.26 (Go) | CMU-1.ndpi | 29,800 | 43.34s | 45.1 Mpix/s |
| openslide 4.0.0 (C) | OS-2.ndpi (126976×73728) | 142,848 | 43.94s | 213.1 Mpix/s |
| opentile-go v0.26 (Go) | OS-2.ndpi | (extrapolated) | ~220s | ~45 Mpix/s |

**Ratio: opentile-go is 5.05× slower per thread on CMU-1, 5.0× projected on OS-2.**

Both opentile-go runs are CPU-bound at 96-100% single-thread (`user+sys ≈ wall`); they're not I/O-blocked. The gap is in actual decode work.

## Reproducible benchmarks

The wsitools session created two minimal benchmark programs that should be moved into opentile-go's `internal/bench/` or `cmd/bench/` and committed for future regression tracking.

### openslide reference (`bench-openslide.c`)

```c
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
    uint32_t *buf = malloc(TS * TS * 4);
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
    openslide_close(s);
    free(buf);
    return 0;
}
```

Build: `clang $(pkg-config --cflags --libs openslide) -O2 -o bench-openslide bench-openslide.c`

### opentile-go test subject (`bench-opentile.go`)

```go
package main

import (
    "flag"
    "fmt"
    "os"
    "time"

    opentile "github.com/wsilabs/opentile-go"
    _ "github.com/wsilabs/opentile-go/decoder/all"
    _ "github.com/wsilabs/opentile-go/formats/all"
)

func main() {
    path := flag.String("in", "", "slide path")
    flag.Parse()
    if *path == "" {
        fmt.Fprintln(os.Stderr, "missing -in"); os.Exit(2)
    }
    slide, err := opentile.OpenFile(*path)
    if err != nil { panic(err) }
    defer slide.Close()
    l0 := slide.Images()[0].Levels[0]
    w, h := l0.Size.W, l0.Size.H
    fmt.Printf("source L0: %dx%d\n", w, h)
    const TS = 256
    cols := (w + TS - 1) / TS
    rows := (h + TS - 1) / TS
    start := time.Now()
    var pix int64
    for r := 0; r < rows; r++ {
        for c := 0; c < cols; c++ {
            x := c * TS
            y := r * TS
            tw := TS
            if x+tw > w { tw = w - x }
            th := TS
            if y+th > h { th = h - y }
            img, err := slide.ReadRegion(0, x, y, tw, th)
            if err != nil { panic(err) }
            pix += int64(tw * th)
            _ = img
        }
    }
    el := time.Since(start).Seconds()
    fmt.Printf("%d tiles, %d MiB pixels in %.2f s (%.1f Mpix/s, %.1f MiB/s)\n",
        rows*cols, pix*3>>20, el, float64(pix)/el/1e6, float64(pix)*3/el/1024/1024)
}
```

Build: `go build -o bench-opentile bench-opentile.go`

## Hypotheses for the gap

Listed in rough order of likelihood. Profile before believing any of them.

1. **Strip-stitch synthesis overhead.** NDPI source files are organized as horizontal JPEG strips, not tiles. opentile-go's NDPI reader synthesizes tile-shaped JPEG bytes from strip MCUs on demand. openslide does the same in spirit but in tighter C code. Possible inefficiencies: per-call strip cache lookup, per-tile JPEG header construction.
2. **Go cgo overhead per tile decode.** Each `slide.ReadRegion` ultimately crosses cgo to libjpeg-turbo for JPEG decode. With ~30K tiles and ~1µs per cgo crossing, that's 30ms — a small constant but doesn't explain 35s gap on CMU-1.
3. **Memory allocation churn.** Go allocates `*decoder.Image` per `ReadRegion` call. openslide writes into a caller-supplied buffer.
4. **MCU-aligned JPEG splice cost.** opentile-go has to splice JPEG headers + DC/AC tables + scan data to produce a standalone JPEG per tile from strip bytes. That's per-tile bookkeeping that openslide may avoid via different decode path.
5. **opentile-go's NDPI reader hasn't been profiled.** Possible algorithmic O(N²) somewhere or unnecessary copies.

## Suggested investigation plan

1. **Reproduce the gap.** Build both benchmarks, run on CMU-1.ndpi and OS-2.ndpi. Confirm ~5× ratio.
2. **Profile opentile-go's single-thread NDPI decode.**
   - Add `runtime/pprof` to the bench-opentile program — `pprof.StartCPUProfile` around the loop.
   - Run, generate profile.
   - `go tool pprof -top -lines bench-opentile.prof` — identify top 5-10 hot functions.
   - `go tool pprof -web` for visual call graph.
3. **Identify the dominant cost.** Likely candidates: NDPI strip-cache lookup, JPEG header synthesis, libjpeg-turbo decode, memory allocation.
4. **Compare to openslide's strategy** (cleanroom — read openslide's NDPI loader at high level only, do not copy code). The openslide NDPI reader lives at `src/openslide-vendor-hamamatsu.c` in the openslide source tree.
5. **Implement targeted optimization(s).** Don't fix everything; focus on the biggest hot spot first.
6. **Re-run benchmark.** Confirm improvement.
7. **Ship opentile-go release.** Then wsitools (and other consumers) get the benefit on dep bump.

## Success criteria

- **Stretch:** opentile-go NDPI decode within 1.5× of openslide single-thread (down from 5×).
- **Acceptable:** opentile-go NDPI decode within 2.5× of openslide single-thread.
- Anything below 1× ratio is suspicious — likely benchmark error rather than real win.

## What NOT to do

- Don't focus on multi-threaded throughput as the primary metric. The single-thread number is the cleanest comparison and is what determines a consumer's per-core efficiency.
- Don't optimize JPEG decode itself (libjpeg-turbo is shared between the two). Focus on the surrounding strip-stitch + buffer logic.
- Don't break the public API. Internal restructuring is fine; `*Slide.ReadRegion` and `*Slide.ScaledStrips` signatures should remain stable.

## Reference: opentile-go's NDPI reader

Lives at `formats/ndpi/` (or similar) in this repo. Spec at `docs/superpowers/specs/` (look for the NDPI design doc by date).

Test fixtures live in `sample_files/ndpi/`:
- `CMU-1.ndpi` (188 MB, 51200×38144) — smallest, fastest iteration target.
- `OS-2.ndpi` (931 MB, 126976×73728) — largest, for confirming scaling.
- `Hamamatsu-1.ndpi` (6.9 GB) — skip for iteration; too big.

## How this brief was produced

I (Claude) was working on wsitools v0.17 + v0.18 brainstorming. The user asked me to investigate why wsitools convert is slower than libvips dzsave on large NDPI. Direct comparison of opentile-go vs openslide tile-decode throughput surfaced the 5× per-thread gap. wsitools doesn't experience the full pain because ScaledStrips' internal parallelism hides it, but the gap is real and worth closing upstream.

The fresh opentile-go session inherits no wsitools context — just this brief. wsitools v0.18 will continue on a separate track (skip-blanks parked, picking a non-perf v0.18 item).
