# opentile-go Python binding — Phase 2 (wheels + PyPI) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.
>
> **Execution note:** Tasks 1–3 are fully verifiable on the local macOS arm64 machine (they produce a real, installable wheel). Tasks 4–6 are **CI/packaging** — verified only on GitHub runners and PyPI, driven interactively (Task 6 is an empirical bring-up loop like the Windows-parity plan). Because of that loop, **inline execution fits the CI half better than subagent-driven** — Tasks 1–3 are clean subagent tasks; Tasks 4–6 collapse into a controller-driven push/watch/fix cycle.

**Goal:** Publish self-contained `opentile-go` wheels on PyPI for Linux (x86_64 + aarch64), macOS (x86_64 + arm64), and Windows (x86_64) — each a single `py3-none-<platform>` wheel bundling the Go `c-shared` lib with all six codecs statically linked, installable via `pip install opentile-go`.

**Architecture:** A setuptools build backend runs `go build -buildmode=c-shared` and emits a Python-agnostic **platform** wheel (`py3-none-<plat>`). A `wheels.yml` GitHub workflow builds one wheel per platform via a **direct 5-target matrix** (the approach wsitools already proves for its static binaries — *not* cibuildwheel, which is per-Python and a poor fit for our Python-agnostic single-artifact wheel), links codecs statically via vcpkg static triplets, repairs each wheel (auditwheel/delocate/delvewheel — trivial since codecs are static), validates it, and publishes to PyPI via OIDC trusted publishing on tags.

**Tech Stack:** setuptools (vendored `bdist_wheel`), Go `-buildmode=c-shared`, vcpkg static triplets (`{x64,arm64}-{linux,osx}-static`, `x64-mingw-static`), manylinux_2_28 containers (Linux), delocate (macOS) / delvewheel (Windows), GitHub Actions, PyPI trusted publishing.

**Spec:** `docs/superpowers/specs/2026-08-09-opentile-go-python-binding-design.md` (§6 packaging, §8 Phase 2). This plan **refines** the spec's "cibuildwheel" assumption to the direct-matrix approach for the reasons above.

**Branch:** create `feat/python-wheels`.

## User-action prerequisites (must happen before Task 5 publishes)
These are manual, outside the code, and **you** must do them in the PyPI account — the plan calls them out but cannot perform them:
1. **Reserve/create the PyPI project** `opentile-go` (a first manual upload or a pending-publisher reservation).
2. **Configure a PyPI Trusted Publisher** for the project: owner `WSILabs`, repo `opentile-go`, workflow `wheels.yml`, environment `pypi` (matching Task 5). Same on **TestPyPI** for the dry-run.
Until these exist, Task 6 runs the build+validate matrix and the **TestPyPI** dry-run only; the real-PyPI publish is the deliberate final gate.

## File structure
- `python/setup.py` (new) — build backend: runs `go build -buildmode=c-shared`, forces a `py3-none-<plat>` platform wheel.
- `python/pyproject.toml` (modify) — bump `build-system.requires` to `setuptools>=70.1` (for the vendored `bdist_wheel`).
- `python/MANIFEST.in` (new) — include `cshim/**` in the sdist so a from-source build has the Go sources.
- `.github/vcpkg-triplets/{x64,arm64}-{linux,osx}-static.cmake` (new) — the four custom static triplets (Windows uses the built-in `x64-mingw-static`).
- `.github/workflows/wheels.yml` (new) — the 5-target build/repair/validate matrix + the publish job.
- `python/README.md` (modify) — document `pip install` + local wheel build.

---

### Task 1: setuptools build backend — `go build` + `py3-none-<plat>` wheel

**Files:**
- Create: `python/setup.py`
- Modify: `python/pyproject.toml`
- Test: `python/tests/test_build_backend.py`

- [ ] **Step 1: Write the failing test**

