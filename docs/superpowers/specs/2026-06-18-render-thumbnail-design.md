# `RenderThumbnail` + `RenderMacro` — rendered slide-preview APIs — design

**Status:** design / implemented
**Date:** 2026-06-18

> This spec covers two sibling additive APIs shipped together: `RenderThumbnail`
> (tissue-extent downscale) and `RenderMacro` (tissue composited at true
> physical scale onto a synthesized slide canvas). `RenderMacro` is specified in
> §7.

---

## 0. Motivation

opentile-go exposes embedded associated images (`AssociatedImages()` →
label / overview / thumbnail / macro, *when present*) but never **generates** a
thumbnail/overview for a slide that lacks one. Consumers (a viewer's slide
gallery; a navigational minimap/overview) need a small whole-slide image
regardless of whether the file embeds one. Today that requires
`Pyramid.ReadRegionScaled(fullL0Region, targetSize)` — which works, but the
caller must read L0's size, pick the longer axis, and compute an
aspect-preserving target, which is easy to get wrong on non-square slides.

`RenderThumbnail` is a thin, discoverable convenience over `ReadRegionScaled`
that owns the fit math.

## 1. API

```go
func (p *Pyramid) RenderThumbnail(bounds Size, opts ...DecodeOption) (*decoder.Image, error)
func (s *Slide)   RenderThumbnail(bounds Size, opts ...DecodeOption) (*decoder.Image, error) // delegates to Pyramid(0)
```

**Sizing — `bounds Size`, where a zero axis means "unconstrained" (derive from
the slide aspect):** (the ImageMagick `256x` / `x256` / `256x256` convention)

| `bounds` | result |
|---|---|
| `{W:256, H:256}` | largest aspect-preserved image fitting **inside** 256×256 (**fit-box**) |
| `{W:256, H:0}`   | **fit-width** — width 256, height from aspect |
| `{W:0, H:256}`   | **fit-height** — height 256, width from aspect |
| `{W:0, H:0}`     | error — must constrain ≥1 axis |

One `Size` parameter expresses fit-width, fit-height, and fit-box with no
separate mode enum. (Exact / non-aspect / stretched-to-box is intentionally NOT
offered — that is already `ReadRegionScaled(fullL0, exactSize)`.)

## 2. Behavior

- **Always renders from the pyramid.** It does NOT fall back to an embedded
  `AssociatedThumbnail` / `AssociatedOverview` — those remain on
  `AssociatedImages()`. Predictable: every slide yields a uniformly-rendered
  tissue-extent thumbnail (good for a gallery). Doc'd clearly.
- **Aspect-preserving, never upscales beyond L0.** If `bounds` exceeds L0, the
  scale is clamped to 1.0 (target = L0 extent). A thumbnail never enlarges.
- **For BIF the render is correctly stitched** (free from v0.46.0 — it goes
  through the layout-aware `ReadRegionScaled`).
- **Best-level sourced + Lanczos-resampled** — inherited from `ReadRegionScaled`
  (reads the smallest pyramid level that satisfies the downsample, so memory is
  bounded even for huge slides; small thumbnails read a tiny top level).
- `opts` pass through (`WithFormat` for RGB/RGBA, etc.). Requires a registered
  decoder for L0's compression (same as `ReadRegionScaled`).
- Output dims are exactly the computed target (`thumbnailTargetSize`).

## 3. Implementation

A new file `thumbnail.go` (package `opentile`):

```go
// thumbnailTargetSize computes the aspect-preserving output size for fitting an
// l0 (W×H) image into bounds, where a zero (or negative) axis in bounds is
// unconstrained. Scale is capped at 1.0 (never upscale). Each axis floors at 1.
func thumbnailTargetSize(l0, bounds Size) (Size, error)
```

`Pyramid.RenderThumbnail`:
1. `l0 := p.Level(0)` (propagate its error).
2. `out, err := thumbnailTargetSize(l0.Size, bounds)`.
3. `return p.slide.imageReadRegionScaled(p.Index, Region{Origin:{0,0}, Size: l0.Size}, out, opts...)`.

