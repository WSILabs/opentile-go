"""setuptools build backend for opentile_go.

Puts libopentile (the opentile-go C API, ../capi) in the package directory,
then emits a Python-agnostic *platform* wheel tagged `py3-none-<platform>`.
The only native artifact is that shared library, which is Python-version
independent, so one wheel serves every CPython >= 3.10.

Two ways to get the library:
  - OPENTILE_GO_STAGE=<dir>: a release build staged by
    capi/scripts/build-release.sh (codecs and lcms2 linked in statically, only
    OS dependencies). Its LICENSE, NOTICE and licenses/ come along. Wheels use
    this, so they ship the same library as the GitHub release archives.
  - otherwise, a dev build: `go build -buildmode=c-shared` of ../capi against
    the codecs and lcms2 found by pkg-config (linked dynamically).
"""

import glob
import os
import shutil
import subprocess
import sys

from setuptools import setup
from setuptools.command.build_py import build_py
from setuptools.command.bdist_wheel import bdist_wheel
from setuptools.dist import Distribution

HERE = os.path.dirname(os.path.abspath(__file__))
PKG = os.path.join(HERE, "opentile_go")
CAPI = os.path.join(HERE, "..", "capi")


def _lib_filename():
    if sys.platform == "win32":
        return "libopentile.dll"
    if sys.platform == "darwin":
        return "libopentile.dylib"
    return "libopentile.so"


def _clean_package_dir():
    # Drop a previous build's library and notices so a wheel never mixes builds.
    for old in glob.glob(os.path.join(PKG, "libopentile.*")) + glob.glob(os.path.join(PKG, "_opentilego.*")):
        os.remove(old)
    for d in ("licenses",):
        shutil.rmtree(os.path.join(PKG, d), ignore_errors=True)
    for f in ("LICENSE", "NOTICE"):
        p = os.path.join(PKG, f)
        if os.path.exists(p):
            os.remove(p)


def _copy_stage(stage):
    sub = "bin" if sys.platform == "win32" else "lib"
    src = os.path.join(stage, sub, _lib_filename())
    if not os.path.isfile(src):
        raise SystemExit(f"OPENTILE_GO_STAGE: {src} not found (run capi/scripts/build-release.sh)")
    shutil.copy2(src, os.path.join(PKG, _lib_filename()))
    for f in ("LICENSE", "NOTICE"):
        shutil.copy2(os.path.join(stage, f), os.path.join(PKG, f))
    shutil.copytree(os.path.join(stage, "licenses"), os.path.join(PKG, "licenses"))


def _dev_build():
    out = os.path.join(PKG, _lib_filename())
    env = dict(os.environ, CGO_ENABLED="1")
    # -buildvcs=false: VCS stamping errors ("dubious ownership") inside build
    # containers, and a shared library needs no stamp.
    cmd = ["go", "build", "-buildmode=c-shared", "-buildvcs=false", "-o", out, "."]
    subprocess.check_call(cmd, cwd=CAPI, env=env)
    h = os.path.splitext(out)[0] + ".h"  # Go's generated header; opentile.h is the public one
    if os.path.exists(h):
        os.remove(h)


class BuildGoLib(build_py):
    """Put libopentile in the package before packaging copies package data."""

    def run(self):
        _clean_package_dir()
        stage = os.environ.get("OPENTILE_GO_STAGE")
        if stage:
            _copy_stage(stage)
        else:
            _dev_build()
        super().run()


class PlatformWheel(bdist_wheel):
    """Emit `py3-none-<platform>` — Python-agnostic, platform-specific.

    `go build -buildmode=c-shared` produces a *single-arch* Mach-O, but a
    universal2 CPython reports the platform as `macosx_..._universal2`. Tagging a
    single-arch lib `universal2` is a lie (it would install on the wrong arch and
    fail to load), so rewrite the platform's arch to the real Go binary arch.
    """

    @staticmethod
    def _forced_tag(tag):
        # Pure tag rewrite: Python-agnostic (py3/none), keep the platform as-is.
        _, _, plat = tag
        return ("py3", "none", plat)

    @staticmethod
    def _macos_arch():
        # arm64 / x86_64 of the just-built shared object; None if truly fat.
        so = os.path.join(HERE, "opentile_go", _lib_filename())
        try:
            out = subprocess.check_output(["lipo", "-archs", so], text=True)
        except (OSError, subprocess.CalledProcessError):
            return None
        archs = out.split()
        return archs[0] if len(archs) == 1 else None

    def finalize_options(self):
        super().finalize_options()
        self.root_is_pure = False  # force a platform (non-pure) wheel

    def get_tag(self):
        py, abi, plat = self._forced_tag(super().get_tag())
        # A universal2 CPython over-reports the platform, but Go's c-shared lib is
        # single-arch — retag to the real arch so the wheel doesn't install on a
        # mac where the .so can't load (and so arm64/x86_64 wheels don't collide).
        if sys.platform == "darwin" and plat.endswith("_universal2"):
            arch = self._macos_arch()
            if arch:
                plat = plat[: -len("universal2")] + arch
        return (py, abi, plat)


class BinaryDistribution(Distribution):
    """Mark the distribution as non-pure so it is platform-tagged."""

    def has_ext_modules(self):
        return True


if os.environ.get("OPENTILE_SETUP_IMPORT_ONLY") != "1":
    setup(
        distclass=BinaryDistribution,
        cmdclass={"build_py": BuildGoLib, "bdist_wheel": PlatformWheel},
    )
