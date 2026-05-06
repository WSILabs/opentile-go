# opentile-go v0.10 Generic TIFF Implementation Plan

> **For agentic workers:** Sequential in-thread execution per the
> v0.7/v0.8/v0.9 closeout precedent. Each task ends with a commit;
> batch boundaries are controller checkpoints.

**Goal:** Land `formats/generictiff/` — a catch-all reader for tiled
pyramidal TIFF without vendor metadata. Closes the gap where
opentile-go would error on a generic tiled WSI TIFF that no vendor
factory claims.

**Spec:** [`docs/superpowers/specs/2026-05-01-opentile-go-v10-generic-tiff-design.md`](../specs/2026-05-01-opentile-go-v10-generic-tiff-design.md)
(9 sealed decisions, sign-off log §10).

**Architecture (sealed in spec §3):** Additive evolution; new
format package; new `"associated"` Kind value (additive to
taxonomy); catch-all dispatch (registered last in `formats/all/`);
reuses v0.5 (`internal/tifflzw`), v0.6 (`internal/oneframe`), v0.9
(`internal/jpeg.InsertPrefixInPlace`) infrastructure.

**Branch:** `feat/v0.10` (off main post-v0.9 ship; one prep commit
already lands the deferred-list striped→stripped item).

**Sample slides:**
- **Real**: `sample_files/generic-tiff/CMU-1.tiff` (195 MB, 9-level
  tiled JPEG pyramid, no associated images). Validates pyramid
  reader path.
