//go:build cgo && !nocgo

package opentile_test

import (
	"testing"

	opentile "github.com/wsilabs/opentile-go"
	"github.com/wsilabs/opentile-go/decoder"
	_ "github.com/wsilabs/opentile-go/decoder/all"
	_ "github.com/wsilabs/opentile-go/formats/all"
)

// TestLegacyClassicTIFFBIF is the #37 regression: legacy iScan scanners
// (Coreo/HT, ~2010-2012, BuildVersion 3.x) wrote BIF as *classic* (non-BigTIFF)
// little-endian TIFF. opentile-go's BIF detection used to require BigTIFF, so
// these slides fell through to the generic-TIFF reader, which lacks BIF's
// blank-tile/associated-image handling and mis-rendered them (the "corrupt BIF"
// symptom in viewers) — or failed to open at all. After the detection fix they
// must open through the BIF reader (which addresses tiles row-major) and read
// cleanly.
//
// These are real clinical slides (PHI in the labels / accession-numbered
// filenames), so they live only in the local gitignored sample set and are
// never published to the public corpus — the test skips when they're absent
// (always in CI). The synthetic classic-TIFF detection case is covered in CI by
// formats/bif.TestSupportsClassicTIFFWithIScan.
func TestLegacyClassicTIFFBIF(t *testing.T) {
	for _, rel := range []string{
		"bif/1_19.bif",
		"bif/AC1.592.bif",
		"bif/S12-18199-1A.bif",
	} {
		rel := rel
		t.Run(rel, func(t *testing.T) {
			s, err := opentile.OpenFile(crossFixture(t, rel)) // t.Skip if missing
			if err != nil {
				t.Fatalf("OpenFile: %v", err)
			}
			defer s.Close()

			// Must be claimed by the BIF reader — not generic-TIFF (which lacks
			// BIF's blank-tile/associated-image handling) and not rejected as unknown.
			if s.Format() != opentile.FormatBIF {
				t.Fatalf("format = %s, want bif (legacy classic-TIFF iScan must route to the BIF reader)", s.Format())
			}

			p := s.Pyramid(0)
			if len(p.Levels) < 2 {
				t.Fatalf("levels = %d, want a multi-level pyramid", len(p.Levels))
			}

			// Read every tile of the smallest level; none must error.
			li := len(p.Levels) - 1
			lv, err := s.Level(li)
			if err != nil {
				t.Fatalf("Level(%d): %v", li, err)
			}
			g := p.Levels[li].Grid
			for y := 0; y < g.H; y++ {
				for x := 0; x < g.W; x++ {
					if _, err := lv.Tile(x, y); err != nil {
						t.Fatalf("smallest-level Tile(%d,%d): %v", x, y, err)
					}
				}
			}

			// A mid-grid base tile decodes to the level's tile size with content.
			base, err := s.Level(0)
			if err != nil {
				t.Fatalf("Level(0): %v", err)
			}
			bg := p.Levels[0].Grid
			img, err := base.DecodedTile(bg.W/2, bg.H/2, opentile.WithFormat(decoder.PixelFormatRGB))
			if err != nil {
				t.Fatalf("L0 DecodedTile: %v", err)
			}
			if img.Width != p.Levels[0].TileSize.W || img.Height != p.Levels[0].TileSize.H {
				t.Errorf("decoded %dx%d, want tile %dx%d", img.Width, img.Height, p.Levels[0].TileSize.W, p.Levels[0].TileSize.H)
			}

			// Associated images (label/overview) are present and decode.
			for _, a := range s.AssociatedImages() {
				if _, err := a.Decode(decoder.DecodeOptions{Format: decoder.PixelFormatRGB}); err != nil {
					t.Errorf("associated %q Decode: %v", a.Type(), err)
				}
			}
		})
	}
}
