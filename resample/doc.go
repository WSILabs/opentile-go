// Package resample provides pure-Go pixel resamplers operating on
// decoder.Image. Used by the v1.0 Slide.ScaledStrips iterator (when it
// lands) and exposed as standalone primitives for ad-hoc callers.
//
// Kernels: Nearest (cheap, ugly for downsampling), Bilinear, Lanczos
// (best for arbitrary ratios), Box (area-averaging, best for integer
// downsampling).
//
// Future Go-assembly acceleration is possible but out of scope for
// v0.22; the public API stays stable.
package resample
