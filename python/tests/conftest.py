import os
import pathlib
import pytest


def pytest_configure(config):
    # CI's wheel test sets this so the suite fails if it picked up the source
    # tree's opentile_go instead of the installed, repaired wheel.
    if os.environ.get("OPENTILE_GO_EXPECT_INSTALLED") == "1":
        import opentile_go
        where = os.path.normcase(opentile_go.__file__)
        assert "site-packages" in where, f"opentile_go loaded from {where}, not the installed wheel"

def _find_slide():
    # OPENTILE_TESTDIR (CI corpus) or the repo's local sample_files.
    roots = []
    if os.environ.get("OPENTILE_TESTDIR"):
        roots.append(pathlib.Path(os.environ["OPENTILE_TESTDIR"]))
    roots.append(pathlib.Path(__file__).resolve().parents[2] / "sample_files")
    for root in roots:
        for pat in ("**/CMU-1-Small-Region.svs", "**/*.svs", "**/*.tiff", "**/*.ndpi"):
            hits = sorted(root.glob(pat))
            if hits:
                return str(hits[0])
    return None

@pytest.fixture(scope="session")
def slide_path():
    p = _find_slide()
    if not p:
        pytest.skip("no fixture slide found (set OPENTILE_TESTDIR or add sample_files/)")
    return p
