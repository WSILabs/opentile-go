# opentile-go Windows parity — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.
>
> **Execution note (important):** This plan is CI/infra-dominant. Tasks 1, 2, 4, 5 are fully verifiable on the local (macOS/Linux) dev machine. Tasks 3 and 6 are verified **only** on a real `windows-latest` GitHub runner — the Windows codec toolchain cannot be reproduced locally on macOS. Task 6 is an **empirical CI bring-up loop**, not classic TDD: push, watch the Windows job, fix what breaks, repeat. Because of that loop, **inline execution (executing-plans) is the better fit than subagent-driven** for this particular plan — the controller drives the push/watch/fix cycle directly. Tasks 1–5 could be subagent tasks; Task 6 must be driven interactively.

**Goal:** opentile-go builds and its full suite passes green on a `windows-latest` CI runner with all six cgo codecs (including openjph/HTJ2K), plus a Windows dev-setup doc.

**Architecture:** Add a Windows-only vcpkg manifest (`vcpkg.json`) that builds the six codec libraries as static MinGW-ABI libs via the built-in `x64-mingw-static` triplet; add a push-only `windows-latest` CI job that provisions setup-go + an MSYS2 gcc/cmake/ninja/pkgconf toolchain + vcpkg, then runs `go build`/`go vet`/`go test` with all codecs; fix Windows-specific portability issues; document the local Windows dev setup. Linux/macOS CI are untouched.

**Tech Stack:** Go 1.23+ (go.mod `go 1.25.0` toolchain auto-fetch), cgo, vcpkg (`x64-mingw-static`), MSYS2/MinGW-w64 (gcc/cmake/ninja/pkgconf), GitHub Actions.

**Spec:** `docs/superpowers/specs/2026-07-23-opentile-go-windows-parity-design.md`

**Branch:** `feat/windows-parity` (already created).

## File structure

