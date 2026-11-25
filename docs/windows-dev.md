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
   # Pin to the "builtin-baseline" in this repo's vcpkg.json for reproducibility:
   git -C C:/vcpkg checkout "$(python -c "import json;print(json.load(open('vcpkg.json'))['builtin-baseline'])")"
   C:/vcpkg/bootstrap-vcpkg.bat -disableMetrics
   ```

## Build the codec libraries

From the repo root (which contains `vcpkg.json`):

```sh
C:/vcpkg/vcpkg install --triplet x64-mingw-static \
  --overlay-triplets=./.github/vcpkg-triplets --x-install-root=./vcpkg_installed
```

The overlay is vcpkg's stock `x64-mingw-static` plus
`-Wa,-muse-unaligned-vector-move`, a guard against GCC bug 54412. Some MinGW
GCC builds (e.g. winlibs 15.3, which CI uses) emit aligned AVX stores to the
stack, which only has 16-byte alignment on Win64. libjxl's decoder and openjph's
encoder then crash intermittently with an access violation at
`0xffffffffffffffff`. Keep the overlay even if your GCC doesn't show it.

The first run compiles openjph, libjxl, and libavif/dav1d from source (~20–40
min). Subsequent runs are cached.

## Build & test opentile-go

Point `pkg-config` at the vcpkg libs and enable cgo:

```sh
export PKG_CONFIG_PATH="$PWD/vcpkg_installed/x64-mingw-static/lib/pkgconfig"
export CGO_ENABLED=1
export CGO_CFLAGS="-O2 -g -Wa,-muse-unaligned-vector-move"   # same guard for cgo's own C/C++
export CGO_CXXFLAGS="$CGO_CFLAGS"
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
  binaries. The binaries still load the toolchain's `libstdc++-6.dll`, though.
- **Runtime shadowing (`0xc0000139`).** If a test binary exits with
  `0xc0000139` (entry point not found) before running, it loaded a different
  MinGW `libstdc++-6.dll` than the one it was built against. Check with
  `where.exe libstdc++-6.dll`: your compiler's `bin` must be first. Git Bash
  puts Git for Windows' own `/mingw64/bin` (or `/ucrt64/bin`) first in every
  shell, and Strawberry Perl ships another copy. Prepend your toolchain's
  `bin` to PATH in the shell you test from.
