import importlib.util
import pathlib


def _load_setup_module():
    path = pathlib.Path(__file__).resolve().parents[1] / "setup.py"
    spec = importlib.util.spec_from_file_location("_ot_setup", path)
    mod = importlib.util.module_from_spec(spec)
    # setup.py guards its setup() call under an import-only flag, so importing it
    # only defines the classes.
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