- **Stripped-SVS**: `sample_files/generic-tiff/CMU-1-Small-Region.stripped.tiff`
  (~1.9 MB, generated from CMU-1-Small-Region.svs by Python
  tifffile-mutating IFD 0's ImageDescription). Validates
  associated-image classifier against realistic SVS-derived shapes.
- **Synthetic**: ~5 small TIFFs under `sample_files/generic-tiff/synth/`,
  generated via Python tifffile from `regen.py`. Cover validator
  reject paths + classifier corner cases.

**Python venv / parity oracle:** N/A. v0.10 has no external parity
oracle (the existing `make parity` tests don't apply — the existing
oracles can't read generic TIFFs in a way that's distinct from the
vendor formats they recognize). Correctness bar = byte-equality
across both backings (existing `TestOpenFileBackingsByteIdentical`
extends to cover generic-format fixtures) + sample-tile SHA fixture
(`tests/fixtures/CMU-1.tiff.json` via `TestSlideParity`) + per-
fixture geometry pin (new `tests/parity/generic_geometry_test.go`).

---

## Universal task contract: "confirm upstream first"

Same as prior milestones: every task starts with `Step 0: Confirm
upstream`. For v0.10 generic TIFF:

1. **TIFF 6.0 spec** — tag semantics + accepted values for
   PhotometricInterpretation / Compression / SampleFormat / etc.
2. **`internal/tiff`** — opentile-go's own parser; the format
   reader doesn't introduce new low-level parsing.
3. **CMU-1.tiff** — the real fixture. Validates against one
   specific encoder's choices.
4. **Stripped-SVS fixture** (T2 deliverable) — realistic SVS-
   derived shape after we mutate IFD 0's ImageDescription.
5. **Existing format readers** — `formats/svs/`, `formats/ome/`,
   `formats/philips/`, `formats/bif/` — mirror their detection +
   level + associated patterns.

When fixtures and spec disagree, trust the fixture (per v0.5 lesson).

---

## Batch A — JIT verification gates (3 tasks)

**Goal:** before writing production code, prove every layout
assumption against real bytes. Each gate runs against CMU-1.tiff
(and the stripped-SVS fixture once T2 lands).

- [ ] **T1 — Pyramid validator gate.** Probe under
  `formats/generictiff/internal/gates/` (build tag `gates`). For
  CMU-1.tiff: parse all IFDs; classify by tiled-or-not; sort
  tiled IFDs by area; compute inter-axis + inter-level scale
  ratios; verify all 9 IFDs satisfy ±2% / ±5% tolerances; record
  baseline tile size + photometric + compression. Confirms the
  Q1/Q2 thresholds work against a real fixture.

- [ ] **T2 — Generate stripped-SVS fixture + probe.** Write
  `sample_files/generic-tiff/regen.py` (Python tifffile script).
  Read `CMU-1-Small-Region.svs`, mutate IFD 0's ImageDescription
  to remove the `"Aperio"` prefix, write
  `CMU-1-Small-Region.stripped.tiff` preserving all IFDs verbatim.
  Probe the result: confirm SVS detector now rejects (no Aperio
  prefix), confirm IFD inventory shows pyramid + label + macro +
  thumbnail. Documents the encoder choice for each associated IFD
  (single-strip vs multi-strip; LZW / JPEG / uncompressed). Drives
  T9's heuristic classifier rules.

- [ ] **T3 — Generate synthetic fixtures + probe.** Extend
  `regen.py` to emit `synth-pyramid-jpeg.tiff` (3-level smoke),
  `synth-pyramid-with-label.tiff` (pyramid + multi-strip LZW
  label), `synth-bad-pyramid.tiff` (3 tiled IFDs whose ratios
  don't match — must be rejected), `synth-stripped-only.tiff`
  (only stripped IFDs — must be rejected). Probe each; record
  per-fixture expected behavior in regen.py comments.

End-of-batch checkpoint: review all probe outputs, confirm
heuristic-classifier expectations are reasonable, green-light Batch B.

---

## Batch B — Validator + classifier helpers (2 tasks)

**Goal:** the pyramid validation + associated-image classifier as
pure helpers in `internal/tiff` (or `formats/generictiff/`), with
unit tests against hand-rolled byte buffers. No format-package
plumbing yet.

- [ ] **T4 — `internal/tiff.ClassifyPyramid` helper.** Takes
  `[]*Page` (all top-level IFDs) and a tolerance config; returns
  the pyramid IFDs (sorted largest-first) + the leftover IFDs +
  validation errors. Implements §4 algorithm:
  filter-tiled, sort-by-area, validate ratios, validate
  photometric/sample/compression, validate ≥3 levels, validate
  multi-pyramid-rejection (Q7). Unit tests (`classify_pyramid_test.go`)
  use minimal-bytes hand-rolled `*tiff.Page`-equivalent test
  scaffolds — exercise reject paths exhaustively, accept path
  against a synthetic 3-level pyramid.

- [ ] **T5 — `formats/generictiff/classifier.go` heuristic classifier.**
  Takes a leftover IFD + the pyramid baseline IFD; returns one of
  `"label"` / `"macro"` / `"thumbnail"` / `"associated"` per the §6
  heuristic table. Unit tests (`classifier_test.go`) cover each
  heuristic branch with synthetic page metadata.

End-of-batch checkpoint: `make test` green; both helpers have
≥80% coverage in isolation.

---

## Batch C — `formats/generictiff/` reader (4 tasks)

**Goal:** the generic format package itself. Mirror SVS / OME tiled
shape; reuse v0.9's splice machinery and v0.5/v0.6's strip-reading
machinery for associated images.

- [ ] **T6 — `formats/generictiff/generic.go` Factory + Detection.**
  `Factory.Supports(*tiff.File)` calls T4's `ClassifyPyramid`; if
  the result satisfies all validation, returns true. Else false.
  Detection runs LAST in `formats/all/` registration order.
  Unit-tested against the synthetic-fixture set (accept path on
  good pyramids; reject path on bad-pyramid + stripped-only).

- [ ] **T7 — `formats/generictiff/tiled.go` Level impl.** Mirror SVS's
  `tiledImage`. Cached `offsets`, `counts`, `jpegTables`,
  `splicePrefix`, `maxTileSize`. Tile() does read + splice (no
  APP14 — generic isn't Aperio); TileInto() uses
  `internal/jpeg.InsertPrefixInPlace` for zero-alloc on splice
  path. WarmLevel hook via internal `warm()` method (v0.9
  pattern). Pin via byte-equality test against CMU-1.tiff (Tile
  output should be identical across both backings; tile bytes
  should be valid standalone JPEGs).

- [ ] **T8 — `formats/generictiff/associated.go` AssociatedImage impl.**
  Per §6 reader-path table: dispatch on shape (single-strip /
  multi-strip uncompressed / multi-strip LZW / multi-strip JPEG /
  tiled). Reuse `internal/tifflzw` for multi-strip LZW
  (v0.5 SVS label path); reuse `internal/oneframe` for multi-strip
  JPEG single-channel; trivial passthrough for the rest. Unit-
  tested against the stripped-SVS fixture (validates against
  real-world SVS-derived associated shapes).

- [ ] **T9 — `formats/generictiff/tiler.go` Tiler.** Wires T7's Level
  + T8's AssociatedImage + T5's classifier. Implements `Tiler`
  interface: `Format() == FormatGenericTIFF`; `Levels()` from the
  pyramid; `Associated()` from the classified leftovers;
  `Metadata()` populated from standard TIFF tags (§7); `ICCProfile()`
  reads tag 34675 from the level-0 IFD; `WarmLevel(i)` via the
  v0.9 pattern. New `opentile.FormatGenericTIFF` constant + new
  `"associated"` Kind value (Q5) in `image.go`'s
  `AssociatedImage.Kind()` docstring.

End-of-batch checkpoint: `make test` green; cervix / SVS / NDPI /
Philips / OME / BIF / IFE all unaffected (existing TestSlideParity
still passes); CMU-1.tiff opens via the generic factory and
returns the expected level count + dimensions.

---

## Batch D — Integration + parity fixtures (3 tasks)

**Goal:** wire CMU-1.tiff and the stripped-SVS fixture into
`tests/integration_test.go`'s `TestSlideParity`; pin per-fixture
geometry; commit sample-tile SHA fixtures.

- [ ] **T10 — `tests/integration_test.go` wiring.** Add
  `"CMU-1.tiff"` and `"CMU-1-Small-Region.stripped.tiff"` to
  `slideCandidates`; extend `resolveSlide` to walk
  `dir/generic-tiff/`; extend `fixtureJSONFor` for
  `.tiff → <stem>.generic.json` (or similar discriminator —
  CMU-1.tiff vs CMU-1.svs need distinct fixture-file names).
  `TestSlideParity` then exercises the generic reader on every
  commit.

- [ ] **T11 — `tests/parity/generic_geometry_test.go`.** Per-
  fixture geometry pinning (mirrors `bif_geometry_test.go` and
  `ife_geometry_test.go`): for CMU-1.tiff, pin the 9 levels'
  dimensions, tile sizes, grids, compression. For the stripped-
  SVS, pin the pyramid levels + the associated-image kinds and
  byte counts. Also pin `Tiler.Format()` returns `FormatGenericTIFF`.
  Cross-format `TestOpenFileBackingsByteIdentical` (v0.9) extended
  to cover the new fixtures.

- [ ] **T12 — Generate sample-tile SHA fixtures.** Run
  `TestGenerateFixtures` for CMU-1.tiff +
  CMU-1-Small-Region.stripped.tiff; commit the resulting JSONs.
  `TestSlideParity` total grows to 19 (5 SVS + 3 NDPI + 4 Philips
  + 2 OME + 2 BIF + 1 IFE + 2 generic).

End-of-batch checkpoint: `make test` + `make cover` green; no
regressions on existing fixtures.

---

## Batch E — Docs + ship (3 tasks)

- [ ] **T13 — `docs/deferred.md` updates.**
  - §1a deviations: add "Generic TIFF reader for non-vendor tiled
    pyramidal TIFFs" entry; add `"associated"` Kind value addition.
  - §2 active limitations: add L26 (stripped pyramid IFDs deferred),
    L27 (multi-pyramid generic TIFFs deferred), L28
    (multi-strip JPEG with PlanarConfiguration=2 deferred), L29
    (pluggable associated-image classifier deferred).
  - §11 consolidated backlog: extend with the L26-L29 entries.
  - §8d (new) v0.10 retirement audit. Mirror v0.9's §8c shape.

- [ ] **T14 — `docs/formats/generictiff.md` (new) + README updates.**
  - New `docs/formats/generictiff.md` per the format-doc template
    (mirrors `docs/formats/bif.md`, `ife.md` shape).
  - README: update the Supported-formats table with a Generic TIFF
    row; update the Detection paragraph to mention catch-all
    dispatch order.
  - Deviations table: add the generic TIFF + `"associated"` Kind
    value entries.

- [ ] **T15 — `CHANGELOG.md [0.10.0]` + `CLAUDE.md` milestone bump.**
  - CHANGELOG: new `[0.10.0]` heading. Added: `formats/generictiff/`,
    `opentile.FormatGenericTIFF` constant, `"associated"` Kind value,
    standard-tag `Tiler.Metadata()` population, ICC profile
    surfacing. Changed: `Tiler.Associated()` callers must handle
    the new `"associated"` Kind value (additive; backward-compat-safe).
  - CLAUDE.md: milestone bump v0.9 → v0.10. New scope, sample
    slides, deferred forward-list.

End-of-batch checkpoint: final validation sweep — `make vet`,
`make test`, `make cover`, `make parity`, baseline gate clean.
Hand back for tag + merge + release.

---

## Risk notes

- **Catch-all dispatch is risky.** The generic factory is registered
  LAST, but if it accepts too aggressively, it could swallow files
  that should error (or that a future vendor format reader would
  claim). Mitigation: tight detection (≥3 tiled IFDs; consistent
  scale ratios; uint8 RGB/YCbCr/grayscale; whitelisted compressions).
  Reject-path tests (synth-bad-pyramid, synth-stripped-only) gate
  this in CI.
- **Stripped-SVS fixture is structurally close to SVS** — if our
  ImageDescription mutation isn't surgical enough, the SVS
  detector might still claim it. Mitigation: T2 explicitly verifies
  SVS factory's `Supports()` returns false on the stripped fixture
  before declaring success.
- **Associated-image heuristics are fundamentally fuzzy.** A
  consumer with vendor knowledge will sometimes get a misclassified
  Kind. Documented as an acceptable risk in v0.10; pluggable
  classifier (Q6 deferred) lands when a consumer asks.
- **Multi-pyramid TIFFs from bioformats** (Q7 reject) — opens via
  the OME factory if they have OME-XML; rejects via generic if they
  don't. Edge case is bioformats output without OME-XML; users in
  that situation get an error rather than a half-correct read.
  Acceptable for v0.10.

---

## Out of scope for v0.10

Per the spec §9 + the consolidated backlog §11:

- Stripped pyramid IFDs (Q4 — deferred)
- Multi-pyramid generic TIFFs (Q7 — OME's job)
- Multi-strip JPEG with PlanarConfiguration=2 for associated
  images (rejected; OME-specific quirk)
- Pluggable `WithAssociatedClassifier` Option (Q6 — wait for signal)
- 16-bit / float / palette / CMYK photometric (out of opentile-go
  scope generally)
- Compression schemes outside {None, JPEG, JP2K, LZW, Deflate}
- ICC profile decoding for associated images (rare)
