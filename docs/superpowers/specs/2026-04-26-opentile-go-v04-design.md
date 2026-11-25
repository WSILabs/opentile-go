# opentile-go v0.4 Design Spec — existing-format completeness

**Status:** Draft, 2026-04-26.
**Predecessors:** v0.1 (`feat/v0.1`, merged), v0.2 (`feat/v0.2`, merged at `d121e28`), v0.3 (`feat/v0.3`, in flight).

## 1. One-paragraph scope

v0.4 closes every real bug and parity gap on the formats we already
support — Aperio SVS and Hamamatsu NDPI. No new format support lands
in this milestone (Philips, 3DHistech, OME stay v0.5+). The v0.3
deferred-audit identified six open items in `docs/deferred.md §2` (L4,
L5, L6, L12, L14, L17) plus four roadmap items targeted at v0.4 (R4,
R9, R13, with R5 demoted to v0.5). v0.4 retires the work-items (L6,
L12, L17, R4, R9, R13); the three Permanent items (L4, L5, L14) stay
documented as design choices.

## 2. Universal task contract: "confirm upstream first"

Every task in the v0.4 plan must reference upstream behavior before
writing a line of production code. The rule that landed in
`feedback_no_guessing.md` and the v0.2 / v0.3 invariants section of
`CLAUDE.md` is now structural — each task body in the plan starts with
a `Step 0: Confirm upstream` action that names:

- The upstream file path + line range that governs the behavior
  (typically under `imi-bigpicture/opentile`, `cgohlke/tifffile`, or
  `imagecodecs`).
- A one-line statement of the rule the Go implementation must match.
- A verification command (Python invocation, fixture check, or
  documentation reference) that proves the rule is what the executor
  thinks it is.

Tasks that have no direct upstream (a new cgo binding, an internal
refactor, a doc update) still carry a Step 0 — explicitly stating "no
direct upstream; this is a port-internal concern" and naming the v0.3
or earlier port commit that establishes the local convention being
extended. The point is to make it impossible for a task to begin with
"I assume upstream does X."

Two failure modes this prevents:
1. Re-introducing a bug we already paid to fix (multiple v0.2 cycles
   were spent re-discovering tag layouts that tifffile documents).
2. Inventing behavior upstream doesn't have, then fighting parity
   tests forever (the v0.3 N-10 task's "extendsBeyond" inversion got
   reverted because the optimization produced different bytes from
   upstream — could have been caught by reading upstream first).

## 3. Themes

### Theme A — NDPI completeness

| ID | What | Mechanism |
|---|---|---|
| L17 | NDPI label cropH MCU rounding | Add ragged-height `CropWithBackground` path threading luminance + chroma DC math through. Fix the OS-2 / Hamamatsu-1 cropH-vs-Python divergence. Regen those two NDPI fixtures. |
| L6 / R13 | NDPI Map pages (`mag == -2.0`) | Add `"map"` `AssociatedImage` `Kind`. Wire NDPI classifier to expose Map pages. OS-2 + Hamamatsu-1 already carry one; CMU-1 doesn't. Update fixtures. |
| L12 | NDPI edge-tile entropy divergence | Either fix locally (if our cgo wrapper is at fault) or document upstream + skip parity (if the divergence is in libjpeg-turbo's `tjTransform` itself). The minimal C-only reproduction in Task 3 decides which. |

### Theme B — SVS completeness

| ID | What | Mechanism |
|---|---|---|
| R9 | JPEG 2000 decode/encode | New `internal/openjp2/` cgo package, scoped like `internal/jpegturbo/`. `Decode` + `Encode` mirroring imagecodecs' options (`level=80, codecformat=J2K, colorspace=SRGB, mct=True, reversible=False, bitspersample=8`). `nocgo` build-tag stubs return `ErrCGORequired`. |
| R4 | Aperio SVS corrupt-edge reconstruct | Port upstream's `_detect_corrupt_edges` (`opentile/formats/svs/svs_image.py:267-291`) and `_get_scaled_tile` / `_get_fixed_tile` (`svs_image.py:301-396`). For corrupt edge tiles, decode neighbors from the higher-resolution parent level, paste into a scratch raster, BILINEAR-resize, re-encode in the page's compression. JPEG path uses libjpeg-turbo encode; JP2K path uses the new openjp2 binding. |

### Theme C — JIT verification gates (run before Theme A or B sink real work)

The two themes have non-trivial unknowns. Rather than committing to
byte-parity bars and discovering halfway through that the upstream
library is non-deterministic, the plan front-loads four verification
tasks. Each has explicit success / failure branches:

1. **JP2K determinism gate** — round-trip a JP2K-encoded SVS tile
   through `decode → encode` twice via imagecodecs with the documented
   options; hash both outputs. If equal, byte-parity is the v0.4 R4
   bar. If divergent, R4 falls back to pixel-equivalent parity (numpy
   `allclose` against Python output) and we file an L-item documenting
   why bytes diverge.
2. **NDPI Map fixture audit** — confirm we have at least one local
   slide with `mag == -2.0` pages (we do: OS-2 + Hamamatsu-1 both
   carry one). If neither were present the task would defer L6/R13 to
   v0.5.
3. **L12 reproduction shape** — minimal C-only repro of the
   `tjTransform` CUSTOMFILTER edge-tile divergence. If reproducible
   without our cgo wrapper, file upstream + skip the parity gate
   permanently. If only through our wrapper, fix locally. Determines
   whether L12 is a 30-minute task or a multi-week one.
4. **R4 mechanism audit** — read `_get_scaled_tile` and confirm the
   chain (decode neighbors → BILINEAR resize → re-encode). Identify
   the Go-side image-processing dependency: stdlib `image` + a
   handwritten BILINEAR-equivalent resize, or a third-party package.
   The gate's outcome shapes Theme B's task list.

### Theme D — Polish + ship

Mirrors v0.3's closing batch. Retirement audit on `docs/deferred.md`,
README + CLAUDE.md milestone bump, final `make cover` / `go vet` /
`-race` sweep, parity oracle on the regenerated fixtures, tag.

## 4. Out-of-scope (deferred to v0.5+)

- New format support (R5 Philips, R6 3DHistech, R7 OME).
- Permanent items (L4 missing MPP, L5 NDPI sniff, L14 NDPI synthesized
  label, R10 remote I/O, R12 CLI). These stay documented in §2 and §1
  respectively.
- Anything beyond the four roadmap items + three L-items called out in
  Themes A/B.

## 5. Branch + workflow

- Branch: `feat/v0.4` from `main` after v0.3 merges. (If v0.3 hasn't
  merged when v0.4 starts, branch from `feat/v0.3` and rebase.)
- Spec: this document.
- Plan: `docs/superpowers/plans/2026-04-26-opentile-go-v04.md` (in
  this same commit).
- Execution: `superpowers:subagent-driven-development` per task; two-
  stage review (spec-compliance + code-quality) at the end of each
  Theme. Universal "confirm upstream" Step 0 is enforced by review.

## 6. Done-when

- All four JIT gates have a recorded outcome (commit + deferred.md
  entry where applicable).
- Every Theme A and Theme B work-item is closed (commit + test +
  deferred.md retirement entry).
- `make cover` clears 80% per package (matches v0.3 gate).
- `make parity` is byte-equal to Python opentile on every existing
  fixture, and on the new R4 reconstructed-tile fixture per the JP2K
  determinism gate's chosen bar.
- `docs/deferred.md` §2 carries only Permanent items; §5 grows a "Retired
  in v0.4" subsection paralleling §5's v0.3 list.
- README + CLAUDE.md reflect v0.4 as the current milestone.