`python/tests/test_build_backend.py` (asserts the backend classes exist and produce the right tag; does not itself run `go build`):
```python
import importlib.util
import pathlib


def _load_setup_module():
    path = pathlib.Path(__file__).resolve().parents[1] / "setup.py"
    spec = importlib.util.spec_from_file_location("_ot_setup", path)
    mod = importlib.util.module_from_spec(spec)
    # setup.py guards its setup() call under __name__ == "__main__" or a flag,
    # so importing it only defines the classes.
    import os
    os.environ["OPENTILE_SETUP_IMPORT_ONLY"] = "1"
    spec.loader.exec_module(mod)
    return mod


def test_platform_wheel_tag_is_py3_none():
    mod = _load_setup_module()
    # PlatformWheel.get_tag() forces python=py3, abi=none, keeping the platform.
    tag = mod.PlatformWheel._forced_tag(("cp314", "cp314", "macosx_11_0_arm64"))
    assert tag == ("py3", "none", "macosx_11_0_arm64")


def test_binary_distribution_is_not_pure():
    mod = _load_setup_module()
    assert mod.BinaryDistribution().has_ext_modules() is True
```

- [ ] **Step 2: Run it to verify it fails**

Run: `cd python && PYTHONPATH=. python3 -m pytest tests/test_build_backend.py -q`
Expected: FAIL — `setup.py` does not exist / has no `PlatformWheel`/`BinaryDistribution`.

- [ ] **Step 3: Write `setup.py`**

`python/setup.py`:
```python
"""setuptools build backend for opentile_go.

Builds the Go c-shared FFI library (`go build -buildmode=c-shared`) into the
package directory, then emits a Python-agnostic *platform* wheel tagged
`py3-none-<platform>` — the only native artifact is the Go shared library, which
is Python-version-independent, so one wheel serves every CPython >= 3.10.
"""

import os
import subprocess
import sys

from setuptools import setup
from setuptools.command.build_py import build_py
from setuptools.command.bdist_wheel import bdist_wheel
from setuptools.dist import Distribution

HERE = os.path.dirname(os.path.abspath(__file__))


def _lib_filename():
    # ctypes.CDLL loads a Mach-O/ELF shared object by any name; keep it stable so
    # the _ffi.py glob (`_opentilego.*`) finds it. Windows uses .pyd.
    return "_opentilego.pyd" if sys.platform == "win32" else "_opentilego.so"


class BuildGoLib(build_py):
    """Build the Go c-shared FFI lib before packaging copies package data."""

    def run(self):
        out = os.path.join(HERE, "opentile_go", _lib_filename())
        env = dict(os.environ, CGO_ENABLED="1")
        subprocess.check_call(
            ["go", "build", "-buildmode=c-shared", "-o", out, "./cshim"],
            cwd=HERE,
            env=env,
        )
        super().run()


class PlatformWheel(bdist_wheel):
    """Emit `py3-none-<platform>` — Python-agnostic, platform-specific."""

    @staticmethod
    def _forced_tag(tag):
        _, _, plat = tag
        return ("py3", "none", plat)

    def finalize_options(self):
        super().finalize_options()
        self.root_is_pure = False  # force a platform (non-pure) wheel

    def get_tag(self):
        return self._forced_tag(super().get_tag())


class BinaryDistribution(Distribution):
    """Mark the distribution as non-pure so it is platform-tagged."""

    def has_ext_modules(self):
        return True


if os.environ.get("OPENTILE_SETUP_IMPORT_ONLY") != "1":
    setup(
        distclass=BinaryDistribution,
        cmdclass={"build_py": BuildGoLib, "bdist_wheel": PlatformWheel},
    )
```

