package opentile_test

import (
	"io"
	"os"
	"path/filepath"
	"testing"

	opentile "github.com/wsilabs/opentile-go"
	_ "github.com/wsilabs/opentile-go/decoder/all"
	_ "github.com/wsilabs/opentile-go/formats/all"
)

func openStripSample(t *testing.T, rel string) *opentile.Slide {
	t.Helper()
	dir := os.Getenv("OPENTILE_TESTDIR")
	if dir == "" {
		t.Skip("OPENTILE_TESTDIR not set")
	}
	path := filepath.Join(dir, rel)
	if _, err := os.Stat(path); err != nil {
		t.Skipf("sample missing: %v", err)
	}
	slide, err := opentile.OpenFile(path)
	if err != nil {
		t.Fatalf("OpenFile: %v", err)
	}
	t.Cleanup(func() { slide.Close() })
	return slide
}

func TestScaledStripsWholeSlideSVS(t *testing.T) {
	slide := openStripSample(t, "svs/CMU-1-Small-Region.svs")
	lvl := slide.Levels()[0]
	it := slide.Pyramid(0).ScaledStrips(
		opentile.Region{Origin: opentile.Point{X: 0, Y: 0}, Size: lvl.Size},
		opentile.Size{W: 512, H: 512},
		64,
	)
	defer it.Close()

	if it.Strips() != 8 {
		t.Errorf("Strips: got %d, want 8 (512/64)", it.Strips())
	}

	stripCount := 0
	for {
		img, err := it.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("Next strip %d: %v", stripCount, err)
		}
		if img.Width != 512 {
			t.Errorf("strip %d width: got %d, want 512", stripCount, img.Width)
		}
		stripCount++
	}
	if stripCount != 8 {
		t.Errorf("got %d strips, want 8", stripCount)
	}
}

func TestScaledStripsCrossFormat(t *testing.T) {
	samples := []struct {
		name string
		rel  string
	}{
		{"SVS", "svs/CMU-1-Small-Region.svs"},
		{"NDPI", "ndpi/CMU-1.ndpi"},
		{"OMETIFF", "ome-tiff/Leica-1.ome.tiff"},
		{"BIF", "bif/OS-1.bif"},
	}
	for _, s := range samples {
		t.Run(s.name, func(t *testing.T) {
			slide := openStripSample(t, s.rel)
			lvl := slide.Levels()[0]
			it := slide.Pyramid(0).ScaledStrips(
				opentile.Region{Origin: opentile.Point{X: 0, Y: 0}, Size: lvl.Size},
				opentile.Size{W: 128, H: 128},
				64,
			)
			defer it.Close()

			imgs := 0
			for {
				img, err := it.Next()
				if err == io.EOF {
					break
				}
				if err != nil {
					t.Fatalf("Next: %v", err)
				}
				if img.Width != 128 {
					t.Errorf("strip width: got %d, want 128", img.Width)
				}
				imgs++
			}
			if imgs == 0 {
				t.Errorf("yielded 0 strips")
			}
		})
	}
}

// TestBIFScaledStripsStitchedGeometry asserts that ScaledStrips and
// ReadRegionScaled for a Ventana DP 200 BIF slide use the stitched L0 extent
// (23432×21504, the compacted hull), not the padded raw-frame IFD extent
// (24576×21504). This exercises the layout-aware tile selection + blit path
// added in #60 for the ScaledStrips iterator.
//
// The key assertion: when scaling the full stitched L0 to a small target, the
// output width must be derived from 23432, not 24576. The test uses a 1/8
// approximate scale: outW = ceil(23432/8) = 2929 ≠ ceil(24576/8) = 3072.
//
// Skipped when Ventana-1.bif is not present locally.
func TestBIFScaledStripsStitchedGeometry(t *testing.T) {
	const (
		stitchedW = 23432
		stitchedH = 21504
		paddedW   = 24576 // raw IFD ImageWidth before #60 fix — wrong
	)
	path := func() string {
		if dir := os.Getenv("OPENTILE_TESTDIR"); dir != "" {
			p := filepath.Join(dir, "bif", "Ventana-1.bif")
			if _, err := os.Stat(p); err == nil {
				return p
			}
		}
		const fallback = "/Volumes/Ext/GitHub/opentile-go/sample_files/bif/Ventana-1.bif"
		if _, err := os.Stat(fallback); err == nil {
			return fallback
		}
		return ""
	}()
	if path == "" {
		t.Skip("Ventana-1.bif not present")
	}

	slide, err := opentile.OpenFile(path)
	if err != nil {
		t.Fatal(err)
	}
	defer slide.Close()

	lvl0, err := slide.Level(0)
	if err != nil {
		t.Fatal(err)
	}
	if lvl0.Size.W != stitchedW || lvl0.Size.H != stitchedH {
		t.Fatalf("precondition: L0 size = %dx%d, want %dx%d (stitched hull)",
			lvl0.Size.W, lvl0.Size.H, stitchedW, stitchedH)
	}

	// Target: ~1/8 scale. outW = ceil(23432/8) = 2929 (stitched).
	// If geometry were derived from the padded 24576: ceil(24576/8) = 3072.
	const scale = 8
	outW := (stitchedW + scale - 1) / scale  // 2929
	outH := (stitchedH + scale - 1) / scale  // 2688
	wrongW := (paddedW + scale - 1) / scale  // 3072 — wrong, padded

	t0src := opentile.Region{
		Origin: opentile.Point{X: 0, Y: 0},
		Size:   opentile.Size{W: stitchedW, H: stitchedH},
	}
	outSize := opentile.Size{W: outW, H: outH}

	// --- ScaledStrips geometry ---
	it := slide.Pyramid(0).ScaledStrips(t0src, outSize, 64)
	defer it.Close()
	stripCount := 0
	for {
		img, err := it.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("ScaledStrips Next: %v", err)
		}
		if img.Width != outW {
			t.Errorf("ScaledStrips strip %d: width = %d, want %d (stitched); got %d (padded) as bug indicator",
				stripCount, img.Width, outW, wrongW)
		}
		stripCount++
	}
	if stripCount == 0 {
		t.Fatal("ScaledStrips yielded 0 strips")
	}
	t.Logf("ScaledStrips: %d strips at %dx(up to %d)px — stitched geometry confirmed", stripCount, outW, 64)

	// --- ReadRegionScaled geometry ---
	img, err := slide.Pyramid(0).ReadRegionScaled(t0src, outSize)
	if err != nil {
		t.Fatalf("ReadRegionScaled: %v", err)
	}
	if img.Width != outW || img.Height != outH {
		t.Errorf("ReadRegionScaled: dims = %dx%d, want %dx%d (stitched; padded would be %dx%d)",
			img.Width, img.Height, outW, outH, wrongW, outH)
	}
	t.Logf("ReadRegionScaled: %dx%d — stitched geometry confirmed", img.Width, img.Height)
}

func TestScaledStripsCancellation(t *testing.T) {
	slide := openStripSample(t, "svs/CMU-1.svs")
	lvl := slide.Levels()[0]
	it := slide.Pyramid(0).ScaledStrips(
		opentile.Region{Origin: opentile.Point{X: 0, Y: 0}, Size: lvl.Size},
		opentile.Size{W: 2048, H: 2048},
		64,
	)
	// Read one strip then close mid-stream.
	if _, err := it.Next(); err != nil {
		t.Fatalf("first Next: %v", err)
	}
	if err := it.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if _, err := it.Next(); err != io.ErrClosedPipe {
		t.Errorf("Next after Close: got %v, want io.ErrClosedPipe", err)
	}
}
