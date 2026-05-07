# opentile-go v0.7 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add Ventana BIF (Roche / iScan) support — the **first format beyond upstream Python opentile's coverage**. Two paths inside one reader: a spec-compliant path for VENTANA DP scanners (DP 200, DP 600, future DP) and a legacy-iScan path for everything else iScan-tagged. Three layered correctness oracles (openslide, tifffile, geometry) account for the fact that openslide rejects spec-compliant fixtures and tifffile rejects nothing — neither alone is sufficient.

**Architecture:** New `formats/bif/` package on top of v0.6-settled `internal/tiff` + `internal/jpeg` + `internal/jpegturbo` + `internal/oneframe` infrastructure. Two new internal capabilities: a small XML walker for `<iScan>` and `<EncodeInfo>` trees (`internal/bifxml/`), and IMAGE_DEPTH (32997) tag plumbing in `internal/tiff` (read but unused — Z-stack support is deferred). Public API extension: additive `TileOverlap() image.Point` method on `Level` interface; existing format levels grow zero-returning impls.

**Tech Stack:** Go 1.23+, libjpeg-turbo 2.1+ (existing), Python `tifffile` (parity, opt-in `//go:build parity`), Python `openslide-python` (new parity dep, opt-in same tag).

**Spec:** `docs/superpowers/specs/2026-04-27-opentile-go-v07-design.md`.
**Research evidence:** `docs/superpowers/notes/2026-04-27-bif-research.md`.

**Branch:** `feat/v0.7` from `main` after the v0.6 merge (`984c454`).

**Sample slides:** 2 BIF fixtures already in `sample_files/ventana-bif/`:

| File | Size | Generation | openslide | Path classification |
|---|---:|---|---|---|
| Ventana-1.bif | 227 MB | DP 200 (BuildVersion 1.1.0.15854, 2019) | rejects (`Direction="LEFT"`) | spec-compliant |
| OS-1.bif | 3.6 GB | iScan Coreo (BuildVersion 3.3.1.1, 2011) | reads cleanly | legacy-iScan |

Set `OPENTILE_TESTDIR=$PWD/sample_files` for integration tests; the resolver gains a `<dir>/ventana-bif` lookup in this plan.

**Python venv:** `/private/tmp/opentile-py/bin/python` (already provisioned). New parity oracle dependency: `openslide-python` (and `pillow` if not already there). Install command in T20.

---

## Universal task contract: "confirm upstream first"

Every task starts with `Step 0: Confirm upstream` — names the upstream rule that governs the behaviour, states it, includes a verification command. No task body proceeds until that command has been run.

For BIF the upstream sources are layered, in priority order:
1. **`docs/superpowers/notes/2026-04-27-bif-research.md`** — the authoritative project-internal mirror of the Roche whitepaper, openslide reader, and fixture probe data. **Cite this first**; it has line-range pointers into the underlying sources.
2. **The Roche BIF whitepaper PDF** — `sample_files/ventana-bif/Roche-Digital-Pathology-BIF-Whitepaper.pdf`. Convert once: `pdftotext -layout sample_files/ventana-bif/Roche-Digital-Pathology-BIF-Whitepaper.pdf /tmp/bif-whitepaper.txt`. Then grep that text. Canonical for VENTANA DP behaviour.
3. **openslide `src/openslide-vendor-ventana.c`** — fetch via `gh api repos/openslide/openslide/contents/src/openslide-vendor-ventana.c -H 'Accept: application/vnd.github.raw' > /tmp/openslide-ventana.c` once, then grep. **LGPL 2.1 — read-for-understanding only; never copy code verbatim into opentile-go.** Canonical for legacy-iScan behaviour and pre-DP200 quirks.
4. **The two local fixtures** — probed via the Python tifffile commands captured in the notes file §2.

Local upstream Python tifffile reference (carry-over from v0.6):
- `/private/tmp/opentile-py/lib/python3.12/site-packages/tifffile/tifffile.py`

---

## File structure

New files this plan creates:

| Path | Responsibility |
|---|---|
| `internal/bifxml/bifxml.go` | XML walker for `<iScan>` and `<EncodeInfo>` trees |
| `internal/bifxml/bifxml_test.go` | Unit tests over fixture XMP fragments |
| `formats/bif/bif.go` | Factory + Open + classifier wiring + Tiler impl |
| `formats/bif/detection.go` | Vendor detection (`<iScan>` substring match) + IFD classification by ImageDescription |
| `formats/bif/detection_test.go` | Unit tests + integration probe |
| `formats/bif/classify.go` | Generation classification (`strings.HasPrefix(model, "VENTANA DP")`) |
| `formats/bif/level.go` | Concrete `Level` impl: tile fetch, serpentine remap, empty-tile fill |
| `formats/bif/level_test.go` | Unit tests for serpentine math + empty-tile path |
| `formats/bif/serpentine.go` | Pure `serpentineToImageOrder(c, r, cols, rows int) (int, error)` and inverse |
| `formats/bif/serpentine_test.go` | Property-based unit tests |
| `formats/bif/blanktile.go` | ScanWhitePoint-filled JPEG tile generator (mirrors `formats/philips/blanktile.go`) |
| `formats/bif/blanktile_test.go` | Unit tests |
| `formats/bif/associated.go` | Label / Probability / Thumbnail AssociatedImage impls |
| `formats/bif/associated_test.go` | Unit tests |
| `formats/bif/metadata.go` | `Tiler.Metadata()` ventana.* keys |
| `formats/bif/metadata_test.go` | Unit tests |
| `tests/oracle/openslide_runner.py` | openslide-python parity oracle script |
| `tests/oracle/openslide_session.go` | Go-side driver mirroring `oracle.NewSession` |
| `tests/oracle/openslide_test.go` | Parity test driving the openslide runner |
| `tests/parity/bif_geometry_test.go` | Geometry sanity tests, no build tag |
| `tests/fixtures/Ventana-1.bif.json` | Sampled-tile fixture (generated) |
| `tests/fixtures/OS-1.bif.json` | Sampled-tile fixture (generated) |
| `docs/formats/bif.md` | Per-format reader notes |

Files modified:

| Path | What changes |
|---|---|
| `internal/tiff/page.go` | Adds IMAGE_DEPTH (32997) constant + `Page.ImageDepth()` accessor (read-only; non-1 values surfaced via metadata, not interpreted) |
| `internal/tiff/page_test.go` | Tests for ImageDepth accessor |
| `image.go` | Adds `TileOverlap() image.Point` to the `Level` interface |
| `formats/svs/tiled.go` | Adds zero-returning `TileOverlap()` |
| `formats/ndpi/striped.go` | Same |
| `formats/ndpi/oneframe.go` | Same (or `internal/oneframe/oneframe.go` if the impl lives there) |
| `formats/philips/tiled.go` | Same |
| `formats/ome/tiled.go` | Same |
| `internal/oneframe/oneframe.go` | Same (if it carries a Level impl per v0.6 outcome) |
| `formats/all/all.go` | Registers the new BIF factory |
| `tests/integration_test.go` | slideCandidates + BIF fixtures; resolveSlide gains `"ventana-bif"` |
| `tests/oracle/parity_test.go` | Skip BIF — opentile-py doesn't read it |
| `tests/generate_test.go` | sampledByDefault gains both BIF fixtures (>100 MB each) |
| `docs/deferred.md` | R14 → ✅; new "Deviations" entries; "Retired in v0.7" subsection |
| `README.md` | Format set bumped to 5 (BIF added); deviations list extended |
| `CHANGELOG.md` | New `[0.7.0]` entry |
| `CLAUDE.md` | v0.7 milestone bump |
| `Makefile` | If `make parity` needs an env-var hint for openslide oracle |