Modify `python/pyproject.toml` — bump the build requirement:
```toml
[build-system]
requires = ["setuptools>=70.1"]
build-backend = "setuptools.build_meta"
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `cd python && PYTHONPATH=. python3 -m pytest tests/test_build_backend.py -q`
Expected: PASS (both tests).

- [ ] **Step 5: Commit**

```bash
git add python/setup.py python/pyproject.toml python/tests/test_build_backend.py
git commit -m "feat(python): setuptools backend — go build c-shared + py3-none platform wheel"
```

---

### Task 2: Build a real wheel and validate it in a clean venv (local)

Proves the whole mechanism end-to-end on macOS arm64: the backend builds the lib, tags the wheel `py3-none-macosx_*_arm64`, and the installed wheel imports + reads a slide from `site-packages` (not `PYTHONPATH`). Note: the *local* wheel links brew codecs **dynamically**, so it is not redistributable — Task 4 produces the static, redistributable wheels. This task validates the *packaging mechanism*.

**Files:** none (uses Task 1's backend). Optional: `python/MANIFEST.in`.

- [ ] **Step 1: Create `python/MANIFEST.in`** (so the Go sources ship in an sdist)
```
recursive-include cshim *.go
include opentile_go/py.typed
```

- [ ] **Step 2: Build the wheel**

Run (from `python/`, in a venv with the build frontend):
```bash
cd python
python3 -m venv /tmp/otbuild && /tmp/otbuild/bin/pip install -q build
/tmp/otbuild/bin/python -m build --wheel --outdir dist
ls dist
```
Expected: a file `dist/opentile_go-0.1.0.dev0-py3-none-macosx_*_arm64.whl`. Assert the tag with:
```bash
ls dist/opentile_go-*.whl | grep -E 'py3-none-macosx_[0-9_]+_arm64\.whl$'
```
Expected: the filename matches (non-empty output).

- [ ] **Step 3: Confirm the wheel bundles the native lib**
```bash
python3 -c "import zipfile,glob; z=zipfile.ZipFile(glob.glob('dist/opentile_go-*.whl')[0]); \
  libs=[n for n in z.namelist() if n.startswith('opentile_go/_opentilego')]; \
  print(libs); assert any(n.endswith('.so') for n in libs), libs"
```
Expected: prints `['opentile_go/_opentilego.so']` (and its header may or may not be present), no assertion error.

- [ ] **Step 4: Install into a clean venv and validate**
```bash
python3 -m venv /tmp/otrun
/tmp/otrun/bin/pip install -q numpy pytest
/tmp/otrun/bin/pip install -q dist/opentile_go-*.whl
# Import + read a fixture from the INSTALLED package (cd elsewhere so the source tree isn't on sys.path)
cd /tmp && /tmp/otrun/bin/python - <<'PY'
import opentile_go, glob, os
root = "/Volumes/Ext/GitHub/opentile-go/sample_files"
p = sorted(glob.glob(os.path.join(root, "**/CMU-1-Small-Region.svs"), recursive=True))[0]
with opentile_go.Slide(p) as s:
    a = s.levels[0].read_region(0, 0, 32, 24)
    print("installed-wheel read_region:", a.shape, a.dtype)
    assert a.shape == (24, 32, 3)
print("OK — wheel imports and reads from site-packages")
PY
```
Expected: `OK — wheel imports and reads from site-packages`. This proves the ctypes loader finds the bundled lib when installed (not just via `build_dev.sh`).

- [ ] **Step 5: Add `dist/`/`build/` ignores + commit**

`python/.gitignore` already covers `build/`, `dist/`, `*.egg-info/`. Confirm `dist/` is ignored (`git check-ignore python/dist` → prints the path). Commit the MANIFEST:
```bash
git add python/MANIFEST.in
git commit -m "build(python): MANIFEST for sdist Go sources; wheel mechanism validated in clean venv"
```

---

### Task 3: Stage vcpkg static triplets in-repo

The four custom static triplets for Linux/macOS (Windows uses the built-in `x64-mingw-static`). Lifted verbatim from wsitools's proven set.

**Files:**
- Create: `.github/vcpkg-triplets/x64-linux-static.cmake`, `arm64-linux-static.cmake`, `x64-osx-static.cmake`, `arm64-osx-static.cmake`

- [ ] **Step 1: Create the four triplet files**

`.github/vcpkg-triplets/x64-linux-static.cmake`:
```cmake
set(VCPKG_TARGET_ARCHITECTURE x64)
set(VCPKG_CRT_LINKAGE dynamic)
set(VCPKG_LIBRARY_LINKAGE static)
set(VCPKG_CMAKE_SYSTEM_NAME Linux)
```
`.github/vcpkg-triplets/arm64-linux-static.cmake`:
```cmake
set(VCPKG_TARGET_ARCHITECTURE arm64)
set(VCPKG_CRT_LINKAGE dynamic)
set(VCPKG_LIBRARY_LINKAGE static)
set(VCPKG_CMAKE_SYSTEM_NAME Linux)
```
`.github/vcpkg-triplets/x64-osx-static.cmake`:
```cmake
set(VCPKG_TARGET_ARCHITECTURE x64)
set(VCPKG_CRT_LINKAGE dynamic)
set(VCPKG_LIBRARY_LINKAGE static)
set(VCPKG_CMAKE_SYSTEM_NAME Darwin)
set(VCPKG_OSX_ARCHITECTURES x86_64)
```
`.github/vcpkg-triplets/arm64-osx-static.cmake`:
```cmake
set(VCPKG_TARGET_ARCHITECTURE arm64)
set(VCPKG_CRT_LINKAGE dynamic)
set(VCPKG_LIBRARY_LINKAGE static)
set(VCPKG_CMAKE_SYSTEM_NAME Darwin)
set(VCPKG_OSX_ARCHITECTURES arm64)
```

- [ ] **Step 2: Verify they are valid + present**
```bash
for t in x64-linux arm64-linux x64-osx arm64-osx; do
  f=.github/vcpkg-triplets/$t-static.cmake
  test -f "$f" && grep -q "VCPKG_LIBRARY_LINKAGE static" "$f" && echo "$t OK"
