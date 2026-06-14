package opentile_test

import (
	"testing"

	opentile "github.com/wsilabs/opentile-go"
	"github.com/wsilabs/opentile-go/decoder"
	_ "github.com/wsilabs/opentile-go/decoder/all"
	_ "github.com/wsilabs/opentile-go/formats/all"
)

// TestDecodedTileTiledNonSelfDescribing covers the allocating DecodedTile path
// on tiled levels whose codec carries no intrinsic dimensions: LZW and
// uncompressed. The ImageScope-exported SVS fixtures use LZW / RAW tile
// compression (vs SVS-standard JPEG/JP2K), which exposed a bug — the allocating
// DecodedTile errored ("Dst is required") because the decode dispatch didn't
// pass a tile-sized Dst, while RawTile / ReadRegion / DecodedTileInto worked.
func TestDecodedTileTiledNonSelfDescribing(t *testing.T) {
	for _, tc := range []struct {
		rel, comp string
	}{
		{"svs/590_crop_lzw_imagescope.tif", "lzw"},
		{"svs/590_crop_none_imagescope.tif", "uncompressed"},
	} {
		tc := tc
		t.Run(tc.comp, func(t *testing.T) {
			s, err := opentile.OpenFile(crossFixture(t, tc.rel))
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			l, err := s.Level(0)
			if err != nil {
				t.Fatal(err)
			}
			// A mid-grid tile (the crop's corner tiles are blank background).
			tx, ty := l.Grid.W/2, l.Grid.H/2

			img, err := l.DecodedTile(tx, ty, opentile.WithFormat(decoder.PixelFormatRGB))
			if err != nil {
				t.Fatalf("DecodedTile(%d,%d) on %s tiled level: %v", tx, ty, tc.comp, err)
			}
			if img.Width != l.TileSize.W || img.Height != l.TileSize.H {
				t.Fatalf("decoded %dx%d, want tile %dx%d", img.Width, img.Height, l.TileSize.W, l.TileSize.H)
			}
			if img.Format != decoder.PixelFormatRGB {
				t.Fatalf("format %v, want RGB", img.Format)
			}
			mn, mx := byte(255), byte(0)
			for _, b := range img.Pix {
				if b < mn {
					mn = b
				}
				if b > mx {
					mx = b
				}
			}
			if mn == mx {
				t.Fatalf("decoded to a constant image (%d) — garbage?", mn)
			}
		})
	}
}
