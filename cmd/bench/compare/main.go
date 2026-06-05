//go:build openslidebench

// Command compare emits the cross-language competitive benchmark report:
// opentile-go vs openslide (ReadRegion + DecodedTile, both against
// openslide read_region — its only decode path) and vs python opentile
// (RawTile), across the format overlap. Build with -tags openslidebench (needs
// libopenslide); the python axis needs a python-opentile interpreter via
// OPENTILE_ORACLE_PYTHON. Run from the repository root (it shells out to
// cmd/bench/compare/opentile_perf.py by relative path).
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"

	opentile "github.com/wsilabs/opentile-go"
	"github.com/wsilabs/opentile-go/bench"
	_ "github.com/wsilabs/opentile-go/decoder/all"
	_ "github.com/wsilabs/opentile-go/formats/all"
	"github.com/wsilabs/opentile-go/internal/openslideshim"
)

const ts = 256

type row struct {
	Format          string  `json:"format"`
	OpentileRegion  float64 `json:"opentile_readregion_mpixs"`
	OpenslideRegion float64 `json:"openslide_readregion_mpixs"`
	OpentileDecoded float64 `json:"opentile_decodedtile_mpixs"`
	OpentileTile    float64 `json:"opentile_tile_mpixs"`
	PythonTile      float64 `json:"python_tile_mpixs"`
}

func pythonBin() string {
	for _, k := range []string{"OPENTILE_OPENSLIDE_PYTHON", "OPENTILE_ORACLE_PYTHON"} {
		if v := os.Getenv(k); v != "" {
			return v
		}
	}
	return "python3"
}

// interiorCoords yields a bounded interior grid of tile coords for an
// opentile-go level (offset 1 in, capped at 16×16).
func interiorCoords(gridW, gridH int) [][2]int {
	var out [][2]int
	for ty := 1; ty < 17 && ty < gridH; ty++ {
		for tx := 1; tx < 17 && tx < gridW; tx++ {
			out = append(out, [2]int{tx, ty})
		}
	}
	if len(out) == 0 {
		out = [][2]int{{0, 0}}
	}
	return out
}

// timeOpentileRegion times opentile-go ReadRegion over a bounded grid.
func timeOpentileRegion(path string) float64 {
	s, err := opentile.OpenFile(path)
	if err != nil {
		return 0
	}
	defer s.Close()
	base := s.Levels()[0]
	tw, th := base.TileSize.W, base.TileSize.H
	coords := interiorCoords(base.Grid.W, base.Grid.H)
	n := 0
	t0 := time.Now()
	for _, c := range coords {
		if _, err := s.ReadRegion(0, c[0]*tw, c[1]*th, tw, th); err == nil {
			n++
		}
	}
	el := time.Since(t0)
	return bench.MpixPerSec(int64(n)*int64(tw)*int64(th), el)
}

// timeOpentileDecoded times opentile-go ImageDecodedTile (the v0.27 fast
// decode path) over a bounded grid. This is the truest decode-vs-decode
// counterpart to openslide read_region (charted in the same column group),
// and the path ScaledStrips/DZI actually consume — leaner than ReadRegion,
// which adds fill/scratch machinery on top of the same decode.
func timeOpentileDecoded(path string) float64 {
	s, err := opentile.OpenFile(path)
	if err != nil {
		return 0
	}
	defer s.Close()
	base := s.Levels()[0]
	tw, th := base.TileSize.W, base.TileSize.H
	coords := interiorCoords(base.Grid.W, base.Grid.H)
	n := 0
	t0 := time.Now()
	for _, c := range coords {
		if _, err := s.ImageDecodedTile(0, base.Index, c[0], c[1]); err == nil {
			n++
		}
	}
	el := time.Since(t0)
	return bench.MpixPerSec(int64(n)*int64(tw)*int64(th), el)
}