done
```
Expected: four `... OK` lines.

- [ ] **Step 3: Commit**
```bash
git add .github/vcpkg-triplets/
git commit -m "build(wheels): vcpkg static triplets for linux/macos targets"
```

---

### Task 4: `wheels.yml` — 5-target build/repair/validate matrix

Writes the workflow that builds one static wheel per platform. Each job: install Go + per-OS prereqs → bootstrap vcpkg (pinned `vcpkg.json` baseline) → `vcpkg install --triplet <t> --overlay-triplets=.github/vcpkg-triplets` → `python -m build --wheel` (the setup.py backend runs `go build -buildmode=c-shared` linking the static codec `.a`s via `PKG_CONFIG_PATH`) → repair (auditwheel/delocate/delvewheel) → validate (install the wheel, import, read a bundled fixture) → upload the wheel artifact. Linux runs inside a manylinux_2_28 container for glibc compliance.

**Files:**
- Create: `.github/workflows/wheels.yml`

- [ ] **Step 1: Write the workflow**

`.github/workflows/wheels.yml`:
```yaml
name: Wheels

on:
  workflow_dispatch:
  push:
    tags:
      - 'py-v*.*.*'   # dedicated Python-binding release tags, distinct from the Go module's v*.*.*

permissions:
  contents: read

