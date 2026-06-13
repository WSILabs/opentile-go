package bif_test

import (
	"bytes"
	"context"
	"errors"
	"image/jpeg"
	"io"
	"os"
	"path/filepath"
	"testing"

	opentile "github.com/wsilabs/opentile-go"
	"github.com/wsilabs/opentile-go/formats/bif"
)

// TestBIFAccessors exercises Image / Tiler accessors on real BIF
// fixtures. Skipped without OPENTILE_TESTDIR — keeps CI working
// without integration data.
//
// Both fixtures must Open cleanly, expose at least one Image with
// non-empty Levels, return decodable JPEG bytes from RawTile(0,0,0),
// expose the expected associated-image types for their generation,
// and surface MetadataOf with the right Generation.
func TestBIFAccessors(t *testing.T) {
	dir := os.Getenv("OPENTILE_TESTDIR")
	if dir == "" {
		t.Skip("OPENTILE_TESTDIR not set")
	}

	cases := []struct {
		name        string
		filename    string
		wantGen     string
		wantTypes   map[string]bool
		wantScanRes float64
		hasICC      bool
	}{
		{
			name:        "spec-compliant Ventana-1",
			filename:    "Ventana-1.bif",
			wantGen:     "spec-compliant",
			wantTypes:   map[string]bool{"overview": true, "probability": true},
			wantScanRes: 0.25,
			hasICC:      true,
		},
		{
			name:        "legacy iScan OS-1",
			filename:    "OS-1.bif",
			wantGen:     "legacy-iscan",
			wantTypes:   map[string]bool{"overview": true, "thumbnail": true},
			wantScanRes: 0.2325,
			hasICC:      false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			slide := filepath.Join(dir, "bif", tc.filename)
			if _, err := os.Stat(slide); err != nil {
				t.Skipf("%s not present under OPENTILE_TESTDIR/bif/", tc.filename)
			}
			tiler, err := opentile.OpenFile(slide)
			if err != nil {
				t.Fatalf("OpenFile: %v", err)
			}
			defer tiler.Close()

			if got := tiler.Format(); got != opentile.FormatBIF {
				t.Errorf("Format: got %q, want %q", got, opentile.FormatBIF)
			}
			if got := len(tiler.Levels()); got == 0 {
				t.Fatal("Levels: empty slice")
			}
			if _, err := tiler.Level(0); err != nil {
				t.Errorf("Level(0): %v", err)
			}
			if _, err := tiler.Level(-1); !errors.Is(err, opentile.ErrLevelOutOfRange) {
				t.Errorf("Level(-1): want ErrLevelOutOfRange, got %v", err)
			}

			// Pyramid-level (value-type fields)
			imgs := tiler.Pyramids()
			if len(imgs) != 1 {
				t.Fatalf("Pyramids: got %d, want 1 (BIF is single-image)", len(imgs))
			}
			img := imgs[0]
			if img.Index != 0 {
				t.Errorf("Image.Index: got %d, want 0", img.Index)
			}
			if len(img.Levels) == 0 {
				t.Fatal("Image.Levels: empty")
			}

			// RawTile(0, 0, 0) decodes to a tile-sized JPEG.
			tile, err := tiler.RawTile(0, 0, 0)
			if err != nil {
				t.Fatalf("RawTile(0,0,0): %v", err)
			}
			decoded, err := jpeg.Decode(bytes.NewReader(tile))
			if err != nil {
				t.Fatalf("jpeg.Decode: %v", err)
			}
			db := decoded.Bounds()
			ts := img.Levels[0].TileSize
			if db.Dx() != ts.W || db.Dy() != ts.H {
				t.Errorf("decoded tile dims: got %dx%d, want %dx%d", db.Dx(), db.Dy(), ts.W, ts.H)
			}

			// TileReader matches RawTile.
			rc, err := tiler.TileReader(0, 0, 0)
			if err != nil {
				t.Fatalf("TileReader: %v", err)
			}
			rcBytes, _ := io.ReadAll(rc)
			rc.Close()
			if !bytes.Equal(rcBytes, tile) {
				t.Errorf("TileReader bytes != RawTile bytes (lengths %d vs %d)", len(rcBytes), len(tile))
			}

			// RangeTiles iterator yields at least 1 entry.
			seen := 0
			for range tiler.RangeTiles(context.Background(), 0) {
				seen++
				if seen >= 4 { // sample only — full iteration is 8000+ tiles
					break
				}
			}
			if seen == 0 {
				t.Error("RangeTiles iterator yielded zero entries")
			}

			// Associated types match expectation.
			ai := tiler.Associated()
			gotTypes := make(map[string]bool)
			for _, a := range ai {
				gotTypes[a.Type()] = true
				if _, err := a.Bytes(); err != nil {
					t.Errorf("Associated[%q].Bytes: %v", a.Type(), err)
				}
			}
			for k := range tc.wantTypes {
				if !gotTypes[k] {
					t.Errorf("missing associated type %q (got %v)", k, gotTypes)
				}
			}

			// Common metadata.
			md := tiler.Metadata()
			if md.ScannerManufacturer != "Roche" {
				t.Errorf("ScannerManufacturer: got %q, want %q", md.ScannerManufacturer, "Roche")
			}

			// BIF-specific metadata.
			bm, ok := bif.MetadataOf(tiler)
			if !ok {
				t.Fatal("bif.MetadataOf: ok=false on a BIF tiler")
			}
			if bm.Generation != tc.wantGen {
				t.Errorf("Generation: got %q, want %q", bm.Generation, tc.wantGen)
			}
			if bm.ScanRes != tc.wantScanRes {
				t.Errorf("ScanRes: got %v, want %v", bm.ScanRes, tc.wantScanRes)
			}

			// ICC profile presence matches the fixture.
			icc := tiler.ICCProfile()
			if tc.hasICC {
				if len(icc) < 40 || string(icc[36:40]) != "acsp" {
					t.Errorf("ICC profile: missing or invalid magic; len=%d", len(icc))
				}
			} else {
				if icc != nil {
					t.Errorf("ICC profile: got %d bytes, want nil", len(icc))
				}
			}
		})
	}
}