`Slide.RenderThumbnail`: `p := s.Pyramid(0); if p == nil { return nil, ErrImageIndexOutOfRange }; return p.RenderThumbnail(bounds, opts...)`.

New error (or reuse): `bounds` constrains neither axis → a descriptive error
(`fmt.Errorf`), since no existing sentinel fits. Empty pyramid / level-0 errors
propagate from `Level(0)`.

## 4. Testing

- **Fixture-free unit tests** (`thumbnail_test.go`, CI-safe) on
  `thumbnailTargetSize`: fit-box (portrait + landscape, each axis binding),
  fit-width, fit-height, no-upscale clamp (bounds > L0 → L0), both-zero error,
  square slide, 1px-floor for extreme downscale.
- **Fixture-gated integration test**: open `CMU-1-Small-Region.svs`, call
  `s.RenderThumbnail(Size{W:256,H:256})`, assert output ≤ 256 on both axes, the
  binding axis ≈ 256, aspect preserved within ±1px, and the image is not
  entirely white. Plus a fit-width and fit-height case. Skips without
  `OPENTILE_TESTDIR`.

## 5. Docs

- README: add `RenderThumbnail` to the API surface / a short usage snippet,
  noting rendered-vs-embedded.
- CHANGELOG `[Unreleased]`: Added — `Slide.RenderThumbnail` / `Pyramid.RenderThumbnail`.
- Additive; no breaking changes; no behavior change to existing APIs.

## 6. Out of scope

- Falling back to / preferring an embedded associated image ("best-available").
  Predictable always-render is simpler; a caller wanting the embedded macro for
  navigation uses `AssociatedImages()` directly. Could be a future
  `BestThumbnail`-style helper if a consumer asks.
- Exact / stretched (non-aspect) output — use `ReadRegionScaled`.
- Caching the rendered thumbnail — caller's concern.

---

## 7. `RenderMacro` — synthesized pseudo-macro (sibling feature)

A viewer needs a macro-style orientation image even for slides that embed no
macro. `RenderMacro` synthesizes one: a slide-shaped canvas (the **non-label
scan area** of a standard slide, ~50×25 mm) with the whole-slide tissue
composited at its **true physical size** and centred.

```go
func (s *Slide) RenderMacro(bounds Size, opts ...DecodeOption) (*decoder.Image, error)
```

- **Canvas = the non-label scan area** (`macroScanWidthMM=50`,
  `macroScanHeightMM=25`), white. The label end is intentionally **excluded**
  (a future option could include/draw it). `bounds` sizes the canvas with the
  same zero-axis fit convention (scan area is ~2:1).
- **Physical scale via `effectiveMPP`:** `Metadata.MPP` if present (a single
  populated axis fills the other); else `10/Magnification` (objective rule of
  thumb — 40×→0.25, 20×→0.5); else **error** (cannot place to scale).
- **Tissue:** physical mm = `L0.Size × mpp / 1000`; rendered to exactly that px
  size via `ReadRegionScaled` (stretch honours anisotropic MPP), blitted
  **centred** on the white canvas. If the tissue exceeds the scan area it is
  scaled to fit, preserving aspect. For BIF the tissue render is stitched.
- **Position:** centred only — true on-slide placement (NDPI offset-from-centre,
  SCN/DICOM/BIF slide-coordinate origin) is a deferred enhancement requiring a
  new cross-format slide-coordinate metadata layer (not built here).

**Helpers (pure, unit-tested):** `effectiveMPP(md) (x,y,err)` and
`fitAspect(aw,ah, bounds) (Size,err)` (like `thumbnailTargetSize` but with no
upscale clamp — the canvas is synthetic).

**Tests:** unit (`fitAspect` fit-box/width/height/upscale/errors; `effectiveMPP`
MPP / objective-fallback / neither-errors) + fixture-gated integration
(canvas 600×300 for `bounds {600}`, composite = white background + centred
tissue at physical scale).

**Out of scope (now):** true position; drawing the label/frosted end;
configurable slide/scan dimensions and colors (sensible fixed defaults for v1).
