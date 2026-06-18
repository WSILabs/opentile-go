package bif

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/wsilabs/opentile-go/internal/bifxml"
)

// Ground truth: Ventana-1 L0 stitched dimensions. DP exactness bar — no tolerance.
//
// Confirmed by running bio-formats (showinf -nopix) on the real Ventana-1.bif as
// a black-box dimension oracle (NOT by reading its GPL source): Series #0 reports
// Width=23432 Height=21504. This is the COMPACTED content extent, NOT the padded
// raw-frame IFD extent (24×21 tiles = 24576×21504, which is what opentile-go
// reported before #60). The lower pyramid levels are exact /2 downsamples of
// 23432×21504 (11716, 5858, 2929, 1464, ...), confirming 23432 — not the
// tile-rounded 23552 — is the canonical stitched L0 width.
//
// Arithmetic (see Roche BIF whitepaper §"Image stitching process", page 15):
//   width  = 23 content columns × 1024 − 120 cumulative horizontal overlap
//            (= 5 LEFT joints × OverlapX=24 per row, uniform across all 21 rows)
//          = 23552 − 120 = 23432
//   height = 21 rows × 1024 = 21504  (DP 200 has no vertical overlap; whitepaper
//            page 15: "do not contain vertical tile overlap")
// The 24th IFD grid column (504 tile offsets vs 483 real frames) is phantom
// raw-frame padding (#60 core) and contributes no content.
const (
	ventana1StitchedW = 23432
	ventana1StitchedH = 21504
	ventana1Cols      = 24 // image grid incl. phantom pad column
	ventana1Rows      = 21
	ventana1TileW     = 1024
	ventana1TileH     = 1024
)

// TestVentana1DPExactDimensions is the make-or-break #60 correctness gate. It
// feeds the committed golden EncodeInfo XML (captured once from the real fixture)
// to the stitch engine and asserts the stitched extent matches bio-formats
// exactly. CI-safe: reads only the committed golden XML, no slide needed.
func TestVentana1DPExactDimensions(t *testing.T) {
	xmp, err := os.ReadFile(filepath.Join("testdata", "ventana1_encodeinfo.xml"))
	if err != nil {
		t.Fatalf("read golden EncodeInfo: %v", err)
	}
	ei, err := bifxml.ParseEncodeInfo(xmp)
	if err != nil {
		t.Fatalf("ParseEncodeInfo: %v", err)
	}
	l := BuildLayout(StitchInput{
		Cols: ventana1Cols, Rows: ventana1Rows,
		TileW: ventana1TileW, TileH: ventana1TileH,
		EncodeInfo: ei, Generation: GenerationSpecCompliant,
	})
	if l.Width != ventana1StitchedW || l.Height != ventana1StitchedH {
		t.Fatalf("stitched dims = %dx%d, want %dx%d (bio-formats ground truth)",
			l.Width, l.Height, ventana1StitchedW, ventana1StitchedH)
	}
}
