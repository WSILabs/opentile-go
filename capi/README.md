# libopentile: the opentile-go C API

`capi/` builds **libopentile**, a C library over opentile-go for every non-Go
consumer: the Python binding (`python/`) and native desktop apps. The public
header is [`opentile.h`](opentile.h). The design and its conventions are in
[`docs/superpowers/specs/2026-11-23-capi-design.md`](../docs/superpowers/specs/2026-11-23-capi-design.md).

This is a separate Go module, so its cgo and lcms2 dependencies never reach
users of the Go library. It always builds against the working tree (`replace ../`).

## Layout

| File | Role |
|---|---|
| `opentile.h` | public header, hand-written (Go's generated one isn't MSVC-compatible) |
| `libopentile.def` | Windows export list for an MSVC import library, generated from the header |
| `capi.go` | `//export` functions, handle table, buffer conventions |
| `json.go`, `enums.go`, `errmap.go`, `version.go` | metadata/TIFF-tag JSON, frozen enum codes, Go error → status code mapping, version and codec probes |
| `errors.c` | per-thread last error |
| `icc.c`, `icc_lcms.go` / `icc_nolcms.go` | lcms2 slide-ICC → sRGB (build tag `nolcms2` disables it) |
| `header_test.go` | header ↔ `//export` sync, frozen constant values, `.def` freshness |
| `test/smoke.c`, `test/threads.c` | C tests against a built library and a real slide |

## Dev build

Needs cgo, the codec libraries the Go library uses, and lcms2 (`pkg-config lcms2`).

```sh
make capi                                  # sync tests + build/libopentile.{so,dylib,dll}
make capi-smoke SLIDE=path/to/slide.svs    # + C smoke and close-race tests
```

This dev build links codecs and lcms2 dynamically.

## Release builds

Each `v*` release of opentile-go has a libopentile archive per platform
attached, plus a `SHA256SUMS` file:

| Archive | Library |
|---|---|
| `libopentile-<v>-linux-x86_64.tar.gz`, `-linux-aarch64` | `lib/libopentile.so` (glibc ≥ 2.28, manylinux_2_28) |
| `libopentile-<v>-macos-arm64.tar.gz`, `-macos-x86_64` | `lib/libopentile.dylib` (install name `@rpath/libopentile.dylib`; macOS ≥ 11.0 arm64, ≥ 10.13 x86_64) |
| `libopentile-<v>-windows-x86_64.zip` | `bin/libopentile.dll`, `lib/libopentile.lib` (MSVC import library), `lib/libopentile.def` |

Each has `include/opentile.h`, `LICENSE`, `NOTICE`, this README and
`licenses/` (notices for every statically linked library). The libraries are
self-contained. The six codecs and lcms2 are linked in statically, and each
library depends only on the OS. AVIF decodes through dav1d; no AV1 encoder
is linked in. Dependencies:
- Linux: libc, libm, libpthread, libdl, librt, libstdc++, libgcc_s.
- macOS: `/usr/lib` and system frameworks.
- Windows: DLLs in `System32` only, with the MinGW C/C++ runtime linked in, so
  an MSVC program links `libopentile.lib` and ships `libopentile.dll` beside it.

The `libopentile` workflow (`.github/workflows/libopentile.yml`) builds them
with `scripts/build-release.sh`. That script runs the Go tests, builds
against the vcpkg static triplets through `pkg-config --static`
(`tools/pkgconfig-static`), and fails unless every `libopentile.def` function
is exported, only OS libraries are linked, and no AV1 encoder is linked in. The workflow then runs
`test/smoke.c` and `test/threads.c` against the packaged library; on Windows
the smoke test is compiled by MSVC and run with nothing but the DLL and
`System32` on `PATH`. The same workflow builds and tests (without attaching
anything) on pull requests that touch `capi/` or its build inputs.

To attach archives to an existing release, run the workflow by hand with
`release_tag` set. To build locally, install the codecs with
`vcpkg install --triplet <triplet> --x-feature=capi
--overlay-triplets=.github/vcpkg-triplets` and run
`capi/scripts/build-release.sh <version> <platform> vcpkg_installed/<triplet> dist`.

## Changing the API

1. Edit `opentile.h` and the matching `//export` function. Append to structs;
   never reorder or remove fields, and never renumber constants.
2. Bump `OT_API_VERSION_MINOR` (additive) in both `opentile.h` and `version.go`.
3. Run `go test -run TestDefFile -update .` to regenerate `libopentile.def`,
   then `go test .`.
