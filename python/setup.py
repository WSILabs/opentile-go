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
