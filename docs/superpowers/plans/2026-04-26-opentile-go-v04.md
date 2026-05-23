# opentile-go v0.4 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Close every real bug and parity gap on Aperio SVS and Hamamatsu NDPI. New format support stays out (Philips → v0.5).

**Architecture:** Same package layout as v0.3 plus one new cgo package: `internal/openjp2/` (libopenjp2 binding, scoped like `internal/jpegturbo/`). One new image-domain helper (`internal/imageops/` for BILINEAR resize). NDPI gains a `"map"` `AssociatedImage` Kind; SVS gains a corrupt-edge reconstruct path.

**Tech Stack:** Go 1.23+, libjpeg-turbo 2.1+ (existing), libopenjp2 2.5+ via cgo (NEW). Python opentile 0.20.0 + imagecodecs (parity-only, opt-in `//go:build parity`).

**Spec:** `docs/superpowers/specs/2026-04-26-opentile-go-v04-design.md`.

**Branch:** `feat/v0.4` from `main` after v0.3 merges (or rebase from `feat/v0.3` if v0.3 hasn't merged when v0.4 starts).

**Sample slides:** Local under `sample_files/svs/`, `sample_files/ndpi/`. Set `OPENTILE_TESTDIR=$PWD/sample_files` for integration tests. The full set: CMU-1-Small-Region.svs, CMU-1.svs, JP2K-33003-1.svs, scan_620_.svs, svs_40x_bigtiff.svs, CMU-1.ndpi, OS-2.ndpi, Hamamatsu-1.ndpi.

**Python venv:** `/private/tmp/opentile-py/bin/python` has Python 3.12 + opentile 0.20.0 + tifffile + imagecodecs (which transitively includes the libopenjp2 wrapper). Reuse for parity-oracle work and for upstream-behaviour verification commands.

**libopenjp2 install (macOS, dev machine):**
```sh
brew install openjpeg
# Verify pkg-config sees it:
pkg-config --modversion libopenjp2  # expect 2.5+
```

---

## Universal task contract: "confirm upstream first"

**Every task** in this plan starts with `Step 0: Confirm upstream`. The step:

1. Names the upstream file path + line range that governs the
   behaviour the task is implementing (typically under
   `imi-bigpicture/opentile`, `cgohlke/tifffile`, or `imagecodecs`).
2. States the rule the Go implementation must match in one or two
   sentences.
3. Includes a verification command — a `python -c "..."` invocation, a
   `grep` against upstream sources, or a documentation reference —
   that proves the rule is what the executor thinks it is. The
   executor MUST run this command before any production-code edit.

Tasks that have no direct upstream (a new cgo binding, an internal
refactor, a doc update) still carry a Step 0 — explicitly stating "no
direct upstream; this is a port-internal concern" and naming the v0.3
or earlier port commit that establishes the local convention being
extended.

The point is to make it impossible for a task to begin with "I assume
upstream does X." Two failure modes this prevents are documented in
the spec §2; reviewers reject any task whose Step 0 was skipped or
papered over.

Local upstream sources are at:
- `/private/tmp/ot/opentile/` — Python opentile 0.20.0 source
- `/private/tmp/opentile-py/lib/python3.12/site-packages/tifffile/tifffile.py` — tifffile
- `/private/tmp/opentile-py/lib/python3.12/site-packages/imagecodecs/` — imagecodecs (C extensions + Python wrappers)

---

## File structure

New files this plan creates:

| Path | Responsibility |
|---|---|
| `internal/openjp2/openjp2.go` | Public API surface (Decode, Encode, types) shared by cgo and nocgo builds |
| `internal/openjp2/openjp2_cgo.go` | cgo implementation linking libopenjp2 (build tag `cgo && !nocgo`) |
| `internal/openjp2/openjp2_nocgo.go` | nocgo stubs returning `ErrCGORequired` |
| `internal/openjp2/openjp2_cgo_test.go` | Decode/Encode round-trip + byte-parity tests |
| `internal/openjp2/testdata/sample.j2k` | Tiny known-good JP2K codestream for tests |
| `internal/imageops/bilinear.go` | BILINEAR resize, ports Pillow's `Image.resize(BILINEAR)` algorithm |
| `internal/imageops/bilinear_test.go` | Resize tests + byte-parity vs Python Pillow |
| `formats/ndpi/mappage.go` | NDPI Map page (`mag == -2.0`) AssociatedImage implementation |
| `formats/ndpi/mappage_test.go` | Tests against OS-2 / Hamamatsu-1 fixtures |
| `formats/svs/reconstruct.go` | SVS corrupt-edge detection + reconstruct (R4 port) |
| `formats/svs/reconstruct_test.go` | Reconstruct tests against synthetic + real fixtures |
| `tests/openjp2_determinism/main.go` | Throwaway harness for the JP2K determinism gate (Task 1) |

Files modified:

| Path | What changes |
|---|---|
| `opentile.go` | Add `KindMap` constant for the new AssociatedImage kind |
| `formats/ndpi/ndpi.go` | Wire Map page classifier; expose Map pages via Tiler.Associated() |
| `formats/ndpi/series.go` | Map page goes through the existing classifier; update for L6/R13 |
| `formats/ndpi/associated.go` | L17 ragged-height label cropH fix |
| `formats/ndpi/ndpi_test.go` | L17 regression test; L6/R13 integration |
| `formats/svs/svs.go` | Wire reconstruct.go into Open(); plumb parent-level reference |
| `formats/svs/tiled.go` | Replace `ErrCorruptTile` return on edge tiles with reconstruct call (when parent is set) |
| `internal/jpegturbo/turbo_cgo.go` | L12 fix or document (per Task 3 outcome) |
| `tests/fixtures/CMU-1.ndpi.json` | Regen for L6/R13 (gains Map page entry) |
| `tests/fixtures/OS-2.ndpi.json` | Regen for L6/R13 + L17 |
| `tests/fixtures/Hamamatsu-1.ndpi.json` | Regen for L6/R13 + L17 |
| `tests/fixtures/CMU-1.svs.json` | Regen if Task 16-17 enables corrupt-edge reconstruct on this slide |
| `tests/fixtures/JP2K-33003-1.svs.json` | Regen if Task 16-17 enables corrupt-edge reconstruct on this slide |
| `tests/oracle/oracle_runner.py` | Extend protocol to support `kind == "map"` |
| `tests/oracle/parity_test.go` | Drop the L10 / Map skip when implementations match upstream |
| `docs/deferred.md` | Retirement audit for v0.4 closures |
| `CLAUDE.md` | Milestone bump v0.3 → v0.4 |
| `README.md` | Spot updates for new capabilities |

---

# Batch A — JIT verification gates

Four gate tasks. Each has explicit success / failure branches; the
outcome shapes Themes B and C. Run all four before sinking any work
into the bigger fixes.

## Task 1: JP2K determinism gate

**Goal:** Confirm whether libopenjp2 produces byte-identical output
across repeated encode passes with the imagecodecs options Python
opentile uses. Outcome decides whether R4's done-when bar is byte-
parity or pixel-equivalent parity.

**Files:**
- Create: `tests/openjp2_determinism/main.go` (throwaway; remove on
  task completion if the gate passes cleanly).
- Modify: `docs/deferred.md` (record outcome; possibly add new L-item).

- [ ] **Step 0: Confirm upstream**

The encode call is at `opentile/formats/svs/svs_image.py:361-369`:
```python
return jpeg2k_encode(
    np.array(image), level=80,
    codecformat=JPEG2K.CODEC.J2K,
    colorspace=JPEG2K.CLRSPC.SRGB,
    bitspersample=self.bit_depth,
    reversible=False, mct=True,
)
```

Verification command (run before Step 1):
```sh
grep -n "jpeg2k_encode\|JPEG2K" /private/tmp/ot/opentile/formats/svs/svs_image.py
```
Expected: shows the import on line 20 and the call on lines 361-369.

The rule: imagecodecs' `jpeg2k_encode` with these exact options is
the byte-parity target. If repeated invocations produce divergent
output, byte-parity isn't achievable through libopenjp2 from either
side.

- [ ] **Step 1: Build the determinism harness (Python side)**

Create a temporary file `/tmp/jp2k_determinism.py`:
```python
import hashlib, sys
import numpy as np
from imagecodecs import JPEG2K, jpeg2k_encode, jpeg2k_decode

# Decode a known SVS JP2K tile (JP2K-33003-1.svs L0 tile 0,0). Use
# the runner-shaped path to grab the raw codestream from our existing
# integration test fixture.
import opentile
t = opentile.OpenTile.open("sample_files/svs/JP2K-33003-1.svs", 1024)
raw = t.get_level(0).get_tile((0, 0))
img = jpeg2k_decode(raw)
print(f"decoded shape={img.shape} dtype={img.dtype}", file=sys.stderr)

# Round-trip twice with identical options.
def roundtrip():
    return jpeg2k_encode(
        np.array(img), level=80,
        codecformat=JPEG2K.CODEC.J2K,
        colorspace=JPEG2K.CLRSPC.SRGB,
        bitspersample=8,
        reversible=False, mct=True,
    )

a = roundtrip()
b = roundtrip()
print(f"pass1 sha256={hashlib.sha256(a).hexdigest()} len={len(a)}")
print(f"pass2 sha256={hashlib.sha256(b).hexdigest()} len={len(b)}")
print(f"identical={a == b}")
```

- [ ] **Step 2: Run the harness**

```sh
/private/tmp/opentile-py/bin/python -P /tmp/jp2k_determinism.py
```

(`-P` disables `sys.path` script-dir prepend so `/tmp/tifffile.py`
shadow isn't an issue — see the bench harness in v0.3 for context.)

- [ ] **Step 3: Branch on outcome**

**If `identical=True`:** byte-parity is achievable. Set the v0.4
plan's R4 done-when bar to byte-equivalence. Record the SHA in
`docs/deferred.md` §5 ("Retired in v0.4") under L18/JP2K-related
notes for future verification.

**If `identical=False`:** byte-parity is not achievable through
libopenjp2 with these options. Record the divergence as a new L-item:
```markdown
### L19 — libopenjp2 jpeg2k_encode is non-deterministic
- **Source:** v0.4 Task 1 JP2K determinism gate
- **Severity:** Permanent — upstream library behaviour
- **Detail:** Round-tripping the same numpy array through
  imagecodecs.jpeg2k_encode with identical options produces different
  byte streams across passes. Pixel output decodes identical (verified
  via numpy.array_equal on jpeg2k_decode of both passes).
- **How to apply:** R4 SVS corrupt-edge reconstruct cannot guarantee
  byte-parity for JP2K-encoded slides. Pixel-equivalent parity is the
  v0.4 done-when bar instead; reconstruct's parity test compares
  decoded numpy arrays via numpy.allclose, not raw bytes.
```

Update Task 16-19's parity assertions accordingly.

- [ ] **Step 4: Cleanup + commit**

```sh
rm /tmp/jp2k_determinism.py
git add docs/deferred.md
git commit -m "docs(v0.4): T1 — JP2K determinism gate result

Outcome: <byte-parity achievable | non-deterministic, see L19>.
SHA-256 from passes 1+2: <hashes>.
Sets the R4 (SVS corrupt-edge reconstruct) done-when bar.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

## Task 2: NDPI Map fixture audit

**Goal:** Confirm at least one local NDPI slide carries a Map page
(`mag == -2.0`). If absent, defer L6/R13 to v0.5.

**Files:**
- Modify: `docs/deferred.md` (record fixture coverage; no change if
  fixtures are already adequate).

- [ ] **Step 0: Confirm upstream**

Upstream classification: `cgohlke/tifffile/tifffile.py:_series_ndpi`
classifies pages by Magnification tag (NDPI tag 65421). Map pages
have `mag == -2.0`. Python opentile's `NdpiTiler` does NOT currently
expose Map pages as a first-class associated image (this is the
divergence we're closing).

Verification command:
```sh
grep -n "mag.*-2\|Magnification" /private/tmp/ot/opentile/formats/ndpi/*.py
```
Expected: shows the magnification check sites; confirms upstream's
treatment.

- [ ] **Step 1: Audit local fixtures**

```sh
/private/tmp/opentile-py/bin/python -P -c "
import tifffile
for path in ['sample_files/ndpi/CMU-1.ndpi', 'sample_files/ndpi/OS-2.ndpi', 'sample_files/ndpi/Hamamatsu-1.ndpi']:
    print('===', path)
    with tifffile.TiffFile(path) as tf:
        for i, p in enumerate(tf.pages):
            mag = p.tags.get(65421)
            print(f'  page {i}: shape={p.shape} mag={mag.value if mag else None}')
"
```

Expected (verified at plan-write time, 2026-04-26): OS-2.ndpi page 11
and Hamamatsu-1.ndpi page 7 are Map pages (`mag == -2.0`). CMU-1.ndpi
has no Map page.

- [ ] **Step 2: Branch on outcome**

**If at least one slide carries a Map page (expected):** record the
slide(s) covering the test path; proceed to Theme B.

**If no slide carries a Map page (unexpected):** defer L6/R13 to v0.5
by editing `docs/deferred.md §1` to move R13 from v0.4 to v0.5 and
adding a note in §2 L6 explaining the fixture gap.

- [ ] **Step 3: Commit**

```sh
git add docs/deferred.md  # only if changed
git commit -m "docs(v0.4): T2 — NDPI Map fixture audit

OS-2.ndpi (page 11) and Hamamatsu-1.ndpi (page 7) carry Map pages
(mag == -2.0). CMU-1.ndpi does not. L6/R13 stays in v0.4 scope.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

## Task 3: L12 reproduction shape

**Goal:** Determine whether the NDPI edge-tile entropy divergence
between Go and Python (L12) reproduces in a minimal C-only test
calling libjpeg-turbo's `tjTransform` with the same CUSTOMFILTER
arguments — vs. only through our cgo wrapper.

**Files:**
- Create: `internal/jpegturbo/cmd_l12_repro/main.c` (throwaway C harness)
- Create: `internal/jpegturbo/cmd_l12_repro/Makefile` (build it)
- Modify: `docs/deferred.md` (record outcome)

- [ ] **Step 0: Confirm upstream**

Python opentile and Go opentile both call libjpeg-turbo's
`tjTransform` with `TJXOPT_PERFECT | TJXOPT_CROP` plus a CUSTOMFILTER
that fills OOB DCT blocks. Upstream's call site:
```sh
grep -n "tjTransform\|CUSTOMFILTER\|crop_multiple" \
  /private/tmp/opentile-py/lib/python3.12/site-packages/turbojpeg.py | head
```

The rule: identical inputs (source JPEG bytes, crop region, fill
luminance, source DQT) must produce identical output bytes. v0.3
parity oracle showed they don't on NDPI edge tiles; the question is
where the non-determinism enters.

- [ ] **Step 1: Build a C-only repro**

Source `internal/jpegturbo/cmd_l12_repro/main.c`: takes a JPEG file,
a crop region, and a luma DC value; runs `tjTransform` twice with
identical inputs; emits both outputs to disk as `out_a.jpg` /
`out_b.jpg`. (Fill in the implementation by mirroring our existing
`go_tj_transform_crop_fill` C helper in `internal/jpegturbo/turbo_cgo.go`
— same flags, same callback signature.)

- [ ] **Step 2: Run against an OS-2.ndpi edge tile**

Use a known-divergent edge tile from L12 logs (e.g. `OS-2.ndpi level 5
tile (3,0)`). Extract its source frame bytes via a small Go helper
(borrow from `formats/ndpi/striped.go::assembleFrame`) and feed to
the C harness.

```sh
cd internal/jpegturbo/cmd_l12_repro && make
./l12_repro /tmp/os2_l5_3_0_frame.jpg \
  --crop 0,0,1024,1024 --luma-dc 170
ls -la out_a.jpg out_b.jpg
sha256sum out_a.jpg out_b.jpg
```

- [ ] **Step 3: Branch on outcome**

**Case A: `out_a.jpg == out_b.jpg`** (deterministic in C, divergent
through cgo). The bug is in our cgo wrapper. Likely candidates:
uninitialised buffer reuse, callback closure capturing stack memory,
goroutine scheduler entering the C call in a different state. Fix
locally — Task 9 carries the fix.

**Case B: `out_a.jpg != out_b.jpg`** (non-deterministic in C, same
behaviour through cgo). The bug is in libjpeg-turbo's `tjTransform`
itself. File an upstream issue / PR; close L12 as
"upstream-non-deterministic, parity skipped." Update `docs/deferred.md`
to mark L12 Permanent (pending upstream fix) and remove the v0.4
target.

**Case C: `out_a.jpg == out_b.jpg` AND matches Python**'s output
byte-for-byte. The bug is somewhere between our Go call site and
the C function — likely a parameter we're computing differently.
Re-examine our luma DC derivation, region rounding, etc.

- [ ] **Step 4: Cleanup + commit**

If Case A or C: leave the C harness for Task 9 to use; commit it
with a comment noting "remove on Task 9 completion." If Case B: the
harness has done its job; remove it.

```sh
git add docs/deferred.md internal/jpegturbo/cmd_l12_repro/  # if kept
git commit -m "docs(v0.4): T3 — L12 reproduction shape

Outcome: <Case A | B | C>. <One-line consequence for L12 fix.>

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

## Task 4: R4 mechanism audit

**Goal:** Read upstream's SVS corrupt-edge reconstruct chain in
detail and document the algorithm we're porting. Identifies the Go-
side image-processing dependency before Theme B sinks libopenjp2 work.

**Files:**
- Create: `docs/superpowers/notes/2026-04-26-svs-reconstruct-port.md`
  (port notes; reviewer checks against this in Theme B)

- [ ] **Step 0: Confirm upstream**

Upstream: `opentile/formats/svs/svs_image.py`. Three relevant methods:

- `_detect_corrupt_edges` (lines 267-291): walks the right edge and
  bottom edge of a level, calling `_tile_is_corrupt` on each tile.
  A tile is corrupt iff its `databytecounts[idx] == 0`. Returns
  `(right_edge_corrupt, bottom_edge_corrupt)`.
- `_get_scaled_tile` (lines 301-372): for a corrupt tile at the
  current level, decode the corresponding 2x2 (or larger) region
  from the parent (higher-resolution) level, paste into a scratch
  raster, BILINEAR resize down to one tile, re-encode in the page's
  compression.
- `_get_fixed_tile` (lines 374-396): caches `_get_scaled_tile`
  output in `_fixed_tiles[Point]`.

Verification command:
```sh
sed -n '267,396p' /private/tmp/ot/opentile/formats/svs/svs_image.py
```
Expected: shows `_detect_corrupt_edges`, `_get_scaled_tile`,
`_get_fixed_tile` in full.

- [ ] **Step 1: Document the port plan**

Write `docs/superpowers/notes/2026-04-26-svs-reconstruct-port.md` with:

```markdown
# SVS corrupt-edge reconstruct — port notes (v0.4 R4)

## Upstream algorithm

[Summary of _detect_corrupt_edges + _get_scaled_tile + _get_fixed_tile,
including the exact shape and dtype operations on numpy arrays.]

## Go-side dependencies

1. **Decode parent tiles to raster.** For JPEG: libjpeg-turbo (we have).
   For JP2K: libopenjp2 via the new internal/openjp2 binding (Theme C
   delivers this).
2. **Paste tiles into a scratch raster.** Pure Go via stdlib `image`
   package's `image.RGBA` + manual pixel copy. No new dep.
3. **BILINEAR resize.** Pillow's `Image.resize(BILINEAR)` is the
   reference; Go stdlib has `image.NewRGBA` but no resize. Options:
   - **Port Pillow's BILINEAR** (Pillow source `src/libImaging/Resample.c`
     `ImagingResampleHorizontal_8bpc` etc. — ~200 lines C, mechanical port).
   - Use `golang.org/x/image/draw.BiLinear` (in stdlib-adjacent `x/image`).
     Almost certainly produces different bytes from Pillow.
   - Write a minimal hand-rolled BILINEAR (15 lines) — also won't be
     byte-equivalent to Pillow.

   The JP2K determinism gate (Task 1) tells us whether byte-parity is
   even our bar. If pixel-parity is the bar, `x/image/draw.BiLinear`
   probably suffices. If byte-parity is the bar, we must port Pillow's
   resampler.

4. **Re-encode.** JPEG: libjpeg-turbo `Encode` (we don't currently have
   this; we only have `Crop`). Adding `Encode` is a small extension to
   `internal/jpegturbo`. JP2K: libopenjp2 `Encode` (Theme C).

## Open question

Whether to expose corrupt-edge reconstruct as opt-in (e.g. a new
`Config.WithSVSReconstructEdges(bool)` option, default false) or always-
on. v0.3 returns ErrCorruptTile; flipping the default changes observable
behaviour. Lean toward opt-in for v0.4, default-on by v0.5.
```

- [ ] **Step 2: Branch on resize-algorithm choice**

Decide based on Task 1 outcome:

**If Task 1 → byte-parity:** target is Pillow byte-equivalence; the
plan adds an `internal/imageops/bilinear.go` ported from Pillow.

**If Task 1 → pixel-parity:** target is numpy `allclose`; Tasks 13-14
can use `golang.org/x/image/draw.BiLinear` instead of porting Pillow.
This drops `internal/imageops` from the new-files list (above) and
swaps in `golang.org/x/image` as a new module dependency.

Update the plan's File Structure table and Theme C accordingly before
proceeding.

- [ ] **Step 3: Commit**

```sh
git add docs/superpowers/notes/2026-04-26-svs-reconstruct-port.md
git commit -m "docs(v0.4): T4 — R4 mechanism audit + port notes

Documents upstream's _detect_corrupt_edges + _get_scaled_tile +
_get_fixed_tile chain. Names the Go-side dependencies (libopenjp2 via
new internal/openjp2; resize via <Pillow port | x/image/draw>).

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

# Batch B — NDPI completeness

## Task 5: L17 — NDPI label cropH ragged-height fix

**Goal:** Fix the parity divergence on OS-2 / Hamamatsu-1 NDPI labels
where our `cropH` rounds down to an MCU multiple but Python's
`crop_multiple` tolerates ragged heights via the CUSTOMFILTER.

**Files:**
- Modify: `formats/ndpi/associated.go` (cropH computation; route through
  CropWithBackground for ragged heights)
- Modify: `formats/ndpi/associated_test.go` (regression test)
- Regen: `tests/fixtures/OS-2.ndpi.json`, `tests/fixtures/Hamamatsu-1.ndpi.json`

- [ ] **Step 0: Confirm upstream**

Upstream label crop is in `opentile/formats/ndpi/ndpi_image.py`;
PyTurboJPEG's `crop_multiple` is at
`/private/tmp/opentile-py/lib/python3.12/site-packages/turbojpeg.py`.

Verification command:
```sh
grep -n "label\|crop_multiple\|cropH\|cropw\|cropy" \
  /private/tmp/ot/opentile/formats/ndpi/ndpi_image.py | head -20
```

The rule: when `image_height % mcu_height != 0`, the label cropH must
extend to the full image height (not round down) — the fill region's
final partial-MCU row must be filled rather than discarded.
PyTurboJPEG accepts a non-MCU-aligned crop_height inside crop_multiple
because its CUSTOMFILTER fills the OOB blocks.

- [ ] **Step 1: Write the failing test**

In `formats/ndpi/associated_test.go`, add:
```go
func TestNDPILabelCropHFullHeight(t *testing.T) {
    dir := os.Getenv("OPENTILE_TESTDIR")
    if dir == "" {
        t.Skip("OPENTILE_TESTDIR not set")
    }
    slide := filepath.Join(dir, "ndpi", "OS-2.ndpi")
    if _, err := os.Stat(slide); err != nil {
        t.Skipf("slide not present: %v", err)
    }
    tiler, err := opentile.OpenFile(slide)
    if err != nil {
        t.Fatal(err)
    }
    defer tiler.Close()
    var label opentile.AssociatedImage
    for _, a := range tiler.Associated() {
        if a.Kind() == "label" {
            label = a
            break
        }
    }
    if label == nil {
        t.Fatal("OS-2.ndpi has no label")
    }
    // OS-2.ndpi label is 344x396 in upstream output, was 344x392 in v0.3.
    if got := label.Size(); got.W != 344 || got.H != 396 {
        t.Errorf("OS-2 label size: got %dx%d, want 344x396", got.W, got.H)
    }
}
```

- [ ] **Step 2: Run the test, watch it fail**

```sh
OPENTILE_TESTDIR="$PWD/sample_files" go test ./formats/ndpi/... -run TestNDPILabelCropHFullHeight -v
```
Expected: FAIL with `got 344x392, want 344x396`.

- [ ] **Step 3: Implement the fix**

In `formats/ndpi/associated.go::newLabelImage`, replace the cropH
computation:

```go
// Before (v0.3):
// cropH := (overview.size.H / mcuH) * mcuH

// After:
cropH := overview.size.H  // ragged; the OOB row will be filled by
// CropWithBackgroundLuminanceOpts.
```

Route through `jpegturbo.CropWithBackgroundLuminanceOpts` (instead of
`Crop`) so the partial final MCU row gets the same white-fill the
preceding ragged column gets. Pass the cached `dcBackground` from the
overview level if available, else compute via
`jpeg.LuminanceToDCCoefficient`.

- [ ] **Step 4: Run the test**

Expected: PASS.

- [ ] **Step 5: Regen the affected fixtures**

```sh
OPENTILE_TESTDIR="$PWD/sample_files" \
  go test ./tests -tags generate -run 'TestGenerateFixtures/(OS-2|Hamamatsu-1)\.ndpi' \
    -generate -v -timeout 30m
```

- [ ] **Step 6: Re-run integration parity + parity oracle**

```sh
OPENTILE_TESTDIR="$PWD/sample_files" go test ./tests -run TestSlideParity -v -timeout 10m
OPENTILE_ORACLE_PYTHON=/private/tmp/opentile-py/bin/python OPENTILE_TESTDIR="$PWD/sample_files" \
  go test ./tests/oracle/... -tags parity -v -timeout 10m
```

Expected: green; OS-2 and Hamamatsu-1 labels are byte-identical to
Python opentile.

- [ ] **Step 7: Commit**

```sh
git add formats/ndpi/associated.go formats/ndpi/associated_test.go \
  tests/fixtures/OS-2.ndpi.json tests/fixtures/Hamamatsu-1.ndpi.json
git commit -m "fix(ndpi): L17 — NDPI label cropH extends to full image height

Routes ragged-height label crops through CropWithBackgroundLuminanceOpts
so the final partial-MCU row is filled rather than discarded. OS-2.ndpi
label is now 344x396 (was 344x392) and matches Python opentile byte-for-
byte; Hamamatsu-1.ndpi label is now 640x732 (was 640x728). CMU-1.ndpi
unaffected (image height is already a multiple of mcu_height).

Closes L17.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

## Task 6: L6 / R13 — `KindMap` constant + classifier

**Goal:** Add a `"map"` `AssociatedImage` Kind and surface NDPI Map
pages through `Tiler.Associated()`.

**Files:**
- Modify: `opentile.go` (export `KindMap` constant)
- Modify: `formats/ndpi/series.go` (classify Map pages)
- Modify: `formats/ndpi/ndpi.go` (wire Map pages into Tiler.Associated())
- Create: `formats/ndpi/mappage.go` (Map page AssociatedImage impl)

- [ ] **Step 0: Confirm upstream**

`tifffile._series_ndpi` doesn't surface Map pages — it returns them
as additional pages in the series but with no `name`. Python opentile
explicitly drops them (no `tiler.maps` property). v0.4's L6/R13 work
is a deliberate Go-side extension paralleling our v0.2 NDPI label
synthesis (L14).

Verification command:
```sh
grep -n "magnification\|-2\.0\|map\b" /private/tmp/ot/opentile/formats/ndpi/*.py
```
Expected: no `kind == "map"` references upstream — confirming this
is a Go-side extension.

- [ ] **Step 1: Add `KindMap` constant**

In `opentile.go`, near the existing `KindLabel`/`KindOverview`/
`KindThumbnail` constants:

```go
// KindMap is the AssociatedImage Kind for Hamamatsu NDPI Map pages
// (Magnification tag value -2.0). Map pages are a Hamamatsu-specific
// low-resolution overview rendering used for spatial orientation in
// NDPI viewers.
//
// Python opentile 0.20.0 does not expose Map pages as associated
// images; surfacing them is a deliberate Go-side extension paralleling
// the synthesised-label extension (see WithNDPISynthesizedLabel).
const KindMap = "map"
```

(Confirm the existing kinds are unexported strings or exported
constants by reading `opentile.go`; match the existing style.)

- [ ] **Step 2: Implement Map page AssociatedImage**

Create `formats/ndpi/mappage.go`:

```go
package ndpi

import (
    "io"

    opentile "github.com/wsilabs/opentile-go"
    "github.com/wsilabs/opentile-go/internal/tiff"
)

// mapPage is the NDPI Map page (Magnification tag value -2.0) exposed
// as an AssociatedImage with Kind() == KindMap.
//
// Map pages carry no JPEGTables (unlike thumbnail/overview); the page's
// strip is a self-contained JPEG. Bytes() returns the raw strip
// passthrough — no concatenation needed.
type mapPage struct {
    page    *tiff.Page
    size    opentile.Size
    reader  io.ReaderAt
    offset  uint64
    length  uint64
    comp    opentile.Compression
}

func newMapPage(p *tiff.Page, r io.ReaderAt) (*mapPage, error) {
    // (Mirror the simpler half of formats/ndpi/associated.go's
    // newOverviewImage — read StripOffsets/StripByteCounts, derive
    // Compression, return the struct.)
}

func (m *mapPage) Kind() string                      { return opentile.KindMap }
func (m *mapPage) Size() opentile.Size               { return m.size }
func (m *mapPage) Compression() opentile.Compression { return m.comp }
func (m *mapPage) Bytes() ([]byte, error) {
    buf := make([]byte, m.length)
    if err := tiff.ReadAtFull(m.reader, buf, int64(m.offset)); err != nil {
        return nil, err
    }
    return buf, nil
}
```

- [ ] **Step 3: Wire the classifier**

In `formats/ndpi/series.go`, the existing classifier returns
`pageMap` for `mag == -2.0` and `Factory.Open` ignores that kind.
Update to instead route Map pages into the AssociatedImage path:

```go
// In Factory.Open, after classifying pages:
for _, mp := range mapPages {
    img, err := newMapPage(pages[mp], file.ReaderAt())
    if err != nil {
        return nil, fmt.Errorf("ndpi: map page %d: %w", mp, err)
    }
    associated = append(associated, img)
}
```

- [ ] **Step 4: Write tests**

In a new `formats/ndpi/mappage_test.go`:

```go
package ndpi_test

import (
    "os"
    "path/filepath"
    "testing"

    opentile "github.com/wsilabs/opentile-go"
    _ "github.com/wsilabs/opentile-go/formats/all"
)

func TestNDPIMapPagePresentOnOS2(t *testing.T) {
    dir := os.Getenv("OPENTILE_TESTDIR")
    if dir == "" {
        t.Skip("OPENTILE_TESTDIR not set")
    }
    slide := filepath.Join(dir, "ndpi", "OS-2.ndpi")
    if _, err := os.Stat(slide); err != nil {
        t.Skipf("slide not present: %v", err)
    }
    tiler, err := opentile.OpenFile(slide)
    if err != nil {
        t.Fatal(err)
    }
    defer tiler.Close()
    var got opentile.AssociatedImage
    for _, a := range tiler.Associated() {
        if a.Kind() == opentile.KindMap {
            got = a
            break
        }
    }
    if got == nil {
        t.Fatal("OS-2.ndpi: KindMap not exposed")
    }
    // OS-2.ndpi page 11 is 198x580 per Task 2 audit.
    if size := got.Size(); size.W != 580 || size.H != 198 {
        t.Errorf("Map size: got %v, want 580x198", size)
    }
    b, err := got.Bytes()
    if err != nil {
        t.Fatalf("Bytes: %v", err)
    }
    if len(b) < 16 || b[0] != 0xFF || b[1] != 0xD8 {
        t.Errorf("Map page does not start with SOI: first 4 bytes %x", b[:4])
    }
}

func TestNDPIMapPageAbsentOnCMU1(t *testing.T) {
    // CMU-1.ndpi has no mag=-2.0 page (verified in Task 2).
    dir := os.Getenv("OPENTILE_TESTDIR")
    if dir == "" {
        t.Skip("OPENTILE_TESTDIR not set")
    }
    slide := filepath.Join(dir, "ndpi", "CMU-1.ndpi")
    if _, err := os.Stat(slide); err != nil {
        t.Skipf("slide not present: %v", err)
    }
    tiler, err := opentile.OpenFile(slide)
    if err != nil {
        t.Fatal(err)
    }
    defer tiler.Close()
    for _, a := range tiler.Associated() {
        if a.Kind() == opentile.KindMap {
            t.Errorf("CMU-1.ndpi unexpectedly has a KindMap entry")
        }
    }
}
```

- [ ] **Step 5: Run tests**

```sh
OPENTILE_TESTDIR="$PWD/sample_files" go test ./formats/ndpi/... -run NDPIMap -v
```
Expected: PASS.

- [ ] **Step 6: Commit**

```sh
git add opentile.go formats/ndpi/series.go formats/ndpi/ndpi.go \
  formats/ndpi/mappage.go formats/ndpi/mappage_test.go
git commit -m "feat(ndpi): L6, R13 — surface NDPI Map pages as KindMap

Adds opentile.KindMap and a Map page AssociatedImage implementation.
Hamamatsu-emitted NDPI files often carry a 'mag = -2.0' Map page; v0.3
silently dropped it. Tiler.Associated() now exposes one entry per Map
page, byte-passthrough.

Deliberate Go-side extension: Python opentile 0.20.0 does not expose
Map pages, paralleling our existing NDPI label synthesis.

Closes L6 / R13.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

## Task 7: L6 / R13 fixture regeneration

**Goal:** Regen OS-2 / Hamamatsu-1 NDPI fixtures so the integration
suite reflects the new Map page entry. CMU-1 has no Map page so its
fixture is unchanged.

**Files:**
- Regen: `tests/fixtures/OS-2.ndpi.json`, `tests/fixtures/Hamamatsu-1.ndpi.json`

- [ ] **Step 0: Confirm upstream**

No upstream behaviour to confirm — this is a fixture regeneration
following Task 6's classifier change. The fixture format is owned by
this repo (see `tests/fixtures.go`); no external contract.

- [ ] **Step 1: Regen**

```sh
OPENTILE_TESTDIR="$PWD/sample_files" \
  go test ./tests -tags generate -run 'TestGenerateFixtures/(OS-2|Hamamatsu-1)\.ndpi' \
    -generate -v -timeout 30m
```

Expected: each fixture's `associated_images` array gains a `kind:
"map"` entry with the SHA-256 of the Map page bytes.

- [ ] **Step 2: Re-run integration parity**

```sh
OPENTILE_TESTDIR="$PWD/sample_files" go test ./tests -run TestSlideParity -v -timeout 10m
```
Expected: green on all fixtures.

- [ ] **Step 3: Commit**

```sh
git add tests/fixtures/OS-2.ndpi.json tests/fixtures/Hamamatsu-1.ndpi.json
git commit -m "test(ndpi): regen OS-2 + Hamamatsu-1 fixtures with KindMap entries

Follows Task 6 (L6/R13). Each fixture's associated_images array gains a
'kind: map' entry. CMU-1.ndpi unchanged (no Map page).

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

## Task 8: L6 / R13 parity oracle support

**Goal:** Extend the Python parity runner to support `kind == "map"`,
since Python opentile doesn't expose Map pages we treat the response
as zero-length (skip), and the parity oracle just confirms our Go
output exists. The parity-test side adds the Map kind to the
associated-image loop.

**Files:**
- Modify: `tests/oracle/oracle_runner.py`
- Modify: `tests/oracle/parity_test.go`

- [ ] **Step 0: Confirm upstream**

Confirmed in Task 2 / Task 6: Python opentile has no `tiler.maps`
property. Calling our runner with `associated map` returns zero-length
(matches the existing skip semantics for kinds Python doesn't expose).

- [ ] **Step 1: Update the runner**

In `tests/oracle/oracle_runner.py`, the `_associated_for(tiler, kind)`
helper currently returns `[]` for unknown kinds (see runner source).
Add an explicit case for `"map"` returning `[]` so the intent is
visible in source rather than implicit:

```python
def _associated_for(tiler, kind: str):
    if kind == "label":
        return tiler.labels
    if kind == "overview":
        return tiler.overviews
    if kind == "thumbnail":
        return tiler.thumbnails
    if kind == "map":
        # Python opentile does not expose Map pages. Returning [] makes
        # the runner emit a zero-length response — the Go parity test
        # treats this as "skip parity" but still verifies our Go side
        # produces a non-empty Map image.
        return []
    return []
```

- [ ] **Step 2: Update parity_test.go**

The existing associated-image loop in `runParityOnSlide` already
handles zero-length responses as skip. No code change needed; verify
by re-running the oracle. (If there's a hardcoded kind list anywhere
in the test that excludes "map," extend it here.)

- [ ] **Step 3: Run the oracle**

```sh
OPENTILE_ORACLE_PYTHON=/private/tmp/opentile-py/bin/python OPENTILE_TESTDIR="$PWD/sample_files" \
  go test ./tests/oracle/... -tags parity -v -timeout 10m
```

Expected: PASS, with new `t.Logf` lines like
`slide OS-2.ndpi associated "map": Python opentile not exposing — Go side produced N bytes`.

- [ ] **Step 4: Commit**

```sh
git add tests/oracle/oracle_runner.py tests/oracle/parity_test.go
git commit -m "test(oracle): map-kind support in batched parity runner

Explicit case in _associated_for for kind == 'map'; returns [] so the
Go parity test treats Python's response as 'not exposed, skip parity'
while still confirming our Go output exists.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

## Task 9: L12 fix — geometry-first OOB dispatch (Case D per T3)

**Background — what T3 found.** The plan originally offered Cases
A/B/C for Task 3's outcome (cgo bug / upstream non-determinism /
call-site parameter). The actual finding (`docs/deferred.md` §6 Task
3) is **Case D — control-flow bug in our Go-side dispatch**:

- Both Go and Python sides are individually byte-deterministic.
- In-image pixels match Python pixel-for-pixel.
- Only the OOB strip diverges: Go produces RGB(128,128,128)
  mid-gray; Python produces RGB(255,255,255) white.
- Our cached `dcBackground` is correct (170 for luminance=1.0 on
  the typical NDPI luma DQT) — we just never use it on these tiles
  because `Crop` succeeds first and we skip the
  `CropWithBackgroundLuminanceOpts` path entirely.
- Python's `__need_fill_background` (`turbojpeg.py:839-863`) is
  geometry-only: route through CUSTOMFILTER iff the crop region
  extends past `image_size` AND luminance ≠ 0.5. No "try Crop first"
  pattern.

This is the v0.3 T30 attempt that landed in `acc2282` as a revert.
The revert was wrong — T30's geometry-first inversion was correct
in spirit, but it used `frameSize` instead of `image_size` as the
geometry comparator, and the v0.3 fixtures already encoded the
buggy mid-gray output (which is what made T30's "correct" output
look like a regression). v0.4 lands the right fix and regenerates
the fixtures.

**Files:**
- Modify: `formats/ndpi/striped.go` (Tile method's OOB dispatch)
- Regen: `tests/fixtures/OS-2.ndpi.json`, `tests/fixtures/Hamamatsu-1.ndpi.json`

- [ ] **Step 0: Confirm upstream**

Re-read `turbojpeg.py:839-863` (`__need_fill_background`). Run:
```sh
sed -n '839,863p' /private/tmp/opentile-py/lib/python3.12/site-packages/turbojpeg.py
```
Expected: the gate is `(crop_region.x + crop_region.w > image_size[0]) OR
(crop_region.y + crop_region.h > image_size[1])`, AND
`background_luminance != 0.5`. The geometry comparator is
`image_size`, not the assembled-frame size.

The rule the Go fix must match: dispatch on tile-position-vs-image-size,
not tile-position-vs-frame-size.

- [ ] **Step 1: Write the failing parity oracle assertion**

Pick one known-divergent tile (e.g. OS-2.ndpi L5 (3,0)). Add a Go
test in `formats/ndpi/striped_test.go` (new file or append) that
opens OS-2.ndpi, decodes the Tile (3,0) at L5, and asserts the OOB
strip (cols 896-1023) is white (RGB ≥ 250 per channel). Run the
test before the fix to confirm it fails with mid-gray output:

```sh
OPENTILE_TESTDIR="$PWD/sample_files" go test ./formats/ndpi/... -run TestL12OOBFillIsWhite -v
```
Expected: FAIL — sees RGB(128,128,128) instead of RGB(255,255,255).

- [ ] **Step 2: Apply the dispatch change**

In `formats/ndpi/striped.go::Tile`, replace the current pattern
(post-`acc2282`):

```go
// CURRENT (buggy):
out, err := jpegturbo.Crop(frame, region)
if err != nil {
    extendsBeyond := left+l.tileSize.W > frameSize.W || top+l.tileSize.H > frameSize.H
    if extendsBeyond {
        out, err = jpegturbo.CropWithBackgroundLuminanceOpts(...)
    }
    if err != nil { return nil, &opentile.TileError{...} }
}
return out, nil
```

with:

```go
// FIX (matches Python's __need_fill_background):
//
// Dispatch on whether the tile (in IMAGE coordinates) extends past
// the image bounds — not on whether plain Crop happens to succeed
// on the assembled-frame geometry. Aligned with
// turbojpeg.py:839-863's gate.
tileXOrigin := x * l.tileSize.W
tileYOrigin := y * l.tileSize.H
extendsBeyond := tileXOrigin+l.tileSize.W > l.size.W || tileYOrigin+l.tileSize.H > l.size.H
var out []byte
if extendsBeyond {
    out, err = jpegturbo.CropWithBackgroundLuminanceOpts(
        frame, region, jpegturbo.DefaultBackgroundLuminance,
        jpegturbo.CropOpts{DCBackground: l.dcBackground},
    )
} else {
    out, err = jpegturbo.Crop(frame, region)
}
if err != nil {
    return nil, &opentile.TileError{Level: l.index, X: x, Y: y, Err: err}
}
return out, nil
```

Remove the v0.3 `acc2282` "geometry-first inversion is unsafe"
comment that justified the broken try-Crop-first dispatch — the
inversion IS safe; what was unsafe was using `frameSize` instead
of image-size as the comparator. Replace with a comment naming
this fix:

```go
// Dispatch matches Python's __need_fill_background gate at
// turbojpeg.py:839-863: CropWithBackground iff the tile crosses
// the image edge in image coordinates. The v0.3 try-Crop-first
// pattern silently returned mid-gray OOB fills (libjpeg-turbo's
// default) on tiles where Crop succeeded despite extending past
// the image — see L12 / Task 9 for the full reproduction.
```

- [ ] **Step 3: Run the test, watch it pass**

```sh
OPENTILE_TESTDIR="$PWD/sample_files" go test ./formats/ndpi/... -run TestL12OOBFillIsWhite -v
```
Expected: PASS.

- [ ] **Step 4: Regenerate the affected NDPI fixtures**

```sh
OPENTILE_TESTDIR="$PWD/sample_files" \
  go test ./tests -tags generate -run 'TestGenerateFixtures/(OS-2|Hamamatsu-1)\.ndpi' \
    -generate -v -timeout 30m
```

Expected: each fixture's affected edge tiles get new (correct) SHAs.
The CMU-1.ndpi fixture is untouched (its tile sizes divide its
image dimensions evenly, so no edge tiles need OOB fill).

- [ ] **Step 5: Run the full integration parity sweep**

```sh
OPENTILE_TESTDIR="$PWD/sample_files" go test ./tests -run TestSlideParity -v -timeout 10m
```
Expected: green on all 8 fixtures.

- [ ] **Step 6: Run the parity oracle**

```sh
OPENTILE_ORACLE_PYTHON=/private/tmp/opentile-py/bin/python OPENTILE_TESTDIR="$PWD/sample_files" \
  go test ./tests/oracle/... -tags parity -v -timeout 10m
```
Expected: the L12 `t.Logf` divergence lines disappear from the
output. Every previously-divergent NDPI edge tile now byte-matches
Python opentile.

- [ ] **Step 7: Remove the parity-oracle L12 carve-out**

In `tests/oracle/parity_test.go`, find the block that downgrades
NDPI edge-tile divergences to `t.Logf` (the `if isNDPI && isEdge`
branch). Delete it — divergences are now real failures again. Keep
only the L10 SVS-label carve-out.

- [ ] **Step 8: Commit**

```sh
git add formats/ndpi/striped.go formats/ndpi/striped_test.go \
  tests/fixtures/OS-2.ndpi.json tests/fixtures/Hamamatsu-1.ndpi.json \
  tests/oracle/parity_test.go
git commit -m "fix(ndpi): L12 — geometry-first OOB dispatch matches Python

Closes L12 — the NDPI edge-tile divergence flagged from v0.2 onward.
Root cause was control-flow, not entropy encoding:

Python (turbojpeg.py:839-863) decides geometry-first — route
through CUSTOMFILTER iff the crop region extends past image_size.
Our striped.go::Tile tried plain Crop first; for edge tiles where
Crop succeeded despite extending past the image, we never reached
the CropWithBackground path and silently returned libjpeg-turbo's
default mid-gray OOB fill (DC=0) instead of the cached white-fill
(DC=170 for luminance=1.0).

Fix: dispatch on tile-position-vs-image-size up front; matches
__need_fill_background's gate exactly.

OS-2.ndpi and Hamamatsu-1.ndpi fixtures regenerated. The
parity_test.go L12 t.Logf carve-out is removed; every NDPI tile is
now byte-equal to Python opentile.

Closes L12.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

Note: this commit reverses the v0.3 \`acc2282\` "geometry-first
inversion is unsafe" claim. The inversion itself was safe; the
v0.3 attempt used the wrong comparator (assembled-frame size
instead of image size) and the v0.3 fixtures already encoded the
buggy output, masking the fact that the inversion produced the
correct bytes.

---

# Batch C — libopenjp2 binding (R9 prerequisite for R4)

## Task 10: `internal/openjp2` package skeleton

**Goal:** Add the cgo + nocgo skeleton for the libopenjp2 binding,
mirroring the v0.2 layout of `internal/jpegturbo/`.

**Files:**
- Create: `internal/openjp2/openjp2.go`
- Create: `internal/openjp2/openjp2_cgo.go`
- Create: `internal/openjp2/openjp2_nocgo.go`

- [ ] **Step 0: Confirm upstream**

No direct upstream — this is a port-internal cgo binding extending
the v0.2 `internal/jpegturbo` pattern. The reference is our own
`internal/jpegturbo/turbo.go` / `turbo_cgo.go` / `turbo_nocgo.go`
file split.

For library API: imagecodecs' `_jpeg2k.pyx`/`_jpeg2k_codec.h` show
the exact OpenJPEG symbols imagecodecs calls. Verification:
```sh
grep -rn "opj_decompress\|opj_encode\|opj_create_compress" \
  /private/tmp/opentile-py/lib/python3.12/site-packages/imagecodecs/ 2>&1 | head -10
```
(May produce empty output if imagecodecs ships only the .so; that's
fine — we'll work from openjp2's own headers.)

- [ ] **Step 1: Write the public API surface**

Create `internal/openjp2/openjp2.go` (build-tag-agnostic):

```go
// Package openjp2 provides a minimal cgo wrapper over OpenJPEG's
// libopenjp2 for JPEG 2000 decode and encode. Used by formats/svs's
// corrupt-edge reconstruct path on JP2K-encoded SVS slides
// (R4/R9, v0.4).
//
// Default builds link libopenjp2 2.5+ via pkg-config. The `nocgo`
// build tag (or CGO_ENABLED=0) swaps in stubs returning ErrCGORequired
// so SVS-only consumers without a JP2K slide can still build and run.
package openjp2

import "errors"

// ErrCGORequired is returned from Decode / Encode when the package
// was compiled without cgo support. Callers propagate this wrapped in
// opentile.TileError.
var ErrCGORequired = errors.New("openjp2: this operation requires cgo + libopenjp2 (build without -tags nocgo)")

// EncodeOpts configures Encode. Defaults match imagecodecs.jpeg2k_encode
// (the upstream Python opentile reference): level=80, codecformat=J2K,
// colorspace=SRGB, mct=true, reversible=false, bitspersample=8.
type EncodeOpts struct {
    // Level is the lossy compression level [0,100]. 100 = maximum
    // quality / largest output.
    Level int
    // ReversibleTransform selects the 5/3 wavelet (lossless) when true,
    // 9/7 (lossy) when false.
    ReversibleTransform bool
    // MCT selects the multi-component transform (luma/chroma decorrelation)
    // when true.
    MCT bool
    // BitsPerSample is the per-sample precision; 8 for typical SVS.
    BitsPerSample int
}

// DefaultEncodeOpts mirrors imagecodecs.jpeg2k_encode defaults used by
// Python opentile in opentile/formats/svs/svs_image.py:361-369.
var DefaultEncodeOpts = EncodeOpts{
    Level: 80,
    ReversibleTransform: false,
    MCT: true,
    BitsPerSample: 8,
}
```

- [ ] **Step 2: Write the cgo implementation skeleton**

Create `internal/openjp2/openjp2_cgo.go`:

```go
//go:build cgo && !nocgo

package openjp2

/*
#cgo pkg-config: libopenjp2
#include <openjpeg.h>
*/
import "C"

import (
    "errors"
    "fmt"
)

// Decode parses src as a JPEG 2000 codestream and returns interleaved
// RGBA8 pixels (width*height*4 bytes), plus the image dimensions.
//
// Currently supports 8-bit-per-sample 3-channel (RGB) inputs only —
// matches every JP2K slide we have. Other configurations return an
// error rather than silently producing incorrect output.
func Decode(src []byte) (pixels []byte, width, height int, err error) {
    if len(src) == 0 {
        return nil, 0, 0, errors.New("openjp2: empty source")
    }
    return nil, 0, 0, errors.New("TODO Task 11")
}

// Encode emits a JPEG 2000 codestream from interleaved RGBA8 pixels
// of the given dimensions. opts.Level / ReversibleTransform / MCT /
// BitsPerSample control the output; DefaultEncodeOpts mirrors the
// imagecodecs.jpeg2k_encode call upstream Python opentile uses.
func Encode(pixels []byte, width, height int, opts EncodeOpts) ([]byte, error) {
    if len(pixels) == 0 {
        return nil, errors.New("openjp2: empty pixel buffer")
    }
    if width*height*4 != len(pixels) {
        return nil, fmt.Errorf("openjp2: pixel buffer size %d != width*height*4 (%d*%d*4 = %d)",
            len(pixels), width, height, width*height*4)
    }
    return nil, errors.New("TODO Task 12")
}
```

- [ ] **Step 3: Write the nocgo stub**

Create `internal/openjp2/openjp2_nocgo.go`:

```go
//go:build !cgo || nocgo

package openjp2

func Decode(src []byte) (pixels []byte, width, height int, err error) {
    return nil, 0, 0, ErrCGORequired
}

func Encode(pixels []byte, width, height int, opts EncodeOpts) ([]byte, error) {
    return nil, ErrCGORequired
}
```

- [ ] **Step 4: Verify build**

```sh
go build ./internal/openjp2/...
go build -tags nocgo ./internal/openjp2/...
```

Both should succeed. The cgo build links libopenjp2; the nocgo build
returns `ErrCGORequired` from both functions.

- [ ] **Step 5: Commit**

```sh
git add internal/openjp2/openjp2.go internal/openjp2/openjp2_cgo.go internal/openjp2/openjp2_nocgo.go
git commit -m "feat(openjp2): R9 — internal/openjp2 package skeleton

cgo + nocgo file split mirroring internal/jpegturbo. Public API:
Decode (returns RGBA8 pixels + dimensions) and Encode (mirrors
imagecodecs.jpeg2k_encode defaults). Tasks 11+12 fill in the
implementations.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

## Task 11: `Decode` implementation

**Goal:** Implement JP2K decode against a known-good fixture; verify
output matches imagecodecs' `jpeg2k_decode` byte-for-byte (or pixel-
for-pixel per Task 1's outcome).

**Files:**
- Modify: `internal/openjp2/openjp2_cgo.go`
- Create: `internal/openjp2/openjp2_cgo_test.go`
- Create: `internal/openjp2/testdata/sample.j2k`

- [ ] **Step 0: Confirm upstream**

imagecodecs' decode wraps OpenJPEG's `opj_decompress` flow — set up
a stream from the codestream bytes, create a decoder, decode into an
`opj_image_t`, copy components into an output array.

Verification: walk OpenJPEG's `examples/opj_decompress.c` (installed
under `$(brew --prefix openjpeg)/share/doc/openjpeg/examples/`) and
confirm the function call sequence. Run:
```sh
ls $(brew --prefix openjpeg)/share/doc/openjpeg-*/examples/ 2>/dev/null \
  || find $(brew --prefix openjpeg) -name "opj_decompress.c"
```

The rule: outputs are interleaved RGBA8 in memory layout
`[r0,g0,b0,255, r1,g1,b1,255, ...]`. Alpha is always 255 since SVS
JP2K is 3-channel.

- [ ] **Step 1: Generate the test fixture**

Use a known JP2K tile from JP2K-33003-1.svs as the input fixture.
Extract via Python:
```sh
/private/tmp/opentile-py/bin/python -P -c "
import opentile
t = opentile.OpenTile.open('sample_files/svs/JP2K-33003-1.svs', 1024)
raw = t.get_level(0).get_tile((0, 0))
with open('internal/openjp2/testdata/sample.j2k', 'wb') as f:
    f.write(raw)
print(f'wrote {len(raw)} bytes')
"
```

Add a comment in the testdata directory's README (create if missing)
noting this fixture is JP2K-encoded RGB at the slide's L0 tile size.

- [ ] **Step 2: Write the failing test**

Create `internal/openjp2/openjp2_cgo_test.go`:

```go
//go:build cgo && !nocgo

package openjp2

import (
    "bytes"
    "os"
    "testing"
)

func TestDecodeSampleJP2K(t *testing.T) {
    src, err := os.ReadFile("testdata/sample.j2k")
    if err != nil {
        t.Skipf("testdata/sample.j2k not present: %v", err)
    }
    pixels, w, h, err := Decode(src)
    if err != nil {
        t.Fatalf("Decode: %v", err)
    }
    // Sample.j2k is JP2K-33003-1.svs L0 tile (0,0) — 240x240 RGB
    // (verify with: python -c "from imagecodecs import jpeg2k_decode;
    //   import sys; img = jpeg2k_decode(open('testdata/sample.j2k','rb').read());
    //   print(img.shape)")
    if w != 240 || h != 240 {
        t.Errorf("dims: got %dx%d, want 240x240", w, h)
    }
    if got, want := len(pixels), w*h*4; got != want {
        t.Errorf("pixels: got %d bytes, want %d", got, want)
    }
    // Alpha channel must be 255 throughout.
    for i := 3; i < len(pixels); i += 4 {
        if pixels[i] != 255 {
            t.Errorf("alpha at byte %d: got %d, want 255", i, pixels[i])
            break
        }
    }
    _ = bytes.Equal(nil, nil) // imports keep happy until later assertions
}
```

- [ ] **Step 3: Verify the test fails for the right reason**

```sh
go test ./internal/openjp2/... -run TestDecodeSampleJP2K -v
```
Expected: FAIL with "TODO Task 11" message from the skeleton.

- [ ] **Step 4: Implement Decode**

Replace the `Decode` function in `openjp2_cgo.go`. Full
implementation: ~50 lines of cgo, mirroring `opj_decompress.c`'s
stream → decoder → image flow. Use `opj_create_decompress(OPJ_CODEC_J2K)`
for raw codestreams (vs. `OPJ_CODEC_JP2` for boxed JP2 files; SVS uses
J2K).

The exact code is too long for inline here; the implementer follows
`/usr/local/Cellar/openjpeg/.../share/doc/openjpeg/examples/opj_decompress.c`
or equivalent. Key decisions to record in code comments:

1. Use a memory-stream callback set (no temp files). OpenJPEG's
   `opj_stream_set_user_data_length` lets us avoid `lseek`.
2. After decode, the `opj_image_t` has 3 separate planes (RGB);
   interleave into RGBA8 and write alpha=255.
3. Free with `opj_image_destroy`, `opj_destroy_codec`,
   `opj_stream_destroy`.

- [ ] **Step 5: Run the test**

```sh
go test ./internal/openjp2/... -run TestDecodeSampleJP2K -v
```
Expected: PASS.

- [ ] **Step 6: Add a Python-parity assertion**

Extend the test to compare a few sampled pixels against
imagecodecs' decoded output:

```go
func TestDecodeMatchesImagecodecs(t *testing.T) {
    src, err := os.ReadFile("testdata/sample.j2k")
    if err != nil {
        t.Skipf("testdata/sample.j2k not present: %v", err)
    }
    pixels, w, h, err := Decode(src)
    if err != nil {
        t.Fatal(err)
    }
    // Reference pixel values pre-extracted via Python:
    //   /private/tmp/opentile-py/bin/python -P -c "
    //     from imagecodecs import jpeg2k_decode
    //     img = jpeg2k_decode(open('internal/openjp2/testdata/sample.j2k','rb').read())
    //     for x, y in [(0,0), (100,100), (239,239)]:
    //       r, g, b = img[y, x]
    //       print(f'({x},{y}): RGB=({r},{g},{b})')
    //   "
    type sample struct {
        x, y, r, g, b int
    }
    refs := []sample{
        // Fill in actual values from the Python invocation above.
        // Plan author can't pre-fill these without running the
        // command. Implementer MUST fill in before committing.
        {0, 0, /* r */ 0, /* g */ 0, /* b */ 0},
        {100, 100, 0, 0, 0},
        {239, 239, 0, 0, 0},
    }
    for _, s := range refs {
        idx := (s.y*w + s.x) * 4
        gotR, gotG, gotB := int(pixels[idx]), int(pixels[idx+1]), int(pixels[idx+2])
        if gotR != s.r || gotG != s.g || gotB != s.b {
            t.Errorf("pixel (%d,%d): got RGB=(%d,%d,%d), want (%d,%d,%d)",
                s.x, s.y, gotR, gotG, gotB, s.r, s.g, s.b)
        }
    }
    _ = h
}
```

The `refs` table values come from the Python verification command in
the test comment. Run that command, fill in the actuals, then re-run
the test.

- [ ] **Step 7: Run + commit**

```sh
go test ./internal/openjp2/... -v
git add internal/openjp2/openjp2_cgo.go internal/openjp2/openjp2_cgo_test.go internal/openjp2/testdata/
git commit -m "feat(openjp2): R9 — Decode implementation + imagecodecs parity

Pixel output matches imagecodecs.jpeg2k_decode at three sampled
positions on JP2K-33003-1.svs L0 tile (0,0). Alpha channel is 255
throughout; output layout is interleaved RGBA8.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

## Task 12: `Encode` implementation

**Goal:** Implement JP2K encode with imagecodecs-equivalent options;
verify output matches the Task 1 determinism gate's bar (byte-parity
or pixel-parity).

**Files:**
- Modify: `internal/openjp2/openjp2_cgo.go`
- Modify: `internal/openjp2/openjp2_cgo_test.go`

- [ ] **Step 0: Confirm upstream**

imagecodecs' encode wraps OpenJPEG's `opj_create_compress` flow.
The mapping from imagecodecs options to OpenJPEG `opj_cparameters_t`:

| imagecodecs option | `opj_cparameters_t` field | Value used by Python opentile |
|---|---|---|
| `level=80` | `tcp_rates[0]` | `100/80 = 1.25` (compression ratio) |
| `codecformat=J2K` | (codec creation arg) | `OPJ_CODEC_J2K` |
| `colorspace=SRGB` | `image->color_space` | `OPJ_CLRSPC_SRGB` |
| `mct=True` | `tcp_mct` | `1` |
| `reversible=False` | `irreversible` | `1` (OpenJPEG inverts the bool) |
| `bitspersample=8` | `image->comps[i].prec` | `8` |

Verification:
```sh
grep -A5 "tcp_rates\|tcp_mct\|irreversible\|cp_disto_alloc" \
  $(brew --prefix openjpeg)/share/doc/openjpeg-*/examples/opj_compress.c 2>/dev/null \
  | head -40
```

The rule: every `opj_cparameters_t` field set by `imagecodecs`
must be set identically here. Mismatches produce different bytes.

- [ ] **Step 1: Write the round-trip test**

Add to `openjp2_cgo_test.go`:

```go
func TestEncodeRoundTrip(t *testing.T) {
    src, err := os.ReadFile("testdata/sample.j2k")
    if err != nil {
        t.Skipf("testdata/sample.j2k not present: %v", err)
    }
    pixels, w, h, err := Decode(src)
    if err != nil {
        t.Fatal(err)
    }
    encoded, err := Encode(pixels, w, h, DefaultEncodeOpts)
    if err != nil {
        t.Fatalf("Encode: %v", err)
    }
    // Round-trip: re-decode and confirm pixels match within tolerance.
    pixels2, w2, h2, err := Decode(encoded)
    if err != nil {
        t.Fatalf("Decode after Encode: %v", err)
    }
    if w2 != w || h2 != h {
        t.Errorf("round-trip dims: got %dx%d, want %dx%d", w2, h2, w, h)
    }
    // Lossy: pixels differ; we only check shape + that no pixel
    // diverges by more than 8 LSBs (typical for JP2K at level=80).
    diff := 0
    for i := 0; i < len(pixels); i++ {
        if i%4 == 3 { continue } // skip alpha
        d := int(pixels[i]) - int(pixels2[i])
        if d < 0 { d = -d }
        if d > 8 { diff++ }
    }
    if diff > len(pixels)/100 {  // < 1% of bytes diverge by >8 LSBs
        t.Errorf("round-trip pixel divergence: %d bytes (%.2f%%) differ by >8 LSBs",
            diff, float64(diff)*100/float64(len(pixels)))
    }
}
```

- [ ] **Step 2: Implement Encode**

Replace `Encode` in `openjp2_cgo.go`. ~80 lines mirroring
`opj_compress.c`'s flow. Set every `opj_cparameters_t` field per the
table in Step 0.

- [ ] **Step 3: Run the test**

Expected: PASS (lossy round-trip is within tolerance).

- [ ] **Step 4: Add the parity test (per Task 1 outcome)**

**If Task 1 → byte-parity:** add a test comparing our `Encode`
output against imagecodecs.jpeg2k_encode output byte-for-byte. The
test extracts a known input via Python, encodes both ways, hashes
both:

```go
func TestEncodeMatchesImagecodecsBytes(t *testing.T) {
    // Reference bytes from:
    //   /private/tmp/opentile-py/bin/python -P -c "
    //     import numpy as np
    //     from imagecodecs import JPEG2K, jpeg2k_encode, jpeg2k_decode
    //     img = jpeg2k_decode(open('internal/openjp2/testdata/sample.j2k','rb').read())
    //     out = jpeg2k_encode(np.array(img), level=80,
    //       codecformat=JPEG2K.CODEC.J2K, colorspace=JPEG2K.CLRSPC.SRGB,
    //       bitspersample=8, reversible=False, mct=True)
    //     import hashlib; print(hashlib.sha256(out).hexdigest(), len(out))
    //   "
    const wantLen = 0      // implementer fills in
    const wantSHA = "..."  // implementer fills in
    pixels, w, h, _ := Decode(...)  // load same input
    out, err := Encode(pixels, w, h, DefaultEncodeOpts)
    if err != nil { t.Fatal(err) }
    if len(out) != wantLen { /* ... */ }
    sha := sha256.Sum256(out)
    if hex.EncodeToString(sha[:]) != wantSHA { /* ... */ }
}
```

**If Task 1 → pixel-parity:** add a test comparing the round-trip
decode of our encode vs imagecodecs' encode + decode, asserting
within-tolerance pixel match (similar to Step 1 but cross-encoder).

- [ ] **Step 5: Run + commit**

```sh
go test ./internal/openjp2/... -v
git add internal/openjp2/openjp2_cgo.go internal/openjp2/openjp2_cgo_test.go
git commit -m "feat(openjp2): R9 — Encode implementation, <byte|pixel>-parity vs imagecodecs

Output matches imagecodecs.jpeg2k_encode <byte-for-byte | within JP2K
quantisation tolerance> on the JP2K-33003-1.svs L0 tile fixture.

DefaultEncodeOpts mirrors Python opentile's call (level=80,
codecformat=J2K, colorspace=SRGB, mct=true, reversible=false,
bitspersample=8).

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