- `vcpkg.json` (new, repo root) — vcpkg manifest: the six codec deps + pinned `builtin-baseline`. Consumed only by the Windows CI job.
- `.github/workflows/ci.yml` (modify) — add `workflow_dispatch:` trigger + a new `integration-windows` job.
- `formats/dzi/level.go` (modify) — build the filesystem tile path with `filepath.Join` (OS-native separators) instead of the forward-slash `internal/dzi.TilePath` (which stays for SZI's ZIP-entry names).
- `formats/dzi/level_test.go` (modify) — add a test asserting the tile path uses OS-native separators.
- `docs/windows-dev.md` (new) — contributor Windows build guide.
- `README.md` (modify) — link the dev doc; note Windows support.
- `CLAUDE.md` (modify) — record Windows as a CI-tested platform.
- `CHANGELOG.md` (modify) — new version entry for Windows support.

---

### Task 1: vcpkg codec manifest

**Files:**
- Create: `vcpkg.json`

- [ ] **Step 1: Write the manifest**

Create `vcpkg.json` at the repo root:

```json
{
  "$schema": "https://raw.githubusercontent.com/microsoft/vcpkg-tool/main/docs/vcpkg.schema.json",
  "name": "opentile-go",
  "version-string": "0.0.0",
  "description": "Codec C libraries for opentile-go Windows cgo builds (Windows-only; Linux/macOS use system apt/brew).",
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

- [ ] **Step 2: Verify it is valid JSON and has the expected shape**

Run:
```bash
python3 -c "import json; d=json.load(open('vcpkg.json')); \
  deps=[x if isinstance(x,str) else x['name'] for x in d['dependencies']]; \
  assert set(deps)=={'libjpeg-turbo','openjpeg','libjxl','libavif','libwebp','openjph'}, deps; \
  assert len(d['builtin-baseline'])==40, d['builtin-baseline']; \
  print('vcpkg.json OK:', deps)"
```
Expected: `vcpkg.json OK: [...]` listing the six codecs. No assertion error.

- [ ] **Step 3: Commit**

```bash
git add vcpkg.json
git commit -m "build(windows): vcpkg manifest for the six cgo codecs (x64-mingw-static)"
```

---

### Task 2: DZI filesystem tile-path portability fix (TDD, locally verifiable)

`internal/dzi.TilePath` builds paths with a hardcoded `/` — correct for SZI (ZIP-entry names are always `/`-separated) but consumed by the bare-DZI reader as a **filesystem** path. Route the DZI filesystem path through `filepath.Join` so it is OS-native on Windows. `internal/dzi.TilePath` is left unchanged (SZI keeps using it).

**Files:**
- Modify: `formats/dzi/level.go` (the `tilePath` method + imports)
- Test: `formats/dzi/level_test.go`

- [ ] **Step 1: Write the failing test**

Add to `formats/dzi/level_test.go` (match the existing `package` clause in that file — it is `package dzi`). Add `os`, `path/filepath`, `strings` to its imports if not already present:

```go
func TestTilePathUsesOSNativeSeparators(t *testing.T) {
	l := &level{
		filesDir: filepath.Join("root", "slide_files"),
		dziLevel: 5,
		format:   "jpeg",
	}
	got := l.tilePath(3, 4)
	want := filepath.Join("root", "slide_files", "5", "3_4.jpeg")
	if got != want {
		t.Errorf("tilePath = %q, want %q", got, want)
	}
	// On Windows the separator is '\\'; a filesystem path must not carry a
	// stray forward slash (the SZI/ZIP forward-slash form is wrong for os.Open).
	if os.PathSeparator == '\\' && strings.ContainsRune(got, '/') {
		t.Errorf("tilePath %q contains a forward slash on a backslash-separator OS", got)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./formats/dzi/ -run TestTilePathUsesOSNativeSeparators -v`
Expected: FAIL — the current `tilePath` returns `idzi.TilePath(...)` = `"root/slide_files/5/3_4.jpeg"` (forward slashes). On macOS `filepath.Join("root","slide_files","5","3_4.jpeg")` is also `"root/slide_files/5/3_4.jpeg"`, so the `want` comparison actually PASSES on macOS today. To make the test meaningful and fail-first on macOS, first confirm it currently passes, then note the real assertion is the Windows guard. If it passes on macOS, that is expected — the fix's value is proven on Windows CI (Task 6). Treat Step 2 as "run and observe"; proceed to make `tilePath` construct the path via `filepath.Join` regardless (so the intent is explicit and correct on Windows).

> Rationale for the plan reader: `filepath.Join` is a no-op transform on Unix, so this test cannot fail on macOS. Its job is to (a) lock the intent — the DZI filesystem path is built with `filepath`, not the ZIP-oriented helper — and (b) actively guard on Windows. Do not skip the code change just because the test is green on macOS.

- [ ] **Step 3: Change `tilePath` to build the path with `filepath.Join`**

In `formats/dzi/level.go`, replace the `tilePath` method:

```go
// tilePath resolves (x, y) to the on-disk tile file path, using OS-native
// separators (filepath.Join) — the bare-DZI reader reads these via os.Open,
// unlike SZI which uses internal/dzi.TilePath for '/'-separated ZIP entries.
func (l *level) tilePath(x, y int) string {
	return filepath.Join(l.filesDir, strconv.Itoa(l.dziLevel), fmt.Sprintf("%d_%d.%s", x, y, l.format))
}
```

Update the `import` block in `formats/dzi/level.go`: add `"fmt"`, `"path/filepath"`, `"strconv"`. Then remove the `idzi "github.com/wsilabs/opentile-go/internal/dzi"` import **only if** it is now unused in this file — check with `grep -n idzi formats/dzi/level.go`; if `tilePath` was its only use, drop it (the compiler will flag an unused import otherwise).

- [ ] **Step 4: Run the test + the DZI suite to verify green and no regression**

Run: `go test ./formats/dzi/ -count=1`
Expected: PASS — the new test passes and every existing DZI test (real-tile reads, overlap parity) stays green (byte-identical; `filepath.Join` is identity on Unix).

Also confirm the whole module still builds/vets:
Run: `go build ./... && go vet ./formats/dzi/`
Expected: clean.

- [ ] **Step 5: Commit**

```bash
git add formats/dzi/level.go formats/dzi/level_test.go
git commit -m "fix(dzi): build filesystem tile path with filepath.Join (Windows separators)"
```

---

### Task 3: Windows CI job + workflow_dispatch trigger

Adds the `integration-windows` job and a manual trigger so the push-only job can be run from a branch during bring-up (Task 6). YAML validity is checked locally; real execution is Task 6.

**Files:**
- Modify: `.github/workflows/ci.yml`

- [ ] **Step 1: Add `workflow_dispatch` to the `on:` block**

In `.github/workflows/ci.yml`, the top is:

```yaml
on:
  push:
    branches: [main]
  pull_request:
    branches: [main]
```

Change it to:

```yaml
on:
  push:
    branches: [main]
  pull_request:
    branches: [main]
  workflow_dispatch:
```

(`workflow_dispatch` lets `gh workflow run ci.yml --ref feat/windows-parity` trigger the workflow on the branch. The Windows job's `if: github.event_name != 'pull_request'` allows `workflow_dispatch` events, so the job runs on the branch before merge.)

- [ ] **Step 2: Append the `integration-windows` job**

Add this job at the end of `.github/workflows/ci.yml` (after the last existing job, at the same indentation level as the other `jobs:` entries — two spaces):

```yaml
  # macOS+HTJ2K's sibling on Windows — full-codec coverage via vcpkg
  # x64-mingw-static (openjph is not an msys2 package; vcpkg is the only path
  # that builds it for MinGW). Push-only (skipped on PRs) — the vcpkg codec
  # build is heavy; the binary cache keeps warm runs fast. Static-linked MinGW
  # libs; no -race (cgo+race on Windows Go is brittle — covered on Linux/macOS).
  integration-windows:
    name: integration (public fixtures, Windows + all codecs)
    if: github.event_name != 'pull_request'
    runs-on: windows-latest
    defaults:
      run:
        shell: bash
    steps:
      - uses: actions/checkout@v4

      - name: Set up Go
        uses: actions/setup-go@v5
        with:
          go-version: '1.23'

      - name: Set up MSYS2 mingw toolchain
        id: msys2
        uses: msys2/setup-msys2@v2
        with:
          msystem: MINGW64
          update: true
          install: >-
            mingw-w64-x86_64-gcc
            mingw-w64-x86_64-cmake
            mingw-w64-x86_64-ninja
            mingw-w64-x86_64-pkgconf

      - name: Put MSYS2 mingw64 on PATH
        run: |
          # setup-msys2's mingw64 (gcc + pkgconf) — NOT git-bash's /mingw64,
          # which is Git's own mingw and has no gcc/pkgconf.
          echo "${{ steps.msys2.outputs.msys2-location }}\\mingw64\\bin" >> "$GITHUB_PATH"

      - name: Bootstrap vcpkg (pinned baseline)
        run: |
          set -euo pipefail
          git clone https://github.com/microsoft/vcpkg "$RUNNER_TEMP/vcpkg"
          BASELINE=$(python -c "import json;print(json.load(open('vcpkg.json'))['builtin-baseline'])")
          git -C "$RUNNER_TEMP/vcpkg" checkout "$BASELINE"
          "$RUNNER_TEMP/vcpkg/bootstrap-vcpkg.sh" -disableMetrics
          echo "VCPKG_ROOT=$RUNNER_TEMP/vcpkg" >> "$GITHUB_ENV"
          mkdir -p "$RUNNER_TEMP/vcpkg-bincache"
          echo "VCPKG_DEFAULT_BINARY_CACHE=$RUNNER_TEMP/vcpkg-bincache" >> "$GITHUB_ENV"

      - name: Cache vcpkg-built codec libs
        uses: actions/cache@v4
        with:
          path: ${{ env.VCPKG_DEFAULT_BINARY_CACHE }}
          key: vcpkg-x64-mingw-static-${{ hashFiles('vcpkg.json') }}

      - name: Install codec libs (vcpkg, x64-mingw-static)
        run: |
          set -euo pipefail
          "$VCPKG_ROOT/vcpkg" install \
            --triplet x64-mingw-static \
            --x-install-root="$PWD/vcpkg_installed"
          echo "PKG_CONFIG_PATH=$PWD/vcpkg_installed/x64-mingw-static/lib/pkgconfig" >> "$GITHUB_ENV"

      - name: Download public fixture corpus (WSILabs/wsi-fixtures)
        env:
          GH_TOKEN: ${{ secrets.GITHUB_TOKEN }}
        run: |
          mkdir -p "$RUNNER_TEMP/tars" "$RUNNER_TEMP/wsi-fixtures"
          # Resolve the newest fixture-corpus release (v1, v2, …) explicitly
          # instead of trusting the repo's "Latest" pointer: a non-corpus
          # release (e.g. a tools-* validator-binary mirror) can grab Latest
          # and carry no *.tar, which would silently break this download.
          TAG="$(gh release list --repo WSILabs/wsi-fixtures --json tagName \
            --jq '.[].tagName' | grep -E '^v[0-9]+(\.[0-9]+)*$' | sort -V | tail -1)"
          echo "fixture corpus tag: ${TAG:?no v* corpus release found}"
          gh release download "$TAG" --repo WSILabs/wsi-fixtures \
            --pattern '*.tar' --dir "$RUNNER_TEMP/tars"
          for t in "$RUNNER_TEMP/tars"/*.tar; do
            tar xf "$t" -C "$RUNNER_TEMP/wsi-fixtures"
          done
          echo "OPENTILE_TESTDIR=$RUNNER_TEMP/wsi-fixtures" >> "$GITHUB_ENV"
          echo "corpus formats:"; ls "$RUNNER_TEMP/wsi-fixtures"

      - name: go build (all six codecs)
        env:
          CGO_ENABLED: '1'
        run: go build ./...

      - name: go vet (all six codecs)
        env:
          CGO_ENABLED: '1'
        run: go vet ./...

      - name: go test (fixture-backed, full codecs incl HTJ2K, no -race)
        env:
          CGO_ENABLED: '1'
        run: go test ./... -count=1 -timeout 30m
```

- [ ] **Step 3: Validate the workflow YAML locally**

Run:
```bash
python3 -c "import yaml,sys; d=yaml.safe_load(open('.github/workflows/ci.yml')); \
  assert 'workflow_dispatch' in d['on'], d['on']; \
  assert 'integration-windows' in d['jobs'], list(d['jobs']); \
  j=d['jobs']['integration-windows']; \
  assert j['runs-on']=='windows-latest'; \
  assert j['if']==\"github.event_name != 'pull_request'\"; \
  print('workflow YAML OK; jobs:', list(d['jobs']))"
```
Expected: `workflow YAML OK; jobs: [...]` including `integration-windows`. No assertion error.

> Note: `yaml.safe_load` parses `on:` as the boolean-ish key — if `d['on']` raises a KeyError because PyYAML interpreted the key as `True`, adjust the check to `d.get('on') or d.get(True)`. This is a PyYAML quirk, not a workflow error.

- [ ] **Step 4: Commit**

```bash
git add .github/workflows/ci.yml
git commit -m "ci(windows): add integration-windows job (vcpkg x64-mingw-static, all codecs) + workflow_dispatch"
```

---

### Task 4: Windows dev-setup doc

**Files:**
- Create: `docs/windows-dev.md`
- Modify: `README.md`

- [ ] **Step 1: Write the dev doc**

Create `docs/windows-dev.md`:

```markdown
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
```

- [ ] **Step 2: Link the doc from README**

In `README.md`, find the intro paragraph block near the top (after the badges and the one-line description). Add a short line pointing to the doc. Locate this line (it exists near the top of README):

```markdown
**opentile-go reads whole-slide pathology images in Go** — extracting raw compressed tiles *and* decoding pixel regions from **12 WSI formats**, with pure-Go raw-tile reads and a single cgo dependency for codec decode.
```

Immediately after that paragraph, add:

```markdown
Runs on Linux, macOS, and **Windows** (x86-64, all codecs) — see [Building on Windows](./docs/windows-dev.md) for the Windows toolchain setup.
```

- [ ] **Step 3: Verify links resolve**

Run:
```bash
test -f docs/windows-dev.md && grep -q "docs/windows-dev.md" README.md && echo "doc + link OK"
```
Expected: `doc + link OK`.

- [ ] **Step 4: Commit**

```bash
git add docs/windows-dev.md README.md
git commit -m "docs(windows): add Windows dev-setup guide + README link"
```

---

### Task 5: CLAUDE.md + CHANGELOG

**Files:**
- Modify: `CLAUDE.md`
- Modify: `CHANGELOG.md`

- [ ] **Step 1: Note Windows in CLAUDE.md invariants**

In `CLAUDE.md`, find the invariant bullet that begins with **"cgo is for codec decode only."** (search for `cgo is for codec decode only`). Append this sentence to the end of that bullet's text (keep it in the same bullet):

```markdown
The same six codecs build on Windows (x86-64) via vcpkg's `x64-mingw-static` triplet with a MinGW-w64 toolchain — the `integration-windows` CI job runs the full suite there (openjph is not an MSYS2 package, so vcpkg is the only MinGW HTJ2K path). See `docs/windows-dev.md`.
```

- [ ] **Step 2: Add a CHANGELOG entry**

In `CHANGELOG.md`, find the most recent version heading (`## [0.62.0] …`) and insert a new section immediately above it. Use the ship date (run `date +%F` and substitute):

```markdown
## [0.63.0] — 2026-07-24

### Added
- **Windows support (x86-64).** opentile-go now builds and its full test suite
  passes on Windows with all six cgo codecs — jpeg, jpeg2000, htj2k (openjph),
  jpegxl, avif, webp. A new push-only `integration-windows` CI job provisions
  the codec libraries via vcpkg's `x64-mingw-static` triplet + a MinGW-w64
  toolchain and runs the fixture-backed suite (HTJ2K included). New
  `docs/windows-dev.md` documents the local Windows build. No API or behavior
  change; Linux/macOS builds are unaffected.

### Fixed
- `formats/dzi`: the bare-DZI reader now builds filesystem tile paths with
  `filepath.Join` (OS-native separators) instead of the forward-slash
  ZIP-entry helper, so DZI directory pyramids read correctly on Windows.
  Byte-identical on Linux/macOS.
```

- [ ] **Step 3: Verify both edits are present**

Run:
```bash
grep -q "integration-windows CI job\|x64-mingw-static" CLAUDE.md && \
grep -q "## \[0.63.0\]" CHANGELOG.md && echo "docs updated OK"
```
Expected: `docs updated OK`.

- [ ] **Step 4: Commit**

```bash
git add CLAUDE.md CHANGELOG.md
git commit -m "docs: record Windows support (CLAUDE.md invariant + CHANGELOG 0.63.0)"
```

---

### Task 6: Empirical Windows CI bring-up loop (verified only on the runner)

This is the core deliverable and it is **not classic TDD** — the Windows codec toolchain cannot be reproduced on the macOS dev machine, so correctness is established by running the job on a real `windows-latest` runner and fixing what breaks. Drive this loop interactively (controller/inline).

- [ ] **Step 1: Push the branch and trigger the Windows job**

```bash
git push -u origin feat/windows-parity
# workflow_dispatch (added in Task 3) runs the push-only job on the branch:
gh workflow run ci.yml --ref feat/windows-parity
```

- [ ] **Step 2: Watch the run**

```bash
# grab the run id for this ref, then watch it
sleep 8
RUN_ID=$(gh run list --workflow ci.yml --branch feat/windows-parity --limit 1 --json databaseId --jq '.[0].databaseId')
gh run watch "$RUN_ID" --exit-status --interval 30
```
Expected (eventual): the `integration (public fixtures, Windows + all codecs)` job is green. The other jobs also run under `workflow_dispatch`; only the Windows job is the new subject.

- [ ] **Step 3: On failure, triage into one of two buckets and act**

Fetch the failing step's log:
```bash
gh run view "$RUN_ID" --log-failed | grep -viE "^.*gtar:|Cannot open: File exists" | \
  grep -iE "FAIL|error:|--- FAIL|panic:|\.go:[0-9]+:|ld:|library not found|Undefined|cannot find|Process completed with exit" | head -60
```

**Bucket A — deterministic Windows failures (fix them; this is expected work):**
- **Build/link errors** (`ld: library 'X' not found`, `pkg-config` can't find a `.pc`, missing include): the vcpkg install or PATH/`PKG_CONFIG_PATH` wiring is off. Confirm `vcpkg_installed/x64-mingw-static/lib/pkgconfig` has the six `.pc` files (add a debug `ls` step if needed) and that the mingw64 bin (with `pkgconf`) is ahead of git-bash on PATH.
- **`aom`/nasm build failure** during vcpkg install: add a `nasm` install (vcpkg usually auto-acquires it on Windows; if not, `choco install nasm` before the vcpkg step).
- **Path-separator failures** in a test: apply `filepath.Join`/`filepath.FromSlash` at the offending site (same pattern as Task 2). Keep Linux/macOS byte-identical.
- **`t.TempDir` / hardcoded `/tmp`** failures: switch to `t.TempDir()`.
- **mmap "file is being used by another process"**: a test mmaps a fixture then deletes/renames/re-opens the same path. Ensure the `*Slide`/mmap `Close()` runs (unmapping) **before** the file is removed — reorder `defer`s or add an explicit `Close()` before the delete. This is the highest-signal Windows-only hazard; it is deterministic and fixable.
- **CRLF** in a text golden comparison: normalize line endings in the comparison, or write the golden with `\n` and compare after `strings.ReplaceAll(got, "\r\n", "\n")`.

For each Bucket-A fix: make the change, `git commit`, `git push`, re-trigger (`gh workflow run ci.yml --ref feat/windows-parity`), and re-watch (back to Step 2). Repeat until green.

**Bucket B — genuine nondeterminism (pass/fail varies across identical re-runs):**
- Most likely a concurrency/timing sensitivity under Windows scheduling, an mmap handle-lifecycle race, or a `-timeout` trip on a slow runner.
- First, distinguish from Bucket A by re-running the *same commit* (`gh run rerun "$RUN_ID"`); if it flips pass/fail with no code change, it is genuinely nondeterministic.
- If it is a timeout only, raise `-timeout` (e.g. 30m → 45m) and re-run; that is a Bucket-A fix, not true flakiness.
- If it is a real race that needs design work beyond this milestone's scope, invoke the **fallback** (Step 5).

- [ ] **Step 4: Confirm the codec proof once green**

When the Windows job is green, confirm HTJ2K actually **ran** (not skipped) — the concrete proof openjph works. In the passing run's test log:
```bash
gh run view "$RUN_ID" --log | grep -iE "htj2k|ok .*decoder/htj2k|ok .*formats/(bif|dicom)" | head
```
Expected: the `decoder/htj2k` package tests report `ok` (not `[no test files]`/`SKIP`), confirming the codec linked and decoded. If htj2k tests skipped, the codec did not link — return to Bucket A.

- [ ] **Step 5: Fallback ONLY if a genuine Bucket-B race blocks the integration suite**

If, after all Bucket-A fixes, a genuinely nondeterministic failure remains in the *fixture-integration* portion and needs out-of-scope design work: narrow the final `go test` step to the non-integration (unit) suite so the milestone still delivers full-codec Windows build+vet+unit parity, and open a follow-up issue for "integration suite green on Windows."

Change the last test step in `.github/workflows/ci.yml` to skip the fixture-gated packages (drop `OPENTILE_TESTDIR` so those tests self-skip, and keep the codec build/vet/unit coverage):

```yaml
      - name: go test (unit, full codecs incl HTJ2K, no -race)
        env:
          CGO_ENABLED: '1'
        run: |
          unset OPENTILE_TESTDIR
          go test ./... -count=1 -timeout 30m
```

Then `git commit`/`push`/re-run to confirm green. Record the deferral in the CHANGELOG entry from Task 5 (append "Windows fixture-integration tests tracked as a follow-up; build + unit parity ships now."). **Only take this branch if Step 3's Bucket-B path genuinely requires it — the default expectation is the full suite lands green.**

- [ ] **Step 6: Revert the temporary trigger convenience (keep `workflow_dispatch`)**

`workflow_dispatch` is intentionally kept (useful for future manual runs). No branch filter was added to `push`, so nothing to revert there. Confirm the Windows job is still `if: github.event_name != 'pull_request'` (push-only + dispatchable) — it should be unchanged from Task 3.

- [ ] **Step 7: Final green confirmation**

Re-trigger one clean run on the branch and confirm all jobs green:
```bash
gh workflow run ci.yml --ref feat/windows-parity
sleep 8
RUN_ID=$(gh run list --workflow ci.yml --branch feat/windows-parity --limit 1 --json databaseId --jq '.[0].databaseId')
gh run watch "$RUN_ID" --exit-status --interval 30
gh run view "$RUN_ID" --json jobs --jq '.jobs[] | "\(.conclusion)\t\(.name)"'
```
Expected: every job `success`, including `integration (public fixtures, Windows + all codecs)`.

---

## Self-review notes (for the executor)

- **Spec coverage:** §3.1 → Task 1; §3.2 → Task 3; §3.3 (dzi) → Task 2, (mmap/temp/CRLF) → Task 6 Bucket A; §3.4 → Task 4; §3.5 → Task 5; §4 verification → Task 6 Steps 4/7; §5 fallback → Task 6 Step 5; §6 risks (nasm, vcpkg time, cache) → Task 6 Step 3 / Task 3 cache step.
- **Local vs runner:** Tasks 1, 2, 4, 5 are fully green-able locally. Task 3 is YAML-validated locally, executed in Task 6. Task 6 is runner-only.
- **Do not** convert Linux/macOS CI to vcpkg; **do not** add a static-release matrix; **do not** enable `-race` on Windows. All out of scope per the spec.

## Completion

After Task 6 is green, use **superpowers:finishing-a-development-branch** to merge `feat/windows-parity` → `main`. Because the Windows job is push-only-to-main, the merge to main will run it once more on `main`; watch that run green before tagging any release. Windows support ships as **v0.63.0** (per the CHANGELOG entry).
```
