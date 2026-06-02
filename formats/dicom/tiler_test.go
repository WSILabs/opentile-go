package dicom

import (
	"bytes"
	"testing"

	opentile "github.com/wsilabs/opentile-go"
	idicom "github.com/wsilabs/opentile-go/internal/dicom"
)

func TestTilerRawTile(t *testing.T) {
	// One VOLUME level, 2x1 grid, TILED_FULL, two synthetic JPEG-ish frames.
	frameA := []byte{0xFF, 0xD8, 0xAA}
	frameB := []byte{0xFF, 0xD8, 0xBB}
	blob := append([]byte("HDR"), buildEncapsulated([][]byte{frameA, frameB})...)

	vol := idicom.Instance{
		SOPClassUID: idicom.WSMStorageUID, SeriesUID: "S1",
		ImageType: []string{"DERIVED", "PRIMARY", "VOLUME", "NONE"},
		TotalCols: 512, TotalRows: 256, TileCols: 256, TileRows: 256,
		NumFrames: 2, DimOrg: "TILED_FULL",
	}
	tiler, err := openSeriesFromInstances([]idicom.Instance{vol},
		func(path string) ([]byte, func() error, error) { return blob, func() error { return nil }, nil })
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer tiler.Close()

	if tiler.Format() != opentile.FormatDICOM {
		t.Errorf("Format = %v", tiler.Format())
	}
	lvl, _ := tiler.Level(0, 0)
	if lvl.Compression != opentile.CompressionJPEG {
		t.Errorf("Compression = %v, want JPEG", lvl.Compression)
	}
	if lvl.Grid != (opentile.Size{W: 2, H: 1}) {
		t.Errorf("Grid = %+v, want 2x1", lvl.Grid)
	}
	got, err := tiler.ImageRawTile(0, 0, 1, 0)
	if err != nil {
		t.Fatalf("RawTile: %v", err)
	}
	if !bytes.Equal(got, frameB) {
		t.Errorf("tile(1,0) = % x, want % x", got, frameB)
	}
}