// timeOpentileTile times opentile-go RawTile (compressed) over the grid.
func timeOpentileTile(path string) float64 {
	s, err := opentile.OpenFile(path)
	if err != nil {
		return 0
	}
	defer s.Close()
	base := s.Levels()[0]
	tw, th := base.TileSize.W, base.TileSize.H
	coords := interiorCoords(base.Grid.W, base.Grid.H)
	n := 0
	t0 := time.Now()
	for _, c := range coords {
		if _, err := s.RawTile(0, c[0], c[1]); err == nil {
			n++
		}
	}
	el := time.Since(t0)
	return bench.MpixPerSec(int64(n)*int64(tw)*int64(th), el)
}

func timeOpenslideRegion(path string) float64 {
	s, err := openslideshim.Open(path)
	if err != nil {
		return 0
	}
	defer s.Close()
	w, h := s.LevelDimensions(0)
	buf := make([]uint32, ts*ts)
	n := 0
	t0 := time.Now()
	for ty := int64(1); ty < 17 && (ty+1)*ts <= h; ty++ {
		for tx := int64(1); tx < 17 && (tx+1)*ts <= w; tx++ {
			if err := s.ReadRegion(buf, 0, tx*ts, ty*ts, ts, ts); err == nil {
				n++
			}
		}
	}
	el := time.Since(t0)
	return bench.MpixPerSec(int64(n)*ts*ts, el)
}

func timePythonTile(path string) float64 {
	script := filepath.Join("cmd", "bench", "compare", "opentile_perf.py")
	out, err := exec.Command(pythonBin(), script, path).Output()
	if err != nil {
		return 0
	}
	var res struct {
		MpixPerS float64 `json:"mpix_per_s"`
		Error    string  `json:"error"`
	}
	if json.Unmarshal(out, &res) != nil || res.Error != "" {
		return 0
	}
	return res.MpixPerS
}

func main() {
	fmt.Printf("compare: %d-core %s/%s\n\n", runtime.NumCPU(), runtime.GOOS, runtime.GOARCH)
	var rows []row
	for _, e := range bench.Matrix {
		path, ok := bench.FixturePath(e.Fixture)
		if !ok {
			fmt.Fprintf(os.Stderr, "skip %s: fixture missing %s\n", e.Format, path)
			continue
		}
		r := row{Format: e.Format}
		r.OpentileRegion = timeOpentileRegion(path)
		r.OpentileDecoded = timeOpentileDecoded(path)
		r.OpentileTile = timeOpentileTile(path)
		if e.Openslide {
			r.OpenslideRegion = timeOpenslideRegion(path)
		}
		if e.Python {
			r.PythonTile = timePythonTile(path)
		}
		rows = append(rows, r)
	}

	ratio := func(a, b float64) string {
		if b == 0 {
			return "—"
		}
		return fmt.Sprintf("%.2fx", a/b)
	}
	num := func(v float64) string {
		if v == 0 {
			return "—"
		}
		return fmt.Sprintf("%.0f", v)
	}

	// openslide's only decode path is read_region, so it serves as the
	// baseline for BOTH the ReadRegion and DecodedTile column groups.
	fmt.Println("| format | ReadRegion: opentile-go | openslide | ratio | DecodedTile: opentile-go | openslide | ratio | RawTile: opentile-go | python | ratio |")
	fmt.Println("|---|---|---|---|---|---|---|---|---|---|")
	for _, r := range rows {
		fmt.Printf("| %s | %s | %s | %s | %s | %s | %s | %s | %s | %s |\n",
			r.Format,
			num(r.OpentileRegion), num(r.OpenslideRegion), ratio(r.OpentileRegion, r.OpenslideRegion),
			num(r.OpentileDecoded), num(r.OpenslideRegion), ratio(r.OpentileDecoded, r.OpenslideRegion),
			num(r.OpentileTile), num(r.PythonTile), ratio(r.OpentileTile, r.PythonTile))
	}

	js, _ := json.MarshalIndent(rows, "", "  ")
	fmt.Printf("\n```json\n%s\n```\n", js)
}
