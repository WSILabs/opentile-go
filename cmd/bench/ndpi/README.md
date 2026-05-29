# cmd/bench/ndpi

Single-thread NDPI tile-decode throughput benchmarks for opentile-go
v0.27+. The Go program is the test subject; the C program is the
reference (openslide).

## Build

```sh
# Go test subject
go build -o /tmp/bench-opentile ./cmd/bench/ndpi/

# C reference (requires openslide installed)
clang $(pkg-config --cflags --libs openslide) -O2 \
    -o /tmp/bench-openslide cmd/bench/ndpi/openslide_ref/bench-openslide.c
```

## Run

```sh
# Reference number
/tmp/bench-openslide sample_files/ndpi/CMU-1.ndpi

# v0.27 opentile-go (with CPU profile)
/tmp/bench-opentile -in sample_files/ndpi/CMU-1.ndpi -cpuprofile /tmp/cpu.prof

# Inspect the profile
go tool pprof -top -lines /tmp/cpu.prof
```

## Expected numbers (Apple Silicon, 13 cores, CMU-1.ndpi)

| Build | Throughput | Wall |
|---|---|---|
| openslide 4.0.0 | ~230 Mpix/s | ~8.4s |
| opentile-go v0.26 | ~44 Mpix/s | ~44s |
| opentile-go v0.27 target (stretch) | ≥155 Mpix/s | ≤7.8s |
| opentile-go v0.27 target (acceptable) | ≥100 Mpix/s | ≤12s |

Numbers below 100 Mpix/s indicate a regression; the Makefile
`bench-ndpi` target enforces ≥130 Mpix/s as the hard gate.

The benchmark iterates every 256×256 region across L0 sequentially,
single-threaded. openslide returns ARGB pixels; opentile-go returns
RGB. Throughput is normalized to Mpix/s (decoded pixel count per
second). Both are CPU-bound (`user+sys ≈ wall`).
