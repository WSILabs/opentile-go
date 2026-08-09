# opentile-go Windows parity — design

**Status:** approved design → this spec.
**Scope:** Sub-project 1 of 2 in the "release a Python interface" arc. The
Python binding + wheels (sub-project 2) is a **separate** spec that builds on
this one; it is out of scope here.

## 1. Problem & goal

opentile-go has no Windows support today: its CI matrix is `ubuntu-latest` +
`macos-latest`, and all six cgo decoders link native libraries via
`#cgo pkg-config:` — the exact thing that is non-trivial on Windows. A
full-codec Windows **wheel** (sub-project 2) cannot be built on top of a Go
library that isn't proven to build and pass on Windows first.

**Goal:** opentile-go builds and its full suite passes green on a
`windows-latest` CI runner with **all six** cgo codecs — jpeg (libjpeg-turbo),
jpeg2000 (openjpeg), **htj2k (openjph)**, jpegxl (libjxl), webp (libwebp), avif
(libavif) — with the HTJ2K tests **running** (not skipped), proving openjph
works on Windows. Plus a contributor-facing Windows dev-setup doc.

This deliverable is independently valuable (it hardens the core library
regardless of Python) and it fully de-risks sub-project 2, whose hardest step —
linking all six codecs on Windows — is exactly what this proves.

## 2. Approach (why this recipe)

