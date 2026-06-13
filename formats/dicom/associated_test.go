package dicom

import (
	"bytes"
	"testing"

	opentile "github.com/wsilabs/opentile-go"
	idicom "github.com/wsilabs/opentile-go/internal/dicom"
)

func TestAssociatedImage(t *testing.T) {
	frame := []byte{0xFF, 0xD8, 0x42}
	blob := append([]byte("X"), buildEncapsulated([][]byte{frame})...)
	label := idicom.Instance{
		SOPClassUID: idicom.WSMStorageUID, SeriesUID: "S1",
		ImageType: []string{"ORIGINAL", "PRIMARY", "LABEL", "NONE"},
		TotalCols: 608, TotalRows: 547, TileCols: 608, TileRows: 547,
		NumFrames: 1, DimOrg: "TILED_FULL", TransferSyntax: "1.2.840.10008.1.2.4.50",
	}
	vol := idicom.Instance{
		SOPClassUID: idicom.WSMStorageUID, SeriesUID: "S1",
		ImageType: []string{"DERIVED", "PRIMARY", "VOLUME", "NONE"},
		TotalCols: 512, TotalRows: 256, TileCols: 256, TileRows: 256, NumFrames: 1, DimOrg: "TILED_FULL",
	}
	openers := map[string][]byte{"label": blob, "vol": append([]byte("Y"), buildEncapsulated([][]byte{{0xFF, 0xD8}})...)}
	label.Path, vol.Path = "label", "vol"
	tiler, err := openSeriesFromInstances([]idicom.Instance{vol, label},
		func(p string) ([]byte, func() error, error) { return openers[p], func() error { return nil }, nil })
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer tiler.Close()
	as := tiler.AssociatedImages()
	if len(as) != 1 || as[0].Type() != "label" {
		t.Fatalf("associated = %+v", as)
	}
	if as[0].Compression() != opentile.CompressionJPEG {
		t.Errorf("label compression = %v", as[0].Compression())
	}
	b, err := as[0].Bytes()
	if err != nil || !bytes.Equal(b, frame) {
		t.Errorf("label bytes = % x (err %v), want % x", b, err, frame)
	}
}