---

# Batch A — JIT verification gates

Five gate tasks. Run all five before sinking any port work. Outcomes recorded under `docs/deferred.md §9` paralleling v0.5 / v0.6 gate-outcomes structure. Each gate is a probe + a recorded "this is what we found" entry; if a gate fails, escalate before proceeding.

## Task 1: Detection gate — `<iScan` substring matches both fixtures, zero false positives

**Goal:** Confirm the §5.1 detection rule (substring `"<iScan"` in any IFD's XMP, on a BigTIFF) matches both BIF fixtures and matches no non-BIF fixture.

**Files:**
- Modify: `docs/deferred.md` (§8 v0.7 gate-outcomes section; create section if absent).

- [ ] **Step 0: Confirm upstream**

```sh
grep -n 'INITIAL_XML_ISCAN\|iScan' /tmp/openslide-ventana.c | head -10
grep -n '## 1\.' docs/superpowers/notes/2026-04-27-bif-research.md
```

Confirms openslide uses the same detection substring (`"iScan"`). Notes file §1 confirms it's the only reliable cross-fixture marker.

- [ ] **Step 1: Probe all 13 fixtures.**

```sh
/private/tmp/opentile-py/bin/python <<'PY'
import tifffile
from pathlib import Path
for p in sorted(Path("sample_files").rglob("*.svs")) + \
         sorted(Path("sample_files").rglob("*.ndpi")) + \
         sorted(Path("sample_files").rglob("*.tiff")) + \
         sorted(Path("sample_files").rglob("*.bif")):
    try:
        with tifffile.TiffFile(p) as tf:
            hits = []
            for i, page in enumerate(tf.pages):
                xmp = page.tags.get('XMP')
                if not xmp:
                    continue
                v = xmp.value
                if isinstance(v, bytes): v = v.decode('utf-8', 'replace')
                if '<iScan' in v:
                    hits.append(i)
            tag = "BIF" if hits else "non-BIF"
            print(f"{tag:7} bigtiff={tf.is_bigtiff} hits_in={hits} {p}")
    except Exception as e:
        print(f"ERR  {p}: {e}")
PY
```

- [ ] **Step 2: Record outcome.** Add an entry under `docs/deferred.md §9` with the 13-fixture matrix. Expected: 2 BIF hits (Ventana-1, OS-1), 0 false positives across 11 non-BIF fixtures. If non-zero false positives, the detection rule needs strengthening before continuing.

## Task 2: ScannerModel prefix gate — both fixtures classify as planned

**Goal:** Confirm `strings.HasPrefix(IFD0/iScan/@ScannerModel, "VENTANA DP")` routes Ventana-1 to the spec-compliant path and OS-1 to the legacy-iScan path.

**Files:**
- Modify: `docs/deferred.md` (§8 v0.7 gate-outcomes).

- [ ] **Step 0: Confirm upstream.**

```sh
grep -n '## 4\|^### 5\.2\|HasPrefix' docs/superpowers/specs/2026-04-27-opentile-go-v07-design.md
```

Confirms spec §4 + §5.2 are the authoritative rule.

- [ ] **Step 1: Probe both fixtures.**

```sh
/private/tmp/opentile-py/bin/python <<'PY'
import tifffile, re
for p in ["sample_files/ventana-bif/Ventana-1.bif",
          "sample_files/ventana-bif/OS-1.bif"]:
    with tifffile.TiffFile(p) as tf:
        xmp = tf.pages[0].tags['XMP'].value
        if isinstance(xmp, bytes): xmp = xmp.decode('utf-8','replace')
        m = re.search(r'<iScan[^>]*ScannerModel="([^"]*)"', xmp)
        sm = m.group(1) if m else "(missing)"
        gen = "spec-compliant" if sm.startswith("VENTANA DP") else "legacy-iScan"
        print(f"{p}: ScannerModel={sm!r} -> {gen}")
PY
```

- [ ] **Step 2: Record outcome.** Expected: Ventana-1 → spec-compliant (`"VENTANA DP 200"`); OS-1 → legacy-iScan (missing attribute). If results differ, the spec assumption is wrong; re-verify before continuing.

## Task 3: IFD-classification-by-description gate — across both fixtures

**Goal:** Confirm IFD roles can be discriminated solely by `ImageDescription` content (per spec §5.3) — *not* by IFD index, since OS-1 has a different IFD layout from the spec.

**Files:**
- Modify: `docs/deferred.md` (§8 v0.7 gate-outcomes).

- [ ] **Step 0: Confirm upstream.**

```sh
grep -n '^### 5\.3\|kind="probability"' docs/superpowers/specs/2026-04-27-opentile-go-v07-design.md
grep -n '^MACRO_DESCRIPTION\|^THUMBNAIL_DESCRIPTION' /tmp/openslide-ventana.c
```

Spec §5.3 lists the discriminator; openslide hardcodes `"Label Image"` / `"Label_Image"` / `"Thumbnail"` as the associated-image markers.

- [ ] **Step 1: Dump every IFD ImageDescription on both fixtures.**

```sh
/private/tmp/opentile-py/bin/python <<'PY'
import tifffile
for p in ["sample_files/ventana-bif/Ventana-1.bif",
          "sample_files/ventana-bif/OS-1.bif"]:
    print(f"\n{p}:")
    with tifffile.TiffFile(p) as tf:
        for i, page in enumerate(tf.pages):
            d = page.tags.get('ImageDescription')
            v = d.value if d else None
            print(f"  IFD{i}: {v!r}")
PY
```

- [ ] **Step 2: Record outcome.** Build a discriminator table. Expected:

```
Label_Image       -> associated, kind="overview" (spec-compliant only)
Label Image       -> associated, kind="overview" (legacy)
Probability_Image -> associated, kind="probability" (spec-compliant only)
Thumbnail         -> associated, kind="thumbnail" (legacy)
level=N mag=M ... -> pyramid level N
```

Document any unexpected description value in the gate-outcome record.

## Task 4: Empty-tile gate — confirm offset-zero / bytecount-zero is the spec marker

**Goal:** Confirm the spec's empty-tile encoding (`TileOffsets[i] == 0` and `TileByteCounts[i] == 0`) matches what tifffile reports. Required because v0.7 fills empty tiles with `ScanWhitePoint`-coloured JPEGs (mirrors Philips R5).

**Files:**
- Modify: `docs/deferred.md` (§8 v0.7 gate-outcomes).

- [ ] **Step 0: Confirm upstream.**

```sh
grep -n 'TILEOFFSETS \[0\|empty JPEG-tiles' /tmp/bif-whitepaper.txt
```

(Whitepaper's "AOI Positions" section explicitly: `TILEOFFSETS [0 0 0 ... 83354589 ...]` and `TILEBYTECOUNTS [0 0 0 ... 926399 ...]` for unscanned tiles.)

- [ ] **Step 1: Count empty tiles per level on both fixtures.**

```sh
/private/tmp/opentile-py/bin/python <<'PY'
import tifffile
for p in ["sample_files/ventana-bif/Ventana-1.bif",
          "sample_files/ventana-bif/OS-1.bif"]:
    print(f"\n{p}:")
    with tifffile.TiffFile(p) as tf:
        for i, page in enumerate(tf.pages):
            offs = page.tags.get('TileOffsets')
            cnts = page.tags.get('TileByteCounts')
            if not offs or not cnts: continue
            os_ = list(offs.value); cs = list(cnts.value)
            empty = sum(1 for o,c in zip(os_,cs) if o == 0 and c == 0)
            print(f"  IFD{i}: total={len(os_)} empty={empty}")
PY
```

- [ ] **Step 2: Record outcome.** Document empty-tile counts per level. Expected: zero or modest empty-tile counts since both fixtures are single-AOI. If a fixture has non-zero empties, that's a real gift — it lets us validate the blank-tile path without a synthetic fixture.

## Task 5: ScanWhitePoint extraction gate

**Goal:** Confirm `ScanWhitePoint` is reachable on both fixtures (from IFD0 XMP `<iScan>/@ScanWhitePoint`). Spec mandates filling empty tiles with this RGB value; if extraction fails on a fixture the blank-tile path needs an alternative default.

**Files:**
- Modify: `docs/deferred.md` (§8 v0.7 gate-outcomes).

- [ ] **Step 0: Confirm upstream.**

```sh
grep -n 'ScanWhitePoint' /tmp/bif-whitepaper.txt docs/superpowers/notes/2026-04-27-bif-research.md
```

- [ ] **Step 1: Extract on both fixtures.**

```sh
/private/tmp/opentile-py/bin/python <<'PY'
import tifffile, re
for p in ["sample_files/ventana-bif/Ventana-1.bif",
          "sample_files/ventana-bif/OS-1.bif"]:
    with tifffile.TiffFile(p) as tf:
        xmp = tf.pages[0].tags['XMP'].value
        if isinstance(xmp, bytes): xmp = xmp.decode('utf-8','replace')
        m = re.search(r'ScanWhitePoint="(\d+)"', xmp)
        print(f"{p}: ScanWhitePoint={m.group(1) if m else '(missing)'}")
PY
```

- [ ] **Step 2: Record outcome.** Expected: Ventana-1 → 235; OS-1 → missing (legacy iScan doesn't carry the attribute). For the legacy path, default to `255` (true white) — record this fallback in the outcome.

---

# Batch B — Plumbing

Per-task: `internal/tiff` IMAGE_DEPTH plumbing, BIF XML walker, and the `Level.TileOverlap()` interface evolution. Once these land, `formats/bif/` can be built up in Batch C without touching shared infra.

## Task 6: `internal/tiff.Page.ImageDepth()` accessor

**Goal:** Read the IMAGE_DEPTH (32997, `0x80E5`) tag at parse time so BIF can surface the Z-stack depth via metadata. v0.7 doesn't *interpret* multiple Z-planes (deferred); we only expose the raw value.

**Files:**
- New: none.
- Modify: `internal/tiff/page.go`, `internal/tiff/page_test.go`.

- [ ] **Step 0: Confirm upstream.**

```sh
grep -n 'IMAGE_DEPTH\|32997\|0x80E5' /tmp/bif-whitepaper.txt
grep -n '0x80BE\|page 5' /tmp/bif-whitepaper.txt
```

Confirms the authoritative tag value (32997, page 6 + Appendix A); page-5 typo `0x80BE` is bogus.

- [ ] **Step 1: Add tag constant and accessor.** In `internal/tiff/page.go`:

```go
const TagImageDepth = 32997 // SGI private tag; Ventana BIF Z-stack depth
```

Add `func (p *Page) ImageDepth() (int, bool)` — returns `(depth, true)` if the tag is present; `(1, false)` otherwise. Treat values < 1 as the missing case.

- [ ] **Step 2: Test.** Add a synthetic BigTIFF with the tag and a synthetic without; confirm the accessor returns expected values.

- [ ] **Step 3: Verify.** `go test ./internal/tiff/... -count=1`.

## Task 7: `internal/bifxml` package — `<iScan>` and `<EncodeInfo>` walker

**Goal:** A minimal stdlib-`encoding/xml` walker that turns an XMP byte slice into typed accessors for the attributes BIF actually uses. No XPath, no schema validation — direct field reads with sensible defaults.

**Files:**
- New: `internal/bifxml/bifxml.go`, `internal/bifxml/bifxml_test.go`.
- Modify: none.

- [ ] **Step 0: Confirm upstream.**

```sh
grep -n '## 1\. Whitepaper digest\|EncodeInfo\|iScan' docs/superpowers/notes/2026-04-27-bif-research.md | head -20
```

Notes file §1 lists every attribute we care about; XMP samples in §2 show the actual layout on both fixtures.

- [ ] **Step 1: Define types.** In `internal/bifxml/bifxml.go`:

```go
package bifxml

// IScan is the IFD-0 metadata block.
type IScan struct {
    ScannerModel   string  // empty if missing
    Magnification  float64 // 0 if missing
    ScanRes        float64 // microns per pixel; 0 if missing
    ScanWhitePoint uint8   // 0..255; 255 default if missing
    ScanWhitePointPresent bool // distinguishes "missing" from "0"
    ZLayers        int     // default 1
    BuildVersion   string
    BuildDate      string
    UnitNumber     string
    UserName       string
    AOIs           []AOI   // 0..N entries; <AOI0>, <AOI1>, ...
    // raw passthrough for Tiler.Metadata mirroring
    RawAttributes  map[string]string
}

type AOI struct {
    Index             int
    Left, Top         int // pixel coords, physical system
    Right, Bottom     int
}

// EncodeInfo is the IFD-2 metadata block.
type EncodeInfo struct {
    Ver           int     // Must be >= 2 per spec
    AoiInfo       AoiInfo // <SlideInfo><AoiInfo>
    ImageInfos    []ImageInfo
    AoiOrigins    []AoiOrigin
}

type AoiInfo struct {
    XImageSize, YImageSize int  // tile dims
    NumRows, NumCols       int
    PosX, PosY             int
}

type ImageInfo struct {
    AOIScanned bool
    AOIIndex   int
    NumRows, NumCols int
    Width, Height    int
    PosX, PosY       int
    Joints     []TileJoint
    Frames     []Frame
}

type TileJoint struct {
    FlagJoined bool
    Direction  string // "LEFT" | "RIGHT" | "UP" | "DOWN"
    Tile1, Tile2 int
    OverlapX, OverlapY int
}

type Frame struct {
    Col, Row int
    Z        int
}

type AoiOrigin struct {
    Index            int
    OriginX, OriginY int
}

func ParseIScan(xmp []byte) (*IScan, error) { ... }
func ParseEncodeInfo(xmp []byte) (*EncodeInfo, error) { ... }
```

Implementation uses `encoding/xml.Decoder` with `Token()` walks. Be lenient:
- The `<AOI0>`, `<AOI1>` ... names are ordinal; iterate any element whose name matches `/^AOI(\d+)$/`.
- `<TileJointInfo>` direction values not in `{LEFT, RIGHT, UP, DOWN}` are passed through verbatim and validated by the caller (per spec §4 caveat).
- All numeric attributes are parsed lenient — missing or empty = zero-value, not error.

- [ ] **Step 2: Test.** Use the XMP samples in `notes/2026-04-27-bif-research.md §2` as fixture inputs. Cover: spec-compliant (Ventana-1) IScan + EncodeInfo; legacy (OS-1) IScan + EncodeInfo (which has fewer attributes); malformed XMP returns a sensible error.

- [ ] **Step 3: Verify.** `go test ./internal/bifxml/... -count=1 -race`.

## Task 8: Add `TileOverlap()` to `Level` interface + zero-impls on existing formats

**Goal:** Evolve the `Level` interface (in `image.go`) with the `TileOverlap() image.Point` method per spec §8. Add zero-returning impls to every existing format's level type so the codebase still compiles before BIF lands.

**Files:**
- Modify: `image.go`, `formats/svs/tiled.go`, `formats/ndpi/striped.go`, `formats/ndpi/oneframe.go`, `formats/philips/tiled.go`, `formats/ome/tiled.go`, `internal/oneframe/oneframe.go` (only if it carries a Level impl per v0.6 outcome — check first).
- New: none.

- [ ] **Step 0: Confirm upstream.**

```sh
grep -n '## 8\.\|TileOverlap' docs/superpowers/specs/2026-04-27-opentile-go-v07-design.md
grep -n 'type Level interface' image.go
```

Confirms the spec's exact signature and where the interface is defined.

- [ ] **Step 1: Add to interface.** In `image.go`:

```go
import "image"

type Level interface {
    // ... existing methods ...

    // TileOverlap returns the pixel overlap between adjacent tiles at this level.
    // Tile (c, r) is positioned in image-space at
    //   (c · (TileSize().X - TileOverlap().X),
    //    r · (TileSize().Y - TileOverlap().Y)).
    // In the overlap region, tiles further along the row/column overwrite earlier
    // tiles (no blending). Returns image.Point{} for non-overlapping levels and
    // non-BIF formats.
    TileOverlap() image.Point
}
```

- [ ] **Step 2: Add zero-impls.** Each existing concrete level type gets:

```go
func (l *tiledImage) TileOverlap() image.Point { return image.Point{} }
```

(Adjust the receiver type per file; e.g., `stripedImage`, `oneframeImage`, etc.)

- [ ] **Step 3: Verify.** `go build ./...` succeeds; `go test ./... -count=1 -race` passes (no behavioural change expected).

## Task 9: BIF blank-tile generator (mirrors Philips R5)

**Goal:** Build the blank-tile JPEG generator that the empty-tile path will use. Single-pixel JPEGs filled with `ScanWhitePoint` (or `255` fallback for legacy iScan), tiled to TileSize via JPEG restart-marker repetition. Mirrors the Philips R5 sparse-tile path.

**Files:**
- New: `formats/bif/blanktile.go`, `formats/bif/blanktile_test.go`.
- Modify: none.

- [ ] **Step 0: Confirm upstream.**

```sh
ls -la formats/philips/blanktile.go 2>&1 | head -3
grep -n 'ScanWhitePoint\|empty JPEG-tiles' /tmp/bif-whitepaper.txt
```

Reuse the Philips approach as a template; spec mandates `ScanWhitePoint`-coloured fill.

- [ ] **Step 1: Implement.** Function signature:

```go
package bif

// blankTile returns a JPEG-encoded tile of size tileW × tileH filled with
// the given RGB white-point colour. Cached by (tileW, tileH, white) tuple.
func blankTile(tileW, tileH int, white uint8) ([]byte, error)
```

Cache key is the tuple; first call generates, subsequent calls return cached bytes. Use `internal/jpegturbo` to encode a synthetic RGB image of size tileW × tileH where every pixel is `(white, white, white)`. Quality factor 95 to match Ventana-1's encoding.

- [ ] **Step 2: Test.** Confirm: cache hit on second call returns same bytes; decoded output is uniform; fallback white=255 produces a valid JPEG.

- [ ] **Step 3: Verify.** `go test ./formats/bif/... -count=1 -race -run TestBlank`.

---

# Batch C — formats/bif core

The format package itself: detection, classification, IFD layout, level objects, tile fetch, serpentine remap. After this batch, both fixtures should `Open()` cleanly and Tile reads should return correct bytes for in-range tile coordinates.

## Task 10: Detection + factory skeleton

**Goal:** Wire BIF into `formats/all` with a working detector and an `Open()` that returns a stub `Tiler`. No tile reads yet.

**Files:**
- New: `formats/bif/bif.go`, `formats/bif/detection.go`, `formats/bif/detection_test.go`.
- Modify: `formats/all/all.go`.

- [ ] **Step 0: Confirm upstream.**

```sh
ls formats/ome/ome.go formats/ome/detection*.go 2>&1
grep -n 'INITIAL_XML_ISCAN' /tmp/openslide-ventana.c
```

Use OME's factory layout as the closest template; openslide's detection rule is `<iScan` substring inside any IFD's XMP.

- [ ] **Step 1: Implement detector.** In `formats/bif/detection.go`:

```go
// Detect returns true iff the file is a BIF candidate: BigTIFF + at least one
// IFD's XMP tag contains the substring "<iScan".
func Detect(f *tiff.File) bool {
    if !f.IsBigTIFF() { return false }
    for _, p := range f.Pages() {
        if xmp, ok := p.XMP(); ok && bytes.Contains(xmp, []byte("<iScan")) {
            return true
        }
    }
    return false
}
```

- [ ] **Step 2: Stub Tiler.** In `formats/bif/bif.go`:

```go
type Tiler struct {
    file *tiff.File
    cfg  opentile.Config
    gen  Generation // from Task 11
    // ... fields populated in later tasks
}

func Open(f *tiff.File, cfg opentile.Config) (*Tiler, error) {
    if !Detect(f) {
        return nil, opentile.ErrUnsupportedFormat
    }
    // ... full Open in Task 12 ...
    return &Tiler{file: f, cfg: cfg}, nil
}
```

- [ ] **Step 3: Register.** In `formats/all/all.go`, add the BIF factory call alongside the existing format factories.

- [ ] **Step 4: Test.** Probe both fixtures: `Detect` returns true on each, false on representative non-BIF samples (one SVS, one NDPI, one Philips, one OME). Use the integration-test pattern from `formats/ome/detection_test.go`.

- [ ] **Step 5: Verify.** `go test ./formats/bif/... ./formats/all/... -count=1 -race`.

## Task 11: Generation classification

**Goal:** Implement the spec §5.2 classifier: `strings.HasPrefix(scannerModel, "VENTANA DP")` → spec-compliant; else → legacy-iScan.

**Files:**
- New: `formats/bif/classify.go`.
- Modify: `formats/bif/bif.go` (call into classifier).

- [ ] **Step 0: Confirm upstream.**

```sh
grep -n '^### 5\.2' docs/superpowers/specs/2026-04-27-opentile-go-v07-design.md
```

- [ ] **Step 1: Implement.** In `formats/bif/classify.go`:

```go
type Generation int
const (
    GenerationSpecCompliant Generation = iota
    GenerationLegacyIScan
)

func classifyGeneration(iscan *bifxml.IScan) Generation {
    if iscan != nil && strings.HasPrefix(iscan.ScannerModel, "VENTANA DP") {
        return GenerationSpecCompliant
    }
    return GenerationLegacyIScan
}
```

- [ ] **Step 2: Wire into Open.** In `Open`, parse IFD0 XMP via `bifxml.ParseIScan`, call `classifyGeneration`, store in `Tiler.gen`. Surface via `Tiler.Metadata().Get("ventana.scanner_model")` and (internal-only) `Tiler.gen`.

- [ ] **Step 3: Test.** Both fixtures classify as expected (Ventana-1 → spec-compliant; OS-1 → legacy-iScan). Synthetic XMP with `ScannerModel="VENTANA DP 600"` → spec-compliant. With `ScannerModel="VENTANA DP 300"` → spec-compliant (prefix match). With missing attribute or `ScannerModel="VENTANA iScan Coreo"` → legacy-iScan.

- [ ] **Step 4: Verify.** `go test ./formats/bif/... -count=1 -race -run TestClassify`.

## Task 12: IFD classification + pyramid level ordering

**Goal:** Walk the IFDs, classify each by `ImageDescription`, sort the pyramid levels by parsed `level=N`. Surface as `Tiler.Images()[0].Levels()`.

**Files:**
- Modify: `formats/bif/bif.go`, `formats/bif/detection.go` (add `classifyIFD` helper).
- New: nothing; tests extend `formats/bif/detection_test.go`.

- [ ] **Step 0: Confirm upstream.**

```sh
grep -n '^### 5\.3\|level=N mag=M' docs/superpowers/specs/2026-04-27-opentile-go-v07-design.md
```

Spec §5.3 lists every recognised description value.

- [ ] **Step 1: Implement.** A pure helper:

```go
type ifdRole int
const (
    ifdRoleUnknown ifdRole = iota
    ifdRoleLabel
    ifdRoleProbability
    ifdRoleThumbnail
    ifdRolePyramid // .level set
)

type classifiedIFD struct {
    Index    int
    Role     ifdRole
    Level    int       // -1 if not pyramid
    Mag      float64
    Quality  int
    Page     *tiff.Page
}

func classifyIFD(p *tiff.Page) classifiedIFD { ... }
```

`classifyIFD` switches on `ImageDescription`:
- `"Label_Image"` or `"Label Image"` → `ifdRoleLabel`
- `"Probability_Image"` → `ifdRoleProbability`
- `"Thumbnail"` → `ifdRoleThumbnail`
- starts with `"level="` → parse 3 SPACE-separated `key=value` tokens → `ifdRolePyramid` with level/mag/quality
- anything else → `ifdRoleUnknown` (logged as warning)

- [ ] **Step 2: Wire into Open.** Build per-Tiler state:
  - `levels []classifiedIFD` sorted by `Level` ascending; backing for `Image.Levels()`.
  - `associated []classifiedIFD` for label / probability / thumbnail.

- [ ] **Step 3: Test.** Both fixtures yield expected `Image.Levels()` count: Ventana-1 → 8 levels, OS-1 → 10 levels. Synthetic IFD list with shuffled `level=N` strings → returned in level-ascending order.

- [ ] **Step 4: Verify.** `go test ./formats/bif/... -count=1 -race`.

## Task 13: Concrete `Level` impl with serpentine remap

**Goal:** Implement `formats/bif/level.go::levelImpl` satisfying the `Level` interface. Tile coordinates are accepted in image-space row-major order; internally remapped to TileOffsets serpentine order via the XMP `<Frame>` table.

**Files:**
- New: `formats/bif/level.go`, `formats/bif/level_test.go`, `formats/bif/serpentine.go`, `formats/bif/serpentine_test.go`.

- [ ] **Step 0: Confirm upstream.**

```sh
grep -n 'serpentine\|Frame-node\|XY=' /tmp/bif-whitepaper.txt
grep -n 'get_tile_coordinates' /tmp/openslide-ventana.c
```

Whitepaper §"Image stitching process" + §"IFD 2: High resolution scan" describe Frame-node ordering; openslide's `get_tile_coordinates` is the algorithmic reference.

- [ ] **Step 1: Implement serpentine algebra.** In `formats/bif/serpentine.go`, two pure helpers:

```go
// imageToSerpentine returns the index into TileOffsets for image-space tile (col, row),
// given the AOI's column/row counts. row 0 is image-top (= AOI top); col 0 is image-left.
// Stage convention: row 0 is bottom; even stage rows go left-to-right, odd go right-to-left.
func imageToSerpentine(col, row, cols, rows int) int

// serpentineToImage is the inverse — index into TileOffsets to (col, row) in image space.
func serpentineToImage(idx, cols, rows int) (col, row int)
```

Algorithm (matches openslide `get_tile_coordinates`):

```
stageRow := rows - 1 - imageRow
stageCol := col
if stageRow % 2 == 1 { stageCol = cols - 1 - col }
serpIdx := stageRow * cols + stageCol
```

Round-trip every (col, row) on every fixture's level 0 to verify correctness. *Or* — preferably — use the explicit `<Frame>` XY mapping from XMP when available; fall back to the algorithmic version when XMP is absent or inconsistent. Spec §"Image stitching process" says the algorithm is canonical; XMP just makes it explicit.

- [ ] **Step 2: Implement levelImpl.** Methods: `Index`, `PyramidIndex`, `Size`, `TileSize`, `Grid`, `Compression`, `MPP`, `FocalPlane`, `TileOverlap`, `Tile`, `TileReader`, `Tiles`. The interesting ones:
  - `TileSize` returns `(XImageSize, YImageSize)` from EncodeInfo `<AoiInfo>`.
  - `Grid` returns `(NumCols, NumRows)`.
  - `TileOverlap` returns `image.Point{X: weightedAvgOverlapX, Y: weightedAvgOverlapY}` for level 0 only; pyramid levels return `image.Point{}`.
  - `Tile(col, row)` calls `imageToSerpentine` to find the TileOffsets index, then reads the tile via `tiff.File.SectionReader` + JPEGTables prepend (Task 15).

- [ ] **Step 3: Test.** Round-trip serpentine algebra over all (col, row) on a synthetic 13×11 grid. Confirm `Tile(0, 0)` on each fixture returns plausible JPEG bytes (`FF D8 FF`).

- [ ] **Step 4: Verify.** `go test ./formats/bif/... -count=1 -race`.

## Task 14: Empty-tile path + Tile() integration

**Goal:** When `TileOffsets[i] == 0 && TileByteCounts[i] == 0`, return a blank tile (Task 9) instead of attempting a TIFF read. Wire fully into `Tile()` and `TileReader()`.

**Files:**
- Modify: `formats/bif/level.go`, `formats/bif/level_test.go`.

- [ ] **Step 0: Confirm upstream.**

```sh
grep -n 'empty image tiles\|TILEOFFSETS \[0' /tmp/bif-whitepaper.txt
```

- [ ] **Step 1: Wire blank-tile.** In `levelImpl.Tile`:

```go
serpIdx := imageToSerpentine(col, row, l.cols, l.rows)
if l.tileOffsets[serpIdx] == 0 && l.tileByteCounts[serpIdx] == 0 {
    return blankTile(l.tileSize.X, l.tileSize.Y, l.tiler.scanWhitePoint)
}
// ... non-empty path: section read + optional JPEGTables prepend ...
```

For legacy iScan (no `ScanWhitePoint` attribute), use `255`.

- [ ] **Step 2: Test.** Confirm both fixtures produce decoded blank tiles for any `(col, row)` whose serpentine index has zero offset+bytecount. If neither fixture has empty tiles per Task 4 outcome, add a synthetic test (override `tileOffsets[k] = 0; tileByteCounts[k] = 0` and confirm `Tile(...)` returns the blank result).

- [ ] **Step 3: Verify.** `go test ./formats/bif/... -count=1 -race`.

## Task 15: JPEGTables composition (shared and embedded)

**Goal:** When the IFD carries a `JPEGTables` (tag 347) shared header, prepend it to each tile's bytes before returning (per TIFF Technote 2). When the IFD doesn't have JPEGTables, tiles already carry their own SOI/DQT/SOF/DHT — return as-is.

**Files:**
- Modify: `formats/bif/level.go`.

- [ ] **Step 0: Confirm upstream.**

```sh
grep -rn 'JPEGTables\|tag 347' formats/svs/ formats/ome/ formats/ndpi/striped.go | head -5
```

The existing format implementations already handle JPEGTables composition; the BIF code reuses the same internal helper.

- [ ] **Step 1: Wire.** In `levelImpl.Tile`, when JPEGTables is present, splice using the existing `internal/jpeg` helper (look at `formats/ome/tiled.go` for the call pattern). When absent, return `b` as-is.

- [ ] **Step 2: Test.** Both fixtures produce decodable JPEGs for sampled tiles. Programmatic check: bytes start with `FF D8` (SOI) and end with `FF D9` (EOI).

- [ ] **Step 3: Verify.** `go test ./formats/bif/... -count=1 -race`.

---

# Batch D — Associated images + metadata + ICC

After Batch C, level reading works. Batch D fills in everything else hanging off the Tiler: associated images, metadata mirror, ICC profile.

## Task 16: AssociatedImage impls — label / probability / thumbnail

**Goal:** Surface label, probability (spec-compliant only), and thumbnail IFDs via `Tiler.Associated()` with kinds `"overview"`, `"probability"`, `"thumbnail"`.

**Files:**
- New: `formats/bif/associated.go`, `formats/bif/associated_test.go`.

- [ ] **Step 0: Confirm upstream.**

```sh
grep -n 'kind=\|Kind()' image.go
ls formats/svs/associated.go formats/ndpi/associated.go formats/philips/associated.go 2>&1
```

Existing format associated-image impls. SVS handles strip-based JPEGs; Philips handles JPEG-tiled-as-single-tile. BIF needs both: Ventana-1 IFD0 is uncompressed strip, OS-1 IFD0 is JPEG single-tile.

- [ ] **Step 1: Implement.** In `formats/bif/associated.go`:

```go
type associatedImage struct {
    page *tiff.Page
    kind string
}

func (a *associatedImage) Kind() string                  { return a.kind }
func (a *associatedImage) Size() opentile.Size           { ... }
func (a *associatedImage) Compression() opentile.Compression { ... }
func (a *associatedImage) Bytes() ([]byte, error)        { ... }
```

`Bytes()` switches on the IFD's compression + layout:
- Strip-based: read all strips, decompress according to compression, return the raw decoded RGB or grayscale bytes per opentile's existing convention. (Confirm: does opentile's AssociatedImage.Bytes() return *raw uncompressed pixels* or *compressed source*? Look at `formats/svs/associated.go` for the established pattern. For SVS, source=JPEG, Bytes returns the JPEG bytes — raw compressed. So BIF Ventana-1 IFD0 (Compression=NONE strip) needs to return uncompressed raw RGB; OS-1 IFD0 (Compression=JPEG single-tile) returns the JPEG bytes.)
- Single-tile JPEG: read the one tile, prepend JPEGTables if present, return JPEG bytes.

- [ ] **Step 2: Wire.** `Tiler.Associated()` returns the slice of associated images built during `Open()`:
- spec-compliant fixture: 2 entries — `kind="overview"` (Label_Image, IFD0), `kind="probability"` (Probability_Image, IFD1)
- legacy fixture: 2 entries — `kind="overview"` (Label Image, IFD0), `kind="thumbnail"` (Thumbnail, IFD1)

- [ ] **Step 3: Test.** Both fixtures: `Tiler.Associated()` returns expected kinds; `Bytes()` returns non-empty data; sizes match the IFD dimensions.

- [ ] **Step 4: Verify.** `go test ./formats/bif/... -count=1 -race -run TestAssociated`.

## Task 17: `Tiler.Metadata()` ventana.* keys

**Goal:** Mirror `<iScan>` attributes into `Tiler.Metadata()` under a `ventana.` namespace. Include AOI origins, ScanWhitePoint, ScannerModel, and other named attributes from the spec.

**Files:**
- New: `formats/bif/metadata.go`, `formats/bif/metadata_test.go`.

- [ ] **Step 0: Confirm upstream.**

```sh
grep -nE 'tiff\.ImageDescription|openslide\.\|ventana\.' /tmp/openslide-ventana.c | head -10
ls formats/svs/metadata.go formats/ome/metadata.go 2>&1
```

openslide's `ventana.*` namespace (visible in the §4 dump in the notes file) is the canonical naming. Match those keys verbatim where possible — eases consumer migration.

- [ ] **Step 1: Implement.** Build a `map[string]string` containing:
- `ventana.scanner_model` — raw `ScannerModel` (or empty)
- `ventana.magnification`, `ventana.scan_res`, `ventana.unit_number`, `ventana.user_name`, `ventana.build_version`, `ventana.build_date`
- `ventana.scan_white_point` (spec-compliant only)
- `ventana.z_layers`, `ventana.z_spacing`
- `ventana.aoi.0.left`, `.top`, `.right`, `.bottom` for each AOI
- standard opentile keys: `tiff.ImageDescription` (from IFD2), `tiff.Software`, `tiff.DateTime`
- `bif.generation` — `"spec-compliant"` or `"legacy-iscan"`

- [ ] **Step 2: Test.** Both fixtures: spot-check ~5 keys against the expected values from `notes/2026-04-27-bif-research.md §2`.

- [ ] **Step 3: Verify.** `go test ./formats/bif/... -count=1 -race -run TestMetadata`.

## Task 18: `Tiler.ICCProfile()` from IFD2

**Goal:** Surface the level-0 IFD's ICCProfile (tag 34675) via `Tiler.ICCProfile()`. Mirrors the existing SVS / OME path.

**Files:**
- Modify: `formats/bif/bif.go`.

- [ ] **Step 0: Confirm upstream.**

```sh
grep -n 'ICCProfile\|TagICCProfile\|34675' image.go internal/tiff/*.go
```

- [ ] **Step 1: Implement.** Locate IFD2 (the `level=0` pyramid level), read tag 34675 if present. Return nil if absent (spec-compliant fixtures always have it; legacy may or may not).

- [ ] **Step 2: Test.** Both fixtures: `ICCProfile()` returns non-empty bytes. Confirm bytes start with the ICC magic (`acsp` at offset 36).

- [ ] **Step 3: Verify.** `go test ./formats/bif/... -count=1 -race`.

## Task 19: Integration — Tiler wiring + Images() + edge cases

**Goal:** Glue everything together. `Tiler.Images()` returns a one-element slice with the BIF Image (single-image format). All Tiler methods — `Format()`, `Images()`, `Levels()`, `Level(i)`, `Associated()`, `Metadata()`, `ICCProfile()`, `Close()` — work end-to-end.

**Files:**
- Modify: `formats/bif/bif.go`.
- New: `formats/bif/bif_test.go`.

- [ ] **Step 0: Confirm upstream.**

```sh
grep -n 'type Tiler\|Images()' tiler.go
```

- [ ] **Step 1: Implement.** Wire all Tiler methods. `Images()` returns `[]opentile.Image{singleImageWrapper{tiler: t}}`. The wrapper's `Levels()` returns the slice built in T12; `MPP()` returns the parsed `ScanRes` value (microns per pixel — same number for X and Y per spec).

- [ ] **Step 2: Edge cases.**
  - EncodeInfo Ver < 2 → `Open()` returns an error per spec. Test with synthetic XMP.
  - Missing `<EncodeInfo>` on a pyramid-only IFD → continue (spec says no XMP on IFD3+).
  - Direction value other than `LEFT/RIGHT/UP/DOWN` → log warning, treat as RIGHT, do not error (defensive).

- [ ] **Step 3: Test.** End-to-end on both fixtures: `Open` → `Images()[0]` → for each level, `Tile(0, 0)` returns valid JPEG/raw bytes; `Associated()` returns expected kinds; `MPP()` returns expected ScanRes value (≈0.25 for Ventana-1, 0.2325 for OS-1).

- [ ] **Step 4: Verify.** `go test ./formats/bif/... ./tests/... -count=1 -race`.

---

# Batch E — Parity oracles + tests

Three oracles (per spec §7) plus fixture generation. Geometry oracle runs without a build tag; openslide and tifffile oracles run under `//go:build parity`.

## Task 20: openslide-python oracle infrastructure

**Goal:** New `tests/oracle/openslide_runner.py` that opens a slide via openslide-python and serves a JSON-RPC interface returning SHA-256 hashes of arbitrary `read_region(x, y, level, w, h)` results. Companion Go-side `openslide_session.go` mirrors the v0.6 `tifffile_session.go` shape.

**Files:**
- New: `tests/oracle/openslide_runner.py`, `tests/oracle/openslide_session.go`, `tests/oracle/openslide_test.go` (`//go:build parity`).
- Modify: `Makefile` (set `OPENTILE_ORACLE_OPENSLIDE_PYTHON` if it differs from the existing `OPENTILE_ORACLE_PYTHON`).

- [ ] **Step 0: Confirm upstream.**

```sh
ls tests/oracle/tifffile_runner.py tests/oracle/tifffile_session.go 2>&1
which openslide-show-properties && /private/tmp/opentile-py/bin/python -c 'import openslide; print(openslide.__version__)' 2>&1
```

If `import openslide` fails: `/private/tmp/opentile-py/bin/pip install openslide-python pillow`.

- [ ] **Step 1: Mirror the tifffile runner pattern.** Same RPC envelope (newline-delimited JSON over stdio), same `slide_open` / `tile_hash` / `slide_close` verbs. `tile_hash` calls `openslide.OpenSlide(path).read_region((x, y), level, (w, h))` and returns `sha256(rgba.tobytes())`. (Note: openslide returns RGBA, not RGB — the Go side has to compose the same RGBA representation when comparing.)

- [ ] **Step 2: Go-side composition.** opentile-go decodes the BIF tile via `internal/jpegturbo`, gets RGB. To match openslide, pad with alpha=255 column-major or row-major (whichever PIL uses) before hashing. **Verify the alpha layout once on a known-good crop** before treating divergent hashes as bugs.

- [ ] **Step 3: Test.** OS-1 only: pick 10 sampled tiles across levels, confirm hash equality. (Skip Ventana-1 — openslide rejects it.)

- [ ] **Step 4: Verify.** `OPENTILE_TESTDIR=$PWD/sample_files go test ./tests/oracle/... -tags parity -run TestOpenslide -count=1 -v`.

## Task 21: tifffile oracle for BIF

**Goal:** Extend the v0.6 tifffile oracle to handle BIF: open via `tifffile.TiffFile`, page-index into the BIF pyramid IFD, slice via `page.asarray()[y:y+h, x:x+w]`, hash. Both fixtures.

**Files:**
- Modify: `tests/oracle/tifffile_runner.py` (add a BIF code path or generalise the existing one), `tests/oracle/tifffile_test.go`.

- [ ] **Step 0: Confirm upstream.**

```sh
ls tests/oracle/tifffile_runner.py
grep -n 'def tile_hash\|def slide_open' tests/oracle/tifffile_runner.py | head -10
```

- [ ] **Step 1: Generalise.** The v0.6 OME path indexes into a SubIFD pyramid; BIF indexes into top-level IFDs ordered by parsed `level=N`. Extend the runner with a per-format level→page-index map computed at `slide_open` time.

- [ ] **Step 2: Apply serpentine remap.** Critical: tifffile sees raw TileOffsets in serpentine order. The Go side reads in image-space (col, row) order. When the test asks for tile (col, row) in image space, the runner must look up the matching tile in tifffile's serpentine layout — which means the runner needs the same `imageToSerpentine` algebra as `formats/bif/serpentine.go`. Port the algorithm to Python.

- [ ] **Step 3: Test.** Both fixtures: 20 sampled tiles across levels, hash equality.

- [ ] **Step 4: Verify.** `OPENTILE_TESTDIR=$PWD/sample_files go test ./tests/oracle/... -tags parity -run TestTifffile -count=1 -v`.

## Task 22: Geometry sanity tests

**Goal:** No-build-tag regression suite. Per fixture: confirm level count, level dimensions, tile dimensions, JPEG marker validity on sampled tiles, downscale factor ≈ 2× per pyramid step, AOI origins are tile-aligned, EncodeInfo Ver ≥ 2.

**Files:**
- New: `tests/parity/bif_geometry_test.go`.

- [ ] **Step 0: Confirm upstream.**

```sh
ls tests/parity/ 2>&1 | head -10
```

If `tests/parity/` doesn't exist, create it; mirror the `tests/oracle/` test scaffold (table-driven fixture iteration, `OPENTILE_TESTDIR` resolution).

- [ ] **Step 1: Implement.** Per-fixture expected-values table; pure Go test, no Python, no build tag. Runs as part of `make test`.

- [ ] **Step 2: Verify.** `OPENTILE_TESTDIR=$PWD/sample_files go test ./tests/parity/... -count=1 -race -v`.

## Task 23: Sampled-tile fixture generation

**Goal:** Generate `tests/fixtures/Ventana-1.bif.json` and `tests/fixtures/OS-1.bif.json` with sampled-tile expectations that lock in the parity behaviour. Mirrors the v0.6 fixture generation pattern.

**Files:**
- Modify: `tests/generate_test.go`.
- New: `tests/fixtures/Ventana-1.bif.json` (generated), `tests/fixtures/OS-1.bif.json` (generated).

- [ ] **Step 0: Confirm upstream.**

```sh
grep -n 'sampledByDefault\|generate' tests/generate_test.go | head -10
```

- [ ] **Step 1: Add fixtures to `sampledByDefault`.** Both BIF fixtures are >100 MB — qualify under the existing size threshold.

- [ ] **Step 2: Generate.**

```sh
OPENTILE_TESTDIR="$PWD/sample_files" \
  go test ./tests -tags generate -run TestGenerateFixtures -generate -v
```

- [ ] **Step 3: Verify.** `git status tests/fixtures/` shows the two new JSON files; `go test ./tests/... -count=1 -race` passes against them.

---

# Batch F — Docs + ship

Finalise: deviations registry, per-format reader notes, README, CHANGELOG, milestone bump, and a final validation sweep.

## Task 24: `docs/deferred.md` updates

**Goal:** R14 → ✅; new deviation entries in §1a; "Retired in v0.7" subsection; v0.7 gate-outcomes section.

**Files:**
- Modify: `docs/deferred.md`.

- [ ] **Step 0: Confirm upstream.**

```sh
grep -n '^| R14\|^### \|^## 1a\|^## 8\|Retired in v0' docs/deferred.md | head -30
```

- [ ] **Step 1: Update the R14 row** to ✅ landed with commit-range placeholder.

- [ ] **Step 2: Add §1a deviations.** Three new entries:
  - "BIF probability map exposure (since v0.7)" — we surface IFD1 of spec-compliant BIFs as an `AssociatedImage` with `kind="probability"`. Upstream opentile doesn't read BIF; this is opentile-go-only.
  - "BIF tile-overlap exposure as `Level.TileOverlap()` (since v0.7)" — additive method on the `Level` interface; non-BIF formats return zero.
  - "BIF non-strict ScannerModel acceptance (since v0.7)" — spec mandates rejecting `ScannerModel != "VENTANA DP 200"`; we accept any iScan-tagged BigTIFF and route via `strings.HasPrefix(scannerModel, "VENTANA DP")`.

- [ ] **Step 3: Add "Retired in v0.7"** subsection capturing R14 close-out and any §10 limitations that were resolved during implementation.

- [ ] **Step 4: Verify.** Render visually; cross-link from spec §6 / §8 and v0.7 plan resolves.

## Task 25: `docs/formats/bif.md` — per-format reader notes

**Goal:** Match the `docs/formats/ometiff.md` template — fixture inventory, IFD layout, classification rules, oracle coverage, known limitations.

**Files:**
- New: `docs/formats/bif.md`.

- [ ] **Step 0: Confirm upstream.**

```sh
ls docs/formats/ 2>&1
wc -l docs/formats/ometiff.md
```

- [ ] **Step 1: Mirror the ome.md structure.** Sections: 1. Overview; 2. Fixture inventory; 3. IFD classification table; 4. Generation classification; 5. Oracle coverage matrix; 6. Active limitations (link §10 of spec); 7. References.

- [ ] **Step 2: Cross-link.** README.md "BIF" entry; deferred.md §1a deviations.

## Task 26: README.md format set update

**Goal:** Bump format count to 5; add BIF row; extend the "Deviations from upstream" list with the three new v0.7 entries.

**Files:**
- Modify: `README.md`.

- [ ] **Step 0: Confirm upstream.**

```sh
grep -n 'BIF\|Format set\|Deviations\|^|' README.md | head -30
```

- [ ] **Step 1: Edit.** Update the supported-formats list, deviations list. Link out to `docs/formats/bif.md`.

## Task 27: CHANGELOG.md `[0.7.0]` entry + CLAUDE.md milestone bump

**Goal:** Standard release-prep edits.

**Files:**
- Modify: `CHANGELOG.md`, `CLAUDE.md`.

- [ ] **Step 0: Confirm upstream.**

```sh
grep -n '^## \[0\.\|^# Current milestone' CHANGELOG.md CLAUDE.md
```

- [ ] **Step 1: CHANGELOG.** New `[0.7.0]` heading. Sections: Added (BIF format support; Level.TileOverlap; ventana.* metadata; openslide oracle; tifffile BIF oracle); Changed (none beyond Level interface evolution — additive); Deferred (Z-stacks; non-zero overlap fixture; DP 600 verification).

- [ ] **Step 2: CLAUDE.md.** Bump "Current milestone" from v0.6 to v0.8 (next milestone — likely R16 Leica SCN per deferred.md). Update "Active limitations" with the three v0.7 permanent design choices noted in spec §10. Update "Deviations" with the three new entries.

## Task 28: Final validation sweep + tag

**Goal:** All gates green; tag `v0.7.0`.

- [ ] **Step 0: Sweep.**

```sh
make vet
make test
make cover
make parity
make bench  # NDPI regression gate; should be unaffected by v0.7
```

- [ ] **Step 1: Coverage.** `make cover` shows ≥80% per package, including new `formats/bif/` and `internal/bifxml/`.

- [ ] **Step 2: Confirm `feat/v0.7` branch is clean.** `git status` clean; `git log --oneline main..feat/v0.7` summarises the work.

- [ ] **Step 3: Open PR.** Title: `v0.7: Ventana BIF support`. Body cross-links spec, plan, notes, deferred.md updates, CHANGELOG entry.

- [ ] **Step 4: After merge, tag.**

```sh
git checkout main && git pull
git tag -a v0.7.0 -m "v0.7.0: Ventana BIF support" && git push --tags
```

---

## Plan-level notes

- **Task ordering rigour.** The Step 0 contract is non-negotiable. v0.4 / v0.5 / v0.6 each had a moment where an implementer proceeded without confirming upstream and shipped a regression; the cost in subagent rounds to back out always exceeded the upfront `grep` cost. BIF is *more* sensitive than v0.6 because there's no upstream Python reference — the notes file and the whitepaper are the only sources of truth, and they are denser to read than `tifffile.py`.
- **Don't guess on overlap.** Both fixtures record `OverlapX=0`, `OverlapY=0`. The `TileOverlap()` return is therefore zero on both — the *path* is exercised but the *non-zero output* never is. Implementers will be tempted to "test what we have" and call it good; that leaves the per-pair weighted-average code uncovered. Add at least one synthetic-XMP unit test that drives non-zero overlap through the parser+collapse pipeline.
- **Don't implement Z-stacks.** Multiple `<Frame>` nodes per (col, row) for `Z>0` are explicitly deferred to v0.8+. v0.7 reads only `Z=0` frames; surface `IMAGE_DEPTH > 1` via metadata so consumers know the slide is volumetric.
- **`make parity` env vars.** v0.6 set `OPENTILE_ORACLE_PYTHON`. v0.7 may need a separate `OPENTILE_ORACLE_OPENSLIDE_PYTHON` if openslide-python isn't co-installed in the same venv. Add to `Makefile` only if the pip install in T20 doesn't keep them aligned.
