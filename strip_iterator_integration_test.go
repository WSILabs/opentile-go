package opentile_test

import (
	"image"
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
	it := slide.ScaledStrips(
		image.Rect(0, 0, lvl.Size.W, lvl.Size.H),
		image.Point{X: 512, Y: 512},
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
			it := slide.ScaledStrips(
				image.Rect(0, 0, lvl.Size.W, lvl.Size.H),
				image.Point{X: 128, Y: 128},
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

func TestScaledStripsCancellation(t *testing.T) {
	slide := openStripSample(t, "svs/CMU-1.svs")
	lvl := slide.Levels()[0]
	it := slide.ScaledStrips(
		image.Rect(0, 0, lvl.Size.W, lvl.Size.H),
		image.Point{X: 2048, Y: 2048},
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
