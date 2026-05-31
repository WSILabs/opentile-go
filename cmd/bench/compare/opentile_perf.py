#!/usr/bin/env python3
"""Perf runner for python opentile (0.20+).

Opens a slide, times N get_tile() calls over a bounded interior tile
grid on level 0, and prints one JSON line:
  {"tiles": int, "pixels": int, "seconds": float, "mpix_per_s": float}
Exits non-zero with a JSON {"error": ...} line if the slide can't be
opened by python opentile (the Go caller treats that as "skip").
"""
import json
import sys
import time

try:
    from opentile import OpenTile
except Exception as e:  # noqa: BLE001
    print(json.dumps({"error": f"import opentile: {e}"}))
    sys.exit(3)


def main():
    if len(sys.argv) < 2:
        print(json.dumps({"error": "usage: opentile_perf.py <slide>"}))
        sys.exit(2)
    path = sys.argv[1]
    try:
        tiler = OpenTile.open(path)
    except Exception as e:  # noqa: BLE001
        print(json.dumps({"error": f"open: {e}"}))
        sys.exit(1)

    level = tiler.get_level(0)
    cols, rows = level.tiled_size.width, level.tiled_size.height
    tw, th = level.tile_size.width, level.tile_size.height

    coords = []
    for ty in range(1, min(rows, 17)):
        for tx in range(1, min(cols, 17)):
            coords.append((tx, ty))
    if not coords:
        coords = [(0, 0)]

    # Warm one tile (first-access decode/IO), then time a full pass.
    level.get_tile(coords[0])
    iters = max(len(coords), 50)
    t0 = time.perf_counter()
    for i in range(iters):
        level.get_tile(coords[i % len(coords)])
    el = time.perf_counter() - t0

    pixels = iters * tw * th
    print(json.dumps({
        "tiles": iters,
        "pixels": pixels,
        "seconds": el,
        "mpix_per_s": (pixels / el / 1e6) if el > 0 else 0.0,
    }))


if __name__ == "__main__":
    main()
