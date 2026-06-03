# DICOM JP2K + HTJ2K decode — design

- **Date:** 2026-06-02
- **Status:** approved (brainstorm complete; awaiting spec review → plan)
- **Target version:** v0.33 (additive; one combined milestone)
- **Work branch (proposed):** `feat/dicom-jp2k-htj2k`
- **Predecessor:** v0.32 DICOM reader — `docs/superpowers/specs/2026-06-02-dicom-reader-design.md`

## 1. Summary

Extend the v0.32 DICOM WSI reader to decode **JPEG 2000 (Part 1)** and
**High-Throughput JPEG 2000 (HTJ2K)** transfer syntaxes, in addition to the
day-one JPEG Baseline + uncompressed support. JP2K is implemented and verified
first; HTJ2K follows in the same plan as a cheap delta. The codec machinery
already exists — OpenJPEG (JP2K, TIFF tag 33003) and openjph (HTJ2K, tag 60003)
are registered decoders used by SVS and the wsi-tools codecs — so this milestone
is concentrated on **transfer-syntax mapping, color/photometric correctness,
fixtures, and a new wsidicom pixel oracle**, not on decoder plumbing.

## 2. Background — current state (v0.32)

- `formats/dicom/` reads VL Whole Slide Microscopy (WSM) series; TILED_FULL +
  TILED_SPARSE; **JPEG Baseline + uncompressed only**.
- Decode dispatch is codec-generic and already complete:
  `Compression → CompressionToTIFFTag → Slide.decoderFor(tag) →
  decoder.GetByCompressionTag(tag)`. `CompressionToTIFFTag` already maps
  `CompressionJP2K → 33003` and `CompressionHTJ2K → 60003` (compression.go),
  and both decoders are registered (`decoder/jpeg2000`, `decoder/htj2k`).
- The transfer-syntax → `opentile.Compression` decision lives in **one place**:
  `formats/dicom/associated.go:69 compressionForSyntax(ts string)`. Today it
  maps JPEG Baseline (`…4.50`) → `CompressionJPEG`, uncompressed (`…1.2` /
  `…1.2.1`) → `CompressionNone`, default → best-effort JPEG.
- Frame extraction (`formats/dicom/frames.go`) walks encapsulated PixelData
  fragments, **one fragment per frame**, deriving offsets from the fragment walk
  (the Basic Offset Table is empty on all observed scanners). This walk is
  codec-agnostic — it extracts compressed bytes regardless of transfer syntax.
- Verified fixtures (all JPEG/uncompressed): Leica GT450 (SPARSE), 3DHISTECH
  (FULL), Grundium (FULL).

## 3. Goals / non-goals

**Goals**
- Decode JPEG 2000 Part 1 DICOM WSM levels + associated images.
- Decode HTJ2K DICOM WSM levels + associated images.
- Correct RGB pixel output for the photometric interpretations real WSI uses
  (`YBR_ICT`, `YBR_RCT`, `RGB`).
- Fixture-backed verification including a Python **wsidicom pixel oracle**.

**Non-goals (→ to-do, §9)**
- Multi-fragment-per-frame pixel data.
- Basic/Extended Offset Table-driven frame location.
- JPEG-LS (`…4.80`/`…4.81`) and RLE (`1.2.5`) transfer syntaxes.
- JPEG 2000 **Part 2** multi-component (`…4.92`/`…4.93`).
- Raw DICOM-attribute access API.
- Multi-optical-path / Z-stack / concatenations (already deferred in v0.32).

## 4. Scope — transfer syntaxes

Added to `compressionForSyntax`. Exact UIDs to be confirmed against DICOM PS3.5
§10 during implementation (read upstream first — do not ship from memory):

| Transfer syntax UID | Name | → `opentile.Compression` |
|---|---|---|
| `1.2.840.10008.1.2.4.90` | JPEG 2000 Image Compression (Lossless Only) | `CompressionJP2K` |
| `1.2.840.10008.1.2.4.91` | JPEG 2000 Image Compression | `CompressionJP2K` |
| `1.2.840.10008.1.2.4.201` | HTJ2K (Lossless Only) | `CompressionHTJ2K` |
| `1.2.840.10008.1.2.4.202` | HTJ2K with RPCL Options (Lossless Only) | `CompressionHTJ2K` |
| `1.2.840.10008.1.2.4.203` | HTJ2K Image Compression | `CompressionHTJ2K` |

Part 2 multi-component (`…4.92`/`…4.93`) is intentionally **excluded** —
multispectral/multi-channel, not brightfield RGB; → to-do.

## 5. Architecture — new surface

The change is concentrated and small. No new packages.

1. **`formats/dicom/compressionForSyntax`** — extend the switch with the §4
   rows. This single function drives both level tiles
   (`tiler.go:90`) and associated images. Default stays best-effort JPEG.
2. **Frame extraction** — expected **no change**. JP2K/HTJ2K WSM frames are
   encapsulated one-fragment-per-frame, identical in shape to the JPEG path.
   If a target fixture violates this (multi-fragment), the reader must fail with
   a clear, specific error rather than silently mis-decode — multi-fragment is
   on the to-do, not in scope.
3. **Color / photometric** — see §6. This is the only part with design risk;
   it may require threading `PhotometricInterpretation (0028,0004)` into the
   decode path, or may be fully handled by the codec from the codestream.

Everything downstream (decoder pool, `DecodedTile`, `ReadRegion`,
`ScaledStrips`, `nohtj2k` tag-out) is inherited unchanged.

