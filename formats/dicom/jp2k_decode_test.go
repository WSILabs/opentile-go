package dicom_test

import (
	"os"
	"path/filepath"
	"testing"

	opentile "github.com/wsilabs/opentile-go"
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
	raw, err := s.RawTile(0, 0, 0)
	if err != nil {
		t.Fatalf("RawTile: %v", err)
	}
	if len(raw) < 4 || raw[0] != 0xFF || raw[1] != 0x4F {
		t.Fatalf("RawTile not a J2K codestream: % x", raw[:min(4, len(raw))])
	}

	// Decode routes to the OpenJPEG decoder and produces a tile-sized image.
	img, err := s.DecodedTile(0, 0, 0)
	if err != nil {
		t.Fatalf("DecodedTile: %v", err)
	}
	if img.Width != lvls[0].TileSize.W || img.Height != lvls[0].TileSize.H {
		t.Fatalf("decoded %dx%d, want tile %dx%d", img.Width, img.Height, lvls[0].TileSize.W, lvls[0].TileSize.H)
	}
}
