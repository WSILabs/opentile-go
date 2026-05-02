#!/usr/bin/env python3
"""regen-generic-tiff.py — generate v0.10 generic-TIFF test fixtures.

Lives in scripts/ (committed to the repo) but writes its output
under sample_files/generic-tiff/ (gitignored alongside the rest of
sample_files/). Run from any directory; the script resolves paths
relative to its own location.

Two kinds of fixtures:

1. Stripped-SVS reference (T2):
   Reads sample_files/svs/CMU-1{,-Small-Region}.svs and rewrites
   IFD 0's ImageDescription so the SVS detector no longer claims it
   (its detection rule is `HasPrefix(desc, "Aperio")`). Replaces
   the 20-byte "Aperio Image Library" prefix with "Custom Image
   Library" (same byte count → all downstream tag offsets unchanged
   → every IFD preserved verbatim). Result: structurally identical
   to a real SVS (pyramid + label + macro + thumbnail) but the SVS
   factory passes on it → falls through to the (future) generic
   factory.

2. Synthetic-pyramid fixtures (T3, added later):
   Hand-rolled minimal TIFFs to exercise classifier paths +
   validator reject paths (synth-pyramid-jpeg, synth-bad-pyramid,
   synth-stripped-only, etc.).

Run: /private/tmp/opentile-py/bin/python scripts/regen-generic-tiff.py
(needs Python tifffile; the parity-oracle venv at
/private/tmp/opentile-py/ already has it)
"""

import sys
from pathlib import Path

import numpy as np
import tifffile

# scripts/ is one level under the repo root; sample_files/ is a sibling.
REPO_ROOT = Path(__file__).resolve().parent.parent
SAMPLE_FILES = REPO_ROOT / "sample_files"
ROOT = SAMPLE_FILES / "generic-tiff"

APERIO_NEEDLE = b"Aperio Image Library"
APERIO_REPLACEMENT = b"Custom Image Library"  # 20 chars; preserves byte count


def strip_svs(src: Path, dst: Path, label: str) -> None:
    """Take an SVS file and rewrite IFD 0's ImageDescription so the
    SVS detector no longer claims it (its detection rule is
    HasPrefix(desc, "Aperio")). Replace the 6-byte "Aperio Image
    Library" prefix with the 6-byte "Custom Image Library" — same
    byte count so all downstream tag offsets are unchanged. Every
    IFD (pyramid + associated) is preserved verbatim.

    Only IFD 0's description is mutated. IFDs 1+ may still carry
    "Aperio" descriptions — the SVS detector only checks IFD 0 so
    this doesn't affect dispatch.
    """
    if not src.exists():
        print(f"SKIP {label}: {src} not present", file=sys.stderr)
        return

    data = bytearray(src.read_bytes())
    idx = data.find(APERIO_NEEDLE)
    if idx < 0:
        sys.exit(f"BUG: {APERIO_NEEDLE!r} not found in {src}")
    if len(APERIO_NEEDLE) != len(APERIO_REPLACEMENT):
        sys.exit("BUG: needle/replacement byte count mismatch (would shift offsets)")
    data[idx : idx + len(APERIO_REPLACEMENT)] = APERIO_REPLACEMENT
    dst.write_bytes(data)

    print(f"wrote {dst} ({len(data):,} bytes)")

    with tifffile.TiffFile(dst) as tf:
        n = len(tf.pages)
        print(f"  IFDs: {n}")
        for i, page in enumerate(tf.pages):
            desc_tag = page.tags.get("ImageDescription")
            desc = desc_tag.value if desc_tag else ""
            tw = page.tags.get("TileWidth")
            spp = page.samplesperpixel
            comp = str(page.compression)
            shape = "tiled" if tw else "stripped"
            print(
                f"  IFD {i}: {page.imagewidth}×{page.imagelength} "
                f"comp={comp} spp={spp} {shape}"
                + (f"  desc[:60]={desc[:60]!r}" if desc else "")
            )


def synth_pyramid_jpeg() -> None:
    """T3 smoke fixture: minimal 3-level tiled JPEG/RGB pyramid,
    no associated images. Validator MUST accept (3 tiled levels at
    consistent 2× downsample).
    """
    dst = ROOT / "synth-pyramid-jpeg.tiff"
    rng = np.random.default_rng(0)
    with tifffile.TiffWriter(dst) as tw:
        for size in (1024, 512, 256):
            img = rng.integers(0, 256, size=(size, size, 3), dtype=np.uint8)
            tw.write(img, tile=(256, 256), compression="jpeg", photometric="rgb")
    _summarize(dst, "synth-pyramid-jpeg (validator-accept smoke)")


