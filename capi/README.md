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

This dev build links codecs and lcms2 dynamically. Self-contained release
builds (static codecs; on Windows a DLL plus `.lib` that needs no MinGW runtime)
are #120 step 3.

## Changing the API

1. Edit `opentile.h` and the matching `//export` function. Append to structs;
   never reorder or remove fields, and never renumber constants.
2. Bump `OT_API_VERSION_MINOR` (additive) in both `opentile.h` and `version.go`.
3. Run `go test -run TestDefFile -update .` to regenerate `libopentile.def`,
   then `go test .`.
