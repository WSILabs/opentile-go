# Building opentile-go on Windows

opentile-go builds on Windows (x86-64) with **cgo + all six codec decoders**
(jpeg, jpeg2000, htj2k, jpegxl, avif, webp). cgo needs a MinGW-w64/gcc
toolchain (not MSVC), and the codec C libraries are provided by **vcpkg** using
the built-in `x64-mingw-static` triplet — openjph (HTJ2K) is not packaged for
MSYS2, and vcpkg is the only path that builds it for MinGW.

This mirrors the `integration-windows` job in `.github/workflows/ci.yml`.

## Prerequisites

1. **Go** — install from https://go.dev/dl/ (the standard Windows amd64 build).
   The module's `go 1.25.0` directive auto-fetches the matching toolchain.
2. **MSYS2** — install from https://www.msys2.org/. Then, in an *MSYS2 MINGW64*
   shell:
   ```sh
   pacman -S --needed mingw-w64-x86_64-gcc mingw-w64-x86_64-cmake \
     mingw-w64-x86_64-ninja mingw-w64-x86_64-pkgconf
   ```
   Put the MinGW64 bin dir on PATH (e.g. `C:\msys64\mingw64\bin`) — it carries
   `gcc` and `pkgconf`. Do **not** use Git-for-Windows' own `mingw64`, which has
   no gcc/pkgconf.
3. **vcpkg** — clone and bootstrap:
   ```sh
   git clone https://github.com/microsoft/vcpkg C:/vcpkg
   # Pin to the baseline in this repo's vcpkg.json for reproducibility:
   git -C C:/vcpkg checkout 40f3c709db80acf154ac4b17a1f83c564ebd022e
   C:/vcpkg/bootstrap-vcpkg.bat -disableMetrics
   ```

## Build the codec libraries

From the repo root (which contains `vcpkg.json`):

```sh
C:/vcpkg/vcpkg install --triplet x64-mingw-static --x-install-root=./vcpkg_installed
```

The first run compiles openjph, libjxl, and libavif/aom from source (~20–40
min). Subsequent runs are cached.

## Build & test opentile-go

Point `pkg-config` at the vcpkg libs and enable cgo:

```sh
export PKG_CONFIG_PATH="$PWD/vcpkg_installed/x64-mingw-static/lib/pkgconfig"
export CGO_ENABLED=1
go build ./...
go vet ./...
go test ./... -count=1 -timeout 30m
```

## Notes

- **No `-race` on Windows.** The race detector with cgo on Windows Go is
  brittle; race coverage runs on Linux/macOS instead.
- **Fixtures.** Fixture-backed tests need `OPENTILE_TESTDIR` pointing at a slide
  corpus; without it those tests skip. CI downloads the public
  `WSILabs/wsi-fixtures` corpus.
- **Static libs.** `x64-mingw-static` statically links the codecs into the test
  binaries — no loose DLLs to place on PATH.
