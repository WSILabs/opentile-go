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


def main() -> int:
    # Multi-level pyramid + associated (validator accept path):
    strip_svs(
        SAMPLE_FILES / "svs" / "CMU-1.svs",
        ROOT / "CMU-1.stripped.tiff",
        "stripped CMU-1.svs",
    )
    # Single-level (validator reject path — only 1 tiled IFD < 3 minimum):
    strip_svs(
        SAMPLE_FILES / "svs" / "CMU-1-Small-Region.svs",
        ROOT / "CMU-1-Small-Region.stripped.tiff",
        "stripped CMU-1-Small-Region.svs",
    )
    return 0


if __name__ == "__main__":
    sys.exit(main())
