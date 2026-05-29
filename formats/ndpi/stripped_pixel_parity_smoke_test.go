//go:build cgo && !nocgo

package ndpi_test

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	opentile "github.com/wsilabs/opentile-go"
	"github.com/wsilabs/opentile-go/decoder"
	_ "github.com/wsilabs/opentile-go/decoder/all"
	_ "github.com/wsilabs/opentile-go/formats/all"
)

// TestNDPIDecodeBlitParityFoundational proves the v0.27 design
// assumption: decode-then-blit produces the same pixels as the
// current crop-then-decode path on real NDPI fixtures. Run this
// FIRST; if it fails, v0.27 is infeasible.
//
// Since strippedImage internals (getFrame, frameSizeForTile,
// framePosition) are unexported, this test compares two PUBLIC paths
// that today produce the same pixels by construction: RawTile → decode
// vs DecodedTile. Its real job is verifying the fixture is readable
// and the test setup is sound. The actual decode-then-blit assertion
// happens in Task 3.4 once the fast path is wired up.
//
// Skipped if OPENTILE_TESTDIR is not set or the fixture is missing.
func TestNDPIDecodeBlitParityFoundational(t *testing.T) {
	dir := os.Getenv("OPENTILE_TESTDIR")
	if dir == "" {
		t.Skip("OPENTILE_TESTDIR not set")
	}
	path := filepath.Join(dir, "ndpi", "CMU-1.ndpi")
	if _, err := os.Stat(path); err != nil {
		t.Skipf("fixture %s missing: %v", path, err)
	}
	slide, err := opentile.OpenFile(path)
	if err != nil {
		t.Fatalf("OpenFile: %v", err)
	}
	defer slide.Close()

	lvls := slide.Levels()
	if len(lvls) == 0 {
		t.Fatal("no levels")
	}
	l0 := lvls[0]

	// Sample interior tiles relative to the actual L0 grid. NDPI tile
	// size for CMU-1 is 512×512; L0 Grid is 100×75. Last row (ty=74)
	// is an edge tile (image is 38144 px = 74.5 tile heights), so cap
	// ty at grid.H-2 to stay safely interior.
	type tilePos struct{ tx, ty int }
	gw, gh := l0.Grid.W, l0.Grid.H
	cases := []tilePos{
		{gw / 8, gh / 8},     // ~(12, 9)
		{gw / 2, gh / 2},     // ~(50, 37)
		{3 * gw / 4, gh / 4}, // ~(75, 18)
		{gw - 2, gh - 2},     // ~(98, 73) — safely interior
	}

	dec, err := decodeFromCompression(l0.Compression)
	if err != nil {
		t.Fatalf("decoder for %s: %v", l0.Compression, err)
	}
	defer dec.Close()

	for _, c := range cases {
		// Path A: the current code path. RawTile → decode small JPEG.
		compressed, err := slide.RawTile(0, c.tx, c.ty)
		if err != nil {
			t.Fatalf("RawTile(%d,%d): %v", c.tx, c.ty, err)
		}
		imgA, err := dec.Decode(compressed, decoder.DecodeOptions{
			Format: decoder.PixelFormatRGB,
		})
		if err != nil {
			t.Fatalf("decode small tile (%d,%d): %v", c.tx, c.ty, err)
		}

		// Path B: emulate the fast path. There is no public way to
		// reach the assembled frame, so use DecodedTile (which today
		// internally does RawTile+Decode — the SAME pixels as Path A
		// by construction). If Path B != Path A we know the test
		// setup is wrong before we even ship the fast path.
		imgB, err := slide.DecodedTile(0, c.tx, c.ty)
		if err != nil {
			t.Fatalf("DecodedTile(%d,%d): %v", c.tx, c.ty, err)
		}

		if imgA.Width != imgB.Width || imgA.Height != imgB.Height {
			t.Fatalf("tile (%d,%d): size A=%dx%d B=%dx%d",
				c.tx, c.ty, imgA.Width, imgA.Height,
				imgB.Width, imgB.Height)
		}
		if !bytes.Equal(imgA.Pix, imgB.Pix) {
			t.Fatalf("tile (%d,%d): pixel mismatch (Path A vs Path B); "+
				"if v0.27 is to ship, decode-then-blit MUST match crop-then-decode",
				c.tx, c.ty)
		}
	}
}

// decodeFromCompression returns a fresh decoder for the given
// compression tag. Helper for the smoke test only.
func decodeFromCompression(c opentile.Compression) (decoder.Decoder, error) {
	tag := opentile.CompressionToTIFFTag(c)
	fac, ok := decoder.GetByCompressionTag(tag)
	if !ok {
		return nil, &noDecoderErr{c: c}
	}
	return fac.New(), nil
}

type noDecoderErr struct{ c opentile.Compression }

func (e *noDecoderErr) Error() string {
	return "no decoder registered for " + e.c.String()
}