## 6. Color / photometric handling — the design risk

JP2K WSM Photometric Interpretation is `YBR_ICT` (irreversible MCT, lossy
`.91`/`.203`), `YBR_RCT` (reversible MCT, lossless `.90`/`.201`), or `RGB`
(no transform). The open question:

> Does the OpenJPEG/openjph decode path emit correct RGB **from the codestream
> alone** (the COD-marker multi-component transform), or must we honor the DICOM
> `PhotometricInterpretation (0028,0004)` tag and apply YBR→RGB ourselves?

**Working hypothesis:** the JPEG 2000 codestream is self-describing — the MCT is
signaled in the codestream and applied by the decoder, which is why SVS JP2K
already produces correct RGB with no DICOM tags involved. If this holds, the
mapping in §4 is the entire change and no photometric plumbing is needed.

**This hypothesis must NOT be assumed.** Per the project's "read upstream first"
invariant, the implementer:
1. Reads **wsidicom**'s JP2K/HTJ2K decode path (BigPicture/Sectra's canonical
   Python DICOM WSI reader — the correct upstream for DICOM, since Python
   opentile does not read DICOM) and the relevant DICOM PS3.5 §8.2.4 / §8.2.14
   color-space text.
2. Confirms empirically via the wsidicom pixel oracle (§8).

If the decoder does **not** fully resolve color, the fallback is to read
`PhotometricInterpretation` from the instance (already parsed cold-path via
`suyashkumar/dicom`) and apply the transform in the DICOM decode path. This
contingency is in scope for this milestone — color correctness is the
deliverable, whichever layer it lands in.

## 7. Fixtures

| Codec | Primary source | Fallback | Notes |
|---|---|---|---|
| JP2K | Real WSM from **NCI Imaging Data Commons** (public AWS/GCP open-data buckets) | Transcode an existing DICOM fixture via `dcmtk dcmcjp2k` | Real scanner fidelity preferred; keep small per the per-fixture size cap |
| HTJ2K | Real if locatable | Transcode (openjph `ojph_compress` / pylibjpeg-openjpeg via pydicom) | Real public HTJ2K WSI is rarer; synthetic acceptable, flagged "upgrade when available" |

Both wired into `TestSlideParity`. Fixture acquisition is the **first plan task**
(gates the parity bar); JP2K is verifiable with a real fixture, so it lands
first even if HTJ2K stays synthetic.

## 8. Verification — wsidicom pixel oracle

- **Extraction parity (existing, unchanged):** `RawTile` compressed bytes
  byte-identical to `suyashkumar/dicom`. Codec-agnostic — covers the new
  syntaxes with no new code. Plus mmap/pread backing parity
  (`parity_test.go`).
- **New — wsidicom pixel oracle:** a build-tagged harness (mirroring the
  existing `tests/oracle` tifffile/opentile pattern) that shells to Python
  `wsidicom`, decodes the same `(image, level, tx, ty)`, and compares decoded
  pixels within a tolerance (lossy JP2K/HTJ2K → small per-channel ε; lossless
  → exact). This is the backbone that de-risks §6, and a reusable scaffold for
  the deferred openslide pixel oracle (#21).
- **Correctness bar:** `make test` green under `-race`; wsidicom oracle green;
  `nohtj2k` build compiles (HTJ2K tagged out like the rest of the codec set);
  `make cover` per-package threshold held.

## 9. To-do (explicitly deferred this phase)

Logged to the roadmap/backlog, not implemented here:
- Multi-fragment-per-frame pixel data.
- Basic/Extended Offset Table-driven frame location.
- JPEG-LS (`…4.80`/`…4.81`) + RLE (`1.2.5`) transfer syntaxes.
- JPEG 2000 Part 2 multi-component (`…4.92`/`…4.93`).
- Raw DICOM-attribute access API.
- Multi-optical-path / Z-stack / concatenations.

## 10. Sealed decisions

1. **One combined milestone**, JP2K implemented + verified first, HTJ2K a delta
   within the same spec/plan (avoids duplicating fixture/oracle scaffolding).
2. **Reuse the registered OpenJPEG/openjph decoders** — no new decoder code;
   the work is mapping + color + verification.
3. **wsidicom pixel oracle built this phase** as the verification backbone
   (chosen over lighter checks; de-risks the color question).
4. **Scope = JP2K Part 1 (`.90`/`.91`) + HTJ2K (`.201`/`.202`/`.203`)** only;
   Part 2 and all of §9 → to-do.
5. **Frame extraction unchanged**; multi-fragment is out of scope and must
   error explicitly rather than mis-decode.
6. **Color correctness is the deliverable** wherever it lands (decoder vs.
   explicit `PhotometricInterpretation` transform), decided by reading wsidicom
   + DICOM PS3.5 and confirmed by the oracle.

## 11. Open risks

- **Color/MCT (§6)** — primary risk; mitigated by upstream read + oracle.
- **HTJ2K real-fixture availability** — mitigated by transcoding; JP2K unblocked
  regardless.
- **openjph HTJ2K-from-DICOM** — openjph decodes HTJ2K codestreams; confirm the
  DICOM-encapsulated form matches what the decoder expects (same J2K/JPH
  codestream, no JP2 box), as with the JP2K-codestream form SVS already feeds
  OpenJPEG.
- **wsidicom oracle environment** — pin a known-good `wsidicom` + codec backend
  (pylibjpeg-openjpeg) in the oracle requirements, as done for the tifffile/
  opentile oracle's Python pin.