# Batch D — SVS image-domain processing

## Task 13: BILINEAR resize (per Task 4 outcome)

**Goal:** Provide the BILINEAR-resize primitive R4 needs to
reconstruct corrupt edge tiles. Choice of implementation depends on
Task 4's resize-algorithm-choice branch.

**Files (if Task 4 → Pillow port):**
- Create: `internal/imageops/bilinear.go` (~200 lines, mechanical port)
- Create: `internal/imageops/bilinear_test.go` (parity vs Pillow)

**Files (if Task 4 → x/image/draw):**
- Modify: `go.mod`, `go.sum` (add `golang.org/x/image`)
- Create: `internal/imageops/bilinear.go` (~30 lines wrapping draw.BiLinear)
- Create: `internal/imageops/bilinear_test.go`

- [ ] **Step 0: Confirm upstream**

Pillow's BILINEAR is in `Pillow/src/libImaging/Resample.c`. Two
relevant functions: `ImagingResampleHorizontal_8bpc` and
`ImagingResampleVertical_8bpc`, called in sequence. Each builds a
weighted filter table once, then applies it.

Verification (if Pillow source isn't already on disk):
```sh
pip download Pillow --no-deps -d /tmp/pillow-src
unzip -p /tmp/pillow-src/Pillow-*.tar.gz Pillow-*/src/libImaging/Resample.c | head -200
```

The rule: same input → same output bytes (byte-parity bar, if Task 1
selects it) or pixel-equivalent (if Task 1 selects pixel-parity).

- [ ] **Step 1-N: Implement per the chosen branch**

Plan author cannot pre-specify the implementation without knowing
Task 4's choice. The two branches differ enough that detailed steps
diverge. Whoever executes follows Task 4's `port notes` markdown
file.

Common requirements:
- Public function: `Resize(src image.Image, w, h int) *image.RGBA`
- Tests: round-trip with known input dimensions, parity vs Python
  Pillow on at least three sample images.

- [ ] **Step Final: Commit**

```sh
git add go.mod go.sum internal/imageops/  # adjust per branch
git commit -m "feat(imageops): R4 prereq — BILINEAR resize via <Pillow port | x/image/draw>

Provides the resize primitive R4's corrupt-edge reconstruct path
needs. Output matches Python Pillow's Image.resize(BILINEAR) <byte-
for-byte | pixel-equivalent> per Task 1's parity bar.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

# Batch E — SVS corrupt-edge reconstruct

## Task 14: corrupt-edge detection

**Goal:** Port `_detect_corrupt_edges` so the SVS Tiler can report
which level edges have at least one corrupt tile. Stays entirely in
the existing-tile-table domain — no decode/encode work yet.

**Files:**
- Create: `formats/svs/reconstruct.go`
- Modify: `formats/svs/svs.go` (call detection; store flags on level)
- Modify: `formats/svs/tiled.go` (add fields for the flags)
- Create: `formats/svs/reconstruct_test.go`

- [ ] **Step 0: Confirm upstream**

`opentile/formats/svs/svs_image.py:267-291` (`_detect_corrupt_edges`):

```python
def _detect_corrupt_edge(self, edge: Region) -> bool:
    for tile in edge.iterate_all():
        frame_index = self._tile_point_to_frame_index(tile)
        if self._page.databytecounts[frame_index] == 0:
            return True
    return False

def _detect_corrupt_edges(self) -> Tuple[bool, bool]:
    if self._parent is None:
        return False, False
    right_edge = Region(Point(W-1, 0), Size(1, H-1))
    bottom_edge = Region(Point(0, H-1), Size(W-1, 1))
    return _detect_corrupt_edge(right_edge), _detect_corrupt_edge(bottom_edge)
```

The rule: a level only checks for corrupt edges if it has a parent
(higher-resolution) level. A tile is corrupt iff its `databytecounts`
is 0. Right-edge is the rightmost column (x=W-1, all y); bottom-edge
is the bottommost row (y=H-1, all x). Top-left corner is excluded
from both regions (Python uses `H-1` / `W-1` for the size, matching
`Range(0, H-1)` semantics).

- [ ] **Step 1: Test-driven port**

Steps 1-N follow the standard TDD cycle: write the failing test,
watch it fail, port the function, watch it pass, commit. The test
uses synthetic SVS bytes with deliberate zero-length tile entries on
the right edge; the assertions match upstream's truth values.

(Detailed step bodies elided for brevity — implementer follows the
TDD pattern from v0.3 Task 16/19. Full code is mechanical from the
upstream snippet above.)

---

## Task 15: corrupt-edge reconstruct (`_get_scaled_tile` port)

**Goal:** Port `_get_scaled_tile` and `_get_fixed_tile` so a corrupt
edge tile's bytes can be synthesised from the parent level. Replaces
the v0.3 `ErrCorruptTile` return with a reconstruct call.

**Files:**
- Modify: `formats/svs/reconstruct.go`
- Modify: `formats/svs/tiled.go` (Tile() routes to reconstruct)
- Modify: `formats/svs/reconstruct_test.go`

- [ ] **Step 0: Confirm upstream**

`opentile/formats/svs/svs_image.py:301-396`. The chain:

1. `_get_scaled_tile(tile_point)`: compute `scale =
   2^(self.pyramid_index - parent.pyramid_index)`; the parent
   region is `Region(tile_point, Size(1,1)) * scale` — i.e., a
   `scale x scale` region of parent tiles.
2. `parent.get_decoded_tiles([...])` decodes each of those parent
   tiles to a numpy array.
3. Build an `np.zeros(shape=(W*scale, H*scale, samples_per_pixel))`
   raster; paste each decoded tile into its slot.
4. `Pillow.Image.fromarray(image_data).resize(self.tile_size,
   resample=BILINEAR)` downscales to one tile.
5. Re-encode in the page's compression: JPEG via `jpeg8_encode`
   with the SOF photometric flag, JP2K via `jpeg2k_encode` with
   imagecodecs' default options. Other compressions raise
   `NonSupportedCompressionException`.

`_get_fixed_tile` adds a per-position cache (`_fixed_tiles[Point]`).

- [ ] **Step 1-N: TDD port**

Implement reconstruct in stages:

1. **JPEG path (depends on Task 13 + a libjpeg-turbo Encode addition).**
   Note: our v0.3 `internal/jpegturbo` only has `Crop` /
   `CropWithBackground`; encode is new. Add `Encode(pixels []byte,
   width, height int, opts EncodeOpts) ([]byte, error)` to
   `internal/jpegturbo` mirroring libjpeg-turbo's `tjCompress2`.
   Sub-tasks:
   - Step Aa: confirm upstream `jpeg8_encode` options.
   - Step Ab: write encode test.
   - Step Ac: implement `jpegturbo.Encode`.
2. **JP2K path (depends on Task 12).**
3. **Wire into Tile().** When `_tile_is_corrupt(tile_point)` returns
   true, call reconstruct; otherwise the existing path.
4. **Opt-in flag.** Add `Config.WithSVSReconstructEdges(bool)`,
   default `false` for v0.4 (changes observable behaviour; v0.5 may
   flip the default).

(Step bodies elided; full code is a mechanical port from the
upstream Python.)

---

## Task 16: SVS reconstruct fixture + integration test

**Goal:** Confirm reconstruct produces correct output on a real SVS
slide with corrupt edges.

**Files:**
- Modify: `tests/integration_test.go` (sub-test exercising
  reconstruct on a slide known to have corrupt edges)
- Regen: affected fixtures with reconstruct enabled

- [ ] **Step 0: Confirm upstream**

Identify a fixture with known corrupt edges. Aperio CMU-1.svs and
CMU-1-Small-Region.svs may not have any (their L0 dimensions are
tile-aligned). JP2K-33003-1.svs is a candidate.

Verification (run before Step 1):
```sh
/private/tmp/opentile-py/bin/python -P -c "
import opentile
for slide in ['sample_files/svs/CMU-1.svs', 'sample_files/svs/JP2K-33003-1.svs', 'sample_files/svs/scan_620_.svs', 'sample_files/svs/svs_40x_bigtiff.svs']:
    print('===', slide)
    t = opentile.OpenTile.open(slide, 1024)
    for level in t.levels:
        if hasattr(level, 'right_edge_corrupt') and (level.right_edge_corrupt or level.bottom_edge_corrupt):
            print(f'  L{level.pyramid_index}: right={level.right_edge_corrupt} bottom={level.bottom_edge_corrupt}')
"
```

If none of our local slides have corrupt edges, this task carries a
synthetic fixture (a small TIFF builder produces a level with
deliberately-zeroed edge tiles) and the integration test is unit-test-
only against that synthetic input. v0.5 promotes to a real slide
fixture once we have one.

- [ ] **Step 1-N: Implementation**

(Detailed steps depend on Step 0's outcome. The implementer follows
the verification command's report: real-slide test if any of our
fixtures has corrupt edges, synthetic-only otherwise.)

---

# Batch F — Polish + ship

## Task 17: deferred.md retirement audit

**Goal:** Mirror v0.3's T39 — retire every L-item and reviewer
suggestion that landed in v0.4.

**Files:**
- Modify: `docs/deferred.md`

- [ ] **Step 0: Confirm upstream**

No upstream — port-internal docs work.

- [ ] **Step 1: Walk every retired item**

For each of L6, L12 (per Task 9 outcome), L17, R4, R9, R13:
confirm there's a v0.4 commit closing it; either delete the section
from §2/§1 or move to §5 "Retired in v0.4."

- [ ] **Step 2: Run the structure check**

```sh
grep -n "^### L\|^### Retired\|^## " docs/deferred.md
```

Expected: §2 carries only L4, L5, L14 (Permanent items). §5 grows a
"Retired in v0.4" subsection.

- [ ] **Step 3: Commit**

```sh
git add docs/deferred.md
git commit -m "docs: T17 — v0.4 retirement audit on deferred.md

Closes the v0.4 polish push by retiring every limitation that landed:
  L6, L17, R4, R9, R13.
  L12: <closed | documented as upstream-permanent per Task 9 outcome>.

Permanent items (L4, L5, L14) remain documented as design choices.
Section 2 now carries only Permanent entries.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

## Task 18: README + CLAUDE.md milestone bump

**Files:**
- Modify: `README.md`
- Modify: `CLAUDE.md`

- [ ] **Step 0: Confirm upstream**

No upstream — port-internal docs work.

- [ ] **Step 1: Update README.md**

Change the "Current milestone" / supported-formats sections to
reflect v0.4's additions:
- SVS corrupt-edge reconstruct (R4)
- JP2K decode/encode via internal/openjp2 (R9)
- NDPI Map page support (R13)

- [ ] **Step 2: Update CLAUDE.md**

Update the "Current milestone" header from v0.3 to v0.4. Add a
"v0.4 invariants" subsection if any new architectural rules emerged
(e.g., the universal "confirm upstream first" task contract).

- [ ] **Step 3: Commit**

```sh
git add README.md CLAUDE.md
git commit -m "docs: v0.4 milestone bump in README + CLAUDE.md

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

## Task 19: Final coverage / vet / race / parity sweep

**Goal:** Run every gate the Makefile provides; confirm green before
tagging.

**Files:**
- (No file changes; gating-only.)

- [ ] **Step 0: Confirm upstream**

No upstream — port-internal verification.

- [ ] **Step 1: Run each Makefile target**

```sh
make test    # go test ./... -race -count=1
make cover   # ≥80% per package
make vet     # go vet ./...
make parity  # parity oracle on every fixture
```

Each must succeed. If `make cover` fails, write tests until it
passes (extends Task 22's pattern from v0.3).

- [ ] **Step 2: Run TestSlideParity**

```sh
OPENTILE_TESTDIR="$PWD/sample_files" go test ./tests -run TestSlideParity -v -timeout 10m
```

- [ ] **Step 3: Tag the milestone**

```sh
git tag -a v0.4.0 -m "v0.4: existing-format completeness"
```

(Don't push the tag automatically — leave that to the user per the
project's git-safety conventions.)

- [ ] **Step 4: Final report**

Compose a brief report summarising what landed in v0.4 (suitable for
a release note); paste into the chat or a `docs/releases/v0.4.md` if
the user wants a permanent record.

---

## Self-review checklist

Before handing off this plan to an executor:

- [ ] Every task has a `Step 0: Confirm upstream` action.
- [ ] No task has placeholder text ("TBD", "implement later", "fill
  in details") — except for tasks whose body explicitly depends on a
  gate-task outcome (Tasks 9, 13, 15, 16). Those tasks document the
  branching contract and point at the gate's outcome record.
- [ ] Type / function names match across tasks (e.g., `KindMap`,
  `EncodeOpts`, `Resize`).
- [ ] Spec coverage: every requirement in
  `docs/superpowers/specs/2026-04-26-opentile-go-v04-design.md` §3
  has a task.
- [ ] Sample-fixture availability: every test that needs a real slide
  names one we have locally (verified in Task 2 and Task 16).
- [ ] Branch order: gate tasks (Batch A) precede the work they
  inform.
- [ ] Commits: every task ends with a concrete `git commit` step
  including a message template.
