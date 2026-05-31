# cmd/bench/compare

Cross-language competitive benchmark report: opentile-go vs openslide
(ReadRegion) and vs python opentile (Tile), across the format overlap.

## Requirements
- libopenslide (`pkg-config --exists openslide`)
- a python interpreter with python opentile (0.20+), via
  `OPENTILE_ORACLE_PYTHON` (or `OPENTILE_OPENSLIDE_PYTHON`)
- fixtures under `OPENTILE_TESTDIR`

## Run (from the repository root)

```sh
go build -tags openslidebench -o /tmp/bench-compare ./cmd/bench/compare/
OPENTILE_TESTDIR="$PWD/sample_files" \
  OPENTILE_ORACLE_PYTHON=/path/to/venv/bin/python \
  /tmp/bench-compare
```

Or `make bench-compare`. The binary shells out to `opentile_perf.py`
by relative path, so run it from the repo root.

## A/B a change (benchstat)

The Go-benchmark layer (`bench/`) is benchstat-friendly:

```sh
go test ./bench/ -bench BenchmarkRead -benchmem -count 6 > /tmp/before.txt
# ...make a change...
go test ./bench/ -bench BenchmarkRead -benchmem -count 6 > /tmp/after.txt
benchstat /tmp/before.txt /tmp/after.txt
```

`—` in the table means that engine cannot read that format (expected:
openslide can't read OME/IFE/SZI/COG-WSI; python opentile reads a
narrower set). Numbers are Mpix/s on a bounded interior tile grid; the
ReadRegion and Tile columns measure different work (decoded pixels vs
compressed-tile read), so compare within a column, not across.

**Caveats:**
- The **Tile** column reports compressed-tile fetch rate. For most
  formats this is dominated by per-call overhead (the tile is returned
  as bytes, not decoded), so the absolute Mpix/s is very large and the
  ratio reflects fetch overhead, not decode. NDPI is the exception (its
  "tile" assembles a JPEG frame, so it measures real work).
- **Multi-region formats (Leica SCN) are not apples-to-apples on
  ReadRegion.** opentile-go addresses tiles within tissue regions while
  openslide reads absolute level-0 coordinates, so a fixed grid lands
  them in different areas — openslide may read sparse/background regions
  trivially, producing a misleadingly high number. Treat the SCN
  ReadRegion ratio as unreliable.
