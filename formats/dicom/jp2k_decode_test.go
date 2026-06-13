package dicom_test

import (
	"os"
	"path/filepath"
	"testing"

	opentile "github.com/wsilabs/opentile-go"
	"github.com/wsilabs/opentile-go/decoder"
	_ "github.com/wsilabs/opentile-go/decoder/all"
	_ "github.com/wsilabs/opentile-go/formats/all"
)

// jp2kSeries returns the JP2K DICOM fixture: 3DHISTECH-1 VOLUME levels
// transcoded to JPEG 2000 transfer syntax (1.2.840.10008.1.2.4.90) via
// `gdcmconv --j2k`. Regenerate with:
//
//	for n in 000010 000011 000012 000013 000014; do
//	  gdcmconv --j2k sample_files/dicom/3DHISTECH-1/$n.dcm \
//	    sample_files/dicom/3DHISTECH-JP2K/$n.dcm
//	done
func jp2kSeries(t *testing.T) string {
	t.Helper()
	base := os.Getenv("OPENTILE_TESTDIR")
	if base == "" {
		t.Skip("OPENTILE_TESTDIR not set")
	}
	dir := filepath.Join(base, "dicom", "3DHISTECH-JP2K")
	if _, err := os.Stat(dir); err != nil {
		t.Skipf("fixture absent: %v", err)
	}
	return dir
}

func TestDICOMJP2KDecode(t *testing.T) {
	s, err := opentile.OpenFile(jp2kSeries(t))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer s.Close()

	lvls := s.Levels()
	if len(lvls) == 0 {
		t.Fatal("no levels")
	}
	// The level must report JP2K so DecodedTile dispatches to OpenJPEG.
	if lvls[0].Compression != opentile.CompressionJP2K {
		t.Fatalf("L0 compression = %v, want CompressionJP2K", lvls[0].Compression)
	}

	// Extraction is codec-agnostic: RawTile is the raw J2K codestream.
	raw, err := mustLevel(t, s, 0).Tile(0, 0)
	if err != nil {
		t.Fatalf("RawTile: %v", err)
	}
	if len(raw) < 4 || raw[0] != 0xFF || raw[1] != 0x4F {
		t.Fatalf("RawTile not a J2K codestream: % x", raw[:min(4, len(raw))])
	}

	// Decode routes to the OpenJPEG decoder and produces a tile-sized image.
	img, err := mustLevel(t, s, 0).DecodedTile(0, 0)
	if err != nil {
		t.Fatalf("DecodedTile: %v", err)
	}
	if img.Width != lvls[0].TileSize.W || img.Height != lvls[0].TileSize.H {
		t.Fatalf("decoded %dx%d, want tile %dx%d", img.Width, img.Height, lvls[0].TileSize.W, lvls[0].TileSize.H)
	}
}

// TestDICOMJP2KColorParity confirms color/photometric correctness without a
// Python oracle: the JP2K fixture is a *lossless* gdcmconv transcode of the
// original JPEG-baseline 3DHISTECH-1, so decoding the same-dimension tile
// from both must match within a tile (any colour-space mishandling would
// show large diffs). Skips unless both fixtures are present.
func TestDICOMJP2KColorParity(t *testing.T) {
	base := os.Getenv("OPENTILE_TESTDIR")
	if base == "" {
		t.Skip("OPENTILE_TESTDIR not set")
	}
	const w, h = 1792, 1888
	dec := func(name string) *decoder.Image {
		dir := filepath.Join(base, "dicom", name)
		if _, err := os.Stat(dir); err != nil {
			t.Skipf("fixture %s absent", name)
		}
		s, err := opentile.OpenFile(dir)
		if err != nil {
			t.Fatalf("open %s: %v", name, err)
		}
		t.Cleanup(func() { s.Close() })
		for _, lv := range s.Levels() {
			if lv.Size.W == w && lv.Size.H == h {
				img, err := lv.DecodedTile(0, 0)
				if err != nil {
					t.Fatalf("decode %s: %v", name, err)
				}
				return img
			}
		}
		t.Fatalf("%s has no %dx%d level", name, w, h)
		return nil
	}
	j2k := dec("3DHISTECH-JP2K")
	jpg := dec("3DHISTECH-1")
	if len(j2k.Pix) != len(jpg.Pix) {
		t.Fatalf("pix len %d vs %d", len(j2k.Pix), len(jpg.Pix))
	}
	max := 0
	for i := range j2k.Pix {
		d := int(j2k.Pix[i]) - int(jpg.Pix[i])
		if d < 0 {
			d = -d
		}
		if d > max {
			max = d
		}
	}
	if max > 3 { // lossless transcode → within JPEG↔RGB↔J2K rounding
		t.Errorf("JP2K vs original-JPEG max channel diff = %d, want <= 3", max)
	}
}
