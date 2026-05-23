package bif

import (
	"bytes"
	"testing"

	"github.com/wsilabs/opentile-go/internal/tiff"
)

// Synthetic abbreviated tile scan. Same shape as
// internal/jpeg/insert_tables_test.go's frame: SOI + SOF0 + SOS +
// scan + EOI. The DQT/DHT segments are *missing* — they live in
// the shared JPEGTables and must be spliced in for the bytes to
// decode. opentile-go itself doesn't validate the JPEG (consumer's
// job) so structurally-valid bytes that pass the SOS-marker scan
// are sufficient for the splice contract.
var jpegTablesTestTileScan = []byte{
	0xFF, 0xD8, // SOI
	0xFF, 0xC0, 0x00, 0x08, 0x08, 0x00, 0x10, 0x00, 0x10, 0x03, // SOF0 stub
	0xFF, 0xDA, 0x00, 0x08, 0x03, 0x01, 0x00, 0x02, 0x11, 0x03, 0x11, // SOS stub
	0xDE, 0xAD, 0xBE, 0xEF, // entropy bytes
	0xFF, 0xD9, // EOI
}

// Synthetic JPEGTables: SOI + DQT + DHT + EOI. The signature byte
// 0x42 in the DQT lets tests confirm the splice happened (the byte
// shouldn't appear in the abbreviated scan but should appear in
// the spliced output).
var jpegTablesTestTables = []byte{
	0xFF, 0xD8, // SOI
	0xFF, 0xDB, 0x00, 0x03, 0x42, // DQT stub with signature byte 0x42
	0xFF, 0xC4, 0x00, 0x03, 0x10, // DHT stub
	0xFF, 0xD9, // EOI
}

// TestTileJPEGTablesSpliced: Tile() on a level whose IFD carries
// shared JPEGTables (tag 347) returns abbreviated-scan bytes with
// DQT/DHT spliced before SOS. No Adobe APP14 marker (BIF uses
// YCbCr, unlike SVS).
func TestTileJPEGTablesSpliced(t *testing.T) {
	data := buildBIFLikeBigTIFF(t, []iFDSpec{
		{xmp: []byte(`<iScan ScannerModel="VENTANA DP 200" ScanRes="0.25"/>`), description: "Label_Image"},
		{
			description:       "level=0 mag=40 quality=95",
			imageWidth:        16, imageLength: 16,
			tileWidth: 16, tileLength: 16,
			tileBytesOverride: jpegTablesTestTileScan,
			jpegTables:        jpegTablesTestTables,
		},
	})
	f, err := tiff.Open(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("tiff.Open: %v", err)
	}
	tiler, err := New().Open(f, nil)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	lvl, _ := tiler.Level(0)
	got, err := lvl.Tile(0, 0)
	if err != nil {
		t.Fatalf("Tile: %v", err)
	}

	// Splice happened iff DQT signature appears in the output.
	if !bytes.Contains(got, []byte{0xFF, 0xDB, 0x00, 0x03, 0x42}) {
		t.Errorf("spliced output missing DQT signature: %x", got)
	}
	if !bytes.Contains(got, []byte{0xFF, 0xC4, 0x00, 0x03, 0x10}) {
		t.Errorf("spliced output missing DHT signature: %x", got)
	}
	// DQT must precede SOS.
	dqtIdx := bytes.Index(got, []byte{0xFF, 0xDB})
	sosIdx := bytes.Index(got, []byte{0xFF, 0xDA})
	if dqtIdx < 0 || sosIdx < 0 || dqtIdx >= sosIdx {
		t.Errorf("ordering: dqt=%d sos=%d (want dqt < sos)", dqtIdx, sosIdx)
	}
	// No Adobe APP14 marker — BIF uses YCbCr, no colorspace fix.
	if bytes.Contains(got, []byte{0xFF, 0xEE}) {
		t.Errorf("BIF JPEGTables splice should NOT include Adobe APP14 marker, got %x", got)
	}
	// Spliced output is longer than the source scan by tables[2:-2] length.
	wantMinLen := len(jpegTablesTestTileScan) + len(jpegTablesTestTables) - 4
	if len(got) != wantMinLen {
		t.Errorf("spliced length: got %d, want %d", len(got), wantMinLen)
	}
}

// TestTileNoJPEGTablesPassthrough: when the IFD has no shared
// JPEGTables (older BIF variant), Tile() returns the abbreviated
// scan bytes verbatim — no splice attempted.
func TestTileNoJPEGTablesPassthrough(t *testing.T) {
	data := buildBIFLikeBigTIFF(t, []iFDSpec{
		{xmp: []byte(`<iScan ScannerModel="VENTANA DP 200" ScanRes="0.25"/>`), description: "Label_Image"},
		{
			description:       "level=0 mag=40 quality=95",
			imageWidth:        16, imageLength: 16,
			tileWidth: 16, tileLength: 16,
			tileBytesOverride: jpegTablesTestTileScan,
			// no jpegTables
		},
	})
	f, _ := tiff.Open(bytes.NewReader(data), int64(len(data)))
	tiler, err := New().Open(f, nil)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	lvl, _ := tiler.Level(0)
	got, err := lvl.Tile(0, 0)
	if err != nil {
		t.Fatalf("Tile: %v", err)
	}
	if !bytes.Equal(got, jpegTablesTestTileScan) {
		t.Errorf("no-tables passthrough: got %x, want %x", got, jpegTablesTestTileScan)
	}
}

// TestTileReaderJPEGTablesSpliced: TileReader returns the same
// spliced bytes as Tile() when JPEGTables are present.
func TestTileReaderJPEGTablesSpliced(t *testing.T) {
	data := buildBIFLikeBigTIFF(t, []iFDSpec{
		{xmp: []byte(`<iScan ScannerModel="VENTANA DP 200"/>`), description: "Label_Image"},
		{
			description:       "level=0 mag=40 quality=95",
			imageWidth:        16, imageLength: 16,
			tileWidth: 16, tileLength: 16,
			tileBytesOverride: jpegTablesTestTileScan,
			jpegTables:        jpegTablesTestTables,
		},
	})
	f, _ := tiff.Open(bytes.NewReader(data), int64(len(data)))
	tiler, _ := New().Open(f, nil)
	lvl, _ := tiler.Level(0)
	want, err := lvl.Tile(0, 0)
	if err != nil {
		t.Fatalf("Tile: %v", err)
	}
	rc, err := lvl.TileReader(0, 0)
	if err != nil {
		t.Fatalf("TileReader: %v", err)
	}
	defer rc.Close()
	got := make([]byte, len(want))
	rc.Read(got)
	if !bytes.Equal(got, want) {
		t.Error("TileReader bytes differ from Tile bytes when JPEGTables present")
	}
}
