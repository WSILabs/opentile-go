package bif_test

import (
	"os"
	"path/filepath"
	"testing"

	opentile "github.com/wsilabs/opentile-go"
	"github.com/wsilabs/opentile-go/decoder"
	_ "github.com/wsilabs/opentile-go/decoder/all"
	_ "github.com/wsilabs/opentile-go/formats/all"
)

// TestReducedDPSeamPixelContinuity is the headline placement-fidelity gate for
// #83: at the compacted frame-join seams of Ventana-1 L1, the two adjacent
// reduced tiles must carry the SAME content in the 12px overlap band that the
// downsample-L0 layout removes. Low band MAD ⇒ compacting there (blitting one
// tile over the other) yields seam-continuous output stitch-aligned with L0.
//
// The compacted L1 seams (12px overlap) are between columns (3,4),(5,6),(7,8),
// (9,10) — every other column is a frame boundary (per the Phase-0 probe).
func TestReducedDPSeamPixelContinuity(t *testing.T) {
	path := "/Volumes/Ext/GitHub/opentile-go/sample_files/bif/Ventana-1.bif"
	if dir := os.Getenv("OPENTILE_TESTDIR"); dir != "" {
		path = filepath.Join(dir, "bif", "Ventana-1.bif")
	}
	if _, err := os.Stat(path); err != nil {
		t.Skip("Ventana-1.bif not present")
	}
	s, err := opentile.OpenFile(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	l1, err := s.Level(1)
	if err != nil {
		t.Fatal(err)
	}

	const band = 12 // px overlap removed at each compacted seam
	seams := [][2]int{{3, 4}, {5, 6}, {7, 8}, {9, 10}}

	var sumMAD float64
	var textured int
	for _, sm := range seams {
		for row := 0; row < l1.Grid.H; row++ {
			a, err := l1.DecodedTile(sm[0], row)
			if err != nil {
				continue
			}
			b, err := l1.DecodedTile(sm[1], row)
			if err != nil {
				continue
			}
			// a's right `band` columns vs b's left `band` columns.
			mad, vari := bandCompare(a, b, band)
			if vari < 30 { // skip blank/flat bands (background)
				continue
			}
			textured++
			sumMAD += mad
		}
	}
	if textured == 0 {
		t.Skip("no textured seam bands found (tissue layout)")
	}
	meanMAD := sumMAD / float64(textured)
	t.Logf("compacted seams with texture: %d, mean band MAD = %.2f", textured, meanMAD)
	// The overlap content matches modulo JPEG + downsample noise; the #83 probe
	// measured per-seam MAD 2–5. A generous ceiling guards against a misplaced
	// (off-by-tile) layout, which would push MAD into the tens/hundreds.
	if meanMAD > 18 {
		t.Errorf("mean seam band MAD = %.2f, want ≤ 18 (overlap bands should match; high ⇒ misplaced layout)", meanMAD)
	}
}

// bandCompare returns (meanAbsDiff, variance) over the last `band` columns of a
// vs the first `band` columns of b, across all shared rows (RGB channels).
func bandCompare(a, b *decoder.Image, band int) (mad, vari float64) {
	abpp := bpp(a)
	bbpp := bpp(b)
	h := a.Height
	if b.Height < h {
		h = b.Height
	}
	var sum, n float64
	for y := 0; y < h; y++ {
		for k := 0; k < band; k++ {
			ax := a.Width - band + k
			bx := k
			ai := y*a.Stride + ax*abpp
			bi := y*b.Stride + bx*bbpp
			for c := 0; c < 3; c++ {
				d := float64(a.Pix[ai+c]) - float64(b.Pix[bi+c])
				if d < 0 {
					d = -d
				}
				sum += d
				n++
			}
		}
	}
	if n == 0 {
		return 0, 0
	}
	mean := sum / n
	// crude texture proxy: variance of a's band luminance
	meanA := 0.0
	for y := 0; y < h; y++ {
		for k := 0; k < band; k++ {
			ax := a.Width - band + k
			ai := y*a.Stride + ax*abpp
			meanA += float64(a.Pix[ai])
		}
	}
	cnt := float64(h * band)
	meanA /= cnt
	for y := 0; y < h; y++ {
		for k := 0; k < band; k++ {
			ax := a.Width - band + k
			ai := y*a.Stride + ax*abpp
			d := float64(a.Pix[ai]) - meanA
			vari += d * d
		}
	}
	vari /= cnt
	return mean, vari
}

func bpp(im *decoder.Image) int {
	if im.Format == decoder.PixelFormatRGBA {
		return 4
	}
	return 3
}
