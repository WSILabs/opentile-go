# Deferred Issues

Running log of items that are intentionally out of scope for the current milestone or were raised during code review and parked for later. Once this branch lands on a hosting service, triage everything below into real tracked issues and delete the entries as they are filed.

Format per item:
- **ID** — short descriptor
- **Source** — commit or review where the item originated
- **Severity** — Scope / Limitation / Suggestion
- **Disposition** — target milestone or backlog note

---

## 1. Roadmap items (scope-deferred by design)

Not bugs; intentional deferral in the design phase. ✅ = retired (landed in a prior or current milestone).

**v0.4 scope (final):** close every real bug and parity gap on SVS and NDPI that we have a fixture to drive. Landed: L12 (NDPI edge-tile OOB fill), L17 (NDPI label cropH), L6 / R13 (NDPI Map pages). Permanent items (L4, L5, L14) stay documented as design choices.

**v0.6+ scope (revised on 2026-04-26 after the v0.5 ship):** OME TIFF
in v0.6 closes the last upstream-opentile format. After that we venture
beyond upstream's coverage starting with **Ventana BIF** in v0.7 — a
clinically-common Roche / Ventana iScan format that openslide reads but
upstream opentile doesn't. **Leica SCN** and **Generic Tiled TIFF** are
tentative v0.8+ candidates, gated on real-slide demand. **3DHistech
TIFF** (R6) and **Sakura SVSlide** (R15) are parked behind GH issues —
3DHistech is a niche MRXS conversion target we've never encountered in
the wild; Sakura is rare enough to follow the same trigger-driven
deferral as R4/R9.

The methodology shift starting in v0.7: opentile-go leaves the
"port-from-upstream-opentile-byte-for-byte" pattern. Openslide is the
practical reference for BIF / SCN / Generic TIFF, but it's LGPL 2.1, so
we read it for understanding rather than direct port; correctness bar
shifts from byte-parity to pixel-equivalence with openslide on decoded
tiles.

