//go:build cgo && !nocgo

package tests_test

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"

	opentile "github.com/wsilabs/opentile-go"
	"github.com/wsilabs/opentile-go/decoder"
	_ "github.com/wsilabs/opentile-go/decoder/all"
	_ "github.com/wsilabs/opentile-go/formats/all"
)

// TestNDPIDecodedTilePathParity walks each NDPI fixture and asserts
// that DecodedTile (v0.27 fast path for stripped levels, slow-path
// fallback for oneframe) returns the same pixels as RawTile + decode
// on a sampled scatter of interior tiles per level.
//
// This is the cross-fixture regression cover for the v0.27 fast path
// described in docs/superpowers/specs/2026-05-28-opentile-go-v27-...
// .md §5.2. Complements the in-package TestNDPIFastPathPixelParity
// which only covers CMU-1.ndpi.
func TestNDPIDecodedTilePathParity(t *testing.T) {
	dir := os.Getenv("OPENTILE_TESTDIR")
	if dir == "" {
		t.Skip("OPENTILE_TESTDIR not set")
	}

	for _, name := range []string{"CMU-1.ndpi", "OS-2.ndpi", "Hamamatsu-1.ndpi"} {
		t.Run(name, func(t *testing.T) {
			path, ok := findNDPIFixture(dir, name)
			if !ok {
				t.Skipf("fixture %s missing", name)
			}
			slide, err := opentile.OpenFile(path)
			if err != nil {
				t.Fatalf("OpenFile: %v", err)
			}
			defer slide.Close()

			for lvlIdx, lvl := range slide.Levels() {
				// Sample 4 interior positions per level. Cap at
				// grid-2 on both axes to stay safely interior
				// (last row/col may be edge tiles).
				gw, gh := lvl.Grid.W, lvl.Grid.H
				if gw < 3 || gh < 3 {
					continue
				}
				positions := [][2]int{
					{gw / 8, gh / 8},
					{gw / 2, gh / 2},
					{3 * gw / 4, gh / 4},
					{gw - 2, gh - 2},
				}
				for _, p := range positions {
					tx, ty := p[0], p[1]
					if tx >= gw-1 || ty >= gh-1 {
						continue
					}
					fast, err := lvl.DecodedTile(tx, ty)
					if err != nil {
						t.Fatalf("L%d (%d,%d) DecodedTile: %v", lvlIdx, tx, ty, err)
					}
					compressed, err := lvl.Tile(tx, ty)
					if err != nil {
						t.Fatalf("L%d (%d,%d) RawTile: %v", lvlIdx, tx, ty, err)
					}
					slow, err := decodeRefRGB(compressed)
					if err != nil {
						t.Fatalf("L%d (%d,%d) decode ref: %v", lvlIdx, tx, ty, err)
					}
					if fast.Width != slow.Width || fast.Height != slow.Height {
						t.Errorf("L%d (%d,%d): size fast=%dx%d slow=%dx%d",
							lvlIdx, tx, ty, fast.Width, fast.Height,
							slow.Width, slow.Height)
						continue
					}
					if !bytes.Equal(fast.Pix, slow.Pix) {
						t.Errorf("L%d (%d,%d): DecodedTile drift vs RawTile+Decode",
							lvlIdx, tx, ty)
					}
				}
			}
		})
	}
}

func findNDPIFixture(testdir, name string) (string, bool) {
	for _, sub := range []string{"ndpi", ""} {
		p := filepath.Join(testdir, sub, name)
		if _, err := os.Stat(p); err == nil {
			return p, true
		}
	}
	return "", false
}

func decodeRefRGB(b []byte) (*decoder.Image, error) {
	tag := opentile.CompressionToTIFFTag(opentile.CompressionJPEG)
	fac, ok := decoder.GetByCompressionTag(tag)
	if !ok {
		return nil, errors.New("no JPEG decoder registered")
	}
	d := fac.New()
	defer d.Close()
	return d.Decode(b, decoder.DecodeOptions{Format: decoder.PixelFormatRGB})
}
