//go:build cgo && !nocgo && !nohtj2k

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

// htj2kSeries: 3DHISTECH-1 VOLUME levels transcoded to HTJ2K Lossless
// (1.2.840.10008.1.2.4.201). Regenerate with /Volumes/Ext/tmp/make_htj2k_fixture.py
// (pydicom decode JPEG -> ojph_compress -reversible -colour_trans false ->
// pydicom re-encapsulate as .201, PhotometricInterpretation RGB).
func htj2kSeries(t *testing.T) string {
	t.Helper()
	base := os.Getenv("OPENTILE_TESTDIR")
	if base == "" {
		t.Skip("OPENTILE_TESTDIR not set")
	}
	dir := filepath.Join(base, "dicom", "3DHISTECH-HTJ2K")
	if _, err := os.Stat(dir); err != nil {
		t.Skipf("fixture absent: %v", err)
	}
	return dir
}

func TestDICOMHTJ2KDecode(t *testing.T) {
	s, err := opentile.OpenFile(htj2kSeries(t))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer s.Close()
	lvls := s.Levels()
	if len(lvls) == 0 {
		t.Fatal("no levels")
	}
	if lvls[0].Compression != opentile.CompressionHTJ2K {
		t.Fatalf("L0 compression = %v, want CompressionHTJ2K", lvls[0].Compression)
	}
	raw, err := s.RawTile(0, 0, 0)
	if err != nil {
		t.Fatalf("RawTile: %v", err)
	}
	if len(raw) < 4 || raw[0] != 0xFF || raw[1] != 0x4F {
		t.Fatalf("RawTile not a J2K/HT codestream: % x", raw[:min(4, len(raw))])
	}
	img, err := s.DecodedTile(0, 0, 0)
	if err != nil {
		t.Fatalf("DecodedTile: %v", err)
	}
	if img.Width != lvls[0].TileSize.W || img.Height != lvls[0].TileSize.H {
		t.Fatalf("decoded %dx%d, want tile %dx%d", img.Width, img.Height, lvls[0].TileSize.W, lvls[0].TileSize.H)
	}
}

// TestDICOMHTJ2KColorParity: the HTJ2K fixture is a lossless transcode of the
// original JPEG 3DHISTECH-1, so the same-dimension tile must match closely
// (any colour mishandling would show large diffs).
func TestDICOMHTJ2KColorParity(t *testing.T) {
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
				img, err := s.DecodedTile(lv.Index, 0, 0)
				if err != nil {
					t.Fatalf("decode %s: %v", name, err)
				}
				return img
			}
		}
		t.Fatalf("%s has no %dx%d level", name, w, h)
		return nil
	}
	ht := dec("3DHISTECH-HTJ2K")
	jpg := dec("3DHISTECH-1")
	if len(ht.Pix) != len(jpg.Pix) {
		t.Fatalf("pix len %d vs %d", len(ht.Pix), len(jpg.Pix))
	}
	max := 0
	for i := range ht.Pix {
		d := int(ht.Pix[i]) - int(jpg.Pix[i])
		if d < 0 {
			d = -d
		}
		if d > max {
			max = d
		}
	}
	if max > 5 { // lossless HTJ2K of pydicom's JPEG decode vs our JPEG decode
		t.Errorf("HTJ2K vs original-JPEG max channel diff = %d, want <= 5", max)
	}
}