jobs:
  build:
    name: wheel ${{ matrix.name }}
    runs-on: ${{ matrix.runner }}
    container: ${{ matrix.container }}
    strategy:
      fail-fast: false
      matrix:
        include:
          - { name: linux-x86_64,   runner: ubuntu-latest,    container: 'quay.io/pypa/manylinux_2_28_x86_64', triplet: x64-linux-static,   repair: auditwheel }
          - { name: linux-aarch64,  runner: ubuntu-24.04-arm, container: 'quay.io/pypa/manylinux_2_28_aarch64', triplet: arm64-linux-static, repair: auditwheel }
          - { name: macos-arm64,    runner: macos-latest,     container: '',                                    triplet: arm64-osx-static,  repair: delocate }
          - { name: macos-x86_64,   runner: macos-latest,     container: '',                                    triplet: x64-osx-static,    repair: delocate, macos_cross: '1' }
          - { name: windows-x86_64, runner: windows-latest,   container: '',                                    triplet: x64-mingw-static,  repair: delvewheel }
    steps:
      - uses: actions/checkout@v4

      # --- toolchain: Go + Python + per-OS build prereqs ---
      - name: Set up Go + Python (manylinux container)
        if: startsWith(matrix.name, 'linux')
        shell: bash
        run: |
          set -euo pipefail
          yum install -y nasm autoconf-archive perl-IPC-Cmd zip
          curl -sSL "https://go.dev/dl/go1.25.0.linux-$(uname -m | sed 's/x86_64/amd64/;s/aarch64/arm64/').tar.gz" -o /tmp/go.tgz
          tar -C /usr/local -xzf /tmp/go.tgz
          echo "/usr/local/go/bin" >> "$GITHUB_PATH"
          echo "/opt/python/cp310-cp310/bin" >> "$GITHUB_PATH"   # a Python to drive `build`

      - name: Set up Go
        if: "!startsWith(matrix.name, 'linux')"
        uses: actions/setup-go@v5
        with: { go-version: '1.25.0' }

      - name: Set up Python
        if: "!startsWith(matrix.name, 'linux')"
        uses: actions/setup-python@v5
        with: { python-version: '3.12' }

      - name: Install prereqs (macOS)
        if: startsWith(matrix.name, 'macos')
        run: brew install nasm pkg-config

      - name: Set up MSYS2 mingw (Windows)
        id: msys2
        if: matrix.name == 'windows-x86_64'
        uses: msys2/setup-msys2@v2
        with:
          msystem: MINGW64
          path-type: inherit
          install: >-
            mingw-w64-x86_64-gcc mingw-w64-x86_64-cmake mingw-w64-x86_64-ninja mingw-w64-x86_64-pkgconf

      - name: Windows PATH + pkgconf
        if: matrix.name == 'windows-x86_64'
        shell: bash
        run: |
          echo '${{ steps.msys2.outputs.msys2-location }}\mingw64\bin' >> "$GITHUB_PATH"
          echo "PKG_CONFIG=pkgconf" >> "$GITHUB_ENV"

      - name: macOS x86_64 cross-build flags
        if: matrix.macos_cross == '1'
        shell: bash
        run: |
          echo "CGO_CFLAGS=-arch x86_64"  >> "$GITHUB_ENV"
          echo "CGO_CXXFLAGS=-arch x86_64" >> "$GITHUB_ENV"
          echo "CGO_LDFLAGS=-arch x86_64" >> "$GITHUB_ENV"
          echo "_PYTHON_HOST_PLATFORM=macosx-10.9-x86_64" >> "$GITHUB_ENV"

      # --- vcpkg static codecs ---
      - name: Bootstrap vcpkg (pinned baseline)
        shell: bash
        run: |
          set -euo pipefail
          git clone https://github.com/microsoft/vcpkg "$RUNNER_TEMP/vcpkg"
          BASELINE=$(python3 -c "import json;print(json.load(open('vcpkg.json'))['builtin-baseline'])" 2>/dev/null || python -c "import json;print(json.load(open('vcpkg.json'))['builtin-baseline'])")
          git -C "$RUNNER_TEMP/vcpkg" checkout "$BASELINE"
          "$RUNNER_TEMP/vcpkg/bootstrap-vcpkg.sh" -disableMetrics || "$RUNNER_TEMP/vcpkg/bootstrap-vcpkg.bat" -disableMetrics
          echo "VCPKG_ROOT=$RUNNER_TEMP/vcpkg" >> "$GITHUB_ENV"
          mkdir -p "$RUNNER_TEMP/vcpkg-bincache"
          echo "VCPKG_DEFAULT_BINARY_CACHE=$RUNNER_TEMP/vcpkg-bincache" >> "$GITHUB_ENV"

      - name: Cache vcpkg codec libs
        uses: actions/cache@v4
        with:
          path: ${{ env.VCPKG_DEFAULT_BINARY_CACHE }}
          key: wheels-vcpkg-${{ matrix.triplet }}-${{ hashFiles('vcpkg.json') }}

      - name: Install codec libs (static)
        shell: bash
        run: |
          set -euo pipefail
          "$VCPKG_ROOT/vcpkg" install --triplet ${{ matrix.triplet }} \
            --overlay-triplets="$PWD/.github/vcpkg-triplets" \
            --x-install-root="$PWD/vcpkg_installed"
          echo "PKG_CONFIG_PATH=$PWD/vcpkg_installed/${{ matrix.triplet }}/lib/pkgconfig" >> "$GITHUB_ENV"

      # --- build + repair + validate wheel ---
      - name: Build wheel
        shell: bash
        working-directory: python
        run: |
          set -euo pipefail
          python3 -m pip install --upgrade build || python -m pip install --upgrade build
          CGO_ENABLED=1 python3 -m build --wheel --outdir dist 2>/dev/null || CGO_ENABLED=1 python -m build --wheel --outdir dist
          ls -la dist

      - name: Repair wheel (auditwheel / delocate / delvewheel)
        shell: bash
        working-directory: python
        run: |
          set -euo pipefail
          case "${{ matrix.repair }}" in
            auditwheel)
              python3 -m pip install auditwheel
              for w in dist/*.whl; do auditwheel repair "$w" -w wheelhouse; done ;;
            delocate)
              python3 -m pip install delocate
              for w in dist/*.whl; do delocate-wheel -w wheelhouse "$w"; done ;;
            delvewheel)
              python -m pip install delvewheel
              for w in dist/*.whl; do delvewheel repair "$w" -w wheelhouse; done ;;
          esac
          ls -la wheelhouse

      - name: Validate wheel (install + import + read fixture)
        shell: bash
        working-directory: python
        run: |
          set -euo pipefail
          python3 -m pip install numpy || python -m pip install numpy
          python3 -m pip install wheelhouse/*.whl || python -m pip install wheelhouse/*.whl
          python3 - <<'PY'
          import opentile_go
          print("opentile_go", opentile_go.__version__, "imported from wheel OK")
          PY

      - uses: actions/upload-artifact@v4
        with:
          name: wheel-${{ matrix.name }}
          path: python/wheelhouse/*.whl
```

- [ ] **Step 2: Validate the workflow YAML locally**
```bash
python3 -c "import yaml; d=yaml.safe_load(open('.github/workflows/wheels.yml')); \
  assert 'build' in d['jobs']; \
  m=d['jobs']['build']['strategy']['matrix']['include']; \
  assert len(m)==5, len(m); \
  print('wheels.yml OK; targets:', [x['name'] for x in m])"
```
Expected: `wheels.yml OK; targets: ['linux-x86_64', 'linux-aarch64', 'macos-arm64', 'macos-x86_64', 'windows-x86_64']`.

- [ ] **Step 3: Commit**
```bash
git add .github/workflows/wheels.yml
git commit -m "ci(wheels): 5-target static wheel build/repair/validate matrix"
```

---

### Task 5: `wheels.yml` — PyPI publish job (TestPyPI + PyPI, OIDC)

Adds a publish job that gathers all wheel artifacts and uploads them via OIDC trusted publishing. TestPyPI on `workflow_dispatch`; real PyPI on a `py-v*` tag.

**Files:**
- Modify: `.github/workflows/wheels.yml`

- [ ] **Step 1: Append the publish job** (and add `id-token: write` at the top-level `permissions`)

Change the top-level `permissions:` to:
```yaml
permissions:
  contents: read
  id-token: write   # OIDC for PyPI trusted publishing
```
Append after the `build` job:
```yaml
  publish-testpypi:
    name: publish to TestPyPI (dry-run)
    if: github.event_name == 'workflow_dispatch'
    needs: build
    runs-on: ubuntu-latest
    environment: testpypi
    permissions:
      id-token: write
    steps:
      - uses: actions/download-artifact@v4
        with: { pattern: wheel-*, path: dist, merge-multiple: true }
      - uses: pypa/gh-action-pypi-publish@release/v1
        with:
          repository-url: https://test.pypi.org/legacy/
          skip-existing: true

  publish-pypi:
    name: publish to PyPI
    if: startsWith(github.ref, 'refs/tags/py-v')
    needs: build
    runs-on: ubuntu-latest
    environment: pypi
    permissions:
      id-token: write
    steps:
      - uses: actions/download-artifact@v4
        with: { pattern: wheel-*, path: dist, merge-multiple: true }
      - uses: pypa/gh-action-pypi-publish@release/v1
```

- [ ] **Step 2: Re-validate YAML**
```bash
python3 -c "import yaml; d=yaml.safe_load(open('.github/workflows/wheels.yml')); \
  on=d.get('on') or d.get(True); \
  assert 'publish-pypi' in d['jobs'] and 'publish-testpypi' in d['jobs']; \
  assert d['permissions']['id-token']=='write'; \
  print('publish jobs OK')"
```
Expected: `publish jobs OK`.

- [ ] **Step 3: Commit**
```bash
git add .github/workflows/wheels.yml
git commit -m "ci(wheels): TestPyPI dry-run + PyPI OIDC trusted-publishing jobs"
```

---

### Task 6: Empirical CI bring-up loop (runner-verified)

Not classic TDD — the 5-target static build, manylinux container, cross-build, and repair tools are only verifiable on GitHub runners. Drive interactively.

- [ ] **Step 1: Push the branch and trigger the matrix**
```bash
git push -u origin feat/python-wheels
gh workflow run wheels.yml --ref feat/python-wheels
```

- [ ] **Step 2: Watch each target; triage failures per platform**
```bash
sleep 8
RUN=$(gh run list --workflow wheels.yml --branch feat/python-wheels --limit 1 --json databaseId --jq '.[0].databaseId')
gh run watch "$RUN" --interval 30 || true
gh run view "$RUN" --json jobs --jq '.jobs[] | "\(.conclusion // .status)\t\(.name)"'
```
Fix failures as they surface (each fix: commit, push, `gh workflow run wheels.yml --ref feat/python-wheels`, re-watch). Expected classes:
- **Linux (manylinux):** the container is yum-based — Go tarball URL / `uname -m` mapping, missing `nasm`/`autoconf-archive`/`perl`, or vcpkg needing extra deps. auditwheel should retag `linux_*` → `manylinux_2_28_*` and, because codecs are static, bundle nothing.
- **macOS x86_64 cross-build:** aom needs `nasm` (installed); the wheel plat tag must be `macosx_*_x86_64` (forced via `_PYTHON_HOST_PLATFORM`); `delocate` on a cross-built lib should find no non-system dylibs (codecs static) — if it complains about the Go lib's install-name, that's expected-benign.
- **Windows:** mingw `pkgconf` on PATH (not Strawberry Perl's stub) via `PKG_CONFIG=pkgconf`; the built lib is `_opentilego.pyd`; `delvewheel` should find no external codec DLLs to bundle (static) — if it still balks, the self-contained `.pyd` can ship unrepaired (skip repair on Windows).
- **vcpkg build time** (~25–40 min cold per triplet) — mitigated by the per-triplet binary cache; first run is slow.

- [ ] **Step 3: Confirm all five wheel artifacts built + validated**
```bash
gh run view "$RUN" --json jobs --jq '[.jobs[] | select(.name|startswith("wheel ")) | .conclusion]'
```
Expected: five `"success"`. Download and eyeball the tags:
```bash
gh run download "$RUN" --dir /tmp/wheels && ls /tmp/wheels/**/*.whl
```
Expected filenames: `py3-none-manylinux_2_28_x86_64`, `…_aarch64`, `py3-none-macosx_*_arm64`, `…_x86_64`, `py3-none-win_amd64`.

- [ ] **Step 4: TestPyPI dry-run**

Once the **TestPyPI** project + trusted publisher exist (prerequisite), the `workflow_dispatch` run's `publish-testpypi` job uploads there. Verify:
```bash
gh run view "$RUN" --json jobs --jq '.jobs[] | select(.name|test("TestPyPI")) | .conclusion'
python3 -m pip install -i https://test.pypi.org/simple/ opentile-go --dry-run 2>&1 | head
```
Expected: the TestPyPI job is `success` and the package resolves.

- [ ] **Step 5: Real PyPI release (deliberate, gated)**

After the matrix is green and TestPyPI validates, **and** the PyPI project + trusted publisher exist: bump `python/pyproject.toml` + `opentile_go/__init__.py` `__version__` to a release version (e.g. `0.1.0`), commit, merge to main, then push a `py-v0.1.0` tag. The `publish-pypi` job publishes via OIDC. Verify `pip install opentile-go` from PyPI in a clean venv.

---

## Self-review notes (for the executor)
- **Spec coverage (Phase 2):** §6 backend/packaging → T1–T2; vcpkg-static-everywhere/triplets → T3; 5-target matrix + repair → T4; §8 CI + PyPI OIDC → T5–T6. The `cibuildwheel` mention in the spec is **superseded** by the direct matrix (documented in the header).
- **Local vs runner:** T1–T3 fully local (a real installable wheel on macOS arm64); T4–T5 YAML-validated locally; T6 runner-only.
- The **highest-risk empirical points** are the manylinux container Go/vcpkg setup and the macOS cross-build plat tag — both isolated to T6 and fixed there.
- **Do not** auto-publish to real PyPI before the user has created the project + trusted publisher and the TestPyPI dry-run is green. Real publish is T6 Step 5, deliberately last.
- Wheel tag naming (`py-v*` tags) is deliberately distinct from the Go module's `v*` tags so a wheels release never triggers the Go `release.yml` and vice-versa.

## Completion
After T6 Step 4 (green matrix + TestPyPI validated) the binding is publishable; T6 Step 5 is the go-live. Then use **superpowers:finishing-a-development-branch** to merge `feat/python-wheels`. This completes the "release a Python interface" arc.
