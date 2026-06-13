package ife

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	opentile "github.com/wsilabs/opentile-go"
)

// TestCervixEndToEnd exercises the IFE Tiler against the real
// cervix_2x_jpeg.iris fixture. Skipped when OPENTILE_TESTDIR is unset
// (matches the rest of the integration suite). Verifies:
//
//   - Magic detection + Open round-trip via the Factory.
//   - Level count and per-level dimensions match the T3 gate findings.
//   - ImageRawTile(0, 0, 0, 0) returns a JPEG bytestream (SOI marker FFD8).
//   - Out-of-bounds (col, row) returns ErrTileOutOfBounds.
//   - Non-zero Z/C/T via TileAt returns ErrDimensionUnavailable.
//   - ImageTileReader streams the same bytes ImageRawTile returns.
func TestCervixEndToEnd(t *testing.T) {
	dir := os.Getenv("OPENTILE_TESTDIR")
	if dir == "" {
		t.Skip("OPENTILE_TESTDIR not set")
	}
	path := filepath.Join(dir, "ife", "cervix_2x_jpeg.iris")
	if _, err := os.Stat(path); err != nil {
		t.Skipf("%s not present", path)
	}

	f := New()
	file, err := os.Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer file.Close()
	stat, _ := file.Stat()

	if !f.SupportsRaw(file, stat.Size()) {
		t.Fatal("SupportsRaw returned false on cervix")
	}
	tiler, err := f.OpenRaw(file, stat.Size(), &opentile.Config{})
	if err != nil {
		t.Fatalf("OpenRaw: %v", err)
	}
	defer tiler.Close()

	if got := tiler.Format(); got != opentile.FormatIFE {
		t.Errorf("Format = %v, want %v", got, opentile.FormatIFE)
	}

	imgs := tiler.Pyramids()
	if len(imgs) != 1 {
		t.Fatalf("Pyramids count = %d, want 1", len(imgs))
	}
	levels := imgs[0].Levels
	if len(levels) != 9 {
		t.Errorf("level count = %d, want 9", len(levels))
	}

	// Native (L0) and coarsest (L8) dims per T3 gate.
	wantSizes := []opentile.Size{
		{W: 126976, H: 88576},
		{W: 63488, H: 44288},
		{W: 31744, H: 22272},
		{W: 15872, H: 11264},
		{W: 7936, H: 5632},
		{W: 4096, H: 2816},
		{W: 2048, H: 1536},
		{W: 1024, H: 768},
		{W: 512, H: 512},
	}
	for i, want := range wantSizes {
		if got := levels[i].Size; got != want {
			t.Errorf("L%d Size = %v, want %v", i, got, want)
		}
	}

	// All levels share 256×256 tiles, JPEG-compressed.
	for i, lvl := range levels {
		if got := lvl.TileSize; got != (opentile.Size{W: 256, H: 256}) {
			t.Errorf("L%d TileSize = %v, want 256×256", i, got)
		}
		if got := lvl.Compression; got != opentile.CompressionJPEG {
			t.Errorf("L%d Compression = %v, want jpeg", i, got)
		}
	}

	// ImageRawTile(0, 0, 0, 0) returns a JPEG SOI marker.
	a, err := tiler.ImageRawTile(0, 0, 0, 0)
	if err != nil {
		t.Fatalf("L0 ImageRawTile(0,0,0,0): %v", err)
	}
	if len(a) < 2 || a[0] != 0xFF || a[1] != 0xD8 {
		t.Errorf("L0 tile: missing JPEG SOI marker; first 4 bytes = % x", a[:min(4, len(a))])
	}

	// TileAt with Z/C/T non-zero → ErrDimensionUnavailable via levelImpl.
	lvl0 := tiler.levelImpls[0]

	b, err := lvl0.TileAt(opentile.TileCoord{X: 0, Y: 0})
	if err != nil {
		t.Fatalf("L0 TileAt: %v", err)
	}
	if !bytes.Equal(a, b) {
		t.Errorf("ImageRawTile and TileAt({X:0,Y:0}) bytes differ (lengths %d/%d)", len(a), len(b))
	}

	// Out-of-bounds.
	_, err = tiler.ImageRawTile(0, 0, -1, 0)
	if !errors.Is(err, opentile.ErrTileOutOfBounds) {
		t.Errorf("ImageRawTile(0,0,-1,0): got %v, want ErrTileOutOfBounds", err)
	}
	_, err = tiler.ImageRawTile(0, 0, 496, 0)
	if !errors.Is(err, opentile.ErrTileOutOfBounds) {
		t.Errorf("ImageRawTile(0,0,496,0): got %v, want ErrTileOutOfBounds", err)
	}

	// Non-zero Z/C/T → ErrDimensionUnavailable (2D-only format).
	for _, coord := range []opentile.TileCoord{
		{X: 0, Y: 0, Z: 1},
		{X: 0, Y: 0, C: 1},
		{X: 0, Y: 0, T: 1},
	} {
		_, err := lvl0.TileAt(coord)
		if !errors.Is(err, opentile.ErrDimensionUnavailable) {
			t.Errorf("TileAt(%+v): got %v, want ErrDimensionUnavailable", coord, err)
		}
	}

	// ImageTileReader returns same bytes.
	rc, err := tiler.ImageTileReader(0, 0, 0, 0)
	if err != nil {
		t.Fatalf("ImageTileReader(0,0,0,0): %v", err)
	}
	defer rc.Close()
	got, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if !bytes.Equal(got, a) {
		t.Errorf("ImageTileReader bytes differ from ImageRawTile (lengths %d/%d)", len(got), len(a))
	}

	// Metadata extraction (since v0.8 metadata closeout).
	cm := tiler.Metadata()
	if cm.Magnification == 0 {
		t.Error("Metadata.Magnification = 0; cervix carries 0.625")
	}
	if !strings.HasPrefix(cm.Writer, "iris/") {
		t.Errorf("Writer (v0.20): got %q, want prefix 'iris/' (IFE writer is the Iris codec; source scanner attribution lives in ImageDescription)", cm.Writer)
	}
	icc := tiler.ICCProfile()
	if len(icc) != 6064 {
		t.Errorf("ICCProfile size = %d, want 6064", len(icc))
	}
	assoc := tiler.AssociatedImages()
	if len(assoc) != 1 {
		t.Fatalf("Associated count = %d, want 1 (thumbnail)", len(assoc))
	}
	if assoc[0].Type() != "thumbnail" {
		t.Errorf("Associated[0].Type = %q, want thumbnail", assoc[0].Type())
	}
	if got := assoc[0].Size(); got.W != 1920 || got.H != 1337 {
		t.Errorf("Associated[0].Size = %v, want 1920×1337", got)
	}
	if assoc[0].Compression() != opentile.CompressionJPEG {
		t.Errorf("Associated[0].Compression = %v, want jpeg", assoc[0].Compression())
	}
	thumbBytes, err := assoc[0].Bytes()
	if err != nil {
		t.Fatalf("thumbnail Bytes: %v", err)
	}
	if len(thumbBytes) < 2 || thumbBytes[0] != 0xFF || thumbBytes[1] != 0xD8 {
		t.Errorf("thumbnail missing JPEG SOI; first 4 = % x", thumbBytes[:min(4, len(thumbBytes))])
	}

	// IFE-specific metadata via MetadataOf.
	ifeMD, ok := MetadataOf(tiler)
	if !ok {
		t.Fatal("MetadataOf returned !ok on cervix")
	}
	if ifeMD.MicronsPerPixel == 0 {
		t.Error("MicronsPerPixel = 0; cervix header carries 16.835")
	}
	if ifeMD.AttributesFormat != AttributesFormatFreeText {
		t.Errorf("AttributesFormat = %v, want free-text", ifeMD.AttributesFormat)
	}
	if got := len(ifeMD.Attributes); got != 24 {
		t.Errorf("Attributes count = %d, want 24", got)
	}
	// Spot-check a few well-known cervix attributes.
	if v := ifeMD.Attributes["aperio.AppMag"]; v != "40" {
		t.Errorf("aperio.AppMag = %q, want 40", v)
	}
	if v := ifeMD.Attributes["aperio.MPP"]; v != "0.262968" {
		t.Errorf("aperio.MPP = %q", v)
	}
	if v := ifeMD.Attributes["aperio.ScannerType"]; v != "GT450" {
		t.Errorf("aperio.ScannerType = %q", v)
	}
	if ifeMD.CodecMajor == 0 {
		t.Errorf("CodecMajor = %d (zero); cervix encoder is 2025.x", ifeMD.CodecMajor)
	}

	// v0.17 cross-format assertions on cervix's real metadata.
	cm = tiler.Metadata()
	// MPP: cervix header reports 16.835 µm/px (a 2× downsample of
	// a 0.263 µm/px GT450 scan). f32→f64 widening introduces a tiny
	// epsilon (16.835 isn't exact in IEEE-754 binary); assert
	// directionally rather than against a specific bit pattern.
	if cm.MicronsPerPixelX < 16.83 || cm.MicronsPerPixelX > 16.84 {
		t.Errorf("cross.MicronsPerPixelX = %v, want ≈ 16.835", cm.MicronsPerPixelX)
	}
	if cm.MicronsPerPixelX != cm.MicronsPerPixelY {
		t.Errorf("cross.MicronsPerPixelX (%v) != Y (%v); IFE pixels are isotropic", cm.MicronsPerPixelX, cm.MicronsPerPixelY)
	}
	if cm.MicronsPerPixel == 0 {
		t.Error("cross.MicronsPerPixel = 0; SetMPPSymmetric should populate it on isotropic pixels")
	}
	// Vendor passthrough: every IFE attribute available under "iris.".
	if got := cm.Properties["iris.aperio.AppMag"]; got != "40" {
		t.Errorf("Properties[iris.aperio.AppMag] = %q, want %q", got, "40")
	}
	if got := cm.Properties["iris.aperio.ScannerType"]; got != "GT450" {
		t.Errorf("Properties[iris.aperio.ScannerType] = %q, want %q", got, "GT450")
	}
	// tiff.ImageDescription → cross.ImageDescription (the cervix
	// fixture preserves the source GT450's banner verbatim).
	if cm.ImageDescription == "" {
		t.Error("cross.ImageDescription empty; cervix carries tiff.ImageDescription attribute")
	}
	// Negative: cervix has no user-name field — canonical
	// PropertyUserName must be absent.
	if _, present := cm.Properties[opentile.PropertyUserName]; present {
		t.Errorf("Properties[%s]: present but cervix has no user-name attribute", opentile.PropertyUserName)
	}

	// Tiles iterator on the coarsest level (a 2×2 grid → 4 tiles).
	count := 0
	for _, res := range tiler.ImageRangeTiles(context.Background(), 0, len(levels)-1) {
		if res.Err != nil {
			t.Errorf("ImageRangeTiles err on coarsest: %v", res.Err)
			break
		}
		if len(res.Bytes) < 2 || res.Bytes[0] != 0xFF || res.Bytes[1] != 0xD8 {
			t.Errorf("Tiles: tile missing JPEG SOI")
			break
		}
		count++
	}
	if count != 4 {
		t.Errorf("Tiles count = %d, want 4 (2×2 grid)", count)
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
