//go:build !openslidebench

// Stub for the default (untagged) build so this package always has a
// buildable Go file — otherwise `go test ./...` / `make test` fail with
// "build constraints exclude all Go files". The real report lives in
// main.go behind //go:build openslidebench (needs libopenslide).
package main

import (
	"fmt"
	"os"
)

func main() {
	fmt.Fprintln(os.Stderr, "cmd/bench/compare requires libopenslide: build with -tags openslidebench")
	fmt.Fprintln(os.Stderr, "  go build -tags openslidebench ./cmd/bench/compare/")
	os.Exit(2)
}