R4 / R9 (SVS corrupt-edge reconstruct + JP2K decode/encode) remain
parked at [#1](https://github.com/cornish/opentile-go/issues/1) until a
real slide motivates the work.

| ID | Feature | Target | Status |
|----|---------|--------|--------|
| R1 | NDPI format support (Hamamatsu) | v0.2 | ✅ landed (Batches 2-7, parity verified) |
| R2 | `internal/jpeg` marker package | v0.2 | ✅ landed (Batch 2) |
| R3 | SVS associated images — label, overview, thumbnail | v0.2 (promoted from v0.3) | ✅ landed (Task 21, `9cd27cb`) |
| R4 | Aperio SVS corrupt-edge reconstruct fix (currently returns `ErrCorruptTile`) | v0.5+ | deferred — see [#1](https://github.com/cornish/opentile-go/issues/1). Originally promoted to v0.4; demoted on 2026-04-26 because none of our local SVS slides exhibit corrupt edges and 12 tasks of cgo + Pillow-port work to deliver a synthetic-fixture-only feature isn't completeness, it's speculation. Issue captures the full upstream algorithm + Go-side dependency tree; trigger to take it on is a real slide that fails on us with `ErrCorruptTile`. |
| R5 | Philips TIFF (sparse-tile filler) | v0.5 | ✅ landed (commits `1ad463c..7e7bde0`, parity verified across 4 fixtures) |
| R6 | 3DHistech TIFF | parked | parked behind GH issue (TBD). MRXS conversion target produced by 3DHistech software; rare in practice. Trigger to take it on is a real slide. Upstream opentile has a ~200 LOC reader; cheap to revive if motivated. |
| R7 | OME TIFF | v0.6 | ✅ landed. Closes the upstream-opentile format set; SubIFD-based pyramid + multi-image deviation + dual-reference parity (opentile-py + tifffile). |
| R8 | BigTIFF support | v0.2 | ✅ landed (Batch 1) |
| R9 | JPEG 2000 decode/encode (currently passes through native tiles; decode matters for associated-image re-encoding and corrupt-tile reconstruct) | v0.5+ | deferred — see [#1](https://github.com/cornish/opentile-go/issues/1). Only consumer is R4; deferred together. Native JP2K tile passthrough (the v0.1+ behaviour) continues to work — decode is only needed for the reconstruct chain. |
| R10 | Remote I/O backends (S3, HTTP range, fsspec equivalents) | out-of-scope; consumers supply `io.ReaderAt` | permanent |
| R11 | Python parity oracle under `//go:build parity` | v0.2 | ✅ landed (Task 25-26, Batch 7) |
| R12 | CLI wrapper | out-of-scope for v1 | permanent |
| R13 | NDPI Map (`mag == -2.0`) pages exposed as associated images | v0.4 | ✅ landed (commit `7ac3f88`, paired with L6). `Tiler.Associated()` now exposes `Kind() == "map"` entries on slides that carry them. |
| R14 | Ventana BIF (Roche / iScan) | v0.7 | ✅ landed (commit range `b2e7f53..f602b20`). New `formats/bif/` package, new `internal/bifxml/` XML walker, `Level.TileOverlap()` interface evolution, blank-tile generator, ScanWhitePoint-filled empty-tile path, JPEGTables splice for legacy iScan. Two parity oracles: tifffile byte-equality on Ventana-1 (DP 200 path), openslide infrastructure shipped + assertion gated to v0.8 (see L19 below). Sampled-tile fixtures committed for both fixtures via `TestSlideParity`. Correctness bar revised mid-implementation: openslide rejects spec-compliant DP 200 BIFs (`Direction="LEFT"`) and may interpret legacy iScan differently, so byte-stable references are tifffile + our own committed sample-tile SHAs rather than openslide pixel-equivalence. |
| R15 | Sakura SVSlide | parked | parked behind GH issue (TBD). Rare format; openslide reads it. Trigger-driven deferral. |
| R16 | Leica SCN | v0.8 (tentative) | BigTIFF-based; common in research microscopy. Openslide reader as reference. Decide based on real-slide demand. |
| R17 | Generic Tiled TIFF | v0.8+ (tentative) | catch-all fallback for unknown vendors with standard TIFF tile layout. Decide based on real-slide demand and whether end users are hitting `ErrUnsupportedFormat` on standards-compliant slides. |
| R18 | Smart Zoom Image (SZI) | post-v0.15 candidate | ZIP container around a Microsoft Deep Zoom Image (DZI) pyramid + `scan-properties.xml` + `associated_images/{label,macro,thumbnail}.jpg`. Open spec by smartinmedia / pathozoom (LGPL + CC-BY); spec PDF at `sample_files/szi/SZI format description - 2018-11-24.pdf` (downloaded 2026-05-08). Two local fixtures: `CMU-1.szi` (1.5 MB, from the spec repo) + `scan_618_grundium_SZI.szi` (709 MB, Grundium-produced). Driven by user's wsi-tools / viewer pipeline targeting Grundium scanner output. Reader would be ZIP central-directory traversal + DZI manifest parser + JPEG passthrough; uncompressed-stored ZIP entries support direct mmap-aliased ReaderAt access (matches v0.9's mmap-default invariant). See **Co-design note (R18 / R19)** below. |
| R19 | Deep Zoom Image (DZI) | post-v0.15 candidate (paired with R18) | Microsoft DZI format — bare directory layout: `<name>.dzi` XML manifest + `<name>_files/<level>/<col>_<row>.<ext>` tile tree. Used by OpenSeadragon and tile-serving infrastructure. Lower demand than SZI as an opentile-go input (DZI is typically *served* rather than *read* — the viewer side already handles it). Co-designed with R18 since SZI is a ZIP wrapper around DZI. See **Co-design note (R18 / R19)** below. |
| R20 | Cross-format `opentile.Metadata` expansion | v0.17 | ✅ landed (see §8k). Hybrid typed + Properties map approach; closes the cross-format API gap surfaced in v0.16 T4 + flagged independently by user's separate consumer. |
| R21 | Cloud Optimized GeoTIFF (COG) first-class support | ✅ retired (COG-WSI portion shipped in v0.19; plain COG deemed permanently YAGNI — opentile-go is WSI-domain, geospatial COG isn't our domain) | A TIFF profile with required structural properties (tiled, internal overviews, IFDs-before-data, optional `GDAL_STRUCTURAL_METADATA` ghost header). **WSI-context COG (COG-WSI)** landed in v0.19 (see §8m) — closes the user's GH Issues #5 + #6 with a strict spec-validated reader pairing the GDAL COG base structure with WSI-domain private tags 65080-87 + `COG_WSI_VERSION` ghost-area marker. **Plain (geospatial) COG** is permanently YAGNI for opentile-go — generic COG files continue to read via `formats/generictiff/` as structurally-valid pyramid TIFFs whenever they meet the v0.10 validator; first-class `Format() == "cog"` labeling + GeoTIFF tag exposure would add no WSI value. Owner seal (v0.19 brainstorm, 2026-05-20): geospatial COG isn't our domain. References: cogeo.org; OGC 21-026 (GeoTIFF 1.1 / Cloud Optimized GeoTIFF). |
| R22 | Cross-format `Writer` typed field on `opentile.Metadata` | v0.20 | ✅ landed in v0.20 (see §8n). `Writer string` carries the file-producer identifier — distinct from `ScannerManufacturer` (scanner OEM) and `ScannerSoftware []string` (broader software stack). Populated by all 10 readers: SVS canonical SoftwareLine + Grundium comma-suffix from v0.18 detection; NDPI Model; Philips raw `DICOM_SOFTWARE_VERSIONS`; OME-TIFF promoting `ome.creator`; Leica SCN primary `<device version>`; BIF iScan `BuildVersion`; IFE encoder identifier; SZI `"<SoftwareName> <SoftwareVersion>"`; generic-TIFF `Software` tag with `"wsitools/<version>"` override; COG-WSI `"wsitools/<WSIToolsVersion>"` from private tag 65084. Properties keys (`ome.creator`, `cog-wsi.wsitools-version`) retained for backward-compat. |
| R23 | Re-apply v0.18 SVS `detectWriter` to COG-WSI files with `WSISourceFormat == "svs"` | ✅ landed in v0.20.1 | Derived bug discovered during v0.20 brainstorm probe: Grundium-source COG-WSI fixtures (e.g., `scan_620_cog-wsi.tiff`, `svs_40x_bigtiff_cog-wsi.tiff`) report `ScannerManufacturer = "Aperio"` because the wsitools writer preserves the source SVS's standard TIFF `Make` tag (which is just the format-vendor label, NOT the writer-vendor — pre-v0.18 behavior). The v0.18 SVS reader fixed this by parsing the comma-suffix from ImageDescription (`"Aperio Image, Grundium Ocus"` → `ScannerManufacturer = "Grundium"`, `ScannerModel = "Ocus"`), but the cogwsi reader (which delegates to generictiff per v0.19 T6 Option A) uses the standard Make tag and doesn't re-run that detection. The detection-source string IS still present in the preserved `Software = [Aperio Image, Grundium Ocus]` field — re-running v0.18's `detectWriter` on Software[0] for COG-WSI files where `Properties["cog-wsi.source-format"] == "svs"` would fix this. ~1-2 tasks: small surgical fix in `formats/cogwsi/metadata.go` or `formats/generictiff` post-processing; fixture JSON updates for affected COG-WSI fixtures. Standalone benefit: matches v0.18 attribution semantics on COG-WSI files derived from non-canonical-Aperio SVS sources. |

---

## 1a. Deviations from upstream Python opentile

Behaviours where opentile-go intentionally differs from upstream
Python opentile 0.20.0. Each entry names the upstream behaviour, our
deviation, and why. This section is the canonical source of truth;
README.md and per-format docs link here.

### NDPI synthesised label (since v0.2)

- **Upstream:** `NdpiTiler.labels` returns empty — Python opentile does
  not surface NDPI labels at all.
- **opentile-go:** synthesises a label by cropping the left 30% of the
  overview page (`formats/ndpi/associated.go::newLabelImage`).
  Disable via `opentile.WithNDPISynthesizedLabel(false)`.
- **Reason:** Aperio-style label affordance is more useful for
  downstream consumers than nothing; opt-out preserves the upstream
  no-label behaviour for callers that need it.
- **Tracking:** L14 in §2 below.

### NDPI Map page surfacing (since v0.4)

- **Upstream:** filters out tifffile's `series.name == 'Map'` pages
  even though tifffile classifies them.
- **opentile-go:** exposes them as `Tiler.Associated()` entries with
  `Kind() == "map"` (single-channel grayscale uncompressed strip
  passthrough).
- **Reason:** the data is in the file and tifffile already classifies
  it; surfacing matches what the underlying TIFF carries. Not opt-out-
  able; slides without a Map page silently produce no map entry.
- **Tracking:** R13 in §1, retired in v0.4 (commit `7ac3f88`).

### Multi-image OME pyramid exposure (since v0.6)

- **Upstream:** in multi-image OME-TIFF files (e.g.
  `Leica-2.ome.tiff` with 4 main pyramids), the base
  `Tiler.__init__` loop silently overwrites `_level_series_index` on
  each match — only the last main pyramid is exposed via the
  legacy single-pyramid API.
- **opentile-go:** exposes all main pyramids via the new
  `Tiler.Images()` API; legacy `Tiler.Levels()` remains as a
  shortcut for `Images()[0].Levels()`. Single-image formats
  (SVS / NDPI / Philips) return a one-element slice.
- **Reason:** encoding upstream's accidental drop in our port would
  bake in an upstream oversight. Not intentional design.
- **Verification:** parity for the dropped Leica-2 main pyramids
  comes via the v0.6 tifffile-based oracle
  (`tests/oracle/tifffile_test.go`); opentile-py oracle covers the
  last-wins-exposed pyramid only.
- **Tracking:** R7 in §1, retired in v0.6.

### OME PlanarConfiguration=2 plane-0-only indexing (since v0.6)

- **Upstream:** silently uses plane 0 only when `PlanarConfiguration=2`
  via flat `y*W + x` indexing into the per-channel-tripled
  TileOffsets / StripOffsets arrays.
- **opentile-go:** mirrors that for byte parity. Other planes are
  inaccessible through our public API.
- **Reason:** matching upstream's plane-0 selection preserves
  byte-parity; exposing the other planes would require either
  decoding + merging (changes our tile-passthrough contract) or
  per-plane bytes (no obvious API). Both Leica fixtures hit this on
  tiled levels.
- **Tracking:** see [`docs/formats/ometiff.md`](formats/ometiff.md).

### OME first-strip-only on multi-strip OneFrame (since v0.6)

- **Upstream:** Python opentile's `_read_frame(0)` consumes only
  strip 0 (plane 0 row 0 on `PlanarConfiguration=2`) and lets
  libjpeg-turbo's `TJERR_WARNING` recover from the truncated scan.
- **opentile-go:** sets `oneframe.Options.FirstStripOnly` on OME
  pages. Our cgo `tjTransform` wrapper distinguishes warning from
  fatal via `tjGetErrorCode` and treats warnings as success when the
  output is populated.
- **Reason:** byte parity for OneFrame levels of OME files (Leica-1
  L2-L4, Leica-2 L2-L5).
- **Tracking:** see [`docs/formats/ometiff.md`](formats/ometiff.md).

### BIF: probability map exposure (since v0.7)

- **Upstream:** Python opentile doesn't read BIF at all; v0.7 is the
  first opentile-go format beyond upstream's coverage. openslide
  exposes only `macro` and `thumbnail` from BIF — the IFD-1 tissue
  probability map (LZW grayscale, spec-compliant DP 200 only) is
  dropped.
- **opentile-go:** surfaces the probability map as
  `AssociatedImage.Kind() == "probability"`. Joins the existing
  `overview` / `macro` / `thumbnail` / `label` / `map` kind taxonomy
  via the new `kind="probability"` enum value.
- **Reason:** the slide author embedded the probability map; throwing
  it away is value loss. Consumers that don't recognise the kind
  iterate-and-skip cleanly via the existing `Associated()` API.
- **Tracking:** see [`docs/formats/bif.md`](formats/bif.md).

### BIF: tile overlap exposed via `Level.TileOverlap()` (since v0.7)

- **Upstream:** Python opentile doesn't read BIF; openslide exposes
  per-pair `TileJointInfo` overlap as `tile_advance_x` /
  `tile_advance_y` properties (level-0 only) and silently composes
  overlapping tiles into the `read_region` output.
- **opentile-go:** new `Level.TileOverlap() image.Point` method on
  the public `Level` interface returns the per-tile-step pixel
  overlap (count-weighted average of all `<TileJointInfo>` entries on
  level 0; zero on pyramid IFDs 1+ which never overlap per spec).
  Non-BIF formats return `image.Point{}` — additive evolution; no
  caller change needed.
- **Reason:** `Level.Tile(c, r)` continues to return raw compressed
  bytes (preserving the byte-passthrough hot path), so the consumer
  needs the overlap value to position tiles correctly. Surface as
  metadata, not as a pixel-space crop.
- **Tracking:** see [`docs/formats/bif.md`](formats/bif.md).

### BIF: non-strict `ScannerModel` acceptance (since v0.7)

- **Upstream:** the Roche BIF whitepaper explicitly mandates
  rejecting any slide whose IFD-0 `<iScan>/@ScannerModel` is not
  `"VENTANA DP 200"` ("Stop processing the BIF-file if the string
  does not match model name").
- **opentile-go:** accepts any iScan-tagged BigTIFF and routes
  internally based on `strings.HasPrefix(scannerModel, "VENTANA DP")`:
  spec-compliant path (DP 200, DP 600, future DP scanners) vs.
  legacy-iScan path (missing attribute, iScan Coreo, iScan HT).
- **Reason:** rejecting legacy iScan slides leaves users with a
  worse-than-openslide experience for that population. Both fixtures
  in our suite pass through Open cleanly with the prefix-match rule.
  DP 600 is not yet validated against a fixture but the prefix
  future-proofs against new DP scanners.
- **Tracking:** see [`docs/formats/bif.md`](formats/bif.md); spec §4
  of the v0.7 design doc.

### Multi-dimensional WSI API addition (since v0.7)

- **Upstream:** Python opentile is 2D-only. Each format's pyramid is
  exposed as a flat list of levels with no Z/C/T axis support.
- **opentile-go:** adds cross-format multi-dim addressing —
  `Level.TileAt(TileCoord)` plus
  `Image.SizeZ/SizeC/SizeT/ChannelName/ZPlaneFocus`. Backward
  compatible: 2D-only formats (SVS / NDPI / Philips) inherit defaults
  from `SingleImage` (returning 1 / 1 / 1 / "" / 0); BIF surfaces
  `IMAGE_DEPTH`-driven multi-Z reads via the new API; OME honestly
  reports `<Pixels SizeZ/SizeC/SizeT>` dimension counts and rejects
  `TileAt(z != 0)` with `ErrDimensionUnavailable` until the per-IFD
  multi-Z reader lands as a separate format-package milestone.
- **Reason:** modern WSI consumers — fluorescence imaging, focal-
  plane viewers, time-series microscopy — need explicit multi-dim
  addressing. Designed cross-format-extensible so OME multi-Z,
  fluorescence, and time-series support land additively without
  re-shaping the API. Q1–Q6 sign-off captured in spec §13 of the
  multi-dim design doc.
- **Tracking:** see [`docs/superpowers/specs/2026-04-29-opentile-go-multidim-design.md`](superpowers/specs/2026-04-29-opentile-go-multidim-design.md)
  + [`docs/superpowers/plans/2026-04-29-opentile-go-multidim.md`](superpowers/plans/2026-04-29-opentile-go-multidim.md).

### Non-TIFF dispatch path (since v0.8)

- **Upstream:** Python opentile dispatches all format detection
  through a single TIFF-parsed entry point — every supported format
  (SVS / NDPI / Philips / OME / 3DHistech) is a TIFF dialect.
- **opentile-go:** adds `FormatFactory.SupportsRaw(io.ReaderAt,
  int64) bool` and `OpenRaw(r, size, *Config) (Tiler, error)` that
  run *before* `tiff.Open`. A `RawUnsupported` zero-impl base
  struct embedded into the existing five TIFF factories returns
  `false` / `ErrUnsupportedFormat` so backward compatibility is
  zero-cost.
- **Reason:** Iris IFE is the first non-TIFF format opentile-go
  reads (v0.8); the table-driven dispatch lets each format own its
  detection rather than wiring magic-byte sniffs into
  `opentile.Open` directly. Future non-TIFF formats (DICOM-WSI,
  vendor-specific containers) drop in additively.
- **Tracking:** see [`docs/superpowers/specs/2026-04-29-opentile-go-ife-design.md`](superpowers/specs/2026-04-29-opentile-go-ife-design.md) §3.

### IFE: TILE_TABLE x_extent / y_extent ignored (since v0.8)

- **Upstream:** the Iris IFE v1.0 spec doc claims `TILE_TABLE.x_extent`
  / `y_extent` carry "image width/height in pixels at top resolution
  layer."
- **opentile-go:** ignores those fields for level dimensions. The
  `cervix_2x_jpeg.iris` fixture (the only public IFE we have)
  stores `x_extent=496, y_extent=345` — exact matches for the
  native layer's `LAYER_EXTENTS.x_tiles=496, y_tiles≈346`, i.e.
  **tile counts, not pixels** as the spec doc claims. The reader
  derives image pixel dims from `native_layer.x_tiles ×
  TileSidePixels` instead.
- **Reason:** spec ambiguity. Either the doc's "pixels" wording is
  wrong or the cervix file is non-conforming; without a second
  fixture we can't tell. Driving size from `LAYER_EXTENTS` is
  unambiguous and matches what the reader needs anyway.
- **Tracking:** T3 gate (commit `d597755`) recorded the surprise.

### Default mmap-backed `OpenFile` (since v0.9)

- **Upstream:** Python opentile uses `open()` semantics — file
  descriptor + pread-style reads. No mmap.
- **opentile-go:** `OpenFile` defaults to memory-mapped tile reads
  via `golang.org/x/exp/mmap`. Tile bytes become userspace memcpy
  from the mapped region; no `pread(2)` syscall per `Tile()` /
  `TileInto()` call. Auto-fallback to `pread` on mmap failure
  (FUSE / network mounts that don't support mapping); explicit opt-
  out via `WithBacking(BackingPread)` for callers that need
  `os.File` truncation semantics.
- **Reason:** universal perf win for the hot path. The cervix
  fixture's serial Tile() went from 22µs to 0.75µs (29×) under
  mmap; SVS / OME / Philips / BIF saw 8-22% wins; only NDPI is
  unchanged because its bottleneck is libjpeg-turbo transcoding,
  not I/O.
- **Failure mode:** SIGBUS on file truncation under an open mmap-
  backed Tiler. WSI files don't get truncated under normal use; if
  your storage allows it, opt out via BackingPread. Documented
  loudly in the `OpenFile` docstring.
- **Tracking:** see [`docs/superpowers/specs/2026-05-01-opentile-go-v09-perf-design.md`](superpowers/specs/2026-05-01-opentile-go-v09-perf-design.md)
  Q2 (sealed 2026-05-01).

### Pool-friendly tile-read API addition (since v0.9)

- **Upstream:** Python opentile's `Tile()` allocates a fresh `bytes`
  object per call.
- **opentile-go:** adds `Level.TileInto(x, y int, dst []byte) (int,
  error)` and `Level.TileMaxSize() int`. The caller pools `dst`
  buffers via `sync.Pool`; `TileInto` writes tile bytes directly
  into the caller's buffer with zero internal allocation on every
  TIFF format and IFE. NDPI / OME OneFrame still allocate their
  internal scratch (per-page assembled frame) but the boundary-
  level allocation is eliminated.
- **Reason:** high-RPS HTTP serving and per-frame desktop viewer
  paint loops generate severe GC pressure under the alloc-per-tile
  contract. The pool path on splice formats (SVS, Philips) achieves
  9-11× speedup and zero allocs after the v0.9 in-place splice
  template lands.
- **Backward compatibility:** existing `Tile()` calls unchanged —
  same signature, same alloc-on-return behavior, same byte output.
- **Tracking:** v0.9 perf design Q5/Q6 (sealed 2026-05-01).

### `Tiler.WarmLevel(i int) error` page-cache pre-warm hook (since v0.9)

- **Upstream:** Python opentile has no equivalent.
- **opentile-go:** adds `Tiler.WarmLevel(i)` for callers that want
  predictable warm-cache latency on slides they're about to read
  intensively (slide-server pre-warm at startup; viewer pre-loading
  the slide the user is about to open). Touches one byte per OS
  page covering the level's tile-data ranges, forcing the kernel
  to populate the page cache.
- **Reason:** under mmap, first-fault latency on cold pages is
  measurable; pre-warming amortizes it. Under pread, the same
  helper does pread-per-page (slower; documented as best-effort).
- **Tracking:** v0.9 perf design Q5 (sealed 2026-05-01).

### Generic-TIFF reader for non-vendor tiled pyramidal TIFFs (since v0.10)

- **Upstream:** Python opentile has no generic-TIFF reader. Its
  factory list is enumerated vendor formats (Aperio SVS, Hamamatsu
  NDPI, Philips iSyntax, OME-TIFF, 3DHistech). Files outside that
  list raise `NotSupportedTilerError`.
- **opentile-go:** adds `formats/generictiff` as a catch-all reader
  registered last in the dispatch order. Activates on any TIFF
  whose IFD layout passes the v0.10 pyramid validator
  (`internal/tiff.ClassifyPyramid`): ≥3 tiled IFDs forming a
  coherent geometric chain (±2% inter-axis, ±5% inter-level scale
  drift), with each IFD on the photometric/sample whitelist
  (RGB / YCbCr / grayscale, 8-bit, JPEG / JP2K / LZW / Deflate /
  None compression). Multi-pyramid files (≥2 leftover tiled IFDs
  >1% of baseline area) are rejected and routed to OME's reader.
- **Reason:** real-world WSI authoring outside Aperio / Hamamatsu /
  Philips is common (Grundium, Roche legacy iScan, vendor-stripped
  derivatives, libtiff-encoded research outputs). A catch-all
  reader makes opentile-go consume any structurally valid pyramid
  TIFF without per-vendor reverse-engineering.
- **Detection conservatism:** when in doubt, return `false` from
  `Supports()`. The dispatch loop falls through to
  `ErrUnsupportedFormat` rather than silently misclassifying a
  vendor-shaped TIFF. Validation thresholds are sealed in the
  v0.10 design spec Q1/Q2/Q7.
- **Format-specific metadata:** `generictiff.MetadataOf(tiler)`
  surfaces a `MicronsPerPixel` derived from
  `XResolution` (282) + `ResolutionUnit` (296) and the
  level-0 IFD's `ImageDescription` (270) verbatim. Cross-format
  `Tiler.Metadata()` populates from `Make` (271) / `Model` (272) /
  `Software` (305) / `DateTime` (306). `Magnification` is always 0
  — generic TIFF has no standard tag for it.
- **Tracking:** [`docs/superpowers/specs/2026-05-01-opentile-go-v10-generic-tiff-design.md`](superpowers/specs/2026-05-01-opentile-go-v10-generic-tiff-design.md)
  (sealed 2026-05-01).

### `"associated"` AssociatedImage Kind value (since v0.10)

- **Upstream:** Python opentile uses three Kind values across its
  format readers: `"label"`, `"overview"`, `"thumbnail"` (SVS
  retains `"overview"` as legacy synonym for macro).
- **opentile-go:** adds `"associated"` as the v0.10 generic-TIFF
  fallback when the heuristic classifier (`formats/generictiff.
  ClassifyAssociated`) can't confidently match a non-pyramid IFD
  to one of the vendor kinds (label / macro / thumbnail). Other
  format readers continue using the established kinds; this is
  generic-only.
- **Reason:** generic TIFFs may carry tiled or stripped IFDs that
  don't match any heuristic (LZW = label, wide-aspect JPEG = macro,
  smaller-square JPEG = thumbnail) but are clearly non-pyramid
  associated images. Returning `"associated"` lets the consumer
  surface them by Bytes() / Size() without forcing a wrong-but-
  plausible label.
- **Tracking:** v0.10 design Q5 (sealed 2026-05-01); documented
  in `image.go`'s `AssociatedImage` interface docstring.

### Leica SCN reader for legacy SCN400/SCN400F output (since v0.11)

- **Upstream:** Python opentile doesn't read SCN.
- **opentile-go:** adds `formats/leicascn` covering Leica's BigTIFF
  dialect for SCN400 / SCN400F scanners. Production discontinued
  ~2015; we ship structural support for the three openslide-testdata
  fixtures (Leica-1, Leica-2, Leica-Fluorescence-1) and lean on
  bio-formats CLI parity for the long tail.
- **Reason:** Leica-Fluorescence-1.scn is the **first real fixture
  exercising `Image.SizeC() > 1`** in opentile-go — tests v0.7's
  multi-channel C-axis API on actual fluorescence data rather than
  synthetic constructions. Leica-2.scn additionally exercises
  multi-region "discontinuous scanning" (one slide, multiple disjoint
  tissue rectangles); the reader composites them into one Image with
  inter-region blank-tile fill.
- **Detection:** BigTIFF + IFD 0 ImageDescription contains the SCN
  schema URN `http://www.leica-microsystems.com/scn/2010/10/01`.
- **Format-specific metadata:** `leicascn.MetadataOf(tiler)` exposes
  CollectionUUID, Barcode, per-Auxiliary illumination + objective,
  per-Region (main scan) slide-physical offset/extent, and per-Channel
  fluorescence filter + exposure metadata.
- **Tracking:** [`docs/superpowers/specs/2026-05-06-opentile-go-v11-leica-scn-design.md`](superpowers/specs/2026-05-06-opentile-go-v11-leica-scn-design.md)
  (sealed 2026-05-06).

---

## 2. Active limitations (open after v0.3)

Real behaviour gaps that have not been closed. Each entry's **Severity**
field declares whether it's a permanent design choice (the library
cannot reasonably address it without breaking compatibility) or a
v0.4 work-item (a real fix is queued for the existing-format
completeness milestone). Items closed during v0.3 are listed in §5.

### L4 — MPP may be absent from some slides' ImageDescription
- **Source:** diagnostic run during Task 20
- **Severity:** Permanent — input-data-dependent. The library cannot fabricate MPP values that the slide doesn't carry; callers must handle the zero case.
- **Detail:** CMU-1 embeds `MPP = 0.4990` in its ImageDescription, but not every Aperio slide does. When absent, `svs.Metadata.MPP` is zero and every Level's `MPP()` returns `SizeMm{0, 0}`. No bug, but a caller who expects non-zero MPP should check `SizeMm.IsZero()` rather than assuming a value.

### L5 — `internal/tiff` peeks at NDPI-specific tag to select IFD layout
- **Source:** Architecture audit (post-Batch 4)
- **Severity:** Permanent — design decision. NDPI files share classic TIFF magic 42 with no header-level distinguisher, so something has to peek to dispatch the NDPI-layout walker. The peek is encapsulated in `sniffNDPI` (see N-9 godoc on that function).
- **Detail:** `internal/tiff/file.go` reads tag 65420 (Hamamatsu NDPI FileFormat) in the first IFD to decide whether to dispatch the NDPI-layout IFD walker. A cleaner split would have `tiff.Open` expose a generic "layout" hint and let format packages drive selection — but that requires callers to know NDPI exists, which inverts the dependency. Revisit only if adding Philips/OME forces a more general dialect-detection scheme.

### L14 — NDPI label synthesis is Go-specific; Python opentile does not expose NDPI labels
- **Source:** Batch 7 parity oracle extension
- **Severity:** Permanent — deliberate Go-side extension. Removable via `WithNDPISynthesizedLabel(false)` (the v0.3 N-5 fix); see `formats/ndpi/synthlabel_test.go` for the opt-out path.
- **Detail:** Python opentile 0.20.0's `NdpiTiler.labels` returns an empty
  list — the upstream library does not surface a "label" AssociatedImage
  for NDPI slides. Our Go implementation synthesizes one by cropping the
  left 30% of the overview page (`formats/ndpi.newLabelImage`,
  `formats/ndpi/associated.go`), matching a convention common in Aperio
  SVS but with no upstream NDPI precedent.

  The Batch 7 parity test accommodates this by treating a zero-length
  Python oracle output as "skip"; downstream consumers inspecting
  `Tiler.Associated()` on an NDPI slide will see a `Kind() == "label"`
  entry our Go side provides and Python's does not. Not a bug, but a
  deliberate Go-side extension that callers switching between the two
  languages should know about.

  *How to fix if we decide it's unwanted:* drop the `newLabelImage` call
  in `formats/ndpi/ndpi.go` Open() so NDPI associated is overview-only.
  Or keep it and add a README note calling it out.

### L19 — BIF: openslide pixel-parity oracle gated, infrastructure shipped (since v0.7)
- **Source:** Batch E execution (T20)
- **Severity:** v0.8 — work item, not permanent. The runner / session
  / protocol all work; the test currently `t.Skip`s with a clear gap
  description. Resolving requires either an opentile-go-side
  AOI-cropped Tile variant or a clearer story about which view is
  authoritative.
- **Detail:** opentile-go's `Tile(col, row)` at level N references
  the **padded TIFF grid** (e.g. OS-1 L5 = 3712×3192) — what the
  TIFF tags actually cover. openslide's `read_region` references the
  **AOI hull** (OS-1 L5 = 3307×2936) — cropped per the spec's
  "padding to top and right" clause. Their `(0, 0)` tiles map to
  different physical regions, so any pixel comparison diverges by
  construction. Anecdotal note from a community reader: openslide is
  also believed to misread *modern* (DP 200+) BIF — Ventana-1's
  `Direction="LEFT"` rejection is one example, and OS-1 readback may
  have its own quirks. Net effect: openslide pixel-equivalence isn't
  load-bearing for v0.7 correctness; tifffile (Ventana-1) +
  committed sample-tile SHAs (both fixtures) are the references that
  matter.

### L20 — BIF: DP 600 and other future "VENTANA DP *" scanners unverified (since v0.7)
- **Source:** v0.7 design spec §10
- **Severity:** v0.8+ — input-data-dependent. The
  `strings.HasPrefix(scannerModel, "VENTANA DP")` rule lands future
  DP scanners on the spec-compliant path automatically; if a real
  DP 600 (or later) fixture surfaces a behavioural difference (Z-stack
  default, varying overlap distribution, new XMP attributes), it'll
  be a deviation to fold into §1a above.

~~### L21 — BIF: volumetric Z-stacks deferred to v0.8+ (since v0.7)~~
**Closed in v0.7 multi-dim closeout (2026-04-29).** Volumetric BIF
slides now expose every focal plane via the new public
`Image.SizeZ()` + `Level.TileAt(TileCoord{Z, ...})` API — see the
multi-dim deviation in §1a + the multi-dim retirement subsection
of §8a below. Synthetic-fixture coverage only (no real volumetric
BIF in `sample_files/`). The work was executed sequentially on
2026-04-29 across 19 plan tasks.

~~### L22 — IFE: METADATA block parsing deferred (since v0.8)~~
**Closed in v0.8 metadata closeout (2026-05-01).** `formats/ife/`
now parses METADATA + ATTRIBUTES + IMAGE_ARRAY + ICC_PROFILE (skips
ANNOTATIONS). `Tiler.Metadata()` surfaces magnification from the
header; `Tiler.ICCProfile()` returns the embedded color profile;
`Tiler.Associated()` exposes the IMAGE_ARRAY entries with normalised
kinds ("thumbnail" / "label" / "overview" / "macro" / "map" /
"probability"; unknown titles surface lowercased). New
`ife.MetadataOf(tiler)` accessor returns IFE-specific fields:
`MicronsPerPixel`, `MagnificationFromHeader`, `CodecMajor/Minor/Build`,
`AttributesFormat`, `Attributes map[string]string`. Cervix surfaces
24 free-form attributes (every original "aperio.*" / "tiff.*" key
the source SVS carried before the Iris re-encode) + a 6064-byte ICC
profile + a 1920×1337 JPEG thumbnail. ANNOTATIONS parsing tracked as
L25 below — fixture-driven; cervix has no annotations.

### L25 — IFE: ANNOTATIONS block parsing deferred (since v0.8)
- **Source:** v0.8 metadata closeout follow-on.
- **Severity:** v0.9+ work — fixture-driven; trigger when a real
  IFE file with annotations surfaces.
- IFE v1.0 defines an ANNOTATIONS block carrying per-region polygon
  / rectangle / ellipse / freehand annotations + grouping metadata.
  The v0.8 reader skips the block (validates the offset is in-bounds
  but doesn't parse contents). Cervix carries
  `annotations_offset == NULL_OFFSET`.
- **Resolution path:** add `ife.Annotation` types + a parse helper.
  Likely a new exported field on `ife.Metadata` so the cross-format
  `opentile.Tiler.Metadata()` stays simple. Estimate: a half day
  given the nesting (ANNOTATION_ENTRY × N + ANNOTATION_BYTES +
  ANNOTATION_GROUP_SIZES + ANNOTATION_GROUP_BYTES).

### L23 — IFE: cross-tool parity vs `tile_server_iris` deferred (since v0.8)
- **Source:** v0.8 IFE design spec §7.
- **Severity:** v0.9+ work — IFE shipped with sample-tile SHA fixtures
  + synthetic-writer unit tests as the correctness bar; cross-tool
  byte-equality vs `tile_server_iris` HTTP output remains a future
  follow-up.
- IFE has no Python analogue (tifffile / opentile-py don't read it),
  so v0.7's tifffile + opentile parity oracles don't port. Coverage
  is `TestSlideParity` SHA hashes against `cervix_2x_jpeg.ife.json`
  + `tests/parity/ife_geometry_test.go` per-fixture pinning + the
  synthetic writer in `formats/ife/synthetic_test.go`. The first
  divergence story (opentile-go produces byte X, consumer Y observes
  byte Z) is debugged from scratch.
- **Resolution path:** if a downstream divergence surfaces, write a
  shell-out runner that fetches tiles from `tile_server_iris` HTTP
  and compares to `Level.Tile(c, r)` byte-for-byte. Same shape as
  the v0.7 openslide oracle, cross-language.

### L24 — IFE: AVIF + Iris-proprietary tile decode is consumer's responsibility (since v0.8)
- **Source:** v0.8 IFE design spec §10 Q9.
- **Severity:** Permanent — design choice. opentile-go is a
  byte-passthrough library by design; AVIF and Iris-proprietary
  codecs are no different from JPEG/JP2K in that respect.
- IFE tiles can be encoded as JPEG (decodable via stdlib),
  AVIF (consumer links libavif or `golang.org/x/image/avif` when
  stdlib gains it), or the Iris-proprietary codec (consumer
  embeds an Iris codec or 501s the request). opentile-go reports
  `CompressionAVIF` / `CompressionIRIS` so consumers know the
  codec without trying decode and discovering by failure.
- **Resolution path:** none. Linking libavif or an Iris codec into
  opentile-go would expand the cgo footprint past `internal/jpegturbo/`
  and break the byte-passthrough contract that v0.1's `Level.Tile`
  established.

### L26 — Generic-TIFF: stripped pyramid IFDs deferred (since v0.10)

- **Source:** v0.10 generic-TIFF design spec Q3 (sealed 2026-05-01).
- **Severity:** Fixture-driven. Stripped pyramid IFDs are unusual
  in WSI — pyramid levels almost always use tiles. The v0.10
  validator routes stripped IFDs into `Others` for the associated-
  image classifier; stripped IFDs that pass none of the heuristic
  rules fall through to `KindAssociated`. Pyramid-stripped support
  would require teaching `tiledImage` to read strip-based pyramid
  levels (orthogonal change to the tile-based hot path).
- **Resolution path:** if a fixture surfaces, port the stripped-
  level read path from `formats/svs/` (which handles SVS's
  stripped associated images already). Update the v0.10 validator
  to admit stripped pyramid IFDs alongside tiled ones; share the
  reader plumbing if practical.

### L27 — Generic-TIFF: multi-pyramid files reject as out-of-scope (since v0.10)

- **Source:** v0.10 generic-TIFF design spec Q7 (sealed 2026-05-01).
- **Severity:** Permanent — design choice; multi-pyramid TIFFs are
  OME's job. The v0.10 validator rejects files with ≥2 leftover
  tiled IFDs above 1% of baseline area
  (`ErrPyramidMultiplePyramid`). OME's reader (which uses SubIFDs
  and OME-XML to enumerate per-image pyramids) handles legitimate
  multi-pyramid files. Files in this state that aren't OME-tagged
  return `ErrUnsupportedFormat` in dispatch.
- **Resolution path:** none in the generic reader; OME-tag the file
  upstream, or add format-specific support if a vendor pattern
  emerges. The Grundium scan_620 fixture is one such file (mixed-
  ratio pyramid that lands here); it's parked as a v0.11 candidate
  in §11.

### L28 — Generic-TIFF: multi-strip JPEG with PlanarConfiguration=2 unsupported (since v0.10)

- **Source:** v0.10 generic-TIFF design spec §6 reader-path table.
- **Severity:** Fixture-driven. The libtiff-default multi-strip
  JPEG layout (a single JPEG split at restart-marker boundaries)
  works via byte concat — verified against CMU-1.stripped.tiff's
  46-strip thumbnail and 27-strip macro. The
  `PlanarConfiguration=2` variant (each strip carries an
  independent JPEG datastream) is OME-specific and excluded by
  spec; OME's own reader handles it.
- **Resolution path:** none expected for the generic reader. If a
  non-OME fixture emerges, the assembly logic from
  `formats/ometiff/oneframe.go` can be lifted into the generic path.

### L29 — Generic-TIFF: pluggable associated-image classifier deferred (since v0.10)

- **Source:** v0.10 generic-TIFF design spec Q6 (sealed 2026-05-01).
- **Severity:** YAGNI. The heuristic classifier
  (`formats/generictiff.ClassifyAssociated`) covers the patterns we've
  seen on real fixtures: LZW = label, wide-JPEG = macro, smaller-
  square JPEG = thumbnail. A pluggable classifier (consumer-
  supplied policy) would add public-API surface (`WithAssociated
  Classifier` Option, classifier interface) for a configuration
  knob no real consumer has asked for.
- **Resolution path:** if a consumer asks for it, expose
  `ClassifyAssociated` (already exported) directly via a `Config`
  Option that overrides the default. The classifier signature is
  already designed for substitution (`func(ifd, baseline tiff.
  PyramidLevelInfo) string`).

### L30 — Leica SCN: multi-Z stack support deferred (since v0.11)

- **Source:** v0.11 SCN design spec §1 "Not in scope".
- **Severity:** Fixture-driven. SCN XML carries `spacingZ` on the
  `<view>` element and supports `<dimension z="N">` attributes
  alongside `r=` and `c=`. Our 3 fixtures all have z=0 only — no
  multi-Z SCN file in the slate. Multi-Z support would extend the
  XML parser to track per-z IFDs and route reads via
  `TileAt(TileCoord{Z, X, Y})` (the v0.7 multi-dim API supports it).
- **Resolution path:** when a multi-Z SCN fixture surfaces, port
  the per-z IFD mapping from the XML's `<dimension z="N" r="K"
  c="C" ifd="K">` entries; reuse the v0.7 multi-dim BIF pattern.

### L31 — Leica SCN: AOI-cropped Tile variant deferred (since v0.11)

- **Source:** v0.11 SCN design spec §1 "Not in scope".
- **Severity:** YAGNI. The composite Image presents the union of
  main-scan rectangles as one slide canvas; consumers wanting a
  per-region AOI view stitch from `leicascn.MetadataOf(tiler).Regions`
  for nm-precise rectangles. No consumer has asked for a built-in
  AOI-crop API.
- **Resolution path:** if a consumer surfaces a use case, expose a
  `Region(i int) opentile.Image` accessor returning a per-region
  Image (one main scan = one Image at its native L0 dimensions).

### L32 — Leica SCN: mismatched-objective regions rejected (since v0.11)

- **Source:** v0.11 SCN design spec §4.2 "Required invariants for
  multi-main composition" (Q5).
- **Severity:** Fixture-driven. Composition requires all main scans
  to share illumination source, objective magnification, and pyramid
  depth (within ±2% per-level resolution similarity). Files violating
  any invariant return `ErrUnsupportedSCN` with a descriptive message.
  None of our 3 fixtures violate; no mismatched-objective SCN file
  has been observed.
- **Resolution path:** if a real-world fixture surfaces with mixed
  objectives, the fix is policy: either (a) accept and pick the
  highest-resolution main as canonical (others surfaced as
  AssociatedImages?), or (b) split into multiple Images. Trigger-
  driven decision when a fixture motivates it.

### L33 — Leica SCN: byte-equality oracle vs bio-formats not feasible (since v0.11)

- **Source:** v0.11 SCN design spec §7 (Q9).
- **Severity:** Permanent — design choice. Bio-formats decodes JPEG
  tiles + re-encodes via its `bfconvert` pipeline; output bytes
  don't match our raw-passthrough output. Structural-equivalence
  parity (series count, per-series dims, SizeC) is the most we can
  assert.
- **Tested via:** `tests/oracle/leicascn_bf_test.go` (build tag
  `bfparity`); confirms our IFD-to-image-pyramid mapping matches
  bio-formats's view of each fixture.

### L34 — Leica SCN: 3-fixture coverage limit (since v0.11)

- **Source:** v0.11 SCN design spec §2.
- **Severity:** Permanent — Leica SCN scanner production stopped
  ~2015. Our 3 fixtures (Leica-1, Leica-2, Leica-Fluorescence-1
  from openslide-testdata) are likely our complete coverage forever.
  Real-world SCN files outside the slate may exhibit edge cases
  (non-tile-aligned regions beyond what tile-snap handles, mixed
  illumination, multi-Z, or other quirks the format permits) that
  surface only when reported.
- **Mitigation:** bio-formats CLI parity (`tests/oracle/leicascn_bf_test.go`)
  catches structural divergences against bio-formats's much larger
  in-the-wild SCN exposure.
- **Resolution path:** none expected proactively. Trigger-driven
  debugging when a divergence surfaces; per-incident fixture or
  policy adjustment.

---

## 3. Reviewer suggestions accepted but not applied

Non-blocking items raised during per-task spec / quality review. The v0.3
batch closed every suggestion in this section; see §6 for the retired list.
Future review cycles re-populate this section.

---

## 4. v0.2 session learnings (process notes)

These are not deferred items — they're observations about how v0.2 development went, kept here so future sessions have context.

- **The "don't guess — read upstream" rule was established mid-session** (commit `993289e`). Codified in CLAUDE.md and in the cross-session memory system (`feedback_no_guessing.md`). Prompted by three separate "guess-and-regret" debugging cycles in v0.2 (NDPI IFD layout, NDPI metadata tag numbers, StripOffsets tag number) and reinforced by two more in Batch 6/7 (NDPI striped vs. oneframe gate on tag 322 — L13; APP14 bytes miscopied — now canonical). Future format work should read upstream first and commit behavior from there.

- **Architecture audit at end of Batch 4** — three minor cleanups landed (NDPI tiler struct consolidated into `tiler.go`, SVS `image.go` renamed to `tiled.go`, stale debug artifacts removed). Full decision trail in commit `cae47cd`.

- **"Does the slide open?" is not a sufficient NDPI smoke test (L13).** The original `formats/ndpi/striped.go` gated on TIFF tag 322 (TileWidth), which NDPI pages never carry. Every pyramid level fell through to `oneFrameImage`'s per-tile whole-level tjTransform — 3000× slower than Python opentile for CMU-1.ndpi L0, but every unit and integration test still passed because they only verified `Open()` + tile counts, never per-tile throughput. Caught during Batch 6 when fixture generation hit a 30-minute timeout on a 188 MB slide. `formats/ndpi/bench_test.go` is now a regression gate that exercises per-tile time under `NDPI_BENCH_SLIDE`.

- **Byte parity is a stronger correctness bar than it first appears (Batch 7).** The parity oracle was originally scoped as a best-effort opt-in check; extending it to `Associated.Bytes()` surfaced two latent divergences in `internal/jpeg/ConcatenateScans` (APP14 byte values and segment ordering) that no other test caught — both produced valid JPEGs, both decoded to correct pixels, both self-consistent inside our own fixtures. Only byte-level comparison against Python opentile surfaced them. Lesson: "decodes to the right pixels" is not synonymous with "correct output" when downstream consumers (wsidicomizer, DICOM pipelines) may hash the output.

- **Parity with an unconfigured upstream has hidden dimensions (Batch 7).** Python opentile 0.20.0's `OpenTile.open(slide, tile_size)` expects `int`, not `(w, h)`; passing a tuple silently worked for SVS and raised `TypeError` from NDPI's adjust_tile_size. Two separate parity agents hit this. The v0.2 runner pins `tile_size: int` and documents the quirk.

- **tjTransform's CUSTOMFILTER callback is not bitwise-deterministic across Go/Python (L12).** Same flags (`TJXOPT_PERFECT | TJXOPT_CROP`), same callback output, same source JPEG → subtly different entropy streams for NDPI edge tiles. Verified our DC coefficient math matches Python's (`round((lum * 2047 - 1024) / dc_quant) = 170` for luminance=1.0 on CMU-1.ndpi L0's DQT=6), AC is untouched on both sides. Root cause unknown; see L12 for v0.3+ debugging plan.

## 5. Retired in v0.3

Items closed during the v0.3 polish-and-test push. One line per ID; see
the named commit's message for the full rationale and the test that
locks the change in.

**Limitations (L-prefix):**

- **L1** — SoftwareLine trailing `\r` (`9d19d48`)
- **L3** — `CompressionUnknown` round-trip via patched in-memory TIFF (`b20c5bf`)
- **L7** — NDPI overview MCU detection (folded into the `MCUSizeOf` helper, `ec3aa2a`)
- **L9** — `jpegturbo.Crop` concurrency stress test (`ecbad22`)
- **L10** — SVS LZW label decode-restitch-encode (`76a7dc9`)
- **L11** — SVS associated-image MCU detection (`ec3aa2a`)
- **L15** — Hamamatsu-1.ndpi sparse fixture (`07dadfa`)
- **L16** — Batched parity oracle runner (`0ec51d1`)
- **L18** — `ConcatenateScans` empty-JPEGTables gate (`31e2b42`)

**Reviewer suggestions:**

- **T1** — `NewTestConfig` to `opentile/opentiletest` (`4e3e7f0`)
- **T2** — `withRegistry` test helper (`6576ff8`)
- **T3 + T6** — IFD-chain helper, real 3-IFD walk, cycle rejection (`90fe92e`)
- **T4** — `tiffCompressionToOpentile` exhaustive coverage (`4360b93`)
- **T5** — Aperio CRLF / whitespace / duplicate-key parser tests (`d4586e1`)
- **A1** — `ErrTooManyIFDs` sentinel (`28b75eb`)
- **A2** — `OpenFile` errors include path (`17a391c`)
- **A3** — `Formats() []Format` introspection helper (`af58f63`)
- **A4** — `Config.TileSize` zero-size semantics documented (`0557c49`)
- **O1** — `walkIFDs` bulk-reads each IFD's body (`b8d7a54`)
- **O2** — `int(e.Count)` 32-bit truncation comment (`a08b4a3`)
- **D1, D2, D3** — `decodeASCII` NUL tolerance, `decodeInline` *byteReader rationale, `Metadata.AcquisitionDateTime` IsZero sentinel (`b6b6234`)
- **I1** — Fold zero-length check into `indexOf` (`cd850a0`)
- **I2** — `walkIFDs` overlapping-IFD limit documented (`b6b6234`)
- **I3** — `Scan` reuses caller-supplied `*bufio.Reader` (`761c3e3`)
- **I6** — Ranged RSTn check in `isStandalone` (`8c241a2`)
- **I7** — `ReplaceSOFDimensions` byte-scan invariant documented (`b6b6234`)
- **I8** — `paddedJPEGOnce` uses `sync.Once` (`1e5c367`)

**N-numbered items from the v0.3 plan:**

- **N-1** — Single canonical SOF dimensions patcher (`3a72914`)
- **N-2** — Segment-walker for SOS / DRI lookup (`2f4378c`)
- **N-3** — Cache luma DC per level via `CropOpts` (`108e6da`)
- **N-4** — DQT lookup walks segments before byte-scan (`d40c6d2`)
- **N-5** — `WithNDPISynthesizedLabel(bool)` option (`cf8889d`)
- **N-6** — Chroma DC=0 visual behaviour documented (`b6b6234`)
- **N-7** — `%w` wrapping audit in `formats/ndpi` (`308d5ad`)
- **N-9** — NDPI sniff cross-cutting peek documented (`b6b6234`)

**N-numbered items closed as not-applicable (architecture changed since plan was written):**

- **I4 / N-? (Assembler)** — Plan proposed an `Assembler` type to precompute the JPEG header prefix per level. NDPI striped already amortises the prefix work via per-frameSize header caching in `getPatchedHeader`; SVS associated `Bytes()` is called rarely enough that per-call overhead doesn't show in profiles. The optimisation didn't apply.
- **I5** — Plan proposed a defensive RSTn count check requiring "exactly 1 RSTn per fragment." Aperio SVS strips legitimately carry multiple internal RSTn markers (one per encoder restart-interval row); enforcing exactly-1 would error on valid inputs.
- **N-10** — Plan proposed inverting striped Tile()'s Crop fall-through to choose the helper up front by geometry. The `extendsBeyond` heuristic is broader than "Crop errored," so the inversion changes which tiles take the OOB-fill code path → different bytes via a different libjpeg-turbo transform path. Reverted with an inline comment naming the divergence (`acc2282`).

---

## 6. Retired in v0.4

Items closed during the v0.4 existing-format completeness milestone.
One line per ID; named commit's message has the full rationale and
the test that locks the change in.

**Limitations (L-prefix):**

- **L6** — NDPI Map pages (`mag == -2.0`) surface as `AssociatedImage` with `Kind() == "map"` (`7ac3f88`, paired with R13). OS-2.ndpi page 11 and Hamamatsu-1.ndpi page 7 exposed as 8-bit grayscale uncompressed strip-passthrough; CMU-1.ndpi unchanged (no Map page). Deliberate Go-side extension paralleling L14 — Python opentile does not expose Map pages even though tifffile classifies them as `series.name == 'Map'`.
- **L12** — NDPI edge-tile OOB fill (`f4c647b`). Control-flow bug, not the "tjTransform CUSTOMFILTER non-determinism" the v0.2/v0.3 entry claimed. `formats/ndpi/striped.go::Tile` now dispatches geometry-first against image size (matching Python's `__need_fill_background` gate at `turbojpeg.py:839-863`); pre-v0.4 it tried plain `Crop` first and silently returned mid-gray OOB fills on edge tiles where Crop happened to succeed. CMU-1 + OS-2 + Hamamatsu-1 NDPI fixtures regenerated; parity oracle's L12 `t.Logf` carve-out removed in the same commit. Every NDPI tile now byte-equal to Python opentile.
- **L17** — NDPI label cropH passes full image height (`bdaa44e`). One-line `cropH` change in `formats/ndpi/associated.go::newLabelImage` plus dropping the now-unused `mcuH` argument. Matches Python's `_crop_parameters[3] = page.shape[0]` at `ndpi_image.py:144`; libjpeg-turbo's `TJXOPT_PERFECT` accepts the partial last MCU row when the crop ends at the image edge. The pre-v0.4 entry's "needs CropWithBackground" advice was wrong (incomplete read of upstream).

**Roadmap items (R-prefix):**

- **R13** — NDPI Map pages exposed as associated images (`7ac3f88`, paired with L6).

**JIT verification gates (Tasks 1-4 of the v0.4 plan):**

- **T1** — JP2K determinism gate (`c27d9f8`): byte-deterministic. R4's done-when bar (when it lands) is byte-parity with Python opentile.
- **T2** — NDPI Map fixture audit (`e6bbcd5`): OS-2 + Hamamatsu-1 carry Map pages; tifffile classifies them, Python opentile chooses not to surface them. Validated the L6 / R13 path.
- **T3** — L12 reproduction shape (`1b651bf`): Case D — control-flow bug in our Go-side dispatch (the plan offered Cases A/B/C; the actual finding was sharper). Drove the L12 fix above.
- **T4** — R4 mechanism audit (`d2fe107`): port notes at `docs/superpowers/notes/2026-04-26-svs-reconstruct-port.md`. Identified that R4 pulls in 4 new pieces of cgo + Pillow-port infrastructure for a feature whose only test would be synthetic. Drove the v0.4 → v0.5+ deferral of R4 / R9 to issue [#1](https://github.com/cornish/opentile-go/issues/1).

**Deferred (not retired, but resolved by punting to a tracked issue):**

- **R4** (SVS corrupt-edge reconstruct) and **R9** (JP2K decode/encode) — moved from v0.4 to v0.5+ on 2026-04-26. Filed as [#1](https://github.com/cornish/opentile-go/issues/1) with the full upstream algorithm, Go-side dependency tree, byte-parity bar, and trigger conditions for picking the work back up. Audit confirmed none of our 5 local SVS slides exhibit the corrupt-edge bug; 12 tasks of speculative cgo work for a synthetic-fixture-only feature didn't pass the project's "fix bugs we can demonstrate, don't write defensive code for hypothetical inputs" rule.

---

## 7. Retired in v0.5

Items closed during the v0.5 Philips TIFF milestone. One line per ID;
named commits' messages have the full rationale and the parity check
that locks the change in.

**Roadmap items (R-prefix):**

- **R5** — Philips TIFF support landed end-to-end (`1ad463c..7e7bde0`).
  New `formats/philips/` package, new `internal/jpegturbo.FillFrame`
  cgo entry point, new `internal/jpeg.InsertTables` (no-APP14 sibling
  to `InsertTablesAndAPP14`). All 4 sample fixtures
  (`Philips-{1,2,3,4}.tiff`) open cleanly: byte-identical to Python
  opentile 0.20.0 across every sampled tile and every associated
  image we expose. Parity oracle slate extended (11 slides total);
  integration suite covers all 12 fixtures (5 SVS + 3 NDPI + 4
  Philips) green on every commit.

**JIT verification gates (Tasks 1-3 of the v0.5 plan):**

- **T1** — `is_philips` detection gate (`f3ac48c`): all 4 Philips
  fixtures match upstream's `software[:10] == 'Philips DP'` AND
  `description[-16:].strip().endswith('</DataObject>')` rule; zero
  false positives across 13 non-Philips fixtures. No detection-rule
  refinement needed.
- **T2** — `FillFrame` determinism gate (`aa49f96`): Python's
  `Jpeg.fill_frame(src, 1.0)` is byte-deterministic across 5 passes
  (sha256 `05c3789cc691d9a207659e250b3fc9c799eca7c5019c4b084a441c4dca9da243`,
  2,364 bytes). Set the v0.5 sparse-tile blank-tile bar to byte
  equality. Cross-check during the FillFrame implementation (commit
  `e5ae3ac`) confirmed our Go `FillFrame` produces the SAME sha on
  the same input.
- **T3** — DICOM XML schema audit (`17cce32`): 11 DICOM_* tags
  inventoried across 4 fixtures. `DICOM_ACQUISITION_DATETIME` and
  `DICOM_DEVICE_SERIAL_NUMBER` absent on 3/4 fixtures (Philips-4
  only); multi-value strings (DICOM_SOFTWARE_VERSIONS,
  DICOM_LOSSY_IMAGE_COMPRESSION_*) are space-separated quoted. Drove
  the metadata parser's per-tag tolerant-of-absence design.

**Mid-task discoveries (where reading upstream changed the design):**

- The v0.5 plan's `computeCorrectedSizes` test expectation was based
  on a misread of `tifffile._philips_load_pages` (assumed N PS
  entries → N corrected sizes including baseline). Reading upstream
  byte-by-byte (the easily-missed `i += 1` at line 6540) corrected
  this: N PS entries → N-1 corrected sizes, with the first PS entry
  calibrating only.
- The synthesised-XML metadata tests passed under a flat
  `encoding/xml` schema, but the real Philips fixtures wrap
  level-specific Attributes inside `PIM_DP_SCANNED_IMAGES > Array >
  DataObject`. Smoke test against real fixtures forced a rewrite to
  a stack-based token decoder mirroring
  `ElementTree.iter('Attribute')`.
- `NativeTiledTiffImage.get_tile` always splices JPEGTables onto
  whatever `_read_frame` returns, including the cached blank tile
  (which already has tables inside `FillFrame`'s input). Result is
  duplicate DQT/DHT segments in the sparse-tile output — JPEG
  decoders accept this. Cross-check against Python at Philips-4 L0
  (0,0) caught our initial single-splice version.

---

## 8n. Retired in v0.20

v0.20 closes R22 — cross-format `Writer string` typed field added
to `opentile.Metadata`. Pure-additive; no API breakage.

**Items shipped:**

- `opentile.Metadata.Writer string` typed field. File-producer
  identifier; distinct from `ScannerManufacturer` (scanner OEM)
  and `ScannerSoftware []string` (broader software stack).
- Per-format population in all 10 readers (SVS canonical + Grundium
  detection from v0.18; NDPI; Philips; OME-TIFF promoting ome.creator;
  Leica SCN; BIF; IFE; SZI combined SoftwareName+Version; Generic-TIFF
  TIFF Software path + wsi-tools override; COG-WSI wsitools/<version>
  from WSIToolsVersion private tag 65084).
- TestCrossFormatMetadata extension: per-fixture Writer substring
  assertion via `wantWriterContains`.

**Architecture invariants preserved:**

- Pure-additive on cross-format Metadata; existing consumers
  unaffected.
- Properties keys (ome.creator, cog-wsi.wsitools-version) retained
  for backward-compat.
- v0.18 SVS writer detection (`writerVendor`) reused for SVS Writer
  population.
- Scanner attribution semantics unchanged (ScannerManufacturer is
  still the scanner OEM; Writer is the file producer).
- v1.0 cut still pending.
- cgo footprint unchanged.

**Deviations retired:**

- R22 (cross-format Writer typed field) — landed.

**Still parked:**

- R19 (bare DZI)
- R23 (re-apply v0.18 SVS detectWriter to Grundium-source COG-WSI)
- Other §11 backlog rows unchanged.

**v0.20 lessons:** the typed/Properties hybrid pattern from v0.17
generalized cleanly to Writer — populate the typed field at the
same site as existing ScannerSoftware population; keep the older
Properties keys for backward-compat at zero extra cost.

**Plan cross-reference:** [`docs/superpowers/plans/2026-05-20-opentile-go-v20-writer-typed-field.md`](superpowers/plans/2026-05-20-opentile-go-v20-writer-typed-field.md).

---

## 8m. Retired in v0.19

v0.19 ships COG-WSI support — closes the user's two GH Issues
[#5](https://github.com/cornish/opentile-go/issues/5) (generic-TIFF
WSI-tag awareness + integer-multiple pyramid ratio relaxation) and
[#6](https://github.com/cornish/opentile-go/issues/6) (dedicated
`cogwsi` reader). Pairs the GDAL Cloud Optimized GeoTIFF base
structure with WSI-domain private tags 65080-87 and a
`COG_WSI_VERSION` ghost-area marker. Also retires R21 fully — see
§1 status update.

**Items shipped:**

- `internal/cog/` — GDAL ghost-area parser: `GhostArea`,
  `ParseGhostArea`, `ParseCOGWSIVersion`, `ErrGhostAreaMalformed`.
  Designed for COG-WSI's primary use + plain-COG forward-compat.
- `internal/tiff` WSI private tag readers — 8 typed accessors on
  `*tiff.Page` covering tags 65080-65087 per COG-WSI spec §5.2.
  Used existing `Page.ASCII` for strings + `Page.scalarU32` for
  LONGs; added a new `Page.doubleTag` helper for DOUBLEs (mirrors
  the existing Float32 accessor pattern).
- `formats/generictiff/` extended with WSI-tag-aware classification
  (Issue #5 part A): wrapper function `ClassifyAssociatedFromPage`
  preserves the existing signature; pyramid build short-circuits
  when every tiled IFD carries `WSILevelIndex`.
- `internal/tiff/classify_pyramid.go::isIntegerMultipleRatio`
  predicate (Issue #5 part B) — accepts clean integer-multiple
  pyramid downscale chains (1×, 2×, 4×, 8×, 16×, and reciprocals).
  Standalone benefit: Aperio / Grundium SVS 4×/2×/2× chains now
  read cleanly via generic-tiff when no vendor reader claims them.
- `formats/cogwsi/` — full Tiler:
  - `Factory.Supports(*tiff.File)` ghost-area dispatch; registered
    AFTER `szi` and BEFORE `generictiff` in `formats/all/all.go`
    so vendor detectors still win, generic-TIFF still catches all.
  - Validation at `Open`: `validateGhost` + `validateIFDs` →
    `ErrNotConformantCOGWSI` sentinel on spec violations.
  - Tiler delegates tile reads to an inner `generictiff.Tiler`
    via Option A wrapper; `UnwrapTiler() opentile.Tiler` exposes
    the inner Tiler so `generictiff.MetadataOf` works through the
    wrapper.
  - Canonical metadata populated from WSI private tags (WSIMPP /
    WSIMPPX/Y / WSIMagnification → cross-format Metadata;
    WSISourceFormat → `Properties["cog-wsi.source-format"]`;
    parsed `COG_WSI_VERSION` → `Properties["cog-wsi.spec-version"]`).
- `opentile.FormatCOGWSI = "cog-wsi"` enum value.
- 10 new fixtures wired into `TestSlideParity` (30 → 40):
  `CMU-1-Small-Region` / `CMU-1` / `JP2K-33003-1` / `scan_620_`
  / `svs_40x_bigtiff` (5 SVS), `Philips-1` (Philips TIFF),
  `Leica-1` (OME-TIFF), `Ventana-1` (BIF), `cervix_2x_jpeg`
  (IFE), `scan_617` (SZI). `CMU-1-Small-Region_cog-wsi.tiff` is
  the full-walk fixture; the other 9 are sampled by default.
- `tests/parity/cogwsi_geometry_test.go` per-fixture geometry
  pin (per-level Size / TileSize / Grid / Compression,
  AssociatedImage Type + sizes, Metadata fields).
- Cross-fixture parity gate confirms each `<source>_cog-wsi.tiff`
  tile bytes equal the original `<source>` file's tile bytes —
  proving the writer's source-format-preserving invariant and our
  reader's byte-passthrough invariant agree.
- `docs/formats/cogwsi.md` — full format doc.

**Architecture invariants preserved:**

- Public API additive (new `FormatCOGWSI` enum value + new
  package + new `cogwsi.ErrNotConformantCOGWSI` sentinel; no
  existing API broken).
- `formats/generictiff` ClassifyAssociated extension via wrapper
  function (minimal diff; existing callers unaffected).
- Lock-free hot path preserved: cogwsi delegates to generictiff,
  which preserves the v0.9 mmap-default + zero-alloc TileInto
  pattern.
- cgo footprint unchanged.
- v1.0 cut still pending.

**Deviations retired:**

- COG-WSI files previously misrouted as generic-TIFF (worked,
  but without canonical WSI metadata or spec validation).
- Aperio / Grundium SVS 4×/2×/2× chains previously rejected by
  the generic-TIFF strict drift check (Issue #5 part B).

**Still parked:**

- R19 (bare DZI) — internal/dzi pre-pares it.
- L19, L20, L23-L25, L26-L29, L30-L34, R4/R6/R9, R15.
- R22 (cross-format `Writer` typed field) — YAGNI without
  explicit consumer ask.

**v0.19 fixture catch-ups (incidental, no reader-side regressions):**

- `scan_620_grundium_TIFF.tiff` geometry expectation flipped
  from buggy 3-level (v0.10 pin) to real 4-level (T3/T4 surfaced
  the discrepancy via the integer-multiple ratio relaxation).
- `Hamamatsu-1.ndpi` + `OS-2.ndpi` gained `scanner_serial`
  metadata expectations (stale since v0.17's NDPI metadata
  expansion).
- `Leica-1.ome.tiff` + `Leica-2.ome.tiff` gained
  `acquisition_rfc3339` metadata expectations (stale since
  v0.17's OME-TIFF metadata expansion).

**v0.19 lessons:** The "is plain COG worth first-classing?"
question got a clean NO answer — opentile-go is WSI-domain, not
geospatial. Better to ship the WSI-context COG profile (COG-WSI)
that closes real consumer issues than to chase a label-only
("`Format() == "cog"`") cosmetic. The wrapper-function approach
for `ClassifyAssociatedFromPage` (preserving the existing
signature) kept the generic-TIFF diff minimal — a generalizable
pattern when extending an internal classifier without breaking
existing callers.

**Plan cross-reference:** [`docs/superpowers/plans/2026-05-20-opentile-go-v19-cog-wsi.md`](superpowers/plans/2026-05-20-opentile-go-v19-cog-wsi.md).

---

## 8l. Retired in v0.18

v0.18 closes a misattribution bug discovered post-v0.17 brainstorm:
SVS files written by non-Aperio scanners (Grundium observed) got
`ScannerManufacturer = "Aperio"` because the SVS reader assumed
format-vendor = writer-vendor. v0.18 detects the actual writer
from ImageDescription first-line + TIFF Software/Make tags;
namespaces Properties keys per detected writer; documents the
recognized writer set explicitly.

**Items shipped:**

- `formats/svs/metadata.go::detectWriter()` — heuristic parser for
  the ImageDescription first line. Patterns recognized:
  - `"Aperio Image Library v..."` → Aperio canonical
  - `"Aperio Image, <name>"` → comma-suffix writer (first word =
    manufacturer; remainder = model)
  - undetected → empty manufacturer; `svs.<key>` Properties fallback
- `parseDescription` updated to use the detected writer; previously
  hardcoded `"Aperio"` removed.
- `ScannerSoftware` now sensibly split (was: single jammed first
  line; now: two-element list for comma-prefix patterns).
- Properties keys namespace per detected writer:
  - `aperio.<key>` for Aperio-written
  - `grundium.<key>` for Grundium-written
  - `svs.<key>` fallback for undetected
- Standardized SVS keys (MPP, AppMag, ScanScope ID, User, Date,
  Time) continue to populate cross-format Metadata regardless of
  writer.
- Fixture JSONs updated for Grundium SVS:
  `scan_620_.json` + `svs_40x_bigtiff.json`
  metadata.scanner_manufacturer flipped from "Aperio" → "Grundium".
- `docs/formats/svs.md` gains "Recognized SVS writers" section
  with the detection table.
- `formats/ometiff/metadata.go` audited (no code change needed):
  Creator is correctly captured as `ome.creator` and kept distinct
  from ScannerManufacturer per v0.18 Q4. Documentation extended
  to make the distinction explicit.

**Architecture invariants preserved:**

- Public API additive (writer detection refines existing
  ScannerManufacturer / ScannerModel / Properties surface; no new
  typed fields).
- Narrow break: consumer code that hardcoded Aperio expectations
  on Grundium SVS files (e.g., test code asserting
  `ScannerManufacturer == "Aperio"` on a Grundium fixture) now
  sees correct attribution and may need updating.
- v0.17 cross-format Metadata structure preserved; per-format
  Metadata structs unchanged.
- v1.0 cut still pending.
- cgo footprint unchanged.

**Deviations retired:**

- Misattribution of Grundium-written SVS files as Aperio (was
  silently bad pre-v0.18).

**Still parked:**

- R19 (bare DZI) — internal/dzi pre-pares it.
- R21 (COG first-class support) — pairs naturally with HTTP-range backing. (Note: R21 was fully retired in v0.19; see §8m.)
- L19, L20, L23-L25, L26-L29, L30-L34, R4/R6/R9, R15.

**v0.18 lessons:** The "writer-vendor vs format-vendor" distinction
is real. SVS is the worst-affected (ad-hoc multi-vendor) but the
pattern (heuristic detection + per-writer namespacing + standardized-key
preservation) generalizes if other formats later need similar
treatment.

**Plan cross-reference:** [`docs/superpowers/plans/2026-05-09-opentile-go-v18-svs-writer-detection.md`](superpowers/plans/2026-05-09-opentile-go-v18-svs-writer-detection.md).

---

## 8k. Retired in v0.17

v0.17 closes R20 — cross-format `opentile.Metadata` expansion.
Hybrid approach: typed additions for what OpenSlide standardizes
(MicronsPerPixel + per-axis X/Y; ImageDescription); flat
`Properties map[string]string` for opentile-go-canonical extensions
(case-number, user-name, scanned-area-mm2, scan-duration-seconds,
comments) and vendor-namespaced passthrough.

**Items shipped:**

- 4 new typed fields on `opentile.Metadata`: `MicronsPerPixel`,
  `MicronsPerPixelX`, `MicronsPerPixelY`, `ImageDescription`
- `Properties map[string]string` for canonical + vendor-namespaced
  extensions
- 5 canonical key constants: `PropertyCaseNumber`, `PropertyUserName`,
  `PropertyScannedAreaMM2`, `PropertyScanDurationSec`, `PropertyComments`
- 2 helper methods: `Metadata.SetMPPSymmetric()`, `Metadata.SetProperty()`
- Every format reader populates the new fields where source data is
  present (SVS, NDPI, Philips, OME-TIFF, BIF, IFE, Leica SCN
  (first-time population), Generic TIFF, SZI)
- Format-specific Metadata structs cleaned up per Q4 Option B:
  cross-format-canonical duplicates removed; raw native
  representations preserved (e.g., SZI's ElapsedTime "0h17m22s"
  string + VendorProperties for SZI-spec convention)
- New `tests/parity/cross_format_metadata_test.go` exercises every
  format's cross-format Metadata population (12 fixtures across 9
  formats; all green)

**Architecture invariants preserved:**

- Public API additive on cross-format struct (existing consumers
  unaffected); narrow break only for struct-literal construction
  of format-specific Metadata structs (struct field reads via
  embedded promotion still work).
- OpenSlide flat-property convention mirrored for the property bag.
- Python opentile precedent for typed cross-format fields preserved
  (Magnification, ScannerManufacturer, etc., and now
  ImageDescription and MPP).
- v0.16 SZI architecture validated: szi.Metadata's previous
  cross-format duplicates now flow through embedded opentile.Metadata
  cleanly.
- v1.0 cut still pending.
- cgo footprint unchanged.

**Deviations retired:**

- R20 (cross-format Metadata gap) — landed.

**Still parked:**

- R19 (bare DZI) — internal/dzi pre-pares it.
- Other §11 backlog items (L19, L20, L23-L25, L26-L29, L30-L34,
  R4/R6/R9, R15) unchanged.

**v0.17 lessons:** the format-specific-struct-cleanup pattern
(Option B from sealed Q4) is generalizable to future cross-format
API expansions. When a new field becomes cross-format, format
packages strip the typed duplicate but preserve raw/native
representations; field promotion keeps consumer code working
without the cross-format-only refactoring break.

**Plan cross-reference:** [`docs/superpowers/plans/2026-05-09-opentile-go-v17-cross-format-metadata.md`](superpowers/plans/2026-05-09-opentile-go-v17-cross-format-metadata.md).

---

## 8j. Retired in v0.16

v0.16 ships Smart Zoom Image (SZI) support — closes R18 from §11
backlog. R19 (bare Deep Zoom Image, filesystem-backed) remains
parked; v0.16 architecture pre-pares it via the new internal/dzi/
package, but no consumer signal yet motivates shipping bare DZI.

**Items shipped:**

- internal/dzi/ — pure DZI manifest parser + tile-coordinate math
  (Manifest, ParseManifest, MaxLevel, LevelDims, GridDims, TilePath).
- formats/szi/ — full Tiler implementation:
  - SupportsRaw byte-level detection (PK\x03\x04 ZIP magic)
  - eager ZIP central-directory parse at Open() (v0.9 lock-free
    invariant honored)
  - uncompressed-stored ZIP entries → SectionReader on the .szi
    file (mmap-aliased fast path preserved)
  - Image / Level with Tile / TileInto / TileReader / TilePrefix /
    TileBodyInto / TileBodyMaxSize / Compression / Size / TileSize
    / Grid
  - Associated images: macro.jpg → "overview" (per v0.15);
    label.jpg → "label"; thumbnail.jpg → "thumbnail"
  - scan-properties.xml parser → cross-format opentile.Metadata +
    SZI-specific szi.Metadata + VendorProperties map
  - szi.MetadataOf(t opentile.Tiler) accessor
- New public enum values: opentile.FormatSZI ("szi"),
  opentile.CompressionPNG.
- 2 new fixtures (CMU-1.szi from spec repo; scan_618_grundium_SZI.szi
  from Grundium scanner). TestSlideParity 30 fixtures (was 28).

**Architecture invariants preserved:**

- Public API additive only (new format reader + new enum values);
  existing consumers unaffected.
- v0.9 mmap-aliased fast path: SZI's uncompressed-stored ZIP
  entries resolve to byte slices over the file, no inflate, no
  copy on the hot path.
- v0.13 splice-prefix model: SZI tiles are self-contained
  JPEG/PNG; TilePrefix() returns nil; TileBodyInto delegates to
  TileInto per the v0.13 non-applicable convention.
- v0.15 Type() canonical naming: SZI's filename macro.jpg maps to
  Type() == "overview" (not exposed as "macro").
- No new active limitations; sparse SZI deferred (spec forbids;
  breadcrumbs left for a future ErrTileMissing sentinel + opt-in
  lenient mode if a real sparse-SZI fixture surfaces).
- v1.0 cut still pending.
- cgo footprint unchanged.

**Deviations retired:**

- R18 (SZI support) — landed; backlog row retires.

**Still parked:**

- R19 (bare DZI) — deferred to v0.17+; internal/dzi/ pre-pares it.

**v0.16 lessons:** the ZIP central-directory eager parse pattern
is a clean fit for the v0.9 lock-free hot-path invariant. Future
ZIP-backed format readers can mirror this shape directly. The
internal/dzi/ extraction validated the co-design intent — only one
backend ships in v0.16, but the pure-function shape lets a future
backend slot in additively.

A v0.16 follow-on item surfaced during T4: the cross-format
opentile.Metadata struct doesn't carry MicronsPerPixel or
ImageDescription, even though every format reader (SVS, Philips,
OME, BIF, IFE, SCN, SZI) has a format-specific equivalent. Tracked
as R20 in §11 below; touches every format reader's Metadata()
implementation, so deferred until a future milestone with explicit
cross-format-API scope.

**Plan cross-reference:** [`docs/superpowers/plans/2026-05-08-opentile-go-v16-szi.md`](superpowers/plans/2026-05-08-opentile-go-v16-szi.md).

---

## 8i. Retired in v0.15

v0.15 is a small naming-cleanup milestone. Renames the
`AssociatedImage.Kind()` method to `Type()` (DICOM ImageType
convention) and aligns every format except Iris IFE on
`"overview"` as the canonical name for the wide-field slide
image. Breaking change; pre-1.0; sole-consumer sign-off.

**Items shipped:**

- `AssociatedImage.Kind() string` → `Type() string` interface
  rename (image.go); 8 format readers + opentile_test.go mock +
  every test call site updated in lockstep.
- `formats/generictiff` constants:
  `KindLabel` / `KindMacro` / `KindThumbnail` / `KindAssociated`
  →
  `TypeLabel` / `TypeOverview` / `TypeThumbnail` / `TypeAssociated`.
  `KindMacro = "macro"` → `TypeOverview = "overview"` flips name
  AND value in one move.
- `formats/leicascn`: `Type() == "macro"` → `Type() == "overview"`
  for the auxiliary `<image>` element. Pre-v0.15 (sealed in v0.11
  Q8) corrected.
- `formats/ife`: untouched. IFE spec defines `LABEL_MACRO` and
  `LABEL_OVERVIEW` as distinct kinds; both preserved.

**Deviations retired:**

- Pre-v0.15 generic-TIFF emitting `"macro"` instead of upstream's
  `"overview"`. v0.10 introduced this; v0.15 corrects.
- Pre-v0.15 Leica SCN emitting `"macro"` instead of upstream's
  `"overview"`. v0.11 introduced this; v0.15 corrects.

**Architecture invariants preserved:**

- Public API broken intentionally (pre-1.0; sole-consumer sign-off);
  semver-respectful via v0.15.0 tag.
- DICOM-standard naming honored (Image Type 0008,0008 value 3 =
  `OVERVIEW`).
- Upstream Python opentile parity restored on Type() naming.
- v1.0 cut still pending.
- cgo footprint unchanged.

**v0.15 lessons:** sweeping renames of method names + constants in
one milestone benefit from per-task isolation: the method rename
(T1) lands first as a pure mechanical sweep across the entire
codebase; the constant + value flip (T2) lands after; the format-
specific value flip for SCN (T3) lands after that; tests adapt
last (T4). This minimizes the "build temporarily broken" window
and makes individual commits cleanly bisectable.

**Plan cross-reference:** [`docs/superpowers/plans/2026-05-08-opentile-go-v15-type-rename.md`](superpowers/plans/2026-05-08-opentile-go-v15-type-rename.md).

---

## 8h. Retired in v0.14

v0.14 is a small additive milestone extending generic-TIFF
compression support to 4 novel tile codecs (WebP / JPEG XL / AVIF /
HTJ2K) plus the registered JP2K tag 34712, all produced by the
user's `wsi-tools` transcoder. No new format support; no breaking
changes.

**Items shipped:**

- 3 new `opentile.Compression` enum values (`CompressionWebP`,
  `CompressionJPEGXL`, `CompressionHTJ2K`); existing
  `CompressionAVIF` reused for tag 60001.
- Validator whitelist (`internal/tiff.validCompression`) accepts
  5 new tag values: 34712, 50001, 50002, 60001, 60003.
- generic-TIFF reader (`tiffCompressionToOpentile`) maps the 5
  new values to the corresponding enum.
- wsi-tools ImageDescription parser populates standard Metadata
  fields (Magnification, ScannerManufacturer, AcquisitionDateTime,
  MicronsPerPixel) when the prefix `wsi-tools/` is present.
- 4 new test fixtures (avif/htj2k/jxl/webp wsi-tools transcodes of
  CMU-1-Small-Region.svs); TestSlideParity total 28 (was 24).

**Architecture invariants preserved:**

- Public API additive only; existing consumers unaffected.
- Byte-passthrough contract preserved: opentile-go reports the
  compression via `Level.Compression()`; consumers ship the right
  decoder. Mirrors v0.8 IFE precedent for AVIF / Iris-proprietary.
- v1.0 cut still pending.
- cgo footprint unchanged.

**v0.14 lessons:** none new. The codec-passthrough pattern from
v0.8 IFE applied cleanly; wsi-tools ImageDescription parsing was
straightforward once gated by the prefix marker.

**Plan cross-reference:** [`docs/superpowers/plans/2026-05-08-opentile-go-v14-novel-codecs.md`](superpowers/plans/2026-05-08-opentile-go-v14-novel-codecs.md).

---

## 8g. Retired in v0.13

v0.13 is the bandwidth-deduplication milestone. No format changes;
no breaking API changes — entirely an additive interface evolution
exposing the JPEG splice prefix and on-disk tile body bytes
separately so client-server consumers can ship the prefix once per
level instead of redundantly on every tile.

**Items shipped:**

- **`opentile.Level.TilePrefix() []byte`** — returns the constant
  per-level JPEG splice prefix (DQT + DHT + optional APP14); nil
  when the level has no shared JPEGTables (tag 347) or is non-JPEG.
  Per-format specializations: SVS (with APP14), Philips / OME tiled
  / leicascn / generictiff (without APP14), BIF (mixed: OS-1 shared,
  Ventana-1 per-tile-embedded → nil). NDPI / IFE / OneFrame levels
  return nil (T1 defaults).
- **`opentile.Level.TileBodyInto(x, y int, dst []byte) (int, error)`**
  — reads on-disk tile bytes WITHOUT applying the splice. On non-
  splice levels, equivalent to `TileInto`. Zero-alloc on the pool
  path for all TIFF formats and IFE.
- **`opentile.Level.TileBodyMaxSize() int`** — upper bound on
  `TileBodyInto` output size. Strictly less than `TileMaxSize()`
  when `TilePrefix()` is non-nil; equal otherwise.
- **`opentile.SpliceJPEGTile(prefix, body []byte) ([]byte, error)`**
  — top-level helper that reconstitutes a complete JPEG from a
  level's `TilePrefix()` + one tile's `TileBodyInto()` output.
  Inserts the prefix at the on-disk tile's SOS boundary. Documented
  for non-Go consumers (web viewer JS reimplementation).
- **`opentile.ErrBadJPEGSplice`** — sentinel error for malformed
  `SpliceJPEGTile` inputs (empty body, missing SOS marker).
- **`tests/parity/tilebody_bench_test.go`** (build tag `benchgate`)
  — Pattern A vs Pattern B L0 bandwidth comparison harness.

**Test impact:**

- Cross-format reconstitution invariant (T5):
  `SpliceJPEGTile(TilePrefix, TileBodyInto)` == `Tile()` verified
  byte-identical on 10 fixtures (SVS, NDPI, Philips, OME, BIF
  Ventana-1 + OS-1, IFE, generictiff, SCN Leica-1 + Leica-2).
- Bench harness (T6): L0 full-walk bandwidth comparison on CMU-1.svs,
  Philips-1.tiff, Leica-1.ome.tiff, Leica-1.scn.

**Architecture invariants preserved:**

- Additive-only API change. Existing consumers using `Tile()` /
  `TileInto()` see no behavior change.
- cgo footprint unchanged at `internal/jpegturbo/`.
- Lock-free hot path preserved; `TileBodyInto` has the same
  concurrency contract as `TileInto`.
- v1.0 cut not committed; v0.13 stays in pre-1.0 territory.

**v0.13 lessons:**

- **Savings depend on fixture-author choice.** Bench results
  confirmed that bandwidth deduplication only applies on slides
  whose encoder used shared JPEGTables (tag 347). Slides with
  per-tile-embedded tables (Ventana-1 BIF, our OME / SCN
  Leica-1 fixture) show 0% Pattern-B savings. Documented honestly
  in `docs/perf.md` "Pattern A vs Pattern B" section.
- **TileBorrow zero-copy aliasing remains parked.** `TilePrefix` is
  the v0.9 §A.3 half-sibling (splice-prefix exposure); the v0.9
  §A.5 companion idea of a zero-copy `TileBorrow` (mmap-aliased
  slice, callee-managed lifetime) is on a different axis (allocation
  vs bandwidth) and is not part of this milestone.

**Plan cross-reference:** [`docs/superpowers/plans/2026-05-07-opentile-go-v13-tile-prefix.md`](superpowers/plans/2026-05-07-opentile-go-v13-tile-prefix.md)
(7 tasks across one batch).

---

## 8f. Retired in v0.12

v0.12 is a focused naming-cleanup milestone. No new format support;
no new features; no API additions — entirely a breaking-API rename
pass consolidating four `docs/deferred.md §11` items.

**Items shipped:**

- **R1 — `striped` → `stripped` terminology rename.** Public NDPI
  API renamed: `formats/ndpi.StripeInfo` → `StripInfo`; 6 field
  renames. File renames: `striped.go` → `stripped.go`,
  `stripes.go` → `strips.go`, `striped_test.go` →
  `stripped_test.go`. Internal `stripedImage` → `strippedImage`
  (and the `newStripedImage` constructor → `newStrippedImage`,
  same root). The `readStripes` function name preserved (sealed
  out of T2 scope per the plan; minor naming legacy that v1.0 may
  revisit).
- **R2 — `FormatPhilips` rename.** Identifier
  `opentile.FormatPhilips` → `FormatPhilipsTIFF`; string value
  `"philips"` → `"philips-tiff"`.
- **R3 — `FormatOME` rename.** Identifier `opentile.FormatOME` →
  `FormatOMETIFF`; string value `"ome"` → `"ome-tiff"`.
- **R4 — Package directory renames.** `formats/philips/` →
  `formats/philipstiff/`; `formats/ome/` → `formats/ometiff/`.
  Package names follow: `package philips` → `package philipstiff`;
  `package ome` → `package ometiff`. Format-doc renames
  (docs/formats/philipstiff.md → philipstiff.md, ome.md → ometiff.md)
  land in T7.

**Test impact:** 6 fixture JSONs (4 Philips + 2 OME) updated to
record the new format strings; all 24-fixture `TestSlideParity`
green post-rename.

**Architecture invariants preserved:**

- Public API is more consistent post-rename: every Format constant
  for vendor-disambiguated formats now follows
  `Format<Vendor><Tag>` (FormatGenericTIFF, FormatLeicaSCN,
  FormatPhilipsTIFF, FormatOMETIFF) plus the unambiguous-vendor
  short forms (FormatSVS, FormatNDPI, FormatBIF, FormatIFE).
- v1.0 cut not committed (per Q1). v0.12 stays in pre-1.0
  territory; subsequent breaking changes remain possible without
  major-version ceremony.
- cgo footprint unchanged.
- No new active limitations.

**Items deferred:** none introduced.

**v0.12 lessons:**

- **BSD sed `\b` reliability**: T1, T3, T4 all surfaced cases where
  `sed -E 's/\bIDENT\b/REPLACEMENT/g'` silently missed identifiers
  on macOS BSD sed. Mitigation: pair every sed pass with a `grep`
  audit and use the Edit tool for surgical fixes when sed misses.

**Migration guide:** see `CHANGELOG.md [0.12.0]` Breaking changes
section for the per-symbol old → new table (T8 commits this).

**Plan cross-reference:** [`docs/superpowers/plans/2026-05-07-opentile-go-v12-naming-cleanup.md`](superpowers/plans/2026-05-07-opentile-go-v12-naming-cleanup.md)
(9 tasks across Batches A–B).

---

## 8e. Retired in v0.11

v0.11 is the Leica SCN milestone — first format reader exercising
`Image.SizeC() > 1` on a real fixture, plus first multi-region
"discontinuous scanning" reader. Folded in: two `formats/generictiff`
validator-cap relaxations to handle real Grundium scanner output.

**Items shipped:**

- **`formats/leicascn` package** — Factory + Detection + Tiler +
  Level + AssociatedImage covering all 3 openslide-testdata SCN
  fixtures. Multi-region composite levels with synthesised blank-
  tile fill for inter-region gaps; multi-channel `TileAt` for
  fluorescence.
- **`opentile.FormatLeicaSCN` Format constant** (`"leica-scn"`).
- **SCN XML parser** (`formats/leicascn/scnxml.go`) — hand-rolled
  walker over `xml.Decoder` tokens. Parses Collection / Image /
  Pixels / Dimension / Channel / scanSettings into Go structs.
  Mirrors `internal/bifxml`'s lenient walker style.
- **Auxiliary/main classifier + multi-main composer**
  (`formats/leicascn/classify.go`) — `IsAuxiliary` (view-extent
  match per Q2), `ComposePyramid` (validates Q5 invariants and
  produces per-level union extent + per-region IFD-per-channel
  tables).
- **Format-specific metadata** (`leicascn.Metadata` + `MetadataOf`)
  exposing CollectionUUID, Barcode, per-Auxiliary illumination +
  objective, per-Region (main scan) slide-physical layout, per-
  Channel fluorescence filter / exposure / CCD-gain metadata.
- **Generictiff validator relaxations (R1 + R2)** —
  `MinLevels: 3 → 1` (admits Grundium scan_619 single-level tiled
  TIFFs); `LeftoverTiledMaxAreaRatio: 0.01 → 0.05` (admits Grundium
  scan_620 mixed-ratio chains where the orphan IFD is 1.56% of
  baseline). Both cap-loosenings; v0.10 fixtures classify
  identically.

**Test coverage:**

- `tests/integration_test.go::TestSlideParity` extended to **24
  fixtures** (was 19 post-v0.10): +2 Grundium + 3 SCN. Full-walk
  SHA fixtures committed for all new files (Leica-1.scn / Leica-
  2.scn are sampled-mode under the >100 MB policy).
- `tests/parity/leicascn_geometry_test.go` — per-fixture geometry
  pinning + per-channel TileAt distinct-bytes check + cross-backing
  parity (mmap default vs pread). Pins all 3 SCN fixtures end-to-
  end including the Leica-2 multi-region union extent
  (44956×139277 px composite L0).
- `tests/parity/generic_geometry_test.go` extended with rows for
  the two Grundium fixtures.
- `tests/oracle/leicascn_bf_test.go` (build tag `bfparity`) —
  bio-formats CLI parity oracle; confirms our IFD-to-image-pyramid
  mapping aligns with bio-formats's view of each SCN file.
- `formats/leicascn/*_test.go` — unit tests for the XML parser,
  classifier, composer, Factory, AssociatedImage, tiledRegion,
  compositeLevel, blank-tile generator, and Tiler. Real-fixture
  coverage on all 3 SCN files.

**Architecture invariants preserved:**

- Public API stable from v0.3. Two new exported Format constants
  (`opentile.FormatLeicaSCN`, the `leicascn` package); the v0.7
  multi-dim API (`Image.SizeC` / `ChannelName` / `TileAt`) is
  reused without additions.
- cgo footprint unchanged at `internal/jpegturbo/`.
- Lock-free hot path preserved.
- Vendor format dispatch order: SCN registers BEFORE generictiff
  (catch-all stays last); existing vendor formats (SVS, NDPI,
  Philips, OME, BIF, IFE) all register before SCN.

**Items deferred (v0.11 design Q-decisions, see §2):**

- **L30** — Multi-Z stack support deferred (no fixture in slate).
- **L31** — AOI-cropped Tile variant deferred (YAGNI; consumers
  composite via `Metadata.Regions`).
- **L32** — Mismatched-objective regions rejected via `ErrUnsupportedSCN`
  (fixture-driven; trigger-driven debug if one surfaces).
- **L33** — Byte-equality oracle vs bio-formats not feasible
  (permanent; structural-equivalence is the achievable bar).
- **L34** — 3-fixture coverage limit (permanent; SCN production
  stopped ~2015).

**v0.11 lessons:**

- **Multi-region tile alignment**: SCN's `<view offsetX/Y>` values
  in nm don't generally tile-align in composite-pixel-space. Naive
  per-tile dispatch (compute composite-px / tile-size, look up
  region, compute region-local tile) breaks when region offsets
  aren't multiples of tile size — Leica-2's region 0 lands 71% of
  a tile off-grid. Resolution: tile-snap region offsets DOWN to
  nearest tile boundary at `compositeLevel` construction. Cost:
  composite position error ≤ one tile (~128 µm at 250 nm/px) —
  pathology-rendering-acceptable. Documented in
  `docs/formats/leicascn.md` "Position imprecision" + inline in
  `formats/leicascn/tiled.go`. Surfaced during T8 implementation;
  not anticipated in the design spec.

- **Spec-implementation divergence on Grundium scan_620 orphan**:
  v0.11 spec §6.2 said the orphan IFD "surfaces as an AssociatedImage
  with `Kind() == 'associated'`". In practice the orphan is a tiled
  IFD, and `formats/generictiff`'s associated reader doesn't handle
  tiled associated images (errors with `errUnsupportedAssociatedShape`,
  silently dropped per the v0.10 spec §6 pattern). Documented in
  `docs/formats/generictiff.md`. Multi-tile-associated remains
  out-of-scope; if a fixture motivates it, lift the SCN auxiliary
  reader's lowest-res-tile-with-splice pattern.

**Plan cross-reference:** [`docs/superpowers/plans/2026-05-06-opentile-go-v11-leica-scn.md`](superpowers/plans/2026-05-06-opentile-go-v11-leica-scn.md)
(15 tasks across Batches A–E).

---

## 8d. Retired in v0.10

v0.10 is the generic-TIFF milestone — first catch-all reader for
tiled pyramidal TIFFs without vendor metadata. No active L items
retired; the milestone adds reader coverage rather than closing
deferred work.

**Items shipped:**

- **`formats/generictiff` package** — Factory + Detection + Tiler +
  Level + AssociatedImage. Registered LAST in the dispatch order
  so vendor format detectors (SVS, NDPI, Philips, OME, BIF) get
  first crack at any TIFF. Activates only when no vendor factory
  matches and the IFD layout passes the v0.10 pyramid validator.
- **`internal/tiff.ClassifyPyramid`** — value-in / value-out
  pyramid validator. Greedy-chain algorithm with ±2% inter-axis
  + ±5% inter-level scale tolerance; multi-pyramid rejection via
  leftover count + area threshold. Used by the generic reader at
  detection and at Open time; reusable by other formats if needed.
- **`formats/generictiff.ClassifyAssociated`** — heuristic kind
  classifier for non-pyramid IFDs. LZW = label, wide-aspect JPEG =
  macro, smaller-square JPEG = thumbnail; fallback `KindAssociated`
  (new "associated" Kind value, see §1a).
- **Multi-strip associated reader paths (T8)** — single-strip
  passthrough; multi-strip uncompressed concat; multi-strip JPEG
  concat (libtiff RST-marker layout); multi-strip LZW decode +
  re-encode (lifted from `formats/svs/lzwlabel.go`'s pattern).
  Multi-strip Deflate and tiled associated images are silently
  dropped per spec §6 — IFD recognised but not exposed.
- **`generictiff.Metadata` + `generictiff.MetadataOf`** — format-specific
  metadata via the established pattern (mirrors
  `svs.MetadataOf`, `bif.MetadataOf`). `MicronsPerPixel` derived
  from `XResolution` + `ResolutionUnit`; `ImageDescription`
  surfaced verbatim; cross-format Metadata fields populate from
  standard TIFF tags (Make / Model / Software / DateTime).
- **`opentile.FormatGenericTIFF = "generic-tiff"`** — new Format
  constant.
- **`"associated"` Kind value** — new fallback in the
  `AssociatedImage.Kind()` taxonomy, used only by the generic
  reader. Documented in `image.go`'s interface docstring.

**Test coverage:**

- `tests/integration_test.go` — `TestSlideParity` extended to 19
  fixtures (5 SVS + 3 NDPI + 4 Philips + 2 OME + 2 BIF + 1 IFE +
  2 generic). Full-walk SHA fixtures committed for both new
  generic files (~2.5 MB JSON each, under the 5 MB cap).
- `tests/parity/generic_geometry_test.go` — per-fixture geometry
  pinning (level dims, tile size, grid, compression) + cross-
  backing byte parity (mmap default vs pread). Mirrors
  `bif_geometry_test.go` / `ife_geometry_test.go`.
- `formats/generictiff/*_test.go` — unit tests for the validator,
  classifier, factory, tiledImage Level, associatedImage
  AssociatedImage, and tiler. Real-fixture coverage on
  CMU-1.tiff + CMU-1.stripped.tiff (T2-generated derivative of
  CMU-1.svs).

**Architecture invariants preserved:**

- Public API stable from v0.3. Three new exported names
  (`opentile.FormatGenericTIFF`, `opentile.CompressionDeflate`, the
  `generic` package itself) and one new Kind value
  (`"associated"`).
- cgo footprint unchanged at `internal/jpegturbo/`.
- Lock-free hot path preserved.
- Vendor format dispatch order preserved: generic registers LAST,
  so vendor detectors continue to short-circuit first.

**Items deferred (v0.10 design Q-decisions, see §2):**

- L26 — stripped pyramid IFDs (fixture-driven; v0.11 candidate).
- L27 — multi-pyramid rejection (permanent; OME's job).
- L28 — multi-strip JPEG with `PlanarConfiguration=2`
  (permanent; OME-specific).
- L29 — pluggable associated-image classifier (YAGNI; first
  consumer ask).

**Items deferred mid-stream (Grundium fixture findings):**

- Single-level tiled TIFF support (Grundium scan_619 fixture)
  — v0.11 candidate; backlog row added 2026-05-05.
- Mixed-ratio pyramid support (Grundium scan_620 fixture) —
  v0.11 candidate; backlog row added 2026-05-05.

**Plan cross-reference:** [`docs/superpowers/plans/2026-05-01-opentile-go-v10-generic-tiff.md`](superpowers/plans/2026-05-01-opentile-go-v10-generic-tiff.md)
(15 tasks across Batches A–E).

---

## 8c. Retired in v0.9

v0.9 is the sole-focus performance milestone. No active L items
retired; no new format support; no API removals. The work is the
five §A items from `docs/opentile-go-svs-perf.md`, all shipped:

**Performance items shipped:**

- **A.1 mmap-backed `OpenFile` (default)** — `WithBacking(BackingMmap |
  BackingPread)` option; auto-fallback to pread on mmap failure;
  explicit opt-out via `BackingPread`. Single dep on
  `golang.org/x/exp/mmap` for cross-platform Linux + macOS + Windows
  coverage.
- **A.2 `Level.TileInto(x, y, dst)` + `Level.TileMaxSize()`** —
  pool-friendly tile-read API. Existing `Tile()` retained as a thin
  wrapper for casual callers.
- **A.3 In-place JPEG splice template** —
  `internal/jpeg.BuildSplicePrefix` + `internal/jpeg.InsertPrefixInPlace`
  cache the tablesMid + APP14 prefix per level; TileInto's splice
  path is now zero-alloc. Initially deferred at T7 per CPU% gate;
  reversed at owner review after a bytes/ns analysis revealed the
  alloc-churn cost the CPU profile didn't capture (see §10a).
- **A.4 `Tiler.WarmLevel(i int) error`** — page-cache pre-warm hook.
  Walks tile-byte ranges; touches one byte per OS page. Mmap path:
  forces kernel readahead. Pread path: pread(1)-per-page best-effort.
- **A.5 Concurrency-contract docs** — Tiler / Level docstrings now
  pin the per-format lock characteristics, the mmap SIGBUS-on-
  truncation contract, and the dst ownership rules for TileInto.

**Headline measured deltas vs v0.8 baseline (warm-cache pool TileInto,
darwin/arm64 Apple M4):**

| Fixture | v0.8 Tile() | v0.9 pool TileInto | Speedup |
|---|---:|---:|---:|
| Cervix IFE | 22,065 ns | 152 ns | 145× |
| Leica OME | 4,286 ns | 376 ns | 11× |
| **CMU-1.svs** | **1,583 ns** | **99.7 ns** | **16×** |
| **Philips-1** | **6,473 ns** | **425 ns** | **15×** |
| Ventana-1.bif | 26,003 ns | 3,225 ns | 8× |
| CMU-1.ndpi (par) | 182k ns | 185k ns | ~same (CPU-bound) |

**Allocs/op on the pool TileInto path: 0 across every TIFF format
and IFE.** NDPI's stripe reassembly path drops from 12 → 4 allocs
but stays internal-scratch-allocating by design.

**Architecture invariants preserved:**

- Public API stable from v0.3. Five new exported names
  (`opentile.Backing`, `opentile.BackingMmap`, `opentile.BackingPread`,
  `opentile.WithBacking`, `opentile.ErrMmapUnavailable`) plus three
  new interface methods (`Level.TileInto`, `Level.TileMaxSize`,
  `Tiler.WarmLevel`).
- Behavior change: `OpenFile` defaults to mmap. SIGBUS on file
  truncation documented loudly; auto-fallback covers mmap-unsupported
  filesystems. Owner sign-off captured in spec §11 Q2.
- cgo footprint unchanged at `internal/jpegturbo/`.
- Lock-free hot path preserved; concurrency contract for each format
  documented in the Tiler / Level interfaces.

**Plan cross-reference:** [`docs/superpowers/plans/2026-05-01-opentile-go-v09-perf.md`](superpowers/plans/2026-05-01-opentile-go-v09-perf.md)
(13 tasks across Batches A–F).

---

## 8b. Retired in v0.8

Items closed during the v0.8 Iris IFE milestone — the first non-TIFF
format opentile-go reads, the first format beyond what's even
adjacent to upstream Python opentile's coverage (BIF in v0.7 was
WSI but not in upstream's slate; IFE is bleeding-edge with no
upstream comparison at all). Branch `feat/v0.8`.

**Roadmap items (R-prefix):**

- **R18** — Iris IFE support landed end-to-end. New `formats/ife/`
  package (FILE_HEADER + TILE_TABLE + LAYER_EXTENTS + TILE_OFFSETS
  parsing, layer-ordering inversion, sparse-tile sentinel via
  `ErrSparseTile`, encoding-enum mapping). Plumbing refactor
  (`FormatFactory.SupportsRaw` + `OpenRaw` + `RawUnsupported` base)
  ships alongside; new `Compression` enum values `CompressionAVIF`
  and `CompressionIRIS` surface so consumers know what they're
  getting. One real fixture (`cervix_2x_jpeg.iris`, 2.16 GB,
  JPEG-encoded). Round-trips through `opentile.OpenFile` cleanly:
  9 levels native-first, 256×256 tiles, JPEG SOI markers on every
  decoded tile.

**Active limitations (L-prefix):**

- **L22** — IFE METADATA block parsing. **Closed mid-v0.8
  (2026-05-01).** `formats/ife/` gained a full metadata reader
  covering METADATA + ATTRIBUTES + IMAGE_ARRAY + ICC_PROFILE.
  `Tiler.Metadata() / ICCProfile() / Associated()` all populate
  for IFE; new `ife.MetadataOf(tiler)` exposes the IFE-specific
  bag (MPP, codec version, free-form attributes map).
  ANNOTATIONS parsing carried forward as L25 — fixture-driven.
- **L23** (cross-tool parity), **L24** (AVIF/Iris decode), and
  the new **L25** (ANNOTATIONS) tracked in §2 above.

**JIT verification gates (Batch A of the v0.8 plan):**

- **T1** — magic bytes + endianness gate (`16907ef`):
  `cervix_2x_jpeg.iris` first 4 bytes are `0x73 0x69 0x72 0x49`,
  assemble as LE-uint32 to `0x49726973` exactly. Confirmed
  upstream's claim.
- **T2** — FILE_HEADER structure offsets gate (`f17ecee`): all 38
  bytes parse cleanly; `extension_major=1`, `file_size=2,161,105,409`
  matches stat, `tile_table_offset` and `metadata_offset` both
  within file bounds.
- **T3** — LAYER_EXTENTS layer ordering gate (`d597755`): 9 layers,
  scales `[1, 2, 4, 8, 16, 32, 64, 128, 256]` strictly increasing —
  coarsest-first as the design's §6 inversion logic requires.
  **One surprise**: TILE_TABLE `x_extent=496, y_extent=345` look
  like tile counts (matches native layer's x_tiles=496) rather
  than pixels; reader filed against L22's neighbor and ignores the
  TILE_TABLE extents in favor of `LAYER_EXTENTS` math.
- **T4** — TILE_OFFSETS sparse-sentinel gate (`3f4b336`): 228,958
  entries = sum-of-tile-counts across all 9 layers; 40+24-bit
  encoding decodes cleanly; cervix has zero sparse entries
  (fully-tiled). Probe handles absence gracefully.

**Mid-task discoveries (where the cervix surprised us):**

- TILE_TABLE.x_extent / y_extent are tile counts, not pixels (T3).
  Logged as a deviation in §1a; reader derives image dims from
  `LAYER_EXTENTS` instead.
- Tile bytes are JPEG-prefixed (`ff d8 ff e0 ... JFIF ...`) rather
  than the abbreviated-scan format SVS / BIF use. Confirms the
  spec's claim that IFE tiles are self-contained — no JPEGTables
  splice needed in the reader, distinct from every other format
  opentile-go has shipped.
- Layer ordering inversion was the highest-risk gate (T3) but
  passed cleanly; §6 stands.

**Architecture invariants preserved:**

- Public API stable. Five new exported names (`opentile.FormatIFE`,
  `opentile.RawUnsupported`, `opentile.ErrSparseTile`,
  `opentile.CompressionAVIF`, `opentile.CompressionIRIS`).
- `FormatFactory` interface evolved additively — existing factories
  embed `RawUnsupported` to inherit defaults; no caller change.
- cgo footprint unchanged at `internal/jpegturbo/`. AVIF and Iris
  codecs are consumer's call.
- Lock-free hot path preserved — IFE Tiler builds metadata at Open
  time; `Tile()` is direct ReadAt against the cached `tileOffsets`
  slice.

**Plan cross-reference:** [`docs/superpowers/plans/2026-04-29-opentile-go-v08-ife.md`](superpowers/plans/2026-04-29-opentile-go-v08-ife.md)
(19 tasks across Batches A–E).

---

## 8a. Retired in v0.7

Items closed during the v0.7 Ventana BIF milestone. The branch
spans `b2e7f53..f602b20` (30 commits across Batches A through F).

**Roadmap items (R-prefix):**

- **R14** — Ventana BIF support landed end-to-end. New
  `formats/bif/` package (detection, generation classification by
  `strings.HasPrefix(scannerModel, "VENTANA DP")`, IFD
  classification by ImageDescription content, serpentine remap,
  empty-tile blank fill, JPEGTables splice), new
  `internal/bifxml/` package (XMP walker for `<iScan>` and
  `<EncodeInfo>`), `Level.TileOverlap()` interface evolution.
  Two real fixtures (Ventana-1 spec-compliant + OS-1 legacy iScan)
  Open cleanly with full-tiler exposure: format, levels, associated
  images, ICC, metadata, MetadataOf.

**Active limitations (L-prefix):**

- **L21** — BIF volumetric Z-stacks. **Closed in v0.7 multi-dim
  closeout (2026-04-29).** Multi-Z BIF reads now exposed via
  `Image.SizeZ()` + `Level.TileAt(TileCoord{Z, ...})` — see the
  multi-dim retirement subsection below.

Carried forward to v0.8+: L19 (openslide pixel-parity gap,
infrastructure-only), L20 (DP 600 unverified — fixture-dependent).

**Mid-task discoveries (where execution surfaced design surprises):**

- **Both real fixtures have NON-ZERO TileOverlap on level 0**
  (Ventana-1 L0=(2,0); OS-1 L0=(18,26)) — the design spec §10's
  claim of "fixture-untested overlap path" was wrong. The notes file
  §2 reported "all zero" based on a 1500-char XMP truncation; the
  full XMP carries a sparse mix of zero and non-zero `<TileJointInfo>`
  entries. The weighted-average path is therefore exercised by real
  data, not just synthetic tests. Updated v0.7 spec §10 + this
  retirement note.
- **OS-1 has no ICC profile** (tag 34675 present with count=0) —
  the notes file §2 claimed the tag was present on both fixtures.
  T18 distinguishes "tag-absent" from "tag-present-with-zero-bytes"
  and returns nil for both.
- **OS-1 has no FrameInfo / Frame XMP elements** at all (predates
  that XMP feature). T7's `internal/bifxml` parser handles this
  gracefully via empty `Frames` slices; T13's serpentine algorithm
  works without Frame data on this fixture.
- **opentile-go's image extent ≠ openslide's image extent for OS-1**
  (3712×3192 vs 3307×2936 at L5). opentile-go reports the padded
  TIFF grid; openslide reports the AOI hull. Their `(0, 0)` tiles
  reference different physical regions. Recorded as L19; resolution
  deferred to v0.8.
- **Two correctness bugs caught by writing the integration test
  (T19), not by any synthetic unit test:**
  (a) `loadEncodeInfo` was silently swallowing
  `bifxml.ParseEncodeInfo`'s Ver<2 error, defeating the
  spec-mandated rejection gate;
  (b) `bif.MetadataOf` didn't unwrap the file-closer Tiler returned
  by `opentile.OpenFile`, so `MetadataOf(opentile.OpenFile(slide))`
  always returned `(nil, false)`.

**Process notes:**

- v0.7 was executed sequentially (no subagents) for Batches C/D/E/F
  per user instruction — the user is on remote control and the
  back-and-forth latency of subagent dispatch + dual-stage review
  was a worse fit than direct execution. Subagents (haiku for
  mechanical tasks, sonnet for design-aware tasks) were used for
  Batches A and B only; review-cycle catches there were a count
  typo (T1), an OME-XMP→BIF-XMP typo in the T4 outcome paragraph,
  and an over-prescriptive sentence in T5. Useful but not
  load-bearing.

### v0.7 multi-dim closeout (2026-04-29)

After the initial v0.7 BIF milestone landed (commits
`b2e7f53..f602b20`), the user grew v0.7's scope to cover
cross-format multi-dimensional WSI abstractions — driven by L21
(BIF Z-stacks) plus the forward-looking goal of supporting OME
multi-Z, fluorescence, and time-series WSI without re-shaping the
public API again.

Design + plan: `docs/superpowers/specs/2026-04-29-opentile-go-multidim-design.md` +
`docs/superpowers/plans/2026-04-29-opentile-go-multidim.md`. 19
plan tasks across 5 batches, executed sequentially.

**Active limitations closed:**

- **L21** — BIF volumetric Z-stacks. Multi-Z BIF reads now exposed
  via `Image.SizeZ()` + `Level.TileAt(TileCoord{Z, ...})`.
  Synthetic-fixture coverage (no real volumetric BIF in
  `sample_files/`).

**API additions (cross-format infrastructure):**

- `TileCoord{X, Y, Z, C, T}` struct + `ErrDimensionUnavailable`
  error sentinel.
- `Level.TileAt(TileCoord) ([]byte, error)` on the `Level`
  interface — additive; existing `Tile(x, y)` unchanged.
- `Image.SizeZ() / SizeC() / SizeT() / ChannelName(c) /
  ZPlaneFocus(z)` on the `Image` interface — additive; defaults
  on `SingleImage` return 1 / 1 / 1 / "" / 0.
- `bif.Metadata.ZSpacing + ZPlaneFoci` for format-specific
  consumers reaching through `bif.MetadataOf(tiler)`.

**Format-specific additions:**

- BIF: per-IFD `IMAGE_DEPTH` (32997) read at construction; tile
  array stride `Z * (cols*rows) + serpIdx`; `<iScan>/@Z-spacing`
  drives `ZPlaneFocus(z)` per BIF whitepaper §"Whole slide imaging
  process" layout (Z=0 nominal, Z=1..nNear near focus,
  Z=nNear+1..N-1 far focus).
- OME: honest `<Pixels SizeZ/SizeT>` reporting; `<Channel>` element
  count discriminates `SizeC()` from `<Pixels SizeC>` (= per-pixel
  RGB sample count, not separately-stored channels — both Leica
  fixtures correctly report `SizeC() == 1`).

**Mid-task discoveries:**

- T2 surfaced the `<Pixels SizeC>` vs `<Channel>`-count
  discrimination question. Without it, every brightfield OME
  fixture would have been misreported as 3-channel
  multi-fluorescence.
- T4 + T8: Distinguishing `ErrDimensionUnavailable` (axis doesn't
  exist on this slide) vs `ErrTileOutOfBounds` (axis exists but
  index past size) caught a subtle BIF check that initially
  returned the wrong sentinel for `imageDepth=1` + `Z=1`.
- T12: existing `formats/ome/metadata_test.go` golden assertions
  needed updates because the parser previously dropped
  SizeZ/C/T silently — exposing them honestly was a behavioural
  change at the test surface.

**OME deferral (carried forward):**

- Multi-Z OME `TileAt(z != 0)` returns `ErrDimensionUnavailable`
  until the per-IFD reader lands. The dimensions are surfaced
  honestly so consumers can detect multi-Z OMEs and gracefully
  fall back. Implementation strategy documented in
  `docs/formats/ometiff.md`'s "Future implementation strategy"
  subsection.

**Test fixtures:**

All 16 existing 2D fixtures pass `TestMultiDimCompat2D` (Tile and
TileAt byte-identical at level-0 (0,0); SizeZ/C/T == 1; non-zero
Z/C/T returns `ErrDimensionUnavailable`). BIF multi-Z synthetic
fixtures cover 1, 3, 5 plane stacks plus edge cases (depth=0/1,
depth=4 even, zero spacing). 11 multi-Z-specific tests in
`formats/bif/multiz_test.go`.

---

## 8. Retired in v0.6

Items closed during the v0.6 OME TIFF milestone. One line per ID;
named commits' messages have the full rationale and the parity check
that locks the change in.

**Roadmap items (R-prefix):**

- **R7** — OME TIFF support landed end-to-end. New `formats/ome/`
  package, new `internal/tiff` SubIFD support (`Page.SubIFDOffsets`
  + `File.PageAtOffset`), new public API (`Image` interface +
  `Tiler.Images()`), shared `internal/oneframe` package factored
  from NDPI. Both Leica fixtures (Leica-1, Leica-2) open cleanly:
  byte-identical to Python opentile 0.20.0 + tifffile across every
  Image / level / sampled position we expose. Multi-image deviation
  exposes Leica-2's 4 main pyramids; the 3 dropped by upstream are
  byte-validated via the new tifffile oracle.

**Public API additions:**

- `Image` interface (Index / Name / Levels / Level / MPP).
- `Tiler.Images() []Image` — always ≥ 1 entry; multi-image OME
  exposes multiple. Existing `Tiler.Levels()` / `Level(i)` continue
  to work as documented shortcuts to `Images()[0]`.
- `opentile.SingleImage` helper used by SVS / NDPI / Philips for the
  one-element wrapper.
- `opentile.FormatOME` constant.

**Internal additions:**

- `internal/tiff.TagSubIFDs` constant (TIFF tag 330) +
  `Page.SubIFDOffsets()` accessor.
- `internal/tiff.File.PageAtOffset(off)` for SubIFD traversal.
  `scalarU32` falls through to `Values64` for BigTIFF LONG8/IFD8
  scalar values (caught while wiring SubIFD reads on Leica
  fixtures — `ImageWidth` / `ImageLength` were silently failing).
- `internal/oneframe/` package — factored from
  `formats/ndpi/oneframe.go`; serves NDPI + OME (and likely v0.7
  BIF). New `Options.FirstStripOnly` flag for OME multi-strip
  planar pages.
- `internal/jpegturbo` cgo wrapper now distinguishes `TJERR_WARNING`
  from fatal via `tjGetErrorCode`; treats warnings as success when
  the output is populated. Required for OME OneFrame's truncated
  scan data; NDPI parity preserved.

**JIT verification gates (Tasks 1-5 of the v0.6 plan):**

- **T1** — `is_ome` detection gate (commit `b2950e6`): both Leica
  fixtures match `description[-10:].strip().endswith('OME>')`; zero
  false positives across 15 non-OME fixtures.
- **T2** — SubIFD parsing audit (commit `2b0a6cc`): SubIFDs reachable
  via tifffile's `series.levels`. Discovery: OneFrame levels are
  dominant (3 of 5 in Leica-1, 4 of 6 per Leica-2 main pyramid).
- **T3** — OneFrame factor decision (commit `72ba57f`): factor.
  NDPI's `oneframe.go` body is already format-agnostic.
- **T4** — OME-XML schema audit (commit `c807f8e`): both fixtures
  use namespace 2016-06; uniform µm units; uint8 type.
- **T5** — tifffile splice-replication harness (commit `e170766`):
  OME files have no shared JPEGTables on tested fixtures; opentile-py
  output is byte-identical to tifffile raw bytes for tiled levels.
  Refined parity strategy.

**Mid-task discoveries (where reading upstream changed the design):**

- **PlanarConfiguration=2 plane-0-only indexing**: discovered when our
  initial OME tile reader rejected Leica-1 L0 with "tile table
  mismatch: offsets=16416 ... grid=72x76". Python opentile silently
  uses plane 0 only via flat indexing; we matched for byte parity
  and added a deviation note.
- **Multi-strip OneFrame**: Leica-1 L2 has 7206 strips (3 planes × 2402
  rows); Python's `_read_frame(0)` consumes only strip 0. Forced the
  shared `oneframe.Options.FirstStripOnly` flag.
- **`tjTransform` warning vs fatal**: libjpeg-turbo raises
  TJERR_WARNING ("premature end of data segment") on OME OneFrame
  inputs whose SOF claims full-page dimensions but only strip 0 of
  scan data is present. Python tolerates the warning silently; we
  added warning-vs-fatal discrimination via `tjGetErrorCode`.

---

## 9. Gate outcomes (live)

JIT verification gate outcomes from the v0.4, v0.5, v0.6, and v0.7 plans.
Each gate decides a done-when bar or fix path for subsequent tasks.

### v0.7 multi-dim gates

#### Task 3 — BIF Z-spacing parsing gate

- **Date:** 2026-04-29
- **Outcome:** Both BIF fixtures carry `<iScan Z-layers="1"
  Z-spacing="1" ...>`. `internal/bifxml.IScan` already exposes
  `ZLayers int`; the parser already lists `"Z-layers"` in
  `knownIScanAttrs` and writes it into `s.ZLayers`. **`Z-spacing`
  is NOT yet a field on `IScan`** — the attribute is consumed by
  the parser (so it doesn't fall into `RawAttributes`) but no
  typed field receives it. T7 (Batch C) adds
  `IScan.ZSpacing float64` and the per-attribute case in
  `parseIScanAttrs`. Both fixture values (Z-spacing=1) are
  meaningless for single-plane scans (ZLayers=1); the field is
  load-bearing only when the synthetic multi-Z fixture builder in
  T10 produces a Z-stacked test slide. Add the field + parse +
  test in T7 before the BIF-side levelImpl wiring in T8.

#### Task 2 — OME `<Pixels>` SizeZ/C/T extraction gate

- **Date:** 2026-04-29
- **Outcome:** Both Leica fixtures (Leica-1.ome.tiff: 2 Images;
  Leica-2.ome.tiff: 5 Images) carry `<Pixels SizeZ="1" SizeC="3"
  SizeT="1" DimensionOrder="XYCZT">` on every Image. **`<Pixels
  SizeC=3>` describes RGB sample-count, NOT separately-stored
  channels** — every Image has exactly one `<Channel>` element,
  meaning the underlying tile bytes are a single composite RGB
  JPEG (the standard brightfield pathology layout). The right
  discriminator for `Image.SizeC()` (the new v0.7 multi-dim
  accessor) is **`<Channel>` element count**, not `<Pixels SizeC>`.
  T12 (Batch D) wires this through: `pyramidImage.SizeC()` reads
  the `<Channel>` count rather than `<Pixels SizeC>`. The current
  `omePixels` decode struct in `formats/ome/metadata.go` only
  parses SizeX/SizeY; it must grow `SizeZ/SizeC/SizeT` fields and
  the parser must additionally count `<Channel>` elements per
  Image. With this rule, every existing Leica fixture reports
  `SizeZ=SizeC=SizeT=1` — no false-positive multi-dim
  reclassification of brightfield slides.

#### Task 1 — IMAGE_DEPTH (32997) accessor gate

- **Date:** 2026-04-29
- **Outcome:** `internal/tiff.Page.ImageDepth()` returns `(1, false)` for every page on every fixture in the 17-fixture local set (5 SVS, 3 NDPI, 1 generic TIFF, 4 Philips TIFF, 2 OME-TIFF, 2 BIF — pages range from 2 (Leica-1) to 12 (OS-2.ndpi, OS-1.bif)). No fixture carries a volumetric Z-stack. Confirms (a) the accessor is fault-free on real data, (b) the BIF multi-Z code path **must be exercised exclusively via in-code synthetic fixtures** in `formats/bif/multiz_test.go` per the design spec Q5 sign-off. The choice to embed synthetic Z-stack fixtures in test source rather than committing binary testdata stands.

### v0.7 gates

#### Task 1 — Detection gate (`<iScan` substring)

- **Date:** 2026-04-27
- **Outcome:** 2 BIF fixtures matched (`<iScan` substring found in IFDs 0 and 2 on both Ventana-1.bif and OS-1.bif), 0 false positives across 15 non-BIF fixtures (5 SVS, 3 NDPI, 1 generic TIFF, 2 OME-TIFF, 4 Philips TIFF). All BIF fixtures are BigTIFF as expected. Confirms substring `<iScan` is sufficient and specific for detection — aligns with openslide's approach (line 328 of `src/openslide-vendor-ventana.c` checks `strstr(xml, INITIAL_XML_ISCAN)`). No detection-rule refinement needed; ready for format reader implementation.

#### Task 2 — ScannerModel prefix gate

- **Date:** 2026-04-27
- **Outcome:** Both fixtures probe as expected per spec §4 and §5.2. Ventana-1.bif reports `ScannerModel="VENTANA DP 200"` (matches prefix `"VENTANA DP"`) → routes to spec-compliant path. OS-1.bif has no ScannerModel attribute in the XMP (`<iScan>` element; attribute missing) → routes to legacy-iScan path. The `strings.HasPrefix(scannerModel, "VENTANA DP")` classification rule is confirmed as sufficient and specific to distinguish Ventana (spec-compliant) from legacy iScan (non-Ventana) scanners. Aligns with openslide's branching logic and the v0.7 design's §4 scope. No spec revision needed; gate passes.

#### Task 3 — IFD-classification-by-description gate

- **Date:** 2026-04-27
- **Outcome:** All IFD ImageDescription values across both BIF fixtures match the spec §5.3 discriminator rule exactly. No unexpected values; no case/whitespace variants requiring special handling.

  **Per-fixture IFD inventory:**

  | Fixture | IFD | ImageDescription | Role | Notes |
  |---------|-----|------------------|------|-------|
  | Ventana-1.bif | 0 | `'Label_Image'` | associated, kind="overview" | spec-compliant (DP 200) |
  | Ventana-1.bif | 1 | `'Probability_Image'` | associated, kind="probability" | spec-compliant |
  | Ventana-1.bif | 2–9 | `level=N mag=M quality=95` | pyramid levels 0–7 | 8 levels total |
  | OS-1.bif | 0 | `'Label Image'` | associated, kind="overview" | legacy (space separator) |
  | OS-1.bif | 1 | `'Thumbnail'` | associated, kind="thumbnail" | legacy variant |
  | OS-1.bif | 2–11 | `level=N mag=M quality=90` | pyramid levels 0–9 | 10 levels total (OS-1 has deeper pyramid) |

  **Discriminator coverage:** The five discriminator patterns from spec §5.3 (`Label_Image`, `Label Image`, `Probability_Image`, `Thumbnail`, `level=N mag=M quality=Q`) account for 100% of observed IFDs. Ventana-1.bif spans the spec-compliant path (DP 200 with probability map); OS-1.bif exercises the legacy-iScan path (label as space-delimited, no probability, thumbnail instead). Both confirm that **classification by ImageDescription content is sufficient** — no need to index into IFD order, validating spec §5.3's core recommendation despite OS-1's non-standard IFD layout (IFD 0 = label, IFD 1 = thumbnail, IFD 2+ = pyramid, unlike the whitepaper's IFD 0/1/2/3+ layout).

#### Task 4 — Empty-tile gate (offset-zero / bytecount-zero marker)

- **Date:** 2026-04-27
- **Outcome:** Both fixtures carry zero empty tiles across all pyramid levels. Whitepaper spec (§3, "AOI Positions") confirms empty-tile encoding: `TileOffsets[i] == 0` AND `TileByteCounts[i] == 0` for unscanned tiles. Probe verified: zero mismatches (no partial-empty case where one field is 0 and the other is non-zero).

  **Per-fixture, per-IFD tile inventory:**

  | Fixture | IFD | Role | Total tiles | Empty tiles | Notes |
  |---------|-----|------|-------------|-------------|-------|
  | Ventana-1.bif | 2 | L0 | 504 | 0 | main pyramid base |
  | Ventana-1.bif | 3 | L1 | 132 | 0 | |
  | Ventana-1.bif | 4 | L2 | 36 | 0 | |
  | Ventana-1.bif | 5 | L3 | 9 | 0 | |
  | Ventana-1.bif | 6 | L4 | 4 | 0 | |
  | Ventana-1.bif | 7 | L5 | 1 | 0 | |
  | Ventana-1.bif | 8 | L6 | 1 | 0 | |
  | Ventana-1.bif | 9 | L7 | 1 | 0 | |
  | OS-1.bif | 0 | overview | 1 | 0 | associated image |
  | OS-1.bif | 1 | thumbnail | 1 | 0 | associated image |
  | OS-1.bif | 2 | L0 | 8700 | 0 | main pyramid base |
  | OS-1.bif | 3 | L1 | 2204 | 0 | |
  | OS-1.bif | 4 | L2 | 551 | 0 | |
  | OS-1.bif | 5 | L3 | 150 | 0 | |
  | OS-1.bif | 6 | L4 | 40 | 0 | |
  | OS-1.bif | 7 | L5 | 12 | 0 | |
  | OS-1.bif | 8 | L6 | 4 | 0 | |
  | OS-1.bif | 9 | L7 | 1 | 0 | |
  | OS-1.bif | 10 | L8 | 1 | 0 | |
  | OS-1.bif | 11 | L9 | 1 | 0 | |

  **Implication for Task 14:** Both fixtures are single-AOI scans with complete tile coverage — no real empty tiles to validate the blank-fill path. Task 14 (empty-tile synthesis testing) must include a synthetic BIF-XMP test fixture with explicit empty tiles. The spec's encoding is confirmed as sufficient discriminator; the implementation path (fill with `ScanWhitePoint`-coloured JPEG) has no in-fixture validation opportunity.

#### Task 5 — ScanWhitePoint extraction gate

- **Date:** 2026-04-27
- **Outcome:** Both fixtures probe successfully for the `ScanWhitePoint` XMP attribute. Ventana-1.bif carries `ScanWhitePoint="235"` (spec-compliant iScan, DP 200 scanner); OS-1.bif returns missing (legacy iScan, no ScannerModel attribute). Matches the design assumption exactly. When `ScanWhitePoint` is absent, the blank-tile filler (Task 9, invoked by Task 14) defaults to RGB 255 (true white). This aligns with typical TIFF and openslide conventions for unspecified fill colour. Forward reference: Tasks 9 (blank-tile generator) and 14 (empty-tile path) will read this value to fill empty tiles; the precise extraction and JPEG-encoding details are specified when those tasks execute.

### v0.6 gates

#### Task 5 — tifffile splice-replication harness

- **Date:** 2026-04-26
- **Outcome:** simpler than spec assumed. **OME files don't carry
  shared JPEGTables** — every page in our fixtures has
  `jpegtables=None`, every tile / strip's raw bytes start with
  SOI+JFIF (self-contained). For tiled levels, opentile-py's
  `get_tile()` output is **byte-identical to tifffile's raw bytes**
  with no splicing involved (verified on Leica-1 L0 (0,0):
  sha `668b391411f5ec95`, 17359 bytes, both paths).

- **Refined parity strategy** (revises spec §5):

  1. **Tiled levels of every Image**: tifffile reads raw tile bytes
     via `dataoffsets[idx]` / `databytecounts[idx]`; compare directly
     to our `Tile(x, y)` output. No Python-side splice needed. Works
     for all images including those opentile-py drops.

  2. **OneFrame levels of opentile-py-exposed images**
     (Leica-1 main; Leica-2 series 4): opentile-py's `get_tile()`
     gives the cropped output bytes. Compare directly to our output.

  3. **OneFrame levels of dropped images** (Leica-2 series 1-3): no
     byte-stable Python reference (opentile-py never sees them, and
     tifffile only gives raw single-strip bytes, not the cropped
     output). Coverage strategy:
     - Transitive correctness: our OneFrame implementation is the
       shared `internal/oneframe/` package validated by NDPI parity
       on every NDPI fixture.
     - Integration fixture SHAs: each OneFrame tile in those images
       gets a committed SHA snapshot; regressions are caught by
       `TestSlideParity`.

- **Consequence:** the new tifffile oracle (Task 19) is simpler than
  the spec's draft — no Python-side splice logic, just raw-byte read.
  ~30 LOC of Python rather than ~80.

#### Task 4 — OME-XML schema audit

- **Date:** 2026-04-26
- **Outcome:** clean. Both fixtures use the OME 2016-06 schema
  (namespace `http://www.openmicroscopy.org/Schemas/OME/2016-06`).
  Per-fixture Image inventory:

  | Fixture | Images | Names observed | PhysicalSize unit | Type |
  |---|---|---|---|---|
  | Leica-1.ome.tiff | 2 | `'macro'`, `''` (1) | µm on every Pixels | uint8 |
  | Leica-2.ome.tiff | 5 | `'macro'`, `''` × 4 | µm on every Pixels | uint8 |

  All Pixels elements carry PhysicalSizeX / PhysicalSizeY +
  PhysicalSizeXUnit / PhysicalSizeYUnit + SizeX / SizeY + Type
  attributes. No fixture is missing any. Empty Name attributes mean
  "main pyramid" per upstream's `_is_*_series` predicates (none of
  label / macro / thumbnail).

- **Consequence:** Task 12 (metadata parser) handles a single
  namespace URI (2016-06) for our fixtures. The parser should still
  use namespace wildcards (`xml:",any"` semantics) to be robust
  across OME schema versions — slides converted by older tooling
  may use earlier namespaces. All extracted fields (PhysicalSize,
  Size) can rely on µm units; reject other units as
  `ErrUnsupportedFormat` rather than half-implementing
  unit conversions. Type=uint8 is the only supported value (matches
  spec §8 out-of-scope).

#### Task 3 — OneFrame factor-or-copy decision

- **Date:** 2026-04-26
- **Outcome:** **factor**. Reading `formats/ndpi/oneframe.go` end-to-end:
  the body is already format-agnostic — single-strip JPEG read via
  generic `tiff.TagStripOffsets` / `tiff.TagStripByteCounts`, SOF
  parse and rewrite via `internal/jpeg`, MCU-aligned crop via
  `internal/jpegturbo`. No NDPI-specific tags (no McuStarts, no NDPI
  metadata refs). The only NDPI-shaped bits are the package name and
  one comment string.
- **Cross-check:** OME OneFrame levels also use single-strip
  JPEG-compressed pages whose strip bytes start with SOI+JFIF
  (self-contained — `jpegtables=None` on every OME fixture page).
  No JPEGTables splice required. Behaviour matches what
  `formats/ndpi/oneframe.go` already does.
- **Consequence:** Task 10 factors the body into `internal/oneframe/`.
  NDPI and OME both import it. Format-specific bits (MPP, pyramid
  index, level index) are set by the format package after
  construction. The factored package is a direct enabler for v0.7
  (BIF) which likely needs the same machinery.

#### Task 2 — SubIFD parsing audit

- **Date:** 2026-04-26
- **Outcome:** clean. SubIFDs (TIFF tag 330) are present on every
  OME pyramid base page and reachable via tifffile's `series.levels`
  API. Per-fixture inventory:

  | Fixture | Main series | SubIFDs / page | Total levels (top + sub) | Tiled / OneFrame split |
  |---|---|---|---|---|
  | Leica-1.ome.tiff | 1 (page 1 base) | 4 | 5 | L0/L1 tiled, L2/L3/L4 OneFrame |
  | Leica-2.ome.tiff | 4 (pages 1-4 bases) | 5 each | 6 each = 24 main pyramid levels | L0/L1 tiled, L2-L5 OneFrame |
  | (both) macro page | macro = page 0 | 2 | 3 (L0 used as AssociatedImage; L1/L2 ignored) | All OneFrame |

  Python opentile reports `5 levels` for Leica-1 main and `6 levels`
  for Leica-2's exposed main series — matching tifffile's `series.levels`
  one-for-one. Compression on every page is JPEG (7) on both fixtures.

- **Consequences:**
  1. `internal/tiff.Page.SubIFDOffsets()` (Task 6) and
     `tiff.File.PageAtOffset()` (Task 7) are required.
  2. **OneFrame levels are dominant** (3-4 of 5-6 levels per main
     pyramid). Skipping them in v0.6 would parity-skip the majority
     of pyramid output — Task 15 (OneFrame support) is mandatory,
     not optional.
  3. The macro's own pyramid (its 2 SubIFDs) is NOT exposed in our
     port; we use only macro L0 as the AssociatedImage, matching
     upstream.

#### Task 1 — `is_ome` detection gate

- **Date:** 2026-04-26
- **Outcome:** clean. Tifffile's detection rule
  (`page index == 0 AND description[-10:].strip().endswith('OME>')`,
  `tifffile.py:10125-10129`) matches both Leica OME fixtures and
  produces zero false-positives across the other 15 fixtures (5 SVS,
  3 NDPI, 4 Philips, 2 Ventana .bif, 1 generic TIFF). Description
  tails: `'</StructuredAnnotations></OME>'` on both Leica fixtures;
  every non-OME fixture's tail is unrelated. The rule is simpler
  than v0.5's spec draft assumed (which proposed a 3-clause check
  on `<?xml`, `<OME ` substring, and the OME namespace URL).
- **Consequence:** the v0.6 OME factory's `Supports()` predicate
  ports the rule verbatim — `strings.HasSuffix(strings.TrimSpace(desc[len-10:]), "OME>")`
  with bounds checks for short descriptions. No namespace-string
  matching needed.

### v0.5 gates

#### Task 1 — `is_philips` detection gate

- **Date:** 2026-04-26
- **Outcome:** clean. Tifffile's detection rule (`software[:10] == 'Philips DP'` AND `description[-16:].strip().endswith('</DataObject>')`) matches all 4 of our local Philips fixtures and produces zero false-positives across the other 13 fixtures (5 SVS, 3 NDPI, 2 OME, 2 Ventana .bif, 1 generic TIFF). Software values: `'Philips DP v1.0'` on every fixture; description tails: `'...</Attribute>\n</DataObject>'`. Non-Philips comparators that confirm the rule's specificity:
  - OME TIFF: `software='OME Bio-Formats 6.0.0-rc1'`, description tail `'</OME>'` — fails both clauses.
  - SVS: `software=None` — fails the prefix clause.
  - NDPI: `software='NDP.scan'` etc. — fails the prefix clause.
  - Ventana .bif: `software=None` or `'ScanOutputManager 1.1.0.15854'` — fails the prefix clause.
- **Consequence:** the v0.5 Philips factory's `Supports()` predicate ports the rule verbatim. No additional disambiguation needed.

#### Task 2 — `FillFrame` determinism gate

- **Date:** 2026-04-26
- **Outcome:** byte-deterministic. 5 passes through `Jpeg.fill_frame(src, 1.0)` in Python opentile (which wraps libjpeg-turbo's `tjTransform` with a CUSTOMFILTER that zeros all DCT coefficients, then sets the luma DC at the first block of each MCU) produced identical output bytes for the same source tile (SHA `05c3789cc691d9a207659e250b3fc9c799eca7c5019c4b084a441c4dca9da243`, 2,364 bytes; source: CMU-1-Small-Region.svs L0 tile (0,0), 3,985 bytes).
- **Consequence:** v0.5 sparse-tile blank-tile output (`internal/jpegturbo.FillFrame`) must byte-match Python opentile. Tasks 5 / 10 enforce byte-equality; the alternative pixel-equivalent fallback is not needed.

#### Task 3 — DICOM XML schema audit

- **Date:** 2026-04-26
- **Outcome:** the 11 attributes in upstream's TAGS list are all extractable from our 4 fixtures, but **two of them are missing on 3 of 4 fixtures** and three have multi-value formats. Concretely:
  - **Always present (4/4):** `DICOM_PIXEL_SPACING` (per-level series, count varies 9-11), `DICOM_BITS_ALLOCATED` (8), `DICOM_BITS_STORED` (8), `DICOM_HIGH_BIT` (7), `DICOM_LOSSY_IMAGE_COMPRESSION_METHOD`, `DICOM_LOSSY_IMAGE_COMPRESSION_RATIO`, `DICOM_MANUFACTURER`, `DICOM_PIXEL_REPRESENTATION` (0), `DICOM_SOFTWARE_VERSIONS`.
  - **Sometimes missing (1/4):** `DICOM_ACQUISITION_DATETIME` (only in Philips-4: `'20160718122300.000000'`), `DICOM_DEVICE_SERIAL_NUMBER` (only in Philips-4: `'FMT0107'`). Parser must return zero/empty for these on the other 3 fixtures.
  - **Multi-value strings** (space-separated, quoted): `DICOM_SOFTWARE_VERSIONS` (e.g. `'"1.6.6186" "20150402_R48" "4.0.3"'` on Philips-4), `DICOM_LOSSY_IMAGE_COMPRESSION_METHOD` (e.g. `'"PHILIPS_DP_1_0" "PHILIPS_TIFF_1_0"'`), `DICOM_LOSSY_IMAGE_COMPRESSION_RATIO`. Parser strips quotes and splits on whitespace.
  - **Scanner manufacturer is not always "Philips":** Philips-1 + Philips-3 say `Hamamatsu`, Philips-2 says `3D Histech`, Philips-4 says `PHILIPS`. The format is open — non-Philips scanners can emit Philips TIFF. Surfacing the actual manufacturer string verbatim is correct (no normalisation).
  - **`DICOM_PIXEL_SPACING` example value:** `'"0.000226891" "0.000226907"'` — quoted, space-separated `(W, H)` in metres-per-pixel. Floats with quotes around each. Parser strips quotes and splits.
  - **`DICOM_ACQUISITION_DATETIME` format:** `'20160718122300.000000'` — Go layout: `"20060102150405.000000"`. Microseconds are always zero in our one example.
- **Consequence:** Task 11 (PhilipsTiffMetadata XML parser) handles missing values via Optional / pointer-to-T patterns, splits on whitespace + strips `"` for the multi-value string fields, parses dates with the documented Go layout. No additional disambiguation needed beyond what upstream's parser already encodes.

### v0.4 gates

### Task 1 — JP2K determinism gate

- **Date:** 2026-04-26
- **Outcome:** byte-deterministic. Two passes through
  `imagecodecs.jpeg2k_encode` with the upstream Python opentile
  options (level=80, codecformat=J2K, colorspace=SRGB, mct=True,
  reversible=False, bitspersample=8) on the JP2K-33003-1.svs L0
  tile (0,0) produced identical bytes:
  ```
  pass1 sha256=5862056ca6dcbd079403c1f5debd36ce104afe9bbb2fd56ffc5fec2ca7f82080 len=58366
  pass2 sha256=5862056ca6dcbd079403c1f5debd36ce104afe9bbb2fd56ffc5fec2ca7f82080 len=58366
  identical=True
  pixel_equal=True
  ```
- **Consequence:** v0.4 R4 (SVS corrupt-edge reconstruct) targets
  **byte-parity** with Python opentile. Tasks 12 / 16 enforce
  byte-equality assertions; the alternative pixel-equivalent fallback
  is not needed.

### Task 3 — L12 reproduction shape

- **Date:** 2026-04-26
- **Outcome:** **Case D — neither libjpeg-turbo bug nor cgo wrapper bug. The divergence is in our control flow above the wrapper.** The plan offered Cases A/B/C; the actual finding is sharper.

  Determinism check (5 passes each, OS-2.ndpi L5 tile (3,0)):
  - Go side: byte-deterministic (sha `16996cac…6fbc`, 76,418 bytes, 5/5 identical)
  - Python side: byte-deterministic (sha `a1173319…8a24`, 76,307 bytes, 5/5 identical)
  - Each side individually deterministic; the divergence is purely between languages.

  Pixel comparison of the two outputs (after libjpeg decode):
  - Image content (cols 0-895, the in-image region): byte-equal pixel-for-pixel ✓
  - OOB strip (cols 896-1023, image x=3968-4095 past the L5 image's x=3968 right edge):
    - Go: RGB = (128, 128, 128) — mid-gray
    - Python: RGB = (255, 255, 255) — white

  Our `LuminanceToDCCoefficient(luminance=1.0)` returns the correct DC=170 (verified locally on the same OS-2 L5 JPEGHeader). So the cached `dcBackground` is right; we just don't use it on this tile because the dispatch never reaches `CropWithBackgroundLuminanceOpts`.

  Root cause:
  - **Python** (`turbojpeg.py:608-694` `crop_multiple`, gated by `__need_fill_background` at `turbojpeg.py:839-863`) decides geometry-first: route through CUSTOMFILTER iff `crop_region.x + crop_region.w > image_size[0]` (or similar in y) AND luminance ≠ 0.5. Doesn't try plain `Crop` first.
  - **Go** (`formats/ndpi/striped.go::Tile` post-acc2282) tries plain `Crop` first, falls through to `CropWithBackgroundLuminanceOpts` only if `Crop` errors. For OS-2 L5 tile (3,0) and similar edge tiles, `Crop` SUCCEEDS (libjpeg-turbo accepts the MCU-aligned geometry even though it extends past the image). We never call `CropWithBackground`. Plain `Crop`'s default OOB fill is mid-gray (DC=0), not the white-fill DC the cached `dcBackground` would have planted.

  This is the same divergence the v0.3 T30 task uncovered — and reverted (`acc2282`). The revert was the wrong call: T30's geometry-first inversion was the correct fix; the v0.3 fixtures that "broke" were encoding the buggy mid-gray output. Deferred entry L12 currently misframes this as "tjTransform CUSTOMFILTER non-determinism," which is wrong on every count.

- **Consequence:** L12 is a control-flow fix, not a libjpeg-turbo investigation. Two changes needed in v0.4 Task 9:
  1. Replace `frameSize`-based `extendsBeyond` with the **image-size-based** geometry check that matches Python's `__need_fill_background`. Specifically:
     `extendsBeyond := tileXOrigin+l.tileSize.W > l.size.W || tileYOrigin+l.tileSize.H > l.size.H`
     where `tileXOrigin = x * l.tileSize.W` (position in the image, not the assembled frame).
  2. Always route through `CropWithBackgroundLuminanceOpts` when `extendsBeyond` is true, regardless of whether `Crop` would succeed.
  3. Regenerate the OS-2 / Hamamatsu-1 NDPI fixtures with the corrected output (the v0.3 fixtures currently encode the wrong mid-gray fill on edge tiles). Confirm new fixtures byte-match Python via the parity oracle.

  No C-only repro needed; no upstream-bug ticket; no L12 → Permanent demotion.

  **Status (2026-04-26): fixed.** Task 9 landed the dispatch change and regen'd CMU-1 / OS-2 / Hamamatsu-1 NDPI fixtures. Parity oracle removes the L12 carve-out in the same commit; every NDPI tile is now byte-equal to Python opentile.

### Task 2 — NDPI Map fixture audit

- **Date:** 2026-04-26
- **Outcome:** Map pages present on two of three local NDPI fixtures.
  ```
  CMU-1.ndpi          : no Map page (5 pages, max mag = 20.0, min = -1.0)
  OS-2.ndpi           : page 11 (198x580)  mag=-2.0  <-- MAP
  Hamamatsu-1.ndpi    : page  7 (205x600)  mag=-2.0  <-- MAP
  ```
  Upstream confirmation, two layers:
  - **tifffile** (`_series_ndpi`, tifffile.py:5049-5072) DOES classify
    Map pages — `s.name = 'Map'` for `mag == -2.0`. The data is
    structurally exposed at the tifffile layer; a consumer reaches
    them via `TiffFile(path).series[i]` then `pages[0].asarray()`.
  - **Python opentile** (`NdpiTiler` in `ndpi_tiler.py:88-102`) does
    NOT expose Map pages. `_is_label_series` and `_is_thumbnail_series`
    both return False; only `_is_overview_series` (matching "Macro")
    surfaces anything. There's no `_is_map_series` predicate and no
    `tiler.maps` property.
  Surfacing Map pages on the Go side is therefore filling an
  opentile-level scope decision, not inventing a new category — the
  precedent exists at the tifffile layer. Parallels the v0.2 NDPI
  label synthesis (L14).
  Both Map pages on our fixtures are 8-bit grayscale (uint8, 2D
  shape — no third dim). Task 6's `mapPage` implementation should
  byte-passthrough the strip (same pattern as `KindLabel` for SVS
  LZW labels: bytes the source carried, with whatever Compression
  the page advertises). Downstream consumers decoding the Map page
  need to expect a single-channel image, not RGB.
- **Consequence:** L6 / R13 stays in v0.4 scope. Tasks 6-8 proceed
  using OS-2 + Hamamatsu-1 fixtures.

---

## 10. Triage process

Once the branch lands on a remote, every numbered item above should become a tracked issue (GitHub, Linear, etc.) — scope items → roadmap epics, limitations → user-facing docs, reviewer suggestions → individual backlog tickets. Delete entries from this file as they get filed. The goal is for this file to eventually shrink to zero as polish milestones retire each item.

## 10a. v0.9 splice-template gate — initially skipped, then shipped

The v0.9 plan's Batch D (T7+T8) was conditional on a profile-driven
decision: would the JPEG-splice cost hit ≥5% of CPU under warm-cache
parallel pool TileInto?

**T7 first pass (commit 37bffaf, 2026-05-01)** profiled the three
splice formats under `-benchtime=10s -cpuprofile` and found:

| Function | CPU% (cumulative) |
|---|---:|
| `jpeg.InsertTables` | 1.34% |
| `jpeg.InsertTablesAndAPP14` | 1.16% |
| **Combined splice cost** | **~2.5%** |

Below the 5% threshold; T8 was deferred.

**Reversed at owner review (commit e0c05c5, 2026-05-01).** The CPU%
view was insufficient. A bytes/ns analysis showed the splice path
running at ~15 bytes/ns vs ~100 bytes/ns for no-splice formats —
a 6× throughput-per-byte penalty driven by the per-call alloc
churn that CPU profiles don't capture. Two extra allocations per
tile on SVS and Philips translate to GC pressure that hurts tail
latency under sustained load.

T8 shipped as `internal/jpeg.BuildSplicePrefix` +
`internal/jpeg.InsertPrefixInPlace` — pre-built prefix cached at
level open, in-place splice with zero internal allocs. SVS pool
TileInto: 988 → 99.7 ns/op (9.9× speedup); Philips pool TileInto:
4,504 → 425 ns/op (10.6× speedup). Every TIFF format that ships
in opentile-go is now 0 allocs/op on the pool TileInto path.

**Lesson for future profile-driven gates:** the 5% CPU threshold
shouldn't be the only signal. Allocation count + bytes/ns
throughput catch costs the CPU profile alone hides.

## 11. Consolidated backlog (re-triage after v0.9)

v0.9 is a sole-focus performance milestone (mmap default, `TileInto`,
`TileMaxSize`, `WarmLevel`, splice template, concurrency-contract
docs). Per owner instruction (2026-05-01), every other deferred work
item is parked here as a single backlog list to be re-prioritised
once v0.9 ships. This is **not** a re-classification of the items
themselves — they retain their existing severity tags in §2 above —
just a unified view that says "we'll decide what's next after v0.9
lands."

Each entry below cross-references the canonical §2 entry where the
item's full reasoning + resolution path live. Permanent items
(L4, L5, L14, L24) are not in this list because they're design
decisions, not deferred work.

| Item | Surface | Severity | Original milestone | Trigger to revisit |
|---|---|---|---|---|
| **L19** — openslide BIF pixel-equivalence | BIF | Research-driven | v0.7 | Investigation: is openslide-pixel-equivalence even the right correctness oracle for BIF? |
| **L20** — DP 600 unverified | BIF | Fixture-driven | v0.7 | First DP 600 fixture surfaces |
| **L23** — IFE cross-tool parity vs `tile_server_iris` | IFE | Trigger-driven | v0.8 | First downstream divergence story |
| **L25** — IFE ANNOTATIONS block parsing | IFE | Fixture-driven | v0.8 | First annotated IFE fixture surfaces |
| **R4** — SVS corrupt-edge reconstruct | SVS | Trigger-driven | parked at [#1](https://github.com/cornish/opentile-go/issues/1) | First corrupt-edge SVS in our slate |
| **R9** — JPEG 2000 decode/encode | SVS | Trigger-driven | parked at [#1](https://github.com/cornish/opentile-go/issues/1) | Same as R4 |
| **R6** — 3DHistech TIFF support | (new) | Trigger-driven | parked at [#2](https://github.com/cornish/opentile-go/issues/2) | First 3DHistech TIFF in the wild |
| **R15** — Sakura SVSlide support | (new) | Trigger-driven | parked at [#3](https://github.com/cornish/opentile-go/issues/3) | First SVSlide in the wild |
| **R16** — Leica SCN support | (new) | **Fixtures available**; v0.11 candidate | mentioned as v0.8 candidate, fixtures landed 2026-05-01 | Owner sign-off to schedule v0.11 |
| **Zero-copy `Level.TileBorrow(x, y) ([]byte, func(), error)`** | A.5 follow-on | YAGNI | v0.9 | Concrete consumer with measured zero-copy benefit. Its half-sibling `TilePrefix` shipped in v0.13; `TileBorrow` is on a different axis (allocation vs bandwidth) and remains parked. |
| **Single-level tiled TIFF support** | generic | Fixture-driven; v0.11 candidate | called out 2026-05-05 | Owner sign-off to relax `MinLevels` for single-level files (Grundium fixture in hand) |
| **Mixed-ratio pyramid support** | generic | Fixture-driven; v0.11 candidate | called out 2026-05-05 | Owner sign-off to handle non-geometric chains (Grundium fixture in hand) |
| **L26** — generic-TIFF stripped pyramid IFDs | generic | Fixture-driven | v0.10 | First stripped-pyramid generic TIFF in the slate |
| **L27** — generic-TIFF multi-pyramid rejection | generic | Permanent (OME's job) | v0.10 | Not revisited; route through OME reader |
| **L28** — generic-TIFF multi-strip JPEG `PlanarConfig=2` | generic | Permanent (OME-specific) | v0.10 | Not revisited unless non-OME fixture emerges |
| **L29** — generic-TIFF pluggable associated classifier | generic | YAGNI | v0.10 | First consumer asks for it |
| **L30** — Leica SCN multi-Z stack | leicascn | Fixture-driven | v0.11 | First multi-Z SCN fixture in the slate |
| **L31** — Leica SCN AOI-cropped Tile variant | leicascn | YAGNI | v0.11 | First consumer asks for it |
| **L32** — Leica SCN mismatched-objective regions | leicascn | Fixture-driven | v0.11 | First mismatched-objective SCN fixture surfaces |
| **L33** — Leica SCN byte-equality vs bio-formats | leicascn | Permanent (decode/re-encode divergence) | v0.11 | Not revisited; structural-equivalence is the bar |
| **L34** — Leica SCN 3-fixture coverage limit | leicascn | Permanent (production discontinued ~2015) | v0.11 | Not revisited proactively; trigger-driven |
| **R18** — Smart Zoom Image (SZI) support | (new) | ✅ **landed in v0.16** (see §8j) | spec + fixtures landed 2026-05-08; reader shipped 2026-05-09 | retired |
| **R19** — Deep Zoom Image (DZI) support | (new) | Co-designed with R18 | called out 2026-05-08 | Owner sign-off; v0.17+ candidate. internal/dzi/ pre-pares it (shipped in v0.16). See co-design note below. |
| **R20** — opentile.Metadata: add MicronsPerPixel + ImageDescription | core API | ✅ **landed in v0.17** (see §8k) | flagged 2026-05-09 in v0.16 T4; reader-wide expansion shipped 2026-05-09 | retired |

**Closed by v0.19:** GH Issues
[#5](https://github.com/cornish/opentile-go/issues/5) (generic-TIFF
WSI-tag awareness + integer-multiple pyramid ratio relaxation) and
[#6](https://github.com/cornish/opentile-go/issues/6) (dedicated
`cogwsi` reader). R21 (general COG first-class support) is also
fully retired — the WSI-context portion shipped as COG-WSI in v0.19;
plain geospatial COG is permanently YAGNI for opentile-go. See §8m.

Re-triage at v0.9 ship: either pick the next milestone's scope from
this list, or fold an item into a v0.9.x point release if the trigger
fires sooner than expected.

### Note on R18 / R19 — SZI + DZI co-design (2026-05-08)

SZI is a ZIP wrapper around DZI plus SZI-specific scan metadata and
associated images. The two formats share substantial structure
(DZI manifest XML schema; tile addressing
`<name>_files/<level>/<col>_<row>.<format>`); they differ in storage
backend (ZIP archive vs. filesystem directory) and in whether SZI-
specific metadata + associated images are present.

**Authoritative DZI facts** (per Microsoft Silverlight / Deep Zoom
File Format Overview, archived at MSDN; verified 2026-05-08):

- Tile path is **`<col>_<row>.<format>`** (column-then-row, NOT
  row-then-column). Quote from the Microsoft spec: *"The tiles are
  named as column_row.format, where row is the row number of the
  tile (starting from 0 at top) and column is the column number of
  the tile (starting from 0 at left). format is the appropriate
  extension for the image format used – either JPEG or PNG."*
- Each pyramid level is stored in `<name>_files/<level>/`. Levels
  are counted **from `1×1` pixel as level 0**; each level is
  `2^level × 2^level`. Practical max level for an image of size
  `W×H` is `ceil(log2(max(W, H)))`.
- Tile size is set by the `TileSize` attribute (typically 256).
- `Format` attribute is restricted to formats supported by
  Silverlight's BitmapImage class — practically **JPEG and PNG**.
- `Overlap` attribute (typical: 0, 1, or 2 pixels) adds buffer
  pixels around each tile to prevent seam artifacts during tiled
  rendering. opentile-go's contract is no-overlap; non-zero overlap
  must either error or pass through with a documented caveat.
- **Sparse images** (subset of tiles intentionally missing for
  detail-only zones) are part of the spec — reader must tolerate a
  missing tile entry the same way Philips's sparse-tile filler does.
- **Collections (DZC format)** with Morton-laid-out shared tiles
  are explicitly OUT OF SCOPE — DZC is a multi-image collection
  format; opentile-go reads single-WSI files only. Bare per-image
  DZI is what we target.
- DZI manifest comes in **XML and JSON** variants (OpenSeadragon
  supports both); SZI uses XML per its spec, so v0.16 implements
  XML-only. JSON is a v0.16+ follow-on if a fixture demands.

**Architectural target:** SZI ships first (the user has fixtures +
a real consumer); DZI follows as an additive companion or in the
same milestone if scope permits. Design SZI to share its DZI core
with a future `formats/dzi/` package without compromising either:

- **Shared core (`internal/dzi/`):** pure DZI manifest parser
  (`Manifest`: Format / Overlap / TileSize / Size); pyramid-level
  derivation (max level + per-level dimensions per the rule above);
  pure-function tile-path formatter
  `TilePath(level, col, row int, format string) string` — note the
  parameter order mirrors the on-disk filename convention
  (`<col>_<row>.<format>`).
- **Storage abstraction:** a minimal `dzi.TileFetcher` interface
  (`Fetch(path string) ([]byte, error)` or equivalent) lets the
  shared core read tiles without knowing whether they live in a ZIP
  or on disk. The interface stays narrow — no allocation contracts
  beyond what the existing `Tile` / `TileInto` semantics demand.
- **Sparse-tile policy:** `TileFetcher` returns a sentinel
  (e.g., `dzi.ErrTileMissing`) when a sparse tile is absent; the
  format-specific layer maps that to the consumer's expected
  behavior (Philips-style fill or pass through `ErrTileNotFound`,
  TBD per Q-decision).
- **SZI specifics in `formats/szi/`:** ZIP central-directory
  traversal; uncompressed-stored ZIP entry → direct `io.SectionReader`
  on the ZIP file (preserves v0.9's mmap-aliased fast path — no
  decompression, no copy); `scan-properties.xml` (namespace
  `http://www.pathozoom.com/szi`) → `Tiler.Metadata()`;
  `associated_images/{label,macro,thumbnail}.jpg` → `Tiler.Associated()`
  with v0.15-canonical Type() values (`"label"` / `"overview"` /
  `"thumbnail"` — note SZI's `macro.jpg` filename maps to
  `Type() == "overview"` per v0.15 alignment).
- **DZI specifics in `formats/dzi/`** (future): filesystem walker
  or `fs.FS` backend; no scan-properties.xml; associated images
  are out-of-spec for bare DZI but could be heuristically detected
  (e.g., sibling `<name>_files/../*.{jpg,png}`) if a fixture demands.

**No-compromise guardrails:**
- SZI's tile-fetch path must remain mmap-aliased (uncompressed-stored
  entries → ReaderAt slice into the file); the storage abstraction
  must not force RAM allocation or copy-out for the ZIP backend.
- DZI's filesystem path remains independently mmap-fast (per-tile
  `os.File` opened on demand, or pre-cached `fs.FS` handle map).
- Don't over-generalize: if a future variant (e.g., HTTP-backed DZI,
  or DZC collections) legitimately can't fit the existing
  `dzi.TileFetcher` contract, it gets its own shim — not a watered-
  down shared interface.
- Both formats use the v0.15-canonical `Type()` values; SZI's
  filename `macro.jpg` is mapped, not exposed as-is.
- Tile naming follows the column-row order from the spec verbatim;
  don't silently swap to row-column even if it "feels more natural"
  in row-major code paths.

**Spec sources** (URLs preserved; PDFs gitignored under
`sample_files/szi/`):

- **SZI:**
  - `sample_files/szi/SZI format description - 2018-11-24.pdf` —
    17-page authoritative spec by smartinmedia / pathozoom (LGPL +
    CC-BY).
  - SZI-Format repo: `https://github.com/smartinmedia/SZI-Format`
  - Sample fixture: `sample_files/szi/CMU-1.szi` (from spec repo);
    `sample_files/szi/scan_618_grundium_SZI.szi` (709 MB
    Grundium-produced).
- **DZI** (3 references; agree on substance, complementary in scope):
  - Microsoft authoritative: `https://learn.microsoft.com/en-us/previous-versions/windows/silverlight/dotnet-windows-silverlight/cc645077(v=vs.95)`
    — file format overview + tile-naming + level-derivation rules
    + DZC collections.
  - OpenSeadragon practical reference: `https://openseadragon.github.io/examples/tilesource-dzi/`
    — confirms XML and JSON manifest variants; documents typical
    Format/TileSize/Overlap defaults; client-side conventions.
  - NIST Deep Zoom paper: `https://isg.nist.gov/deepzoomweb/resources/nist/paper/deepZoom_published004866_10.pdf`
    — academic reference; tile-pyramid rationale + WSI-context
    notes (link preserved here for v0.16 design phase; PDF was not
    text-extracted at note-writing time).

**Trigger:** Owner sign-off + viewer-side need confirmed; estimate
6-8 tasks for SZI alone, 8-10 for SZI + DZI together.

### Note on R16 — Leica SCN status as of v0.10 design

Three openslide-testdata SCN samples landed in
`sample_files/scn/` on 2026-05-01:

- `Leica-1.scn` (278 MB) — single ROI brightfield; 2D smoke
  fixture
- `Leica-2.scn` (2.1 GB) — multi-ROI brightfield (mouse kidney
  H&E); exercises `Tiler.Images()` like multi-image OME
- `Leica-Fluorescence-1.scn` (21 MB) — **3-channel separated
  fluorescence**, single ROI; 4-level pyramid per channel

Probe of the fluorescence file (commit context: 2026-05-01) confirms
**genuine separated multi-channel data**: IFDs 6-17 carry 3 channels
× 4 pyramid levels each as `spp=1, photo=grayscale` planes. SCN
XML in IFD 0's ImageDescription maps `(r, c) → ifd_index`. Not
pre-merged RGB.

**Updated R16 valuation:** the multi-channel fluorescence sample is
the first real-fixture exercise of `Image.SizeC() > 1` — every
existing fixture (cervix, Leica OME, Ventana BIF) reports `SizeC=1`.
SCN-as-format-support gains value as **fixture coverage for v0.7's
multi-dim C-axis API**, independent of whether the Leica SCN scanner
line is still in active production (it isn't — last new SCN
hardware was ~2015). v0.11 candidate.

Implementation lift: similar pattern to OME — BigTIFF with vendor
XML in ImageDescription, SubIFD pyramids. Reuses v0.6 SubIFD
machinery and v0.7 multi-dim API. Estimated ~1 week.

### Note on Grundium TIFFs (single-level + mixed-ratio pyramid)

The user's Grundium Ocus scanner emits two TIFF dialects that the
v0.10 generic reader **rejects** on detection. Both fixtures are
parked in `sample_files/generic-tiff/`; both are valid tiled BigTIFF
output, just outside what v0.10's sealed Q2/Q7 thresholds accept.
Probed 2026-05-05.

- **`scan_619_grundium_pyramid_TIFF.tif`** (281 MB) — single tiled
  IFD: 43008×27136, 512×512 tiles, JPEG, photo=YCbCr, software=
  none, ImageDescription=`Grundium Ocus`. **One** IFD, no reduced
  levels at all. Validator rejects with `ErrPyramidTooFewLevels`
  ("got 1 tiled+valid IFDs, need ≥3"). **Diagnosis:** this isn't
  a pyramid — it's a single tiled image — and v0.10's `MinLevels=3`
  (Q2 sealed) explicitly rejects single-level files. Single-level
  tiled TIFFs are common ("just-tile-this-image" output); a v0.11
  expansion could relax `MinLevels` to 1 with a corresponding
  spec-§4 update, exposing one Level and zero Associated.

- **`scan_620_grundium_TIFF.tif`** (266 MB) — 4-IFD mixed-ratio
  pyramid: L0 49152×32768, L1 12288×8192 (4× scale), L2 6144×4096
  (8×, odd-step), L3 3072×2048 (16×). Validator's greedy chain
  picks L0+L1+L3 (consistent 4× ratio) and leaves L2 over; L2 is
  1.56% of baseline — just above the 1% `LeftoverTiledMaxAreaRatio`
  cap — so the file is rejected as `ErrPyramidMultiplePyramid`.
  **Diagnosis:** this is a real pyramid the validator's greedy
  chain-search can't represent cleanly. Tifffile splits it the
  same way (3-level series + 1-level series). v0.11 options:
  (a) bump `LeftoverTiledMaxAreaRatio` to ~5%, silently dropping
  the orphan; (b) attempt multiple chain candidates and pick the
  one that minimises leftovers; (c) surface the orphan as a
  format-specific Level via a new "supplementary level" surface.
  Option (a) is the simplest path and may be the right v0.11
  default given how often Grundium-style mixed-ratio chains
  appear in real WSI output.

These are **not vendor-specific** — Grundium just happens to be the
fixture source. Any TIFF authoring path that writes "one tiled
image" or "4× then 2×/2×/2× downsamples" hits the same gates.
v0.11 candidates; both fixtures already on disk for the
investigation.

### Note on the terminology fix

> **Update (v0.12):** This rename shipped on 2026-05-07. The
> rationale below documents the pre-v0.12 state for historical
> reference. See `§8f` for the retirement audit.

The TIFF spec (and libtiff source) uses **"strip"** — `StripOffsets`
(tag 273), `StripByteCounts` (tag 279), `RowsPerStrip` (tag 278) —
not "stripe." The opentile-go codebase has used "stripe/striped"
inconsistently since v0.2 NDPI work. Two scoping options:

- **(a) Full rename, breaking.** Public `formats/ndpi.StripeInfo`
  → `StripInfo` and its public fields (`StripeW`, `StripeH`,
  `StripedW`, `StripedH`, `StripeOffsets`, `StripeByteCounts`). File
  names, internal types, comments, ~20 files. Cleanest. Breaks the
  v0.3 API-stability invariant — would require a v1.0 bump or
  explicit owner sign-off (we have no external users yet).
- **(b) Internal-only.** File names (`striped.go` → `stripped.go`),
  internal identifiers, comments. Public NDPI types stay; docstrings
  flag the legacy spelling. Non-breaking.

Pick when: bundling with v1.0 if/when one is cut; or as a standalone
v0.x.y if the owner wants the cleanup without a major bump.

### Note on the naming corrections

> **Update (v0.12):** This rename shipped on 2026-05-07. The
> rationale below documents the pre-v0.12 state for historical
> reference. See `§8f` for the retirement audit.

opentile-go's format identifiers and package paths use shorter forms
than is ideal for distinguishing TIFF-dialect formats from non-TIFF
descendants of the same vendor / project. Three corrections, called
out 2026-05-01 (sample-dir renames already done; code-side
corrections done in the same session for sample-dir paths;
public-API name corrections deferred):

- **`opentile.FormatPhilips = "philips"` → `"philips-tiff"`** — to
  differentiate from any future Philips non-TIFF formats and align
  with the (now corrected) sample-dir name `philips-tiff/` and our
  docs/deferred.md format conventions. Breaking change to the
  Format constant value; existing callers comparing against
  `"philips"` would break.
- **`opentile.FormatOME = "ome"` → `"ome-tiff"`** — OME has multiple
  on-disk formats (OME-TIFF, OME-Zarr, OME-NGFF). The bare `"ome"`
  identifier ambiguously claims the family. Breaking value change.
- **Sample-directory naming finalized** (sample-dir renames done
  2026-05-01; code-side updates done in the same commit as this
  backlog entry): `phillips-tiff/` → `philips-tiff/` (typo fix);
  `ventana-bif/` → `bif/` (drop the vendor prefix since BIF is
  unambiguous). No public API impact (the dirs are gitignored
  fixture paths, not exported strings).

The first two items are public-API breaking and bundle with the
striped → stripped cleanup at v1.0 (or earlier if owner sign-off
green-lights an early breaking change before v1.0). The package
directory paths (`formats/philips/`, `formats/ome/`) could also be
renamed to `formats/philipstiff/` / `formats/ometiff/` for full
consistency, but that's a deeper rename touching every importer of
those packages. Defer the directory rename to whenever the
constant rename happens.

Pick when: same as the striped → stripped item — bundle as one
v1.0 naming-cleanup batch.
