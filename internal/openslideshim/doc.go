// Package openslideshim is a minimal cgo wrapper over libopenslide,
// used ONLY by the benchmark suite to compare opentile-go against
// openslide. The implementation is gated behind the `openslidebench`
// build tag (see openslide.go) so the shipping library keeps its single
// cgo dependency (internal/jpegturbo) — normal builds, `go test`, CI,
// and the `nocgo` build never compile the cgo code.
//
// This file carries no build tag so the package always has at least one
// buildable Go file; without the `openslidebench` tag the package is
// simply empty (and `go test ./...` / `go build ./...` stay green).
package openslideshim
