# Codec-domain Scale in ScaledStrips + ReadRegionScaled — scope/design

- **Date:** 2026-06-04
- **Status:** SCOPED for next (not started). Follow-up to v0.33 (#10/#12) + v0.34.
- **Relation:** consumer-side use of the `DecodeOptions.Scale` contract that
  #11 unified across decoders. #11 made the *decoders* honor Scale; this makes
  the *downsampling consumers* use it.

## Background

As of v0.34, three tile codecs honor `DecodeOptions.Scale ∈ {1,2,4,8}` —
decode to `ceil(dim/Scale)` directly: `jpeg` (IDCT fast-scale), `jpeg2000`
(`cp_reduce` DWT resolution decode), `htj2k` (`restrict_input_resolution`).
That decode is **faster** (less work), **anti-aliased** (codec low-pass, not a
spatial box), and **seam-free** (per-tile). The library's own downsampling
paths don't yet exploit this for non-JPEG sources.

## Finding 1 — ScaledStrips is JPEG-gated (small win)

`StripIterator` already does codec-domain downscale: `autoIDCTScale`
(`strip_geometry.go:51`) picks a 1/2/4/8 factor and `strip_workers.go:31`
passes it via `WithScale(idctScale)` to `ImageDecodedTile`; the residual is
finished spatially with the configured kernel. But `autoIDCTScale` bails for
anything but JPEG:

```go
// strip_geometry.go:52
if level.Compression != CompressionJPEG {
    return 1
}
```

This gate was correct in v0.26 (only JPEG honored Scale). It is now stale.

**Change:** replace the `!= CompressionJPEG` gate with a scale-capable-codec
predicate (`JPEG | JP2K | HTJ2K`); rename `idctScale` → `codecScale` for
honesty. The rest of the machinery is already codec-agnostic — cache sizing
(`strip_iterator.go:104`, `TileSize/scale`) and the blit assume `ceil(TileSize/
scale)` tiles, which is exactly the dims contract all three decoders satisfy.
Must NOT enable for non-scale-capable levels (uncompressed/webp/avif/jpegxl) —
`WithScale != 1` there returns `ErrUnsupportedScale`. Respect `nohtj2k` (a
CompressionHTJ2K level has no decoder under that tag — fall back to scale 1).

**Benefit:** DZI / dzsave / tile-server strips on SVS-JP2K, DICOM-JP2K/HTJ2K,
and wsi-tools HTJ2K sources get the ~5×/4× decode win + anti-aliased downscale.

## Finding 2 — ReadRegionScaled uses NO codec scale (bigger win)

`ImageReadRegionScaled` (`region_scaled.go:42`) picks the best level via
`ImageBestLevelForDownsample`, reads it at **full** level resolution with
`ImageReadRegion`, then spatially resamples the **entire** residual
(`resample.ImageInto`). It never applies a codec scale — so it leaves the
optimization unused for **every** codec, including JPEG.

`imageReadRegionImpl` (`region.go:69`) assembles tiles at the level's full
resolution; there is no per-tile scaled read path for regions. So unlike
ScaledStrips, the machinery does not yet exist here.

**Change (larger):** give `ReadRegionScaled` a codec-scale stage like the strip
path — choose a `codecScale` for the residual (level→output), decode tiles
scaled, assemble into the scaled-down intermediate, finish spatially. Cleanest
implementation is to share the strip iterator's scaled-tile-assembly rather
than duplicate it (extract a small `scaledRegionInto` helper used by both), or
to route `ReadRegionScaled` through a one-strip `StripIterator`.

**Benefit:** every `ReadRegionScaled` caller (openscope viewport reads, region
extraction) gets faster + sharper downsampling on all scale-capable codecs.

## Finding 3 — stale public doc (trivial)

`decode_options.go:40` `WithScale` says "(JPEG decoders only)" and "Non-JPEG
sources return ErrUnsupportedScale". Both are wrong since v0.34. Update to:
JPEG (IDCT) + JPEG 2000 / HTJ2K (DWT resolution), `{1,2,4,8}`, else
`ErrUnsupportedScale`.

## Verified non-sites

- Associated images (label / macro / overview / thumbnail) are read
  pre-rendered from the file — no library-side downsample.
- No whole-slide `Thumbnail` / overview generator exists.
- `cmd/bench`, `bench/` use raw `DecodedTile` (no auto-downsample).

## Phasing

1. **Finding 3** (doc) — fold into whichever change lands first.
2. **Finding 1** (ScaledStrips generalization) — small, surgical; the headline
   win for the DZI/strip consumers. Do first.
3. **Finding 2** (ReadRegionScaled) — larger (new scaled-region assembly or
   shared helper); do second, ideally sharing machinery with Finding 1.

## Correctness bar

- Existing strip + region-scaled parity tests stay green (the codec-scale path
  must be output-equivalent within resampling tolerance to the
  full-decode-then-resample path — wavelet low-pass ≠ box by design, so
  *close*, not bit-equal, like the v0.33/v0.34 quality tests).
- New tests over a JP2K source (e.g. `JP2K-33003-1.svs`) and a DICOM-JP2K/HTJ2K
  series confirming codecScale > 1 is actually selected and the output is sane.
- `make test` green under `-race`; `nohtj2k` falls back cleanly.
