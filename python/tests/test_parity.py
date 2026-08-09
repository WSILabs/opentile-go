import subprocess
import pathlib
import numpy as np
from opentile_go import Slide

def test_read_region_byte_parity_with_go(slide_path):
    x, y, w, h = 0, 0, 64, 48
    gen = pathlib.Path(__file__).parent / "golden" / "gen_region.go"
    repo = pathlib.Path(__file__).resolve().parents[2]
    ref = subprocess.run(
        ["go", "run", str(gen), slide_path, str(x), str(y), str(w), str(h)],
        cwd=str(repo), capture_output=True, check=True,
    ).stdout
    with Slide(slide_path) as s:
        arr = s.levels[0].read_region(x, y, w, h, rgba=False)
    assert arr.tobytes() == ref, "python read_region bytes differ from Go reader"
    assert arr.shape == (h, w, 3)