The sibling project **wsitools** already ships a full-codec Windows binary
(`wsitools-windows-amd64.zip`, release notes: *"every target includes all
codecs: jpeg, jpeg2000, htj2k, jpegxl, avif, webp"*). Its recipe is the proven
path we lift:

- **Not** msys2's codec packages — openjph is not packaged for msys2, which is
  why wsitools's *smoke*-CI Windows job builds with `-tags nohtj2k`. That path
  cannot meet "full codecs."
- **vcpkg with the built-in `x64-mingw-static` triplet** — builds all six
  codecs including openjph (a vcpkg port) as static MinGW-ABI libraries that
  cgo can link. This is wsitools's *release* path.
- Toolchain: standard `actions/setup-go` for Go + MSYS2 for the
  gcc/cmake/ninja/pkgconf toolchain only (msys2's own Go is brittle).
- Static linking side-steps loose-DLL/PATH issues **and** directly rehearses
  the static link the Python Windows wheel will want in sub-project 2.

Rejected alternatives: msys2 codec packages (no openjph → can't be full-codec);
MSVC (cgo does not drive MSVC — it needs a gcc/clang-compatible toolchain).

## 3. Components

### 3.1 `vcpkg.json` (repo root, new)

A vcpkg manifest listing the six codec libraries with a pinned
`builtin-baseline`, lifted from wsitools's manifest:

```json
{
  "$schema": "https://raw.githubusercontent.com/microsoft/vcpkg-tool/main/docs/vcpkg.schema.json",
  "name": "opentile-go",
  "version-string": "0.0.0",
  "description": "Codec C libraries for opentile-go Windows cgo builds",
  "dependencies": [
    "libjpeg-turbo",
    "openjpeg",
    "libjxl",
    { "name": "libavif", "default-features": false, "features": ["aom"] },
    "libwebp",
    "openjph"
  ],
  "builtin-baseline": "40f3c709db80acf154ac4b17a1f83c564ebd022e"
}
```

(Baseline `40f3c709…` is a current vcpkg `master` commit, as of 2026-07-24 —
deliberately fresher than wsitools's pin. openjph at this baseline is 0.30.1,
the same codec version wsitools ships, so there is no version divergence; the
newer baseline just carries a more current overall ports tree. Bump
deliberately later if needed.)

Notes:
- No custom triplet files are needed — `x64-mingw-static` is a vcpkg **built-in**
  community triplet. (wsitools's `.github/vcpkg-triplets/` overrides are for its
  Linux/macOS *static-release* binaries, which opentile-go does not produce.)
- vcpkg is **Windows-only** in this repo. Linux and macOS CI keep their existing
  apt (`awalsh128/cache-apt-pkgs-action`) and brew dynamic-linking setup
  **unchanged**. opentile-go is a library, not a binary-shipping CLI, so we do
  **not** add wsitools's full static-release matrix — only the Windows codec
  build needed to run the suite.
- The `builtin-baseline` is pinned for reproducibility to the current vcpkg
  `master` commit `40f3c709…` (§ manifest above), deliberately fresher than
  wsitools's pin while resolving to the same openjph 0.30.1.

### 3.2 Windows CI job (`.github/workflows/ci.yml`, new job)

A new job modeled on wsitools's release build steps and the existing
macOS+HTJ2K integration job:

- `name: integration (public fixtures, Windows + all codecs)`
- `runs-on: windows-latest`
- `if: github.event_name != 'pull_request'` — **push-to-main only**, matching
  the macOS+HTJ2K integration job. The vcpkg codec build is too heavy for every
  PR; the vcpkg binary cache keeps cached runs fast.
- Steps:
  1. `actions/checkout@v4`.
  2. `actions/setup-go@v5` with `go-version: '1.23'` (same as the other jobs;
     go.mod's `go 1.25.0` directive triggers the 1.25 toolchain auto-fetch
     identically on Windows). Do **not** disable the build cache here — but see
     §6 risk note on the openjph/pkg-config parallel to the macOS cache bug.
  3. `msys2/setup-msys2@v2` installing
     `mingw-w64-x86_64-{gcc,cmake,ninja,pkgconf}`. Capture the install location.
  4. Prepend the setup-msys2 `…\mingw64\bin` (its gcc + pkgconf) to
     `GITHUB_PATH` — **not** git-bash's `/mingw64`, which is Git's own mingw and
     has no gcc/pkgconf.
  5. Bootstrap vcpkg pinned to `vcpkg.json`'s `builtin-baseline`
     (`git clone` + `git checkout <the baseline read from vcpkg.json>` +
     `bootstrap-vcpkg`), matching
     wsitools's `build-static` composite action.
  6. `actions/cache@v4` over the vcpkg binary cache dir
     (`VCPKG_DEFAULT_BINARY_CACHE`), keyed
     `vcpkg-x64-mingw-static-${{ hashFiles('vcpkg.json') }}`. First build is
     slow (openjph/libjxl/aom compiled from source, ~20–40 min); cached runs are
     fast.
  7. `vcpkg install --triplet x64-mingw-static --x-install-root=…/vcpkg_installed`.
  8. Export `PKG_CONFIG_PATH=…/vcpkg_installed/x64-mingw-static/lib/pkgconfig`,
     `CGO_ENABLED=1`, with the mingw64 bin on PATH — so the existing
     `#cgo pkg-config:` directives resolve unchanged, no Go source change.
  9. Download the public `WSILabs/wsi-fixtures` corpus into `OPENTILE_TESTDIR`
     — reuse the exact step from the macOS+HTJ2K job (resolve newest `v*` corpus
     release, download `*.tar`, untar, set `OPENTILE_TESTDIR`).
  10. `go build ./...`
  11. `go vet ./...`
  12. `go test ./... -count=1 -timeout 30m` — **all six codecs, no `nohtj2k`
      tag, no `-race`** (cgo+race on Windows Go is brittle; race stays covered on
      Linux/macOS). 30m is a generous start (the macOS+HTJ2K job uses 20m;
      Windows tends to run slower); the plan tunes it down if headroom allows.

Shell: use `shell: bash` (git-bash) for the vcpkg/env steps, matching
wsitools's `build-static`, with the setup-msys2 mingw64 bin prepended so the
correct gcc/pkgconf win on PATH.

### 3.3 Go portability fixes

Running the real suite on Windows is the point — it surfaces genuine Windows-only
bugs. Known candidates to address up front; anything else the suite flags is
fixed as found.

- **`internal/dzi.TilePath` / `formats/dzi` filesystem paths.**
  `internal/dzi.TilePath` builds paths with a hardcoded `/`
  (`fmt.Sprintf("%s/%d/%d_%d.%s", …)`). This is **correct** for SZI, where the
  result is a **ZIP-entry name** (ZIP always uses `/`). But the bare-DZI reader
  (`formats/dzi/level.go`'s `tilePath` → `os.ReadFile`/`os.Open`) consumes the
  same helper as a **filesystem** path. Windows `os.Open` tolerates forward
  slashes, so it may already work; the clean fix, if the integration run shows a
  problem, is to route the DZI filesystem path through `filepath.FromSlash`
  (leaving SZI's ZIP-entry use untouched). The `formats/dzi` factory/tiler
  already use `filepath.Join`/`Dir`/`Base`/`Ext` for the root — only the
  per-tile leaf is at issue.
- **Test harness assumptions.** Audit for hardcoded `/tmp` (use `t.TempDir()`),
  path-separator assumptions in golden strings, and CRLF/line-ending
  sensitivity in any text comparison.
- **mmap file-locking (the highest-signal Windows-specific hazard).** Windows
  keeps a memory-mapped file locked while the mapping is open. Any test that
  mmaps a fixture and then deletes/renames/re-opens that same path (temp
  fixtures, `t.TempDir` cleanup, re-open-after-close) can fail deterministically
  with "file in use." Fix by ensuring `Close()`/unmap completes before cleanup,
  or adjusting the test. (mmap itself is already portable via
  `golang.org/x/exp/mmap` → `CreateFileMapping`/`MapViewOfFile`; no production
  mmap code change is expected — this is a test-lifecycle concern.)
- **No `syscall`-specific production code exists** beyond `x/exp/mmap` (already
  portable); the SIGBUS-on-truncation godoc in `internal/tiff/mmap.go` is
  unix-flavored but doc-only.

Fixes must not change Linux/macOS behavior: use `filepath.FromSlash`/`t.TempDir`
rather than platform-branching where possible, and keep tile bytes / decoded
pixels byte-identical across platforms.

### 3.4 Windows dev-setup doc (`docs/windows-dev.md`, new)

A contributor-facing guide mirroring the CI recipe for a local Windows build:
install MSYS2 + `mingw-w64-x86_64-{gcc,cmake,ninja,pkgconf}`; bootstrap vcpkg;
`vcpkg install --triplet x64-mingw-static`; set `PKG_CONFIG_PATH` and PATH; then
`go build ./...` / `go test ./...`. Include the `-race`-is-unsupported note and
the first-build-is-slow / binary-cache tip. Linked from `README.md` (no
`CONTRIBUTING.md` exists).

### 3.5 Doc/consistency updates

- `CLAUDE.md`: record Windows as a CI-tested platform (the "cgo is for codec
  decode only" invariant already implies the codec set; note the Windows job now
  covers it via vcpkg `x64-mingw-static`).
- `CHANGELOG.md`: an entry under a new version heading — Windows support is
  user-facing for the Go library (a MINOR, additive milestone; no API change).
- `README.md`: link the new dev doc; add Windows to any supported-platforms
  statement.

## 4. Verification / success criteria

- The Windows CI job is **green** on `windows-latest`: `go build ./...` and
  `go vet ./...` clean, and `go test ./...` passes with **all six codecs and no
  `nohtj2k` tag**.
- The HTJ2K decode tests **run and pass** on Windows (not skipped) — the
  concrete proof openjph links and works. Likewise the other five codec test
  suites run.
- The fixture-backed integration tests (via the downloaded `wsi-fixtures`
  corpus) pass on Windows.
- Linux and macOS CI remain unchanged and green (no regression from the shared
  `vcpkg.json` / new job).
- The dev-setup doc lets a contributor reproduce the build locally.

## 5. Fallback (scoped contingency)

The deterministic Windows failures in §3.3 are ordinary fixes, not flakiness.
If — after those are fixed — the **fixture-integration** step still exhibits
**genuinely nondeterministic** failures that need real design work (most likely
concurrency/timing under Windows scheduling, or an mmap handle-lifecycle race),
we do **not** block the milestone: ship `go build` + `go vet` + **unit** suite
parity on Windows (all six codecs) as the delivered gate, and track "integration
suite green on Windows" as a fast-follow. This is insurance so a single stubborn
timing-sensitive test can't hold the milestone hostage; the expectation is that
the deterministic fixes cover most of it and the integration suite lands green.

## 6. Risks & mitigations

- **vcpkg build time** (openjph + libjxl + libavif/aom from source, ~20–40 min
  first run): mitigated by the `actions/cache` vcpkg binary cache keyed on
  `vcpkg.json`. Only the first/baseline-change build pays full cost.
- **aom needs nasm**: vcpkg auto-acquires nasm on Windows (unlike macOS, where
  wsitools installs it manually), so no extra step is expected — confirm during
  implementation.
- **vcpkg/network transients** (cloning vcpkg, fetching source tarballs):
  build-step flakiness, not test flakiness; the binary cache limits exposure.
  Acceptable for a push-only job.
- **Go build-cache vs pkg-config staleness** (the macOS+HTJ2K openjph incident):
  Go's build cache doesn't track pkg-config output, so a persisted cache could
  pin a stale vcpkg path if the baseline changes. Because the vcpkg install root
  is under the workspace and the baseline is pinned in `vcpkg.json`, the path is
  stable across runs; if this bites, apply the same `cache: false` guard used on
  the macOS+HTJ2K `setup-go` step.
- **`windows-latest` image drift** (Go/msys2/vcpkg version bumps): pinned vcpkg
  baseline + pinned action versions bound this; the push-only cadence surfaces
  breakage on merge, consistent with the macOS integration job.

## 7. Out of scope

- The Python binding, FFI shim, `cibuildwheel`, and wheels (sub-project 2 —
  separate spec).
- Static **release binaries** for opentile-go on any platform (it's a library).
- Windows ARM64 (`windows/arm64`) — GitHub-hosted Windows ARM runners are not
  first-class; x86_64 only for v1.
- `-race` on Windows (brittle with cgo; covered on Linux/macOS).
- Changing Linux/macOS from dynamic apt/brew linking to vcpkg.