def synth_pyramid_with_label() -> None:
    """T3 multi-strip-LZW exerciser: 3-level tiled JPEG pyramid +
    a small multi-strip LZW label IFD. Validator accepts the
    pyramid; classifier MUST identify the label IFD as
    Kind()='label' per the LZW heuristic.
    """
    dst = ROOT / "synth-pyramid-with-label.tiff"
    rng = np.random.default_rng(1)
    with tifffile.TiffWriter(dst) as tw:
        # Pyramid (3 tiled JPEG levels, 2× downsample)
        for size in (512, 256, 128):
            img = rng.integers(0, 256, size=(size, size, 3), dtype=np.uint8)
            tw.write(img, tile=(128, 128), compression="jpeg", photometric="rgb")
        # Multi-strip LZW label: 200×400 RGB at 8-row strips.
        # 400/8 = 50 strips. Mimics SVS's multi-strip LZW label shape.
        label = rng.integers(0, 256, size=(400, 200, 3), dtype=np.uint8)
        tw.write(label, compression="lzw", photometric="rgb", rowsperstrip=8)
    _summarize(dst, "synth-pyramid-with-label (multi-strip LZW associated)")


def synth_bad_pyramid() -> None:
    """T3 validator-reject path: 3 tiled IFDs whose dimensions
    DON'T form a coherent pyramid. Validator MUST reject (the
    middle IFD's scale ratio breaks the inter-level check).

    Layout: 1024×1024, 600×500 (random shape), 256×256.
      - L0→L1 ratio: 1024/600=1.71, 1024/500=2.05 → inter-axis 17%
      - That alone fails the ±2% inter-axis check → reject.
    """
    dst = ROOT / "synth-bad-pyramid.tiff"
    rng = np.random.default_rng(2)
    sizes = [(1024, 1024), (500, 600), (256, 256)]  # (h, w)
    with tifffile.TiffWriter(dst) as tw:
        for h, w in sizes:
            img = rng.integers(0, 256, size=(h, w, 3), dtype=np.uint8)
            tw.write(img, tile=(128, 128), compression="jpeg", photometric="rgb")
    _summarize(dst, "synth-bad-pyramid (validator-reject: inter-axis tolerance)")


def synth_stripped_only() -> None:
    """T3 validator-reject path: 2 stripped IFDs, no tiled. Validator
    MUST reject (zero tiled candidates < 3 minimum).
    """
    dst = ROOT / "synth-stripped-only.tiff"
    rng = np.random.default_rng(3)
    with tifffile.TiffWriter(dst) as tw:
        for h, w in ((600, 800), (300, 400)):
            img = rng.integers(0, 256, size=(h, w, 3), dtype=np.uint8)
            tw.write(img, compression="jpeg", photometric="rgb")
    _summarize(dst, "synth-stripped-only (validator-reject: 0 tiled IFDs)")


def _summarize(path: Path, label: str) -> None:
    """Print a compact per-IFD summary so the regen output is readable."""
    print(f"wrote {path} ({path.stat().st_size:,} bytes)  — {label}")
    with tifffile.TiffFile(path) as tf:
        for i, p in enumerate(tf.pages):
            tw = p.tags.get("TileWidth")
            shape = "tiled" if tw else "stripped"
            print(
                f"  IFD {i}: {p.imagewidth}×{p.imagelength} "
                f"comp={p.compression!s:>14}  {shape}"
            )


def main() -> int:
    ROOT.mkdir(parents=True, exist_ok=True)

    # T2 — stripped-SVS reference fixtures:
    strip_svs(
        SAMPLE_FILES / "svs" / "CMU-1.svs",
        ROOT / "CMU-1.stripped.tiff",
        "stripped CMU-1.svs",
    )
    strip_svs(
        SAMPLE_FILES / "svs" / "CMU-1-Small-Region.svs",
        ROOT / "CMU-1-Small-Region.stripped.tiff",
        "stripped CMU-1-Small-Region.svs",
    )

    # T3 — synthetic fixtures:
    print()
    synth_pyramid_jpeg()
    print()
    synth_pyramid_with_label()
    print()
    synth_bad_pyramid()
    print()
    synth_stripped_only()
    return 0


if __name__ == "__main__":
    sys.exit(main())
