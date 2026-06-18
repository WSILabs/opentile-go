//go:build bfparity

package oracle_test

import (
	"bytes"
	"fmt"
	"image"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	opentile "github.com/wsilabs/opentile-go"
	_ "github.com/wsilabs/opentile-go/decoder/all"
	_ "github.com/wsilabs/opentile-go/formats/all"
)

// TestBIFStitchPixelOracle is an END-TO-END pixel correctness proof for BIF
// stitching (GH #60). It compares opentile-go's stitched ReadRegion pixels
// against bio-formats' equivalent crop of the SAME stitched image (Ventana
// series #0 = the full-resolution L0 mosaic), within a per-channel tolerance.
//
// Why a tolerance and not byte-equality: bio-formats decodes the underlying
// JPEG tiles with the JVM JPEG codec and re-encodes the crop as PNG, whereas
// opentile-go decodes with libjpeg-turbo. The two IDCT/upsampling paths differ
// by ±1–2 per channel on a minority of pixels — that's decoder rounding, not a
// stitch error. So the assertion is two-pronged:
//
//	(a) per-channel tolerance + a small over-tolerance fraction cap, and
//	(b) a structural mean-abs-diff guard.
//
// The structural guard is the actual placement proof: a tile MISPLACEMENT
// (the #57-class serpentine bug, or the #60 grid/width bug) shifts whole tiles
// relative to bio-formats' ground truth, so the crops diverge structurally and
// the mean-abs-diff explodes far past 2.0 — it cannot sneak through the
// per-pixel tolerance. Decoder rounding alone keeps mean-abs-diff well under 1.
//
// This is LOCAL / fixture-gated, like TestBIFTilePlacementSpatial. It SKIPS
// cleanly on CI when either the Ventana-1.bif fixture or the bio-formats CLI
// (/opt/bftools/bfconvert) is absent. openslide can't be the reference here —
// it rejects this DP 200 file ("Bad direction LEFT").
//
// Build tag: bfparity (the same tag the existing bio-formats oracle,
// TestBioFormatsParity_SCN, uses).
func TestBIFStitchPixelOracle(t *testing.T) {
	const bfconvert = "/opt/bftools/bfconvert"
	if _, err := exec.LookPath(bfconvert); err != nil {
		t.Skipf("%s not installed", bfconvert)
	}
	dir := os.Getenv("OPENTILE_TESTDIR")
	if dir == "" {
		t.Skip("OPENTILE_TESTDIR not set")
	}
	bifPath := filepath.Join(dir, "bif", "Ventana-1.bif")
	if _, err := os.Stat(bifPath); err != nil {
		t.Skipf("Ventana-1.bif not present: %v", err)
	}

	// Tolerance + structural guard (per Task 10 spec).
	const (
		perChannelTol  = 3
		maxOverTolFrac = 0.005 // < ~0.5% of channel samples may exceed tolerance
		maxMeanAbsDiff = 2.0   // structural guard — a misplacement blows this up
	)

	// Region: a tissue-bearing interior rectangle. (8000,8000) lands on dark
	// tissue (probed mean ~80), well clear of the white right/bottom padding
	// and the slide edges. 1024×1024 spans multiple 2048²-ish BIF tiles, so it
	// straddles tile seams — exactly where a placement bug would show.
	const (
		rx, ry = 8000, 8000
		rw, rh = 1024, 1024
	)

	// --- opentile-go stitched crop --------------------------------------
	s, err := opentile.OpenFile(bifPath)
	if err != nil {
		t.Fatalf("OpenFile: %v", err)
	}
	defer s.Close()

	lvl, err := s.Pyramid(0).Level(0)
	if err != nil {
		t.Fatalf("Level(0): %v", err)
	}
	img, err := lvl.ReadRegion(opentile.Region{
		Origin: opentile.Point{X: rx, Y: ry},
		Size:   opentile.Size{W: rw, H: rh},
	})
	if err != nil {
		t.Fatalf("ReadRegion: %v", err)
	}
	if img.Width != rw || img.Height != rh {
		t.Fatalf("ReadRegion returned %dx%d, want %dx%d", img.Width, img.Height, rw, rh)
	}

	// --- bio-formats ground-truth crop ----------------------------------
	tmpDir := t.TempDir()
	bfPNG := filepath.Join(tmpDir, "bf_crop.png")
	cmd := exec.Command(bfconvert,
		"-no-upgrade", "-overwrite",
		"-series", "0",
		"-crop", fmt.Sprintf("%d,%d,%d,%d", rx, ry, rw, rh),
		bifPath, bfPNG)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("bfconvert crop failed: %v\n%s", err, out)
	}
	bf, err := decodePNG(bfPNG)
	if err != nil {
		t.Fatalf("decode bf crop: %v", err)
	}
	if bf.Bounds().Dx() != rw || bf.Bounds().Dy() != rh {
		t.Fatalf("bio-formats crop is %dx%d, want %dx%d",
			bf.Bounds().Dx(), bf.Bounds().Dy(), rw, rh)
	}

	// --- compare per channel --------------------------------------------
	var (
		totalChannels int64
		overTol       int64
		sumAbsDiff    int64
	)
	b := bf.Bounds()
	for y := 0; y < rh; y++ {
		row := img.Pix[y*img.Stride:]
		for x := 0; x < rw; x++ {
			r16, g16, b16, _ := bf.At(b.Min.X+x, b.Min.Y+y).RGBA()
			want := [3]int{int(r16 >> 8), int(g16 >> 8), int(b16 >> 8)}
			got := [3]int{int(row[x*3]), int(row[x*3+1]), int(row[x*3+2])}
			for c := 0; c < 3; c++ {
				d := got[c] - want[c]
				if d < 0 {
					d = -d
				}
				sumAbsDiff += int64(d)
				totalChannels++
				if d > perChannelTol {
					overTol++
				}
			}
		}
	}

	overTolFrac := float64(overTol) / float64(totalChannels)
	meanAbsDiff := float64(sumAbsDiff) / float64(totalChannels)

	t.Logf("BIF stitch oracle: region (%d,%d) %dx%d L0 ; "+
		"over-tol(>%d) fraction = %.4f%% (cap %.2f%%) ; mean-abs-diff = %.4f (cap %.2f)",
		rx, ry, rw, rh, perChannelTol,
		overTolFrac*100, maxOverTolFrac*100, meanAbsDiff, maxMeanAbsDiff)

	if meanAbsDiff >= maxMeanAbsDiff {
		t.Fatalf("STRUCTURAL MISMATCH: mean-abs-diff %.4f >= %.2f — "+
			"this indicates a tile MISPLACEMENT in the stitch engine (a decoder-rounding-only "+
			"difference stays well under 1.0). Do NOT loosen the threshold; the stitch is wrong.",
			meanAbsDiff, maxMeanAbsDiff)
	}
	if overTolFrac >= maxOverTolFrac {
		t.Fatalf("per-pixel mismatch: %.4f%% of channels exceed tolerance %d (cap %.2f%%); "+
			"mean-abs-diff %.4f was within structural guard, so this is likely a thin "+
			"seam line or excessive decoder divergence — report, don't loosen",
			overTolFrac*100, perChannelTol, maxOverTolFrac*100, meanAbsDiff)
	}
}

// decodePNG reads a PNG file into an image.Image.
func decodePNG(path string) (image.Image, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return png.Decode(bytes.NewReader(data))
}
